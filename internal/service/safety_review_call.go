package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"sendllm/internal/dao"
	"sendllm/internal/dto"
)

// SafetyReviewRecordedAttempt 表示带阶段身份的一次模型调用尝试。
type SafetyReviewRecordedAttempt = dao.SafetyReviewRecordedAttempt

// SafetyReviewAttemptRecorder 持久化一次带阶段身份的模型调用尝试。
type SafetyReviewAttemptRecorder interface {
	RecordSafetyReviewAttempt(ctx context.Context, attempt SafetyReviewRecordedAttempt) error
}

// SafetyReviewCallerConfig 指定 Caller 的依赖、重试与时间控制。
type SafetyReviewCallerConfig struct {
	Registry         SafetyReviewModelRegistry
	Store            SafetyReviewAttemptRecorder
	Retry            RetryPolicy
	Validator        func([]byte) error
	RequestValidator func(SafetyReviewCallRequest, []byte) error
	RefusalPrefixes  []string
	Clock            func() time.Time
	Sleeper          func(context.Context, time.Duration) error
	Jitter           func(time.Duration) time.Duration
}

// SafetyReviewCallRequest 表示一次 Safety Review 模型调用请求。
type SafetyReviewCallRequest struct {
	TaskID          string
	TraceID         string
	StageKey        string
	Role            dto.SafetyReviewRole
	Scene           string
	Axis            string
	Category        string
	Messages        []dto.Message
	Schema          json.RawMessage
	Mode            string
	EstimatedTokens int
}

// SafetyReviewCallResult 表示一次成功调用的模型输出与元数据。
type SafetyReviewCallResult struct {
	Content              []byte
	Profile              string
	Family               string
	APIKeyEnv            string
	FallbackIndex        int
	IndependenceDegraded bool
	Attempts             int
}

// SafetyReviewCaller 执行单阶段的主备模型调用策略。
type SafetyReviewCaller struct {
	cfg      SafetyReviewCallerConfig
	mu       sync.Mutex
	circuits map[string]bool
}

