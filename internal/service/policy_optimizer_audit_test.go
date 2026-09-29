package service_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"sendllm/internal/service"
)

// TestPolicyOptimizerAuditInspectAndMappingApproval 验证 manifest、哈希和 mapping 审批闭环。
func TestPolicyOptimizerAuditInspectAndMappingApproval(t *testing.T) {
	root := t.TempDir()
	writePolicyOptimizerTestFile(t, filepath.Join(root, "manifest.yaml"), `version: 1
package_id: audit-001
objective: [discover_model_bias]
current_policy_version: p04b-v1.0
analysis_permissions: [rule_problem]
sources:
  - source_id: source-1
    type: sft_vs_gold
    path: sources/source.jsonl
    format: jsonl
    selection_reason: model_disagreement
    expected_records: 1
    mapping: mappings/source-1.candidate.yaml
    trust:
      gold_label: approved_gold
      model_label: candidate
change_requests: []
`)
	writePolicyOptimizerTestFile(t, filepath.Join(root, "sources", "source.jsonl"), `{"id":"one"}`+"\n")
	pkg, err := service.LoadAuditPackage(root)
	if err != nil {
		t.Fatalf("LoadAuditPackage() error = %v", err)
	}
	inspection, err := service.InspectAuditPackage(context.Background(), service.AuditInspectConfig{Package: pkg})
	if err != nil {
		t.Fatalf("InspectAuditPackage() error = %v", err)
	}
	if !inspection.NeedsApproval || inspection.Sources[0].ActualCount != 1 ||
		len(inspection.Sources[0].SHA256) != 64 {
		t.Fatalf("inspection = %+v", inspection)
	}

	candidate := filepath.Join(root, "mappings", "source-1.candidate.yaml")
	writePolicyOptimizerTestFile(t, candidate, `version: 1
source_id: source-1
source_sha256: `+inspection.Sources[0].SHA256+`
format: jsonl
status: candidate
approved_by: ""
approved_at: 0001-01-01T00:00:00Z
record_selector: each_record
fields:
  sample_id: /id
judgments:
  - actor_type: gold
    actor_id: gold-v1
    authority: approved_gold_evidence
    label: /gold_label
    risk_types: /risk_types
  - actor_type: model
    actor_id: model-v1
    authority: automated_proposal
    label: /model_label
    risk_types: /model_risk_types
comparison:
  left: 0
  right: 1
  type: label_mismatch
`)
	approved := filepath.Join(root, "mappings", "source-1.approved.yaml")
	if err := service.ApproveAuditMapping(candidate, approved, "reviewer-1", time.Now()); err != nil {
		t.Fatalf("ApproveAuditMapping() error = %v", err)
	}
	if _, err := os.Stat(approved); err != nil {
		t.Fatalf("approved mapping missing: %v", err)
	}
}
