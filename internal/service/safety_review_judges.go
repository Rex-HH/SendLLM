package service

import (
	"encoding/json"
	"fmt"

	"sendllm/internal/dto"
)

// parseSafetyReviewJudgment 解析 Judge A/B 的结构化结果。
func parseSafetyReviewJudgment(raw []byte) (*dto.SafetyReviewJudgment, error) {
	result := &dto.SafetyReviewJudgment{}
	if err := json.Unmarshal(raw, result); err != nil {
		return nil, fmt.Errorf("decode safety review judgment: %w", err)
	}
	return result, nil
}
