package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"sendllm/internal/dto"
	"sendllm/internal/service"
)

// TestSafetyReviewCallerSuccess 验证成功调用返回模型结果并持久化尝试。
func TestSafetyReviewCallerSuccess(t *testing.T) {
	completer := safetyReviewSuccessCompleter()
	registry, store := safetyReviewCallerDependencies(t, completer)
	caller, err := service.NewSafetyReviewCaller(safetyReviewCallerConfig(registry, store))
	if err != nil {
		t.Fatalf("NewSafetyReviewCaller() error = %v", err)
	}

	result, err := caller.Call(context.Background(), safetyReviewCallRequest(dto.SafetyReviewJudgeA))
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if string(result.Content) != `{"ok":true}` || result.Profile != "primary" || result.FallbackIndex != 0 {
		t.Fatalf("result = %+v, want primary success", result)
	}
	if result.Attempts != 1 || len(store.attempts) != 1 {
		t.Fatalf("attempts = %d/%d, want 1/1", result.Attempts, len(store.attempts))
	}
	recorded := store.attempts[0]
	if recorded.TaskID != "task-1" || recorded.TraceID != "trace-1" || recorded.StageKey != "judge:a" {
		t.Fatalf("recorded identity = %+v, want task/stage identity", recorded)
	}
}

// TestSafetyReviewCallerRejectsExcessiveTransientAttempts 验证单个 Profile 最多三次瞬态尝试。
func TestSafetyReviewCallerRejectsExcessiveTransientAttempts(t *testing.T) {
	completer := safetyReviewSuccessCompleter()
	registry, store := safetyReviewCallerDependencies(t, completer)
	cfg := safetyReviewCallerConfig(registry, store)
	cfg.Retry.MaxAttempts = 4
	if _, err := service.NewSafetyReviewCaller(cfg); err == nil {
		t.Fatal("NewSafetyReviewCaller() accepted more than three transient attempts")
	}
}

// TestSafetyReviewCallerTransientRetry 验证网络错误按策略重试并记录每次尝试。
func TestSafetyReviewCallerTransientRetry(t *testing.T) {
	completer := &safetyReviewTestCompleter{
		script: []safetyReviewOutcome{
			{err: &dto.ProviderError{Kind: dto.ProviderNetwork}},
			{err: &dto.ProviderError{Kind: dto.ProviderTimeout}},
			{response: dto.CompletionResponse{Content: []byte(`{"ok":true}`)}},
		},
	}
	registry, store := safetyReviewCallerDependencies(t, completer)
	cfg := safetyReviewCallerConfig(registry, store)
	cfg.Retry.MaxAttempts = 3
	caller, err := service.NewSafetyReviewCaller(cfg)
	if err != nil {
		t.Fatalf("NewSafetyReviewCaller() error = %v", err)
	}

	result, err := caller.Call(context.Background(), safetyReviewCallRequest(dto.SafetyReviewJudgeA))
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if result.Attempts != 3 || len(store.attempts) != 3 {
		t.Fatalf("attempts = %d/%d, want 3/3", result.Attempts, len(store.attempts))
	}
}

// TestSafetyReviewCallerRetryAfter 验证 429 的 Retry-After 被传递给 Sleeper。
func TestSafetyReviewCallerRetryAfter(t *testing.T) {
	completer := &safetyReviewTestCompleter{
		script: []safetyReviewOutcome{
			{err: &dto.ProviderError{Kind: dto.ProviderRateLimited, RetryAfter: 2 * time.Second}},
			{response: dto.CompletionResponse{Content: []byte(`{"ok":true}`)}},
		},
	}
	registry, store := safetyReviewCallerDependencies(t, completer)
	cfg := safetyReviewCallerConfig(registry, store)
	var slept []time.Duration
	cfg.Sleeper = func(ctx context.Context, d time.Duration) error {
		slept = append(slept, d)
		return nil
	}
	caller, err := service.NewSafetyReviewCaller(cfg)
	if err != nil {
		t.Fatalf("NewSafetyReviewCaller() error = %v", err)
	}

	if _, err := caller.Call(context.Background(), safetyReviewCallRequest(dto.SafetyReviewJudgeA)); err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if len(slept) != 1 || slept[0] != 2*time.Second {
		t.Fatalf("slept = %v, want [2s]", slept)
	}
}

