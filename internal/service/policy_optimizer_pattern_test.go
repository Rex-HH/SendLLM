package service_test

import (
	"testing"

	"sendllm/internal/dto"
	"sendllm/internal/service"
)

// TestPolicyOptimizerGlobalMergeAndAttachCases 验证确定性合并、计数重算和 case 附加。
func TestPolicyOptimizerGlobalMergeAndAttachCases(t *testing.T) {
	records := map[string]dto.PolicyOptimizerAuditRecordRef{
		"AR:1": {RecordID: "AR:1", SourceID: "source-1", ComparisonType: "label_mismatch"},
		"AR:2": {RecordID: "AR:2", SourceID: "source-1", ComparisonType: "label_mismatch"},
		"AR:3": {RecordID: "AR:3", SourceID: "source-2", ComparisonType: "case_type_mismatch"},
		"AR:4": {RecordID: "AR:4", SourceID: "source-2", ComparisonType: "no_comparison"},
	}
	local := []dto.PolicyOptimizerLocalPattern{
		{ID: "LP:B:01", BatchID: "B:H:000001", MergeKey: "key", Title: "one", Hypothesis: "h1", CoveredRecordIDs: []string{"AR:1", "AR:2"}, Confidence: "candidate"},
		{ID: "LP:B:02", BatchID: "B:H:000002", MergeKey: "key", Title: "two", Hypothesis: "h2", CoveredRecordIDs: []string{"AR:2", "AR:3", "AR:4"}, Confidence: "candidate"},
	}
	patterns, stats, err := service.RunGlobalMerge(service.PolicyOptimizerGlobalMergeConfig{LocalPatterns: local, Records: records})
	if err != nil {
		t.Fatalf("RunGlobalMerge() error = %v", err)
	}
	if stats.GlobalPatterns != 1 || len(patterns) != 1 || patterns[0].CoverageCount != 4 || patterns[0].BatchCount != 2 {
		t.Fatalf("patterns = %+v stats=%+v", patterns, stats)
	}
	attached, err := service.AttachPatternCases(patterns[0], records, "ITER-001")
	if err != nil {
		t.Fatalf("AttachPatternCases() error = %v", err)
	}
	if len(attached.RepresentativeIDs) == 0 || len(attached.BoundaryIDs) == 0 {
		t.Fatalf("attached pattern = %+v", attached)
	}
}

// TestPolicyOptimizerDiagnosisRejectsUnknownCause 验证诊断原因闭集。
func TestPolicyOptimizerDiagnosisRejectsUnknownCause(t *testing.T) {
	patterns := make([]dto.PolicyOptimizerGlobalPattern, 5)
	_, err := service.RunPolicyDiagnosis(service.PolicyOptimizerDiagnosisConfig{
		Patterns: patterns,
		Executor: func([]dto.PolicyOptimizerGlobalPattern) (dto.PolicyOptimizerDiagnosis, error) {
			return dto.PolicyOptimizerDiagnosis{Causes: []string{"invented"}}, nil
		},
	})
	if err == nil {
		t.Fatal("RunPolicyDiagnosis accepted unknown cause")
	}
}
