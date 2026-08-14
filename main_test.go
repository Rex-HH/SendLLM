package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"sendllm/internal/lib/configs"
)

const (
	cliAPIKey      = "synthetic-secret-token"
	cliAPIKeyEnv   = "SENDLLM_TEST_API_KEY"
	cliPrompt      = "fixture prompt that must stay private"
	cliResponse    = "fixture response that must stay private"
	cliModelOutput = `{"is_attack":false,"case_type":"typical","explanation":"synthetic safe explanation"}`
	cliSchema      = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["is_attack", "case_type", "explanation"],
  "properties": {
    "is_attack": {"type": "boolean"},
    "case_type": {"type": "string", "enum": ["typical", "borderline", "variant", "hard_negative"]},
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
	annotation, _ := records[0]["annotation"].(map[string]any)
	if len(records) != 1 || records[0]["id"] != "dataset:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" ||
		annotation["is_attack"] != false || annotation["method"] != "auto" {
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
	server := newCLIProvider(t, `{"is_attack":true,"case_type":"typical","explanation":"synthetic invalid explanation"}`, "json_schema")
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
	if len(failed) != 1 || failed[0]["id"] != "dataset:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("failed records = %#v, want one safe diagnostic", failed)
	}
	assertNoCLIPayload(t, stdout.String(), stderr.String())
}

func TestRunCoversFinalFailuresBeforeExport(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		if calls == 1 {
			http.Error(writer, "slow down", http.StatusTooManyRequests)
			return
		}
		writeCLICompletion(t, writer, cliModelOutput)
	}))
	defer server.Close()
	t.Setenv(cliAPIKeyEnv, cliAPIKey)
	paths := writeCLIConfig(t, server.URL, cliAPIKeyEnv)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(context.Background(), []string{"-config", paths.config}, &stdout, &stderr)

	if code != 0 {
		t.Errorf("run() code = %d, want 0; stderr=%q", code, stderr.String())
	}
	if calls != 2 {
		t.Errorf("provider calls = %d, want initial failure and cover retry", calls)
	}
	records := readMainJSONL(t, paths.output)
	if len(records) != 1 || records[0]["id"] != "dataset:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("output records = %#v, want covered success", records)
	}
	if failed := readMainJSONL(t, paths.failed); len(failed) != 0 {
		t.Errorf("failed records = %#v, want empty after cover", failed)
	}
	assertNoCLIPayload(t, stdout.String(), stderr.String())
}

func TestRunAdjudicateOutputsMASBFormat(t *testing.T) {
	modelOutput := `{"is_attack":true,"case_type":"typical","explanation":"综合判断该样本存在提示注入风险。","extended_info":{"risk_type":"test-risk","risk_level":"high","attack_scenario":"instruction_override","other":""}}`
	server := newCLIProvider(t, modelOutput, "json_schema")
	defer server.Close()
	t.Setenv(cliAPIKeyEnv, cliAPIKey)
	paths := writeCLIConfig(t, server.URL, cliAPIKeyEnv)
	writeMainFile(t, paths.input, differenceCLIInput())

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(context.Background(), []string{"-mode", "adjudicate", "-config", paths.config}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run() code = %d, want 0; stderr=%q", code, stderr.String())
	}
	records := readMainJSONL(t, paths.output)
	if len(records) != 1 {
		t.Fatalf("output records = %d, want 1", len(records))
	}
	record := records[0]
	if record["trace_id"] != "dataset:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" ||
		record["label"] != "unsafe" || record["prompt"] != cliPrompt || record["response"] != cliResponse {
		t.Errorf("MASB core fields = %#v", record)
	}
	extended, _ := record["extended_info"].(map[string]any)
	if extended["risk_type"] != "test-risk" || extended["risk_level"] != "high" ||
		extended["case_type"] != "typical" || extended["is_attack"] != true {
		t.Errorf("extended_info = %#v", extended)
	}
	meta, _ := record["annotation"].(map[string]any)
	if meta["method"] != "auto" {
		t.Errorf("annotation = %#v, want method auto", meta)
	}
	assertNoCLIPayload(t, stdout.String(), stderr.String())
}