// TestSafetyReviewCallerHTTPTransientCategories 验证 408 与 5xx 均按瞬态错误重试。
func TestSafetyReviewCallerHTTPTransientCategories(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "408", err: &dto.ProviderError{Kind: dto.ProviderTimeout, StatusCode: 408}},
		{name: "5xx", err: &dto.ProviderError{Kind: dto.ProviderServer, StatusCode: 503}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			completer := &safetyReviewTestCompleter{
				script: []safetyReviewOutcome{
					{err: test.err},
					{response: dto.CompletionResponse{Content: []byte(`{"ok":true}`)}},
				},
			}
			registry, store := safetyReviewCallerDependencies(t, completer)
			caller, err := service.NewSafetyReviewCaller(safetyReviewCallerConfig(registry, store))
			if err != nil {
				t.Fatalf("NewSafetyReviewCaller() error = %v", err)
			}
			result, err := caller.Call(context.Background(), safetyReviewCallRequest(dto.SafetyReviewJudgeA))
			if err != nil {
				t.Fatalf("Call() error = %v", err)
			}
			if result.Attempts != 2 || len(store.attempts) != 2 {
				t.Fatalf("attempts = %d/%d, want 2/2", result.Attempts, len(store.attempts))
			}
		})
	}
}

// TestSafetyReviewCallerRepair 验证非法 JSON 后执行一次修复调用。
func TestSafetyReviewCallerRepair(t *testing.T) {
	completer := &safetyReviewTestCompleter{
		script: []safetyReviewOutcome{
			{response: dto.CompletionResponse{Content: []byte(`not-json`)}},
			{response: dto.CompletionResponse{Content: []byte(`{"ok":true}`)}},
		},
	}
	registry, store := safetyReviewCallerDependencies(t, completer)
	cfg := safetyReviewCallerConfig(registry, store)
	cfg.Validator = func(raw []byte) error {
		if string(raw) != `{"ok":true}` {
			return &service.ValidationError{Problems: []string{"invalid JSON"}}
		}
		return nil
	}
	caller, err := service.NewSafetyReviewCaller(cfg)
	if err != nil {
		t.Fatalf("NewSafetyReviewCaller() error = %v", err)
	}

	result, err := caller.Call(context.Background(), safetyReviewCallRequest(dto.SafetyReviewJudgeA))
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if result.Attempts != 2 || len(store.attempts) != 2 {
		t.Fatalf("attempts = %d/%d, want 2/2", result.Attempts, len(store.attempts))
	}
	if len(completer.calls) != 2 {
		t.Fatalf("model calls = %d, want 2", len(completer.calls))
	}
}

// TestSafetyReviewCallerSemanticInvalid 验证语义非法输出也会触发一次修复。
func TestSafetyReviewCallerSemanticInvalid(t *testing.T) {
	completer := &safetyReviewTestCompleter{
		script: []safetyReviewOutcome{
			{response: dto.CompletionResponse{Content: []byte(`{"label":"unknown"}`)}},
			{response: dto.CompletionResponse{Content: []byte(`{"ok":true}`)}},
		},
	}
	registry, store := safetyReviewCallerDependencies(t, completer)
	cfg := safetyReviewCallerConfig(registry, store)
	cfg.Validator = func(raw []byte) error {
		if string(raw) == `{"label":"unknown"}` {
			return &service.ValidationError{Problems: []string{"label is invalid"}}
		}
		return nil
	}
	caller, err := service.NewSafetyReviewCaller(cfg)
	if err != nil {
		t.Fatalf("NewSafetyReviewCaller() error = %v", err)
	}
	result, err := caller.Call(context.Background(), safetyReviewCallRequest(dto.SafetyReviewJudgeA))
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if result.Attempts != 2 || len(store.attempts) != 2 {
		t.Fatalf("attempts = %d/%d, want 2/2", result.Attempts, len(store.attempts))
	}
}

