package service

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"sendllm/internal/dto"
)

// PolicyOptimizerAuditPackage 表示已验证路径边界的 Audit Package。
type PolicyOptimizerAuditPackage struct {
	Root       string
	Manifest   dto.PolicyOptimizerAuditManifest
	SourcePath map[string]string
}

// PolicyOptimizerSource 表示一个已载入的审计源。
type PolicyOptimizerSource struct {
	Manifest dto.PolicyOptimizerSourceManifest
	Path     string
	SHA256   string
	Count    int
}

// AuditInspectConfig 指定一次结构检查的输入。
type AuditInspectConfig struct {
	Package *PolicyOptimizerAuditPackage
}

// LoadAuditPackage 严格加载 Audit Package manifest 并校验源路径。
func LoadAuditPackage(path string) (*PolicyOptimizerAuditPackage, error) {
	root, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve audit package: %w", err)
	}
	manifestPath := filepath.Join(root, "manifest.yaml")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read audit package manifest: %w", err)
	}
	manifest := dto.PolicyOptimizerAuditManifest{}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("parse audit package manifest: %w", err)
	}
	if manifest.Version != 1 || manifest.PackageID == "" ||
		manifest.CurrentPolicyVersion == "" || len(manifest.Sources) == 0 {
		return nil, fmt.Errorf("audit package manifest identity is incomplete")
	}
	sourcePaths := make(map[string]string, len(manifest.Sources))
	for _, source := range manifest.Sources {
		if source.SourceID == "" || source.Type == "" || source.Path == "" ||
			source.Format == "" || source.ExpectedRecords < 0 {
			return nil, fmt.Errorf("audit source %q is incomplete", source.SourceID)
		}
		if _, exists := sourcePaths[source.SourceID]; exists {
			return nil, fmt.Errorf("audit source %q is duplicated", source.SourceID)
		}
		fullPath, err := containedPolicyOptimizerPath(root, source.Path)
		if err != nil {
			return nil, fmt.Errorf("audit source %q: %w", source.SourceID, err)
		}
		sourcePaths[source.SourceID] = fullPath
	}
	return &PolicyOptimizerAuditPackage{Root: root, Manifest: manifest, SourcePath: sourcePaths}, nil
}

// InspectAuditPackage 执行确定性结构检查并报告映射审批缺口。
func InspectAuditPackage(ctx context.Context, cfg AuditInspectConfig) (dto.PolicyOptimizerInspection, error) {
	if cfg.Package == nil {
		return dto.PolicyOptimizerInspection{}, fmt.Errorf("audit package is nil")
	}
	result := dto.PolicyOptimizerInspection{
		PackageID:            cfg.Package.Manifest.PackageID,
		CurrentPolicyVersion: cfg.Package.Manifest.CurrentPolicyVersion,
		Sources:              make([]dto.PolicyOptimizerSourceInspection, 0, len(cfg.Package.Manifest.Sources)),
	}
	for _, source := range cfg.Package.Manifest.Sources {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		path := cfg.Package.SourcePath[source.SourceID]
		hash, count, err := inspectPolicyOptimizerSource(path, source)
		if err != nil {
			return result, err
		}
		mappingStatus, err := inspectPolicyOptimizerMappingStatus(cfg.Package, source, hash)
		if err != nil {
			return result, err
		}
		if source.ExpectedRecords != 0 && source.ExpectedRecords != count {
			return result, fmt.Errorf("audit source %q count mismatch", source.SourceID)
		}
		if mappingStatus != "approved" {
			result.NeedsApproval = true
		}
		result.Sources = append(result.Sources, dto.PolicyOptimizerSourceInspection{
			SourceID: source.SourceID, SourceType: source.Type, Format: source.Format,
			SHA256: hash, ExpectedCount: source.ExpectedRecords, ActualCount: count,
			MappingStatus:    mappingStatus,
			SuitableAnalyses: []string{"disagreement", "error_pattern"},
			UnsuitableUses:   []string{"direct_gold_without_approval"},
			Ambiguities:      []string{},
		})
	}
	return result, nil
}

