package service_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sendllm/internal/dto"
	"sendllm/internal/service"
)

// TestPolicyOptimizerNormalizeAndSelectDisagreement 验证 Canonical Record 和挖掘选择。
func TestPolicyOptimizerNormalizeAndSelectDisagreement(t *testing.T) {
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
    mapping: mappings/source-1.approved.yaml
    trust:
      gold_label: approved_gold
      model_label: candidate
change_requests: []
`)
	writePolicyOptimizerTestFile(t, filepath.Join(root, "sources", "source.jsonl"), `{"id":"one","scene":"response","prompt":"p","response":"r","gold_label":"safe","model_label":"unsafe","gold_risks":[],"model_risks":["ethnic_discrimination"]}`+"\n")
	pkg, err := service.LoadAuditPackage(root)
	if err != nil {
		t.Fatalf("LoadAuditPackage() error = %v", err)
	}
	source := pkg.Manifest.Sources[0]
	hash, _, err := inspectPolicyOptimizerSourceForTest(pkg.SourcePath[source.SourceID], source)
	if err != nil {
		t.Fatalf("inspect source: %v", err)
	}
	mapping := dto.PolicyOptimizerMapping{
		Version: 1, SourceID: source.SourceID, SourceSHA256: hash, Format: source.Format,
		Status: "approved", ApprovedBy: "reviewer-1", ApprovedAt: timeNowForTest(),
		RecordSelector: "each_record",
		Fields: map[string]string{
			"sample_id": "/id", "scene": "/scene", "prompt": "/prompt",
			"response": "/response", "source_task": "/id",
		},
		Judgments: []dto.PolicyOptimizerMappingJudgment{
			{ActorType: "gold", ActorID: "gold-v1", Authority: "approved_gold_evidence", Label: "/gold_label", RiskTypes: "/gold_risks"},
			{ActorType: "model", ActorID: "model-v1", Authority: "automated_proposal", Label: "/model_label", RiskTypes: "/model_risks"},
		},
		Comparison: dto.PolicyOptimizerMappingComparison{Left: 0, Right: 1, Type: "label_mismatch"},
	}
	output := filepath.Join(root, "normalized", "source-1.jsonl")
	stats, err := service.NormalizeAuditSource(context.Background(), service.PolicyOptimizerNormalizeConfig{
		Package: pkg, Source: source, Mapping: mapping, OutputPath: output,
	})
	if err != nil {
		t.Fatalf("NormalizeAuditSource() error = %v", err)
	}
	if stats.RecordCount != 1 || len(stats.SHA256) != 64 {
		t.Fatalf("stats = %+v", stats)
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read normalized output: %v", err)
	}
	record := dto.PolicyOptimizerAuditRecord{}
	if err := json.Unmarshal(bytes.TrimSpace(raw), &record); err != nil {
		t.Fatalf("decode normalized record: %v", err)
	}
	selected, err := service.MineDisagreements([]dto.PolicyOptimizerAuditRecord{record}, service.PolicyOptimizerSelectionPolicy{})
	if err != nil || len(selected) != 1 {
		t.Fatalf("MineDisagreements() = (%v,%v)", selected, err)
	}
}

// TestPolicyOptimizerImportQualityEvents 验证 quality event JSONL 的无 payload 契约。
func TestPolicyOptimizerImportQualityEvents(t *testing.T) {
	input := strings.NewReader(`{"trace_id":"t1","scene":"response","policy_version":"p04b-v1.0","policy_hash":"` + strings.Repeat("a", 64) + `","terminal_state":"quarantined","primary_category":"","disagreement_type":"","quarantine_reason":"irreducible_uncertainty","error_pattern_id":""}` + "\n")
	events, err := service.ImportQualityEvents(input)
	if err != nil {
		t.Fatalf("ImportQualityEvents() error = %v", err)
	}
	if len(events) != 1 || events[0].TraceID != "t1" {
		t.Fatalf("events = %+v", events)
	}
}

// inspectPolicyOptimizerSourceForTest 计算测试源哈希。
func inspectPolicyOptimizerSourceForTest(path string, source dto.PolicyOptimizerSourceManifest) (string, int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), bytes.Count(raw, []byte("\n")), nil
}

// timeNowForTest 返回固定测试时间。
func timeNowForTest() time.Time {
	return time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
}
