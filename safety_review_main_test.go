package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSafetyReviewMainDispatchAndLegacyRegression 验证新子命令分发且旧入口不回退。
func TestSafetyReviewMainDispatchAndLegacyRegression(t *testing.T) {
	env := "SAFETY_REVIEW_MAIN_API_KEY"
	t.Setenv(env, "main-key-value")
	config := writeSafetyReviewMainConfig(t, env)
	var stdout, stderr strings.Builder

	code := run(
		context.Background(),
		[]string{"safety-review", "validate", "--config", config},
		&stdout,
		&stderr,
	)
	if code != 0 {
		t.Fatalf("run(safety-review validate) = %d, want 0; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "validation=PASS") {
		t.Fatalf("stdout = %q, want safety-review validation output", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"-config", "/missing/config.yaml"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run(legacy missing config) = %d, want 1", code)
	}
	if strings.Contains(stderr.String(), "safety-review") {
		t.Fatalf("legacy branch unexpectedly used safety-review boundary: %s", stderr.String())
	}
}

// writeSafetyReviewMainConfig 写入用于根入口验证的临时配置。
func writeSafetyReviewMainConfig(t *testing.T, env string) string {
	t.Helper()
	root, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	temp := t.TempDir()
	taskDir := filepath.Join(temp, "runs", "main-test")
	input := filepath.Join(temp, "input.jsonl")
	inputLine := `{"trace_id":"one","prompt":"synthetic prompt","response":"synthetic response"}` + "\n"
	if err := os.WriteFile(input, []byte(inputLine), 0o600); err != nil {
		t.Fatalf("write input: %v", err)
	}
	baseURL := "https://example.test/v1"
	policyDir := filepath.Join(root, "policy", "releases", "p04b-v1.0")
	config := `version: 1
task:
  id: main-test
  input: ` + input + `
  task_dir: ` + taskDir + `
  scene: response
policy:
  bundle_dir: ` + policyDir + `
models:
  profiles:
    glm_5_2:
      family: glm
      base_url: ` + baseURL + `
      api_key_env: ` + env + `
      name: GLM-5.3-Flash
      structured_output: json_object
      max_tokens: 100
      timeout: 1s
    qwen3_max:
      family: qwen
      base_url: ` + baseURL + `
      api_key_env: ` + env + `
      name: qwen3-max
      structured_output: json_object
      max_tokens: 100
      timeout: 1s
    minimax_m2_5:
      family: minimax
      base_url: ` + baseURL + `
      api_key_env: ` + env + `
      name: MiniMax-M2.5
      structured_output: json_object
      max_tokens: 100
      timeout: 1s
      quota_group: minimax_shared
    deepseek_v4_pro:
      family: deepseek
      base_url: ` + baseURL + `
      api_key_env: ` + env + `
      name: deepseek-v4-pro
      structured_output: json_object
      max_tokens: 100
      timeout: 1s
  roles:
    judge_a:
      primary: glm_5_2
      fallbacks: [deepseek_v4_pro]
      concurrency: 1
      requests_per_minute: 0
      tokens_per_minute: 0
    judge_b:
      primary: qwen3_max
      fallbacks: [minimax_m2_5]
      concurrency: 1
      requests_per_minute: 0
      tokens_per_minute: 0
    router:
      primary: minimax_m2_5
      fallbacks: [qwen3_max]
      concurrency: 1
      requests_per_minute: 0
      tokens_per_minute: 0
    expert:
      primary: minimax_m2_5
      fallbacks: [deepseek_v4_pro]
      concurrency: 1
      requests_per_minute: 0
      tokens_per_minute: 0
    arbiter:
      primary: deepseek_v4_pro
      fallbacks: [glm_5_2]
      concurrency: 1
      requests_per_minute: 0
      tokens_per_minute: 0
  quota_groups:
    minimax_shared:
      concurrency: 1
      requests_per_minute: 0
      tokens_per_minute: 0
runtime:
  shutdown_timeout: 1s
  status_interval: 1s
retry:
  transient_attempts_per_model: 1
  format_repair_attempts: 1
  refusal_reprompt_attempts: 1
  initial_backoff: 1ms
  max_backoff: 2ms
output:
  clean: clean.jsonl
  audit: audit.jsonl
  quality_events: quality-events.jsonl
  quarantine: quarantine.jsonl
  report: report.json
  run_status: run-status.json
`
	path := filepath.Join(temp, "safety-review.yaml")
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
