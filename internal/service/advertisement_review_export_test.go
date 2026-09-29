package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sendllm/internal/dao"
)

func advertisementSourceWithTraceID(traceID, label, scenario string) string {
	return fmt.Sprintf(`{"trace_id":%q,"scene":"prompt","label":%q,"prompt":"广告内容-%s-%s","response":"旧回复","explanation":"旧解释","source":"source","quality_score":0.5,"extended_info":{"attack_scenario":%q}}`, traceID, label, label, scenario, scenario)
}

func advertisementSource(label, scenario string) string {
	return advertisementSourceWithTraceID("ad-"+label+"-"+scenario, label, scenario)
}

func storeWithAdvertisementRows(t *testing.T, rows []string) *dao.Store {
	t.Helper()
	store := openAdvertisementReviewStore(t)
	if err := store.EnsureTask(context.Background(), dao.Task{ID: "task", SemanticHash: "hash"}); err != nil {
		t.Fatalf("EnsureTask() error = %v", err)
	}
	input := "[" + strings.Join(rows, ",") + "]"
	if _, err := ImportAdvertisementReview(context.Background(), store, "task", strings.NewReader(input)); err != nil {
		t.Fatalf("ImportAdvertisementReview() error = %v", err)
	}
	return store
}

func finishAdvertisementRow(t *testing.T, store *dao.Store, traceID string, decision any, failedCategory string) {
	t.Helper()
	if _, err := store.Claim(context.Background(), "task", 10, time.Now()); err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	attempt := dao.Attempt{Phase: "advertisement_review_batch", RequestNumber: 1}
	if failedCategory != "" {
		if err := store.MarkFailed(context.Background(), "task", traceID, attempt, failedCategory, "safe failure"); err != nil {
			t.Fatalf("MarkFailed() error = %v", err)
		}
		return
	}
	annotation, err := json.Marshal(decision)
	if err != nil {
		t.Fatalf("marshal decision: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(annotation, &fields); err != nil {
		t.Fatalf("unmarshal decision for attempt metadata: %v", err)
	}
	fields["a"] = 1
	annotation, err = json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal decision with attempt metadata: %v", err)
	}
	if err := store.MarkSucceeded(context.Background(), "task", traceID, attempt, annotation); err != nil {
		t.Fatalf("MarkSucceeded() error = %v", err)
	}
}

func readJSONL(t *testing.T, path string) []map[string]any {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if len(contents) == 0 {
		return nil
	}
	var rows []map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(contents)))
	for decoder.More() {
		var row map[string]any
		if err := decoder.Decode(&row); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		rows = append(rows, row)
	}
	return rows
}

func TestAdvertisementReviewPartitionDerivesStableIssues(t *testing.T) {
	tests := []struct {
		name     string
		label    string
		decision advertisementReviewDecision
		want     []string
	}{
		{name: "agreement clean", label: "safe", decision: advertisementReviewDecision{Index: 0, Label: 1}, want: nil},
		{name: "safe to unsafe", label: "safe", decision: advertisementReviewDecision{Index: 0, Label: 2}, want: []string{"label_error"}},
		{name: "unsafe to safe", label: "unsafe", decision: advertisementReviewDecision{Index: 0, Label: 1}, want: []string{"label_error"}},
		{name: "overlap", label: "unsafe", decision: advertisementReviewDecision{Index: 0, Label: 2, LegacyRisk: "jailbreak"}, want: []string{"legacy_overlap"}},
		{name: "scenario", label: "unsafe", decision: advertisementReviewDecision{Index: 0, Label: 2, ScenarioSuspect: 1}, want: []string{"scenario_suspect"}},
		{name: "uncertain", label: "safe", decision: advertisementReviewDecision{Index: 0, Label: 0}, want: []string{"uncertain"}},
		{name: "multiple", label: "safe", decision: advertisementReviewDecision{Index: 0, Label: 0, LegacyRisk: "jailbreak", ScenarioSuspect: 1}, want: []string{"uncertain", "legacy_overlap", "scenario_suspect"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, err := parseAdvertisementReviewInput([]byte(advertisementSource(tt.label, "wechat_contact")))
			if err != nil {
				t.Fatalf("parse source: %v", err)
			}
			got, risk := advertisementReviewIssues(source, tt.decision)
			if len(got) != len(tt.want) || !reflectDeepEqual(got, tt.want) || risk != tt.decision.LegacyRisk {
				t.Fatalf("issues = %v, risk = %q", got, risk)
			}
		})
	}
}

