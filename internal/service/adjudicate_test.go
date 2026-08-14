package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"sendllm/internal/dao"
	"sendllm/internal/dto"
	"sendllm/internal/lib/limiter"
	"sendllm/internal/service"
)

const adjudicateSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["is_attack", "case_type", "explanation"],
  "properties": {
    "is_attack": {"type": "boolean"},
    "case_type": {"type": "string", "enum": ["typical", "borderline", "variant", "hard_negative"]},
    "explanation": {"type": "string", "minLength": 10, "maxLength": 70},
    "extended_info": {
      "type": "object",
      "properties": {
        "risk_type": {"type": "string"},
        "risk_level": {"type": "string", "enum": ["low", "medium", "high"]},
        "attack_scenario": {"type": "string"},
        "other": {"type": "string"}
      },
      "additionalProperties": true
    }
  }
}`

func TestAdjudicateIgnoresExistingOutputAndUsesSQLiteState(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "differences.jsonl")
	outputPath := filepath.Join(directory, "final.jsonl")
	statePath := filepath.Join(directory, "state.db")
	writeAdjudicateFile(t, inputPath, adjudicateDifferenceLine("first"), adjudicateDifferenceLine("second"))
	writeAdjudicateFile(t, outputPath, `{"trace_id":"old-file-row","label":"unsafe"}`+"\n")
	store := openAdjudicateStore(t, statePath)
	seedAdjudicateStore(t, store, dao.Item{
		TaskID:     "adjudicate-task",
		TraceID:    "first",
		InputIndex: 1,
		RawJSON:    []byte(adjudicateDifferenceLine("first")),
		State:      dao.ItemSucceeded,
	})
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	var calledFirst bool
	completer := adjudicateCompleterFunc(func(ctx context.Context, request dto.CompletionRequest) (dto.CompletionResponse, error) {
		if strings.Contains(request.Messages[1].Content, `"id":"first"`) {
			calledFirst = true
		}
		return adjudicateCompletion("test-risk"), nil
	})

	_, err := service.Adjudicate(context.Background(), adjudicateTestConfigWithState(
		t,
		inputPath,
		outputPath,
		statePath,
		completer,
		1,
	))
	if err != nil {
		t.Fatalf("Adjudicate() error = %v", err)
	}
	if calledFirst {
		t.Fatal("Adjudicate called model for already succeeded SQLite row")
	}
	lines := readAdjudicateLines(t, outputPath)
	if len(lines) != 2 {
		t.Fatalf("output lines = %d, want 2", len(lines))
	}
	if !strings.Contains(lines[0], `"trace_id":"first"`) || !strings.Contains(lines[1], `"trace_id":"second"`) {
		t.Fatalf("output lines = %#v, want SQLite export to replace old file contents", lines)
	}
}

func TestAdjudicateReportsSafeLineError(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "differences.jsonl")
	outputPath := filepath.Join(directory, "final.jsonl")
	writeAdjudicateFile(t, inputPath, adjudicateDifferenceLine("first"), adjudicateDifferenceLine("second"))
	completer := adjudicateCompleterFunc(func(ctx context.Context, request dto.CompletionRequest) (dto.CompletionResponse, error) {
		if strings.Contains(request.Messages[1].Content, `"id":"second"`) {
			return dto.CompletionResponse{}, &dto.ProviderError{Kind: dto.ProviderBadRequest, StatusCode: 400}
		}
		return adjudicateCompletion("test-risk"), nil
	})

	_, err := service.Adjudicate(context.Background(), adjudicateTestConfig(t, inputPath, outputPath, completer))
	if err == nil || !strings.Contains(err.Error(), "bad_request") {
		t.Fatalf("Adjudicate() error = %v, want safe category", err)
	}
	if strings.Contains(err.Error(), "synthetic prompt") {
		t.Fatalf("Adjudicate() error leaked payload: %v", err)
	}
}

func TestAdjudicateRecordsRejectedLineAndContinues(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "differences.jsonl")
	outputPath := filepath.Join(directory, "final.jsonl")
	writeAdjudicateFile(
		t,
		inputPath,
		adjudicateDifferenceLine("first"),
		adjudicateDifferenceLine("second"),
		adjudicateDifferenceLine("third"),
	)
	completer := adjudicateCompleterFunc(func(ctx context.Context, request dto.CompletionRequest) (dto.CompletionResponse, error) {
		if strings.Contains(request.Messages[1].Content, `"id":"second"`) {
			return dto.CompletionResponse{}, &dto.ProviderError{Kind: dto.ProviderContentRejected, StatusCode: 400}
		}
		return adjudicateCompletion("test-risk"), nil
	})

	_, err := service.Adjudicate(context.Background(), adjudicateTestConfig(t, inputPath, outputPath, completer))
	if err != nil {
		t.Fatalf("Adjudicate() error = %v", err)
	}
	lines := readAdjudicateLines(t, outputPath)
	if len(lines) != 2 || !strings.Contains(lines[0], `"trace_id":"first"`) ||
		!strings.Contains(lines[1], `"trace_id":"third"`) {
		t.Fatalf("output lines = %#v, want only successful first and third", lines)
	}
	failed := readAdjudicateLines(t, filepath.Join(directory, "final.failed.jsonl"))
	if len(failed) != 1 {
		t.Fatalf("failed lines = %d, want 1", len(failed))
	}
	var failedRecord map[string]any
	if err := json.Unmarshal([]byte(failed[0]), &failedRecord); err != nil {
		t.Fatalf("failed line is not JSON: %v", err)
	}
	if failedRecord["trace_id"] != "second" || failedRecord["error_category"] != "content_rejected" {
		t.Fatalf("failed record = %#v, want safe category for second", failedRecord)
	}
	if strings.Contains(failed[0], "synthetic prompt") {
		t.Fatalf("failed record leaked payload: %s", failed[0])
	}
}

func TestAdjudicateSendsMinimalJudgmentPrompt(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "differences.jsonl")
	outputPath := filepath.Join(directory, "final.jsonl")
	writeAdjudicateFile(t, inputPath, adjudicateDifferenceLine("first"))
	completer := adjudicateCompleterFunc(func(ctx context.Context, request dto.CompletionRequest) (dto.CompletionResponse, error) {
		var prompt map[string]any
		if err := json.Unmarshal([]byte(request.Messages[1].Content), &prompt); err != nil {
			t.Fatalf("adjudicate prompt is not JSON: %v", err)
		}
		for _, key := range []string{"source", "meta", "label"} {
			if _, ok := prompt[key]; ok {
				t.Fatalf("adjudicate prompt contains %q: %#v", key, prompt)
			}
		}
		if _, ok := prompt["messages"]; !ok {
			t.Fatalf("adjudicate prompt = %#v, want messages retained", prompt)
		}
		judgment, ok := prompt["model_judgment"].(map[string]any)
		if !ok {
			t.Fatalf("model_judgment = %#v, want object", prompt["model_judgment"])
		}
		if judgment["explanation"] != "模型判断存在风险。" {
			t.Fatalf("model_judgment = %#v, want explanation", judgment)
		}
		reasons, ok := prompt["reasons"].(map[string]any)
		if !ok {
			t.Fatalf("reasons = %#v, want object", prompt["reasons"])
		}
		if reasons["original"] != "原始标签判断存在风险。" || reasons["model"] != "模型判断存在风险。" {
			t.Fatalf("reasons = %#v, want original and model reasons", reasons)
		}
		for _, key := range []string{"method", "extended_info", "is_attack", "case_type"} {
			if _, ok := judgment[key]; ok {
				t.Fatalf("model_judgment contains %q: %#v", key, judgment)
			}
		}
		return adjudicateCompletion("test-risk"), nil
	})

	if _, err := service.Adjudicate(context.Background(), adjudicateTestConfig(t, inputPath, outputPath, completer)); err != nil {
		t.Fatalf("Adjudicate() error = %v", err)
	}
}

func TestAdjudicateUsesSQLiteStateForResumeAndFailedRetry(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "differences.jsonl")
	outputPath := filepath.Join(directory, "final.jsonl")
	statePath := filepath.Join(directory, "state.db")
	writeAdjudicateFile(t, inputPath, adjudicateDifferenceLine("first"), adjudicateDifferenceLine("second"))
	store := openAdjudicateStore(t, statePath)
	seedAdjudicateStore(t, store,
		dao.Item{
			TaskID:     "adjudicate-task",
			TraceID:    "first",
			InputIndex: 1,
			RawJSON:    []byte(adjudicateDifferenceLine("first")),
			State:      dao.ItemSucceeded,
		},
		dao.Item{
			TaskID:     "adjudicate-task",
			TraceID:    "second",
			InputIndex: 2,
			RawJSON:    []byte(adjudicateDifferenceLine("second")),
			State:      dao.ItemFailed,
		},
	)
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	var calls []string
	completer := adjudicateCompleterFunc(func(ctx context.Context, request dto.CompletionRequest) (dto.CompletionResponse, error) {
		var prompt map[string]json.RawMessage
		if err := json.Unmarshal([]byte(request.Messages[1].Content), &prompt); err != nil {
			t.Fatalf("request prompt JSON error: %v", err)
		}
		var id string
		_ = json.Unmarshal(prompt["id"], &id)
		calls = append(calls, id)
		return adjudicateCompletion("test-risk"), nil
	})

	_, err := service.Adjudicate(context.Background(), adjudicateTestConfigWithState(
		t,
		inputPath,
		outputPath,
		statePath,
		completer,
		1,
	))
	if err != nil {
		t.Fatalf("Adjudicate() error = %v", err)
	}
	if strings.Join(calls, ",") != "second" {
		t.Fatalf("model calls = %#v, want only failed row retried", calls)
	}
	lines := readAdjudicateLines(t, outputPath)
	if len(lines) != 2 || !strings.Contains(lines[0], `"trace_id":"first"`) ||
		!strings.Contains(lines[1], `"trace_id":"second"`) {
		t.Fatalf("output lines = %#v, want succeeded rows exported from SQLite", lines)
	}
}

func TestAdjudicateRunsWithConfiguredConcurrency(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "differences.jsonl")
	outputPath := filepath.Join(directory, "final.jsonl")
	statePath := filepath.Join(directory, "state.db")
	writeAdjudicateFile(
		t,
		inputPath,
		adjudicateDifferenceLine("first"),
		adjudicateDifferenceLine("second"),
		adjudicateDifferenceLine("third"),
	)
	var mu sync.Mutex
	active := 0
	maxActive := 0
	completer := adjudicateCompleterFunc(func(ctx context.Context, request dto.CompletionRequest) (dto.CompletionResponse, error) {
		mu.Lock()
		active++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()
		defer func() {
			mu.Lock()
			active--
			mu.Unlock()
		}()
		select {
		case <-ctx.Done():
			return dto.CompletionResponse{}, ctx.Err()
		case <-time.After(20 * time.Millisecond):
			return adjudicateCompletion("test-risk"), nil
		}
	})

	_, err := service.Adjudicate(context.Background(), adjudicateTestConfigWithState(
		t,
		inputPath,
		outputPath,
		statePath,
		completer,
		2,
	))
	if err != nil {
		t.Fatalf("Adjudicate() error = %v", err)
	}
	if maxActive != 2 {
		t.Fatalf("max active calls = %d, want 2", maxActive)
	}
}

func TestAdjudicateExportsSucceededRowsAfterCancellation(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "differences.jsonl")
	outputPath := filepath.Join(directory, "final.jsonl")
	writeAdjudicateFile(t, inputPath, adjudicateDifferenceLine("first"), adjudicateDifferenceLine("second"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	completer := adjudicateCompleterFunc(func(ctx context.Context, request dto.CompletionRequest) (dto.CompletionResponse, error) {
		if strings.Contains(request.Messages[1].Content, `"id":"first"`) {
			cancel()
			return adjudicateCompletion("test-risk"), nil
		}
		<-ctx.Done()
		return dto.CompletionResponse{}, ctx.Err()
	})

	_, err := service.Adjudicate(ctx, adjudicateTestConfig(t, inputPath, outputPath, completer))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Adjudicate() error = %v, want context canceled", err)
	}
	lines := readAdjudicateLines(t, outputPath)
	if len(lines) != 1 || !strings.Contains(lines[0], `"trace_id":"first"`) {
		t.Fatalf("output lines = %#v, want succeeded row exported after cancellation", lines)
	}
}

type adjudicateCompleterFunc func(context.Context, dto.CompletionRequest) (dto.CompletionResponse, error)

func (f adjudicateCompleterFunc) Complete(
	ctx context.Context,
	request dto.CompletionRequest,
) (dto.CompletionResponse, error) {
	return f(ctx, request)
}

func adjudicateTestConfig(t *testing.T, inputPath, outputPath string, completer service.Completer) service.AdjudicateConfig {
	t.Helper()
	return adjudicateTestConfigWithState(t, inputPath, outputPath, "", completer, 1)
}

func adjudicateTestConfigWithState(
	t *testing.T,
	inputPath string,
	outputPath string,
	statePath string,
	completer service.Completer,
	concurrency int,
) service.AdjudicateConfig {
	t.Helper()
	validator, err := service.NewValidator([]byte(adjudicateSchema), map[string]string{"test-risk": "test"}, 10, 70)
	if err != nil {
		t.Fatalf("NewValidator() error = %v", err)
	}
	requestLimiter, err := limiter.New(limiter.Config{Concurrency: concurrency})
	if err != nil {
		t.Fatalf("limiter.New() error = %v", err)
	}
	if statePath == "" {
		statePath = filepath.Join(t.TempDir(), "state.db")
	}
	return service.AdjudicateConfig{
		TaskID:       "adjudicate-task",
		InputPath:    inputPath,
		OutputPath:   outputPath,
		StatePath:    statePath,
		SemanticHash: "synthetic-adjudicate-hash",
		SystemPrompt: []byte("synthetic adjudicate system prompt"),
		Scene:        "auto",
		Schema:       []byte(adjudicateSchema),
		Mode:         "json_schema",
		Completer:    completer,
		Validator:    validator,
		Limiter:      requestLimiter,
		MaxTokens:    100,
		MaxAttempts:  1,
		Shutdown:     time.Second,
		RetryPolicy: service.RetryPolicy{
			MaxAttempts:    1,
			InitialBackoff: time.Millisecond,
			MaxBackoff:     time.Millisecond,
		},
	}
}

func openAdjudicateStore(t *testing.T, path string) *dao.Store {
	t.Helper()
	store, err := dao.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("dao.Open() error = %v", err)
	}
	if err := store.EnsureTask(context.Background(), dao.Task{ID: "adjudicate-task", SemanticHash: "synthetic-adjudicate-hash"}); err != nil {
		t.Fatalf("EnsureTask() error = %v", err)
	}
	return store
}

func seedAdjudicateStore(t *testing.T, store *dao.Store, items ...dao.Item) {
	t.Helper()
	imp, err := store.BeginImport(context.Background(), "adjudicate-task")
	if err != nil {
		t.Fatalf("BeginImport() error = %v", err)
	}
	defer func() { _ = imp.Rollback() }()
	targetStates := make(map[string]dao.ItemState, len(items))
	for _, item := range items {
		targetStates[item.TraceID] = item.State
		item.TaskID = "adjudicate-task"
		if item.State == dao.ItemSucceeded || item.State == dao.ItemFailed {
			item.State = dao.ItemProcessing
		}
		if _, err := imp.Add(context.Background(), item); err != nil {
			t.Fatalf("Import.Add(%q) error = %v", item.TraceID, err)
		}
	}
	if err := imp.Commit(); err != nil {
		t.Fatalf("Import.Commit() error = %v", err)
	}
	for traceID, state := range targetStates {
		attempt := dao.Attempt{
			Phase:         "classification",
			RequestNumber: 1,
			StartedAt:     time.Now(),
			FinishedAt:    time.Now(),
		}
		switch state {
		case dao.ItemSucceeded:
			annotation, err := json.Marshal(dto.Annotation{
				IsAttack:    true,
				CaseType:    "typical",
				Explanation: "综合判断该样本存在风险。",
				ExtendedInfo: &dto.ExtendedInfo{
					RiskType:  "test-risk",
					RiskLevel: "high",
				},
			})
			if err != nil {
				t.Fatalf("Marshal(annotation) error = %v", err)
			}
			if err := store.MarkSucceeded(context.Background(), "adjudicate-task", traceID, attempt, annotation); err != nil {
				t.Fatalf("MarkSucceeded(%q) error = %v", traceID, err)
			}
		case dao.ItemFailed:
			if err := store.MarkFailed(
				context.Background(),
				"adjudicate-task",
				traceID,
				attempt,
				"content_rejected",
				"provider request failed",
			); err != nil {
				t.Fatalf("MarkFailed(%q) error = %v", traceID, err)
			}
		}
	}
}

func adjudicateDifferenceLine(id string) string {
	encodedID, _ := json.Marshal(id)
	return `{"id":` + string(encodedID) + `,"source":{"dataset":"test","path":"source.json","index":1},` +
		`"messages":[{"role":"user","content":"synthetic prompt"}],` +
		`"meta":{"source_fields":{"reason":"原始标签判断存在风险。"}},` +
		`"annotation":{"method":"auto","is_attack":true,"case_type":"typical",` +
		`"explanation":"模型判断存在风险。","extended_info":{"risk_type":"test-risk","risk_level":"high"}},` +
		`"original_label":{"label":"unsafe","risk_type":"test-risk","risk_level":"low",` +
		`"case_type":"typical","is_attack":true},` +
		`"model_label":{"label":"unsafe","risk_type":"test-risk","risk_level":"high",` +
		`"case_type":"typical","is_attack":true}}` + "\n"
}

func adjudicateCompletion(riskType string) dto.CompletionResponse {
	content := `{"is_attack":true,"case_type":"typical","explanation":"综合判断该样本存在风险。",` +
		`"extended_info":{"risk_type":` + strconvQuote(riskType) + `,"risk_level":"high"}}`
	return dto.CompletionResponse{Content: []byte(content)}
}

func strconvQuote(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func writeAdjudicateFile(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(lines, "")), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func readAdjudicateLines(t *testing.T, path string) []string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	trimmed := strings.TrimSpace(string(contents))
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}
