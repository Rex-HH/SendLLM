package configs_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"sendllm/internal/lib/configs"
)

// TestSafetyReviewSingleProfileDefaultsToIndependentProfiles 验证旧配置默认使用独立模型模式。
func TestSafetyReviewSingleProfileDefaultsToIndependentProfiles(t *testing.T) {
	config, err := configs.LoadSafetyReview(writeSafetyReviewConfig(t, validSafetyReviewYAML()))
	if err != nil {
		t.Fatalf("LoadSafetyReview(legacy config) error = %v", err)
	}
	if config.Models.ExecutionMode != "independent_profiles" {
		t.Fatalf("ExecutionMode = %q, want independent_profiles", config.Models.ExecutionMode)
	}
}

// TestSafetyReviewSingleProfileAcceptsOperationalProfile 验证唯一 operational Profile 可加载。
func TestSafetyReviewSingleProfileAcceptsOperationalProfile(t *testing.T) {
	config, err := configs.LoadSafetyReview(
		writeSafetyReviewConfig(t, validSafetyReviewSingleProfileYAML()),
	)
	if err != nil {
		t.Fatalf("LoadSafetyReview(single_profile) error = %v", err)
	}
	profile := config.Models.Profiles["operational"]
	if profile.Name != "deepseek-v4-pro" || profile.QuotaGroup != "operational_shared" {
		t.Fatalf("operational profile = %#v", profile)
	}
	if profile.MaxTokens < 12000 {
		t.Fatalf("operational MaxTokens = %d, want at least 12000 for reasoning models", profile.MaxTokens)
	}
	for roleID, role := range config.Models.Roles {
		if role.Primary != "operational" || len(role.Fallbacks) != 0 {
			t.Fatalf("role %s = %#v, want operational primary without fallback", roleID, role)
		}
	}
}

// TestSafetyReviewSingleProfileRejectsInvalidModelContracts 验证单模型闭集校验。
func TestSafetyReviewSingleProfileRejectsInvalidModelContracts(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(string) string
	}{
		{name: "zero profiles", mutate: func(yaml string) string {
			start := strings.Index(yaml, "  profiles:")
			end := strings.Index(yaml, "  roles:")
			return yaml[:start] + "  profiles: {}\n" + yaml[end:]
		}},
		{name: "multiple profiles", mutate: func(yaml string) string {
			return strings.Replace(
				yaml,
				"  roles:",
				"    unexpected:\n      family: deepseek\n      base_url: https://example.test/v1\n"+
					"      api_key_env: TEST_KEY\n      name: unexpected\n      structured_output: json_object\n"+
					"      max_tokens: 100\n      timeout: 30s\n  roles:",
				1,
			)
		}},
		{name: "wrong primary", mutate: func(yaml string) string {
			return strings.Replace(yaml, "primary: operational", "primary: unexpected", 1)
		}},
		{name: "fallback present", mutate: func(yaml string) string {
			return strings.Replace(
				yaml,
				"      fallbacks: []\n      concurrency: 1\n      requests_per_minute: 0\n      tokens_per_minute: 0\n    judge_b:",
				"      fallbacks: [unexpected]\n      concurrency: 1\n"+
					"      requests_per_minute: 0\n      tokens_per_minute: 0\n    judge_b:",
				1,
			)
		}},
		{name: "missing quota group", mutate: func(yaml string) string {
			return strings.Replace(yaml, "      quota_group: operational_shared\n", "", 1)
		}},
		{name: "unknown mode", mutate: func(yaml string) string {
			return strings.Replace(yaml, "  execution_mode: single_profile", "  execution_mode: mixed", 1)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := configs.LoadSafetyReview(
				writeSafetyReviewConfig(t, test.mutate(validSafetyReviewSingleProfileYAML())),
			)
			if !errors.Is(err, configs.ErrInvalidConfig) {
				t.Fatalf("LoadSafetyReview(%s) error = %v, want %v", test.name, err, configs.ErrInvalidConfig)
			}
		})
	}
}

