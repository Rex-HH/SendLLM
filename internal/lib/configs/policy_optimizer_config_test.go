package configs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPolicyOptimizerConfigLoadAndValidate 验证严格配置加载、版本派生和指纹分离。
func TestPolicyOptimizerConfigLoadAndValidate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy-optimizer.yaml")
	writePolicyOptimizerConfig(t, path, validPolicyOptimizerYAML())
	config, err := LoadPolicyOptimizer(path)
	if err != nil {
		t.Fatalf("LoadPolicyOptimizer() error = %v", err)
	}
	if config.CandidateVersion() != "p04b-v1.1-candidate.1" ||
		config.PreviewVersion() != "p04b-v1.1-preview.1" {
		t.Fatalf("candidate versions = %q/%q", config.CandidateVersion(), config.PreviewVersion())
	}
	before, err := config.SemanticFingerprint(map[string][]byte{"policy.yaml": []byte("one")})
	if err != nil {
		t.Fatalf("SemanticFingerprint() error = %v", err)
	}
	runtimeChanged := *config
	runtimeChanged.Output.StatusInterval++
	afterRuntime, err := runtimeChanged.SemanticFingerprint(map[string][]byte{"policy.yaml": []byte("one")})
	if err != nil {
		t.Fatalf("SemanticFingerprint(runtime) error = %v", err)
	}
	if before != afterRuntime {
		t.Fatal("runtime status interval changed semantic fingerprint")
	}
	semanticChanged := *config
	semanticChanged.Policy.TargetVersion = "p04b-v1.2"
	afterSemantic, err := semanticChanged.SemanticFingerprint(map[string][]byte{"policy.yaml": []byte("one")})
	if err != nil {
		t.Fatalf("SemanticFingerprint(semantic) error = %v", err)
	}
	if before == afterSemantic {
		t.Fatal("semantic target version did not change fingerprint")
	}
}

// TestPolicyOptimizerConfigRejectsUnknownAndInvalidContracts 验证未知字段和冻结闭集拒绝。
func TestPolicyOptimizerConfigRejectsUnknownAndInvalidContracts(t *testing.T) {
	dir := t.TempDir()
	unknownPath := filepath.Join(dir, "unknown.yaml")
	writePolicyOptimizerConfig(t, unknownPath, validPolicyOptimizerYAML()+"\nunknown: true\n")
	if _, err := LoadPolicyOptimizer(unknownPath); err == nil {
		t.Fatal("LoadPolicyOptimizer accepted unknown key")
	}

	invalidPath := filepath.Join(dir, "invalid.yaml")
	invalid := strings.Replace(validPolicyOptimizerYAML(), "target_version: p04b-v1.1", "target_version: p04b-v1.0", 1)
	writePolicyOptimizerConfig(t, invalidPath, invalid)
	if _, err := LoadPolicyOptimizer(invalidPath); err == nil {
		t.Fatal("LoadPolicyOptimizer accepted equal base and target versions")
	}

	escapePath := filepath.Join(dir, "escape.yaml")
	escaped := strings.Replace(
		validPolicyOptimizerYAML(),
		"dir: ./iterations/ITER-001",
		"dir: ../../escape",
		1,
	)
	writePolicyOptimizerConfig(t, escapePath, escaped)
	if _, err := LoadPolicyOptimizer(escapePath); err == nil {
		t.Fatal("LoadPolicyOptimizer accepted an escaping relative path")
	}
}

// writePolicyOptimizerConfig 写入测试配置文件。
func writePolicyOptimizerConfig(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// validPolicyOptimizerYAML 返回一个满足冻结配置形状的测试配置。
func validPolicyOptimizerYAML() string {
	return `version: 1
iteration:
  id: ITER-001
  dir: ./iterations/ITER-001
policy:
  releases_dir: ./policy/releases
  base_version: p04b-v1.0
  target_version: p04b-v1.1
skills:
  root: ./policy-optimization/skills
  manifest: ./policy-optimization/skills/manifest.yaml
models:
  profiles:
    minimax_miner:
      family: minimax
      base_url: https://provider.invalid/v1
      api_key_env: MINIMAX_API_KEY
      name: MiniMax-M2.5
      structured_output: json_object
      max_tokens: 4000
      timeout: 90s
      quota_group: minimax_shared
    qwen_merger:
      family: qwen
      base_url: https://provider.invalid/v1
      api_key_env: QWEN_API_KEY
      name: qwen3-max
      structured_output: json_object
      max_tokens: 4000
      timeout: 90s
    qwen_critic:
      family: qwen
      base_url: https://provider.invalid/v1
      api_key_env: QWEN_API_KEY
      name: qwen3-max
      structured_output: json_object
      max_tokens: 4000
      timeout: 90s
    deepseek_policy:
      family: deepseek
      base_url: https://provider.invalid/v1
      api_key_env: DEEPSEEK_API_KEY
      name: deepseek-v4-pro
      structured_output: json_object
      max_tokens: 4000
      timeout: 90s
  roles:
    source_interpreter: {primary: minimax_miner, fallbacks: [], concurrency: 2, requests_per_minute: 0, tokens_per_minute: 0}
    local_miner: {primary: minimax_miner, fallbacks: [], concurrency: 4, requests_per_minute: 0, tokens_per_minute: 0}
    global_merger: {primary: qwen_merger, fallbacks: [], concurrency: 2, requests_per_minute: 0, tokens_per_minute: 0}
    case_adjudicator: {primary: deepseek_policy, fallbacks: [], concurrency: 2, requests_per_minute: 0, tokens_per_minute: 0}
    policy_diagnoser: {primary: deepseek_policy, fallbacks: [], concurrency: 2, requests_per_minute: 0, tokens_per_minute: 0}
    rule_author: {primary: deepseek_policy, fallbacks: [], concurrency: 1, requests_per_minute: 0, tokens_per_minute: 0}
    critic: {primary: qwen_critic, fallbacks: [], concurrency: 1, requests_per_minute: 0, tokens_per_minute: 0}
    change_resolver: {primary: deepseek_policy, fallbacks: [], concurrency: 1, requests_per_minute: 0, tokens_per_minute: 0}
  quota_groups:
    minimax_shared: {concurrency: 4, requests_per_minute: 0, tokens_per_minute: 0}
batching:
  homogeneous_percent: 70
  conflict_percent: 20
  random_percent: 10
  target_size: 50
  min_size: 30
  max_size: 100
context:
  max_input_tokens: 24000
  max_artifacts: 100
retry:
  transient_attempts_per_model: 3
  format_repair_attempts: 1
  refusal_reprompt_attempts: 1
  initial_backoff: 1s
  max_backoff: 60s
regression:
  contracts_dir: ./policy-optimization/regression/contracts
  approved_gold_dir: ./policy-optimization/regression/gold
  hidden_gold: ./Safety_Review_P04B_Hidden.jsonl
  gate_policy: ./policy-optimization/regression/gates/p04b-gate-v1.yaml
  safety_review_config: ./config/safety-review-eval.example.yaml
output:
  status_interval: 5s
  shutdown_timeout: 30s
`
}