// TestSafetyReviewCallerRequestValidatorRepair 验证带阶段上下文的请求校验器会触发修复。
func TestSafetyReviewCallerRequestValidatorRepair(t *testing.T) {
	completer := &safetyReviewTestCompleter{
		script: []safetyReviewOutcome{
			{response: dto.CompletionResponse{Content: []byte(`{"wrong":true}`)}},
			{response: dto.CompletionResponse{Content: []byte(`{"ok":true}`)}},
		},
	}
	registry, store := safetyReviewCallerDependencies(t, completer)
	cfg := safetyReviewCallerConfig(registry, store)
	cfg.RequestValidator = func(req service.SafetyReviewCallRequest, raw []byte) error {
		if req.Scene != "response" || req.Role != dto.SafetyReviewJudgeA {
			return errors.New("request context mismatch")
		}
		if string(raw) != `{"ok":true}` {
			return &service.ValidationError{Problems: []string{"invalid request result"}}
		}
		return nil
	}
	caller, err := service.NewSafetyReviewCaller(cfg)
	if err != nil {
		t.Fatalf("NewSafetyReviewCaller() error = %v", err)
	}
	request := safetyReviewCallRequest(dto.SafetyReviewJudgeA)
	request.Scene = "response"
	result, err := caller.Call(context.Background(), request)
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if result.Attempts != 2 || len(store.attempts) != 2 {
		t.Fatalf("attempts = %d/%d, want 2/2", result.Attempts, len(store.attempts))
	}
}

// TestSafetyReviewCallerRefusalReprompt 验证拒答后执行一次重提示。
func TestSafetyReviewCallerRefusalReprompt(t *testing.T) {
	completer := &safetyReviewTestCompleter{
		script: []safetyReviewOutcome{
			{response: dto.CompletionResponse{Content: []byte("I cannot help with that.")}},
			{response: dto.CompletionResponse{Content: []byte(`{"ok":true}`)}},
		},
	}
	registry, store := safetyReviewCallerDependencies(t, completer)
	cfg := safetyReviewCallerConfig(registry, store)
	cfg.RefusalPrefixes = []string{"I cannot help"}
	caller, err := service.NewSafetyReviewCaller(cfg)
	if err != nil {
		t.Fatalf("NewSafetyReviewCaller() error = %v", err)
	}

	result, err := caller.Call(context.Background(), safetyReviewCallRequest(dto.SafetyReviewJudgeA))
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if result.Attempts != 2 || len(store.attempts) != 2 {
		t.Fatalf("attempts = %d/%d, want 2/2", result.Attempts, len(store.attempts))
	}
	if len(completer.calls) != 2 {
		t.Fatalf("model calls = %d, want 2", len(completer.calls))
	}
}

// TestSafetyReviewCallerRepeatedRefusalFallsBack 验证连续拒答后切换备用模型。
func TestSafetyReviewCallerRepeatedRefusalFallsBack(t *testing.T) {
	primary := &safetyReviewTestCompleter{
		script: []safetyReviewOutcome{
			{response: dto.CompletionResponse{Content: []byte("I cannot help with that.")}},
			{response: dto.CompletionResponse{Content: []byte("I cannot help with that either.")}},
		},
	}
	fallback := safetyReviewSuccessCompleter()
	registry, store := safetyReviewFallbackDependencies(t, primary, fallback)
	cfg := safetyReviewCallerConfig(registry, store)
	cfg.RefusalPrefixes = []string{"I cannot help"}
	caller, err := service.NewSafetyReviewCaller(cfg)
	if err != nil {
		t.Fatalf("NewSafetyReviewCaller() error = %v", err)
	}
	result, err := caller.Call(context.Background(), safetyReviewCallRequest(dto.SafetyReviewJudgeA))
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if result.Profile != "fallback" || result.FallbackIndex != 1 || result.Attempts != 3 {
		t.Fatalf("result = %+v, want fallback after three attempts", result)
	}
}

