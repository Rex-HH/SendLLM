package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"sendllm/internal/dao"
	"sendllm/internal/dto"
	"sendllm/internal/lib/limiter"
	"sendllm/internal/service"
)

func TestRunner_DoesNotRepeatSucceededItems(t *testing.T) {
	store := openTestStore(t)
	seedRunnerItems(t, store,
		runnerItem("pending-id", 1, dao.ItemPending),
		runnerItem("succeeded-id", 2, dao.ItemSucceeded),
	)
	fake := newFakeCompleter(func(ctx context.Context, req dto.CompletionRequest) (dto.CompletionResponse, error) {
		return completion(validSafe), nil
	})
	runner := newTestRunner(t, store, fake, 2, 2, 1)

	summary, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if fake.Calls("succeeded-id") != 0 {
		t.Errorf("succeeded item calls = %d, want 0", fake.Calls("succeeded-id"))
	}
	if summary.Succeeded != 2 {
		t.Errorf("Succeeded = %d, want 2", summary.Succeeded)
	}
}

func TestRunner_BoundsConcurrencyAndUsesConfiguredSchema(t *testing.T) {
	store := openTestStore(t)
	items := make([]dao.Item, 0, 6)
	for index := 0; index < 6; index++ {
		items = append(items, runnerItem("item-"+string(rune('a'+index)), int64(index+1), dao.ItemPending))
	}
	seedRunnerItems(t, store, items...)
	fake := newFakeCompleter(func(ctx context.Context, req dto.CompletionRequest) (dto.CompletionResponse, error) {
		if !bytes.Equal(req.Schema, []byte(validatorSchema)) {
			t.Errorf("CompletionRequest.Schema differs from Validator schema")
		}
		select {
		case <-ctx.Done():
			return dto.CompletionResponse{}, ctx.Err()
		case <-time.After(10 * time.Millisecond):
			return completion(validSafe), nil
		}
	})
	runner := newTestRunner(t, store, fake, 2, 1, 0)

	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := fake.MaxActive(); got != 2 {
		t.Errorf("max active calls = %d, want 2", got)
	}
}

func TestRunner_UsesConfiguredModeForClassificationAndRepair(t *testing.T) {
	for _, mode := range []string{"json_schema", "json_object", "prompt_only"} {
		t.Run(mode, func(t *testing.T) {
			store := openTestStore(t)
			seedRunnerItems(t, store, runnerItem("mode-id", 1, dao.ItemPending))
			seen := make(map[string]string)
			fake := newFakeCompleter(
				func(ctx context.Context, req dto.CompletionRequest) (dto.CompletionResponse, error) {
					key := requestKey(req)
					seen[key] = req.Mode
					if key == "repair" {
						return completion(validSafe), nil
					}
					return completion("invalid"), nil
				},
			)
			runner := newTestRunnerWithMode(t, store, fake, 1, 1, 1, mode, nil)

			if _, err := runner.Run(context.Background()); err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if seen["mode-id"] != mode || seen["repair"] != mode {
				t.Errorf("request modes = %+v, want classification and repair %q", seen, mode)
			}
		})
	}
}

func TestNewRunner_RejectsInvalidMode(t *testing.T) {
	store := openTestStore(t)
	validator, err := service.NewValidator(
		[]byte(validatorSchema),
		map[string]string{"jailbreak": "test"},
		10,
		70,
	)
	if err != nil {
		t.Fatalf("NewValidator() error = %v", err)
	}
	requestLimiter, err := limiter.New(limiter.Config{Concurrency: 1})
	if err != nil {
		t.Fatalf("limiter.New() error = %v", err)
	}
	fake := newFakeCompleter(func(ctx context.Context, req dto.CompletionRequest) (dto.CompletionResponse, error) {
		return completion(validSafe), nil
	})

	for _, mode := range []string{"", "unsupported"} {
		t.Run(mode, func(t *testing.T) {
			_, err := service.NewRunner(service.RunnerConfig{
				TaskID:             "task-1",
				SystemPrompt:       []byte("synthetic system prompt"),
				Scene:              "auto",
				Schema:             []byte(validatorSchema),
				Mode:               mode,
				MaxOutputTokens:    100,
				RequestMaxAttempts: 1,
				ShutdownTimeout:    time.Second,
				Store:              store,
				Completer:          fake,
				Validator:          validator,
				Limiter:            requestLimiter,
			})
			if err == nil {
				t.Fatalf("NewRunner(Mode=%q) error = nil, want invalid mode error", mode)
			}
		})
	}
}

