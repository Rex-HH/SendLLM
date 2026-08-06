package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"sendllm/internal/dao"
	"sendllm/internal/dto"
	"sendllm/internal/lib/limiter"
)

func TestRunner_RunPreservesTaskCancellationDuringClaim(t *testing.T) {
	store := openRunnerInternalStore(t)
	seedRunnerInternalItems(t, store, runnerInternalItem("auth-claim", dao.ItemPending))
	secondClaim := make(chan struct{})
	providerErr := &dto.ProviderError{
		Kind:       dto.ProviderAuthentication,
		StatusCode: 401,
	}
	runner := newRunnerInternal(t, store, runnerCompleterFunc(
		func(context.Context, dto.CompletionRequest) (dto.CompletionResponse, error) {
			<-secondClaim
			return dto.CompletionResponse{}, providerErr
		},
	))
	claimCount := 0
	runner.store = &runnerBarrierStore{
		runnerStore: store,
		beforeClaim: func(ctx context.Context) {
			claimCount++
			if claimCount != 2 {
				return
			}
			close(secondClaim)
			<-ctx.Done()
		},
	}

	_, err := runner.Run(context.Background())
	var gotProviderErr *dto.ProviderError
	if !errors.As(err, &gotProviderErr) || gotProviderErr.Kind != dto.ProviderAuthentication {
		t.Fatalf("Run() error = %v, want authentication ProviderError", err)
	}
}

func TestRunner_RunPreservesCallerCancellationAfterRetryLookup(t *testing.T) {
	store := openRunnerInternalStore(t)
	seedRunnerInternalItems(t, store, runnerInternalItem("missing-retry-time", dao.ItemRetryWait))
	runner := newRunnerInternal(t, store, runnerCompleterFunc(
		func(context.Context, dto.CompletionRequest) (dto.CompletionResponse, error) {
			return dto.CompletionResponse{}, errors.New("unexpected completion call")
		},
	))
	retryLookupDone := make(chan struct{})
	releaseRetryLookup := make(chan struct{})
	runner.store = &runnerBarrierStore{
		runnerStore: store,
		afterNextRetryAt: func(context.Context) {
			close(retryLookupDone)
			<-releaseRetryLookup
		},
	}
	callerErr := errors.New("caller canceled task")
	ctx, cancel := context.WithCancelCause(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := runner.Run(ctx)
		done <- err
	}()

	<-retryLookupDone
	cancel(callerErr)
	close(releaseRetryLookup)
	if err := <-done; !errors.Is(err, callerErr) {
		t.Fatalf("Run() error = %v, want caller cause %v", err, callerErr)
	}
}

type runnerCompleterFunc func(context.Context, dto.CompletionRequest) (dto.CompletionResponse, error)

type runnerBarrierStore struct {
	runnerStore
	beforeClaim      func(context.Context)
	afterNextRetryAt func(context.Context)
}

func (f runnerCompleterFunc) Complete(
	ctx context.Context,
	request dto.CompletionRequest,
) (dto.CompletionResponse, error) {
	return f(ctx, request)
}

func (s *runnerBarrierStore) Claim(
	ctx context.Context,
	taskID string,
	limit int,
	now time.Time,
) ([]dao.Item, error) {
	if s.beforeClaim != nil {
		s.beforeClaim(ctx)
	}
	return s.runnerStore.Claim(ctx, taskID, limit, now)
}

func (s *runnerBarrierStore) NextRetryAt(
	ctx context.Context,
	taskID string,
) (time.Time, bool, error) {
	next, ok, err := s.runnerStore.NextRetryAt(ctx, taskID)
	if s.afterNextRetryAt != nil {
		s.afterNextRetryAt(ctx)
	}
	return next, ok, err
}

func newRunnerInternal(t *testing.T, store *dao.Store, completer Completer) *Runner {
	t.Helper()
	validator, err := NewValidator([]byte(`{"type":"object"}`), map[string]string{"test": "test"}, 1, 70)
	if err != nil {
		t.Fatalf("NewValidator() error = %v", err)
	}
	requestLimiter, err := limiter.New(limiter.Config{Concurrency: 2})
	if err != nil {
		t.Fatalf("limiter.New() error = %v", err)
	}
	runner, err := NewRunner(RunnerConfig{
		TaskID:               "task-1",
		SystemPrompt:         []byte("synthetic system prompt"),
		Scene:                "auto",
		Schema:               []byte(`{"type":"object"}`),
		Mode:                 "json_schema",
		MaxOutputTokens:      100,
		RequestMaxAttempts:   1,
		FormatRepairAttempts: 0,
		Store:                store,
		Completer:            completer,
		Validator:            validator,
		Limiter:              requestLimiter,
		RetryPolicy: RetryPolicy{
			MaxAttempts:    1,
			InitialBackoff: time.Millisecond,
			MaxBackoff:     time.Millisecond,
		},
		Jitter: func(delay time.Duration) time.Duration { return delay },
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	return runner
}

func openRunnerInternalStore(t *testing.T) *dao.Store {
	t.Helper()
	store, err := dao.Open(context.Background(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("dao.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	if err := store.EnsureTask(
		context.Background(),
		dao.Task{ID: "task-1", SemanticHash: "test-hash"},
	); err != nil {
		t.Fatalf("EnsureTask() error = %v", err)
	}
	return store
}

func seedRunnerInternalItems(t *testing.T, store *dao.Store, items ...dao.Item) {
	t.Helper()
	ctx := context.Background()
	batch, err := store.BeginImport(ctx, "task-1")
	if err != nil {
		t.Fatalf("BeginImport() error = %v", err)
	}
	defer func() { _ = batch.Rollback() }()
	for _, item := range items {
		if _, err := batch.Add(ctx, item); err != nil {
			t.Fatalf("Add(%q) error = %v", item.TraceID, err)
		}
	}
	if err := batch.Commit(); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
}

func runnerInternalItem(traceID string, state dao.ItemState) dao.Item {
	return dao.Item{
		TaskID:     "task-1",
		TraceID:    traceID,
		InputIndex: 1,
		RawJSON:    []byte(`{"trace_id":"` + traceID + `","prompt":"synthetic"}`),
		Prompt:     "synthetic",
		State:      state,
	}
}
