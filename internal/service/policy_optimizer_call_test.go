package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"sendllm/internal/dto"
	"sendllm/internal/service"
)

// TestPolicyOptimizerCallerRetriesAndRepairs 验证瞬态重试、修复和 fallback。
func TestPolicyOptimizerCallerRetriesAndRepairs(t *testing.T) {
	primary := &safetyReviewTestCompleter{
		script: []safetyReviewOutcome{
			{err: &dto.ProviderError{Kind: dto.ProviderRateLimited}},
			{response: dto.CompletionResponse{Content: []byte("not-json")}},
			{response: dto.CompletionResponse{Content: []byte(`{"ok":true}`)}},
		},
	}
	fallback := safetyReviewSuccessCompleter()
	registry := policyOptimizerTestRegistry{
		"local_miner": {
			{Profile: "primary", Family: "minimax", Completer: primary},
			{Profile: "fallback", Family: "qwen", Completer: fallback},
		},
	}
	recorder := &policyOptimizerTestRecorder{}
	caller, err := service.NewPolicyOptimizerCaller(service.PolicyOptimizerCallerConfig{
		Registry: registry, Recorder: recorder,
		Retry: service.RetryPolicy{MaxAttempts: 3, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond},
		Validator: func(raw []byte) error {
			if string(raw) != `{"ok":true}` {
				return errors.New("invalid json")
			}
			return nil
		},
		Sleeper: func(context.Context, time.Duration) error { return nil },
		Jitter:  func(value time.Duration) time.Duration { return value },
	})
	if err != nil {
		t.Fatalf("NewPolicyOptimizerCaller() error = %v", err)
	}
	result, err := caller.Call(context.Background(), service.PolicyOptimizerCallRequest{
		RunID: "RUN:one", IterationID: "ITER-001", Stage: "local_mining",
		SkillID: "local-error-pattern-miner", Role: "local_miner",
		Messages: []dto.Message{{Role: "user", Content: "synthetic"}},
		Schema:   []byte(`{"type":"object"}`), Mode: "json_object",
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if result.Profile != "primary" || result.Attempts != 3 {
		t.Fatalf("result = %+v", result)
	}
	if len(recorder.attempts) != 3 {
		t.Fatalf("recorded attempts = %d, want 3", len(recorder.attempts))
	}
}

type policyOptimizerTestRegistry map[string][]service.PolicyOptimizerModel

// Chain 返回角色模型链副本。
func (r policyOptimizerTestRegistry) Chain(role string) []service.PolicyOptimizerModel {
	values := r[role]
	return append([]service.PolicyOptimizerModel(nil), values...)
}

type policyOptimizerTestRecorder struct {
	attempts []service.PolicyOptimizerAttempt
}

// RecordPolicyOptimizerAttempt 记录一次模型尝试。
func (r *policyOptimizerTestRecorder) RecordPolicyOptimizerAttempt(
	_ context.Context,
	attempt service.PolicyOptimizerAttempt,
) error {
	r.attempts = append(r.attempts, attempt)
	return nil
}