// TestSafetyReviewCallerFallback 验证认证和坏模型错误会切换备用模型。
func TestSafetyReviewCallerFallback(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "authentication", err: &dto.ProviderError{Kind: dto.ProviderAuthentication}},
		{name: "bad model", err: &dto.ProviderError{Kind: dto.ProviderBadRequest}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			primary := &safetyReviewTestCompleter{script: []safetyReviewOutcome{{err: test.err}}}
			fallback := safetyReviewSuccessCompleter()
			registry, store := safetyReviewFallbackDependencies(t, primary, fallback)
			caller, err := service.NewSafetyReviewCaller(safetyReviewCallerConfig(registry, store))
			if err != nil {
				t.Fatalf("NewSafetyReviewCaller() error = %v", err)
			}

			result, err := caller.Call(context.Background(), safetyReviewCallRequest(dto.SafetyReviewJudgeA))
			if err != nil {
				t.Fatalf("Call() error = %v", err)
			}
			if result.Profile != "fallback" || result.FallbackIndex != 1 {
				t.Fatalf("result = %+v, want fallback profile/index 1", result)
			}
			if len(primary.calls) != 1 {
				t.Fatalf("primary calls = %d, want immediate profile switch", len(primary.calls))
			}
			if !result.IndependenceDegraded {
				t.Fatalf("result = %+v, want independence degraded for judge fallback", result)
			}
		})
	}
}

// TestSafetyReviewCallerContentRejectionIsTerminal 验证内容拒绝不会切换 Safe 或备用模型。
func TestSafetyReviewCallerContentRejectionIsTerminal(t *testing.T) {
	completer := &safetyReviewTestCompleter{
		script: []safetyReviewOutcome{
			{err: &dto.ProviderError{Kind: dto.ProviderContentRejected}},
		},
	}
	registry, store := safetyReviewCallerDependencies(t, completer)
	caller, err := service.NewSafetyReviewCaller(safetyReviewCallerConfig(registry, store))
	if err != nil {
		t.Fatalf("NewSafetyReviewCaller() error = %v", err)
	}
	if _, err := caller.Call(context.Background(), safetyReviewCallRequest(dto.SafetyReviewJudgeA)); err == nil {
		t.Fatal("Call() returned nil error for content rejection")
	}
	if len(completer.calls) != 1 || len(store.attempts) != 1 {
		t.Fatalf("calls/attempts = %d/%d, want 1/1", len(completer.calls), len(store.attempts))
	}
}

// TestSafetyReviewCallerFallbackExhaustion 验证备用模型耗尽时返回错误。
func TestSafetyReviewCallerFallbackExhaustion(t *testing.T) {
	primary := &safetyReviewTestCompleter{
		script: []safetyReviewOutcome{{err: &dto.ProviderError{Kind: dto.ProviderAuthentication}}},
	}
	fallback := &safetyReviewTestCompleter{
		script: []safetyReviewOutcome{{err: &dto.ProviderError{Kind: dto.ProviderAuthentication}}},
	}
	registry, store := safetyReviewFallbackDependencies(t, primary, fallback)
	caller, err := service.NewSafetyReviewCaller(safetyReviewCallerConfig(registry, store))
	if err != nil {
		t.Fatalf("NewSafetyReviewCaller() error = %v", err)
	}

	if _, err := caller.Call(context.Background(), safetyReviewCallRequest(dto.SafetyReviewJudgeA)); err == nil {
		t.Fatal("Call() returned nil error after fallback exhaustion")
	}
}

