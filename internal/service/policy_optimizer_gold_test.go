package service_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"sendllm/internal/dto"
	"sendllm/internal/service"
)

// TestPolicyOptimizerGoldLifecycle 验证 Candidate -> Approved -> Core 生命周期。
func TestPolicyOptimizerGoldLifecycle(t *testing.T) {
	candidate, err := service.CreateCandidateGold(dto.PolicyOptimizerCandidateGoldInput{
		SampleRef: "safety-review:sample-1", SourceSHA256: hashForPolicyOptimizerTest("source"),
		PolicyVersion: "p04b-v1.0", Label: "unsafe", RiskTypes: []string{"ethnic_discrimination"},
		CaseType: "typical", RuleIDs: []string{"DISCRIMINATION-R01"},
		DecisionIDs: []string{"P04B-DECISION-001"}, ModelCandidateArtifact: "ART:candidate",
	})
	if err != nil {
		t.Fatalf("CreateCandidateGold() error = %v", err)
	}
	if candidate.Status != "candidate" || candidate.ApprovalArtifact != "" || candidate.GoldID == "" {
		t.Fatalf("candidate = %+v", candidate)
	}
	approval := policyOptimizerGoldApproval(t, "AP:gold", "gold", candidate.GoldID, candidate)
	approved, err := service.ApproveGold(candidate, approval)
	if err != nil {
		t.Fatalf("ApproveGold() error = %v", err)
	}
	if approved.Status != "approved" || approved.ApprovalArtifact != approval.ApprovalID {
		t.Fatalf("approved = %+v", approved)
	}
	coreApproval := policyOptimizerGoldApproval(t, "AP:core", "core_gold", approved.GoldID, approved)
	core, err := service.PromoteCoreGold(approved, dto.PolicyOptimizerGoldHistory{
		SuccessfulReleaseIDs: []string{"p04b-v1.1", "p04b-v1.2", "p04b-v1.3"},
	}, coreApproval)
	if err != nil {
		t.Fatalf("PromoteCoreGold() error = %v", err)
	}
	if core.Status != "core" || len(core.SuccessfulReleaseIDs) != 3 {
		t.Fatalf("core = %+v", core)
	}
}

// TestPolicyOptimizerGoldRejectsInvalidPromotion 验证 Core 提升需要三个成功 release 且无未解决挑战。
func TestPolicyOptimizerGoldRejectsInvalidPromotion(t *testing.T) {
	candidate, err := service.CreateCandidateGold(dto.PolicyOptimizerCandidateGoldInput{
		SampleRef: "safety-review:sample-2", SourceSHA256: hashForPolicyOptimizerTest("source-2"),
		PolicyVersion: "p04b-v1.0", Label: "safe", CaseType: "hard_negative",
		RuleIDs: []string{"DISCRIMINATION-R01"}, DecisionIDs: []string{"P04B-DECISION-001"},
		ModelCandidateArtifact: "ART:candidate-2",
	})
	if err != nil {
		t.Fatalf("CreateCandidateGold() error = %v", err)
	}
	approved, err := service.ApproveGold(candidate, policyOptimizerGoldApproval(t, "AP:gold-2", "gold", candidate.GoldID, candidate))
	if err != nil {
		t.Fatalf("ApproveGold() error = %v", err)
	}
	approval := policyOptimizerGoldApproval(t, "AP:core-2", "core_gold", approved.GoldID, approved)
	if _, err := service.PromoteCoreGold(approved, dto.PolicyOptimizerGoldHistory{
		SuccessfulReleaseIDs: []string{"p04b-v1.1", "p04b-v1.2"},
	}, approval); err == nil {
		t.Fatal("PromoteCoreGold() expected insufficient-release error, got nil")
	}
	if _, err := service.PromoteCoreGold(approved, dto.PolicyOptimizerGoldHistory{
		SuccessfulReleaseIDs:   []string{"p04b-v1.1", "p04b-v1.2", "p04b-v1.3"},
		UnresolvedChallengeIDs: []string{"CH-1"},
	}, approval); err == nil {
		t.Fatal("PromoteCoreGold() expected unresolved-challenge error, got nil")
	}
}

// policyOptimizerGoldApproval 生成绑定 subject hash 的人工审批。
func policyOptimizerGoldApproval(
	t *testing.T,
	id string,
	subjectType string,
	subjectID string,
	subject any,
) dto.PolicyOptimizerGoldApproval {
	t.Helper()
	hash, err := service.PolicyOptimizerGoldSubjectHash(subject)
	if err != nil {
		t.Fatalf("PolicyOptimizerGoldSubjectHash() error = %v", err)
	}
	return dto.PolicyOptimizerGoldApproval{
		Version: 1, ApprovalID: id, SubjectType: subjectType, SubjectID: subjectID,
		SubjectSHA256: hash, ApproverID: "reviewer-1", Decision: "approved",
		CreatedAt: time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC),
	}
}

func hashForPolicyOptimizerTest(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}
