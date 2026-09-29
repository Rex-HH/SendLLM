package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sendllm/internal/dao"
	"sendllm/internal/dto"
	"sendllm/internal/lib/limiter"
)

func openAdvertisementReviewStore(t *testing.T) *dao.Store {
	t.Helper()
	store, err := dao.Open(context.Background(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("dao.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestParseAdvertisementReviewInputPreservesOriginalTraceIDAndUnknownFields(t *testing.T) {
	raw := []byte(`{"trace_id":"ad-original-001","scene":"prompt","label":"safe","prompt":"普通介绍","quality_score":0.9,"extended_info":{"attack_scenario":""},"future_field":{"nested":true}}`)
	got, err := parseAdvertisementReviewInput(raw)
	if err != nil {
		t.Fatalf("parseAdvertisementReviewInput() error = %v", err)
	}
	if got.TraceID != "ad-original-001" || got.Scene != "prompt" || got.Label != "safe" || got.Prompt != "普通介绍" || got.ExtendedInfo.AttackScenario != "" {
		t.Fatalf("parsed fields = %+v", got)
	}
	var rawValue, gotValue any
	if err := json.Unmarshal(raw, &rawValue); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if err := json.Unmarshal(got.Raw, &gotValue); err != nil {
		t.Fatalf("unmarshal preserved raw: %v", err)
	}
	if !reflectDeepEqual(rawValue, gotValue) {
		t.Fatal("unknown fields were not preserved")
	}
}

func TestParseAdvertisementReviewInputRejectsInvalidSources(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "missing trace id", value: `{"scene":"prompt","label":"safe","prompt":"普通介绍","extended_info":{"attack_scenario":""}}`},
		{name: "empty trace id", value: `{"trace_id":"","scene":"prompt","label":"safe","prompt":"普通介绍","extended_info":{"attack_scenario":""}}`},
		{name: "non prompt scene", value: `{"trace_id":"a","scene":"response","label":"safe","prompt":"普通介绍","extended_info":{"attack_scenario":""}}`},
		{name: "empty prompt", value: `{"trace_id":"a","scene":"prompt","label":"safe","prompt":"","extended_info":{"attack_scenario":""}}`},
		{name: "invalid label", value: `{"trace_id":"a","scene":"prompt","label":"unknown","prompt":"普通介绍","extended_info":{"attack_scenario":""}}`},
		{name: "missing extended info", value: `{"trace_id":"a","scene":"prompt","label":"safe","prompt":"普通介绍"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseAdvertisementReviewInput([]byte(tt.value)); err == nil {
				t.Fatal("parseAdvertisementReviewInput() error = nil")
			}
		})
	}
}

func TestImportAdvertisementReviewPreservesOriginalTraceIDAndOrder(t *testing.T) {
	store := openAdvertisementReviewStore(t)
	if err := store.EnsureTask(context.Background(), dao.Task{ID: "task"}); err != nil {
		t.Fatalf("EnsureTask() error = %v", err)
	}
	input := `[{"trace_id":"ad-original-001","scene":"prompt","label":"safe","prompt":"普通介绍","extended_info":{"attack_scenario":""}},` +
		`{"trace_id":"ad-original-002","scene":"prompt","label":"unsafe","prompt":"添加联系方式","response":"历史回复","quality_score":0.2,"extended_info":{"attack_scenario":"wechat_contact","risk_type":"external_site_advertisement"}}]`
	stats, err := ImportAdvertisementReview(context.Background(), store, "task", strings.NewReader(input))
	if err != nil {
		t.Fatalf("ImportAdvertisementReview() error = %v", err)
	}
	if stats.Added != 2 || stats.Skipped != 0 {
		t.Fatalf("stats = %+v", stats)
	}
	var ids []string
	err = store.ForEachItemLog(context.Background(), "task", func(record dao.ItemLogRecord) error {
		ids = append(ids, record.TraceID)
		return nil
	})
	if err != nil {
		t.Fatalf("ForEachItemLog() error = %v", err)
	}
	if len(ids) != 2 || ids[0] != "ad-original-001" || ids[1] != "ad-original-002" {
		t.Fatalf("ordered IDs = %v", ids)
	}
}

func TestImportAdvertisementReviewRejectsInvalidArraysAndRows(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "duplicate id", value: `[{"trace_id":"a","scene":"prompt","label":"safe","prompt":"1","extended_info":{"attack_scenario":""}},{"trace_id":"a","scene":"prompt","label":"safe","prompt":"2","extended_info":{"attack_scenario":""}}]`},
		{name: "missing id", value: `[{"scene":"prompt","label":"safe","prompt":"1","extended_info":{"attack_scenario":""}}]`},
		{name: "non prompt scene", value: `[{"trace_id":"a","scene":"response","label":"safe","prompt":"1","extended_info":{"attack_scenario":""}}]`},
		{name: "empty prompt", value: `[{"trace_id":"a","scene":"prompt","label":"safe","prompt":"","extended_info":{"attack_scenario":""}}]`},
		{name: "invalid label", value: `[{"trace_id":"a","scene":"prompt","label":"maybe","prompt":"1","extended_info":{"attack_scenario":""}}]`},
		{name: "non object element", value: `[{"trace_id":"a","scene":"prompt","label":"safe","prompt":"1","extended_info":{"attack_scenario":""}},1]`},
		{name: "malformed array", value: `[{"trace_id":"a","scene":"prompt","label":"safe","prompt":"1"`},
		{name: "trailing json", value: `[{"trace_id":"a","scene":"prompt","label":"safe","prompt":"1","extended_info":{"attack_scenario":""}}] extra`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := openAdvertisementReviewStore(t)
			if _, err := ImportAdvertisementReview(context.Background(), store, "task", strings.NewReader(tt.value)); err == nil {
				t.Fatal("ImportAdvertisementReview() error = nil")
			}
		})
	}
}

func TestImportAdvertisementReviewRejectsTopLevelObject(t *testing.T) {
	store := openAdvertisementReviewStore(t)
	input := `{"trace_id":"a","scene":"prompt","label":"safe","prompt":"1","extended_info":{"attack_scenario":""}}`
	if _, err := ImportAdvertisementReview(context.Background(), store, "task", strings.NewReader(input)); err == nil {
		t.Fatal("ImportAdvertisementReview() error = nil")
	}
}

func reflectDeepEqual(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

func advertisementReviewItems(t *testing.T, source string) []dao.Item {
	t.Helper()
	raws := []string{
		`{"trace_id":"ad-original-001","scene":"prompt","label":"safe","prompt":"普通介绍","response":"旧回复","explanation":"旧解释","source":"source","quality_score":0.9,"extended_info":{"attack_scenario":""}}`,
		`{"trace_id":"ad-original-002","scene":"prompt","label":"unsafe","prompt":"添加联系方式","response":"旧回复","explanation":"旧解释","source":"source","quality_score":0.8,"extended_info":{"attack_scenario":"wechat_contact","risk_type":"external_site_advertisement"}}`,
	}
	if source != "" {
		raws = []string{source}
	}
	items := make([]dao.Item, 0, len(raws))
	for index, raw := range raws {
		parsed, err := parseAdvertisementReviewInput([]byte(raw))
		if err != nil {
			t.Fatalf("parseAdvertisementReviewInput() error = %v", err)
		}
		items = append(items, dao.Item{
			TaskID:     "task",
			TraceID:    parsed.TraceID,
			InputIndex: int64(index + 1),
			RawJSON:    parsed.Raw,
			Prompt:     parsed.Prompt,
		})
	}
	return items
}

func TestAdvertisementReviewRequestBlindsForbiddenSourceFields(t *testing.T) {
	items := advertisementReviewItems(t, "")
	request, err := advertisementReviewBatchRequest(items, []byte("system"), json.RawMessage(`{"r":[]}`), "json_object")
	if err != nil {
		t.Fatalf("advertisementReviewBatchRequest() error = %v", err)
	}
	if len(request.Messages) != 2 || request.Messages[0].Role != "system" || request.Messages[1].Role != "user" {
		t.Fatalf("request messages = %+v", request.Messages)
	}
	var payload struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal([]byte(request.Messages[1].Content), &payload); err != nil {
		t.Fatalf("unmarshal user payload: %v", err)
	}
	if len(payload.Items) != 2 {
		t.Fatalf("payload items = %d", len(payload.Items))
	}
	for _, item := range payload.Items {
		if len(item) != 3 {
			t.Fatalf("item keys = %v", item)
		}
		for _, key := range []string{"i", "p", "s"} {
			if _, ok := item[key]; !ok {
				t.Fatalf("item missing %s: %v", key, item)
			}
		}
	}
	serialized := request.Messages[1].Content
	for _, forbidden := range []string{"ad-original-001", "ad-original-002", `"label"`, `"trace_id"`, `"response"`, `"explanation"`, `"source"`, `"quality_score"`, `"external_site_advertisement"`} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("model payload contains forbidden value %q", forbidden)
		}
	}
}

func TestAdvertisementReviewResultsValidatesClosedContract(t *testing.T) {
	riskTypes := map[string]struct{}{"jailbreak": {}, "other_illegal_or_prohibited_content": {}}
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "valid uncertain", value: `{"r":[{"i":0,"l":0,"x":"","s":0}]}`},
		{name: "valid safe", value: `{"r":[{"i":0,"l":1,"x":"","s":0}]}`},
		{name: "valid unsafe with overlap", value: `{"r":[{"i":0,"l":2,"x":"jailbreak","s":1}]}`},
		{name: "cardinality missing", value: `{"r":[]}`, wantErr: true},
		{name: "cardinality extra", value: `{"r":[{"i":0,"l":1,"x":"","s":0},{"i":1,"l":1,"x":"","s":0}]}`, wantErr: true},
		{name: "duplicate index", value: `{"r":[{"i":0,"l":1,"x":"","s":0},{"i":0,"l":2,"x":"","s":0}]}`, wantErr: true},
		{name: "out of range index", value: `{"r":[{"i":1,"l":1,"x":"","s":0}]}`, wantErr: true},
		{name: "missing index", value: `{"r":[{"i":0,"l":1,"x":"","s":0},{"i":0,"l":1,"x":"","s":0}]}`, wantErr: true},
		{name: "invalid label", value: `{"r":[{"i":0,"l":3,"x":"","s":0}]}`, wantErr: true},
		{name: "invalid scenario", value: `{"r":[{"i":0,"l":1,"x":"","s":2}]}`, wantErr: true},
		{name: "unknown risk", value: `{"r":[{"i":0,"l":2,"x":"unknown","s":0}]}`, wantErr: true},
		{name: "safe with risk", value: `{"r":[{"i":0,"l":1,"x":"jailbreak","s":0}]}`, wantErr: true},
		{name: "extra result field", value: `{"r":[{"i":0,"l":1,"x":"","s":0,"z":1}]}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := advertisementReviewBatchResults([]byte(tt.value), 1, riskTypes)
			if (err != nil) != tt.wantErr {
				t.Fatalf("advertisementReviewBatchResults() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAdvertisementReviewPackingIsConsecutiveGreedyAndBounded(t *testing.T) {
	items := advertisementReviewItems(t, "")
	items = append(items, items...)
	cfg := advertisementReviewPackingConfig{
		SystemPrompt:        []byte("system"),
		Schema:              json.RawMessage(`{"r":[]}`),
		Mode:                "json_object",
		BatchSize:           3,
		BatchMaxInputTokens: 2,
		MaxTokens:           10,
		Estimate: func(messages []dto.Message, _ int) int {
			var payload struct {
				Items []struct{} `json:"items"`
			}
			if err := json.Unmarshal([]byte(messages[1].Content), &payload); err != nil {
				t.Errorf("unmarshal candidate payload: %v", err)
				return 1 << 30
			}
			return len(payload.Items)
		},
	}
	batches, err := packAdvertisementReviewBatches(items, cfg)
	if err != nil {
		t.Fatalf("packAdvertisementReviewBatches() error = %v", err)
	}
	if len(batches) != 2 || len(batches[0]) != 2 || len(batches[1]) != 2 {
		t.Fatalf("batch sizes = %v", batchSizes(batches))
	}
	for _, batch := range batches {
		candidate := append([]dao.Item(nil), batch...)
		candidate = append(candidate, items[0])
		candidateRequest, err := advertisementReviewBatchRequest(candidate, cfg.SystemPrompt, cfg.Schema, cfg.Mode)
		if err != nil {
			t.Fatalf("build candidate request: %v", err)
		}
		candidateTokens := cfg.Estimate(candidateRequest.Messages, cfg.MaxTokens)
		if candidateTokens <= cfg.BatchMaxInputTokens {
			t.Fatalf("candidate with appended first row fit token cap: %d", candidateTokens)
		}
	}
}

func TestAdvertisementReviewPackingRespectsCountCap(t *testing.T) {
	items := advertisementReviewItems(t, "")
	items = append(items, items...)
	cfg := advertisementReviewPackingConfig{
		SystemPrompt:        []byte("system"),
		Schema:              json.RawMessage(`{"r":[]}`),
		Mode:                "json_object",
		BatchSize:           2,
		BatchMaxInputTokens: 100,
		MaxTokens:           10,
		Estimate:            func([]dto.Message, int) int { return 1 },
	}
	batches, err := packAdvertisementReviewBatches(items, cfg)
	if err != nil {
		t.Fatalf("packAdvertisementReviewBatches() error = %v", err)
	}
	if len(batches) != 2 || len(batches[0]) != 2 || len(batches[1]) != 2 {
		t.Fatalf("batch sizes = %v", batchSizes(batches))
	}
}

func TestAdvertisementReviewPackingKeepsSingleOversizedRow(t *testing.T) {
	items := advertisementReviewItems(t, "")
	cfg := advertisementReviewPackingConfig{
		SystemPrompt:        []byte("system"),
		Schema:              json.RawMessage(`{"r":[]}`),
		Mode:                "json_object",
		BatchSize:           3,
		BatchMaxInputTokens: 1,
		MaxTokens:           10,
		Estimate:            func([]dto.Message, int) int { return 1 << 30 },
	}
	batches, err := packAdvertisementReviewBatches(items, cfg)
	if err != nil {
		t.Fatalf("packAdvertisementReviewBatches() error = %v", err)
	}
	if len(batches) != 2 || len(batches[0]) != 1 || len(batches[1]) != 1 {
		t.Fatalf("batch sizes = %v", batchSizes(batches))
	}
}

func batchSizes(batches [][]dao.Item) []int {
	sizes := make([]int, len(batches))
	for index, batch := range batches {
		sizes[index] = len(batch)
	}
	return sizes
}

type advertisementFakeCompleter struct {
	calls        atomic.Int32
	invalidCalls atomic.Int32
	respond      func(context.Context, dto.CompletionRequest) (dto.CompletionResponse, error)
}

func (f *advertisementFakeCompleter) Complete(
	ctx context.Context,
	req dto.CompletionRequest,
) (dto.CompletionResponse, error) {
	f.calls.Add(1)
	return f.respond(ctx, req)
}

func advertisementReviewSource(traceID, label, prompt, scenario string) string {
	return fmt.Sprintf(`{"trace_id":%q,"scene":"prompt","label":%q,"prompt":%q,"extended_info":{"attack_scenario":%q}}`, traceID, label, prompt, scenario)
}

func writeAdvertisementReviewInput(t *testing.T, rows ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input.json")
	contents := "[" + strings.Join(rows, ",") + "]"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write input: %v", err)
	}
	return path
}

func advertisementReviewRunConfig(
	t *testing.T,
	input string,
	completer Completer,
	stateDir string,
) AdvertisementReviewConfig {
	t.Helper()
	return AdvertisementReviewConfig{
		TaskID:              "task",
		InputPath:           input,
		OutputPath:          filepath.Join(stateDir, "clean.original.jsonl"),
		StatePath:           filepath.Join(stateDir, "state.db"),
		SemanticHash:        "semantic-hash",
		SystemPrompt:        []byte("system"),
		Schema:              json.RawMessage(`{"r":[]}`),
		Mode:                "json_object",
		Completer:           completer,
		Limiter:             newAdvertisementReviewLimiter(t, 1),
		RiskTypes:           map[string]struct{}{"jailbreak": {}},
		MaxTokens:           100,
		MaxAttempts:         1,
		RetryPolicy:         RetryPolicy{MaxAttempts: 1},
		Shutdown:            100 * time.Millisecond,
		BatchSize:           2,
		BatchMaxInputTokens: 1000,
	}
}

func newAdvertisementReviewLimiter(t *testing.T, concurrency int) *limiter.Limiter {
	t.Helper()
	created, err := limiter.New(limiter.Config{Concurrency: concurrency})
	if err != nil {
		t.Fatalf("limiter.New() error = %v", err)
	}
	return created
}

func advertisementReviewResponse(request dto.CompletionRequest, label int) dto.CompletionResponse {
	var payload struct {
		Items []struct {
			Index int `json:"i"`
		} `json:"items"`
	}
	_ = json.Unmarshal([]byte(request.Messages[1].Content), &payload)
	results := make([]advertisementReviewDecision, 0, len(payload.Items))
	for _, item := range payload.Items {
		results = append(results, advertisementReviewDecision{Index: item.Index, Label: label})
	}
	encoded, _ := json.Marshal(struct {
		Results []advertisementReviewDecision `json:"r"`
	}{Results: results})
	return dto.CompletionResponse{
		Content:     encoded,
		RawResponse: append([]byte(nil), encoded...),
		Usage:       dto.Usage{PromptTokens: 10, CompletionTokens: 2},
		APIKeyEnv:   "AI_GATEWAY_API_KEY",
	}
}

func TestAdvertisementReviewRunnerPartitionsAndExportsOriginalRows(t *testing.T) {
	input := writeAdvertisementReviewInput(t,
		advertisementReviewSource("ad-clean", "safe", "clean prompt", ""),
		advertisementReviewSource("ad-issue", "safe", "issue prompt", "wechat_contact"),
	)
	dir := t.TempDir()
	completer := &advertisementFakeCompleter{}
	completer.respond = func(_ context.Context, request dto.CompletionRequest) (dto.CompletionResponse, error) {
		var payload struct {
			Items []struct {
				Index  int    `json:"i"`
				Prompt string `json:"p"`
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(request.Messages[1].Content), &payload); err != nil {
			return dto.CompletionResponse{}, err
		}
		results := make([]advertisementReviewDecision, 0, len(payload.Items))
		for _, item := range payload.Items {
			label := 1
			if strings.Contains(item.Prompt, "issue") {
				label = 2
			}
			results = append(results, advertisementReviewDecision{Index: item.Index, Label: label})
		}
		encoded, _ := json.Marshal(struct {
			Results []advertisementReviewDecision `json:"r"`
		}{Results: results})
		return dto.CompletionResponse{Content: encoded, RawResponse: append([]byte(nil), encoded...), Usage: dto.Usage{PromptTokens: 10, CompletionTokens: 2}}, nil
	}
	cfg := advertisementReviewRunConfig(t, input, completer, dir)
	stats, err := AdvertisementReview(context.Background(), cfg)
	if err != nil {
		t.Fatalf("AdvertisementReview() error = %v", err)
	}
	if stats.Clean != 1 || stats.Issues != 1 || stats.SafeToUnsafe != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	clean := readJSONL(t, cfg.OutputPath)
	issues := readJSONL(t, filepath.Join(filepath.Dir(cfg.OutputPath), "issues.original.jsonl"))
	if len(clean) != 1 || clean[0]["trace_id"] != "ad-clean" {
		t.Fatalf("clean = %+v", clean)
	}
	if len(issues) != 1 || issues[0]["trace_id"] != "ad-issue" {
		t.Fatalf("issues = %+v", issues)
	}
	if stats.Requests != 1 || stats.SuccessfulRequests != 1 || stats.ItemsInSuccessfulRequests != 2 || stats.Attempts != 2 {
		t.Fatalf("request stats = %+v", stats)
	}
}

func TestAdvertisementReviewRunnerResumeDoesNotCallSucceededRows(t *testing.T) {
	input := writeAdvertisementReviewInput(t, advertisementReviewSource("ad-clean", "safe", "clean prompt", ""))
	dir := t.TempDir()
	first := &advertisementFakeCompleter{}
	first.respond = func(_ context.Context, request dto.CompletionRequest) (dto.CompletionResponse, error) {
		return advertisementReviewResponse(request, 1), nil
	}
	cfg := advertisementReviewRunConfig(t, input, first, dir)
	firstStats, err := AdvertisementReview(context.Background(), cfg)
	if err != nil {
		t.Fatalf("first AdvertisementReview() error = %v", err)
	}
	if firstStats.Attempts != 1 {
		t.Fatalf("first attempts = %d", firstStats.Attempts)
	}
	second := &advertisementFakeCompleter{}
	second.respond = func(context.Context, dto.CompletionRequest) (dto.CompletionResponse, error) {
		return dto.CompletionResponse{}, errors.New("succeeded row was called again")
	}
	cfg.Completer = second
	stats, err := AdvertisementReview(context.Background(), cfg)
	if err != nil {
		t.Fatalf("resume AdvertisementReview() error = %v", err)
	}
	if second.calls.Load() != 0 || stats.Clean != 1 || stats.Issues != 0 || stats.Attempts != 1 ||
		stats.Requests != 1 || stats.SuccessfulRequests != 1 || stats.ItemsInSuccessfulRequests != 1 ||
		stats.MinItemsPerSuccessfulRequest != 1 || stats.MaxItemsPerSuccessfulRequest != 1 ||
		stats.InputTokens != 10 || stats.OutputTokens != 2 {
		t.Fatalf("calls = %d, stats = %+v", second.calls.Load(), stats)
	}
}

func TestAdvertisementReviewRunnerRetriesInvalidResults(t *testing.T) {
	input := writeAdvertisementReviewInput(t, advertisementReviewSource("ad-clean", "safe", "clean prompt", ""))
	dir := t.TempDir()
	completer := &advertisementFakeCompleter{}
	completer.respond = func(_ context.Context, request dto.CompletionRequest) (dto.CompletionResponse, error) {
		if completer.calls.Load() == 1 {
			return dto.CompletionResponse{Content: []byte(`{"r":[]}`), RawResponse: []byte(`{"r":[]}`)}, nil
		}
		return advertisementReviewResponse(request, 1), nil
	}
	cfg := advertisementReviewRunConfig(t, input, completer, dir)
	cfg.MaxAttempts = 2
	cfg.RetryPolicy.MaxAttempts = 2
	stats, err := AdvertisementReview(context.Background(), cfg)
	if err != nil {
		t.Fatalf("AdvertisementReview() error = %v", err)
	}
	if completer.calls.Load() != 2 || stats.Clean != 1 || stats.Issues != 0 {
		t.Fatalf("calls = %d, stats = %+v", completer.calls.Load(), stats)
	}
}

func TestAdvertisementReviewRunnerSplitsInvalidWholeBatchResults(t *testing.T) {
	input := writeAdvertisementReviewInput(t,
		advertisementReviewSource("ad-one", "safe", "one", ""),
		advertisementReviewSource("ad-two", "unsafe", "two", "wechat_contact"),
	)
	dir := t.TempDir()
	completer := &advertisementFakeCompleter{}
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
			return dto.CompletionResponse{Content: []byte(`{"r":[]}`), RawResponse: []byte(`{"r":[]}`)}, nil
		}
		return advertisementReviewResponse(request, 1), nil
	}
	cfg := advertisementReviewRunConfig(t, input, completer, dir)
	stats, err := AdvertisementReview(context.Background(), cfg)
	if err != nil {
		t.Fatalf("AdvertisementReview() error = %v", err)
	}
	if completer.calls.Load() != 3 || stats.Clean+stats.Issues != 2 {
		t.Fatalf("calls = %d, stats = %+v", completer.calls.Load(), stats)
	}
}

func TestAdvertisementReviewRunnerMarksExhaustedProviderFailureAsIssue(t *testing.T) {
	input := writeAdvertisementReviewInput(t, advertisementReviewSource("ad-provider", "safe", "provider", ""))
	dir := t.TempDir()
	completer := &advertisementFakeCompleter{}
	completer.respond = func(context.Context, dto.CompletionRequest) (dto.CompletionResponse, error) {
		return dto.CompletionResponse{}, &dto.ProviderError{Kind: dto.ProviderServer, StatusCode: 500}
	}
	cfg := advertisementReviewRunConfig(t, input, completer, dir)
	stats, err := AdvertisementReview(context.Background(), cfg)
	if err != nil {
		t.Fatalf("AdvertisementReview() error = %v", err)
	}
	if stats.Clean != 0 || stats.Issues != 1 || stats.ProviderFailures != 1 || stats.Failed != 1 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestAdvertisementReviewRunnerCancellationDrainsOwnedWorkers(t *testing.T) {
	input := writeAdvertisementReviewInput(t, advertisementReviewSource("ad-blocked", "safe", "blocked", ""))
	dir := t.TempDir()
	started := make(chan struct{})
	returned := make(chan struct{})
	var once sync.Once
	completer := &advertisementFakeCompleter{}
	completer.respond = func(ctx context.Context, _ dto.CompletionRequest) (dto.CompletionResponse, error) {
		once.Do(func() { close(started) })
		defer close(returned)
		<-ctx.Done()
		return dto.CompletionResponse{}, ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-started
		cancel()
	}()
	_, err := AdvertisementReview(ctx, advertisementReviewRunConfig(t, input, completer, dir))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("AdvertisementReview() error = %v", err)
	}
	select {
	case <-returned:
	default:
		t.Fatal("owned completer did not drain before return")
	}
}

func TestAdvertisementReviewResultsRejectDuplicateZeroIndex(t *testing.T) {
	riskTypes := map[string]struct{}{"jailbreak": {}}
	raw := []byte(`{"r":[{"i":0,"l":1,"x":"","s":0},{"i":0,"l":2,"x":"","s":0}]}`)
	if _, err := advertisementReviewBatchResults(raw, 2, riskTypes); err == nil {
		t.Fatal("advertisementReviewBatchResults() accepted duplicate index zero")
	}
}

func TestAdvertisementReviewRunnerCountsNestedSplitAttempts(t *testing.T) {
	rows := make([]string, 0, 4)
	for index := range 4 {
		rows = append(rows, advertisementReviewSource(fmt.Sprintf("ad-nested-%d", index), "safe", "nested", ""))
	}
	input := writeAdvertisementReviewInput(t, rows...)
	dir := t.TempDir()
	completer := &advertisementFakeCompleter{}
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
			completer.invalidCalls.Add(1)
			return dto.CompletionResponse{Content: []byte(`{"r":[]}`), RawResponse: []byte(`{"r":[]}`)}, nil
		}
		if completer.invalidCalls.Load() < 7 {
			completer.invalidCalls.Add(1)
			return dto.CompletionResponse{Content: []byte(`{"r":[]}`), RawResponse: []byte(`{"r":[]}`)}, nil
		}
		return advertisementReviewResponse(request, 1), nil
	}
	cfg := advertisementReviewRunConfig(t, input, completer, dir)
	cfg.MaxAttempts = 4
	cfg.RetryPolicy.MaxAttempts = 4
	cfg.BatchSize = 4
	cfg.BatchMaxInputTokens = 100000
	stats, err := AdvertisementReview(context.Background(), cfg)
	if err != nil {
		t.Fatalf("AdvertisementReview() error = %v", err)
	}
	store, err := dao.Open(context.Background(), cfg.StatePath)
	if err != nil {
		t.Fatalf("reopen state: %v", err)
	}
	defer func() { _ = store.Close() }()
	var attemptNumbers []int
	if err := store.ForEachItemLog(context.Background(), cfg.TaskID, func(record dao.ItemLogRecord) error {
		var annotation struct {
			AttemptCount int `json:"a"`
		}
		if err := json.Unmarshal(record.Annotation, &annotation); err != nil {
			return err
		}
		attemptNumbers = append(attemptNumbers, annotation.AttemptCount)
		return nil
	}); err != nil {
		t.Fatalf("iterate state: %v", err)
	}
	if stats.Attempts != 24 || !reflectDeepEqual(attemptNumbers, []int{6, 6, 6, 6}) {
		t.Fatalf("stats.Attempts = %d, attemptNumbers = %v", stats.Attempts, attemptNumbers)
	}
}
