package service_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"sendllm/internal/dao"
	"sendllm/internal/service"
)

func TestExportWritesOrderedMergedSuccessAndSafeFailures(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	ensureTask(t, store, ctx)
	seedExportItems(t, store, ctx,
		dao.Item{
			TaskID:     "task-1",
			TraceID:    "dataset:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			InputIndex: 1,
			RawJSON: []byte(
				`{"id":"dataset:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",` +
					`"messages":[{"role":"user","content":"synthetic prompt one"}],` +
					`"label":{"value":"safe"},"custom":{"rank":1},"annotation":{"method":"manual"}}`,
			),
			Prompt: "synthetic prompt one",
			State:  dao.ItemPending,
		},
		dao.Item{
			TaskID:     "task-1",
			TraceID:    "dataset:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			InputIndex: 2,
			RawJSON: []byte(
				`{"id":"dataset:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",` +
					`"messages":[{"role":"user","content":"synthetic prompt two"}],` +
					`"label":{"value":"unsafe","risk_type":"RT10_对抗性攻击","risk_level":"high"},` +
					`"source_extra":"kept for manual"}`,
			),
			Prompt: "synthetic prompt two",
			State:  dao.ItemPending,
		},
		dao.Item{
			TaskID:     "task-1",
			TraceID:    "dataset:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			InputIndex: 3,
			RawJSON: []byte(
				`{"id":"dataset:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",` +
					`"messages":[{"role":"user","content":"synthetic prompt three"}],` +
					`"label":{"value":"safe"},"custom":"kept"}`,
			),
			Prompt: "synthetic prompt three",
			State:  dao.ItemPending,
		},
	)
	claimExportItems(t, store, ctx, 3)
	markExportSucceeded(t, store, ctx, "dataset:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", 1, 0,
		`{"is_attack":false,"case_type":"typical","explanation":"synthetic safe explanation"}`,
	)
	markExportFailed(t, store, ctx, "dataset:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		2, 1, "content_rejected", "provider rejected content")
	markExportSucceeded(t, store, ctx, "dataset:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 1, 0,
		`{"is_attack":true,"case_type":"typical","explanation":"synthetic unsafe explanation",`+
			`"extended_info":{"risk_type":"test-risk","risk_level":"high"}}`,
	)

	directory := t.TempDir()
	outputPath := filepath.Join(directory, "result.jsonl")
	stats, err := service.Export(ctx, store, "task-1", outputPath)
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if stats.Succeeded != 2 || stats.Failed != 1 {
		t.Errorf("Export() stats = %+v, want Succeeded=2 Failed=1", stats)
	}

	succeeded := readJSONLObjects(t, outputPath)
	if len(succeeded) != 2 {
		t.Fatalf("success records = %d, want 2", len(succeeded))
	}
	if got := []any{succeeded[0]["id"], succeeded[1]["id"]}; !reflect.DeepEqual(got, []any{
		"dataset:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"dataset:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
	}) {
		t.Errorf("success id order = %v, want compact IDs in input order", got)
	}
	if !reflect.DeepEqual(succeeded[0]["custom"], map[string]any{"rank": float64(1)}) {
		t.Errorf("preserved custom field = %#v, want nested rank", succeeded[0]["custom"])
	}
	if succeeded[0]["is_attack"] != nil || succeeded[0]["case_type"] != nil ||
		succeeded[0]["explanation"] != nil || succeeded[0]["extended_info"] != nil {
		t.Errorf("generated fields leaked to top level: %#v", succeeded[0])
	}
	if !reflect.DeepEqual(succeeded[0]["label"], map[string]any{"value": "safe"}) {
		t.Errorf("label = %#v, want original compact label preserved", succeeded[0]["label"])
	}
	annotation, ok := succeeded[0]["annotation"].(map[string]any)
	if !ok || annotation["method"] != "auto" || annotation["is_attack"] != true ||
		annotation["case_type"] != "typical" || annotation["explanation"] != "synthetic unsafe explanation" {
		t.Errorf("annotation = %#v, want nested automatic model result", succeeded[0]["annotation"])
	}
	extended, ok := annotation["extended_info"].(map[string]any)
	if !ok || extended["risk_type"] != "test-risk" {
		t.Errorf("annotation.extended_info = %#v, want generated object", annotation["extended_info"])
	}

	failed := readJSONLObjects(t, filepath.Join(directory, "result.failed.jsonl"))
	if len(failed) != 1 {
		t.Fatalf("failed records = %d, want 1", len(failed))
	}
	wantAnnotation := map[string]any{
		"method":         "manual_required",
		"is_attack":      nil,
		"case_type":      "",
		"explanation":    "",
		"extended_info":  map[string]any{},
		"error_category": "content_rejected",
		"error_summary":  "provider rejected content",
		"attempts":       float64(3),
	}
	for key, want := range map[string]any{
		"id":           "dataset:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"source_extra": "kept for manual",
		"label": map[string]any{
			"value":      "unsafe",
			"risk_type":  "RT10_对抗性攻击",
			"risk_level": "high",
		},
	} {
		if got := failed[0][key]; !reflect.DeepEqual(got, want) {
			t.Errorf("failed[%q] = %#v, want %#v", key, got, want)
		}
	}
	if !reflect.DeepEqual(failed[0]["annotation"], wantAnnotation) {
		t.Errorf("failed annotation = %#v, want %#v", failed[0]["annotation"], wantAnnotation)
	}
}

func TestExportAtomicallyReplacesOrPreservesExistingFiles(t *testing.T) {
	t.Run("replaces complete files", func(t *testing.T) {
		store := openTestStore(t)
		ctx := context.Background()
		ensureTask(t, store, ctx)
		seedExportItems(t, store, ctx, dao.Item{
			TaskID:     "task-1",
			TraceID:    "success",
			InputIndex: 1,
			RawJSON:    []byte(`{"trace_id":"success","prompt":"synthetic prompt"}`),
			Prompt:     "synthetic prompt",
			State:      dao.ItemPending,
		})
		claimExportItems(t, store, ctx, 1)
		markExportSucceeded(t, store, ctx, "success", 1, 0,
			`{"is_attack":false,"case_type":"typical","explanation":"synthetic safe explanation"}`,
		)

		directory := t.TempDir()
		outputPath := filepath.Join(directory, "result")
		failedPath := outputPath + ".failed.jsonl"
		writeOldExportFiles(t, outputPath, failedPath)

		if _, err := service.Export(ctx, store, "task-1", outputPath); err != nil {
			t.Fatalf("Export() error = %v", err)
		}
		if got := readFile(t, outputPath); got == "old success\n" {
			t.Error("success file was not replaced")
		}
		if got := readFile(t, failedPath); got != "" {
			t.Errorf("failed file = %q, want empty replacement", got)
		}
		assertNoExportTemps(t, directory)
	})

	t.Run("preserves old files when staging fails", func(t *testing.T) {
		store := openTestStore(t)
		ctx := context.Background()
		ensureTask(t, store, ctx)
		seedExportItems(t, store, ctx, dao.Item{
			TaskID:     "task-1",
			TraceID:    "broken",
			InputIndex: 1,
			RawJSON:    []byte(`{"trace_id":"broken","prompt":"synthetic prompt"}`),
			Prompt:     "synthetic prompt",
			State:      dao.ItemPending,
		})
		claimExportItems(t, store, ctx, 1)
		markExportSucceeded(t, store, ctx, "broken", 1, 0, `not-json`)

		directory := t.TempDir()
		outputPath := filepath.Join(directory, "result.jsonl")
		failedPath := filepath.Join(directory, "result.failed.jsonl")
		writeOldExportFiles(t, outputPath, failedPath)

		if _, err := service.Export(ctx, store, "task-1", outputPath); err == nil {
			t.Fatal("Export() error = nil, want annotation decode error")
		}
		if got := readFile(t, outputPath); got != "old success\n" {
			t.Errorf("success file = %q, want old content", got)
		}
		if got := readFile(t, failedPath); got != "old failure\n" {
			t.Errorf("failed file = %q, want old content", got)
		}
		assertNoExportTemps(t, directory)
	})

	t.Run("restores old files when second publish fails", func(t *testing.T) {
		store, ctx, directory, outputPath, failedPath := prepareExportPublish(t)
		writeOldExportFiles(t, outputPath, failedPath)
		publishErr := errors.New("synthetic second publish failure")
		rename := failSecondExportPublish(publishErr)

		_, err := service.ExportWithFileOpsForTest(ctx, store, "task-1", outputPath, service.ExportFileOpsForTest{
			Rename: rename,
		})
		if !errors.Is(err, publishErr) {
			t.Fatalf("Export() error = %v, want %v", err, publishErr)
		}
		if got := readFile(t, outputPath); got != "old success\n" {
			t.Errorf("success file = %q, want old content", got)
		}
		if got := readFile(t, failedPath); got != "old failure\n" {
			t.Errorf("failed file = %q, want old content", got)
		}
		assertNoExportTemps(t, directory)
	})

	t.Run("removes new files when old targets did not exist", func(t *testing.T) {
		store, ctx, directory, outputPath, failedPath := prepareExportPublish(t)
		publishErr := errors.New("synthetic second publish failure")
		rename := failSecondExportPublish(publishErr)

		_, err := service.ExportWithFileOpsForTest(ctx, store, "task-1", outputPath, service.ExportFileOpsForTest{
			Rename: rename,
		})
		if !errors.Is(err, publishErr) {
			t.Fatalf("Export() error = %v, want %v", err, publishErr)
		}
		for _, path := range []string{outputPath, failedPath} {
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("Lstat(%q) error = %v, want not exist", path, err)
			}
		}
		assertNoExportTemps(t, directory)
	})

	t.Run("joins publish and restore failures", func(t *testing.T) {
		store, ctx, _, outputPath, failedPath := prepareExportPublish(t)
		writeOldExportFiles(t, outputPath, failedPath)
		publishErr := errors.New("synthetic second publish failure")
		restoreErr := errors.New("synthetic restore failure")
		publishRename := failSecondExportPublish(publishErr)
		rename := func(oldPath, newPath string) error {
			if strings.HasPrefix(filepath.Base(filepath.Dir(oldPath)), ".sendllm-export-backup-") &&
				newPath == outputPath {
				return restoreErr
			}
			return publishRename(oldPath, newPath)
		}

		_, err := service.ExportWithFileOpsForTest(ctx, store, "task-1", outputPath, service.ExportFileOpsForTest{
			Rename: rename,
		})
		if !errors.Is(err, publishErr) || !errors.Is(err, restoreErr) {
			t.Fatalf("Export() error = %v, want joined publish and restore errors", err)
		}
		if _, err := os.Lstat(outputPath); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("Lstat(success) error = %v, want not exist after failed restore", err)
		}
		if got := readFile(t, failedPath); got != "old failure\n" {
			t.Errorf("failed file = %q, want old content at formal path", got)
		}
		backupDirs, err := filepath.Glob(filepath.Join(filepath.Dir(outputPath), ".sendllm-export-backup-*"))
		if err != nil {
			t.Fatalf("Glob(export backups) error = %v", err)
		}
		if len(backupDirs) != 1 {
			t.Fatalf("export backup directories = %v, want one", backupDirs)
		}
		if got := readFile(t, filepath.Join(backupDirs[0], "succeeded.jsonl")); got != "old success\n" {
			t.Errorf("success backup = %q, want old content", got)
		}
		if _, err := os.Lstat(filepath.Join(backupDirs[0], "failed.jsonl")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("Lstat(failed backup) error = %v, want old failure only at formal path", err)
		}
	})

	t.Run("keeps formal paths while preparing backups", func(t *testing.T) {
		store, ctx, _, outputPath, failedPath := prepareExportPublish(t)
		writeOldExportFiles(t, outputPath, failedPath)
		publishErr := errors.New("stop at first publish")
		rename := func(oldPath, newPath string) error {
			if strings.HasPrefix(filepath.Base(oldPath), ".sendllm-export-") {
				if got := readFile(t, outputPath); got != "old success\n" {
					t.Errorf("success file before publish = %q, want old content", got)
				}
				if got := readFile(t, failedPath); got != "old failure\n" {
					t.Errorf("failed file before publish = %q, want old content", got)
				}
				return publishErr
			}
			return os.Rename(oldPath, newPath)
		}

		_, err := service.ExportWithFileOpsForTest(ctx, store, "task-1", outputPath, service.ExportFileOpsForTest{
			Rename: rename,
		})
		if !errors.Is(err, publishErr) {
			t.Fatalf("Export() error = %v, want %v", err, publishErr)
		}
	})

	t.Run("preserves old files when backup creation fails", func(t *testing.T) {
		store, ctx, directory, outputPath, failedPath := prepareExportPublish(t)
		writeOldExportFiles(t, outputPath, failedPath)
		backupErr := errors.New("synthetic backup failure")

		_, err := service.ExportWithFileOpsForTest(ctx, store, "task-1", outputPath, service.ExportFileOpsForTest{
			Link: func(oldPath, newPath string) error {
				if oldPath == outputPath {
					return backupErr
				}
				return os.Link(oldPath, newPath)
			},
			Open: func(path string) (*os.File, error) {
				if path == outputPath {
					return nil, backupErr
				}
				return os.Open(path)
			},
		})
		if !errors.Is(err, backupErr) {
			t.Fatalf("Export() error = %v, want %v", err, backupErr)
		}
		if got := readFile(t, outputPath); got != "old success\n" {
			t.Errorf("success file = %q, want old content", got)
		}
		if got := readFile(t, failedPath); got != "old failure\n" {
			t.Errorf("failed file = %q, want old content", got)
		}
		assertNoExportTemps(t, directory)
	})

	t.Run("copies backups when hard links are unavailable", func(t *testing.T) {
		store, ctx, directory, outputPath, failedPath := prepareExportPublish(t)
		writeOldExportFiles(t, outputPath, failedPath)

		if _, err := service.ExportWithFileOpsForTest(ctx, store, "task-1", outputPath, service.ExportFileOpsForTest{
			Link: func(string, string) error {
				return errors.New("synthetic unsupported hard link")
			},
		}); err != nil {
			t.Fatalf("Export() error = %v", err)
		}
		if got := readFile(t, outputPath); got == "old success\n" {
			t.Error("success file was not replaced")
		}
		if got := readFile(t, failedPath); got != "" {
			t.Errorf("failed file = %q, want empty replacement", got)
		}
		assertNoExportTemps(t, directory)
	})

	t.Run("restores copied backups when second publish fails", func(t *testing.T) {
		store, ctx, directory, outputPath, failedPath := prepareExportPublish(t)
		writeOldExportFiles(t, outputPath, failedPath)
		publishErr := errors.New("synthetic second publish failure")

		_, err := service.ExportWithFileOpsForTest(ctx, store, "task-1", outputPath, service.ExportFileOpsForTest{
			Rename: failSecondExportPublish(publishErr),
			Link: func(string, string) error {
				return errors.New("synthetic unsupported hard link")
			},
		})
		if !errors.Is(err, publishErr) {
			t.Fatalf("Export() error = %v, want %v", err, publishErr)
		}
		if got := readFile(t, outputPath); got != "old success\n" {
			t.Errorf("success file = %q, want old copied content", got)
		}
		if got := readFile(t, failedPath); got != "old failure\n" {
			t.Errorf("failed file = %q, want old copied content", got)
		}
		assertNoExportTemps(t, directory)
	})

	t.Run("keeps committed files when backup cleanup partially fails", func(t *testing.T) {
		store, ctx, _, outputPath, failedPath := prepareExportPublish(t)
		writeOldExportFiles(t, outputPath, failedPath)
		cleanupErr := errors.New("synthetic partial cleanup failure")
		renameCount := 0

		stats, err := service.ExportWithFileOpsForTest(ctx, store, "task-1", outputPath, service.ExportFileOpsForTest{
			Rename: func(oldPath, newPath string) error {
				renameCount++
				return os.Rename(oldPath, newPath)
			},
			RemoveAll: func(backupDir string) error {
				if err := os.Remove(filepath.Join(backupDir, "succeeded.jsonl")); err != nil {
					t.Fatalf("remove first backup: %v", err)
				}
				return cleanupErr
			},
		})
		if !errors.Is(err, cleanupErr) {
			t.Fatalf("Export() error = %v, want %v", err, cleanupErr)
		}
		if stats != (service.ExportStats{}) {
			t.Errorf("Export() stats = %+v, want zero stats on cleanup error", stats)
		}
		if got := readFile(t, outputPath); got == "old success\n" {
			t.Error("success file rolled back after commit")
		}
		if got := readFile(t, failedPath); got != "" {
			t.Errorf("failed file = %q, want committed empty snapshot", got)
		}
		if renameCount != 2 {
			t.Errorf("Rename() calls = %d, want two publish calls only", renameCount)
		}
	})
}