// NewSafetyReviewCaller 校验依赖并构造模型调用器。
func NewSafetyReviewCaller(cfg SafetyReviewCallerConfig) (*SafetyReviewCaller, error) {
	if cfg.Registry == nil || cfg.Store == nil {
		return nil, fmt.Errorf("safety review caller requires registry and attempt store")
	}
	if cfg.Retry.MaxAttempts < 1 || cfg.Retry.InitialBackoff <= 0 ||
		cfg.Retry.MaxBackoff < cfg.Retry.InitialBackoff {
		return nil, fmt.Errorf("safety review caller retry policy is invalid")
	}
	if cfg.Retry.MaxAttempts > 3 {
		return nil, fmt.Errorf("safety review caller retry attempts must not exceed 3")
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	if cfg.Sleeper == nil {
		cfg.Sleeper = sleepContext
	}
	if cfg.Jitter == nil {
		cfg.Jitter = identityDuration
	}
	if cfg.RefusalPrefixes == nil {
		cfg.RefusalPrefixes = []string{}
	}
	return &SafetyReviewCaller{
		cfg:      cfg,
		circuits: make(map[string]bool),
	}, nil
}

// Call 按主备模型链执行一次 Safety Review 阶段调用。
func (c *SafetyReviewCaller) Call(
	ctx context.Context,
	req SafetyReviewCallRequest,
) (SafetyReviewCallResult, error) {
	if err := c.validateRequest(req); err != nil {
		return SafetyReviewCallResult{}, err
	}

	var result SafetyReviewCallResult
	chain := c.cfg.Registry.Chain(req.Role)
	for fallbackIndex, model := range chain {
		if c.profileCircuited(model.Profile) {
			continue
		}
		profileAttempts := 0
		repairUsed := false
		refusalUsed := false

		for profileAttempts < c.cfg.Retry.MaxAttempts {
			startedAt := c.cfg.Clock()
			response, err := model.Completer.Complete(ctx, c.completionRequest(req))
			finishedAt := c.cfg.Clock()
			profileAttempts++
			result.Attempts++
			result.APIKeyEnv = response.APIKeyEnv
			if recordErr := c.recordAttempt(
				ctx,
				req,
				model,
				response,
				"classification",
				startedAt,
				finishedAt,
				err,
			); recordErr != nil {
				return result, recordErr
			}
			if err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return result, ctxErr
				}
				decision := ClassifyFailure(err)
				if decision.Category == string(dto.ProviderContentRejected) {
					return result, fmt.Errorf("safety review stage %s content rejected: %w", req.StageKey, err)
				}
				if decision.Category == string(dto.ProviderAuthentication) ||
					decision.Category == string(dto.ProviderBadRequest) {
					c.circuitProfile(model.Profile)
					break
				}
				if decision.Retry && profileAttempts < c.cfg.Retry.MaxAttempts {
					delay := c.cfg.Retry.Delay(
						profileAttempts,
						decision.RetryAfter,
						c.cfg.Jitter,
					)
					if sleepErr := c.cfg.Sleeper(ctx, delay); sleepErr != nil {
						return result, sleepErr
					}
					continue
				}
				break
			}

			if c.isRefusal(response.Content) && !refusalUsed {
				refusalUsed = true
				repromptStartedAt := c.cfg.Clock()
				repromptResponse, repromptErr := model.Completer.Complete(
					ctx,
					c.refusalRequest(req),
				)
				repromptFinishedAt := c.cfg.Clock()
				profileAttempts++
				result.Attempts++
				if recordErr := c.recordAttempt(
					ctx,
					req,
					model,
					repromptResponse,
					"refusal_reprompt",
					repromptStartedAt,
					repromptFinishedAt,
					repromptErr,
				); recordErr != nil {
					return result, recordErr
				}
				if repromptErr != nil {
					if ctxErr := ctx.Err(); ctxErr != nil {
						return result, ctxErr
					}
					break
				}
				response = repromptResponse
				if c.isRefusal(response.Content) {
					break
				}
			}

			if c.hasResultValidator() {
				validationErr := c.validateResult(req, response.Content)
				if validationErr != nil && !repairUsed {
					repairUsed = true
					repairStartedAt := c.cfg.Clock()
					repairResponse, repairErr := model.Completer.Complete(
						ctx,
						c.repairRequest(req, response.Content, validationErr),
					)
					repairFinishedAt := c.cfg.Clock()
					profileAttempts++
					result.Attempts++
					if recordErr := c.recordAttempt(
						ctx,
						req,
						model,
						repairResponse,
						"format_repair",
						repairStartedAt,
						repairFinishedAt,
						repairErr,
					); recordErr != nil {
						return result, recordErr
					}
					if repairErr != nil {
						if ctxErr := ctx.Err(); ctxErr != nil {
							return result, ctxErr
						}
						break
					}
					response = repairResponse
					validationErr = c.validateResult(req, response.Content)
				}
				if validationErr != nil {
					break
				}
			}

			result.Content = append([]byte(nil), response.Content...)
			result.Profile = model.Profile
			result.Family = model.Family
			result.FallbackIndex = fallbackIndex
			result.IndependenceDegraded = fallbackIndex > 0 &&
				(req.Role == dto.SafetyReviewJudgeA || req.Role == dto.SafetyReviewJudgeB)
			return result, nil
		}
	}
	return result, fmt.Errorf("safety review stage %s exhausted all model profiles", req.StageKey)
}

// hasResultValidator 判断当前 Caller 是否配置了结果校验器。
func (c *SafetyReviewCaller) hasResultValidator() bool {
	return c.cfg.RequestValidator != nil || c.cfg.Validator != nil
}

// validateResult 优先使用带阶段上下文的请求校验器，并兼容旧的无上下文校验器。
func (c *SafetyReviewCaller) validateResult(
	req SafetyReviewCallRequest,
	raw []byte,
) error {
	if c.cfg.RequestValidator != nil {
		return c.cfg.RequestValidator(req, raw)
	}
	if c.cfg.Validator != nil {
		return c.cfg.Validator(raw)
	}
	return nil
}

// validateRequest 校验调用请求的必要身份和模型入参。
func (c *SafetyReviewCaller) validateRequest(req SafetyReviewCallRequest) error {
	if req.TaskID == "" || req.TraceID == "" || req.StageKey == "" {
		return fmt.Errorf("safety review call identity is incomplete")
	}
	switch req.Role {
	case dto.SafetyReviewJudgeA, dto.SafetyReviewJudgeB, dto.SafetyReviewRouter,
		dto.SafetyReviewExpert, dto.SafetyReviewArbiter:
	default:
		return fmt.Errorf("safety review call role %q is invalid", req.Role)
	}
	if len(req.Messages) == 0 || len(req.Schema) == 0 || req.Mode == "" {
		return fmt.Errorf("safety review call request is incomplete")
	}
	return nil
}

