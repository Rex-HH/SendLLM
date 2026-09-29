package service

import (
	"bufio"
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

// TestSafetyReviewExportWritesAtomicArtifacts 验证导出顺序、投影、分区和脱敏。
func TestSafetyReviewExportWritesAtomicArtifacts(t *testing.T) {
	store, taskID, _ := newSafetyReviewExportStore(t)
	policy := &SafetyReviewPolicy{ReleaseVersion: "p04b-v1.0", AggregateHash: "policy-hash"}
	directory := t.TempDir()
	paths := newSafetyReviewExportPaths(directory)
	for _, path := range paths.all() {
		if err := os.WriteFile(path, []byte("stale\n"), 0o600); err != nil {
			t.Fatalf("write stale output: %v", err)
		}
	}

	exporter, err := NewSafetyReviewExporter(SafetyReviewExporterConfig{
		TaskID: taskID, Store: store, Policy: policy,
		Clean: paths.clean, Quarantine: paths.quarantine, Audit: paths.audit,
		QualityEvents: paths.qualityEvents, Report: paths.report, RunStatus: paths.runStatus,
		Now: func() time.Time {
			return time.Date(2026, time.September, 10, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("NewSafetyReviewExporter() error = %v", err)
	}
	stats, err := exporter.Export(context.Background(), taskID)
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if stats.Clean != 1 || stats.Quarantine != 1 || stats.Audit != 2 ||
		stats.QualityEvents != 2 || !stats.Report || !stats.RunStatus {
		t.Fatalf("Export() stats = %+v", stats)
	}

	clean := readSafetyReviewExportJSONL(t, paths.clean)
	if len(clean) != 1 || clean[0]["trace_id"] != "one" {
		t.Fatalf("clean rows = %#v", clean)
	}
	custom, _ := clean[0]["custom"].(map[string]any)
	if custom["kept"] != float64(7) {
		t.Fatalf("unknown fields were not preserved: %#v", clean[0])
	}
	annotation, _ := clean[0]["annotation"].(map[string]any)
	wantAnnotation := map[string]any{
		"method": "auto", "label": "unsafe", "is_attack": true,
		"attack_method": "jailbreak", "attack_domain": "ethnic_discrimination",
		"risk_type":      "ethnic_discrimination",
		"attack_methods": []any{"jailbreak"}, "attack_domains": []any{"ethnic_discrimination"},
		"case_type": "variant",
	}
	for key, want := range wantAnnotation {
		if fmt.Sprint(annotation[key]) != fmt.Sprint(want) {
			t.Fatalf("annotation[%q] = %#v, want %#v", key, annotation[key], want)
		}
	}

	quarantine := readSafetyReviewExportJSONL(t, paths.quarantine)
	if len(quarantine) != 1 || quarantine[0]["trace_id"] != "two" {
		t.Fatalf("quarantine rows = %#v", quarantine)
	}
	quarantineAnnotation, _ := quarantine[0]["annotation"].(map[string]any)
	if quarantineAnnotation["method"] != "manual_required" ||
		quarantineAnnotation["quarantine_reason"] != "irreducible_uncertainty" {
		t.Fatalf("quarantine annotation = %#v", quarantineAnnotation)
	}
	if fmt.Sprint(quarantineAnnotation["attack_methods"]) != "[]" ||
		fmt.Sprint(quarantineAnnotation["attack_domains"]) != "[]" {
		t.Fatalf("quarantine arrays = %#v", quarantineAnnotation)
	}

	audit := readSafetyReviewExportJSONL(t, paths.audit)
	quality := readSafetyReviewExportJSONL(t, paths.qualityEvents)
	if len(audit) != 2 || len(quality) != 2 {
		t.Fatalf("audit/quality counts = %d/%d, want 2/2", len(audit), len(quality))
	}
	assertSafetyReviewSanitized(t, audit, quality, paths.report, paths.runStatus)

	report := readSafetyReviewExportJSON(t, paths.report)
	if report["task_id"] != taskID || report["acceptance_state"] != "unvalidated" ||
		report["total"] != float64(3) ||
		report["clean"] != float64(1) || report["quarantine"] != float64(1) {
		t.Fatalf("report = %#v", report)
	}
	runStatus := readSafetyReviewExportJSON(t, paths.runStatus)
	if runStatus["task_id"] != taskID || runStatus["status"] != "running" ||
		runStatus["acceptance_state"] != "unvalidated" {
		t.Fatalf("run status = %#v", runStatus)
	}
	assertNoSafetyReviewTemporaryFiles(t, directory)
}

// TestSafetyReviewExportFailureInjection 验证写、同步、重命名和 chmod 失败不留下半截文件。
func TestSafetyReviewExportFailureInjection(t *testing.T) {
	tests := []struct {
		name   string
		failAt string
	}{
		{name: "partial write", failAt: "write"},
		{name: "sync", failAt: "sync"},
		{name: "rename", failAt: "rename"},
		{name: "chmod", failAt: "chmod"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, taskID, _ := newSafetyReviewExportStore(t)
			directory := t.TempDir()
			paths := newSafetyReviewExportPaths(directory)
			exporter, err := newSafetyReviewExporter(SafetyReviewExporterConfig{
				TaskID: taskID, Store: store,
				Policy: &SafetyReviewPolicy{ReleaseVersion: "v1", AggregateHash: "hash"},
				Clean:  paths.clean, Quarantine: paths.quarantine, Audit: paths.audit,
				QualityEvents: paths.qualityEvents, Report: paths.report,
				RunStatus: paths.runStatus, Now: time.Now,
			}, safetyReviewFileOps{
				createTemp: func(dir, pattern string) (safetyReviewAtomicFile, error) {
					file, createErr := os.CreateTemp(dir, pattern)
					if createErr != nil {
						return nil, createErr
					}
					return &failingSafetyReviewAtomicFile{file: file, failAt: test.failAt}, nil
				},
				rename: func(string, string) error {
					if test.failAt == "rename" {
						return errors.New("synthetic rename failure")
					}
					return nil
				},
				chmod: func(string, os.FileMode) error {
					if test.failAt == "chmod" {
						return errors.New("synthetic chmod failure")
					}
					return nil
				},
				remove: os.Remove,
			})
			if err != nil {
				t.Fatalf("newSafetyReviewExporter() error = %v", err)
			}
			if _, err := exporter.Export(context.Background(), taskID); err == nil {
				t.Fatal("Export() returned nil error under injected failure")
			}
			for _, path := range paths.all() {
				if test.failAt == "chmod" {
					continue
				}
				if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("Lstat(%q) error = %v, want not exist", path, err)
				}
			}
			assertNoSafetyReviewTemporaryFiles(t, directory)
		})
	}
}

// safetyReviewExportPaths 保存六个导出目标路径。
type safetyReviewExportPaths struct {
	clean         string
	quarantine    string
	audit         string
	qualityEvents string
	report        string
	runStatus     string
}

// newSafetyReviewExportPaths 构造导出路径集合。
func newSafetyReviewExportPaths(directory string) safetyReviewExportPaths {
	return safetyReviewExportPaths{
		clean:         filepath.Join(directory, "clean.jsonl"),
		quarantine:    filepath.Join(directory, "quarantine.jsonl"),
		audit:         filepath.Join(directory, "audit.jsonl"),
		qualityEvents: filepath.Join(directory, "quality-events.jsonl"),
		report:        filepath.Join(directory, "report.json"),
		runStatus:     filepath.Join(directory, "run-status.json"),
	}
}

// all 返回全部导出路径。
func (p safetyReviewExportPaths) all() []string {
	return []string{
		p.clean, p.quarantine, p.audit, p.qualityEvents, p.report, p.runStatus,
	}
}

// newSafetyReviewExportStore 构造包含一条 Unsafe 和一条 Quarantine 的状态库。
func newSafetyReviewExportStore(t *testing.T) (*dao.SafetyReviewStore, string, string) {
	t.Helper()
	directory := t.TempDir()
	store, err := dao.OpenSafetyReview(context.Background(), filepath.Join(directory, "state.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	taskID := "export-test"
	if err := store.EnsureTask(context.Background(), dao.SafetyReviewTask{
		ID: taskID, SemanticFingerprint: "fingerprint", Scene: "response",
		SnapshotDir: filepath.Join(directory, "snapshots"),
	}); err != nil {
		t.Fatalf("ensure task: %v", err)
	}
	input := strings.Join([]string{
		`{"trace_id":"one","prompt":"PROMPT_CANARY","response":"RESPONSE_CANARY",` +
			`"custom":{"kept":7},"annotation":{"method":"manual"}}`,
		`{"trace_id":"two","prompt":"PROMPT_CANARY_TWO","response":"RESPONSE_CANARY_TWO",` +
			`"custom":{"kept":8}}`,
		`{"trace_id":"three","prompt":"PROMPT_CANARY_THREE","response":"RESPONSE_CANARY_THREE",` +
			`"custom":{"kept":9}}`,
	}, "\n") + "\n"
	if _, err := store.ImportJSONL(context.Background(), taskID, strings.NewReader(input)); err != nil {
		t.Fatalf("import rows: %v", err)
	}
	completeSafetyReviewExportItem(t, store, taskID, "one", dao.SafetyReviewDecisionRecord{
		FinalState: "resolved_unsafe", Label: "unsafe", IsAttack: true,
		AttackMethods: []byte(`["jailbreak"]`), AttackDomains: []byte(`["ethnic_discrimination"]`),
		PrimaryAttackMethod: "jailbreak", PrimaryAttackDomain: "ethnic_discrimination",
		PrimaryRiskType: "ethnic_discrimination", CaseType: "variant",
		Evidence:      []byte(`[{"source":"prompt","kind":"operational_attack_wording","span":"EVIDENCE_CANARY"}]`),
		DecisionRules: []byte(`["jailbreak"]`), Rationale: "RATIONALE_CANARY",
		DecidedAt: time.Now(),
	})
	completeSafetyReviewExportItem(t, store, taskID, "two", dao.SafetyReviewDecisionRecord{
		FinalState: "quarantined", Label: "", IsAttack: false,
		AttackMethods: []byte("[]"), AttackDomains: []byte("[]"),
		CaseType: "", Evidence: []byte("[]"), DecisionRules: []byte(`["P04B-DECISION-001"]`),
		Rationale: "RATIONALE_CANARY", QuarantineReason: "irreducible_uncertainty",
		DecidedAt: time.Now(),
	})
	return store, taskID, filepath.Join(directory, "state.db")
}

// completeSafetyReviewExportItem 通过 Router 和 Arbiter 阶段写入一条最终决策。
func completeSafetyReviewExportItem(
	t *testing.T,
	store *dao.SafetyReviewStore,
	taskID string,
	traceID string,
	decision dao.SafetyReviewDecisionRecord,
) {
	t.Helper()
	router := claimSafetyReviewExportStage(t, store, taskID, "router")
	if err := store.CompleteStage(context.Background(), dao.SafetyReviewStageCompletion{
		TaskID: taskID, TraceID: traceID, StageKey: router.StageKey,
		Outcome: dao.SafetyReviewStageSucceeded,
		ResultJSON: []byte(`{"features":[],"attack_method_candidates":[],` +
			`"attack_domain_candidates":[],"coverage_complete":true}`),
		DownstreamStages: []dao.SafetyReviewStageSpec{{StageKey: "arbiter", Role: "arbiter"}},
		ItemState:        "pending_arbiter",
		Attempt: dao.SafetyReviewAttempt{
			AttemptKind: "classification", ModelProfile: "profile", ModelFamily: "family",
			APIKeyEnv: "TEST_API_ENV", StartedAt: time.Now().Add(-time.Second),
			FinishedAt: time.Now(), RawResponse: []byte("RAW_OUTPUT_CANARY API_KEY_CANARY"),
		},
	}); err != nil {
		t.Fatalf("complete router: %v", err)
	}
	arbiter := claimSafetyReviewExportStage(t, store, taskID, "arbiter")
	if err := store.CompleteStage(context.Background(), dao.SafetyReviewStageCompletion{
		TaskID: taskID, TraceID: traceID, StageKey: arbiter.StageKey,
		Outcome: dao.SafetyReviewStageSucceeded, ResultJSON: []byte(`{"verdict":"resolved"}`),
		ItemState: decision.FinalState, Decision: &decision,
		Attempt: dao.SafetyReviewAttempt{
			AttemptKind: "classification", ModelProfile: "profile", ModelFamily: "family",
			APIKeyEnv: "TEST_API_ENV", StartedAt: time.Now().Add(-time.Second),
			FinishedAt: time.Now(), ErrorCategory: "server", ErrorSummary: "safe summary",
			RawResponse: []byte("RAW_OUTPUT_CANARY API_KEY_CANARY"),
		},
	}); err != nil {
		t.Fatalf("complete arbiter: %v", err)
	}
}

// claimSafetyReviewExportStage 领取指定角色阶段。
func claimSafetyReviewExportStage(
	t *testing.T,
	store *dao.SafetyReviewStore,
	taskID string,
	role string,
) dao.SafetyReviewStageWork {
	t.Helper()
	work, ok, err := store.ClaimStage(context.Background(), dao.SafetyReviewClaim{
		TaskID: taskID, Role: role, Now: time.Now(),
		ModelProfile: "profile", ModelFamily: "family",
	})
	if err != nil || !ok {
		t.Fatalf("claim %s = (%+v, %v, %v)", role, work, ok, err)
	}
	return work
}

// failingSafetyReviewAtomicFile 按指定阶段失败。
type failingSafetyReviewAtomicFile struct {
	file   *os.File
	failAt string
}

// Write 在指定阶段返回失败。
func (f *failingSafetyReviewAtomicFile) Write(p []byte) (int, error) {
	if f.failAt == "write" {
		return 0, errors.New("synthetic write failure")
	}
	return f.file.Write(p)
}

// Sync 在指定阶段返回失败。
func (f *failingSafetyReviewAtomicFile) Sync() error {
	if f.failAt == "sync" {
		return errors.New("synthetic sync failure")
	}
	return f.file.Sync()
}

// Close 立即成功。
func (f *failingSafetyReviewAtomicFile) Close() error { return f.file.Close() }

// Name 返回固定测试文件名。
func (f *failingSafetyReviewAtomicFile) Name() string { return f.file.Name() }

// readSafetyReviewExportJSONL 读取 JSONL 为对象切片。
func readSafetyReviewExportJSONL(t *testing.T, path string) []map[string]any {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = file.Close() }()
	var result []map[string]any
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		value := map[string]any{}
		if err := json.Unmarshal(line, &value); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		result = append(result, value)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	return result
}

// readSafetyReviewExportJSON 读取 JSON 对象。
func readSafetyReviewExportJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	value := map[string]any{}
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return value
}

// assertSafetyReviewSanitized 断言非正式导出不包含载荷或凭据。
func assertSafetyReviewSanitized(
	t *testing.T,
	audit []map[string]any,
	quality []map[string]any,
	reportPath string,
	runStatusPath string,
) {
	t.Helper()
	rawReport, _ := os.ReadFile(reportPath)
	rawStatus, _ := os.ReadFile(runStatusPath)
	rawAudit, _ := json.Marshal(audit)
	rawQuality, _ := json.Marshal(quality)
	combined := string(rawAudit) + string(rawQuality) + string(rawReport) + string(rawStatus)
	for _, canary := range []string{
		"PROMPT_CANARY", "RESPONSE_CANARY", "EVIDENCE_CANARY",
		"RAW_OUTPUT_CANARY", "RATIONALE_CANARY", "TEST_API_ENV", "API_KEY_CANARY",
	} {
		if strings.Contains(combined, canary) {
			t.Fatalf("sanitized output contains canary %q", canary)
		}
	}
	allowedQuality := map[string]bool{
		"trace_id": true, "scene": true, "policy_version": true, "policy_hash": true,
		"terminal_state": true, "categories": true, "stages": true,
		"disagreement_type": true, "fallback_used": true, "independence_degraded": true,
		"quarantine_reason": true, "error_pattern_ids": true,
	}
	for _, event := range quality {
		for key := range event {
			if !allowedQuality[key] {
				t.Fatalf("quality event contains disallowed field %q", key)
			}
		}
	}
}

// assertNoSafetyReviewTemporaryFiles 断言导出目录没有残留临时文件。
func assertNoSafetyReviewTemporaryFiles(t *testing.T, directory string) {
	t.Helper()
	for _, pattern := range []string{".safety-review-*", "*.tmp"} {
		matches, err := filepath.Glob(filepath.Join(directory, pattern))
		if err != nil {
			t.Fatalf("glob temporary files: %v", err)
		}
		if len(matches) != 0 {
			t.Fatalf("temporary files remain: %v", matches)
		}
	}
}