func TestRunner_RetriesProviderFailures(t *testing.T) {
	tests := []struct {
		name string
		kind dto.ProviderErrorKind
	}{
		{name: "rate limited", kind: dto.ProviderRateLimited},
		{name: "server error", kind: dto.ProviderServer},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := openTestStore(t)
			seedRunnerItems(t, store, runnerItem("retry-id", 1, dao.ItemPending))
			var calls int
			fake := newFakeCompleter(func(ctx context.Context, req dto.CompletionRequest) (dto.CompletionResponse, error) {
				calls++
				if calls == 1 {
					return dto.CompletionResponse{}, &dto.ProviderError{
						Kind:       test.kind,
						StatusCode: 500,
						RetryAfter: time.Millisecond,
					}
				}
				return completion(validSafe), nil
			})
			runner := newTestRunner(t, store, fake, 1, 2, 0)

			summary, err := runner.Run(context.Background())
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if fake.Calls("retry-id") != 2 || summary.Succeeded != 1 {
				t.Fatalf("calls = %d, summary = %+v, want two calls and success", fake.Calls("retry-id"), summary)
			}
		})
	}
}

func TestRunner_RateLimitCooldownIsShared(t *testing.T) {
	store := openTestStore(t)
	seedRunnerItems(t, store,
		runnerItem("cooldown-a", 1, dao.ItemPending),
		runnerItem("cooldown-b", 2, dao.ItemPending),
		runnerItem("cooldown-c", 3, dao.ItemPending),
	)
	var mu sync.Mutex
	started := make(map[string]time.Time)
	aCalls := 0
	fake := newFakeCompleter(func(ctx context.Context, req dto.CompletionRequest) (dto.CompletionResponse, error) {
		key := requestKey(req)
		mu.Lock()
		if _, ok := started[key]; !ok {
			started[key] = time.Now()
		}
		if key == "cooldown-a" {
			aCalls++
		}
		callNumber := aCalls
		mu.Unlock()
		if key == "cooldown-a" && callNumber == 1 {
			return dto.CompletionResponse{}, &dto.ProviderError{
				Kind:       dto.ProviderRateLimited,
				StatusCode: 429,
				RetryAfter: 40 * time.Millisecond,
			}
		}
		if key == "cooldown-b" {
			select {
			case <-ctx.Done():
				return dto.CompletionResponse{}, ctx.Err()
			case <-time.After(10 * time.Millisecond):
			}
		}
		return completion(validSafe), nil
	})
	runner := newTestRunner(t, store, fake, 2, 2, 0)

	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	mu.Lock()
	delay := started["cooldown-c"].Sub(started["cooldown-a"])
	mu.Unlock()
	if delay < 25*time.Millisecond {
		t.Errorf("third request delay = %v, want shared cooldown", delay)
	}
}

func TestRunner_FinalRateLimitStillStartsSharedCooldown(t *testing.T) {
	store := openTestStore(t)
	seedRunnerItems(t, store,
		runnerItem("final-cooldown-a", 1, dao.ItemPending),
		runnerItem("final-cooldown-b", 2, dao.ItemPending),
		runnerItem("final-cooldown-c", 3, dao.ItemPending),
	)
	var mu sync.Mutex
	started := make(map[string]time.Time)
	fake := newFakeCompleter(func(ctx context.Context, req dto.CompletionRequest) (dto.CompletionResponse, error) {
		key := requestKey(req)
		mu.Lock()
		started[key] = time.Now()
		mu.Unlock()
		switch key {
		case "final-cooldown-a":
			return dto.CompletionResponse{}, &dto.ProviderError{
				Kind:       dto.ProviderRateLimited,
				StatusCode: 429,
				RetryAfter: 40 * time.Millisecond,
			}
		case "final-cooldown-b":
			select {
			case <-ctx.Done():
				return dto.CompletionResponse{}, ctx.Err()
			case <-time.After(10 * time.Millisecond):
			}
		}
		return completion(validSafe), nil
	})
	runner := newTestRunner(t, store, fake, 2, 1, 0)

	summary, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if summary.Failed != 1 || summary.Succeeded != 2 {
		t.Fatalf("Summary = %+v, want one failed and two succeeded", summary)
	}
	mu.Lock()
	delay := started["final-cooldown-c"].Sub(started["final-cooldown-a"])
	mu.Unlock()
	if delay < 25*time.Millisecond {
		t.Errorf("third request delay = %v, want final 429 shared cooldown", delay)
	}
}

