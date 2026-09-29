package configs_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sendllm/internal/lib/configs"
)

// TestSafetyReviewLoadValidConfig 验证有效配置能完整加载并保留关键路径与限额。
func TestSafetyReviewLoadValidConfig(t *testing.T) {
	got, err := configs.LoadSafetyReview(writeSafetyReviewConfig(t, validSafetyReviewYAML()))
	if err != nil {
		t.Fatalf("LoadSafetyReview() error = %v", err)
	}
	if got.Version != 1 {
		t.Errorf("Version = %d, want 1", got.Version)
	}
	if got.Task.ID != "p04b-pilot-001" || got.Task.Scene != "response" {
		t.Errorf("task identity = %#v", got.Task)
	}
	if !filepath.IsAbs(got.Task.Input) || !filepath.IsAbs(got.Task.TaskDir) || !filepath.IsAbs(got.Policy.BundleDir) {
		t.Errorf("config-relative paths must resolve to absolute paths: %#v", got.Task)
	}
	outputs := map[string]string{
		"clean":          got.Output.Clean,
		"audit":          got.Output.Audit,
		"quality_events": got.Output.QualityEvents,
		"quarantine":     got.Output.Quarantine,
		"report":         got.Output.Report,
		"run_status":     got.Output.RunStatus,
	}
	for name, path := range outputs {
		if !filepath.IsAbs(path) {
			t.Errorf("output %s is not absolute: %q", name, path)
		}
		relative, err := filepath.Rel(got.Task.TaskDir, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			t.Errorf("output %s escapes task directory: %q", name, path)
		}
	}
	if got.Models.Profiles["minimax_m2_5"].QuotaGroup != "minimax_shared" {
		t.Error("minimax profile does not reference quota group")
	}
	if got.Models.Roles["expert"].Concurrency != 4 || *got.Models.Roles["expert"].RequestsPerMinute != 0 {
		t.Errorf("expert limits = %#v", got.Models.Roles["expert"])
	}
	if got.Runtime.ShutdownTimeout != 30*time.Second || got.Runtime.StatusInterval != 5*time.Second {
		t.Errorf("runtime = %#v", got.Runtime)
	}
}

// TestSafetyReviewLoadRejectsUnknownFields 验证各层 YAML 未知字段都会被拒绝。
func TestSafetyReviewLoadRejectsUnknownFields(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "top level",
			yaml: strings.Replace(validSafetyReviewYAML(), "version: 1\n", "version: 1\nunknown: 1\n", 1),
		},
		{
			name: "task",
			yaml: strings.Replace(validSafetyReviewYAML(), "  scene: response\n", "  scene: response\n  unknown: 1\n", 1),
		},
		{
			name: "profile",
			yaml: strings.Replace(validSafetyReviewYAML(), "      timeout: 90s\n", "      timeout: 90s\n      unknown: 1\n", 1),
		},
		{
			name: "role",
			yaml: strings.Replace(
				validSafetyReviewYAML(),
				"      tokens_per_minute: 0\n",
				"      tokens_per_minute: 0\n      unknown: 1\n",
				1,
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := configs.LoadSafetyReview(writeSafetyReviewConfig(t, tt.yaml))
			if !errors.Is(err, configs.ErrInvalidConfig) {
				t.Fatalf("LoadSafetyReview() error = %v, want %v", err, configs.ErrInvalidConfig)
			}
		})
	}
}

// TestSafetyReviewRejectsUserinfoWithoutCredentialLeak 验证端点凭据被拒绝且错误信息不回显。
func TestSafetyReviewRejectsUserinfoWithoutCredentialLeak(t *testing.T) {
	yaml := strings.Replace(
		validSafetyReviewYAML(),
		"https://glm.example.test/v1",
		"https://user:password@glm.example.test/v1",
		1,
	)
	_, err := configs.LoadSafetyReview(writeSafetyReviewConfig(t, yaml))
	if !errors.Is(err, configs.ErrInvalidConfig) {
		t.Fatalf("LoadSafetyReview() error = %v, want %v", err, configs.ErrInvalidConfig)
	}
	message := err.Error()
	if strings.Contains(message, "user") || strings.Contains(message, "password") ||
		strings.Contains(message, "https://user:password@glm.example.test/v1") {
		t.Fatalf("LoadSafetyReview() leaked URL credential material: %s", message)
	}
}

