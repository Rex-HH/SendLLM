package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunCalibrationCLIReportsCountsOnly(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.json")
	worksheetPath := filepath.Join(directory, "worksheet.csv")
	writeCalibrationJSONArray(t, sourcePath, []map[string]any{
		calibrationRow("cli-1", "unsafe", "不得打印"),
		calibrationRow("cli-2", "safe", "不得打印"),
	})
	writeCalibrationWorksheet(t, worksheetPath, []map[string]string{
		{"trace_id": "cli-1", "human_label": "unsafe", "human_legacy_overlap": "no"},
		{"trace_id": "cli-2", "human_label": "safe", "human_legacy_overlap": "no"},
	})
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{
		"calibration",
		"-source", sourcePath,
		"-worksheet", worksheetPath,
		"-output-dir", filepath.Join(directory, "output"),
	}, &stdout, &stderr); err != nil {
		t.Fatalf("run(calibration) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "total=2") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if strings.Contains(stdout.String(), "不得打印") || strings.Contains(stderr.String(), "不得打印") {
		t.Fatal("calibration CLI printed prompt")
	}
}

func TestRunCLIRejectsProtectedAndMissingPaths(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.json")
	approvedPath := filepath.Join(directory, "approved.jsonl")
	writeCalibrationJSONArray(t, sourcePath, []map[string]any{
		applyRow("cli-protected", "unsafe", "other_spam", true),
	})
	writeCalibrationFile(t, approvedPath, "")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{
		"apply",
		"-source", sourcePath,
		"-approved", approvedPath,
		"-output", sourcePath,
	}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run(apply) overwrote protected source")
	}
	if err := run([]string{"run"}, &stdout, &stderr); err == nil {
		t.Fatal("run(run) accepted missing config and report")
	}
	if err := run([]string{"unknown"}, &stdout, &stderr); err == nil {
		t.Fatal("run(unknown) accepted unknown subcommand")
	}
}

func TestRunLayerCLIFakeProvider(t *testing.T) {
	t.Setenv("SENDLLM_FULL_CLEAN_TEST_KEY", "test-key")
	directory := t.TempDir()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve root: %v", err)
	}
	inputPath := filepath.Join(directory, "input.jsonl")
	outputPath := filepath.Join(directory, "decisions.jsonl")
	statePath := filepath.Join(directory, "state.db")
	reportPath := filepath.Join(directory, "report.json")
	if err := os.WriteFile(inputPath, []byte(
		`{"trace_id":"fake-1","source":"secret","label":"unsafe","prompt":"不得打印的合成文本"}`+"\n",
	), 0o600); err != nil {
		t.Fatalf("write input: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode fake request: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		var payload struct {
			Items []struct {
				Index int `json:"i"`
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(body.Messages[len(body.Messages)-1].Content), &payload); err != nil {
			t.Errorf("decode compact payload: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		results := make([]map[string]any, 0, len(payload.Items))
		for _, item := range payload.Items {
			results = append(results, map[string]any{"i": item.Index, "l": 2, "x": ""})
		}
		content, err := json.Marshal(map[string]any{"r": results})
		if err != nil {
			t.Errorf("marshal fake result: %v", err)
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		response := map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]any{"role": "assistant", "content": string(content)},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 2},
		}
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(response); err != nil {
			t.Errorf("encode fake response: %v", err)
		}
	}))
	defer server.Close()
	configPath := filepath.Join(directory, "task.yaml")
	config := fmt.Sprintf(`task:
  id: full-clean-cli-fake
  input: %s
  output: %s
  state: %s
model:
  base_url: %s
  api_key_env: SENDLLM_FULL_CLEAN_TEST_KEY
  name: fake-model
  structured_output: json_object
  temperature: 0
  top_p: 1
  max_tokens: 100
  timeout: 5s
prompt:
  system_file: %s
  scene: prompt
  risk_types_file: %s
runtime:
  concurrency: 1
  requests_per_minute: 0
  tokens_per_minute: 0
  shutdown_timeout: 1s
  batch_size: 2
  batch_max_input_tokens: 10000
retry:
  request_max_attempts: 1
  format_repair_attempts: 0
  initial_backoff: 1ms
  max_backoff: 1ms
output:
  schema_file: %s
  explanation_min_length: 1
  explanation_max_length: 1
`,
		inputPath,
		outputPath,
		statePath,
		server.URL,
		filepath.Join(root, "prompts", "advertisement-full-review-system.txt"),
		filepath.Join(root, "config", "risk-types.yaml"),
		filepath.Join(root, "config", "advertisement-full-review-result-schema.json"),
	)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{
		"run",
		"-config", configPath,
		"-report", reportPath,
	}, &stdout, &stderr); err != nil {
		t.Fatalf("run(run) error = %v; stderr = %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "succeeded=1") || !strings.Contains(stdout.String(), "failed=0") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if strings.Contains(stdout.String(), "不得打印的合成文本") ||
		strings.Contains(stderr.String(), "不得打印的合成文本") {
		t.Fatal("run CLI printed prompt")
	}
	decisions, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read decisions: %v", err)
	}
	if !strings.Contains(string(decisions), "fake-1") ||
		strings.Contains(string(decisions), "不得打印的合成文本") {
		t.Fatalf("decisions output = %s", decisions)
	}
	report, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if strings.Contains(string(report), "不得打印的合成文本") {
		t.Fatal("report leaked prompt")
	}
}
