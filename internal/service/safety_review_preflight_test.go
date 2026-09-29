package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"sendllm/internal/dto"
	"sendllm/internal/service"
)

// TestSafetyReviewPreflightProbesEveryRoleProfile 验证 preflight 覆盖全部角色链。
func TestSafetyReviewPreflightProbesEveryRoleProfile(t *testing.T) {
	completers := map[dto.SafetyReviewRole]*safetyReviewTestCompleter{
		dto.SafetyReviewJudgeA:  safetyReviewSuccessCompleter(),
		dto.SafetyReviewJudgeB:  safetyReviewSuccessCompleter(),
		dto.SafetyReviewRouter:  safetyReviewSuccessCompleter(),
		dto.SafetyReviewExpert:  safetyReviewSuccessCompleter(),
		dto.SafetyReviewArbiter: safetyReviewSuccessCompleter(),
	}
	chains := make(map[dto.SafetyReviewRole][]service.SafetyReviewModel, len(completers))
	for role, completer := range completers {
		chains[role] = []service.SafetyReviewModel{{
			Profile:   string(role),
			Family:    "family-" + string(role),
			Completer: completer,
		}}
	}
	registry, err := service.NewSafetyReviewModelRegistry(chains)
	if err != nil {
		t.Fatalf("NewSafetyReviewModelRegistry() error = %v", err)
	}

	cfg := service.SafetyReviewPreflightConfig{
		Registry: registry,
		Scene:    "prompt",
		Schema:   []byte(`{"type":"object"}`),
		OnProbe: func(event service.SafetyReviewPreflightEvent) {
			if event.State == "succeeded" {
				t.Logf("preflight role=%s profile=%s", event.Role, event.Profile)
			}
		},
	}
	if err := service.RunSafetyReviewPreflight(context.Background(), cfg); err != nil {
		t.Fatalf("RunSafetyReviewPreflight() error = %v", err)
	}
	for role, completer := range completers {
		if len(completer.calls) != 1 {
			t.Fatalf("role %s call count = %d, want 1", role, len(completer.calls))
		}
	}
}

// TestSafetyReviewPreflightUsesHarmlessSyntheticProbe 验证探测输入不携带业务载荷。
func TestSafetyReviewPreflightUsesHarmlessSyntheticProbe(t *testing.T) {
	completers := map[dto.SafetyReviewRole]*safetyReviewTestCompleter{
		dto.SafetyReviewJudgeA:  safetyReviewSuccessCompleter(),
		dto.SafetyReviewJudgeB:  safetyReviewSuccessCompleter(),
		dto.SafetyReviewRouter:  safetyReviewSuccessCompleter(),
		dto.SafetyReviewExpert:  safetyReviewSuccessCompleter(),
		dto.SafetyReviewArbiter: safetyReviewSuccessCompleter(),
	}
	chains := make(map[dto.SafetyReviewRole][]service.SafetyReviewModel, len(completers))
	for role, completer := range completers {
		chains[role] = []service.SafetyReviewModel{{
			Profile:   string(role),
			Family:    "family-" + string(role),
			Completer: completer,
		}}
	}
	registry, err := service.NewSafetyReviewModelRegistry(chains)
	if err != nil {
		t.Fatalf("NewSafetyReviewModelRegistry() error = %v", err)
	}
	cfg := service.SafetyReviewPreflightConfig{
		Registry: registry,
		Scene:    "prompt",
		Schema:   []byte(`{"type":"object"}`),
	}
	if err := service.RunSafetyReviewPreflight(context.Background(), cfg); err != nil {
		t.Fatalf("RunSafetyReviewPreflight() error = %v", err)
	}
	for role, completer := range completers {
		if len(completer.calls) != 1 {
			t.Fatalf("role %s call count = %d, want 1", role, len(completer.calls))
		}
		userContent := completer.calls[0].Messages[1].Content
		if !strings.Contains(userContent, "preflight") || !strings.Contains(userContent, "synthetic") {
			t.Fatalf("role %s probe content is not harmless synthetic: %s", role, userContent)
		}
		if strings.Contains(userContent, "actual prompt") || strings.Contains(userContent, "actual response") {
			t.Fatalf("role %s probe contains business payload markers", role)
		}
	}
}