// TestSafetyReviewScenesAndPathResolution 验证场景闭集和配置相对路径的精确解析。
func TestSafetyReviewScenesAndPathResolution(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "nested")
	path := writeSafetyReviewConfigInDirectory(t, configDir, validSafetyReviewYAML())

	promptConfig, err := configs.LoadSafetyReview(
		writeSafetyReviewConfig(t, sceneSafetyReviewYAML("prompt")),
	)
	if err != nil {
		t.Fatalf("LoadSafetyReview(prompt) error = %v", err)
	}
	if promptConfig.Task.Scene != "prompt" {
		t.Fatalf("prompt scene = %q", promptConfig.Task.Scene)
	}

	responseConfig, err := configs.LoadSafetyReview(path)
	if err != nil {
		t.Fatalf("LoadSafetyReview(response) error = %v", err)
	}
	if responseConfig.Task.Scene != "response" {
		t.Fatalf("response scene = %q", responseConfig.Task.Scene)
	}

	wantTaskDir := filepath.Join(configDir, "runs", "p04b-pilot-001")
	if got := responseConfig.Task.Input; got != filepath.Join(configDir, "input.jsonl") {
		t.Errorf("Task.Input = %q", got)
	}
	if got := responseConfig.Task.TaskDir; got != wantTaskDir {
		t.Errorf("Task.TaskDir = %q", got)
	}
	if got := responseConfig.Policy.BundleDir; got != filepath.Join(configDir, "policy", "releases", "p04b-v1.0") {
		t.Errorf("Policy.BundleDir = %q", got)
	}
	wantOutputs := map[string]string{
		"clean":          "clean.jsonl",
		"audit":          "audit.jsonl",
		"quality_events": "quality-events.jsonl",
		"quarantine":     "quarantine.jsonl",
		"report":         "report.json",
		"run_status":     "run-status.json",
	}
	for name, filename := range wantOutputs {
		want := filepath.Join(wantTaskDir, filename)
		var got string
		switch name {
		case "clean":
			got = responseConfig.Output.Clean
		case "audit":
			got = responseConfig.Output.Audit
		case "quality_events":
			got = responseConfig.Output.QualityEvents
		case "quarantine":
			got = responseConfig.Output.Quarantine
		case "report":
			got = responseConfig.Output.Report
		case "run_status":
			got = responseConfig.Output.RunStatus
		}
		if got != want {
			t.Errorf("output %s = %q, want %q", name, got, want)
		}
	}
}

// TestSafetyReviewLoadRejectsInvalidScenes 验证 Safety Review 只接受 prompt 和 response。
func TestSafetyReviewLoadRejectsInvalidScenes(t *testing.T) {
	for _, scene := range []string{"pair", "auto", "unknown"} {
		t.Run(scene, func(t *testing.T) {
			_, err := configs.LoadSafetyReview(
				writeSafetyReviewConfig(t, sceneSafetyReviewYAML(scene)),
			)
			if !errors.Is(err, configs.ErrInvalidConfig) {
				t.Fatalf("LoadSafetyReview(%s) error = %v, want %v", scene, err, configs.ErrInvalidConfig)
			}
		})
	}
}

