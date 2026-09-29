package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"sendllm/internal/dto"
)

// SafetyReviewPreflightConfig 指定 preflight 所需的模型链与基础契约。
type SafetyReviewPreflightConfig struct {
	Registry SafetyReviewModelRegistry
	Scene    string
	Schema   json.RawMessage
	Requests map[dto.SafetyReviewRole]dto.CompletionRequest
	Retry    RetryPolicy
	Sleeper  func(context.Context, time.Duration) error
	Jitter   func(time.Duration) time.Duration
	OnProbe  func(SafetyReviewPreflightEvent)
}

// SafetyReviewPreflightEvent 表示一次 preflight 探测的安全进度事件。
type SafetyReviewPreflightEvent struct {
	Role          dto.SafetyReviewRole
	Profile       string
	Family        string
	State         string
	ErrorCategory string
	Duration      time.Duration
}

// RunSafetyReviewPreflight 逐个探测所有已配置角色链中的模型。
func RunSafetyReviewPreflight(ctx context.Context, cfg SafetyReviewPreflightConfig) error {
	if cfg.Registry == nil {
		return fmt.Errorf("safety review preflight registry is nil")
	}
	if cfg.Scene != "prompt" && cfg.Scene != "response" {
		return fmt.Errorf("safety review preflight scene is invalid")
	}
	if len(cfg.Schema) == 0 {
		return fmt.Errorf("safety review preflight schema is empty")
	}

	roles := []dto.SafetyReviewRole{
		dto.SafetyReviewJudgeA,
		dto.SafetyReviewJudgeB,
		dto.SafetyReviewRouter,
		dto.SafetyReviewExpert,
		dto.SafetyReviewArbiter,
	}
	type probe struct {
		role  dto.SafetyReviewRole
		model SafetyReviewModel
		req   dto.CompletionRequest
	}
	probes := make([]probe, 0, len(roles))
	for _, role := range roles {
		chain := cfg.Registry.Chain(role)
		if len(chain) == 0 {
			return fmt.Errorf("safety review preflight role %s has no model chain", role)
		}
		request := cfg.Requests[role]
		if len(request.Messages) == 0 {
			request = dto.CompletionRequest{
				Messages: safetyReviewPreflightMessages(),
				Schema:   append(json.RawMessage(nil), cfg.Schema...),
				Mode:     "json_object",
			}
		}
		if len(request.Messages) == 0 || len(request.Schema) == 0 || request.Mode == "" {
			return fmt.Errorf("safety review preflight role %s request is incomplete", role)
		}
		for _, model := range chain {
			if model.Completer == nil {
				return fmt.Errorf("safety review preflight model %s has no completer", model.Profile)
			}
			probe := dto.CompletionRequest{
				Messages: append([]dto.Message(nil), request.Messages...),
				Schema:   append(json.RawMessage(nil), request.Schema...),
				Mode:     request.Mode,
			}
			probes = append(probes, struct {
				role  dto.SafetyReviewRole
				model SafetyReviewModel
				req   dto.CompletionRequest
			}{role: role, model: model, req: probe})
		}
	}
	errorsByProbe := make([]error, len(probes))
	var wait sync.WaitGroup
	for index, current := range probes {
		index, current := index, current
		wait.Add(1)
		go func() {
			defer wait.Done()
			startedAt := time.Now()
			emitSafetyReviewPreflightEvent(cfg, SafetyReviewPreflightEvent{
				Role: current.role, Profile: current.model.Profile,
				Family: current.model.Family, State: "running",
			})
			err := completeSafetyReviewPreflight(ctx, cfg, current.model, current.req)
			finishedAt := time.Now()
			if err != nil {
				emitSafetyReviewPreflightEvent(cfg, SafetyReviewPreflightEvent{
					Role: current.role, Profile: current.model.Profile, Family: current.model.Family,
					State: "failed", ErrorCategory: ClassifyFailure(err).Category,
					Duration: finishedAt.Sub(startedAt),
				})
				errorsByProbe[index] = fmt.Errorf(
					"safety review preflight role %s profile %s: %w",
					current.role, current.model.Profile, err,
				)
				return
			}
			emitSafetyReviewPreflightEvent(cfg, SafetyReviewPreflightEvent{
				Role: current.role, Profile: current.model.Profile, Family: current.model.Family,
				State: "succeeded", Duration: finishedAt.Sub(startedAt),
			})
		}()
	}
	wait.Wait()
	for _, err := range errorsByProbe {
		if err != nil {
			return err
		}
	}
	return nil
}

// emitSafetyReviewPreflightEvent 发送不含 payload 的 preflight 事件。
func emitSafetyReviewPreflightEvent(cfg SafetyReviewPreflightConfig, event SafetyReviewPreflightEvent) {
	if cfg.OnProbe != nil {
		cfg.OnProbe(event)
	}
}

// completeSafetyReviewPreflight 按正式调用策略重试 preflight 的瞬态失败。
func completeSafetyReviewPreflight(
	ctx context.Context,
	cfg SafetyReviewPreflightConfig,
	model SafetyReviewModel,
	probe dto.CompletionRequest,
) error {
	attempts := cfg.Retry.MaxAttempts
	if attempts < 1 {
		attempts = 1
	}
	sleeper := cfg.Sleeper
	if sleeper == nil {
		sleeper = sleepContext
	}
	jitter := cfg.Jitter
	if jitter == nil {
		jitter = identityDuration
	}
	for attempt := 1; attempt <= attempts; attempt++ {
		_, err := model.Completer.Complete(ctx, probe)
		if err == nil {
			return nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		decision := ClassifyFailure(err)
		if !decision.Retry || attempt == attempts {
			return err
		}
		delay := cfg.Retry.Delay(attempt, decision.RetryAfter, jitter)
		if sleepErr := sleeper(ctx, delay); sleepErr != nil {
			return sleepErr
		}
	}
	return nil
}

// safetyReviewPreflightMessages 构造固定无害的合成探测消息。
func safetyReviewPreflightMessages() []dto.Message {
	return []dto.Message{
		{Role: "system", Content: "safety review preflight"},
		{Role: "user", Content: `{"preflight":"synthetic benign content","scene":"prompt"}`},
	}
}
