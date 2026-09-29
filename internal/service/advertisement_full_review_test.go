package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"sendllm/internal/dao"
	"sendllm/internal/dto"
	"sendllm/internal/lib/limiter"
)

type fullReviewFakeCompleter struct {
	calls   atomic.Int32
	respond func(context.Context, dto.CompletionRequest) (dto.CompletionResponse, error)
}

// Complete 执行合成供应商响应。
func (f *fullReviewFakeCompleter) Complete(
	ctx context.Context,
	req dto.CompletionRequest,
) (dto.CompletionResponse, error) {
	f.calls.Add(1)
	return f.respond(ctx, req)
}

func TestAdvertisementFullReviewRequestIsBlind(t *testing.T) {
	items := []dao.Item{{
		TraceID: "trace-blind",
		RawJSON: []byte(`{"trace_id":"trace-blind","source":"secret-source","label":"unsafe",` +
			`"prompt":"待判断文本","response":"secret-response","explanation":"secret-explanation",` +
			`"annotation":{"quality_score":0.9},"extended_info":{"risk_type":"jailbreak",` +
			`"attack_scenario":"wechat_contact"}}`),
	}}
	request, err := advertisementFullReviewRequest(
		items,
		[]byte("system"),
		json.RawMessage(`{"type":"object"}`),
		"json_object",
	)
	if err != nil {
		t.Fatalf("advertisementFullReviewRequest() error = %v", err)
	}
	if len(request.Messages) != 2 || request.Messages[0].Role != "system" || request.Messages[1].Role != "user" {
		t.Fatalf("request messages = %+v", request.Messages)
	}
	if request.Messages[1].Content != `{"items":[{"i":0,"p":"待判断文本"}]}` {
		t.Fatalf("blind payload = %s", request.Messages[1].Content)
	}
	for _, forbidden := range []string{
		"trace-blind", "secret-source", "unsafe", "secret-response", "secret-explanation",
		"quality_score", "jailbreak", "wechat_contact",
	} {
		if strings.Contains(request.Messages[1].Content, forbidden) {
			t.Fatalf("blind payload contains %q", forbidden)
		}
	}
}