// ApproveAuditMapping 绑定源哈希、审批人和时间，并写入新映射文件。
func ApproveAuditMapping(candidatePath, outputPath, approver string, approvedAt time.Time) error {
	if strings.TrimSpace(candidatePath) == "" || strings.TrimSpace(outputPath) == "" ||
		strings.TrimSpace(approver) == "" || approvedAt.IsZero() {
		return fmt.Errorf("audit mapping approval is incomplete")
	}
	raw, err := os.ReadFile(candidatePath)
	if err != nil {
		return fmt.Errorf("read audit mapping candidate: %w", err)
	}
	mapping := dto.PolicyOptimizerMapping{}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&mapping); err != nil {
		return fmt.Errorf("parse audit mapping candidate: %w", err)
	}
	sourceHash := mapping.SourceSHA256
	if sourceHash == "" {
		return fmt.Errorf("audit mapping source hash is required")
	}
	mapping.Status = "approved"
	mapping.ApprovedBy = approver
	mapping.ApprovedAt = approvedAt.UTC()
	encoded, err := yaml.Marshal(mapping)
	if err != nil {
		return fmt.Errorf("encode audit mapping: %w", err)
	}
	if err := os.WriteFile(outputPath, encoded, 0o600); err != nil {
		return fmt.Errorf("write approved audit mapping: %w", err)
	}
	return nil
}

// ValidateApprovedMapping 校验 Mapping 与 Source 的哈希和字段闭环。
func ValidateApprovedMapping(mapping dto.PolicyOptimizerMapping, source PolicyOptimizerSource) error {
	if mapping.Version != 1 || mapping.Status != "approved" ||
		mapping.SourceID != source.Manifest.SourceID ||
		mapping.SourceSHA256 != source.SHA256 || mapping.Format != source.Manifest.Format {
		return fmt.Errorf("approved audit mapping identity is invalid")
	}
	if mapping.ApprovedBy == "" || mapping.ApprovedAt.IsZero() ||
		mapping.RecordSelector != "each_record" || len(mapping.Fields) == 0 {
		return fmt.Errorf("approved audit mapping approval fields are invalid")
	}
	if len(mapping.Judgments) < 2 ||
		mapping.Comparison.Left < 0 || mapping.Comparison.Right < 0 ||
		mapping.Comparison.Left >= len(mapping.Judgments) ||
		mapping.Comparison.Right >= len(mapping.Judgments) {
		return fmt.Errorf("approved audit mapping judgments are invalid")
	}
	return nil
}

// containedPolicyOptimizerPath 返回包含在 package root 下的绝对路径。
func containedPolicyOptimizerPath(root, relative string) (string, error) {
	if filepath.IsAbs(relative) {
		return "", fmt.Errorf("absolute source path is forbidden")
	}
	target := filepath.Clean(filepath.Join(root, filepath.FromSlash(relative)))
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("source path escapes audit package")
	}
	return target, nil
}

// inspectPolicyOptimizerSource 计算源哈希和记录数。
func inspectPolicyOptimizerSource(path string, source dto.PolicyOptimizerSourceManifest) (string, int, error) {
	if source.Format != "jsonl" && source.Format != "csv" && source.Format != "markdown" {
		return "", 0, fmt.Errorf("audit source %q format is invalid", source.SourceID)
	}
	file, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("open audit source %q: %w", source.SourceID, err)
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	count := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Bytes()
		_, _ = hash.Write(line)
		_, _ = hash.Write([]byte("\n"))
		if strings.TrimSpace(string(line)) != "" {
			count++
		}
	}
	if err := scanner.Err(); err != nil {
		return "", 0, fmt.Errorf("scan audit source %q: %w", source.SourceID, err)
	}
	return hex.EncodeToString(hash.Sum(nil)), count, nil
}

// inspectPolicyOptimizerMappingStatus 返回源的映射状态并校验哈希。
func inspectPolicyOptimizerMappingStatus(
	pkg *PolicyOptimizerAuditPackage,
	source dto.PolicyOptimizerSourceManifest,
	sourceHash string,
) (string, error) {
	if source.Mapping == "" {
		return "not_required", nil
	}
	mappingPath, err := containedPolicyOptimizerPath(pkg.Root, source.Mapping)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(mappingPath)
	if err != nil {
		return "candidate", nil
	}
	mapping := dto.PolicyOptimizerMapping{}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&mapping); err != nil {
		return "", fmt.Errorf("parse audit mapping for %q: %w", source.SourceID, err)
	}
	if mapping.Status != "approved" {
		return "candidate", nil
	}
	if mapping.SourceSHA256 != sourceHash {
		return "invalidated", nil
	}
	return "approved", nil
}
