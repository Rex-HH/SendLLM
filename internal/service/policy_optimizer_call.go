package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"sendllm/internal/dto"
)

// PolicyOptimizerModel 表示一个可用的优化器模型。
type PolicyOptimizerModel struct {
	Profile   string
	Family    string
	Completer Completer
}

// PolicyOptimizerModelRegistry 提供角色到模型链的只读视图。
type PolicyOptimizerModelRegistry interface {
	Chain(role string) []PolicyOptimizerModel
}

// PolicyOptimizerAttempt 表示一次模型调用尝试的安全摘要。
type PolicyOptimizerAttempt struct {
	RunID            string
	IterationID      string
	Stage            string
	SkillID          string
	ModelProfile     string
	ModelFamily      string
	AttemptKind      string
	ErrorCategory    string
	PromptTokens     int
	CompletionTokens int
	StartedAt        time.Time
	FinishedAt       time.Time
}

// PolicyOptimizerAttemptRecorder 持久化模型调用尝试。
type PolicyOptimizerAttemptRecorder interface {
	RecordPolicyOptimizerAttempt(ctx context.Context, attempt PolicyOptimizerAttempt) error
}

// PolicyOptimizerCallerConfig 指定模型调用重试、验证和依赖。
type PolicyOptimizerCallerConfig struct {
	Registry        PolicyOptimizerModelRegistry
	Recorder        PolicyOptimizerAttemptRecorder
	Retry           RetryPolicy
	Validator       func([]byte) error
	RefusalPrefixes []string
	Clock           func() time.Time
	Sleeper         func(context.Context, time.Duration) error
	Jitter          func(time.Duration) time.Duration
}

// PolicyOptimizerCallRequest 表示一次 Skill 模型调用。
type PolicyOptimizerCallRequest struct {
	RunID       string
	IterationID string
	Stage       string
	SkillID     string
	Role        string
	Messages    []dto.Message
	Schema      json.RawMessage
	Mode        string
}

// PolicyOptimizerCallResult 表示一次成功模型调用。
type PolicyOptimizerCallResult struct {
	Content  []byte
	Profile  string
	Family   string
	Attempts int
}

// PolicyOptimizerCaller 执行优化器模型的持久化重试策略。
type PolicyOptimizerCaller struct {
	cfg PolicyOptimizerCallerConfig
}

