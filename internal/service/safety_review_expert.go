// Package service 提供 Safety Review 单规则卡 Expert 调用装配。
package service

import (
	"encoding/json"
	"fmt"

	"sendllm/internal/dao"
	"sendllm/internal/dto"
)

// BuildSafetyReviewExpertRequest 构建只包含一个冻结规则卡的盲区 Expert 请求。
func BuildSafetyReviewExpertRequest(
	work dao.SafetyReviewStageWork,
	policy *SafetyReviewPolicy,
) (SafetyReviewCallRequest, error) {
	if work.Role != string(dto.SafetyReviewExpert) {
		return SafetyReviewCallRequest{}, fmt.Errorf("invalid safety review expert role %q", work.Role)
	}
	if policy == nil || !safetyReviewScene(work.Scene) {
		return SafetyReviewCallRequest{}, fmt.Errorf("safety review expert policy or scene is invalid")
	}
	wantStageKey := "expert:" + work.Axis + ":" + work.Category
	card := policy.Cards[work.Category]
	if work.StageKey != wantStageKey || card == nil || card.Axis != work.Axis ||
		!safetyReviewSceneEnabled(card, work.Scene) {
		return SafetyReviewCallRequest{}, fmt.Errorf("invalid safety review expert assignment %q", work.StageKey)
	}
	commonJSON, err := json.Marshal(policy.Common)
	if err != nil {
		return SafetyReviewCallRequest{}, fmt.Errorf("encode expert common policy: %w", err)
	}
	cardJSON, err := json.Marshal(card)
	if err != nil {
		return SafetyReviewCallRequest{}, fmt.Errorf("encode expert rule card: %w", err)
	}
	messages, err := BuildSafetyReviewMessages(dto.SafetyReviewExpert, dto.SafetyReviewRoleInput{
		TraceID:      work.TraceID,
		Scene:        work.Scene,
		Prompt:       work.Prompt,
		Response:     work.Response,
		Policy:       string(commonJSON),
		Schema:       policy.Schemas["expert"],
		RuleCard:     cardJSON,
		SystemPrompt: policy.Prompts["expert"],
	})
	if err != nil {
		return SafetyReviewCallRequest{}, err
	}
	return newSafetyReviewCallRequest(work, messages, policy.Schemas["expert"]), nil
}

// newSafetyReviewCallRequest 组装阶段调用的公共身份字段。
func newSafetyReviewCallRequest(
	work dao.SafetyReviewStageWork,
	messages []dto.Message,
	schema []byte,
) SafetyReviewCallRequest {
	return SafetyReviewCallRequest{
		TaskID:   work.TaskID,
		TraceID:  work.TraceID,
		StageKey: work.StageKey,
		Role:     dto.SafetyReviewRole(work.Role),
		Scene:    work.Scene,
		Axis:     work.Axis,
		Category: work.Category,
		Messages: messages,
		Schema:   append([]byte(nil), schema...),
		Mode:     "json_object",
	}
}
