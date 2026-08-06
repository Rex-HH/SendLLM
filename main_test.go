package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const (
	cliAPIKey      = "synthetic-secret-token"
	cliAPIKeyEnv   = "SENDLLM_TEST_API_KEY"
	cliPrompt      = "fixture prompt that must stay private"
	cliResponse    = "fixture response that must stay private"
	cliModelOutput = `{"label":"safe","explanation":"synthetic safe explanation"}`
	cliSchema      = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["label", "explanation"],
  "properties": {
    "label": {"enum": ["safe", "unsafe"]},
    "explanation": {"type": "string"},
    "extended_info": {"type": "object"}
  }
}`
)

func TestRunReturnsZeroAndWiresConfiguredSchema(t *testing.T) {
	server := newCLIProvider(t, cliModelOutput, "json_schema")
	defer server.Close()
	t.Setenv(cliAPIKeyEnv, cliAPIKey)
	paths := writeCLIConfig(t, server.URL, cliAPIKeyEnv)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(context.Background(), []string{"-config", paths.config}, &stdout, &stderr)

	if code != 0 {
		t.Errorf("run() code = %d, want 0; stderr=%q", code, stderr.String())
	}
	records := readMainJSONL(t, paths.output)
	if len(records) != 1 || records[0]["trace_id"] != "trace-success" || records[0]["label"] != "safe" {
		t.Errorf("output records = %#v, want one successful trace", records)
	}
	assertNoCLIPayload(t, stdout.String(), stderr.String())
}

func TestRunWiresConfiguredStructuredOutputMode(t *testing.T) {
	server := newCLIProvider(t, cliModelOutput, "json_object")
	defer server.Close()
	t.Setenv(cliAPIKeyEnv, cliAPIKey)
	paths := writeCLIConfigWithMode(t, server.URL, cliAPIKeyEnv, "json_object")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(context.Background(), []string{"-config", paths.config}, &stdout, &stderr)

	if code != 0 {
		t.Errorf("run() code = %d, want 0; stderr=%q", code, stderr.String())
	}
	assertNoCLIPayload(t, stdout.String(), stderr.String())
}

func TestRunReturnsTwoWhenFinalFailuresExist(t *testing.T) {
	server := newCLIProvider(t, `{"label":"invalid","explanation":"synthetic invalid explanation"}`, "json_schema")
	defer server.Close()
	t.Setenv(cliAPIKeyEnv, cliAPIKey)
	paths := writeCLIConfig(t, server.URL, cliAPIKeyEnv)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(context.Background(), []string{"-config", paths.config}, &stdout, &stderr)

	if code != 2 {
		t.Errorf("run() code = %d, want 2; stderr=%q", code, stderr.String())
	}
	failed := readMainJSONL(t, paths.failed)
	if len(failed) != 1 || failed[0]["trace_id"] != "trace-success" {
		t.Errorf("failed records = %#v, want one safe diagnostic", failed)
	}
	assertNoCLIPayload(t, stdout.String(), stderr.String())
}

func TestRunReturnsOneWhenAPIKeyIsMissing(t *testing.T) {
	t.Setenv(cliAPIKeyEnv, "")
	paths := writeCLIConfig(t, "https://example.invalid/v1", cliAPIKeyEnv)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(context.Background(), []string{"-config", paths.config}, &stdout, &stderr)

	if code != 1 {
		t.Errorf("run() code = %d, want 1; stderr=%q", code, stderr.String())
	}
	assertNoCLIPayload(t, stdout.String(), stderr.String())
}

func TestRunReturns130ForCanceledContext(t *testing.T) {
	t.Setenv(cliAPIKeyEnv, cliAPIKey)
	paths := writeCLIConfig(t, "https://example.invalid/v1", cliAPIKeyEnv)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(ctx, []string{"-config", paths.config}, &stdout, &stderr)

	if code != 130 {
		t.Errorf("run() code = %d, want 130; stderr=%q", code, stderr.String())
	}
	assertNoCLIPayload(t, stdout.String(), stderr.String())
}

func TestRunExportsExistingTerminalRecordsWhenResumeImportFails(t *testing.T) {
	server := newCLIProvider(t, cliModelOutput, "json_schema")
	defer server.Close()
	t.Setenv(cliAPIKeyEnv, cliAPIKey)
	paths := writeCLIConfig(t, server.URL, cliAPIKeyEnv)

	var firstStdout bytes.Buffer
	var firstStderr bytes.Buffer
	if code := run(context.Background(), []string{"-config", paths.config}, &firstStdout, &firstStderr); code != 0 {
		t.Fatalf("run(first) code = %d, want 0; stderr=%q", code, firstStderr.String())
	}
	writeMainFile(t, paths.input, `{"trace_id":"trace-success","prompt":"changed synthetic prompt"}`+"\n")
	if err := os.Remove(paths.output); err != nil {
		t.Fatalf("Remove(output) error = %v", err)
	}
	if err := os.Remove(paths.failed); err != nil {
		t.Fatalf("Remove(failed) error = %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(context.Background(), []string{"-config", paths.config}, &stdout, &stderr)

	if code != 1 {
		t.Errorf("run(resume conflict) code = %d, want 1; stderr=%q", code, stderr.String())
	}
	records := readMainJSONL(t, paths.output)
	if len(records) != 1 || records[0]["trace_id"] != "trace-success" {
		t.Errorf("output records = %#v, want prior terminal record", records)
	}
	assertNoCLIPayload(t, stdout.String(), stderr.String())
}

type cliPaths struct {
	config string
	input  string
	output string
	failed string
}

func writeCLIConfig(t *testing.T, baseURL, apiKeyEnv string) cliPaths {
	t.Helper()
	return writeCLIConfigWithMode(t, baseURL, apiKeyEnv, "json_schema")
}

func writeCLIConfigWithMode(t *testing.T, baseURL, apiKeyEnv, mode string) cliPaths {
	t.Helper()
	directory := t.TempDir()
	writeMainFile(t, filepath.Join(directory, "input.jsonl"),
		fmt.Sprintf("{\"trace_id\":\"trace-success\",\"prompt\":%q,\"response\":%q}\n", cliPrompt, cliResponse),
	)
	writeMainFile(t, filepath.Join(directory, "schema.json"), cliSchema)
	writeMainFile(t, filepath.Join(directory, "risk-types.yaml"), "test-risk: synthetic risk\n")
	writeMainFile(t, filepath.Join(directory, "system.txt"), "Risk types:\n{{RISK_TYPES}}\nSchema:\n{{RESULT_SCHEMA}}\n")

	config := fmt.Sprintf(`task:
  id: cli-test
  input: input.jsonl
  output: output.jsonl
  state: state.db