func TestRunner_ProgressCountsInFlightAsPending(t *testing.T) {
	store := openTestStore(t)
	seedRunnerItems(t, store,
		runnerItem("progress-a", 1, dao.ItemPending),
		runnerItem("progress-b", 2, dao.ItemPending),
	)
	release := make(chan struct{})
	fake := newFakeCompleter(func(ctx context.Context, req dto.CompletionRequest) (dto.CompletionResponse, error) {
		if requestKey(req) == "progress-b" {
			select {
			case <-ctx.Done():
				return dto.CompletionResponse{}, ctx.Err()
			case <-release:
			}
		}
		return completion(validSafe), nil
	})
	progress := make(chan service.Summary, 1)
	var reportOnce sync.Once
	runner := newTestRunnerWithProgress(t, store, fake, 2, 1, 0, func(summary service.Summary) {
		reportOnce.Do(func() { progress <- summary })
	})
	done := make(chan error, 1)
	go func() {
		_, err := runner.Run(context.Background())
		done <- err
	}()

	summary := <-progress
	if summary.Pending != 1 || summary.Succeeded != 1 {
		t.Errorf("progress = %+v, want one succeeded and one in-flight pending", summary)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRunner_AuthenticationFailureStopsTask(t *testing.T) {
	store := openTestStore(t)
	seedRunnerItems(t, store, runnerItem("auth-id", 1, dao.ItemPending))
	fake := newFakeCompleter(func(ctx context.Context, req dto.CompletionRequest) (dto.CompletionResponse, error) {
		return dto.CompletionResponse{}, &dto.ProviderError{
			Kind:       dto.ProviderAuthentication,
			StatusCode: 401,
		}
	})
	runner := newTestRunner(t, store, fake, 1, 2, 0)

	if _, err := runner.Run(context.Background()); err == nil {
		t.Fatal("Run() error = nil, want task-level authentication error")
	}
}

func TestRunner_TaskLevelFailureCancelsWorkersWithCause(t *testing.T) {
	store := openTestStore(t)
	seedRunnerItems(t, store,
		runnerItem("auth-cause", 1, dao.ItemPending),
		runnerItem("auth-peer", 2, dao.ItemPending),
		runnerItem("auth-late", 3, dao.ItemPending),
	)
	peerStarted := make(chan struct{})
	peerCause := make(chan error, 1)
	fake := newFakeCompleter(func(ctx context.Context, req dto.CompletionRequest) (dto.CompletionResponse, error) {
		switch requestKey(req) {
		case "auth-cause":
			select {
			case <-ctx.Done():
				return dto.CompletionResponse{}, ctx.Err()
			case <-peerStarted:
			}
			return dto.CompletionResponse{}, &dto.ProviderError{
				Kind:       dto.ProviderAuthentication,
				StatusCode: 401,
			}
		case "auth-peer":
			close(peerStarted)
			<-ctx.Done()
			peerCause <- context.Cause(ctx)
			return dto.CompletionResponse{}, ctx.Err()
		case "auth-late":
			t.Error("model called after task-level authentication failure")
		}
		return completion(validSafe), nil
	})
	runner := newTestRunner(t, store, fake, 2, 2, 0)

	_, err := runner.Run(context.Background())
	var providerErr *dto.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Kind != dto.ProviderAuthentication {
		t.Fatalf("Run() error = %v, want authentication ProviderError", err)
	}
	if cause := <-peerCause; !errors.As(cause, &providerErr) || providerErr.Kind != dto.ProviderAuthentication {
		t.Fatalf("peer cancellation cause = %v, want authentication ProviderError", cause)
	}
	if got := fake.Calls("auth-late"); got != 0 {
		t.Errorf("late item calls = %d, want 0", got)
	}
}

func TestRunner_RepairsInvalidJSONWithoutSourcePayload(t *testing.T) {
	store := openTestStore(t)
	item := runnerItem("repair-id", 1, dao.ItemPending)
	item.Prompt = "secret prompt"
	item.Response = "secret response"
	seedRunnerItems(t, store, item)
	fake := newFakeCompleter(func(ctx context.Context, req dto.CompletionRequest) (dto.CompletionResponse, error) {
		if requestKey(req) != "repair" {
			return completion("not-json"), nil
		}
		encoded, err := json.Marshal(req.Messages)
		if err != nil {
			t.Errorf("Marshal(messages) error = %v", err)
		}
		if bytes.Contains(encoded, []byte("secret prompt")) || bytes.Contains(encoded, []byte("secret response")) {
			t.Error("repair request contains source payload")
		}
		return completion(validSafe), nil
	})
	runner := newTestRunner(t, store, fake, 1, 2, 1)

	summary, err := runner.Run(context.Background())
	if err != nil || summary.Succeeded != 1 || fake.Calls("repair") != 1 {
		t.Fatalf("Run() = (%+v, %v), repair calls = %d, want success after one repair", summary, err, fake.Calls("repair"))
	}
}

func TestRunner_ReclassifiesAfterRepairExhaustion(t *testing.T) {
	store := openTestStore(t)
	seedRunnerItems(t, store, runnerItem("format-id", 1, dao.ItemPending))
	var classificationCalls int
	fake := newFakeCompleter(func(ctx context.Context, req dto.CompletionRequest) (dto.CompletionResponse, error) {
		if requestKey(req) == "repair" {
			return completion("still-invalid"), nil
		}
		classificationCalls++
		if classificationCalls == 1 {
			return completion("invalid"), nil
		}
		return completion(validSafe), nil
	})
	runner := newTestRunner(t, store, fake, 1, 2, 1)

	summary, err := runner.Run(context.Background())
	if err != nil || summary.Succeeded != 1 || fake.Calls("format-id") != 2 {
		t.Fatalf(
			"Run() = (%+v, %v), classification calls = %d, want reclassification success",
			summary,
			err,
			fake.Calls("format-id"),
		)
	}
}

func TestRunner_MarksFinalFailure(t *testing.T) {
	store := openTestStore(t)
	seedRunnerItems(t, store, runnerItem("failed-id", 1, dao.ItemPending))
	fake := newFakeCompleter(func(ctx context.Context, req dto.CompletionRequest) (dto.CompletionResponse, error) {
		return dto.CompletionResponse{}, &dto.ProviderError{Kind: dto.ProviderContentRejected, StatusCode: 400}
	})
	runner := newTestRunner(t, store, fake, 1, 1, 0)

	summary, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if summary.Failed != 1 || summary.Succeeded != 0 {
		t.Fatalf("Summary = %+v, want one failed", summary)
	}
}

func TestRunner_CancellationStopsWorkers(t *testing.T) {
	store := openTestStore(t)
	seedRunnerItems(t, store,
		runnerItem("cancel-a", 1, dao.ItemPending),
		runnerItem("cancel-b", 2, dao.ItemPending),
	)
	fake := newFakeCompleter(func(ctx context.Context, req dto.CompletionRequest) (dto.CompletionResponse, error) {
		<-ctx.Done()
		return dto.CompletionResponse{}, ctx.Err()
	})
	runner := newTestRunner(t, store, fake, 2, 1, 0)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := runner.Run(ctx)
		done <- err
	}()
	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop all workers")
	}
}

