package dao

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
)

// TestSafetyReviewImportJSONLCreatesInitialStages 验证导入保留未知字段、顺序和三条初始阶段。
func TestSafetyReviewImportJSONLCreatesInitialStages(t *testing.T) {
	store, path := openSafetyReviewStore(t)
	ctx := context.Background()
	defer closeSafetyReviewStore(t, store)
	task := newSafetyReviewTask("response")
	if err := store.EnsureTask(ctx, task); err != nil {
		t.Fatalf("EnsureTask() error = %v", err)
	}

	input := strings.Join([]string{
		`{"trace_id":"one","prompt":"context one","response":"synthetic one","unknown":{"kept":true}}`,
		`{"trace_id":"two","prompt":"context two","response":"synthetic two","unknown":"kept"}`,
		`{"trace_id":"three","prompt":"context three","response":"synthetic three"}`,
	}, "\n") + "\n"
	stats, err := store.ImportJSONL(ctx, task.ID, strings.NewReader(input))
	if err != nil {
		t.Fatalf("ImportJSONL() error = %v", err)
	}
	if stats.Added != 3 || stats.Skipped != 0 {
		t.Fatalf("stats = %+v, want three added", stats)
	}

	db := openSafetyReviewRawDB(t, path)
	defer closeSafetyReviewRawDB(t, db)
	ids := querySafetyReviewRows(t, db, `SELECT trace_id FROM review_items ORDER BY input_index`)
	wantIDs := []string{"one", "two", "three"}
	if len(ids) != len(wantIDs) {
		t.Fatalf("item count = %d, want %d", len(ids), len(wantIDs))
	}
	for index, id := range wantIDs {
		if ids[index] != id {
			t.Fatalf("item[%d] = %q, want %q", index, ids[index], id)
		}
	}
	var raw string
	if err := db.QueryRow(`SELECT raw_json FROM review_items WHERE trace_id = 'one'`).Scan(&raw); err != nil {
		t.Fatalf("read raw_json error = %v", err)
	}
	if !strings.Contains(raw, `"unknown"`) {
		t.Fatal("raw_json did not preserve unknown fields")
	}
	stageCount := querySafetyReviewInt(t, db, "SELECT count(*) FROM review_stages")
	if stageCount != 9 {
		t.Fatalf("stage count = %d, want 9", stageCount)
	}
	initialStates := querySafetyReviewRows(t, db, `SELECT DISTINCT state FROM review_stages`)
	if len(initialStates) != 1 || initialStates[0] != "pending" {
		t.Fatalf("initial stage states = %v, want pending", initialStates)
	}
}

// TestSafetyReviewImportJSONLAllowsPromptSceneContextResponse 验证 Prompt 场景导入时保留回复上下文。
func TestSafetyReviewImportJSONLAllowsPromptSceneContextResponse(t *testing.T) {
	store, path := openSafetyReviewStore(t)
	ctx := context.Background()
	defer closeSafetyReviewStore(t, store)
	task := newSafetyReviewTask("prompt")
	if err := store.EnsureTask(ctx, task); err != nil {
		t.Fatalf("EnsureTask() error = %v", err)
	}
	line := `{"trace_id":"one","prompt":"synthetic prompt","response":"synthetic context"}`
	stats, err := store.ImportJSONL(ctx, task.ID, strings.NewReader(line+"\n"))
	if err != nil {
		t.Fatalf("ImportJSONL(prompt with response) error = %v", err)
	}
	if stats.Added != 1 || stats.Skipped != 0 {
		t.Fatalf("stats = %+v, want one added", stats)
	}
	db := openSafetyReviewRawDB(t, path)
	defer closeSafetyReviewRawDB(t, db)
	response := querySafetyReviewText(t, db, `SELECT response FROM review_items WHERE trace_id = 'one'`)
	if response != "synthetic context" {
		t.Fatalf("response = %q, want preserved context", response)
	}
}

