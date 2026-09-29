// Package service 提供 Safety Review 角色消息构建。
package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"sendllm/internal/dto"
)

// BuildSafetyReviewMessages 构建指定角色的盲区安全模型消息。
func BuildSafetyReviewMessages(role dto.SafetyReviewRole, input dto.SafetyReviewRoleInput) ([]dto.Message, error) {
	switch role {
	case dto.SafetyReviewJudgeA, dto.SafetyReviewJudgeB, dto.SafetyReviewRouter,
		dto.SafetyReviewExpert, dto.SafetyReviewArbiter:
	default:
		return nil, fmt.Errorf("invalid safety review role %q", role)
	}
	if input.Scene != "prompt" && input.Scene != "response" {
		return nil, fmt.Errorf("invalid safety review scene %q", input.Scene)
	}
	if input.SystemPrompt == "" || input.Policy == "" || len(input.Schema) == 0 {
		return nil, fmt.Errorf("safety review role input lacks policy artifacts")
	}
	payload := map[string]any{
		"trace_id": input.TraceID,
		"scene":    input.Scene,
		"prompt":   input.Prompt,
		"policy":   input.Policy,
		"schema":   json.RawMessage(input.Schema),
	}
	if input.Scene == "response" {
		payload["response"] = input.Response
	}
	switch role {
	case dto.SafetyReviewExpert:
		if len(input.RuleCard) == 0 {
			return nil, fmt.Errorf("expert input lacks rule card")
		}
		payload["rule_card"] = json.RawMessage(input.RuleCard)
	case dto.SafetyReviewArbiter:
		if len(input.PriorOutputs) == 0 {
			return nil, fmt.Errorf("arbiter input lacks prior outputs")
		}
		if err := validateSafetyReviewPriorOutputs(input.PriorOutputs); err != nil {
			return nil, err
		}
		payload["prior_outputs"] = json.RawMessage(input.PriorOutputs)
	}
	userJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode safety review user payload: %w", err)
	}
	return []dto.Message{
		{Role: "system", Content: input.SystemPrompt},
		{Role: "user", Content: string(userJSON)},
	}, nil
}

// validateSafetyReviewPriorOutputs 校验 Arbiter 先验结果的固定对象契约。
func validateSafetyReviewPriorOutputs(raw []byte) error {
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil || len(fields) == 0 {
		return fmt.Errorf("arbiter prior outputs must be a JSON object")
	}
	required := []string{
		"judge_a", "judge_b", "router", "experts",
		"terminal_failures", "fallback_used", "independence_degraded",
	}
	if len(fields) != len(required) {
		return fmt.Errorf("arbiter prior outputs must contain exactly the fixed fields")
	}
	for _, name := range required {
		if _, ok := fields[name]; !ok {
			return fmt.Errorf("arbiter prior outputs lack %s", name)
		}
	}
	for _, name := range []string{"judge_a", "judge_b", "router"} {
		if !jsonObjectStart(fields[name]) {
			return fmt.Errorf("arbiter prior output %s must be an object", name)
		}
	}
	for _, name := range []string{"experts", "terminal_failures"} {
		var values []json.RawMessage
		if err := json.Unmarshal(fields[name], &values); err != nil || values == nil {
			return fmt.Errorf("arbiter prior output %s must be an array", name)
		}
	}
	for _, name := range []string{"fallback_used", "independence_degraded"} {
		var value bool
		if err := json.Unmarshal(fields[name], &value); err != nil {
			return fmt.Errorf("arbiter prior output %s must be a boolean", name)
		}
	}
	return nil
}

// jsonObjectStart 判断 JSON 原始值是否以对象开头。
func jsonObjectStart(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return strings.HasPrefix(trimmed, "{")
}
