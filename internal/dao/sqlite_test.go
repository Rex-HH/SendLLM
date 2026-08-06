package dao_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
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

func TestImport_AddLazilyMigratesLegacyHashForExactSource(t *testing.T) {
	tests := []struct {
		name      string
		traceID   string
		raw       string
		canonical string
	}{
		{
			name:      "large integer",
			traceID:   "legacy-large",
			raw:       `{"trace_id":"legacy-large","prompt":"test","sequence":9007199254740993}`,
			canonical: `{"prompt":"test","sequence":9007199254740993,"trace_id":"legacy-large"}`,
		},
		{
			name:      "decimal lexical form",
			traceID:   "legacy-decimal",
			raw:       `{"trace_id":"legacy-decimal","prompt":"test","score":1.0}`,
			canonical: `{"prompt":"test","score":1.0,"trace_id":"legacy-decimal"}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.db")
			store := openTestStoreAt(t, path)
			ctx := context.Background()
			ensureTask(t, store, ctx)
			seedItems(t, store, ctx, sourceItem(test.traceID, 1, test.raw))

			legacyHash := legacySourceHash(t, []byte(test.raw))
			wantHashBytes := sha256.Sum256([]byte(test.canonical))
			wantHash := hex.EncodeToString(wantHashBytes[:])
			if legacyHash == wantHash {
				t.Fatal("legacy and current hashes unexpectedly match")
			}
			writeStoredSourceHash(t, path, ctx, test.traceID, legacyHash)

			batch := beginImport(t, store, ctx)
			disposition, err := batch.Add(ctx, sourceItem(test.traceID, 1, test.raw))
			if err != nil || disposition != dao.ImportSkipped {
				_ = batch.Rollback()
				t.Fatalf("Add(exact legacy source) = (%v, %v), want (%v, nil)", disposition, err, dao.ImportSkipped)
			}
			if err := batch.Commit(); err != nil {
				t.Fatalf("Commit() error = %v", err)
			}
			if gotHash := readStoredSourceHash(t, path, ctx, test.traceID); gotHash != wantHash {
				t.Fatalf("source_hash = %q, want migrated hash %q", gotHash, wantHash)
			}
		})
	}
}

func TestImport_AddRejectsDifferentSourceDespiteLegacyHashCollision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store := openTestStoreAt(t, path)
	ctx := context.Background()
	ensureTask(t, store, ctx)
	existingRaw := []byte(`{"trace_id":"legacy-collision","prompt":"test","sequence":9007199254740992}`)
	incomingRaw := []byte(`{"trace_id":"legacy-collision","prompt":"test","sequence":9007199254740993}`)
	seedItems(t, store, ctx, sourceItem("legacy-collision", 1, string(existingRaw)))

	existingLegacyHash := legacySourceHash(t, existingRaw)
	if incomingLegacyHash := legacySourceHash(t, incomingRaw); incomingLegacyHash != existingLegacyHash {
		t.Fatalf("legacy hashes differ: existing=%q incoming=%q", existingLegacyHash, incomingLegacyHash)
	}
	writeStoredSourceHash(t, path, ctx, "legacy-collision", existingLegacyHash)

	batch := beginImport(t, store, ctx)
	defer func() { _ = batch.Rollback() }()
	_, err := batch.Add(ctx, sourceItem("legacy-collision", 1, string(incomingRaw)))
	if !errors.Is(err, dao.ErrTraceConflict) {
		t.Fatalf("Add(legacy collision) error = %v, want %v", err, dao.ErrTraceConflict)
	}
}

func openTestStore(t *testing.T) *dao.Store {
	t.Helper()
	return openTestStoreAt(t, filepath.Join(t.TempDir(), "state.db"))
}

func openTestStoreAt(t *testing.T, path string) *dao.Store {
	t.Helper()
	store, err := dao.Open(context.Background(), path)
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

func legacySourceHash(t *testing.T, raw []byte) string {
	t.Helper()
	var source any
	if err := json.Unmarshal(raw, &source); err != nil {
		t.Fatalf("json.Unmarshal(legacy source) error = %v", err)
	}
	canonical, err := json.Marshal(source)
	if err != nil {
		t.Fatalf("json.Marshal(legacy source) error = %v", err)
	}
	hash := sha256.Sum256(canonical)
	return hex.EncodeToString(hash[:])
}

func writeStoredSourceHash(t *testing.T, path string, ctx context.Context, traceID, sourceHash string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open(hash update) error = %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close(hash update DB) error = %v", err)
		}
	}()
	if _, err := db.ExecContext(
		ctx,
		"UPDATE items SET source_hash = ? WHERE task_id = ? AND trace_id = ?",
		sourceHash,
		"task-1",
		traceID,
	); err != nil {
		t.Fatalf("update stored source_hash error = %v", err)
	}
}

func readStoredSourceHash(t *testing.T, path string, ctx context.Context, traceID string) string {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open(read hash) error = %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close(read hash DB) error = %v", err)
		}
	}()
	var sourceHash string
	if err := db.QueryRowContext(
		ctx,
		"SELECT source_hash FROM items WHERE task_id = ? AND trace_id = ?",
		"task-1",
		traceID,
	).Scan(&sourceHash); err != nil {
		t.Fatalf("read stored source_hash error = %v", err)
	}
	return sourceHash
}
