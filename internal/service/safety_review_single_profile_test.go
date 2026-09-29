package service_test

import (
	"context"
	"testing"
	"time"

	"sendllm/internal/dto"
	"sendllm/internal/lib/limiter"
	"sendllm/internal/service"
)

// TestSafetyReviewSingleProfileRegistryKeepsFiveRoleChains 验证单模型仍保留五个角色链。
func TestSafetyReviewSingleProfileRegistryKeepsFiveRoleChains(t *testing.T) {
	completer := safetyReviewSuccessCompleter()
	model := service.SafetyReviewModel{
		Profile: "operational", Family: "deepseek", Completer: completer,
	}
	chains := make(map[dto.SafetyReviewRole][]service.SafetyReviewModel, 5)
	for _, role := range []dto.SafetyReviewRole{
		dto.SafetyReviewJudgeA, dto.SafetyReviewJudgeB, dto.SafetyReviewRouter,
		dto.SafetyReviewExpert, dto.SafetyReviewArbiter,
	} {
		chains[role] = []service.SafetyReviewModel{model}
	}
	registry, err := service.NewSafetyReviewModelRegistry(chains)
	if err != nil {
		t.Fatalf("NewSafetyReviewModelRegistry() error = %v", err)
	}
	for role, chain := range registryChains(registry) {
		if len(chain) != 1 || chain[0].Profile != "operational" || chain[0].Completer == nil {
			t.Fatalf("chain %s = %#v, want one operational model", role, chain)
		}
	}
}

// TestSafetyReviewSingleProfileSharedQuotaLimitsRoleConcurrency 验证所有角色共享一个并发上限。
func TestSafetyReviewSingleProfileSharedQuotaLimitsRoleConcurrency(t *testing.T) {
	zero := 0
	quota, err := limiter.NewSafetyReviewQuota(limiter.SafetyReviewQuotaConfig{
		Roles: map[string]limiter.SafetyReviewRoleQuota{
			"judge_a": {Concurrency: 1, RequestsPerMinute: zero, TokensPerMinute: zero},
			"judge_b": {Concurrency: 1, RequestsPerMinute: zero, TokensPerMinute: zero},
		},
		Groups: map[string]limiter.SafetyReviewGroupQuota{
			"operational_shared": {Concurrency: 1, RequestsPerMinute: zero, TokensPerMinute: zero},
		},
	})
	if err != nil {
		t.Fatalf("NewSafetyReviewQuota() error = %v", err)
	}
	release, err := quota.Acquire(context.Background(), "judge_a", "operational_shared", 0)
	if err != nil {
		t.Fatalf("first Acquire() error = %v", err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := quota.Acquire(ctx, "judge_b", "operational_shared", 0); err == nil {
		t.Fatal("second role acquired the exhausted shared quota")
	}
}

// registryChains 返回五个角色的模型链快照。
func registryChains(registry service.SafetyReviewModelRegistry) map[dto.SafetyReviewRole][]service.SafetyReviewModel {
	result := make(map[dto.SafetyReviewRole][]service.SafetyReviewModel, 5)
	for _, role := range []dto.SafetyReviewRole{
		dto.SafetyReviewJudgeA, dto.SafetyReviewJudgeB, dto.SafetyReviewRouter,
		dto.SafetyReviewExpert, dto.SafetyReviewArbiter,
	} {
		result[role] = registry.Chain(role)
	}
	return result
}
