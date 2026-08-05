package dao_test

import (
	"context"
	"errors"
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
	if err := store.MarkSucceeded(ctx, "task-1", "success", attempt, []byte(`{"label":"safe"}`)); err != nil {
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