// TestSafetyReviewCallerContextCancel 验证取消会中断阻塞调用且不泄漏尝试。
func TestSafetyReviewCallerContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	completer := &safetyReviewTestCompleter{blockOn: ctx}
	registry, store := safetyReviewCallerDependencies(t, completer)
	caller, err := service.NewSafetyReviewCaller(safetyReviewCallerConfig(registry, store))
	if err != nil {
		t.Fatalf("NewSafetyReviewCaller() error = %v", err)
	}
	cancel()
	if _, err := caller.Call(ctx, safetyReviewCallRequest(dto.SafetyReviewJudgeA)); !errors.Is(err, context.Canceled) {
		t.Fatalf("Call() error = %v, want context.Canceled", err)
	}
}

// safetyReviewAttemptStore 记录 Caller 写入的每次尝试。
type safetyReviewAttemptStore struct {
	mu       sync.Mutex
	attempts []service.SafetyReviewRecordedAttempt
}

// RecordSafetyReviewAttempt 记录一次模型尝试。
func (s *safetyReviewAttemptStore) RecordSafetyReviewAttempt(
	ctx context.Context,
	attempt service.SafetyReviewRecordedAttempt,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts = append(s.attempts, attempt)
	return nil
}

// safetyReviewCallerConfig 构造默认 Caller 配置。
func safetyReviewCallerConfig(
	registry service.SafetyReviewModelRegistry,
	store *safetyReviewAttemptStore,
) service.SafetyReviewCallerConfig {
	return service.SafetyReviewCallerConfig{
		Registry: registry,
		Store:    store,
		Retry: service.RetryPolicy{
			MaxAttempts:    3,
			InitialBackoff: time.Millisecond,
			MaxBackoff:     time.Millisecond,
		},
		Clock:   time.Now,
		Sleeper: func(context.Context, time.Duration) error { return nil },
		Jitter:  func(d time.Duration) time.Duration { return d },
	}
}

// safetyReviewCallerDependencies 构造单模型 Caller 依赖。
func safetyReviewCallerDependencies(
	t *testing.T,
	completer service.Completer,
) (service.SafetyReviewModelRegistry, *safetyReviewAttemptStore) {
	t.Helper()
	registry, err := service.NewSafetyReviewModelRegistry(map[dto.SafetyReviewRole][]service.SafetyReviewModel{
		dto.SafetyReviewJudgeA: {{Profile: "primary", Family: "primary-family", Completer: completer}},
	})
	if err != nil {
		t.Fatalf("NewSafetyReviewModelRegistry() error = %v", err)
	}
	return registry, &safetyReviewAttemptStore{}
}

// safetyReviewFallbackDependencies 构造主备模型 Caller 依赖。
func safetyReviewFallbackDependencies(
	t *testing.T,
	primary service.Completer,
	fallback service.Completer,
) (service.SafetyReviewModelRegistry, *safetyReviewAttemptStore) {
	t.Helper()
	registry, err := service.NewSafetyReviewModelRegistry(map[dto.SafetyReviewRole][]service.SafetyReviewModel{
		dto.SafetyReviewJudgeA: {
			{Profile: "primary", Family: "primary-family", Completer: primary},
			{Profile: "fallback", Family: "fallback-family", Completer: fallback},
		},
	})
	if err != nil {
		t.Fatalf("NewSafetyReviewModelRegistry() error = %v", err)
	}
	return registry, &safetyReviewAttemptStore{}
}

// safetyReviewCallRequest 构造固定测试请求。
func safetyReviewCallRequest(role dto.SafetyReviewRole) service.SafetyReviewCallRequest {
	return service.SafetyReviewCallRequest{
		TaskID:   "task-1",
		TraceID:  "trace-1",
		StageKey: "judge:a",
		Role:     role,
		Messages: []dto.Message{{Role: "user", Content: "synthetic request"}},
		Schema:   []byte(`{"type":"object"}`),
		Mode:     "json_object",
	}
}
