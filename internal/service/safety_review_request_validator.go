package service

import (
	"fmt"

	"sendllm/internal/dto"
)

// NewSafetyReviewRequestValidator 构造按角色校验单次模型结果的修复前校验器。
func NewSafetyReviewRequestValidator(
	policy *SafetyReviewPolicy,
) (func(SafetyReviewCallRequest, []byte) error, error) {
	validator, err := NewSafetyReviewValidator(policy)
	if err != nil {
		return nil, err
	}
	return func(req SafetyReviewCallRequest, raw []byte) error {
		switch req.Role {
		case dto.SafetyReviewJudgeA, dto.SafetyReviewJudgeB:
			_, validateErr := validator.ValidateJudgment(req.Scene, raw)
			return validateErr
		case dto.SafetyReviewRouter:
			_, validateErr := validator.ValidateRoute(req.Scene, raw)
			return validateErr
		case dto.SafetyReviewExpert:
			_, validateErr := validator.ValidateExpert(
				req.Scene,
				req.Axis,
				req.Category,
				raw,
			)
			return validateErr
		case dto.SafetyReviewArbiter:
			return validator.validateDecisionDraft(req.Scene, raw)
		default:
			return fmt.Errorf("safety review request validator role %q is invalid", req.Role)
		}
	}, nil
}
