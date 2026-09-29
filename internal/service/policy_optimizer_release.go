package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"sendllm/internal/dto"
)

// PolicyOptimizerReleaseConfig 指定一次性不可变 release 发布的全部前置输入。
type PolicyOptimizerReleaseConfig struct {
	Candidate     dto.PolicyOptimizerCandidate
	CandidateDir  string
	ReleasesDir   string
	TargetVersion string
	Approval      dto.PolicyOptimizerApproval
	Regression    dto.PolicyOptimizerRegressionReport
	Critic        dto.PolicyOptimizerCritique
	Gate          dto.PolicyOptimizerGatePolicy
}

var policyOptimizerReleaseVersionPattern = regexp.MustCompile(`^p04b-v[1-9][0-9]*\.[0-9]+$`)

// CreatePolicyApproval 创建绑定 candidate/regression/critic 哈希的人工审批。
func CreatePolicyApproval(
	candidate dto.PolicyOptimizerCandidate,
	regression dto.PolicyOptimizerRegressionReport,
	critic dto.PolicyOptimizerCritique,
	approver string,
	at time.Time,
) (dto.PolicyOptimizerApproval, error) {
	if strings.TrimSpace(candidate.Version) == "" || strings.TrimSpace(candidate.SHA256) == "" ||
		strings.TrimSpace(approver) == "" || at.IsZero() {
		return dto.PolicyOptimizerApproval{}, fmt.Errorf("policy optimizer release approval identity is incomplete")
	}
	candidateHash, err := PolicyOptimizerGoldSubjectHash(candidate)
	if err != nil {
		return dto.PolicyOptimizerApproval{}, err
	}
	regressionHash, err := PolicyOptimizerGoldSubjectHash(regression)
	if err != nil {
		return dto.PolicyOptimizerApproval{}, err
	}
	criticHash, err := PolicyOptimizerGoldSubjectHash(critic)
	if err != nil {
		return dto.PolicyOptimizerApproval{}, err
	}
	gateHash, err := PolicyOptimizerGoldSubjectHash(policyOptimizerDefaultP04BGatePolicy())
	if err != nil {
		return dto.PolicyOptimizerApproval{}, err
	}
	related := map[string]string{
		"candidate": candidateHash, "regression_report": regressionHash,
		"gate_report": gateHash, "critic_report": criticHash,
	}
	raw, err := json.Marshal([]string{"policy_release", candidate.Version, candidateHash, approver, at.UTC().Format(time.RFC3339Nano)})
	if err != nil {
		return dto.PolicyOptimizerApproval{}, fmt.Errorf("encode policy optimizer approval identity: %w", err)
	}
	return dto.PolicyOptimizerApproval{
		Version: 1, ApprovalID: "AP:" + policyOptimizerHashBytes(raw), SubjectType: "policy_release",
		SubjectID: candidate.Version, SubjectSHA256: candidateHash, RelatedSHA256: related,
		ApproverID: approver, Decision: "approved", CreatedAt: at.UTC(),
	}, nil
}

