package service_test

import (
	"fmt"
	"testing"

	"sendllm/internal/dto"
	"sendllm/internal/service"
)

// TestPolicyOptimizerStratifyDeterministicCounts 验证 70/20/10、唯一分配和批次边界。
func TestPolicyOptimizerStratifyDeterministicCounts(t *testing.T) {
	records := make([]dto.PolicyOptimizerAuditRecordRef, 10000)
	for index := range records {
		records[index] = dto.PolicyOptimizerAuditRecordRef{
			RecordID: fmt.Sprintf("AR:%05d", index), SourceID: "source-1",
			Scene: "response", ComparisonType: "label_mismatch", Category: "ethnic_discrimination",
		}
	}
	cfg := service.PolicyOptimizerBatchingConfig{
		HomogeneousPercent: 70, ConflictPercent: 20, RandomPercent: 10,
		TargetSize: 50, MinSize: 30, MaxSize: 100,
	}
	batches, err := service.StratifyAuditRecords(records, cfg, "ITER-001")
	if err != nil {
		t.Fatalf("StratifyAuditRecords() error = %v", err)
	}
	counts := map[string]int{}
	seen := map[string]bool{}
	total := 0
	for _, batch := range batches {
		counts[batch.MixType] += batch.Count
		total += batch.Count
		if batch.Count < cfg.MinSize || batch.Count > cfg.MaxSize {
			t.Fatalf("batch %s count = %d", batch.ID, batch.Count)
		}
		for _, recordID := range batch.RecordIDs {
			if seen[recordID] {
				t.Fatalf("record %s assigned twice", recordID)
			}
			seen[recordID] = true
		}
	}
	if total != len(records) || counts["homogeneous"] != 7000 ||
		counts["conflict"] != 2000 || counts["random"] != 1000 {
		t.Fatalf("counts = %+v total=%d", counts, total)
	}
}

// TestPolicyOptimizerStratifySmallDataset 验证小于最小批次的数据集保持单批。
func TestPolicyOptimizerStratifySmallDataset(t *testing.T) {
	records := []dto.PolicyOptimizerAuditRecordRef{{RecordID: "AR:1", SourceID: "s", Scene: "prompt"}}
	batches, err := service.StratifyAuditRecords(records, service.PolicyOptimizerBatchingConfig{
		HomogeneousPercent: 70, ConflictPercent: 20, RandomPercent: 10,
		TargetSize: 50, MinSize: 30, MaxSize: 100,
	}, "ITER-001")
	if err != nil || len(batches) != 1 || batches[0].Count != 1 {
		t.Fatalf("batches = %+v err=%v", batches, err)
	}
}
