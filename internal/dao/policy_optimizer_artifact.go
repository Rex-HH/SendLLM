package dao

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PolicyOptimizerArtifact 表示一条不可变 Artifact 元数据。
type PolicyOptimizerArtifact struct {
	ID                   string
	IterationID          string
	Type                 string
	RelativePath         string
	MediaType            string
	SHA256               string
	ByteSize             int64
	ProducerStage        string
	ProducerSkill        string
	ProducerModelProfile string
	ParentSHA256         []string
	Sensitivity          string
	Active               bool
}

// PolicyOptimizerArtifactWrite 表示一次不可变 Artifact 写入。
type PolicyOptimizerArtifactWrite struct {
	ID                   string
	IterationID          string
	Type                 string
	RelativePath         string
	MediaType            string
	Bytes                []byte
	ProducerStage        string
	ProducerSkill        string
	ProducerModelProfile string
	ParentSHA256         []string
	Sensitivity          string
	Active               bool
}

// PolicyOptimizerArtifactStore 表示绑定迭代目录的 Artifact 存储。
type PolicyOptimizerArtifactStore struct {
	store *PolicyOptimizerStore
	root  string
}

// NewPolicyOptimizerArtifactStore 构造迭代根目录下的 Artifact Store。
func NewPolicyOptimizerArtifactStore(store *PolicyOptimizerStore, root string) (*PolicyOptimizerArtifactStore, error) {
	if store == nil || strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("policy optimizer artifact store requires state and root")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve policy optimizer artifact root: %w", err)
	}
	return &PolicyOptimizerArtifactStore{store: store, root: absolute}, nil
}

// Put 写入不可变 Artifact，并在同一事务中登记元数据。
func (a *PolicyOptimizerArtifactStore) Put(
	ctx context.Context,
	write PolicyOptimizerArtifactWrite,
) (PolicyOptimizerArtifact, error) {
	if write.ID == "" || write.IterationID == "" || write.Type == "" ||
		write.RelativePath == "" || write.MediaType == "" || write.ProducerStage == "" {
		return PolicyOptimizerArtifact{}, fmt.Errorf("policy optimizer artifact write is incomplete")
	}
	if err := validatePolicyOptimizerArtifactPath(a.root, write.RelativePath); err != nil {
		return PolicyOptimizerArtifact{}, err
	}
	target := filepath.Join(a.root, filepath.FromSlash(write.RelativePath))
	if _, err := os.Lstat(target); err == nil {
		return PolicyOptimizerArtifact{}, fmt.Errorf("policy optimizer artifact already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return PolicyOptimizerArtifact{}, fmt.Errorf("inspect policy optimizer artifact target: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return PolicyOptimizerArtifact{}, fmt.Errorf("create policy optimizer artifact directory: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(target), ".policy-optimizer-*")
	if err != nil {
		return PolicyOptimizerArtifact{}, fmt.Errorf("create policy optimizer artifact temp: %w", err)
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if _, err := temp.Write(write.Bytes); err != nil {
		_ = temp.Close()
		return PolicyOptimizerArtifact{}, fmt.Errorf("write policy optimizer artifact temp: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return PolicyOptimizerArtifact{}, fmt.Errorf("sync policy optimizer artifact temp: %w", err)
	}
	if err := temp.Close(); err != nil {
		return PolicyOptimizerArtifact{}, fmt.Errorf("close policy optimizer artifact temp: %w", err)
	}
	if err := os.Rename(tempPath, target); err != nil {
		return PolicyOptimizerArtifact{}, fmt.Errorf("publish policy optimizer artifact: %w", err)
	}
	digest := sha256.Sum256(write.Bytes)
	artifact := PolicyOptimizerArtifact{
		ID: write.ID, IterationID: write.IterationID, Type: write.Type,
		RelativePath: write.RelativePath, MediaType: write.MediaType,
		SHA256: hex.EncodeToString(digest[:]), ByteSize: int64(len(write.Bytes)),
		ProducerStage: write.ProducerStage, ProducerSkill: write.ProducerSkill,
		ProducerModelProfile: write.ProducerModelProfile,
		ParentSHA256:         append([]string(nil), write.ParentSHA256...),
		Sensitivity:          write.Sensitivity, Active: write.Active,
	}
	if err := a.insertArtifact(ctx, artifact); err != nil {
		_ = os.Remove(target)
		return PolicyOptimizerArtifact{}, err
	}
	return artifact, nil
}

// insertArtifact 在单个事务中登记 Artifact 元数据。
func (a *PolicyOptimizerArtifactStore) insertArtifact(
	ctx context.Context,
	artifact PolicyOptimizerArtifact,
) error {
	parents, err := json.Marshal(artifact.ParentSHA256)
	if err != nil {
		return fmt.Errorf("encode policy optimizer parent hashes: %w", err)
	}
	active := 0
	if artifact.Active {
		active = 1
	}
	now := policyOptimizerTimestamp(time.Now())
	tx, err := a.store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin policy optimizer artifact insert: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO optimization_artifacts (
		artifact_id, iteration_id, artifact_type, relative_path, media_type, sha256,
		byte_size, producer_stage, producer_skill, producer_model_profile,
		parent_sha256_json, sensitivity, is_active, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		artifact.ID, artifact.IterationID, artifact.Type, artifact.RelativePath,
		artifact.MediaType, artifact.SHA256, artifact.ByteSize, artifact.ProducerStage,
		artifact.ProducerSkill, artifact.ProducerModelProfile, string(parents),
		artifact.Sensitivity, active, now,
	); err != nil {
		return fmt.Errorf("insert policy optimizer artifact: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit policy optimizer artifact: %w", err)
	}
	return nil
}

// validatePolicyOptimizerArtifactPath 拒绝绝对路径、空段和逃逸路径。
func validatePolicyOptimizerArtifactPath(root, relative string) error {
	if filepath.IsAbs(relative) || filepath.Clean(relative) != relative ||
		relative == "." || strings.HasPrefix(relative, "../") {
		return fmt.Errorf("policy optimizer artifact path is invalid")
	}
	target := filepath.Join(root, filepath.FromSlash(relative))
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("policy optimizer artifact path escapes root")
	}
	return nil
}

// ReadArtifact 按 Artifact ID 读取元数据。
func (a *PolicyOptimizerArtifactStore) ReadArtifact(
	ctx context.Context,
	artifactID string,
) (PolicyOptimizerArtifact, error) {
	var artifact PolicyOptimizerArtifact
	var parents string
	var active int
	err := a.store.db.QueryRowContext(ctx, `SELECT artifact_id, iteration_id, artifact_type,
		relative_path, media_type, sha256, byte_size, producer_stage, producer_skill,
		producer_model_profile, parent_sha256_json, sensitivity, is_active
		FROM optimization_artifacts WHERE artifact_id = ?`, artifactID,
	).Scan(
		&artifact.ID, &artifact.IterationID, &artifact.Type, &artifact.RelativePath,
		&artifact.MediaType, &artifact.SHA256, &artifact.ByteSize, &artifact.ProducerStage,
		&artifact.ProducerSkill, &artifact.ProducerModelProfile, &parents,
		&artifact.Sensitivity, &active,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return artifact, ErrPolicyOptimizerNotFound
	}
	if err != nil {
		return artifact, fmt.Errorf("read policy optimizer artifact: %w", err)
	}
	if err := json.Unmarshal([]byte(parents), &artifact.ParentSHA256); err != nil {
		return artifact, fmt.Errorf("decode policy optimizer parent hashes: %w", err)
	}
	artifact.Active = active == 1
	return artifact, nil
}