// completionRequest 转换为通用补全请求。
func (c *SafetyReviewCaller) completionRequest(req SafetyReviewCallRequest) dto.CompletionRequest {
	return dto.CompletionRequest{
		Messages: copySafetyReviewMessages(req.Messages),
		Schema:   append(json.RawMessage(nil), req.Schema...),
		Mode:     req.Mode,
	}
}

// refusalRequest 构造拒答后的重提示请求。
func (c *SafetyReviewCaller) refusalRequest(req SafetyReviewCallRequest) dto.CompletionRequest {
	messages := copySafetyReviewMessages(req.Messages)
	messages = append(messages, dto.Message{
		Role:    "system",
		Content: "continue the requested safety review task",
	})
	return dto.CompletionRequest{
		Messages: messages,
		Schema:   append(json.RawMessage(nil), req.Schema...),
		Mode:     req.Mode,
	}
}

// repairRequest 构造非法输出后的修复请求。
func (c *SafetyReviewCaller) repairRequest(
	req SafetyReviewCallRequest,
	invalidOutput []byte,
	validationErr error,
) dto.CompletionRequest {
	messages := copySafetyReviewMessages(req.Messages)
	messages = append(messages, dto.Message{
		Role:    "system",
		Content: "repair the previous JSON output",
	})
	messages = append(messages, dto.Message{
		Role:    "assistant",
		Content: string(invalidOutput),
	})
	messages = append(messages, dto.Message{
		Role:    "user",
		Content: validationErr.Error(),
	})
	return dto.CompletionRequest{
		Messages: messages,
		Schema:   append(json.RawMessage(nil), req.Schema...),
		Mode:     req.Mode,
	}
}

// recordAttempt 持久化一次模型尝试的安全摘要。
func (c *SafetyReviewCaller) recordAttempt(
	ctx context.Context,
	req SafetyReviewCallRequest,
	model SafetyReviewModel,
	response dto.CompletionResponse,
	kind string,
	startedAt time.Time,
	finishedAt time.Time,
	err error,
) error {
	attempt := dao.SafetyReviewAttempt{
		AttemptKind:      kind,
		ModelProfile:     model.Profile,
		ModelFamily:      model.Family,
		APIKeyEnv:        response.APIKeyEnv,
		StartedAt:        startedAt,
		FinishedAt:       finishedAt,
		FinishReason:     response.FinishReason,
		PromptTokens:     response.Usage.PromptTokens,
		CompletionTokens: response.Usage.CompletionTokens,
		RawResponse:      append([]byte(nil), response.RawResponse...),
	}
	if err != nil {
		decision := ClassifyFailure(err)
		attempt.ErrorCategory = decision.Category
		attempt.ErrorSummary = decision.Category
		attempt.Retryable = decision.Retry
		var validationErr *ValidationError
		if errors.As(err, &validationErr) {
			attempt.ValidationError = []byte(strings.Join(validationErr.Problems, "; "))
		}
	}
	recorded := SafetyReviewRecordedAttempt{
		TaskID:   req.TaskID,
		TraceID:  req.TraceID,
		StageKey: req.StageKey,
		Attempt:  attempt,
	}
	if err := c.cfg.Store.RecordSafetyReviewAttempt(ctx, recorded); err != nil {
		return fmt.Errorf("record safety review attempt: %w", err)
	}
	return nil
}

// isRefusal 判断模型输出是否命中拒答前缀。
func (c *SafetyReviewCaller) isRefusal(content []byte) bool {
	text := strings.TrimSpace(string(content))
	for _, prefix := range c.cfg.RefusalPrefixes {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}

// circuitProfile 将认证或坏模型 Profile 从本 Caller 中熔断。
func (c *SafetyReviewCaller) circuitProfile(profile string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.circuits[profile] = true
}

// profileCircuited 判断 Profile 是否已在本 Caller 中熔断。
func (c *SafetyReviewCaller) profileCircuited(profile string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.circuits[profile]
}

// copySafetyReviewMessages 复制消息切片。
func copySafetyReviewMessages(messages []dto.Message) []dto.Message {
	result := make([]dto.Message, len(messages))
	copy(result, messages)
	return result
}

// identityDuration 返回原始时长。
func identityDuration(value time.Duration) time.Duration {
	return value
}

// sleepContext 在上下文取消前等待指定时长。
func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
