package dto

import (
	"testing"
	"time"
)

// TestPolicyOptimizerContractValidation 验证 Audit Record 和 Change Request 的冻结字段。
func TestPolicyOptimizerContractValidation(t *testing.T) {
	validRecord := PolicyOptimizerAuditRecord{
		RecordID: "AR:abc", SampleID: "sample-1", SourceID: "source-1",
		SourceType: "sft_vs_gold", SourceTask: "task-1", SelectionReason: "model_disagreement",
		Scene: PolicyOptimizerSceneResponse, Prompt: "synthetic prompt", Response: "synthetic response",
		Judgments: []PolicyOptimizerJudgment{
			{
				ActorType: "gold", ActorID: "gold-v1", Label: "safe",
				RiskTypes: []string{}, Authority: PolicyOptimizerAuthorityApprovedGold,
				SourceRef: "gold.jsonl:1",
			},
			{
				ActorType: "model", ActorID: "model-v1", Label: "unsafe",
				RiskTypes: []string{"ethnic_discrimination"},
				Authority: PolicyOptimizerAuthorityAutomatedProposal, SourceRef: "model.jsonl:1",
			},
		},
		PolicyVersion: "p04b-v1.0",
		Comparison:    PolicyOptimizerComparison{Left: 0, Right: 1, Type: PolicyOptimizerComparisonLabelMismatch},
		Metadata:      map[string]any{},
	}
	if err := ValidateAuditRecord(validRecord); err != nil {
		t.Fatalf("ValidateAuditRecord(valid) error = %v", err)
	}
	invalidRecord := validRecord
	invalidRecord.Comparison.Right = 9
	if err := ValidateAuditRecord(invalidRecord); err == nil {
		t.Fatal("ValidateAuditRecord accepted out-of-range comparison")
	}

	validRequest := PolicyOptimizerChangeRequest{
		Version: 1, ChangeID: "CR-1",
		Source:    PolicyOptimizerChangeSource{Type: "human", ActorID: "user-1"},
		Authority: PolicyOptimizerAuthorityHumanDirective,
		Targets:   []string{"expert_prompt"}, RequestedChanges: []string{"tighten ownership"},
		Rationale: "synthetic rationale", EvidenceRefs: []string{"AR:abc"},
		CreatedAt: time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC),
	}
	if err := ValidateChangeRequest(validRequest); err != nil {
		t.Fatalf("ValidateChangeRequest(valid) error = %v", err)
	}
	invalidRequest := validRequest
	invalidRequest.Version = 2
	if err := ValidateChangeRequest(invalidRequest); err == nil {
		t.Fatal("ValidateChangeRequest accepted invalid version")
	}
}
