package dao_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"sendllm/internal/dao"
)

func TestStore_ItemStateMachine(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	ensureTask(t, store, ctx)
	seedItems(t, store, ctx, sourceItem("success", 1, `{"trace_id":"success","prompt":"test"}`))

	claimed, err := store.Claim(ctx, "task-1", 1, time.Now())
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if len(claimed) != 1 || claimed[0].TraceID != "success" {
		t.Fatalf("Claim() = %+v, want success item", claimed)
	}
	attempt := dao.Attempt{Phase: "classification", RequestNumber: 1, StartedAt: time.Now(), FinishedAt: time.Now()}
	if err := store.MarkSucceeded(
		ctx,
		"task-1",
		"success",
		attempt,
		[]byte(`{"is_attack":false,"case_type":"typical","explanation":"内容没有攻击或规避安全控制的意图"}`),
	); err != nil {
		t.Fatalf("MarkSucceeded() error = %v", err)
	}
	claimed, err = store.Claim(ctx, "task-1", 1, time.Now())
	if err != nil {
		t.Fatalf("Claim(after success) error = %v", err)
	}
	if len(claimed) != 0 {
		t.Fatalf("Claim(after success) = %+v, want no items", claimed)
	}
	err = store.MarkFailed(ctx, "task-1", "success", attempt, "permanent", "safe summary")
	if !errors.Is(err, dao.ErrInvalidTransition) {
		t.Fatalf("MarkFailed(after success) error = %v, want %v", err, dao.ErrInvalidTransition)
	}
}

func TestStore_RetryAndFailureTransitions(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	ensureTask(t, store, ctx)
	seedItems(t, store, ctx, sourceItem("retry", 1, `{"trace_id":"retry","prompt":"test"}`))

	claimed, err := store.Claim(ctx, "task-1", 1, time.Now())
	if err != nil || len(claimed) != 1 {
		t.Fatalf("Claim() = (%+v, %v), want one item", claimed, err)
	}
	next := time.Now().Add(time.Minute).UTC().Truncate(time.Microsecond)
	attempt := dao.Attempt{
		Phase:         "classification",
		RequestNumber: 1,
		StartedAt:     time.Now(),
		FinishedAt:    time.Now(),
		ErrorCategory: "server",
		Retryable:     true,
	}
	if err := store.ScheduleRetry(ctx, "task-1", "retry", attempt, next, "server", "safe summary"); err != nil {
		t.Fatalf("ScheduleRetry() error = %v", err)
	}
	if claimed, err := store.Claim(ctx, "task-1", 1, next.Add(-time.Second)); err != nil || len(claimed) != 0 {
		t.Fatalf("Claim(before retry) = (%+v, %v), want no items", claimed, err)
	}
	gotNext, ok, err := store.NextRetryAt(ctx, "task-1")
	if err != nil || !ok || !gotNext.Equal(next) {
		t.Fatalf("NextRetryAt() = (%v, %v, %v), want (%v, true, nil)", gotNext, ok, err, next)
	}
	claimed, err = store.Claim(ctx, "task-1", 1, next)
	if err != nil || len(claimed) != 1 || claimed[0].RequestAttempts != 1 {
		t.Fatalf("Claim(retry) = (%+v, %v), want persisted attempt count", claimed, err)
	}
	attempt.RequestNumber = 2
	attempt.Retryable = false
	if err := store.MarkFailed(ctx, "task-1", "retry", attempt, "server", "exhausted"); err != nil {
		t.Fatalf("MarkFailed() error = %v", err)
	}
	counts, err := store.Counts(ctx, "task-1")
	if err != nil {
		t.Fatalf("Counts() error = %v", err)
	}
	if counts.Failed != 1 || counts.Pending != 0 || counts.Processing != 0 || counts.RetryWait != 0 {
		t.Fatalf("Counts() = %+v, want one failed", counts)
	}
}

