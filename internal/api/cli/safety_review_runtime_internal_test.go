package cli

import (
	"testing"
	"time"

	"sendllm/internal/lib/configs"
)

// TestSafetyReviewRuntimeSkipsUnusedProfiles 验证未被角色链引用的固定 Profile 不会阻塞运行时装配。
func TestSafetyReviewRuntimeSkipsUnusedProfiles(t *testing.T) {
	zero := 0
	cfg := &configs.SafetyReviewConfig{
		Models: configs.SafetyReviewModelsConfig{
			Profiles: map[string]configs.SafetyReviewModelProfile{
				"unused": {
					Family: "glm", BaseURL: "https://unused.example.test/v1",
					APIKeyEnv: "AI_GATEWAY_API_KEY", Name: "GLM-5.3-Flash",
					StructuredOutput: "json_object", MaxTokens: 100, Timeout: time.Second,
				},
				"used": {
					Family: "qwen", BaseURL: "https://used.example.test/v1",
					APIKeyEnv: "AI_GATEWAY_API_KEY", Name: "qwen3-max",
					StructuredOutput: "json_object", MaxTokens: 100, Timeout: time.Second,
				},
			},
			Roles: map[string]configs.SafetyReviewRoleConfig{
				"judge_a": {
					Primary: "used", Fallbacks: []string{"used"}, Concurrency: 1,
					RequestsPerMinute: &zero, TokensPerMinute: &zero,
				},
			},
			QuotaGroups: map[string]configs.SafetyReviewQuotaConfig{},
		},
	}

	if _, err := buildSafetyReviewRuntime(cfg); err != nil {
		t.Fatalf("buildSafetyReviewRuntime() error = %v", err)
	}
}