// TestSafetyReviewValidate 覆盖配置闭集、引用、限额和输出边界的失败分支。
func TestSafetyReviewValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*configs.SafetyReviewConfig)
		wantErr bool
	}{
		{name: "shared api env across profiles", mutate: func(c *configs.SafetyReviewConfig) {
			profile := c.Models.Profiles["glm_5_2"]
			profile.APIKeyEnv = c.Models.Profiles["deepseek_v4_pro"].APIKeyEnv
			c.Models.Profiles["glm_5_2"] = profile
		}},
		{name: "invalid version", mutate: func(c *configs.SafetyReviewConfig) { c.Version = 2 }, wantErr: true},
		{name: "empty task id", mutate: func(c *configs.SafetyReviewConfig) { c.Task.ID = "" }, wantErr: true},
		{name: "empty input", mutate: func(c *configs.SafetyReviewConfig) { c.Task.Input = "" }, wantErr: true},
		{name: "empty task dir", mutate: func(c *configs.SafetyReviewConfig) { c.Task.TaskDir = "" }, wantErr: true},
		{name: "pair scene", mutate: func(c *configs.SafetyReviewConfig) { c.Task.Scene = "pair" }, wantErr: true},
		{name: "auto scene", mutate: func(c *configs.SafetyReviewConfig) { c.Task.Scene = "auto" }, wantErr: true},
		{name: "empty bundle dir", mutate: func(c *configs.SafetyReviewConfig) { c.Policy.BundleDir = "" }, wantErr: true},
		{name: "missing profiles", mutate: func(c *configs.SafetyReviewConfig) { c.Models.Profiles = nil }, wantErr: true},
		{name: "missing roles", mutate: func(c *configs.SafetyReviewConfig) { c.Models.Roles = nil }, wantErr: true},
		{name: "missing quota groups", mutate: func(c *configs.SafetyReviewConfig) {
			c.Models.QuotaGroups = nil
		}, wantErr: true},
		{name: "extra profile", mutate: func(c *configs.SafetyReviewConfig) {
			c.Models.Profiles["unexpected"] = c.Models.Profiles["glm_5_2"]
		}, wantErr: true},
		{name: "missing profile", mutate: func(c *configs.SafetyReviewConfig) {
			delete(c.Models.Profiles, "glm_5_2")
		}, wantErr: true},
		{name: "extra role", mutate: func(c *configs.SafetyReviewConfig) {
			c.Models.Roles["unexpected"] = c.Models.Roles["router"]
		}, wantErr: true},
		{name: "missing role", mutate: func(c *configs.SafetyReviewConfig) {
			delete(c.Models.Roles, "arbiter")
		}, wantErr: true},
		{name: "empty profile family", mutate: func(c *configs.SafetyReviewConfig) {
			profile := c.Models.Profiles["glm_5_2"]
			profile.Family = ""
			c.Models.Profiles["glm_5_2"] = profile
		}, wantErr: true},
		{name: "wrong profile family", mutate: func(c *configs.SafetyReviewConfig) {
			profile := c.Models.Profiles["glm_5_2"]
			profile.Family = "qwen"
			c.Models.Profiles["glm_5_2"] = profile
		}, wantErr: true},
		{name: "empty profile base url", mutate: func(c *configs.SafetyReviewConfig) {
			profile := c.Models.Profiles["glm_5_2"]
			profile.BaseURL = ""
			c.Models.Profiles["glm_5_2"] = profile
		}, wantErr: true},
		{name: "reserved example host", mutate: func(c *configs.SafetyReviewConfig) {
			profile := c.Models.Profiles["glm_5_2"]
			profile.BaseURL = "https://provider.invalid/v1"
			c.Models.Profiles["glm_5_2"] = profile
		}, wantErr: true},
		{name: "invalid profile url", mutate: func(c *configs.SafetyReviewConfig) {
			profile := c.Models.Profiles["glm_5_2"]
			profile.BaseURL = "not-a-url"
			c.Models.Profiles["glm_5_2"] = profile
		}, wantErr: true},
		{name: "empty api env", mutate: func(c *configs.SafetyReviewConfig) {
			profile := c.Models.Profiles["glm_5_2"]
			profile.APIKeyEnv = ""
			c.Models.Profiles["glm_5_2"] = profile
		}, wantErr: true},
		{name: "empty model name", mutate: func(c *configs.SafetyReviewConfig) {
			profile := c.Models.Profiles["glm_5_2"]
			profile.Name = ""
			c.Models.Profiles["glm_5_2"] = profile
		}, wantErr: true},
		{name: "wrong structured output", mutate: func(c *configs.SafetyReviewConfig) {
			profile := c.Models.Profiles["glm_5_2"]
			profile.StructuredOutput = "json_schema"
			c.Models.Profiles["glm_5_2"] = profile
		}, wantErr: true},
		{name: "zero max tokens", mutate: func(c *configs.SafetyReviewConfig) {
			profile := c.Models.Profiles["glm_5_2"]
			profile.MaxTokens = 0
			c.Models.Profiles["glm_5_2"] = profile
		}, wantErr: true},
		{name: "zero timeout", mutate: func(c *configs.SafetyReviewConfig) {
			profile := c.Models.Profiles["glm_5_2"]
			profile.Timeout = 0
			c.Models.Profiles["glm_5_2"] = profile
		}, wantErr: true},
		{name: "unknown quota group", mutate: func(c *configs.SafetyReviewConfig) {
			profile := c.Models.Profiles["minimax_m2_5"]
			profile.QuotaGroup = "missing"
			c.Models.Profiles["minimax_m2_5"] = profile
		}, wantErr: true},
		{name: "empty role primary", mutate: func(c *configs.SafetyReviewConfig) {
			role := c.Models.Roles["judge_a"]
			role.Primary = ""
			c.Models.Roles["judge_a"] = role
		}, wantErr: true},
		{name: "empty role fallbacks", mutate: func(c *configs.SafetyReviewConfig) {
			role := c.Models.Roles["judge_a"]
			role.Fallbacks = nil
			c.Models.Roles["judge_a"] = role
		}, wantErr: true},
		{name: "unknown role primary", mutate: func(c *configs.SafetyReviewConfig) {
			role := c.Models.Roles["judge_a"]
			role.Primary = "missing"
			c.Models.Roles["judge_a"] = role
		}, wantErr: true},
		{name: "unknown role fallback", mutate: func(c *configs.SafetyReviewConfig) {
			role := c.Models.Roles["judge_a"]
			role.Fallbacks = []string{"missing"}
			c.Models.Roles["judge_a"] = role
		}, wantErr: true},
		{name: "duplicate fallback", mutate: func(c *configs.SafetyReviewConfig) {
			role := c.Models.Roles["judge_a"]
			role.Fallbacks = []string{"deepseek_v4_pro", "deepseek_v4_pro"}
			c.Models.Roles["judge_a"] = role
		}, wantErr: true},
		{name: "zero role concurrency", mutate: func(c *configs.SafetyReviewConfig) {
			role := c.Models.Roles["judge_a"]
			role.Concurrency = 0
			c.Models.Roles["judge_a"] = role
		}, wantErr: true},
		{name: "missing role rpm", mutate: func(c *configs.SafetyReviewConfig) {
			role := c.Models.Roles["judge_a"]
			role.RequestsPerMinute = nil
			c.Models.Roles["judge_a"] = role
		}, wantErr: true},
		{name: "missing role tpm", mutate: func(c *configs.SafetyReviewConfig) {
			role := c.Models.Roles["judge_a"]
			role.TokensPerMinute = nil
			c.Models.Roles["judge_a"] = role
		}, wantErr: true},
		{name: "negative role rate", mutate: func(c *configs.SafetyReviewConfig) {
			role := c.Models.Roles["judge_a"]
			rate := -1
			role.RequestsPerMinute = &rate
			c.Models.Roles["judge_a"] = role
		}, wantErr: true},
		{name: "zero quota concurrency", mutate: func(c *configs.SafetyReviewConfig) {
			quota := c.Models.QuotaGroups["minimax_shared"]
			quota.Concurrency = 0
			c.Models.QuotaGroups["minimax_shared"] = quota
		}, wantErr: true},
		{name: "missing quota rpm", mutate: func(c *configs.SafetyReviewConfig) {
			quota := c.Models.QuotaGroups["minimax_shared"]
			quota.RequestsPerMinute = nil
			c.Models.QuotaGroups["minimax_shared"] = quota
		}, wantErr: true},
		{name: "missing quota tpm", mutate: func(c *configs.SafetyReviewConfig) {
			quota := c.Models.QuotaGroups["minimax_shared"]
			quota.TokensPerMinute = nil
			c.Models.QuotaGroups["minimax_shared"] = quota
		}, wantErr: true},
		{name: "negative quota rate", mutate: func(c *configs.SafetyReviewConfig) {
			quota := c.Models.QuotaGroups["minimax_shared"]
			rate := -1
			quota.TokensPerMinute = &rate
			c.Models.QuotaGroups["minimax_shared"] = quota
		}, wantErr: true},
		{name: "zero shutdown timeout", mutate: func(c *configs.SafetyReviewConfig) {
			c.Runtime.ShutdownTimeout = 0
		}, wantErr: true},
		{name: "zero status interval", mutate: func(c *configs.SafetyReviewConfig) {
			c.Runtime.StatusInterval = 0
		}, wantErr: true},
		{name: "zero transient attempts", mutate: func(c *configs.SafetyReviewConfig) {
			c.Retry.TransientAttemptsPerModel = 0
		}, wantErr: true},
		{name: "zero repair attempts", mutate: func(c *configs.SafetyReviewConfig) {
			c.Retry.FormatRepairAttempts = 0
		}, wantErr: true},
		{name: "zero refusal attempts", mutate: func(c *configs.SafetyReviewConfig) {
			c.Retry.RefusalRepromptAttempts = 0
		}, wantErr: true},
		{name: "zero initial backoff", mutate: func(c *configs.SafetyReviewConfig) {
			c.Retry.InitialBackoff = 0
		}, wantErr: true},
		{name: "max backoff below initial", mutate: func(c *configs.SafetyReviewConfig) {
			c.Retry.MaxBackoff = time.Millisecond
		}, wantErr: true},
		{name: "empty clean output", mutate: func(c *configs.SafetyReviewConfig) {
			c.Output.Clean = ""
		}, wantErr: true},
		{name: "empty audit output", mutate: func(c *configs.SafetyReviewConfig) {
			c.Output.Audit = ""
		}, wantErr: true},
		{name: "empty quality output", mutate: func(c *configs.SafetyReviewConfig) {
			c.Output.QualityEvents = ""
		}, wantErr: true},
		{name: "empty quarantine output", mutate: func(c *configs.SafetyReviewConfig) {
			c.Output.Quarantine = ""
		}, wantErr: true},
		{name: "empty report output", mutate: func(c *configs.SafetyReviewConfig) {
			c.Output.Report = ""
		}, wantErr: true},
		{name: "empty run status output", mutate: func(c *configs.SafetyReviewConfig) {
			c.Output.RunStatus = ""
		}, wantErr: true},
		{name: "output escapes task dir", mutate: func(c *configs.SafetyReviewConfig) {
			c.Output.Clean = filepath.Join(c.Task.TaskDir, "..", "clean.jsonl")
		}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := configs.LoadSafetyReview(writeSafetyReviewConfig(t, validSafetyReviewYAML()))
			if err != nil {
				t.Fatalf("LoadSafetyReview() baseline error = %v", err)
			}
			tt.mutate(config)
			err = config.Validate()
			if tt.wantErr && !errors.Is(err, configs.ErrInvalidConfig) {
				t.Fatalf("Validate() error = %v, want %v", err, configs.ErrInvalidConfig)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() error = %v, want nil", err)
			}
		})
	}
}