func TestAdvertisementReviewExportWritesUnchangedPartitionsAndSafeArtifacts(t *testing.T) {
	rows := []string{
		advertisementSourceWithTraceID("ad-safe-a", "safe", ""),
		advertisementSourceWithTraceID("ad-safe-b", "safe", ""),
		advertisementSource("unsafe", "wechat_contact"),
		advertisementSource("unsafe", "game_trading"),
	}
	store := storeWithAdvertisementRows(t, rows)
	finishAdvertisementRow(t, store, "ad-safe-a", advertisementReviewDecision{Index: 0, Label: 1}, "")
	finishAdvertisementRow(t, store, "ad-safe-b", advertisementReviewDecision{Index: 0, Label: 2}, "")
	finishAdvertisementRow(t, store, "ad-unsafe-wechat_contact", advertisementReviewDecision{Index: 0, Label: 2, LegacyRisk: "jailbreak", ScenarioSuspect: 1}, "")
	finishAdvertisementRow(t, store, "ad-unsafe-game_trading", nil, "invalid_result")

	dir := t.TempDir()
	cleanPath := filepath.Join(dir, "clean.original.jsonl")
	stats, err := ExportAdvertisementReview(context.Background(), store, "task", cleanPath)
	if err != nil {
		t.Fatalf("ExportAdvertisementReview() error = %v", err)
	}
	if stats.Clean != 1 || stats.Issues != 3 || stats.Failed != 1 || stats.Attempts != 4 {
		t.Fatalf("stats = %+v", stats)
	}
	clean := readJSONL(t, cleanPath)
	issues := readJSONL(t, filepath.Join(dir, "issues.original.jsonl"))
	manifest := readJSONL(t, filepath.Join(dir, "issues.manifest.jsonl"))
	if len(clean) != 1 || clean[0]["trace_id"] != "ad-safe-a" {
		t.Fatalf("clean = %+v", clean)
	}
	if len(issues) != 3 {
		t.Fatalf("issues = %+v", issues)
	}
	if len(manifest) != 3 {
		t.Fatalf("manifest = %+v", manifest)
	}
	sourceByID := make(map[string]map[string]any)
	for _, row := range rows {
		var source map[string]any
		if err := json.Unmarshal([]byte(row), &source); err != nil {
			t.Fatalf("unmarshal source: %v", err)
		}
		sourceByID[source["trace_id"].(string)] = source
	}
	if !reflectDeepEqual(clean[0], sourceByID["ad-safe-a"]) {
		t.Fatal("clean object is not deeply equal to source")
	}
	expectedIssueIDs := []string{"ad-safe-b", "ad-unsafe-wechat_contact", "ad-unsafe-game_trading"}
	for index, row := range issues {
		id := row["trace_id"].(string)
		if id != expectedIssueIDs[index] {
			t.Fatalf("issue order = %v", idsFromRows(issues))
		}
		if !reflectDeepEqual(row, sourceByID[id]) {
			t.Fatalf("issue object %s is not deeply equal to source", id)
		}
	}
	cleanIDs := map[string]bool{"ad-safe-a": true}
	issueIDs := make(map[string]bool, len(issues))
	for _, row := range issues {
		id := row["trace_id"].(string)
		if cleanIDs[id] || issueIDs[id] {
			t.Fatalf("partition IDs are not complete, disjoint, and unique: %v", idsFromRows(issues))
		}
		issueIDs[id] = true
	}
	if len(cleanIDs)+len(issueIDs) != len(rows) {
		t.Fatalf("partition union size = %d", len(cleanIDs)+len(issueIDs))
	}
	if manifest[1]["candidate_legacy_risk"] != "jailbreak" {
		t.Fatalf("manifest legacy risk = %v", manifest[1]["candidate_legacy_risk"])
	}
	manifestIDs := make(map[string]bool, len(manifest))
	for _, row := range manifest {
		if len(row) < 2 || len(row) > 3 {
			t.Fatalf("manifest keys = %v", row)
		}
		manifestIDs[row["trace_id"].(string)] = true
	}
	for _, row := range issues {
		if !manifestIDs[row["trace_id"].(string)] {
			t.Fatalf("issue missing manifest: %v", row["trace_id"])
		}
	}
	reportBytes, err := os.ReadFile(filepath.Join(dir, "review-report.json"))
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	report := string(reportBytes)
	for _, forbidden := range []string{"广告内容", "旧回复", "旧解释", "Authorization", "api_key"} {
		if strings.Contains(report, forbidden) {
			t.Fatalf("report leaks %q", forbidden)
		}
	}
}

func TestAdvertisementReviewExportRejectsNonTerminalRows(t *testing.T) {
	store := storeWithAdvertisementRows(t, []string{advertisementSource("safe", "")})
	dir := t.TempDir()
	if _, err := ExportAdvertisementReview(context.Background(), store, "task", filepath.Join(dir, "clean.original.jsonl")); err == nil {
		t.Fatal("ExportAdvertisementReview() error = nil")
	} else if !errors.Is(err, ErrInvalidResult) {
		t.Fatalf("ExportAdvertisementReview() error = %v", err)
	}
}

func idsFromRows(rows []map[string]any) []string {
	ids := make([]string, len(rows))
	for index, row := range rows {
		ids[index] = row["trace_id"].(string)
	}
	return ids
}
