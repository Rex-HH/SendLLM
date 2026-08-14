package service

import (
	"context"
	"encoding/json"
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

func TestRunner_RunDrainsPeerWhenProgressRacesWithCallerCancellation(t *testing.T) {
	store := openRunnerInternalStore(t)
	finishedItem := runnerInternalItem("drain-finished", dao.ItemPending)
	peerItem := runnerInternalItem("drain-peer", dao.ItemPending)
	peerItem.InputIndex = 2
	seedRunnerInternalItems(t, store, finishedItem, peerItem)

	peerStarted := make(chan struct{})
	releasePeer := make(chan struct{})
	peerCanceled := make(chan error, 1)
	peerExited := make(chan struct{})
	response := dto.CompletionResponse{
		Content:     []byte(`{"is_attack":false,"case_type":"typical","explanation":"内容没有攻击或规避安全控制的意图"}`),
		RawResponse: []byte(`{"synthetic":"response"}`),
	}
	runner := newRunnerInternal(t, store, runnerCompleterFunc(
		func(ctx context.Context, request dto.CompletionRequest) (dto.CompletionResponse, error) {
			switch runnerInternalTraceID(request) {
			case "drain-finished":
				<-peerStarted
				return response, nil
			case "drain-peer":
				close(peerStarted)
				defer close(peerExited)
				select {
				case <-ctx.Done():
					peerCanceled <- context.Cause(ctx)
					return dto.CompletionResponse{}, ctx.Err()
				case <-releasePeer:
					return response, nil
				}
			default:
				return dto.CompletionResponse{}, errors.New("unexpected trace_id")
			}
		},
	))
	runner.cfg.ShutdownTimeout = 500 * time.Millisecond

	progressQueryStarted := make(chan struct{})
	releaseProgressQuery := make(chan struct{})
	progressReported := make(chan struct{}, 1)
	countsCalls := 0
	runner.store = &runnerBarrierStore{
		runnerStore: store,
		beforeCounts: func(context.Context) {
			countsCalls++
			if countsCalls != 2 {
				return
			}
			close(progressQueryStarted)
			<-releaseProgressQuery
		},
	}
	runner.cfg.OnProgress = func(Summary) {
		select {
		case progressReported <- struct{}{}:
		default:
		}
	}

	callerCause := errors.New("caller canceled during progress")
	ctx, cancel := context.WithCancelCause(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := runner.Run(ctx)
		done <- err
	}()

	select {
	case <-progressQueryStarted:
	case <-time.After(time.Second):
		close(releasePeer)
		t.Fatal("Run() did not reach completed-result progress query")
	}
	cancel(callerCause)
	close(releaseProgressQuery)
	select {
	case cause := <-peerCanceled:
		close(releasePeer)
		<-done
		t.Fatalf("peer canceled before graceful drain: %v", cause)
	case <-progressReported:
	case <-time.After(100 * time.Millisecond):
	}
	close(releasePeer)

	select {
	case err := <-done:
		if !errors.Is(err, callerCause) {
			t.Fatalf("Run() error = %v, want caller cause %v", err, callerCause)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not finish after graceful drain")
	}
	select {
	case <-peerExited:
	default:
		t.Fatal("peer worker did not exit before Run returned")
	}
	select {
	case cause := <-peerCanceled:
		t.Fatalf("peer cancellation cause = %v, want graceful completion", cause)
	default:
	}
	counts, err := store.Counts(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("Counts() error = %v", err)
	}
	if counts.Succeeded != 2 || counts.Processing != 0 {
		t.Fatalf("Counts() = %+v, want two succeeded and no processing items", counts)
	}
}

func TestRunner_RunBoundsDrainWhenProgressQueryBlocksAfterCallerCancellation(t *testing.T) {
	store := openRunnerInternalStore(t)
	finishedItem := runnerInternalItem("blocked-progress-finished", dao.ItemPending)
	peerItem := runnerInternalItem("blocked-progress-peer", dao.ItemPending)
	peerItem.InputIndex = 2
	seedRunnerInternalItems(t, store, finishedItem, peerItem)

	peerStarted := make(chan struct{})
	peerCanceled := make(chan error, 1)
	response := dto.CompletionResponse{
		Content:     []byte(`{"is_attack":false,"case_type":"typical","explanation":"内容没有攻击或规避安全控制的意图"}`),
		RawResponse: []byte(`{"synthetic":"response"}`),
	}
	runner := newRunnerInternal(t, store, runnerCompleterFunc(
		func(ctx context.Context, request dto.CompletionRequest) (dto.CompletionResponse, error) {
			switch runnerInternalTraceID(request) {
			case "blocked-progress-finished":
				<-peerStarted
				return response, nil
			case "blocked-progress-peer":
				close(peerStarted)
				<-ctx.Done()
				peerCanceled <- context.Cause(ctx)
				return dto.CompletionResponse{}, ctx.Err()
			default:
				return dto.CompletionResponse{}, errors.New("unexpected trace_id")
			}
		},
	))
	runner.cfg.ShutdownTimeout = 80 * time.Millisecond

	progressQueryStarted := make(chan struct{})
	releaseProgressQuery := make(chan struct{})
	countsCalls := 0
	runner.store = &runnerBarrierStore{
		runnerStore: store,
		beforeCounts: func(ctx context.Context) {
			countsCalls++
			if countsCalls != 2 {
				return
			}
			close(progressQueryStarted)
			select {
			case <-ctx.Done():
			case <-releaseProgressQuery:
			}
		},
	}

	callerCause := errors.New("caller canceled while progress query blocked")
	ctx, cancel := context.WithCancelCause(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := runner.Run(ctx)
		done <- err
	}()

	select {
	case <-progressQueryStarted:
	case <-time.After(time.Second):
		close(releaseProgressQuery)
		t.Fatal("Run() did not reach blocked progress query")
	}
	cancel(callerCause)
	select {
	case err := <-done:
		if !errors.Is(err, callerCause) {
			t.Fatalf("Run() error = %v, want caller cause %v", err, callerCause)
		}
	case <-time.After(400 * time.Millisecond):
		close(releaseProgressQuery)
		<-done
		t.Fatal("Run() did not honor shutdown_timeout while progress query was blocked")
	}
	select {
	case cause := <-peerCanceled:
		if !errors.Is(cause, callerCause) {
			t.Fatalf("peer cancellation cause = %v, want caller cause %v", cause, callerCause)
		}
	default:
		t.Fatal("peer worker was not canceled during bounded drain")
	}
}

type runnerCompleterFunc func(context.Context, dto.CompletionRequest) (dto.CompletionResponse, error)

type runnerBarrierStore struct {
	runnerStore
	beforeClaim      func(context.Context)
	beforeCounts     func(context.Context)
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

func (s *runnerBarrierStore) Counts(ctx context.Context, taskID string) (dao.Counts, error) {
	if s.beforeCounts != nil {
		s.beforeCounts(ctx)
	}
	return s.runnerStore.Counts(ctx, taskID)
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
		ShutdownTimeout:      50 * time.Millisecond,
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

func runnerInternalTraceID(request dto.CompletionRequest) string {
	if len(request.Messages) == 0 {
		return ""
	}
	var input struct {
		TraceID string `json:"trace_id"`
	}
	if err := json.Unmarshal([]byte(request.Messages[len(request.Messages)-1].Content), &input); err != nil {
		return ""
	}
	return input.TraceID
}