func TestAdvertisementFullReviewResultsStrict(t *testing.T) {
	riskTypes := map[string]struct{}{"jailbreak": {}}
	tests := []struct {
		name    string
		raw     string
		count   int
		wantErr bool
	}{
		{name: "valid", raw: `{"r":[{"i":0,"l":2,"x":"jailbreak"},{"i":1,"l":1,"x":""}]}`, count: 2},
		{name: "safe risk", raw: `{"r":[{"i":0,"l":1,"x":"jailbreak"}]}`, count: 1, wantErr: true},
		{name: "unknown risk", raw: `{"r":[{"i":0,"l":2,"x":"unknown"}]}`, count: 1, wantErr: true},
		{name: "missing index", raw: `{"r":[{"i":0,"l":1,"x":""}]}`, count: 2, wantErr: true},
		{name: "duplicate index", raw: `{"r":[{"i":0,"l":1,"x":""},{"i":0,"l":2,"x":""}]}`, count: 2, wantErr: true},
		{name: "out of order", raw: `{"r":[{"i":1,"l":2,"x":""},{"i":0,"l":1,"x":""}]}`, count: 2, wantErr: true},
		{name: "invalid label", raw: `{"r":[{"i":0,"l":3,"x":""}]}`, count: 1, wantErr: true},
		{name: "extra field", raw: `{"r":[{"i":0,"l":1,"x":"","s":0}]}`, count: 1, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := advertisementFullReviewResults([]byte(tt.raw), tt.count, riskTypes)
			if (err != nil) != tt.wantErr {
				t.Fatalf("advertisementFullReviewResults() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAdvertisementFullReviewPackingIsGreedy(t *testing.T) {
	items := []dao.Item{
		{TraceID: "one", RawJSON: []byte(`{"trace_id":"one","prompt":"one"}`)},
		{TraceID: "two", RawJSON: []byte(`{"trace_id":"two","prompt":"two"}`)},
		{TraceID: "three", RawJSON: []byte(`{"trace_id":"three","prompt":"three"}`)},
	}
	cfg := advertisementFullReviewPackingConfig{
		SystemPrompt:        []byte("system"),
		Schema:              json.RawMessage(`{"type":"object"}`),
		Mode:                "json_object",
		BatchSize:           3,
		BatchMaxInputTokens: 2,
		MaxTokens:           10,
		Estimate: func(messages []dto.Message, _ int) int {
			var payload struct {
				Items []struct{} `json:"items"`
			}
			if err := json.Unmarshal([]byte(messages[1].Content), &payload); err != nil {
				t.Errorf("decode candidate payload: %v", err)
				return 1 << 30
			}
			return len(payload.Items)
		},
	}
	batches, err := packAdvertisementFullReviewBatches(items, cfg)
	if err != nil {
		t.Fatalf("packAdvertisementFullReviewBatches() error = %v", err)
	}
	if len(batches) != 2 || len(batches[0]) != 2 || len(batches[1]) != 1 {
		t.Fatalf("batch sizes = %v", fullReviewBatchSizes(batches))
	}
	for index, batch := range batches {
		if index == len(batches)-1 {
			break
		}
		candidate := append([]dao.Item(nil), batch...)
		candidate = append(candidate, batches[index+1][0])
		request, err := advertisementFullReviewRequest(candidate, cfg.SystemPrompt, cfg.Schema, cfg.Mode)
		if err != nil {
			t.Fatalf("build candidate request: %v", err)
		}
		if cfg.Estimate(request.Messages, cfg.MaxTokens) <= cfg.BatchMaxInputTokens {
			t.Fatalf("batch was not greedy: %v", fullReviewBatchSizes(batches))
		}
	}
}

func TestAdvertisementFullReviewFakeProviderAndResume(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.jsonl")
	input := `{"trace_id":"full-1","source":"secret","label":"unsafe","prompt":"文本一",` +
		`"response":"secret","explanation":"secret","extended_info":{"risk_type":"jailbreak"}}` + "\n" +
		`{"trace_id":"full-2","source":"secret","label":"unsafe","prompt":"文本二"}` + "\n"
	if err := os.WriteFile(inputPath, []byte(input), 0o600); err != nil {
		t.Fatalf("write input: %v", err)
	}
	outputPath := filepath.Join(directory, "decisions.jsonl")
	statePath := filepath.Join(directory, "state.db")
	completer := &fullReviewFakeCompleter{}
	completer.respond = func(_ context.Context, request dto.CompletionRequest) (dto.CompletionResponse, error) {
		var payload struct {
			Items []struct {
				Index int `json:"i"`
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(request.Messages[1].Content), &payload); err != nil {
			return dto.CompletionResponse{}, err
		}
		results := make([]map[string]any, 0, len(payload.Items))
		for _, item := range payload.Items {
			results = append(results, map[string]any{"i": item.Index, "l": 2, "x": ""})
		}
		encoded, err := json.Marshal(map[string]any{"r": results})
		if err != nil {
			return dto.CompletionResponse{}, err
		}
		return dto.CompletionResponse{Content: encoded, RawResponse: encoded, Usage: dto.Usage{
			PromptTokens: 10, CompletionTokens: 2,
		}}, nil
	}
	createLimiter := func() *limiter.Limiter {
		created, err := limiter.New(limiter.Config{Concurrency: 1})
		if err != nil {
			t.Fatalf("limiter.New() error = %v", err)
		}
		return created
	}
	cfg := AdvertisementFullReviewConfig{
		TaskID:              "full-review-task",
		InputPath:           inputPath,
		OutputPath:          outputPath,
		StatePath:           statePath,
		SemanticHash:        "semantic",
		SystemPrompt:        []byte("system"),
		Schema:              json.RawMessage(`{"type":"object"}`),
		Mode:                "json_object",
		Completer:           completer,
		Limiter:             createLimiter(),
		RiskTypes:           map[string]struct{}{"jailbreak": {}},
		MaxTokens:           100,
		MaxAttempts:         1,
		BatchSize:           2,
		BatchMaxInputTokens: 1000,
		RetryPolicy:         RetryPolicy{MaxAttempts: 1},
		Shutdown:            time.Second,
	}
	stats, err := AdvertisementFullReview(context.Background(), cfg)
	if err != nil {
		t.Fatalf("AdvertisementFullReview() error = %v", err)
	}
	if stats.Succeeded != 2 || stats.Failed != 0 || stats.Requests != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	outputContents := mustReadFile(t, outputPath)
	if strings.Contains(string(outputContents), "文本一") {
		t.Fatal("decision output leaked prompt")
	}
	var outputRows []struct {
		TraceID string `json:"trace_id"`
		State   string `json:"state"`
		Label   int    `json:"l"`
	}
	for _, line := range strings.Split(strings.TrimSpace(string(outputContents)), "\n") {
		if line == "" {
			continue
		}
		var row struct {
			TraceID string `json:"trace_id"`
			State   string `json:"state"`
			Label   int    `json:"l"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("decode decision output: %v", err)
		}
		outputRows = append(outputRows, row)
	}
	if len(outputRows) != 2 || outputRows[0].TraceID != "full-1" || outputRows[1].TraceID != "full-2" {
		t.Fatalf("decision output IDs = %+v", outputRows)
	}
	second := &fullReviewFakeCompleter{}
	second.respond = func(context.Context, dto.CompletionRequest) (dto.CompletionResponse, error) {
		return dto.CompletionResponse{}, errors.New("succeeded row called again")
	}
	cfg.Completer = second
	cfg.Limiter = createLimiter()
	resumed, err := AdvertisementFullReview(context.Background(), cfg)
	if err != nil {
		t.Fatalf("resume AdvertisementFullReview() error = %v", err)
	}
	if second.calls.Load() != 0 || resumed.Succeeded != 2 {
		t.Fatalf("resume calls = %d stats = %+v", second.calls.Load(), resumed)
	}
}

func TestAdvertisementFullReviewSplitsProviderContextRejection(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.jsonl")
	input := `{"trace_id":"context-1","prompt":"一"}` + "\n" +
		`{"trace_id":"context-2","prompt":"二"}` + "\n"
	if err := os.WriteFile(inputPath, []byte(input), 0o600); err != nil {
		t.Fatalf("write input: %v", err)
	}
	completer := &fullReviewFakeCompleter{}
	completer.respond = func(_ context.Context, request dto.CompletionRequest) (dto.CompletionResponse, error) {
		var payload struct {
			Items []struct {
				Index int `json:"i"`
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(request.Messages[1].Content), &payload); err != nil {
			return dto.CompletionResponse{}, err
		}
		if len(payload.Items) > 1 {
			return dto.CompletionResponse{}, &dto.ProviderError{
				Kind: dto.ProviderBadRequest,
				Err:  errors.New("maximum context length exceeded"),
			}
		}
		encoded, err := json.Marshal(map[string]any{
			"r": []map[string]any{{"i": 0, "l": 2, "x": ""}},
		})
		if err != nil {
			return dto.CompletionResponse{}, err
		}
		return dto.CompletionResponse{Content: encoded, RawResponse: encoded}, nil
	}
	createdLimiter, err := limiter.New(limiter.Config{Concurrency: 1})
	if err != nil {
		t.Fatalf("limiter.New() error = %v", err)
	}
	stats, err := AdvertisementFullReview(context.Background(), AdvertisementFullReviewConfig{
		TaskID:              "context-task",
		InputPath:           inputPath,
		OutputPath:          filepath.Join(directory, "decisions.jsonl"),
		StatePath:           filepath.Join(directory, "state.db"),
		SemanticHash:        "semantic",
		SystemPrompt:        []byte("system"),
		Schema:              json.RawMessage(`{"type":"object"}`),
		Mode:                "json_object",
		Completer:           completer,
		Limiter:             createdLimiter,
		RiskTypes:           map[string]struct{}{"jailbreak": {}},
		MaxTokens:           100,
		MaxAttempts:         1,
		BatchSize:           2,
		BatchMaxInputTokens: 1000,
		RetryPolicy:         RetryPolicy{MaxAttempts: 1},
		Shutdown:            time.Second,
	})
	if err != nil {
		t.Fatalf("AdvertisementFullReview() error = %v", err)
	}
	if stats.Succeeded != 2 || stats.ContextOverflows != 1 {
		t.Fatalf("context split stats = %+v", stats)
	}
}

// fullReviewBatchSizes 返回批次大小摘要。
func fullReviewBatchSizes(batches [][]dao.Item) []int {
	sizes := make([]int, len(batches))
	for index, batch := range batches {
		sizes[index] = len(batch)
	}
	return sizes
}

// mustReadFile 读取测试文件并在失败时终止。
func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	return contents
}