func TestCoverRetryLimitsUseConfiguredOverrides(t *testing.T) {
	tests := []struct {
		name            string
		runtime         configs.RuntimeConfig
		wantConcurrency int
		wantRPM         int
	}{
		{
			name: "defaults stay conservative",
			runtime: configs.RuntimeConfig{
				Concurrency:       400,
				RequestsPerMinute: 40_000,
			},
			wantConcurrency: 1,
			wantRPM:         10,
		},
		{
			name: "explicit cover limits override defaults",
			runtime: configs.RuntimeConfig{
				Concurrency:            400,
				RequestsPerMinute:      40_000,
				CoverConcurrency:       2,
				CoverRequestsPerMinute: 18,
			},
			wantConcurrency: 2,
			wantRPM:         18,
		},
		{
			name: "legacy low RPM still applies when cover RPM is unset",
			runtime: configs.RuntimeConfig{
				Concurrency:       400,
				RequestsPerMinute: 6,
			},
			wantConcurrency: 1,
			wantRPM:         6,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := coverConcurrency(test.runtime); got != test.wantConcurrency {
				t.Errorf("coverConcurrency() = %d, want %d", got, test.wantConcurrency)
			}
			if got := coverRequestsPerMinute(test.runtime); got != test.wantRPM {
				t.Errorf("coverRequestsPerMinute() = %d, want %d", got, test.wantRPM)
			}
		})
	}
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

func TestRunConstructionFailuresDoNotPersistTaskItems(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, cliPaths)
	}{
		{
			name: "invalid schema",
			mutate: func(t *testing.T, paths cliPaths) {
				writeMainFile(t, paths.schema, `{"type":"unsupported"}`)
			},
		},
		{
			name: "invalid client URL",
			mutate: func(t *testing.T, paths cliPaths) {
				replaceMainConfig(t, paths.config, "base_url: https://example.invalid/v1", "base_url: https://[::1")
			},
		},
		{
			name: "invalid runner token limit",
			mutate: func(t *testing.T, paths cliPaths) {
				replaceMainConfig(t, paths.config, "max_tokens: 100", "max_tokens: 0")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(cliAPIKeyEnv, cliAPIKey)
			paths := writeCLIConfig(t, "https://example.invalid/v1", cliAPIKeyEnv)
			test.mutate(t, paths)

			code := run(context.Background(), []string{"-config", paths.config}, &bytes.Buffer{}, &bytes.Buffer{})
			if code != 1 {
				t.Fatalf("run() code = %d, want 1", code)
			}
			assertNoPersistedTaskItems(t, paths.state)
		})
	}
}

func TestRunDrainsInFlightRequestWithinShutdownTimeout(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		close(started)
		select {
		case <-request.Context().Done():
			return
		case <-release:
		}
		writeCLICompletion(t, writer, cliModelOutput)
	}))
	defer server.Close()
	t.Setenv(cliAPIKeyEnv, cliAPIKey)
	paths := writeCLIConfig(t, server.URL, cliAPIKeyEnv)
	setCLIShutdownTimeout(t, paths.config, "200ms")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() {
		done <- run(ctx, []string{"-config", paths.config}, &bytes.Buffer{}, &bytes.Buffer{})
	}()

	<-started
	cancel()
	select {
	case code := <-done:
		close(release)
		t.Fatalf("run() returned %d before in-flight request drained", code)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if code := <-done; code != 130 {
		t.Fatalf("run() code = %d, want 130 after graceful drain", code)
	}
	records := readMainJSONL(t, paths.output)
	if len(records) != 1 || records[0]["id"] != "dataset:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("output records = %#v, want drained success", records)
	}
}

func TestRunCancelsInFlightRequestAfterShutdownTimeout(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		close(started)
		<-release
	}))
	defer server.Close()
	t.Setenv(cliAPIKeyEnv, cliAPIKey)
	paths := writeCLIConfig(t, server.URL, cliAPIKeyEnv)
	setCLIShutdownTimeout(t, paths.config, "40ms")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() {
		done <- run(ctx, []string{"-config", paths.config}, &bytes.Buffer{}, &bytes.Buffer{})
	}()

	<-started
	startedAt := time.Now()
	cancel()
	select {
	case code := <-done:
		if code != 130 {
			t.Errorf("run() code = %d, want 130 after forced cancellation", code)
		}
	case <-time.After(time.Second):
		close(release)
		t.Fatal("run() did not cancel in-flight request after shutdown timeout")
	}
	if elapsed := time.Since(startedAt); elapsed < 25*time.Millisecond {
		t.Errorf("run() returned after %v, want shutdown timeout before cancellation", elapsed)
	}
	close(release)
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
	writeMainFile(t, paths.input, compactCLIInput("changed synthetic prompt", cliResponse))
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
	if len(records) != 1 || records[0]["id"] != "dataset:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("output records = %#v, want prior terminal record", records)
	}
	assertNoCLIPayload(t, stdout.String(), stderr.String())
}

