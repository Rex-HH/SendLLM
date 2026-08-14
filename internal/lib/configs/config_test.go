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

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr error
	}{
		{name: "applies defaults", yaml: validYAML(""), wantErr: nil},
		{name: "rejects zero concurrency", yaml: validYAML("concurrency: 0"), wantErr: configs.ErrInvalidConfig},
		{name: "rejects concurrency above account ceiling", yaml: validYAML("concurrency: 501"), wantErr: configs.ErrInvalidConfig},
		{name: "rejects zero cover concurrency", yaml: validYAML("cover_concurrency: 0"), wantErr: configs.ErrInvalidConfig},
		{name: "rejects negative cover RPM", yaml: validYAML("cover_requests_per_minute: -1"), wantErr: configs.ErrInvalidConfig},
		{name: "rejects missing result schema", yaml: validYAML("schema_file: missing.json"), wantErr: configs.ErrInvalidConfig},
		{name: "rejects retry maximum below one", yaml: validYAML("request_max_attempts: 0"), wantErr: configs.ErrInvalidConfig},
		{name: "rejects missing risk placeholder", yaml: validYAML("system_file: missing-risk.txt"), wantErr: configs.ErrInvalidConfig},
		{name: "rejects duplicate schema placeholder", yaml: validYAML("system_file: duplicate-schema.txt"), wantErr: configs.ErrInvalidConfig},
		{name: "rejects managed extra body key", yaml: validYAML("extra_body:\n    model: managed-elsewhere"), wantErr: configs.ErrInvalidConfig},
		{name: "rejects zero max tokens", yaml: validYAML("max_tokens: 0"), wantErr: configs.ErrInvalidConfig},
		{name: "rejects negative max tokens", yaml: validYAML("max_tokens: -1"), wantErr: configs.ErrInvalidConfig},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := writeConfig(t, tt.yaml)
			got, err := configs.Load(path)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Load() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if got.Runtime.Concurrency != 64 {
				t.Errorf("Concurrency = %d, want 64", got.Runtime.Concurrency)
			}
			if got.Runtime.CoverConcurrency != 0 || got.Runtime.CoverRequestsPerMinute != 0 {
				t.Errorf(
					"cover limits = %d/%d, want unset zeros",
					got.Runtime.CoverConcurrency,
					got.Runtime.CoverRequestsPerMinute,
				)
			}
			if got.Runtime.ShutdownTimeout != 30*time.Second {
				t.Errorf("ShutdownTimeout = %s, want 30s", got.Runtime.ShutdownTimeout)
			}
			if got.Retry.RequestMaxAttempts != 5 {
				t.Errorf("RequestMaxAttempts = %d, want 5", got.Retry.RequestMaxAttempts)
			}
			if got.Output.ExplanationMinLength != 10 || got.Output.ExplanationMaxLength != 70 {
				t.Errorf("explanation lengths = %d/%d, want 10/70", got.Output.ExplanationMinLength, got.Output.ExplanationMaxLength)
			}
			if !strings.Contains(string(got.SystemPrompt), `"jailbreak":"越狱攻击"`) {
				t.Error("SystemPrompt does not contain the substituted risk types")
			}
		})
	}
}

func TestLoadAcceptsConfiguredCoverRetryLimits(t *testing.T) {
	got, err := configs.Load(writeConfig(t, validYAML("cover_concurrency: 2\n  cover_requests_per_minute: 18")))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Runtime.CoverConcurrency != 2 || got.Runtime.CoverRequestsPerMinute != 18 {
		t.Errorf(
			"cover limits = %d/%d, want 2/18",
			got.Runtime.CoverConcurrency,
			got.Runtime.CoverRequestsPerMinute,
		)
	}
}

