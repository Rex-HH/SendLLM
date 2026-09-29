package dao

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

// TestSafetyReviewStoreOpenPreparesIndependentSchema 验证独立库只包含六张表并启用安全连接参数。
func TestSafetyReviewStoreOpenPreparesIndependentSchema(t *testing.T) {
	store, path := openSafetyReviewStore(t)
	_ = context.Background()
	defer closeSafetyReviewStore(t, store)

	assertSafetyReviewPragma(t, store, "journal_mode", "wal")
	assertSafetyReviewPragma(t, store, "busy_timeout", "5000")
	assertSafetyReviewPragma(t, store, "foreign_keys", "1")

	db := openSafetyReviewRawDB(t, path)
	defer closeSafetyReviewRawDB(t, db)
	tables := querySafetyReviewStrings(t, db, `SELECT name FROM sqlite_master
		WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	wantTables := []string{
		"review_attempts", "review_decisions", "review_items",
		"review_preflight_runs", "review_stages", "review_tasks",
	}
	if len(tables) != len(wantTables) {
		t.Fatalf("table count = %d, want %d", len(tables), len(wantTables))
	}
	for index, table := range wantTables {
		if tables[index] != table {
			t.Fatalf("table[%d] = %q, want %q", index, tables[index], table)
		}
	}
	indexes := querySafetyReviewStrings(t, db, `SELECT name FROM sqlite_master
		WHERE type = 'index' AND tbl_name = 'review_stages' ORDER BY name`)
	foundClaimIndex := false
	for _, name := range indexes {
		if name == "review_stages_claim_idx" {
			foundClaimIndex = true
		}
	}
	if !foundClaimIndex {
		t.Fatalf("review_stages indexes = %v, want claim index", indexes)
	}
}

// TestSafetyReviewStoreSchemaEnforcesForeignKeys 验证核心外键均已声明。
func TestSafetyReviewStoreSchemaEnforcesForeignKeys(t *testing.T) {
	store, path := openSafetyReviewStore(t)
	defer closeSafetyReviewStore(t, store)
	db := openSafetyReviewRawDB(t, path)
	defer closeSafetyReviewRawDB(t, db)

	foreignKeys := map[string]int{
		"review_items":          1,
		"review_stages":         2,
		"review_attempts":       3,
		"review_decisions":      2,
		"review_preflight_runs": 1,
	}
	for table, want := range foreignKeys {
		got := querySafetyReviewInt(t, db, "SELECT count(*) FROM pragma_foreign_key_list('"+table+"')")
		if got != want {
			t.Fatalf("table %s foreign key count = %d, want %d", table, got, want)
		}
	}
}

// TestSafetyReviewStoreEnsureTaskIdentity 验证任务身份可恢复且语义、场景、快照不可漂移。
func TestSafetyReviewStoreEnsureTaskIdentity(t *testing.T) {
	store, path := openSafetyReviewStore(t)
	ctx := context.Background()
	task := SafetyReviewTask{
		ID:                  "safety-task-1",
		SemanticFingerprint: "fingerprint-a",
		Scene:               "response",
		SnapshotDir:         "snapshots/a",
	}
	if err := store.EnsureTask(ctx, task); err != nil {
		t.Fatalf("EnsureTask(first) error = %v", err)
	}
	if err := store.EnsureTask(ctx, task); err != nil {
		t.Fatalf("EnsureTask(resume) error = %v", err)
	}

	mutations := []SafetyReviewTask{
		{ID: task.ID, SemanticFingerprint: "fingerprint-b", Scene: task.Scene, SnapshotDir: task.SnapshotDir},
		{ID: task.ID, SemanticFingerprint: task.SemanticFingerprint, Scene: "prompt", SnapshotDir: task.SnapshotDir},
		{ID: task.ID, SemanticFingerprint: task.SemanticFingerprint, Scene: task.Scene, SnapshotDir: "snapshots/b"},
	}
	for index, mutation := range mutations {
		if err := store.EnsureTask(ctx, mutation); !errors.Is(err, ErrTaskMismatch) {
			t.Fatalf("EnsureTask(mutation %d) error = %v, want task mismatch", index, err)
		}
	}

	closeSafetyReviewStore(t, store)
	reopened, err := OpenSafetyReview(ctx, path)
	if err != nil {
		t.Fatalf("OpenSafetyReview(reopen) error = %v", err)
	}
	defer closeSafetyReviewStore(t, reopened)
	if err := reopened.EnsureTask(ctx, task); err != nil {
		t.Fatalf("EnsureTask(after reopen) error = %v", err)
	}
}

// openSafetyReviewStore 打开一个临时 Safety Review 状态库。
func openSafetyReviewStore(t *testing.T) (*SafetyReviewStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "safety-review.db")
	store, err := OpenSafetyReview(context.Background(), path)
	if err != nil {
		t.Fatalf("OpenSafetyReview() error = %v", err)
	}
	return store, path
}

// openSafetyReviewRawDB 打开只读检查使用的原生 SQLite 连接。
func openSafetyReviewRawDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open(safety review) error = %v", err)
	}
	return db
}

// closeSafetyReviewRawDB 关闭原生 SQLite 检查连接。
func closeSafetyReviewRawDB(t *testing.T, db *sql.DB) {
	t.Helper()
	if err := db.Close(); err != nil {
		t.Errorf("raw DB Close() error = %v", err)
	}
}

// closeSafetyReviewStore 关闭 Safety Review 状态库。
func closeSafetyReviewStore(t *testing.T, store *SafetyReviewStore) {
	t.Helper()
	if err := store.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

// assertSafetyReviewPragma 断言 SQLite PRAGMA 的持久化配置值。
func assertSafetyReviewPragma(t *testing.T, store *SafetyReviewStore, name, want string) {
	t.Helper()
	got := querySafetyReviewText(t, store.db, "PRAGMA "+name)
	if got != want {
		t.Fatalf("PRAGMA %s = %q, want %q", name, got, want)
	}
}

// querySafetyReviewStrings 查询字符串列表。
func querySafetyReviewStrings(t *testing.T, db *sql.DB, query string) []string {
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

// querySafetyReviewInt 查询单个整数值。
func querySafetyReviewInt(t *testing.T, db *sql.DB, query string) int {
	t.Helper()
	var value int
	if err := db.QueryRow(query).Scan(&value); err != nil {
		t.Fatalf("QueryRow(%q) error = %v", query, err)
	}
	return value
}

// querySafetyReviewText 查询单个字符串值。
func querySafetyReviewText(t *testing.T, db *sql.DB, query string) string {
	t.Helper()
	var value string
	if err := db.QueryRow(query).Scan(&value); err != nil {
		t.Fatalf("QueryRow(%q) error = %v", query, err)
	}
	return value
}