func TestStore_ItemLogIncludesLastAPIKeyEnv(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	ensureTask(t, store, ctx)
	seedItems(t, store, ctx, sourceItem("keyed", 1, `{"trace_id":"keyed","prompt":"test"}`))

	claimed, err := store.Claim(ctx, "task-1", 1, time.Now())
	if err != nil || len(claimed) != 1 {
		t.Fatalf("Claim() = (%+v, %v), want one item", claimed, err)
	}
	attempt := dao.Attempt{
		Phase:         "classification",
		RequestNumber: 1,
		StartedAt:     time.Now(),
		FinishedAt:    time.Now(),
		APIKeyEnv:     "KEY_A",
	}
	if err := store.MarkFailed(ctx, "task-1", "keyed", attempt, "server", "safe summary"); err != nil {
		t.Fatalf("MarkFailed() error = %v", err)
	}

	var got dao.ItemLogRecord
	if err := store.ForEachItemLog(ctx, "task-1", func(record dao.ItemLogRecord) error {
		got = record
		return nil
	}); err != nil {
		t.Fatalf("ForEachItemLog() error = %v", err)
	}
	if got.APIKeyEnv != "KEY_A" {
		t.Fatalf("APIKeyEnv = %q, want KEY_A", got.APIKeyEnv)
	}
}

func TestStore_OpenMigratesMissingAttemptAPIKeyEnvColumn(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	rawDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	if _, err := rawDB.ExecContext(ctx, `
		CREATE TABLE tasks (
			id TEXT PRIMARY KEY,
			semantic_hash TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE items (
			task_id TEXT NOT NULL,
			trace_id TEXT NOT NULL,
			input_index INTEGER NOT NULL,
			source_hash TEXT NOT NULL,
			raw_json BLOB NOT NULL,
			prompt TEXT NOT NULL,
			response TEXT NOT NULL,
			state TEXT NOT NULL,
			request_attempts INTEGER NOT NULL DEFAULT 0,
			repair_attempts INTEGER NOT NULL DEFAULT 0,
			next_attempt_at TEXT,
			annotation BLOB,
			error_category TEXT,
			error_summary TEXT,
			PRIMARY KEY (task_id, trace_id)
		);
		CREATE TABLE attempts (
			id INTEGER PRIMARY KEY,
			task_id TEXT NOT NULL,
			trace_id TEXT NOT NULL,
			phase TEXT NOT NULL,
			started_at TEXT NOT NULL,
			finished_at TEXT,
			http_status INTEGER,
			error_category TEXT,
			retryable INTEGER NOT NULL DEFAULT 0,
			raw_response TEXT,
			validation_error TEXT,
			prompt_tokens INTEGER,
			completion_tokens INTEGER
		);
	`); err != nil {
		_ = rawDB.Close()
		t.Fatalf("create legacy schema error = %v", err)
	}
	if err := rawDB.Close(); err != nil {
		t.Fatalf("Close(raw DB) error = %v", err)
	}

	store, err := dao.Open(ctx, path)
	if err != nil {
		t.Fatalf("dao.Open(legacy) error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close(store) error = %v", err)
	}

	rawDB, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open(after migration) error = %v", err)
	}
	defer rawDB.Close()
	rows, err := rawDB.QueryContext(ctx, "PRAGMA table_info(attempts)")
	if err != nil {
		t.Fatalf("PRAGMA table_info(attempts) error = %v", err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan table info error = %v", err)
		}
		if name == "api_key_env" {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate table info error = %v", err)
	}
	if !found {
		t.Fatal("api_key_env column was not added")
	}
}