func TestLoadResolvesRelativePaths(t *testing.T) {
	got, err := configs.Load(writeConfig(t, validYAML("")))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !filepath.IsAbs(got.Task.Input) || !filepath.IsAbs(got.Task.Output) || !filepath.IsAbs(got.Task.State) {
		t.Errorf("task paths must be absolute: %#v", got.Task)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	_, err := configs.Load(writeConfig(t, validYAML("unknown_limit: 1")))
	if !errors.Is(err, configs.ErrInvalidConfig) {
		t.Fatalf("Load() error = %v, want %v", err, configs.ErrInvalidConfig)
	}
}

func TestSemanticFingerprintIgnoresRuntimeSettings(t *testing.T) {
	first, err := configs.Load(writeConfig(t, validYAML("concurrency: 1")))
	if err != nil {
		t.Fatalf("Load() first error = %v", err)
	}
	second, err := configs.Load(writeConfig(t, validYAML("concurrency: 500")))
	if err != nil {
		t.Fatalf("Load() second error = %v", err)
	}
	firstFingerprint, err := first.SemanticFingerprint()
	if err != nil {
		t.Fatalf("first.SemanticFingerprint() error = %v", err)
	}
	secondFingerprint, err := second.SemanticFingerprint()
	if err != nil {
		t.Fatalf("second.SemanticFingerprint() error = %v", err)
	}
	if firstFingerprint != secondFingerprint {
		t.Errorf("runtime settings changed fingerprint: %q != %q", firstFingerprint, secondFingerprint)
	}
}

func TestAPIKey(t *testing.T) {
	config, err := configs.Load(writeConfig(t, validYAML("")))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	t.Setenv("SENDLLM_TEST_KEY", "present")
	got, err := config.APIKey()
	if err != nil {
		t.Fatalf("APIKey() error = %v", err)
	}
	if got != "present" {
		t.Errorf("APIKey() = %q, want present", got)
	}
}

func TestExampleConfig(t *testing.T) {
	path := filepath.Join("..", "..", "..", "config", "task.example.yaml")
	got, err := configs.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Model.BaseURL != "https://aigateway.venusgroup.com.cn/ai/deepseek/openai" {
		t.Errorf("BaseURL = %q", got.Model.BaseURL)
	}
	if got.Model.Name != "deepseek-v4-pro" || got.Model.APIKeyEnv != "AI_GATEWAY_API_KEY" {
		t.Errorf("model identity = %q/%q", got.Model.Name, got.Model.APIKeyEnv)
	}
	if got.Model.StructuredOutput != "json_object" || got.Model.MaxTokens != 2000 {
		t.Errorf(
			"model recommendation = %q/%d, want json_object/2000",
			got.Model.StructuredOutput,
			got.Model.MaxTokens,
		)
	}
	if got.Runtime.Concurrency != 4 {
		t.Errorf("runtime concurrency = %d, want 4", got.Runtime.Concurrency)
	}
}

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	directory := t.TempDir()
	writeFile(t, filepath.Join(directory, "risk-types.yaml"), "jailbreak: 越狱攻击\n")
	writeFile(t, filepath.Join(directory, "result-schema.json"), `{"type":"object"}`)
	writeFile(t, filepath.Join(directory, "system.txt"), "风险：{{RISK_TYPES}}\nSchema：{{RESULT_SCHEMA}}")
	writeFile(t, filepath.Join(directory, "missing-risk.txt"), "Schema：{{RESULT_SCHEMA}}")
	writeFile(t, filepath.Join(directory, "duplicate-schema.txt"), "{{RISK_TYPES}} {{RESULT_SCHEMA}} {{RESULT_SCHEMA}}")
	path := filepath.Join(directory, "task.yaml")
	writeFile(t, path, contents)
	return path
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func validYAML(replacement string) string {
	base := `task:
  id: test-task
  input: input.jsonl
  output: output.jsonl
  state: state.db
model:
  base_url: https://example.test/v1
  api_key_env: SENDLLM_TEST_KEY
  name: test-model
  structured_output: json_schema
  max_tokens: 100
prompt:
  system_file: system.txt
  scene: auto
  risk_types_file: risk-types.yaml
runtime:
retry:
output:
  schema_file: result-schema.json
`
	if replacement == "" {
		return base
	}
	if strings.HasPrefix(replacement, "max_tokens") {
		return strings.Replace(base, "max_tokens: 100", replacement, 1)
	}

	parts := strings.SplitN(replacement, ":", 2)
	if len(parts) != 2 {
		return base
	}
	section := "runtime"
	if strings.HasPrefix(replacement, "schema_file") {
		section = "output"
	}
	if strings.HasPrefix(replacement, "request_max_attempts") {
		section = "retry"
	}
	if strings.HasPrefix(replacement, "system_file") {
		section = "prompt"
	}
	if strings.HasPrefix(replacement, "extra_body") {
		section = "model"
	}
	return strings.Replace(base, section+":\n", section+":\n  "+replacement+"\n", 1)
}
