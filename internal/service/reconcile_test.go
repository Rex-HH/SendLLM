package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sendllm/internal/dto"
	"sendllm/internal/lib/limiter"
	"sendllm/internal/service"
)

const reconcileSchema = `{
  "type":"object",
  "additionalProperties":false,
  "required":["is_attack","case_type","explanation","quality_score","extended_info"],
  "properties":{
    "is_attack":{"type":"boolean"},
    "case_type":{"type":"string","enum":["typical","borderline","variant","hard_negative"]},
    "explanation":{"type":"string","minLength":10,"maxLength":70},
    "quality_score":{"type":"number","minimum":0,"maximum":1},
    "extended_info":{
      "type":"object",
      "required":["attack_method","attack_domain"],
      "properties":{
        "attack_method":{"type":"string"},
        "attack_domain":{"type":"string"},
        "risk_level":{"type":"string","enum":["low","medium","high"]},
        "attack_scenario":{"type":"string"},
        "other":{"type":"string"}
      },
      "additionalProperties":true
    }
  }
}`

func TestReconcileImportsRunsAndExports(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.jsonl")
	outputPath := filepath.Join(directory, "final.jsonl")
	statePath := filepath.Join(directory, "state.db")
	writeAdjudicateFile(t, inputPath,
		reconcileInputLine("method-row", "in-1-prompt-injection", "unsafe", "medium", "原始标签理由。"),
		reconcileInputLine("domain-row", "out-A.1-g-false_harmful_information", "unsafe", "medium", "原始领域理由。"),
	)

	completer := reconcileCompleterFunc(func(ctx context.Context, request dto.CompletionRequest) (dto.CompletionResponse, error) {
		if strings.Contains(request.Messages[1].Content, `"trace_id":"method-row"`) {
			return reconcileCompletion(`{
				"is_attack":true,
				"case_type":"typical",
				"explanation":"模型判断为提示词注入。",
				"quality_score":0.9,
				"extended_info":{"attack_method":"prompt_injection","attack_domain":"","risk_level":"medium"}
			}`), nil
		}
		return reconcileCompletion(`{
			"is_attack":true,
			"case_type":"typical",
			"explanation":"模型判断为虚假有害信息。",
			"quality_score":0.9,
			"extended_info":{"attack_method":"","attack_domain":"harmful_misinformation","risk_level":"medium"}
		}`), nil
	})

	stats, err := service.Reconcile(context.Background(), reconcileTestConfig(
		t,
		inputPath,
		outputPath,
		statePath,
		completer,
	))
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if stats.Succeeded != 2 || stats.Failed != 0 {
		t.Fatalf("stats = %#v, want 2 succeeded and 0 failed", stats)
	}

	lines := readAdjudicateLines(t, outputPath)
	if len(lines) != 2 {
		t.Fatalf("output lines = %d, want 2", len(lines))
	}
	if !strings.Contains(lines[0], `"explanation":"原始标签理由。"`) {
		t.Fatalf("method row explanation = %s, want original reason reused", lines[0])
	}
	if !strings.Contains(lines[1], `"explanation":"原始领域理由。"`) {
		t.Fatalf("domain row explanation = %s, want original reason reused", lines[1])
	}

	var first map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("first line is not JSON: %v", err)
	}
	extended := first["extended_info"].(map[string]any)
	if extended["attack_method"] != "prompt_injection" {
		t.Fatalf("first attack_method = %#v, want prompt_injection", extended["attack_method"])
	}
	if _, exists := extended["risk_type"]; exists {
		t.Fatalf("first extended_info contains legacy risk_type: %#v", extended)
	}
}