func TestRunner_RecoversLegacyProcessing(t *testing.T) {
	store := openTestStore(t)
	seedRunnerItems(t, store, runnerItem("legacy-id", 1, dao.ItemProcessing))
	fake := newFakeCompleter(func(ctx context.Context, req dto.CompletionRequest) (dto.CompletionResponse, error) {
		return completion(validSafe), nil
	})
	runner := newTestRunner(t, store, fake, 1, 1, 0)

	summary, err := runner.Run(context.Background())
	if err != nil || summary.Succeeded != 1 || fake.Calls("legacy-id") != 1 {
		t.Fatalf("Run() = (%+v, %v), calls = %d, want recovered success", summary, err, fake.Calls("legacy-id"))
	}
}

type fakeCompleter struct {
	mu        sync.Mutex
	respond   func(context.Context, dto.CompletionRequest) (dto.CompletionResponse, error)
	calls     map[string]int
	active    int
	maxActive int
}

func newFakeCompleter(
	respond func(context.Context, dto.CompletionRequest) (dto.CompletionResponse, error),
) *fakeCompleter {
	return &fakeCompleter{respond: respond, calls: make(map[string]int)}
}

func (f *fakeCompleter) Complete(ctx context.Context, req dto.CompletionRequest) (dto.CompletionResponse, error) {
	key := requestKey(req)
	f.mu.Lock()
	f.calls[key]++
	f.active++
	if f.active > f.maxActive {
		f.maxActive = f.active
	}
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.active--
		f.mu.Unlock()
	}()
	return f.respond(ctx, req)
}