// TestSafetyReviewRoleFallbacksUseDifferentFamilies 验证所有角色备用模型均跨家族。
func TestSafetyReviewRoleFallbacksUseDifferentFamilies(t *testing.T) {
	config, err := configs.LoadSafetyReview(writeSafetyReviewConfig(t, validSafetyReviewYAML()))
	if err != nil {
		t.Fatalf("LoadSafetyReview() error = %v", err)
	}
	for roleID, role := range config.Models.Roles {
		primaryFamily := config.Models.Profiles[role.Primary].Family
		for _, fallbackID := range role.Fallbacks {
			fallbackFamily := config.Models.Profiles[fallbackID].Family
			if fallbackFamily == primaryFamily {
				t.Errorf("role %s fallback %s uses the same family %s", roleID, fallbackID, fallbackFamily)
			}
		}
	}
}

// TestSafetyReviewSemanticFingerprintSeparatesSemanticAndRuntimeValues 验证语义和运行参数的指纹隔离。
func TestSafetyReviewSemanticFingerprintSeparatesSemanticAndRuntimeValues(t *testing.T) {
	config, err := configs.LoadSafetyReview(writeSafetyReviewConfig(t, validSafetyReviewYAML()))
	if err != nil {
		t.Fatalf("LoadSafetyReview() error = %v", err)
	}
	snapshot := map[string][]byte{
		"policy/common.yaml":    []byte("common-policy"),
		"schemas/judgment.json": []byte(`{"type":"object"}`),
	}
	base, err := config.SemanticFingerprint(snapshot)
	if err != nil {
		t.Fatalf("base SemanticFingerprint() error = %v", err)
	}

	runtimeConfig, err := configs.LoadSafetyReview(writeSafetyReviewConfig(t, validSafetyReviewYAML()))
	if err != nil {
		t.Fatalf("LoadSafetyReview() runtime error = %v", err)
	}
	role := runtimeConfig.Models.Roles["judge_a"]
	role.Concurrency = 17
	role.RequestsPerMinute = new(int)
	role.TokensPerMinute = new(int)
	runtimeConfig.Models.Roles["judge_a"] = role
	runtimeConfig.Runtime.ShutdownTimeout = 11 * time.Second
	runtimeConfig.Runtime.StatusInterval = 2 * time.Second
	runtimeConfig.Retry.InitialBackoff = 3 * time.Second
	runtimeConfig.Retry.MaxBackoff = 9 * time.Second
	runtimeConfig.Output.Clean = "changed-clean.jsonl"
	runtimeFingerprint, err := runtimeConfig.SemanticFingerprint(snapshot)
	if err != nil {
		t.Fatalf("runtime SemanticFingerprint() error = %v", err)
	}
	if runtimeFingerprint != base {
		t.Errorf("runtime values changed fingerprint: %q != %q", runtimeFingerprint, base)
	}

	semanticConfig, err := configs.LoadSafetyReview(writeSafetyReviewConfig(t, validSafetyReviewYAML()))
	if err != nil {
		t.Fatalf("LoadSafetyReview() semantic error = %v", err)
	}
	profile := semanticConfig.Models.Profiles["deepseek_v4_pro"]
	profile.Name = "deepseek-v4-pro-changed"
	semanticConfig.Models.Profiles["deepseek_v4_pro"] = profile
	semanticFingerprint, err := semanticConfig.SemanticFingerprint(snapshot)
	if err != nil {
		t.Fatalf("semantic SemanticFingerprint() error = %v", err)
	}
	if semanticFingerprint == base {
		t.Error("semantic model mutation did not change fingerprint")
	}

	changedSnapshot := map[string][]byte{
		"policy/common.yaml":    []byte("changed-policy"),
		"schemas/judgment.json": []byte(`{"type":"object"}`),
	}
	snapshotFingerprint, err := config.SemanticFingerprint(changedSnapshot)
	if err != nil {
		t.Fatalf("snapshot SemanticFingerprint() error = %v", err)
	}
	if snapshotFingerprint == base {
		t.Error("snapshot mutation did not change fingerprint")
	}
}

