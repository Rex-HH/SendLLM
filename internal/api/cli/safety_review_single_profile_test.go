package cli_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sendllm/internal/api/cli"
)

// TestSafetyReviewSingleProfileRunPerformsRoleSpecificPreflight 验证单模型 run 对五个角色分别执行真实 preflight。
func TestSafetyReviewSingleProfileRunPerformsRoleSpecificPreflight(t *testing.T) {
	env := "SAFETY_REVIEW_SINGLE_PROFILE_API_KEY"
	t.Setenv(env, "test-key-value")
	root := safetyReviewCLIRepoRoot(t)
	temp := t.TempDir()
	inputPath := writeSafetyReviewSingleProfileInput(t, temp, "single-unsafe-01")
	taskDir := filepath.Join(temp, "runs", "single-profile")
	server := newSafetyReviewEvalServer(t, filepath.Join(root, "policy", "releases", "p04b-v1.0"))
	configPath := writeSafetyReviewSingleProfileConfig(t, server.URL, env, inputPath, taskDir)

	var stdout, stderr strings.Builder
	code := cli.RunSafetyReview(
		context.Background(), []string{"run", "--config", configPath}, &stdout, &stderr,
	)
	if code != 0 {
		t.Fatalf("RunSafetyReview(single profile run) = %d, want 0; stderr=%s", code, stderr.String())
	}
	entries := server.preflightEntries()
	roleCounts := map[string]int{}
	models := map[string]bool{}
	for _, entry := range entries {
		roleCounts[entry.role]++
		models[entry.model] = true
	}
	for _, role := range []string{"judge_a", "judge_b", "router", "expert", "arbiter"} {
		if roleCounts[role] != 1 {
			t.Fatalf("preflight role %s count = %d, want 1", role, roleCounts[role])
		}
	}
	if len(models) != 1 || !models["deepseek-v4-pro"] {
		t.Fatalf("preflight models = %#v, want only deepseek-v4-pro", models)
	}
	systems := server.preflightSystemsSnapshot()
	seen := make(map[string]bool, len(systems))
	for role, system := range systems {
		if system == "" {
			t.Fatalf("preflight role %s lacks its role-specific prompt", role)
		}
		if seen[system] {
			t.Fatalf("preflight role %s reused another role prompt", role)
		}
		seen[system] = true
	}
	classificationRoles := server.classificationRoleCounts()
	for _, role := range []string{"judge_a", "judge_b", "router", "expert", "arbiter"} {
		if classificationRoles[role] == 0 {
			t.Fatalf("single-profile classification role %s count = 0", role)
		}
	}
	assertSafetyReviewRunArtifacts(t, taskDir, 1, 0, "unvalidated")
}

// TestSafetyReviewSingleProfileRunQuarantinesProviderFailure 验证单模型失败耗尽后进入隔离而非 Safe。
func TestSafetyReviewSingleProfileRunQuarantinesProviderFailure(t *testing.T) {
	env := "SAFETY_REVIEW_SINGLE_PROFILE_API_KEY"
	t.Setenv(env, "test-key-value")
	root := safetyReviewCLIRepoRoot(t)
	temp := t.TempDir()
	inputPath := writeSafetyReviewSingleProfileInput(t, temp, "failed-01")
	taskDir := filepath.Join(temp, "runs", "single-profile-failure")
	server := newSafetyReviewEvalServer(t, filepath.Join(root, "policy", "releases", "p04b-v1.0"))
	server.setClassificationFailure(true)
	configPath := writeSafetyReviewSingleProfileConfig(t, server.URL, env, inputPath, taskDir)

	var stdout, stderr strings.Builder
	code := cli.RunSafetyReview(
		context.Background(), []string{"run", "--config", configPath}, &stdout, &stderr,
	)
	if code != 0 {
		t.Fatalf("RunSafetyReview(provider failure) = %d, want 0; stderr=%s", code, stderr.String())
	}
	assertSafetyReviewRunArtifacts(t, taskDir, 0, 1, "unvalidated")
	quarantine := readSafetyReviewCLIJSONL(t, filepath.Join(taskDir, "quarantine.jsonl"))
	annotation, _ := quarantine[0]["annotation"].(map[string]any)
	if annotation["method"] != "manual_required" || annotation["label"] == "safe" {
		t.Fatalf("quarantine annotation = %#v, want unresolved non-Safe record", annotation)
	}
}

// TestSafetyReviewSingleProfileEvalRejectsBeforeSideEffects 验证 eval 在任何副作用前拒绝单模型模式。
func TestSafetyReviewSingleProfileEvalRejectsBeforeSideEffects(t *testing.T) {
	env := "SAFETY_REVIEW_SINGLE_PROFILE_API_KEY"
	t.Setenv(env, "test-key-value")
	root := safetyReviewCLIRepoRoot(t)
	temp := t.TempDir()
	inputPath := writeSafetyReviewSingleProfileInput(t, temp, "eval-01")
	taskDir := filepath.Join(temp, "runs", "must-not-exist")
	server := newSafetyReviewEvalServer(t, filepath.Join(root, "policy", "releases", "p04b-v1.0"))
	configPath := writeSafetyReviewSingleProfileConfig(t, server.URL, env, inputPath, taskDir)

	var stdout, stderr strings.Builder
	code := cli.RunSafetyReview(
		context.Background(), []string{"eval", "--config", configPath}, &stdout, &stderr,
	)
	if code != 1 {
		t.Fatalf("RunSafetyReview(single profile eval) = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "error_category=configuration") {
		t.Fatalf("stderr = %q, want configuration error", stderr.String())
	}
	if server.requestCount() != 0 {
		t.Fatalf("network request count = %d, want 0", server.requestCount())
	}
	if _, err := os.Stat(taskDir); !os.IsNotExist(err) {
		t.Fatalf("task directory was created: stat error=%v", err)
	}
}

// writeSafetyReviewSingleProfileInput 写入一条普通未标注输入。
func writeSafetyReviewSingleProfileInput(t *testing.T, directory, traceID string) string {
	t.Helper()
	path := filepath.Join(directory, "single-profile-input.jsonl")
	contents := `{"trace_id":"` + traceID + `","prompt":"synthetic prompt","response":"synthetic response"}` + "\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write single-profile input: %v", err)
	}
	return path
}

// writeSafetyReviewSingleProfileConfig 写入单模型运营配置。
func writeSafetyReviewSingleProfileConfig(t *testing.T, baseURL, env, inputPath, taskDir string) string {
	t.Helper()
	root := safetyReviewCLIRepoRoot(t)
	config := fmt.Sprintf(`version: 1
task:
  id: single-profile
  input: %s
  task_dir: %s
  scene: response
policy:
  bundle_dir: %s
models:
  execution_mode: single_profile
  profiles:
    operational:
      family: deepseek
      base_url: %s
      api_key_env: %s
      name: deepseek-v4-pro
      structured_output: json_object
      max_tokens: 12000
      timeout: 2s
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
  shutdown_timeout: 200ms
  status_interval: 5s
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
`, inputPath, taskDir, filepath.Join(root, "policy", "releases", "p04b-v1.0"), baseURL, env)
	path := filepath.Join(t.TempDir(), "safety-review-single-profile.yaml")
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatalf("write single-profile config: %v", err)
	}
	return path
}