func prepareExportPublish(t *testing.T) (*dao.Store, context.Context, string, string, string) {
	t.Helper()
	store := openTestStore(t)
	ctx := context.Background()
	ensureTask(t, store, ctx)
	seedExportItems(t, store, ctx, dao.Item{
		TaskID:     "task-1",
		TraceID:    "success",
		InputIndex: 1,
		RawJSON:    []byte(`{"trace_id":"success","prompt":"synthetic prompt"}`),
		Prompt:     "synthetic prompt",
		State:      dao.ItemPending,
	})
	claimExportItems(t, store, ctx, 1)
	markExportSucceeded(t, store, ctx, "success", 1, 0,
		`{"is_attack":false,"case_type":"typical","explanation":"synthetic safe explanation"}`,
	)
	directory := t.TempDir()
	outputPath := filepath.Join(directory, "result.jsonl")
	return store, ctx, directory, outputPath, filepath.Join(directory, "result.failed.jsonl")
}

func failSecondExportPublish(publishErr error) func(string, string) error {
	publishCount := 0
	return func(oldPath, newPath string) error {
		if strings.HasPrefix(filepath.Base(oldPath), ".sendllm-export-") {
			publishCount++
			if publishCount == 2 {
				return publishErr
			}
		}
		return os.Rename(oldPath, newPath)
	}
}