func (f *fakeCompleter) Calls(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[key]
}

func (f *fakeCompleter) MaxActive() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.maxActive
}

func requestKey(req dto.CompletionRequest) string {
	if len(req.Messages) == 0 {
		return ""
	}
	var input map[string]json.RawMessage
	if err := json.Unmarshal([]byte(req.Messages[len(req.Messages)-1].Content), &input); err != nil {
		return ""
	}
	if _, ok := input["invalid_response"]; ok {
		return "repair"
	}
	var traceID string
	_ = json.Unmarshal(input["trace_id"], &traceID)
	return traceID
}

func newTestRunner(
	t *testing.T,
	store *dao.Store,
	completer service.Completer,
	concurrency int,
	requestAttempts int,
	repairAttempts int,
) *service.Runner {
	return newTestRunnerWithMode(
		t,
		store,
		completer,
		concurrency,
		requestAttempts,
		repairAttempts,
		"json_schema",
		nil,
	)
}

func newTestRunnerWithProgress(
	t *testing.T,
	store *dao.Store,
	completer service.Completer,
	concurrency int,
	requestAttempts int,
	repairAttempts int,
	onProgress func(service.Summary),
) *service.Runner {
	return newTestRunnerWithMode(
		t,
		store,
		completer,
		concurrency,
		requestAttempts,
		repairAttempts,
		"json_schema",
		onProgress,
	)
}

func newTestRunnerWithMode(
	t *testing.T,
	store *dao.Store,
	completer service.Completer,
	concurrency int,
	requestAttempts int,
	repairAttempts int,
	mode string,
	onProgress func(service.Summary),
) *service.Runner {
	t.Helper()
	validator, err := service.NewValidator(
		[]byte(validatorSchema),
		map[string]string{"jailbreak": "test"},
		10,
		70,
	)
	if err != nil {
		t.Fatalf("NewValidator() error = %v", err)
	}
	requestLimiter, err := limiter.New(limiter.Config{Concurrency: concurrency})
	if err != nil {
		t.Fatalf("limiter.New() error = %v", err)
	}
	runner, err := service.NewRunner(service.RunnerConfig{
		TaskID:               "task-1",
		SystemPrompt:         []byte("synthetic system prompt"),
		Scene:                "auto",
		Schema:               []byte(validatorSchema),
		Mode:                 mode,
		MaxOutputTokens:      100,
		RequestMaxAttempts:   requestAttempts,
		FormatRepairAttempts: repairAttempts,
		ShutdownTimeout:      50 * time.Millisecond,
		Store:                store,
		Completer:            completer,
		Validator:            validator,
		Limiter:              requestLimiter,
		RetryPolicy: service.RetryPolicy{
			MaxAttempts:    requestAttempts,
			InitialBackoff: time.Millisecond,
			MaxBackoff:     time.Millisecond,
		},
		Jitter:     func(delay time.Duration) time.Duration { return delay },
		OnProgress: onProgress,
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	return runner
}

func seedRunnerItems(t *testing.T, store *dao.Store, items ...dao.Item) {
	t.Helper()
	ctx := context.Background()
	ensureTask(t, store, ctx)
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

func runnerItem(traceID string, inputIndex int64, state dao.ItemState) dao.Item {
	return dao.Item{
		TaskID:     "task-1",
		TraceID:    traceID,
		InputIndex: inputIndex,
		RawJSON:    []byte(`{"trace_id":"` + traceID + `","prompt":"synthetic"}`),
		Prompt:     "synthetic",
		State:      state,
	}
}

func completion(content string) dto.CompletionResponse {
	return dto.CompletionResponse{
		Content:     []byte(content),
		RawResponse: []byte(`{"synthetic":"response"}`),
		Usage:       dto.Usage{PromptTokens: 5, CompletionTokens: 7, TotalTokens: 12},
	}
}
