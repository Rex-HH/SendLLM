package dao_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"sendllm/internal/dao"
)

func TestStore_EnsureTaskRejectsSemanticChange(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	if err := store.EnsureTask(ctx, dao.Task{ID: "task-1", SemanticHash: "hash-a"}); err != nil {
		t.Fatalf("EnsureTask(first) error = %v", err)
	}

	err := store.EnsureTask(ctx, dao.Task{ID: "task-1", SemanticHash: "hash-b"})
	if !errors.Is(err, dao.ErrTaskMismatch) {
		t.Fatalf("EnsureTask(second) error = %v, want %v", err, dao.ErrTaskMismatch)
	}
}

func TestStore_EnsureTaskAcceptsMatchingSemanticHash(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	task := dao.Task{ID: "task-1", SemanticHash: "hash-a"}
	if err := store.EnsureTask(ctx, task); err != nil {
		t.Fatalf("EnsureTask(first) error = %v", err)
	}
	if err := store.EnsureTask(ctx, task); err != nil {
		t.Fatalf("EnsureTask(resume) error = %v", err)
	}
}

func TestImport_AddSkipsExactDuplicate(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	ensureTask(t, store, ctx)

	first := beginImport(t, store, ctx)
	if got, err := first.Add(ctx, sourceItem("id-1", 1, `{"trace_id":"id-1","prompt":"first"}`)); err != nil || got != dao.ImportAdded {
		t.Fatalf("Add(first) = (%v, %v), want (%v, nil)", got, err, dao.ImportAdded)
	}
	if err := first.Commit(); err != nil {
		t.Fatalf("Commit(first) error = %v", err)
	}

	second := beginImport(t, store, ctx)
	defer func() { _ = second.Rollback() }()
	if got, err := second.Add(ctx, sourceItem("id-1", 1, `{"prompt":"first", "trace_id":"id-1"}`)); err != nil || got != dao.ImportSkipped {
		t.Fatalf("Add(duplicate) = (%v, %v), want (%v, nil)", got, err, dao.ImportSkipped)
	}
}

func TestImport_AddConflictRollsBackBatch(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	ensureTask(t, store, ctx)

	seed := beginImport(t, store, ctx)
	if _, err := seed.Add(ctx, sourceItem("existing", 1, `{"trace_id":"existing","prompt":"old"}`)); err != nil {
		t.Fatalf("Add(seed) error = %v", err)
	}
	if err := seed.Commit(); err != nil {
		t.Fatalf("Commit(seed) error = %v", err)
	}

	imp := beginImport(t, store, ctx)
	if _, err := imp.Add(ctx, sourceItem("new", 2, `{"trace_id":"new","prompt":"new"}`)); err != nil {
		t.Fatalf("Add(new) error = %v", err)
	}
	if _, err := imp.Add(ctx, sourceItem("existing", 1, `{"trace_id":"existing","prompt":"changed"}`)); !errors.Is(err, dao.ErrTraceConflict) {
		t.Fatalf("Add(conflict) error = %v, want %v", err, dao.ErrTraceConflict)
	}
	if err := imp.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}

	afterRollback := beginImport(t, store, ctx)
	defer func() { _ = afterRollback.Rollback() }()
	if got, err := afterRollback.Add(ctx, sourceItem("new", 2, `{"trace_id":"new","prompt":"new"}`)); err != nil || got != dao.ImportAdded {
		t.Fatalf("Add(rolled back item) = (%v, %v), want (%v, nil)", got, err, dao.ImportAdded)
	}
}

func TestImport_AddDetectsConflictBetweenAdjacentLargeIntegers(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	ensureTask(t, store, ctx)

	first := beginImport(t, store, ctx)
	if _, err := first.Add(
		ctx,
		sourceItem("large-integer", 1, `{"trace_id":"large-integer","prompt":"test","sequence":9007199254740992}`),
	); err != nil {
		t.Fatalf("Add(first) error = %v", err)
	}
	if err := first.Commit(); err != nil {
		t.Fatalf("Commit(first) error = %v", err)
	}

	second := beginImport(t, store, ctx)
	defer func() { _ = second.Rollback() }()
	_, err := second.Add(
		ctx,
		sourceItem("large-integer", 1, `{"trace_id":"large-integer","prompt":"test","sequence":9007199254740993}`),
	)
	if !errors.Is(err, dao.ErrTraceConflict) {
		t.Fatalf("Add(conflicting large integer) error = %v, want %v", err, dao.ErrTraceConflict)
	}
}

func openTestStore(t *testing.T) *dao.Store {
	t.Helper()
	store, err := dao.Open(context.Background(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return store
}

func ensureTask(t *testing.T, store *dao.Store, ctx context.Context) {
	t.Helper()
	if err := store.EnsureTask(ctx, dao.Task{ID: "task-1", SemanticHash: "hash-a"}); err != nil {
		t.Fatalf("EnsureTask() error = %v", err)
	}
}

func beginImport(t *testing.T, store *dao.Store, ctx context.Context) *dao.Import {
	t.Helper()
	imp, err := store.BeginImport(ctx, "task-1")
	if err != nil {
		t.Fatalf("BeginImport() error = %v", err)
	}
	return imp
}

func sourceItem(traceID string, inputIndex int64, raw string) dao.Item {
	return dao.Item{
		TaskID:     "task-1",
		TraceID:    traceID,
		InputIndex: inputIndex,
		RawJSON:    []byte(raw),
		Prompt:     "unused by DAO test",
		State:      dao.ItemPending,
	}
}