func seedExportItems(t *testing.T, store *dao.Store, ctx context.Context, items ...dao.Item) {
	t.Helper()
	imp, err := store.BeginImport(ctx, "task-1")
	if err != nil {
		t.Fatalf("BeginImport() error = %v", err)
	}
	defer func() { _ = imp.Rollback() }()
	for _, item := range items {
		if _, err := imp.Add(ctx, item); err != nil {
			t.Fatalf("Add(%q) error = %v", item.TraceID, err)
		}
	}
	if err := imp.Commit(); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
}

func claimExportItems(t *testing.T, store *dao.Store, ctx context.Context, count int) {
	t.Helper()
	items, err := store.Claim(ctx, "task-1", count, time.Now())
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if len(items) != count {
		t.Fatalf("Claim() count = %d, want %d", len(items), count)
	}
}

func markExportSucceeded(
	t *testing.T,
	store *dao.Store,
	ctx context.Context,
	traceID string,
	requestAttempts int,
	repairAttempts int,
	annotation string,
) {
	t.Helper()
	attempt := exportAttempt(requestAttempts, repairAttempts)
	if err := store.MarkSucceeded(ctx, "task-1", traceID, attempt, []byte(annotation)); err != nil {
		t.Fatalf("MarkSucceeded(%q) error = %v", traceID, err)
	}
}