// NewPolicyOptimizerCaller 校验并构造优化器模型调用器。
func NewPolicyOptimizerCaller(cfg PolicyOptimizerCallerConfig) (*PolicyOptimizerCaller, error) {
	if cfg.Registry == nil || cfg.Recorder == nil {
		return nil, fmt.Errorf("policy optimizer caller requires registry and recorder")
	}
	if cfg.Retry.MaxAttempts < 1 || cfg.Retry.MaxAttempts > 3 ||
		cfg.Retry.InitialBackoff <= 0 || cfg.Retry.MaxBackoff < cfg.Retry.InitialBackoff {
		return nil, fmt.Errorf("policy optimizer caller retry policy is invalid")
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
	return &PolicyOptimizerCaller{cfg: cfg}, nil
}

// Call 按模型链执行一次优化器 Skill 调用。
func (c *PolicyOptimizerCaller) Call(
	ctx context.Context,
	req PolicyOptimizerCallRequest,
) (PolicyOptimizerCallResult, error) {
	if req.RunID == "" || req.IterationID == "" || req.Stage == "" || req.SkillID == "" ||
		req.Role == "" || len(req.Messages) == 0 || len(req.Schema) == 0 || req.Mode == "" {
		return PolicyOptimizerCallResult{}, fmt.Errorf("policy optimizer call request is incomplete")
	}
	chain := c.cfg.Registry.Chain(req.Role)
	if len(chain) == 0 {
		return PolicyOptimizerCallResult{}, fmt.Errorf("policy optimizer role %q has no model chain", req.Role)
	}
	var result PolicyOptimizerCallResult
	for _, model := range chain {
		repairUsed := false
		refusalUsed := false
		for attempt := 1; attempt <= c.cfg.Retry.MaxAttempts; attempt++ {
			started := c.cfg.Clock()
			response, err := model.Completer.Complete(ctx, dto.CompletionRequest{
				Messages: copySafetyReviewMessages(req.Messages), Schema: append(json.RawMessage(nil), req.Schema...),
				Mode: req.Mode,
			})
			finished := c.cfg.Clock()
			result.Attempts++
			if recordErr := c.record(ctx, req, model, "classification", started, finished, response, err); recordErr != nil {
				return result, recordErr
			}
			if err != nil {
				decision := ClassifyFailure(err)
				if decision.Category == string(dto.ProviderContentRejected) {
					break
				}
				if decision.Retry && attempt < c.cfg.Retry.MaxAttempts {
					if sleepErr := c.cfg.Sleeper(ctx, c.cfg.Retry.Delay(attempt, decision.RetryAfter, c.cfg.Jitter)); sleepErr != nil {
						return result, sleepErr
					}
					continue
				}
				break
			}
			if c.refusal(response.Content) && !refusalUsed {
				refusalUsed = true
				reprompt := append(copySafetyReviewMessages(req.Messages), dto.Message{
					Role: "system", Content: "return only the required JSON object",
				})
				repromptStarted := c.cfg.Clock()
				repromptResponse, repromptErr := model.Completer.Complete(ctx, dto.CompletionRequest{
					Messages: reprompt, Schema: append(json.RawMessage(nil), req.Schema...), Mode: req.Mode,
				})
				repromptFinished := c.cfg.Clock()
				result.Attempts++
				if recordErr := c.record(
					ctx, req, model, "refusal_reprompt", repromptStarted, repromptFinished,
					repromptResponse, repromptErr,
				); recordErr != nil {
					return result, recordErr
				}
				if repromptErr != nil {
					break
				}
				response = repromptResponse
			}
			if c.cfg.Validator != nil {
				validationErr := c.cfg.Validator(response.Content)
				if validationErr != nil && !repairUsed {
					repairUsed = true
					repairStarted := c.cfg.Clock()
					repairResponse, repairErr := model.Completer.Complete(ctx, dto.CompletionRequest{
						Messages: appendSafetyReviewRepair(req.Messages, response.Content, validationErr),
						Schema:   append(json.RawMessage(nil), req.Schema...), Mode: req.Mode,
					})
					repairFinished := c.cfg.Clock()
					result.Attempts++
					if recordErr := c.record(
						ctx, req, model, "format_repair", repairStarted, repairFinished,
						repairResponse, repairErr,
					); recordErr != nil {
						return result, recordErr
					}
					if repairErr != nil {
						break
					}
					response = repairResponse
					validationErr = c.cfg.Validator(response.Content)
				}
				if validationErr != nil {
					break
				}
			}
			result.Content = append([]byte(nil), response.Content...)
			result.Profile = model.Profile
			result.Family = model.Family
			return result, nil
		}
	}
	return result, fmt.Errorf("policy optimizer run %q exhausted model chain", req.RunID)
}

// record 持久化一次模型调用尝试。
func (c *PolicyOptimizerCaller) record(
	ctx context.Context,
	req PolicyOptimizerCallRequest,
	model PolicyOptimizerModel,
	kind string,
	started, finished time.Time,
	response dto.CompletionResponse,
	err error,
) error {
	category := ""
	if err != nil {
		category = ClassifyFailure(err).Category
	}
	return c.cfg.Recorder.RecordPolicyOptimizerAttempt(ctx, PolicyOptimizerAttempt{
		RunID: req.RunID, IterationID: req.IterationID, Stage: req.Stage, SkillID: req.SkillID,
		ModelProfile: model.Profile, ModelFamily: model.Family, AttemptKind: kind,
		ErrorCategory: category, PromptTokens: response.Usage.PromptTokens,
		CompletionTokens: response.Usage.CompletionTokens,
		StartedAt:        started, FinishedAt: finished,
	})
}

// refusal 判断内容是否为文本拒答。
func (c *PolicyOptimizerCaller) refusal(content []byte) bool {
	text := strings.TrimSpace(string(content))
	for _, prefix := range c.cfg.RefusalPrefixes {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}

// appendSafetyReviewRepair 复用安全修复消息形状，避免重复拼装逻辑。
func appendSafetyReviewRepair(messages []dto.Message, invalid []byte, validationErr error) []dto.Message {
	result := copySafetyReviewMessages(messages)
	result = append(result, dto.Message{Role: "system", Content: "repair the previous JSON output"})
	result = append(result, dto.Message{Role: "assistant", Content: string(invalid)})
	result = append(result, dto.Message{Role: "user", Content: validationErr.Error()})
	return result
}