// TestSafetyReviewImportJSONLDuplicateRules 验证完全相同来源跳过，不同来源整体回滚。
func TestSafetyReviewImportJSONLDuplicateRules(t *testing.T) {
	store, path := openSafetyReviewStore(t)
	ctx := context.Background()
	defer closeSafetyReviewStore(t, store)
	task := newSafetyReviewTask("prompt")
	if err := store.EnsureTask(ctx, task); err != nil {
		t.Fatalf("EnsureTask() error = %v", err)
	}

	first := `{"prompt":"synthetic one","trace_id":"one","sequence":9007199254740993}`
	stats, err := store.ImportJSONL(ctx, task.ID, strings.NewReader(first+"\n"))
	if err != nil || stats.Added != 1 {
		t.Fatalf("ImportJSONL(first) = (%+v, %v), want one added", stats, err)
	}
	stats, err = store.ImportJSONL(
		ctx,
		task.ID,
		strings.NewReader(`{"trace_id":"one","prompt":"synthetic one","sequence":9007199254740993}`+"\n"),
	)
	if err != nil || stats.Skipped != 1 || stats.Added != 0 {
		t.Fatalf("ImportJSONL(exact duplicate) = (%+v, %v), want one skipped", stats, err)
	}

	conflict := strings.Join([]string{
		`{"trace_id":"two","prompt":"synthetic two"}`,
		`{"trace_id":"one","prompt":"synthetic one","sequence":9007199254740992}`,
	}, "\n") + "\n"
	if _, err := store.ImportJSONL(ctx, task.ID, strings.NewReader(conflict)); !errors.Is(err, ErrTraceConflict) {
		t.Fatalf("ImportJSONL(conflict) error = %v, want %v", err, ErrTraceConflict)
	}
	db := openSafetyReviewRawDB(t, path)
	defer closeSafetyReviewRawDB(t, db)
	if count := querySafetyReviewInt(t, db, "SELECT count(*) FROM review_items"); count != 1 {
		t.Fatalf("item count after rollback = %d, want 1", count)
	}
}

// TestSafetyReviewImportJSONLUseNumberPreservesDecimalLexeme 验证十进制词形不会被浮点重写。
func TestSafetyReviewImportJSONLUseNumberPreservesDecimalLexeme(t *testing.T) {
	store, _ := openSafetyReviewStore(t)
	ctx := context.Background()
	defer closeSafetyReviewStore(t, store)
	task := newSafetyReviewTask("prompt")
	if err := store.EnsureTask(ctx, task); err != nil {
		t.Fatalf("EnsureTask() error = %v", err)
	}
	first := `{"trace_id":"decimal","prompt":"synthetic","score":1.0}`
	stats, err := store.ImportJSONL(ctx, task.ID, strings.NewReader(first+"\n"))
	if err != nil || stats.Added != 1 {
		t.Fatalf("ImportJSONL(first) = (%+v, %v), want one added", stats, err)
	}
	stats, err = store.ImportJSONL(
		ctx,
		task.ID,
		strings.NewReader(`{"prompt":"synthetic","score":1.0,"trace_id":"decimal"}`+"\n"),
	)
	if err != nil || stats.Skipped != 1 || stats.Added != 0 {
		t.Fatalf("ImportJSONL(exact decimal duplicate) = (%+v, %v), want one skipped", stats, err)
	}
}

// TestSafetyReviewImportJSONLRejectsInvalidRows 验证非法 JSON、缺失 ID、
// 空载荷和场景不匹配都会回滚。
func TestSafetyReviewImportJSONLRejectsInvalidRows(t *testing.T) {
	tests := []struct {
		name string
		task SafetyReviewTask
		line string
	}{
		{name: "invalid json", task: newSafetyReviewTask("prompt"), line: `{"trace_id":`},
		{name: "missing trace id", task: newSafetyReviewTask("prompt"), line: `{"prompt":"synthetic"}`},
		{name: "empty payload", task: newSafetyReviewTask("prompt"), line: `{"trace_id":"one"}`},
		{
			name: "response scene without response",
			task: newSafetyReviewTask("response"),
			line: `{"trace_id":"one","prompt":"synthetic"}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, path := openSafetyReviewStore(t)
			ctx := context.Background()
			defer closeSafetyReviewStore(t, store)
			if err := store.EnsureTask(ctx, test.task); err != nil {
				t.Fatalf("EnsureTask() error = %v", err)
			}
			if _, err := store.ImportJSONL(ctx, test.task.ID, strings.NewReader(test.line+"\n")); err == nil {
				t.Fatal("ImportJSONL() accepted invalid row")
			}
			db := openSafetyReviewRawDB(t, path)
			defer closeSafetyReviewRawDB(t, db)
			if count := querySafetyReviewInt(t, db, "SELECT count(*) FROM review_items"); count != 0 {
				t.Fatalf("item count = %d, want 0", count)
			}
		})
	}
}

// newSafetyReviewTask 构造带固定 ID 的 Safety Review 任务。
func newSafetyReviewTask(scene string) SafetyReviewTask {
	return SafetyReviewTask{
		ID:                  "safety-task-1",
		SemanticFingerprint: "fingerprint-a",
		Scene:               scene,
		SnapshotDir:         "snapshots/a",
	}
}

// querySafetyReviewRows 查询多行字符串结果。
func querySafetyReviewRows(t *testing.T, db *sql.DB, query string) []string {
	t.Helper()
	rows, err := db.Query(query)
	if err != nil {
		t.Fatalf("Query(%q) error = %v", query, err)
	}
	defer rows.Close()
	values := make([]string, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatalf("Scan() error = %v", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows error = %v", err)
	}
	return values
}