// TestSafetyReviewPreflightFailsOnSingleProfile 验证任一 Profile 失败则整体失败。
func TestSafetyReviewPreflightFailsOnSingleProfile(t *testing.T) {
	failing := &safetyReviewTestCompleter{
		script: []safetyReviewOutcome{{err: errors.New("preflight failure")}},
	}
	registry, err := service.NewSafetyReviewModelRegistry(map[dto.SafetyReviewRole][]service.SafetyReviewModel{
		dto.SafetyReviewJudgeA: {{Profile: "primary", Family: "family", Completer: failing}},
	})
	if err != nil {
		t.Fatalf("NewSafetyReviewModelRegistry() error = %v", err)
	}
	cfg := service.SafetyReviewPreflightConfig{
		Registry: registry,
		Scene:    "prompt",
		Schema:   []byte(`{"type":"object"}`),
	}
	if err := service.RunSafetyReviewPreflight(context.Background(), cfg); err == nil {
		t.Fatal("RunSafetyReviewPreflight() returned nil error for failing profile")
	}
}

// TestSafetyReviewPreflightRetriesTransientFailure 验证 429 等瞬态失败会重试。
func TestSafetyReviewPreflightRetriesTransientFailure(t *testing.T) {
	transient := &safetyReviewTestCompleter{
		script: []safetyReviewOutcome{
			{err: &dto.ProviderError{Kind: dto.ProviderRateLimited}},
			{response: dto.CompletionResponse{Content: []byte(`{"ok":true}`)}},
		},
	}
	registry, err := service.NewSafetyReviewModelRegistry(map[dto.SafetyReviewRole][]service.SafetyReviewModel{
		dto.SafetyReviewJudgeA: {{Profile: "primary", Family: "family", Completer: transient}},
		dto.SafetyReviewJudgeB: {{
			Profile: "judge-b", Family: "family-b", Completer: safetyReviewSuccessCompleter(),
		}},
		dto.SafetyReviewRouter: {{
			Profile: "router", Family: "family-router", Completer: safetyReviewSuccessCompleter(),
		}},
		dto.SafetyReviewExpert: {{
			Profile: "expert", Family: "family-expert", Completer: safetyReviewSuccessCompleter(),
		}},
		dto.SafetyReviewArbiter: {{
			Profile: "arbiter", Family: "family-arbiter", Completer: safetyReviewSuccessCompleter(),
		}},
	})
	if err != nil {
		t.Fatalf("NewSafetyReviewModelRegistry() error = %v", err)
	}
	cfg := service.SafetyReviewPreflightConfig{
		Registry: registry,
		Scene:    "prompt",
		Schema:   []byte(`{"type":"object"}`),
		Retry: service.RetryPolicy{
			MaxAttempts:    2,
			InitialBackoff: time.Millisecond,
			MaxBackoff:     time.Millisecond,
		},
		Sleeper: func(context.Context, time.Duration) error { return nil },
		Jitter:  func(value time.Duration) time.Duration { return value },
	}
	if err := service.RunSafetyReviewPreflight(context.Background(), cfg); err != nil {
		t.Fatalf("RunSafetyReviewPreflight() error = %v", err)
	}
	if len(transient.calls) != 2 {
		t.Fatalf("transient calls = %d, want 2", len(transient.calls))
	}
}

// TestSafetyReviewPreflightRejectsMissingRole 验证缺失角色链不能被静默跳过。
func TestSafetyReviewPreflightRejectsMissingRole(t *testing.T) {
	registry, err := service.NewSafetyReviewModelRegistry(map[dto.SafetyReviewRole][]service.SafetyReviewModel{
		dto.SafetyReviewJudgeA: {{
			Profile:   "judge-a",
			Family:    "family-a",
			Completer: safetyReviewSuccessCompleter(),
		}},
	})
	if err != nil {
		t.Fatalf("NewSafetyReviewModelRegistry() error = %v", err)
	}
	cfg := service.SafetyReviewPreflightConfig{
		Registry: registry,
		Scene:    "prompt",
		Schema:   []byte(`{"type":"object"}`),
	}
	if err := service.RunSafetyReviewPreflight(context.Background(), cfg); err == nil {
		t.Fatal("RunSafetyReviewPreflight() accepted missing role chains")
	}
}