// TestSafetyReviewSingleProfileChangesSemanticFingerprint 验证执行模式进入语义指纹。
func TestSafetyReviewSingleProfileChangesSemanticFingerprint(t *testing.T) {
	independent, err := configs.LoadSafetyReview(writeSafetyReviewConfig(t, validSafetyReviewYAML()))
	if err != nil {
		t.Fatalf("LoadSafetyReview(independent) error = %v", err)
	}
	single, err := configs.LoadSafetyReview(
		writeSafetyReviewConfig(t, validSafetyReviewSingleProfileYAML()),
	)
	if err != nil {
		t.Fatalf("LoadSafetyReview(single) error = %v", err)
	}
	snapshot := map[string][]byte{"policy/common.yaml": []byte("common")}
	independentFingerprint, err := independent.SemanticFingerprint(snapshot)
	if err != nil {
		t.Fatalf("independent SemanticFingerprint() error = %v", err)
	}
	singleFingerprint, err := single.SemanticFingerprint(snapshot)
	if err != nil {
		t.Fatalf("single SemanticFingerprint() error = %v", err)
	}
	if independentFingerprint == singleFingerprint {
		t.Fatal("single_profile did not change semantic fingerprint")
	}
}

// TestSafetyReviewSingleProfileExampleConfigLoads 验证单模型示例配置可加载且不含密钥值。
func TestSafetyReviewSingleProfileExampleConfigLoads(t *testing.T) {
	path := filepath.Join("..", "..", "..", "config", "safety-review-single-model.example.yaml")
	config, err := configs.LoadSafetyReview(path)
	if err != nil {
		t.Fatalf("LoadSafetyReview(example) error = %v", err)
	}
	if config.Models.ExecutionMode != "single_profile" {
		t.Fatalf("ExecutionMode = %q, want single_profile", config.Models.ExecutionMode)
	}
}

// validSafetyReviewSingleProfileYAML 返回完整有效的单模型运营配置。
func validSafetyReviewSingleProfileYAML() string {
	return `version: 1
task:
  id: operational-001
  input: input.jsonl
  task_dir: runs/operational-001
  scene: response
policy:
  bundle_dir: policy/releases/p04b-v1.0
models:
  execution_mode: single_profile
  profiles:
    operational:
      family: deepseek
      base_url: https://aigateway.venusgroup.com.cn/ai/aliyun/openai
      api_key_env: AI_GATEWAY_API_KEY
      name: deepseek-v4-pro
      structured_output: json_object
      max_tokens: 12000
      timeout: 90s
      quota_group: operational_shared
  roles:
    judge_a:
      primary: operational
      fallbacks: []
      concurrency: 1
      requests_per_minute: 0
      tokens_per_minute: 0
    judge_b:
      primary: operational
      fallbacks: []
      concurrency: 1
      requests_per_minute: 0
      tokens_per_minute: 0
    router:
      primary: operational
      fallbacks: []
      concurrency: 1
      requests_per_minute: 0
      tokens_per_minute: 0
    expert:
      primary: operational
      fallbacks: []
      concurrency: 1
      requests_per_minute: 0
      tokens_per_minute: 0
    arbiter:
      primary: operational
      fallbacks: []
      concurrency: 1
      requests_per_minute: 0
      tokens_per_minute: 0
  quota_groups:
    operational_shared:
      concurrency: 1
      requests_per_minute: 0
      tokens_per_minute: 0
runtime:
  shutdown_timeout: 30s
  status_interval: 5s
retry:
  transient_attempts_per_model: 3
  format_repair_attempts: 1
  refusal_reprompt_attempts: 1
  initial_backoff: 1s
  max_backoff: 60s
output:
  clean: clean.jsonl
  audit: audit.jsonl
  quality_events: quality-events.jsonl
  quarantine: quarantine.jsonl
  report: report.json
  run_status: run-status.json
`
}