func TestStore_RetryTimesSortChronologicallyAcrossFractionWidth(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	ensureTask(t, store, ctx)
	seedItems(t, store, ctx,
		sourceItem("earlier", 1, `{"trace_id":"earlier","prompt":"test"}`),
		sourceItem("later", 2, `{"trace_id":"later","prompt":"test"}`),
	)

	base := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	claimed, err := store.Claim(ctx, "task-1", 2, base)
	if err != nil || len(claimed) != 2 {
		t.Fatalf("Claim() = (%+v, %v), want two items", claimed, err)
	}
	attempt := dao.Attempt{
		Phase:         "classification",
		RequestNumber: 1,
		StartedAt:     base,
		FinishedAt:    base,
		ErrorCategory: "server",
		Retryable:     true,
	}
	earlier := base.Add(100 * time.Millisecond)
	later := base.Add(110 * time.Millisecond)
	if err := store.ScheduleRetry(ctx, "task-1", "earlier", attempt, earlier, "server", "safe summary"); err != nil {
		t.Fatalf("ScheduleRetry(earlier) error = %v", err)
	}
	if err := store.ScheduleRetry(ctx, "task-1", "later", attempt, later, "server", "safe summary"); err != nil {
		t.Fatalf("ScheduleRetry(later) error = %v", err)
	}

	gotNext, ok, err := store.NextRetryAt(ctx, "task-1")
	if err != nil || !ok || !gotNext.Equal(earlier) {
		t.Errorf("NextRetryAt() = (%v, %v, %v), want (%v, true, nil)", gotNext, ok, err, earlier)
	}
	claimed, err = store.Claim(ctx, "task-1", 2, base.Add(105*time.Millisecond))
	if err != nil || len(claimed) != 1 || claimed[0].TraceID != "earlier" {
		t.Errorf("Claim(between retry times) = (%+v, %v), want earlier item", claimed, err)
	}
}

func TestStore_ClaimMigratesLegacyVariableWidthRetryTime(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := dao.Open(ctx, path)
	if err != nil {
		t.Fatalf("Open(first) error = %v", err)
	}
	ensureTask(t, store, ctx)
	seedItems(t, store, ctx, sourceItem("legacy", 1, `{"trace_id":"legacy","prompt":"test"}`))

	base := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	claimed, err := store.Claim(ctx, "task-1", 1, base)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("Claim() = (%+v, %v), want one item", claimed, err)
	}
	attempt := dao.Attempt{Phase: "classification", RequestNumber: 1, StartedAt: base, FinishedAt: base}
	retryAt := base.Add(100 * time.Millisecond)
	if err := store.ScheduleRetry(ctx, "task-1", "legacy", attempt, retryAt, "server", "safe summary"); err != nil {
		t.Fatalf("ScheduleRetry() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close(first) error = %v", err)
	}

	rawDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	if _, err := rawDB.ExecContext(
		ctx,
		"UPDATE items SET next_attempt_at = ? WHERE task_id = ? AND trace_id = ?",
		retryAt.Format(time.RFC3339Nano),
		"task-1",
		"legacy",
	); err != nil {
		_ = rawDB.Close()
		t.Fatalf("write legacy retry time error = %v", err)
	}
	if err := rawDB.Close(); err != nil {
		t.Fatalf("Close(raw DB) error = %v", err)
	}

	store, err = dao.Open(ctx, path)
	if err != nil {
		t.Fatalf("Open(resume) error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close(resume) error = %v", err)
		}
	})
	claimed, err = store.Claim(ctx, "task-1", 1, base.Add(105*time.Millisecond))
	if err != nil || len(claimed) != 1 || claimed[0].TraceID != "legacy" {
		t.Fatalf("Claim(resumed legacy retry) = (%+v, %v), want legacy item", claimed, err)
	}
}

func TestStore_ResetProcessing(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	ensureTask(t, store, ctx)
	seedItems(t, store, ctx,
		sourceItem("processing", 1, `{"trace_id":"processing","prompt":"test"}`),
		sourceItem("pending", 2, `{"trace_id":"pending","prompt":"test"}`),
	)
	if claimed, err := store.Claim(ctx, "task-1", 1, time.Now()); err != nil || len(claimed) != 1 {
		t.Fatalf("Claim() = (%+v, %v), want one item", claimed, err)
	}
	reset, err := store.ResetProcessing(ctx, "task-1")
	if err != nil || reset != 1 {
		t.Fatalf("ResetProcessing() = (%d, %v), want (1, nil)", reset, err)
	}
	claimed, err := store.Claim(ctx, "task-1", 2, time.Now())
	if err != nil || len(claimed) != 2 {
		t.Fatalf("Claim(after reset) = (%+v, %v), want two items", claimed, err)
	}
}

func seedItems(t *testing.T, store *dao.Store, ctx context.Context, items ...dao.Item) {
	t.Helper()
	imp := beginImport(t, store, ctx)
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