func markExportFailed(
	t *testing.T,
	store *dao.Store,
	ctx context.Context,
	traceID string,
	requestAttempts int,
	repairAttempts int,
	category string,
	summary string,
) {
	t.Helper()
	attempt := exportAttempt(requestAttempts, repairAttempts)
	if err := store.MarkFailed(ctx, "task-1", traceID, attempt, category, summary); err != nil {
		t.Fatalf("MarkFailed(%q) error = %v", traceID, err)
	}
}

func exportAttempt(requestAttempts, repairAttempts int) dao.Attempt {
	now := time.Now()
	return dao.Attempt{
		Phase:         "classification",
		RequestNumber: requestAttempts,
		RepairNumber:  repairAttempts,
		StartedAt:     now,
		FinishedAt:    now,
	}
}

func readJSONLObjects(t *testing.T, path string) []map[string]any {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open(%q) error = %v", path, err)
	}
	defer func() { _ = file.Close() }()

	var records []map[string]any
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var record map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("Unmarshal(%q) error = %v", scanner.Text(), err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan %q: %v", path, err)
	}
	return records
}

func writeOldExportFiles(t *testing.T, outputPath, failedPath string) {
	t.Helper()
	if err := os.WriteFile(outputPath, []byte("old success\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(success) error = %v", err)
	}
	if err := os.WriteFile(failedPath, []byte("old failure\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(failed) error = %v", err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	return string(contents)
}

func assertNoExportTemps(t *testing.T, directory string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(directory, ".sendllm-export-*"))
	if err != nil {
		t.Fatalf("Glob(export temps) error = %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("export temp files remain: %v", matches)
	}
}
