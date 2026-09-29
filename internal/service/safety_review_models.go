// Package service 提供 Safety Review 的模型链与调用策略。
package service

import (
	"fmt"

	"sendllm/internal/dto"
)

// SafetyReviewModel 表示一个可调用的 Safety Review 模型。
type SafetyReviewModel struct {
	Profile   string
	Family    string
	Completer Completer
}

// SafetyReviewModelRegistry 提供角色到主备模型链的只读视图。
type SafetyReviewModelRegistry interface {
	Chain(role dto.SafetyReviewRole) []SafetyReviewModel
}

// safetyReviewModelRegistry 是模型链的默认只读实现。
type safetyReviewModelRegistry struct {
	chains map[dto.SafetyReviewRole][]SafetyReviewModel
}

// NewSafetyReviewModelRegistry 构造并校验角色模型链。
func NewSafetyReviewModelRegistry(
	chains map[dto.SafetyReviewRole][]SafetyReviewModel,
) (SafetyReviewModelRegistry, error) {
	if len(chains) == 0 {
		return nil, fmt.Errorf("safety review model registry is empty")
	}
	copied := make(map[dto.SafetyReviewRole][]SafetyReviewModel, len(chains))
	for role, chain := range chains {
		switch role {
		case dto.SafetyReviewJudgeA, dto.SafetyReviewJudgeB, dto.SafetyReviewRouter,
			dto.SafetyReviewExpert, dto.SafetyReviewArbiter:
		default:
			return nil, fmt.Errorf("unknown safety review role %q", role)
		}
		if len(chain) == 0 {
			return nil, fmt.Errorf("safety review role %q has an empty model chain", role)
		}
		models := make([]SafetyReviewModel, len(chain))
		for index, model := range chain {
			if model.Profile == "" || model.Family == "" || model.Completer == nil {
				return nil, fmt.Errorf("safety review role %q model %d is incomplete", role, index)
			}
			models[index] = model
		}
		copied[role] = models
	}
	return &safetyReviewModelRegistry{chains: copied}, nil
}

// Chain 返回指定角色的模型链副本。
func (r *safetyReviewModelRegistry) Chain(role dto.SafetyReviewRole) []SafetyReviewModel {
	chain := r.chains[role]
	if len(chain) == 0 {
		return nil
	}
	result := make([]SafetyReviewModel, len(chain))
	copy(result, chain)
	return result
}