func TestReconcileExportsSucceededRowsAfterCancellation(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.jsonl")
	outputPath := filepath.Join(directory, "final.jsonl")
	writeAdjudicateFile(t, inputPath,
		reconcileInputLine("first", "in-1-prompt-injection", "unsafe", "medium", "原始理由。"),
		reconcileInputLine("second", "in-1-prompt-injection", "unsafe", "medium", "原始理由。"),
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	completer := reconcileCompleterFunc(func(ctx context.Context, request dto.CompletionRequest) (dto.CompletionResponse, error) {
		if strings.Contains(request.Messages[1].Content, `"trace_id":"first"`) {
			cancel()
			return reconcileCompletion(`{
				"is_attack":true,
				"case_type":"typical",
				"explanation":"模型判断为提示词注入。",
				"quality_score":0.9,
				"extended_info":{"attack_method":"prompt_injection","attack_domain":"","risk_level":"medium"}
			}`), nil
		}
		<-ctx.Done()
		return dto.CompletionResponse{}, ctx.Err()
	})

	_, err := service.Reconcile(ctx, reconcileTestConfig(
		t,
		inputPath,
		outputPath,
		filepath.Join(directory, "state.db"),
		completer,
	))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Reconcile() error = %v, want context canceled", err)
	}
	lines := readAdjudicateLines(t, outputPath)
	if len(lines) != 1 || !strings.Contains(lines[0], `"trace_id":"first"`) {
		t.Fatalf("output lines = %#v, want only first succeeded row after cancellation", lines)
	}
}

func TestReconcileWritesFreshRunLog(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.jsonl")
	outputPath := filepath.Join(directory, "final.jsonl")
	logPath := filepath.Join(directory, "final.reconcile-log.jsonl")
	if err := os.WriteFile(logPath, []byte("old-log"), 0o600); err != nil {
		t.Fatalf("WriteFile(logPath) error = %v", err)
	}
	writeAdjudicateFile(t, inputPath,
		reconcileInputLine("same-row", "in-1-prompt-injection", "unsafe", "medium", "原始理由。"),
		reconcileInputLine("diff-row", "in-1-prompt-injection", "unsafe", "medium", "原始理由。"),
	)

	completer := reconcileCompleterFunc(func(ctx context.Context, request dto.CompletionRequest) (dto.CompletionResponse, error) {
		if strings.Contains(request.Messages[1].Content, `"trace_id":"diff-row"`) {
			return reconcileCompletion(`{
				"is_attack":true,
				"case_type":"typical",
				"explanation":"模型认为该样本风险等级更高。",
				"quality_score":0.8,
				"extended_info":{"attack_method":"prompt_injection","attack_domain":"","risk_level":"high"}
			}`), nil
		}
		return reconcileCompletion(`{
			"is_attack":true,
			"case_type":"typical",
			"explanation":"模型判断为提示词注入。",
			"quality_score":0.9,
			"extended_info":{"attack_method":"prompt_injection","attack_domain":"","risk_level":"medium"}
		}`), nil
	})

	if _, err := service.Reconcile(context.Background(), reconcileTestConfig(
		t,
		inputPath,
		outputPath,
		filepath.Join(directory, "state.db"),
		completer,
	)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	contents, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(logPath) error = %v", err)
	}
	if strings.Contains(string(contents), "old-log") {
		t.Fatal("reconcile log was appended to old content instead of being replaced")
	}

	type logLine struct {
		RunStatus string `json:"run_status"`
		TraceID   string `json:"trace_id"`
		Status    string `json:"status"`
		APIKeyEnv string `json:"api_key_env"`
		Changes   []struct {
			Field    string `json:"field"`
			Original string `json:"original"`
			Final    string `json:"final"`
		} `json:"changes"`
	}
	lines := strings.Split(strings.TrimSpace(string(contents)), "\n")
	if len(lines) != 2 {
		t.Fatalf("log lines = %d, want 2", len(lines))
	}
	var sameRow, diffRow logLine
	for _, line := range lines {
		var decoded logLine
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			t.Fatalf("log line is not JSON: %v", err)
		}
		if decoded.RunStatus != "completed" {
			t.Fatalf("run_status = %q, want completed", decoded.RunStatus)
		}
		if decoded.APIKeyEnv == "" {
			t.Fatalf("api_key_env is empty in log line %s", line)
		}
		switch decoded.TraceID {
		case "same-row":
			sameRow = decoded
		case "diff-row":
			diffRow = decoded
		default:
			t.Fatalf("unexpected log trace_id %q", decoded.TraceID)
		}
	}
	if sameRow.Status != "succeeded_consistent" {
		t.Fatalf("same-row status = %q, want succeeded_consistent", sameRow.Status)
	}
	if diffRow.Status != "succeeded_changed" {
		t.Fatalf("diff-row status = %q, want succeeded_changed", diffRow.Status)
	}
	if !hasFieldChange(diffRow.Changes, "risk_level") || !hasFieldChange(diffRow.Changes, "explanation") {
		t.Fatalf("diff-row changes = %#v, want risk_level and explanation", diffRow.Changes)
	}
}