model:
  base_url: %s
  api_key_env: %s
  name: synthetic-model
  structured_output: %s
  max_tokens: 100
  timeout: 2s
prompt:
  system_file: system.txt
  scene: auto
  risk_types_file: risk-types.yaml
runtime:
  concurrency: 1
  requests_per_minute: 0
  tokens_per_minute: 0
  shutdown_timeout: 1s
retry:
  request_max_attempts: 1
  format_repair_attempts: 0
  initial_backoff: 1ms
  max_backoff: 1ms
output:
  schema_file: schema.json
  explanation_min_length: 10
  explanation_max_length: 70
`, baseURL, apiKeyEnv, mode)
	configPath := filepath.Join(directory, "task.yaml")
	writeMainFile(t, configPath, config)
	return cliPaths{
		config: configPath,
		input:  filepath.Join(directory, "input.jsonl"),
		output: filepath.Join(directory, "output.jsonl"),
		failed: filepath.Join(directory, "output.failed.jsonl"),
	}
}

func newCLIProvider(t *testing.T, modelOutput, mode string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+cliAPIKey {
			http.Error(writer, "bad authorization", http.StatusUnauthorized)
			return
		}
		var body struct {
			ResponseFormat struct {
				Type       string `json:"type"`
				JSONSchema struct {
					Schema json.RawMessage `json:"schema"`
				} `json:"json_schema"`
			} `json:"response_format"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			http.Error(writer, "bad JSON", http.StatusBadRequest)
			return
		}
		wantResponseType := mode
		if mode == "prompt_only" {
			wantResponseType = ""
		}
		if body.ResponseFormat.Type != wantResponseType {
			http.Error(writer, "bad response schema", http.StatusBadRequest)
			return
		}
		if mode == "json_schema" && !sameJSON(body.ResponseFormat.JSONSchema.Schema, []byte(cliSchema)) {
			http.Error(writer, "bad response schema", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"choices": []any{map[string]any{
				"message":       map[string]any{"role": "assistant", "content": modelOutput},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 5,
				"total_tokens":      15,
			},
		})
	}))
}

func sameJSON(left, right []byte) bool {
	var leftValue any
	if err := json.Unmarshal(left, &leftValue); err != nil {
		return false
	}
	var rightValue any
	if err := json.Unmarshal(right, &rightValue); err != nil {
		return false
	}
	return reflect.DeepEqual(leftValue, rightValue)
}

func writeMainFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func readMainJSONL(t *testing.T, path string) []map[string]any {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	lines := strings.Split(strings.TrimSpace(string(contents)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	records := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("Unmarshal(output) error = %v", err)
		}
		records = append(records, record)
	}
	return records
}

func assertNoCLIPayload(t *testing.T, outputs ...string) {
	t.Helper()
	for _, output := range outputs {
		for _, secret := range []string{cliPrompt, cliResponse, cliAPIKey, cliModelOutput, "Authorization"} {
			if strings.Contains(output, secret) {
				t.Errorf("CLI output contains sensitive fixture %q", secret)
			}
		}
	}
}
