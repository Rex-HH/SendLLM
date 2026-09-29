package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"sendllm/internal/dto"
)

// CreateCandidateGold 从 Case Adjudication 候选创建不可变 Candidate Gold。
func CreateCandidateGold(input dto.PolicyOptimizerCandidateGoldInput) (dto.PolicyOptimizerGoldRecord, error) {
	if strings.TrimSpace(input.SampleRef) == "" || strings.TrimSpace(input.SourceSHA256) == "" ||
		strings.TrimSpace(input.PolicyVersion) == "" || strings.TrimSpace(input.ModelCandidateArtifact) == "" {
		return dto.PolicyOptimizerGoldRecord{}, fmt.Errorf("policy optimizer candidate gold identity is incomplete")
	}
	if input.Label != "safe" && input.Label != "unsafe" {
		return dto.PolicyOptimizerGoldRecord{}, fmt.Errorf("policy optimizer candidate gold label is invalid")
	}
	if input.CaseType == "" || len(input.RuleIDs) == 0 || len(input.DecisionIDs) == 0 {
		return dto.PolicyOptimizerGoldRecord{}, fmt.Errorf("policy optimizer candidate gold evidence is incomplete")
	}
	goldID, err := policyOptimizerGoldID(input)
	if err != nil {
		return dto.PolicyOptimizerGoldRecord{}, err
	}
	now := time.Now().UTC()
	return dto.PolicyOptimizerGoldRecord{
		GoldID: goldID, SampleRef: input.SampleRef, SourceSHA256: input.SourceSHA256,
		PolicyVersion: input.PolicyVersion, Status: "candidate", Label: input.Label,
		RiskTypes: append([]string(nil), input.RiskTypes...), CaseType: input.CaseType,
		RuleIDs: append([]string(nil), input.RuleIDs...), DecisionIDs: append([]string(nil), input.DecisionIDs...),
		ModelCandidateArtifact: input.ModelCandidateArtifact, CreatedAt: now, UpdatedAt: now,
	}, nil
}

// ApproveGold 绑定人工审批并把 Candidate Gold 提升为 Approved Gold。
func ApproveGold(candidate dto.PolicyOptimizerGoldRecord, approval dto.PolicyOptimizerGoldApproval) (dto.PolicyOptimizerGoldRecord, error) {
	if candidate.Status != "candidate" || candidate.ApprovalArtifact != "" {
		return dto.PolicyOptimizerGoldRecord{}, fmt.Errorf("policy optimizer gold candidate status is invalid")
	}
	if err := validatePolicyOptimizerGoldApproval(approval, "gold", candidate.GoldID, candidate); err != nil {
		return dto.PolicyOptimizerGoldRecord{}, err
	}
	candidate.Status = "approved"
	candidate.ApprovalArtifact = approval.ApprovalID
	candidate.UpdatedAt = approval.CreatedAt
	return candidate, nil
}

// PromoteCoreGold 在历史满足三个成功 release 且无未解决挑战时提升 Core Gold。
func PromoteCoreGold(
	approved dto.PolicyOptimizerGoldRecord,
	history dto.PolicyOptimizerGoldHistory,
	approval dto.PolicyOptimizerGoldApproval,
) (dto.PolicyOptimizerGoldRecord, error) {
	if approved.Status != "approved" {
		return dto.PolicyOptimizerGoldRecord{}, fmt.Errorf("policy optimizer core gold requires approved ancestry")
	}
	releases := sortDedupStrings(history.SuccessfulReleaseIDs)
	if len(releases) < 3 {
		return dto.PolicyOptimizerGoldRecord{}, fmt.Errorf("policy optimizer core gold requires three successful releases")
	}
	if len(sortDedupStrings(history.UnresolvedChallengeIDs)) != 0 {
		return dto.PolicyOptimizerGoldRecord{}, fmt.Errorf("policy optimizer core gold has unresolved challenges")
	}
	if err := validatePolicyOptimizerGoldApproval(approval, "core_gold", approved.GoldID, approved); err != nil {
		return dto.PolicyOptimizerGoldRecord{}, err
	}
	approved.Status = "core"
	approved.SuccessfulReleaseIDs = releases
	approved.ChallengeIDs = []string{}
	approved.UpdatedAt = approval.CreatedAt
	return approved, nil
}

// PolicyOptimizerGoldSubjectHash 计算审批绑定的规范主体哈希。
func PolicyOptimizerGoldSubjectHash(subject any) (string, error) {
	raw, err := json.Marshal(subject)
	if err != nil {
		return "", fmt.Errorf("encode policy optimizer gold subject: %w", err)
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

// validatePolicyOptimizerGoldApproval 校验审批主体、审批人和主体哈希。
func validatePolicyOptimizerGoldApproval(
	approval dto.PolicyOptimizerGoldApproval,
	subjectType string,
	subjectID string,
	subject any,
) error {
	if approval.Version != 1 || approval.SubjectType != subjectType || approval.SubjectID != subjectID ||
		strings.TrimSpace(approval.ApprovalID) == "" || strings.TrimSpace(approval.ApproverID) == "" ||
		approval.Decision != "approved" || approval.CreatedAt.IsZero() {
		return fmt.Errorf("policy optimizer gold approval is invalid")
	}
	hash, err := PolicyOptimizerGoldSubjectHash(subject)
	if err != nil {
		return err
	}
	if approval.SubjectSHA256 != hash {
		return fmt.Errorf("policy optimizer gold approval subject hash mismatch")
	}
	return nil
}

// policyOptimizerGoldID 按 sample/source/policy 计算确定性 Gold ID。
func policyOptimizerGoldID(input dto.PolicyOptimizerCandidateGoldInput) (string, error) {
	raw, err := json.Marshal([]string{input.SampleRef, input.SourceSHA256, input.PolicyVersion})
	if err != nil {
		return "", fmt.Errorf("encode policy optimizer gold identity: %w", err)
	}
	digest := sha256.Sum256(raw)
	return "G:" + hex.EncodeToString(digest[:]), nil
}