// ReleasePolicy 校验全部前置条件后原子发布不可变策略 bundle。
func ReleasePolicy(ctx context.Context, cfg PolicyOptimizerReleaseConfig) (dto.PolicyOptimizerRelease, error) {
	if ctx.Err() != nil {
		return dto.PolicyOptimizerRelease{}, ctx.Err()
	}
	if !policyOptimizerReleaseVersionPattern.MatchString(cfg.TargetVersion) {
		return dto.PolicyOptimizerRelease{}, fmt.Errorf("policy optimizer release version is invalid")
	}
	if cfg.Critic.Verdict == "block" {
		return dto.PolicyOptimizerRelease{}, fmt.Errorf("policy optimizer release is blocked by critic")
	}
	if err := EvaluateP04BGates(cfg.Regression, cfg.Gate); err != nil {
		return dto.PolicyOptimizerRelease{}, err
	}
	candidateBundle, err := VerifyPolicyBundle(cfg.CandidateDir)
	if err != nil {
		return dto.PolicyOptimizerRelease{}, err
	}
	if candidateBundle.AggregateHash != cfg.Candidate.SHA256 {
		return dto.PolicyOptimizerRelease{}, fmt.Errorf("policy optimizer candidate hash mismatch")
	}
	if err := validatePolicyOptimizerReleaseApproval(
		cfg.Approval, cfg.Candidate, cfg.Regression, cfg.Critic, cfg.Gate,
	); err != nil {
		return dto.PolicyOptimizerRelease{}, err
	}
	destination := filepath.Join(cfg.ReleasesDir, cfg.TargetVersion)
	if !policyOptimizerPathInside(cfg.ReleasesDir, destination) {
		return dto.PolicyOptimizerRelease{}, fmt.Errorf("policy optimizer release path escapes releases directory")
	}
	if _, err := os.Stat(destination); err == nil {
		return dto.PolicyOptimizerRelease{}, fmt.Errorf("policy optimizer release version already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return dto.PolicyOptimizerRelease{}, fmt.Errorf("inspect policy optimizer release destination: %w", err)
	}
	if err := os.MkdirAll(cfg.ReleasesDir, 0o700); err != nil {
		return dto.PolicyOptimizerRelease{}, fmt.Errorf("create policy optimizer releases directory: %w", err)
	}
	staging := filepath.Join(cfg.ReleasesDir, "."+cfg.TargetVersion+".staging")
	if _, err := os.Stat(staging); err == nil {
		return dto.PolicyOptimizerRelease{}, fmt.Errorf("policy optimizer release staging path exists")
	}
	if err := copyPolicyOptimizerBundle(cfg.CandidateDir, staging); err != nil {
		return dto.PolicyOptimizerRelease{}, err
	}
	defer func() { _ = os.RemoveAll(staging) }()
	if err := updatePolicyOptimizerReleaseVersion(staging, cfg.TargetVersion); err != nil {
		return dto.PolicyOptimizerRelease{}, err
	}
	manifest, err := loadSafetyReviewManifest(filepath.Join(staging, "release.yaml"))
	if err != nil {
		return dto.PolicyOptimizerRelease{}, err
	}
	if _, err := CompilePolicyPrompts(staging, PolicyOptimizerCompileConfig{
		CompilerVersion: manifest.CompilerVersion,
	}); err != nil {
		return dto.PolicyOptimizerRelease{}, err
	}
	if _, err := VerifyPolicyBundle(staging); err != nil {
		return dto.PolicyOptimizerRelease{}, err
	}
	if err := os.Rename(staging, destination); err != nil {
		return dto.PolicyOptimizerRelease{}, fmt.Errorf("publish policy optimizer release: %w", err)
	}
	released, err := VerifyPolicyBundle(destination)
	if err != nil {
		return dto.PolicyOptimizerRelease{}, err
	}
	return dto.PolicyOptimizerRelease{
		Version: cfg.TargetVersion, Candidate: cfg.Candidate.Version,
		Path: destination, SHA256: released.AggregateHash, ApprovalID: cfg.Approval.ApprovalID,
		ReleasedAt: time.Now().UTC(),
	}, nil
}

// policyOptimizerHashBytes 计算字节稳定 SHA-256。
func policyOptimizerHashBytes(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

// policyOptimizerPathInside 判断 target 是否位于 root 内。
func policyOptimizerPathInside(root, target string) bool {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absoluteTarget, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(absoluteRoot, absoluteTarget)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// validatePolicyOptimizerReleaseApproval 校验审批主体和相关哈希。
func validatePolicyOptimizerReleaseApproval(
	approval dto.PolicyOptimizerApproval,
	candidate dto.PolicyOptimizerCandidate,
	regression dto.PolicyOptimizerRegressionReport,
	critic dto.PolicyOptimizerCritique,
	gate dto.PolicyOptimizerGatePolicy,
) error {
	if approval.Version != 1 || approval.SubjectType != "policy_release" ||
		approval.SubjectID != candidate.Version || approval.Decision != "approved" ||
		strings.TrimSpace(approval.ApprovalID) == "" || strings.TrimSpace(approval.ApproverID) == "" ||
		approval.CreatedAt.IsZero() {
		return fmt.Errorf("policy optimizer release approval is invalid")
	}
	candidateHash, err := PolicyOptimizerGoldSubjectHash(candidate)
	if err != nil {
		return err
	}
	if approval.SubjectSHA256 != candidateHash || approval.RelatedSHA256["candidate"] != candidateHash {
		return fmt.Errorf("policy optimizer release approval candidate hash mismatch")
	}
	regressionHash, err := PolicyOptimizerGoldSubjectHash(regression)
	if err != nil {
		return err
	}
	criticHash, err := PolicyOptimizerGoldSubjectHash(critic)
	if err != nil {
		return err
	}
	if approval.RelatedSHA256["regression_report"] != regressionHash || approval.RelatedSHA256["critic_report"] != criticHash {
		return fmt.Errorf("policy optimizer release approval related hash mismatch")
	}
	gateHash, err := PolicyOptimizerGoldSubjectHash(gate)
	if err != nil {
		return err
	}
	if approval.RelatedSHA256["gate_report"] != gateHash {
		return fmt.Errorf("policy optimizer release approval gate hash mismatch")
	}
	return nil
}