// TestSafetyReviewExampleConfigUsesReservedHost 验证通用示例保留不可路由占位主机。
func TestSafetyReviewExampleConfigsUseReservedHost(t *testing.T) {
	path := filepath.Join("..", "..", "..", "config", "safety-review.example.yaml")
	_, err := configs.LoadSafetyReview(path)
	if !errors.Is(err, configs.ErrInvalidConfig) || !strings.Contains(err.Error(), "provider.invalid") {
		t.Fatalf("LoadSafetyReview(safety-review.example.yaml) error = %v, want reserved provider.invalid rejection", err)
	}
}

// writeSafetyReviewConfig 在临时目录写入配置并返回路径。
func writeSafetyReviewConfig(t *testing.T, contents string) string {
	t.Helper()
	return writeSafetyReviewConfigInDirectory(t, t.TempDir(), contents)
}

// writeSafetyReviewConfigInDirectory 在指定目录写入配置并返回绝对路径。
func writeSafetyReviewConfigInDirectory(t *testing.T, directory, contents string) string {
	t.Helper()
	if err := os.MkdirAll(directory, 0o750); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", directory, err)
	}
	path := filepath.Join(directory, "safety-review.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

// sceneSafetyReviewYAML 返回指定审查场景的完整有效配置。
func sceneSafetyReviewYAML(scene string) string {
	return strings.Replace(validSafetyReviewYAML(), "  scene: response\n", "  scene: "+scene+"\n", 1)
}

// validSafetyReviewYAML 返回一份完整的有效 Safety Review 配置。
func validSafetyReviewYAML() string {
	return `version: 1
task:
  id: p04b-pilot-001
  input: input.jsonl
  task_dir: runs/p04b-pilot-001
  scene: response
policy:
  bundle_dir: policy/releases/p04b-v1.0
models:
  profiles:
    glm_5_2:
      family: glm
      base_url: https://glm.example.test/v1
      api_key_env: AI_GATEWAY_API_KEY
      name: GLM-5.3-Flash
      structured_output: json_object
      max_tokens: 2000
      timeout: 90s
    qwen3_max:
      family: qwen
      base_url: https://qwen.example.test/v1
      api_key_env: AI_GATEWAY_API_KEY
      name: qwen3-max
      structured_output: json_object
      max_tokens: 2000
      timeout: 90s
    minimax_m2_5:
      family: minimax
      base_url: https://minimax.example.test/v1
      api_key_env: AI_GATEWAY_API_KEY
      name: MiniMax-M2.5
      structured_output: json_object
      max_tokens: 2000
      timeout: 90s
      quota_group: minimax_shared
    deepseek_v4_pro:
      family: deepseek
      base_url: https://aigateway.venusgroup.com.cn/ai/aliyun/openai
      api_key_env: AI_GATEWAY_API_KEY
      name: deepseek-v4-pro
      structured_output: json_object
      max_tokens: 2000
      timeout: 90s
  roles:
    judge_a:
      primary: glm_5_2
      fallbacks: [deepseek_v4_pro]
      concurrency: 4
      requests_per_minute: 0
      tokens_per_minute: 0
    judge_b:
      primary: qwen3_max
      fallbacks: [minimax_m2_5]
      concurrency: 4
      requests_per_minute: 0
      tokens_per_minute: 0
    router:
      primary: minimax_m2_5
      fallbacks: [qwen3_max]
      concurrency: 2
      requests_per_minute: 0
      tokens_per_minute: 0
    expert:
      primary: minimax_m2_5
      fallbacks: [deepseek_v4_pro]
      concurrency: 4
      requests_per_minute: 0
      tokens_per_minute: 0
    arbiter:
      primary: deepseek_v4_pro
      fallbacks: [glm_5_2]
      concurrency: 2
      requests_per_minute: 0
      tokens_per_minute: 0
  quota_groups:
    minimax_shared:
      concurrency: 4
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