func TestReconcileBatchSendsMultipleRowsInOneRequest(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.jsonl")
	outputPath := filepath.Join(directory, "final.jsonl")
	statePath := filepath.Join(directory, "state.db")
	writeAdjudicateFile(t, inputPath,
		reconcileInputLine("first", "in-1-prompt-injection", "unsafe", "medium", "原始一。"),
		reconcileInputLine("second", "out-A.1-g-false_harmful_information", "unsafe", "medium", "原始二。"),
	)

	calls := 0
	completer := reconcileCompleterFunc(func(ctx context.Context, request dto.CompletionRequest) (dto.CompletionResponse, error) {
		calls++
		if len(request.Messages) != 2 {
			t.Fatalf("messages = %d, want system and one batch user message", len(request.Messages))
		}
		var payload struct {
			Items []struct {
				TraceID string `json:"trace_id"`
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(request.Messages[1].Content), &payload); err != nil {
			t.Fatalf("batch user message is not JSON: %v", err)
		}
		if len(payload.Items) != 2 || payload.Items[0].TraceID != "first" || payload.Items[1].TraceID != "second" {
			t.Fatalf("batch items = %#v, want first and second in one request", payload.Items)
		}
		return reconcileCompletion(`{"results":[
			{
				"trace_id":"first",
				"is_attack":true,
				"case_type":"typical",
				"explanation":"模型判断为提示词注入。",
				"quality_score":0.9,
				"extended_info":{"attack_method":"prompt_injection","attack_domain":"","risk_level":"medium"}
			},
			{
				"trace_id":"second",
				"is_attack":true,
				"case_type":"typical",
				"explanation":"模型判断为虚假信息。",
				"quality_score":0.9,
				"extended_info":{"attack_method":"","attack_domain":"harmful_misinformation","risk_level":"medium"}
			}
		]}`), nil
	})

	cfg := reconcileTestConfig(t, inputPath, outputPath, statePath, completer)
	cfg.BatchSize = 2
	stats, err := service.ReconcileBatch(context.Background(), cfg)
	if err != nil {
		t.Fatalf("ReconcileBatch() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("model calls = %d, want 1", calls)
	}
	if stats.Succeeded != 2 || stats.Failed != 0 {
		t.Fatalf("stats = %#v, want 2 succeeded and 0 failed", stats)
	}
}

func TestReconcileBatchRetriesOnlyMissingRowsFromBatchResponse(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.jsonl")
	outputPath := filepath.Join(directory, "final.jsonl")
	statePath := filepath.Join(directory, "state.db")
	writeAdjudicateFile(t, inputPath,
		reconcileInputLine("first", "in-1-prompt-injection", "unsafe", "medium", "原始一。"),
		reconcileInputLine("second", "out-A.1-g-false_harmful_information", "unsafe", "medium", "原始二。"),
	)

	calls := 0
	completer := reconcileCompleterFunc(func(ctx context.Context, request dto.CompletionRequest) (dto.CompletionResponse, error) {
		calls++
		if calls == 1 {
			return reconcileCompletion(`{"results":[
				{
					"trace_id":"first",
					"is_attack":true,
					"case_type":"typical",
					"explanation":"模型判断为提示词注入。",
					"quality_score":0.9,
					"extended_info":{"attack_method":"prompt_injection","attack_domain":"","risk_level":"medium"}
				}
			]}`), nil
		}
		return reconcileCompletion(`{"results":[
			{
				"trace_id":"second",
				"is_attack":true,
				"case_type":"typical",
				"explanation":"模型判断为虚假信息。",
				"quality_score":0.9,
				"extended_info":{"attack_method":"","attack_domain":"harmful_misinformation","risk_level":"medium"}
			}
		]}`), nil
	})

	cfg := reconcileTestConfig(t, inputPath, outputPath, statePath, completer)
	cfg.BatchSize = 2
	cfg.MaxAttempts = 2
	cfg.RetryPolicy.MaxAttempts = 2
	stats, err := service.ReconcileBatch(context.Background(), cfg)
	if err != nil {
		t.Fatalf("ReconcileBatch() error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("model calls = %d, want missing row retried once", calls)
	}
	if stats.Succeeded != 2 || stats.Failed != 0 {
		t.Fatalf("stats = %#v, want 2 succeeded and 0 failed", stats)
	}
}

func hasFieldChange(changes []struct {
	Field    string `json:"field"`
	Original string `json:"original"`
	Final    string `json:"final"`
}, target string) bool {
	for _, change := range changes {
		if change.Field == target {
			return true
		}
	}
	return false
}

type reconcileCompleterFunc func(context.Context, dto.CompletionRequest) (dto.CompletionResponse, error)

func (f reconcileCompleterFunc) Complete(
	ctx context.Context,
	request dto.CompletionRequest,
) (dto.CompletionResponse, error) {
	return f(ctx, request)
}

func reconcileCompletion(content string) dto.CompletionResponse {
	raw := `{
		"id":"chatcmpl-test",
		"object":"chat.completion",
		"created":0,
		"model":"test-model",
		"choices":[{
			"index":0,
			"message":{"role":"assistant","content":` + jsonString(content) + `},
			"finish_reason":"stop"
		}],
		"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
	}`
	return dto.CompletionResponse{
		Content:      []byte(content),
		RawResponse:  []byte(raw),
		FinishReason: "stop",
		APIKeyEnv:    "KEY_A",
	}
}

func jsonString(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func reconcileInputLine(id, pathFragment, value, riskLevel, reason string) string {
	record := map[string]any{
		"id": id,
		"source": map[string]any{
			"dataset": "v2_authoritative",
			"path":    "datasets/benchmarkV2.0/38-Categories/" + pathFragment + "/samples.jsonl",
			"index":   0,
		},
		"messages": []map[string]string{{"role": "user", "content": "待审查提示词"}},
		"label": map[string]string{
			"value":      value,
			"risk_type":  "RT01",
			"risk_level": riskLevel,
		},
		"meta": map[string]any{
			"sample_type": "明确正例",
			"split":       "train",
			"source_fields": map[string]string{
				"reason": reason,
			},
		},
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		panic(err)
	}
	return string(encoded) + "\n"
}

func reconcileTestConfig(
	t *testing.T,
	inputPath string,
	outputPath string,
	statePath string,
	completer service.Completer,
) service.ReconcileConfig {
	t.Helper()
	validator, err := service.NewValidator(
		[]byte(reconcileSchema),
		map[string]string{
			"prompt_injection":       "提示词注入攻击",
			"harmful_misinformation": "传播虚假有害信息",
		},
		10,
		70,
	)
	if err != nil {
		t.Fatalf("NewValidator() error = %v", err)
	}
	requestLimiter, err := limiter.New(limiter.Config{Concurrency: 1})
	if err != nil {
		t.Fatalf("limiter.New() error = %v", err)
	}
	return service.ReconcileConfig{
		TaskID:               "reconcile-task",
		InputPath:            inputPath,
		OutputPath:           outputPath,
		StatePath:            statePath,
		SemanticHash:         "synthetic-reconcile-hash",
		SystemPrompt:         []byte("synthetic reconcile system prompt"),
		Scene:                "auto",
		Schema:               []byte(reconcileSchema),
		Mode:                 "json_object",
		Completer:            completer,
		Validator:            validator,
		Limiter:              requestLimiter,
		MaxTokens:            100,
		MaxAttempts:          1,
		FormatRepairAttempts: 0,
		Shutdown:             time.Second,
		RetryPolicy: service.RetryPolicy{
			MaxAttempts:    1,
			InitialBackoff: time.Millisecond,
			MaxBackoff:     time.Millisecond,
		},
	}
}