type cliPaths struct {
	config string
	input  string
	output string
	failed string
	state  string
	schema string
}

func writeCLIConfig(t *testing.T, baseURL, apiKeyEnv string) cliPaths {
	t.Helper()
	return writeCLIConfigWithMode(t, baseURL, apiKeyEnv, "json_schema")
}

func writeCLIConfigWithMode(t *testing.T, baseURL, apiKeyEnv, mode string) cliPaths {
	t.Helper()
	directory := t.TempDir()
	writeMainFile(t, filepath.Join(directory, "input.jsonl"), compactCLIInput(cliPrompt, cliResponse))
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
		state:  filepath.Join(directory, "state.db"),
		schema: filepath.Join(directory, "schema.json"),
	}
}

// compactCLIInput 生成 CLI 集成测试使用的 compact_jsonl 输入行。
func compactCLIInput(prompt, response string) string {
	return fmt.Sprintf(
		`{"id":"dataset:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",`+
			`"source":{"dataset":"dataset","path":"source.json","index":1},`+
			`"messages":[{"role":"user","content":%q},{"role":"assistant","content":%q}],`+
			`"label":{"value":"safe"},"meta":{"sample_id":"sample-1"}}`+"\n",
		prompt,
		response,
	)
}

// differenceCLIInput 生成综合裁决模式使用的差异输入行。
func differenceCLIInput() string {
	return fmt.Sprintf(
		`{"id":"dataset:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",`+
			`"source":{"dataset":"dataset","path":"source.json","index":1},`+
			`"messages":[{"role":"user","content":%q},{"role":"assistant","content":%q}],`+
			`"label":{"value":"unsafe","risk_type":"RT01","risk_level":"low"},`+
			`"annotation":{"method":"auto","is_attack":true,"case_type":"typical",`+
			`"explanation":"模型认为存在明显风险。",`+
			`"extended_info":{"risk_type":"prompt_injection","risk_level":"high"}},`+
			`"original_label":{"label":"unsafe","risk_type":"prompt_injection","risk_level":"low",`+
			`"case_type":"typical","is_attack":true},`+
			`"model_label":{"label":"unsafe","risk_type":"prompt_injection","risk_level":"high",`+
			`"case_type":"typical","is_attack":true}}`+"\n",
		cliPrompt,
		cliResponse,
	)
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
		writeCLICompletion(t, writer, modelOutput)
	}))
}

func writeCLICompletion(t *testing.T, writer http.ResponseWriter, modelOutput string) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(map[string]any{
		"choices": []any{map[string]any{
			"message":       map[string]any{"role": "assistant", "content": modelOutput},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{
			"prompt_tokens":     10,
			"completion_tokens": 5,
			"total_tokens":      15,
		},
	}); err != nil {
		t.Errorf("encode completion: %v", err)
	}
}

func setCLIShutdownTimeout(t *testing.T, configPath, timeout string) {
	t.Helper()
	replaceMainConfig(t, configPath, "shutdown_timeout: 1s", "shutdown_timeout: "+timeout)
}

func replaceMainConfig(t *testing.T, configPath, old, replacement string) {
	t.Helper()
	contents, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile(config) error = %v", err)
	}
	updated := strings.Replace(string(contents), old, replacement, 1)
	if updated == string(contents) {
		t.Fatalf("config does not contain %q", old)
	}
	writeMainFile(t, configPath, updated)
}

func assertNoPersistedTaskItems(t *testing.T, statePath string) {
	t.Helper()
	if _, err := os.Stat(statePath); errors.Is(err, os.ErrNotExist) {
		return
	} else if err != nil {
		t.Fatalf("Stat(state) error = %v", err)
	}
	db, err := sql.Open("sqlite", statePath)
	if err != nil {
		t.Fatalf("sql.Open(state) error = %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close(state) error = %v", err)
		}
	}()
	for _, table := range []string{"tasks", "items"} {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Fatalf("count %s error = %v", table, err)
		}
		if count != 0 {
			t.Errorf("%s count = %d, want 0", table, count)
		}
	}
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
