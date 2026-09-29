package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"sendllm/internal/dao"
	"sendllm/internal/dto"
	"sendllm/internal/lib/limiter"
	"sendllm/internal/lib/tokenizer"
)

// advertisementExtendedInfo 只抽取复核所需的广告小类。
type advertisementExtendedInfo struct {
	AttackScenario string `json:"attack_scenario"`
}

// advertisementReviewInput 表示一条保持原样的广告复核输入。
type advertisementReviewInput struct {
	TraceID      string                    `json:"trace_id"`
	Scene        string                    `json:"scene"`
	Label        string                    `json:"label"`
	Prompt       string                    `json:"prompt"`
	ExtendedInfo advertisementExtendedInfo `json:"extended_info"`
	Raw          json.RawMessage           `json:"-"`
}

// ImportAdvertisementReview 原子导入广告 JSON 数组并保留原始 trace_id。
func ImportAdvertisementReview(ctx context.Context, store *dao.Store, taskID string, reader io.Reader) (ImportStats, error) {
	imp, err := store.BeginImport(ctx, taskID)
	if err != nil {
		return ImportStats{}, fmt.Errorf("begin advertisement review import: %w", err)
	}
	defer func() { _ = imp.Rollback() }()

	decoder := json.NewDecoder(reader)
	decoder.UseNumber()
	startToken, err := decoder.Token()
	if err != nil {
		return ImportStats{}, fmt.Errorf("read advertisement array start: %w", err)
	}
	if startToken != json.Delim('[') {
		return ImportStats{}, fmt.Errorf("advertisement input is not a top-level array")
	}

	seen := make(map[string]struct{})
	var stats ImportStats
	var inputIndex int64
	for decoder.More() {
		inputIndex++
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return ImportStats{}, fmt.Errorf("decode advertisement element %d: %w", inputIndex, err)
		}
		record, err := parseAdvertisementReviewInput(raw)
		if err != nil {
			return ImportStats{}, fmt.Errorf("parse advertisement element %d: %w", inputIndex, err)
		}
		if _, exists := seen[record.TraceID]; exists {
			return ImportStats{}, fmt.Errorf("duplicate advertisement trace_id %q", record.TraceID)
		}
		seen[record.TraceID] = struct{}{}
		disposition, err := imp.Add(ctx, dao.Item{
			TaskID:     taskID,
			TraceID:    record.TraceID,
			InputIndex: inputIndex,
			RawJSON:    record.Raw,
			Prompt:     record.Prompt,
			State:      dao.ItemPending,
		})
		if err != nil {
			return ImportStats{}, fmt.Errorf("import advertisement element %d: %w", inputIndex, err)
		}
		if disposition == dao.ImportAdded {
			stats.Added++
			continue
		}
		stats.Skipped++
	}
	endToken, err := decoder.Token()
	if err != nil {
		return ImportStats{}, fmt.Errorf("read advertisement array end: %w", err)
	}
	if endToken != json.Delim(']') {
		return ImportStats{}, fmt.Errorf("advertisement input has an invalid array end")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return ImportStats{}, fmt.Errorf("advertisement input has trailing JSON")
		}
		return ImportStats{}, fmt.Errorf("read advertisement trailing input: %w", err)
	}
	if err := imp.Commit(); err != nil {
		return ImportStats{}, fmt.Errorf("commit advertisement review import: %w", err)
	}
	return stats, nil
}

// parseAdvertisementReviewInput 校验复核必需字段并保留完整原始 JSON。
func parseAdvertisementReviewInput(raw []byte) (advertisementReviewInput, error) {
	var record advertisementReviewInput
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&record); err != nil {
		return advertisementReviewInput{}, fmt.Errorf("decode JSON: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return advertisementReviewInput{}, fmt.Errorf("multiple JSON values")
		}
		return advertisementReviewInput{}, fmt.Errorf("read trailing JSON: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return advertisementReviewInput{}, fmt.Errorf("decode JSON fields: %w", err)
	}
	for _, field := range []string{"trace_id", "scene", "label", "prompt", "extended_info"} {
		if _, ok := fields[field]; !ok {
			return advertisementReviewInput{}, fmt.Errorf("required field %s is missing", field)
		}
	}
	if record.TraceID == "" {
		return advertisementReviewInput{}, fmt.Errorf("trace_id is empty")
	}
	if record.Scene != "prompt" {
		return advertisementReviewInput{}, fmt.Errorf("scene is invalid")
	}
	if record.Label != "safe" && record.Label != "unsafe" {
		return advertisementReviewInput{}, fmt.Errorf("label is invalid")
	}
	if record.Prompt == "" {
		return advertisementReviewInput{}, fmt.Errorf("prompt is empty")
	}
	record.Raw = append(json.RawMessage(nil), raw...)
	return record, nil
}

// advertisementReviewBatchItem 表示一次请求内可见的最小语义输入。
type advertisementReviewBatchItem struct {
	Index    int    `json:"i"`
	Prompt   string `json:"p"`
	Scenario string `json:"s"`
}

// advertisementReviewBatchPayload 表示紧凑模型用户消息。
type advertisementReviewBatchPayload struct {
	Items []advertisementReviewBatchItem `json:"items"`
}

// advertisementReviewDecision 表示一条已通过本地闭集校验的模型结论。
type advertisementReviewDecision struct {
	Index           int    `json:"i"`
	Label           int    `json:"l"`
	LegacyRisk      string `json:"x"`
	ScenarioSuspect int    `json:"s"`
	AttemptCount    int    `json:"a,omitempty"`
	RequestItems    int    `json:"q,omitempty"`
	InputTokens     int    `json:"it,omitempty"`
	OutputTokens    int    `json:"ot,omitempty"`
}

// advertisementReviewPackingConfig 描述批量打包所需的固定边界。
type advertisementReviewPackingConfig struct {
	SystemPrompt        []byte
	Schema              json.RawMessage
	Mode                string
	BatchSize           int
	BatchMaxInputTokens int
	MaxTokens           int
	Estimate            func([]dto.Message, int) int
}

// advertisementReviewBatchRequest 构造只包含批内短序号、提示词和小类的请求。
func advertisementReviewBatchRequest(
	items []dao.Item,
	systemPrompt []byte,
	schema json.RawMessage,
	mode string,
) (dto.CompletionRequest, error) {
	payload := advertisementReviewBatchPayload{Items: make([]advertisementReviewBatchItem, 0, len(items))}
	for index, item := range items {
		source, err := parseAdvertisementReviewInput(item.RawJSON)
		if err != nil {
			return dto.CompletionRequest{}, fmt.Errorf("parse advertisement source for trace_id %q: %w", item.TraceID, err)
		}
		payload.Items = append(payload.Items, advertisementReviewBatchItem{
			Index:    index,
			Prompt:   source.Prompt,
			Scenario: source.ExtendedInfo.AttackScenario,
		})
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return dto.CompletionRequest{}, fmt.Errorf("encode advertisement review model input: %w", err)
	}
	return dto.CompletionRequest{
		Messages: []dto.Message{
			{Role: "system", Content: string(systemPrompt)},
			{Role: "user", Content: string(encoded)},
		},
		Schema: append(json.RawMessage(nil), schema...),
		Mode:   mode,
	}, nil
}

// advertisementReviewBatchResults 严格解析并校验一批紧凑模型结果。
func advertisementReviewBatchResults(
	raw []byte,
	count int,
	riskTypes map[string]struct{},
) ([]advertisementReviewDecision, error) {
	type rawDecision struct {
		Index           *int    `json:"i"`
		Label           *int    `json:"l"`
		LegacyRisk      *string `json:"x"`
		ScenarioSuspect *int    `json:"s"`
	}
	type rawResponse struct {
		Results []rawDecision `json:"r"`
	}
	var response rawResponse
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("decode advertisement review response: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("advertisement review response has multiple JSON values")
		}
		return nil, fmt.Errorf("read advertisement review trailing response: %w", err)
	}
	if len(response.Results) != count {
		return nil, fmt.Errorf("result count is %d, want %d", len(response.Results), count)
	}
	results := make([]advertisementReviewDecision, count)
	for _, result := range response.Results {
		if result.Index == nil || result.Label == nil || result.LegacyRisk == nil || result.ScenarioSuspect == nil {
			return nil, fmt.Errorf("result fields are missing")
		}
		index := *result.Index
		if index < 0 || index >= count {
			return nil, fmt.Errorf("result index %d is out of range", index)
		}
		if results[index].Index != 0 {
			return nil, fmt.Errorf("result index %d is duplicated", index)
		}
		if *result.Label < 0 || *result.Label > 2 {
			return nil, fmt.Errorf("result label %d is invalid", *result.Label)
		}
		if *result.ScenarioSuspect != 0 && *result.ScenarioSuspect != 1 {
			return nil, fmt.Errorf("result scenario flag %d is invalid", *result.ScenarioSuspect)
		}
		if *result.LegacyRisk != "" {
			if _, ok := riskTypes[*result.LegacyRisk]; !ok {
				return nil, fmt.Errorf("result legacy risk is not in the original 38 classes")
			}
			if *result.Label == 1 {
				return nil, fmt.Errorf("safe result cannot carry a legacy risk")
			}
		}
		results[index] = advertisementReviewDecision{
			Index:           index,
			Label:           *result.Label,
			LegacyRisk:      *result.LegacyRisk,
			ScenarioSuspect: *result.ScenarioSuspect,
		}
	}
	for index, result := range results {
		if result.Index != index {
			return nil, fmt.Errorf("result index %d is missing", index)
		}
	}
	return results, nil
}

// packAdvertisementReviewBatches 按连续顺序贪心装入批次，单条超限仍保留为独立批次。
func packAdvertisementReviewBatches(items []dao.Item, cfg advertisementReviewPackingConfig) ([][]dao.Item, error) {
	if cfg.BatchSize < 1 {
		return nil, fmt.Errorf("advertisement review batch size is invalid")
	}
	if cfg.BatchMaxInputTokens < 1 {
		return nil, fmt.Errorf("advertisement review token limit is invalid")
	}
	if len(cfg.SystemPrompt) == 0 || len(cfg.Schema) == 0 {
		return nil, fmt.Errorf("advertisement review request template is incomplete")
	}
	estimate := cfg.Estimate
	if estimate == nil {
		estimate = tokenizer.Estimate
	}
	batches := make([][]dao.Item, 0)
	current := make([]dao.Item, 0, cfg.BatchSize)
	for _, item := range items {
		candidate := make([]dao.Item, 0, len(current)+1)
		candidate = append(candidate, current...)
		candidate = append(candidate, item)
		request, err := advertisementReviewBatchRequest(candidate, cfg.SystemPrompt, cfg.Schema, cfg.Mode)
		if err != nil {
			return nil, err
		}
		tokens := estimate(request.Messages, cfg.MaxTokens)
		if tokens > cfg.BatchMaxInputTokens && len(current) > 0 {
			batches = append(batches, current)
			current = append(current[:0], item)
		} else {
			current = candidate
		}
		if len(current) == cfg.BatchSize {
			batches = append(batches, current)
			current = make([]dao.Item, 0, cfg.BatchSize)
		}
	}
	if len(current) != 0 {
		batches = append(batches, current)
	}
	return batches, nil
}

// AdvertisementReviewConfig 指定广告轻量复核所需的全部依赖和边界。
type AdvertisementReviewConfig struct {
	TaskID              string
	InputPath           string
	OutputPath          string
	StatePath           string
	SemanticHash        string
	SystemPrompt        []byte
	Schema              json.RawMessage
	Mode                string
	Completer           Completer
	Limiter             *limiter.Limiter
	RiskTypes           map[string]struct{}
	MaxTokens           int
	MaxAttempts         int
	RetryPolicy         RetryPolicy
	Shutdown            time.Duration
	BatchSize           int
	BatchMaxInputTokens int
	OnProgress          func(Summary)
}

// advertisementReviewRunner 持有一次可恢复任务的执行状态。
type advertisementReviewRunner struct {
	cfg             AdvertisementReviewConfig
	store           *dao.Store
	statsMu         sync.Mutex
	stats           AdvertisementReviewStats
	attemptCountsMu sync.Mutex
	attemptCounts   map[string]int
}

// AdvertisementReview 导入 JSON 数组、运行可恢复复核并导出四个原样产物。
func AdvertisementReview(ctx context.Context, cfg AdvertisementReviewConfig) (AdvertisementReviewStats, error) {
	if err := validateAdvertisementReviewConfig(cfg); err != nil {
		return AdvertisementReviewStats{}, err
	}
	if cfg.OnProgress == nil {
		cfg.OnProgress = func(Summary) {}
	}
	if err := os.MkdirAll(filepath.Dir(cfg.StatePath), 0o755); err != nil {
		return AdvertisementReviewStats{}, fmt.Errorf("create advertisement state directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfg.OutputPath), 0o755); err != nil {
		return AdvertisementReviewStats{}, fmt.Errorf("create advertisement output directory: %w", err)
	}
	store, err := dao.Open(ctx, cfg.StatePath)
	if err != nil {
		return AdvertisementReviewStats{}, fmt.Errorf("open advertisement review state: %w", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.EnsureTask(ctx, dao.Task{ID: cfg.TaskID, SemanticHash: cfg.SemanticHash}); err != nil {
		return AdvertisementReviewStats{}, fmt.Errorf("ensure advertisement review task: %w", err)
	}

	input, err := os.Open(cfg.InputPath)
	if err != nil {
		return AdvertisementReviewStats{}, fmt.Errorf("open advertisement review input: %w", err)
	}
	_, importErr := ImportAdvertisementReview(ctx, store, cfg.TaskID, input)
	closeErr := input.Close()
	if importErr != nil {
		return AdvertisementReviewStats{}, importErr
	}
	if closeErr != nil {
		return AdvertisementReviewStats{}, fmt.Errorf("close advertisement review input: %w", closeErr)
	}

	runner := advertisementReviewRunner{cfg: cfg, store: store, attemptCounts: make(map[string]int)}
	if err := runner.Run(ctx); err != nil {
		return AdvertisementReviewStats{}, err
	}
	return exportAdvertisementReview(ctx, store, cfg.TaskID, cfg.OutputPath, runner.stats)
}

// validateAdvertisementReviewConfig 校验启动前必须稳定的任务边界。
func validateAdvertisementReviewConfig(cfg AdvertisementReviewConfig) error {
	if cfg.TaskID == "" || cfg.InputPath == "" || cfg.OutputPath == "" || cfg.StatePath == "" || cfg.SemanticHash == "" {
		return fmt.Errorf("advertisement review configuration is incomplete")
	}
	if len(cfg.SystemPrompt) == 0 || len(cfg.Schema) == 0 || cfg.Mode == "" {
		return fmt.Errorf("advertisement review model template is incomplete")
	}
	if cfg.Completer == nil || cfg.Limiter == nil || len(cfg.RiskTypes) == 0 {
		return fmt.Errorf("advertisement review dependencies are incomplete")
	}
	if cfg.MaxTokens < 1 || cfg.MaxAttempts < 1 || cfg.Shutdown <= 0 {
		return fmt.Errorf("advertisement review execution bounds are invalid")
	}
	if cfg.BatchSize < 1 || cfg.BatchMaxInputTokens < 1 {
		return fmt.Errorf("advertisement review batch bounds are invalid")
	}
	return nil
}

// Run 领取、打包并驱动广告复核 Worker 到全部终态。
func (r *advertisementReviewRunner) Run(ctx context.Context) error {
	if _, err := r.store.ResetProcessing(ctx, r.cfg.TaskID); err != nil {
		return fmt.Errorf("reset interrupted advertisement review task: %w", err)
	}
	counts, err := r.store.Counts(ctx, r.cfg.TaskID)
	if err != nil {
		return err
	}
	tracker := newProgressTracker(time.Now(), counts)
	workerCount := r.cfg.Limiter.Concurrency()
	jobs := make(chan []dao.Item)
	results := make(chan workerResult)
	workCtx, cancelWork := context.WithCancelCause(context.WithoutCancel(ctx))
	defer cancelWork(nil)

	var workers sync.WaitGroup
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			r.worker(workCtx, jobs, results)
		}()
	}

	active := 0
	jobsClosed := false
	closeJobs := func() {
		if jobsClosed {
			return
		}
		close(jobs)
		jobsClosed = true
	}
	drainWorkers := func(cause error) error {
		closeJobs()
		timer := time.NewTimer(r.cfg.Shutdown)
		defer timer.Stop()
		for active > 0 {
			select {
			case <-timer.C:
				cancelWork(cause)
				workers.Wait()
				return cause
			case <-results:
				active--
			}
		}
		workers.Wait()
		return cause
	}
	defer closeJobs()

	for {
		if cause := context.Cause(ctx); cause != nil {
			return drainWorkers(cause)
		}
		if cause := context.Cause(workCtx); cause != nil {
			closeJobs()
			workers.Wait()
			return cause
		}
		capacity := workerCount - active
		for ; capacity > 0; capacity-- {
			claimed, err := r.store.Claim(ctx, r.cfg.TaskID, r.cfg.BatchSize, time.Now())
			if err != nil {
				cancelWork(err)
				closeJobs()
				workers.Wait()
				return err
			}
			if len(claimed) == 0 {
				break
			}
			batches, err := packAdvertisementReviewBatches(claimed, advertisementReviewPackingConfig{
				SystemPrompt:        r.cfg.SystemPrompt,
				Schema:              r.cfg.Schema,
				Mode:                r.cfg.Mode,
				BatchSize:           r.cfg.BatchSize,
				BatchMaxInputTokens: r.cfg.BatchMaxInputTokens,
				MaxTokens:           r.cfg.MaxTokens,
			})
			if err != nil {
				cancelWork(err)
				closeJobs()
				workers.Wait()
				return err
			}
			for _, batch := range batches {
				select {
				case jobs <- batch:
					active++
				case <-ctx.Done():
					return drainWorkers(context.Cause(ctx))
				}
			}
		}
		if active > 0 {
			select {
			case result := <-results:
				active--
				if result.err != nil {
					cancelWork(result.err)
					closeJobs()
					workers.Wait()
					return result.err
				}
				r.reportProgress(ctx, tracker)
			case <-ctx.Done():
				return drainWorkers(context.Cause(ctx))
			}
			continue
		}
		counts, err := r.store.Counts(ctx, r.cfg.TaskID)
		if err != nil {
			return err
		}
		if counts.Pending == 0 && counts.Processing == 0 && counts.RetryWait == 0 {
			r.cfg.OnProgress(tracker.summary(time.Now(), counts))
			closeJobs()
			workers.Wait()
			return nil
		}
		if err := r.waitForRetry(ctx); err != nil {
			return err
		}
	}
}

// worker 顺序处理调度器拥有的一个批次并交回结果。
func (r *advertisementReviewRunner) worker(ctx context.Context, jobs <-chan []dao.Item, results chan<- workerResult) {
	for {
		select {
		case <-ctx.Done():
			return
		case items, ok := <-jobs:
			if !ok {
				return
			}
			err := r.processBatch(ctx, items)
			select {
			case results <- workerResult{err: err}:
			case <-ctx.Done():
				return
			}
		}
	}
}

// processBatch 执行一次紧凑批量请求并把每行映射回原 trace_id。
func (r *advertisementReviewRunner) processBatch(ctx context.Context, items []dao.Item) error {
	request, err := advertisementReviewBatchRequest(items, r.cfg.SystemPrompt, r.cfg.Schema, r.cfg.Mode)
	if err != nil {
		return err
	}
	estimatedTokens := tokenizer.Estimate(request.Messages, r.cfg.MaxTokens)
	if estimatedTokens > r.cfg.BatchMaxInputTokens && len(items) > 1 {
		return r.splitBatch(ctx, items)
	}
	startedAt := time.Now()
	release, err := r.cfg.Limiter.Acquire(ctx, estimatedTokens)
	if err != nil {
		if errors.Is(err, limiter.ErrTokenBudgetExceeded) && len(items) > 1 {
			return r.splitBatch(ctx, items)
		}
		return fmt.Errorf("acquire advertisement review request permit: %w", err)
	}
	response, callErr := r.cfg.Completer.Complete(ctx, request)
	release()
	finishedAt := time.Now()
	attempt := dao.Attempt{
		Phase:        "advertisement_review_batch",
		StartedAt:    startedAt,
		FinishedAt:   finishedAt,
		APIKeyEnv:    response.APIKeyEnv,
		RawResponse:  append([]byte(nil), response.RawResponse...),
		InputTokens:  response.Usage.PromptTokens,
		OutputTokens: response.Usage.CompletionTokens,
	}
	var providerErr *dto.ProviderError
	if errors.As(callErr, &providerErr) {
		attempt.HTTPStatus = providerErr.StatusCode
	}
	if callErr != nil {
		return r.handleCallFailure(ctx, items, attempt, callErr)
	}
	decisions, parseErr := advertisementReviewBatchResults(response.Content, len(items), r.cfg.RiskTypes)
	if parseErr != nil {
		if len(items) > 1 {
			return r.splitInvalidBatch(ctx, items, attempt)
		}
		attempt.RequestNumber = r.nextAttemptNumber(items[0])
		return r.finishInvalid(ctx, items[0], attempt, "invalid_result", "batch response is invalid")
	}
	for index, item := range items {
		itemAttempt := attempt
		itemAttempt.RequestNumber = r.nextAttemptNumber(item)
		decision := decisions[index]
		decision.AttemptCount = itemAttempt.RequestNumber
		decision.RequestItems = len(items)
		decision.InputTokens = response.Usage.PromptTokens
		decision.OutputTokens = response.Usage.CompletionTokens
		encoded, err := json.Marshal(decision)
		if err != nil {
			return fmt.Errorf("encode advertisement decision for trace_id %q: %w", item.TraceID, err)
		}
		if err := r.store.MarkSucceeded(ctx, r.cfg.TaskID, item.TraceID, itemAttempt, encoded); err != nil {
			return err
		}
	}
	return nil
}

// handleCallFailure 记录供应商错误并按配置重试或落入问题池。
func (r *advertisementReviewRunner) handleCallFailure(
	ctx context.Context,
	items []dao.Item,
	attempt dao.Attempt,
	callErr error,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	decision := ClassifyFailure(callErr)
	attempt.ErrorCategory = decision.Category
	attempt.Retryable = decision.Retry
	delay := time.Duration(0)
	if decision.Retry {
		delay = r.cfg.RetryPolicy.Delay(1, decision.RetryAfter, fullJitter)
	}
	if decision.GlobalCooldown {
		r.cfg.Limiter.Cooldown(time.Now().Add(delay))
	}
	for _, item := range items {
		itemAttempt := attempt
		itemAttempt.RequestNumber = r.nextAttemptNumber(item)
		if isTaskLevelFailure(callErr) {
			if err := r.store.RecordAttempt(ctx, r.cfg.TaskID, item.TraceID, itemAttempt); err != nil {
				return err
			}
			continue
		}
		if err := r.finishInvalid(ctx, item, itemAttempt, decision.Category, "provider request failed"); err != nil {
			return err
		}
	}
	if isTaskLevelFailure(callErr) {
		return fmt.Errorf("task-level advertisement review failure: %w", callErr)
	}
	return nil
}

// finishInvalid 在未达尝试上限时调度重试，否则标记为最终问题。
func (r *advertisementReviewRunner) finishInvalid(
	ctx context.Context,
	item dao.Item,
	attempt dao.Attempt,
	category string,
	summary string,
) error {
	attempt.ErrorCategory = category
	if attempt.RequestNumber < r.cfg.MaxAttempts {
		attempt.Retryable = true
		delay := r.cfg.RetryPolicy.Delay(attempt.RequestNumber, 0, fullJitter)
		return r.store.ScheduleRetry(
			ctx,
			r.cfg.TaskID,
			item.TraceID,
			attempt,
			time.Now().Add(delay),
			category,
			summary,
		)
	}
	attempt.Retryable = false
	return r.store.MarkFailed(ctx, r.cfg.TaskID, item.TraceID, attempt, category, summary)
}

// splitInvalidBatch 保留尝试历史后递归拆分非法整批响应。
func (r *advertisementReviewRunner) splitInvalidBatch(
	ctx context.Context,
	items []dao.Item,
	attempt dao.Attempt,
) error {
	for index := range items {
		itemAttempt := attempt
		itemAttempt.RequestNumber = r.nextAttemptNumber(items[index])
		itemAttempt.ErrorCategory = "invalid_result"
		itemAttempt.Retryable = true
		if err := r.store.RecordAttempt(ctx, r.cfg.TaskID, items[index].TraceID, itemAttempt); err != nil {
			return err
		}
		items[index].RequestAttempts = itemAttempt.RequestNumber
	}
	return r.splitBatch(ctx, items)
}

// splitBatch 将上下文或响应问题二分到单条，单条由失败路径处理。
func (r *advertisementReviewRunner) splitBatch(ctx context.Context, items []dao.Item) error {
	if len(items) == 1 {
		attempt := dao.Attempt{
			Phase:         "advertisement_review_batch",
			RequestNumber: r.nextAttemptNumber(items[0]),
			StartedAt:     time.Now(),
			FinishedAt:    time.Now(),
			ErrorCategory: "context_length",
		}
		return r.store.MarkFailed(ctx, r.cfg.TaskID, items[0].TraceID, attempt, "context_length", "single item exceeds context limit")
	}
	mid := len(items) / 2
	if err := r.processBatch(ctx, items[:mid]); err != nil {
		return err
	}
	return r.processBatch(ctx, items[mid:])
}

// nextAttemptNumber 为一条记录分配进程内连续尝试序号。
func (r *advertisementReviewRunner) nextAttemptNumber(item dao.Item) int {
	r.attemptCountsMu.Lock()
	defer r.attemptCountsMu.Unlock()
	current := r.attemptCounts[item.TraceID]
	if current < item.RequestAttempts {
		current = item.RequestAttempts
	}
	current++
	r.attemptCounts[item.TraceID] = current
	return current
}

// waitForRetry 等待最早的重试时间到达。
func (r *advertisementReviewRunner) waitForRetry(ctx context.Context) error {
	next, ok, err := r.store.NextRetryAt(ctx, r.cfg.TaskID)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("advertisement review has unfinished items without retry schedule")
	}
	delay := time.Until(next)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// reportProgress 输出只包含聚合计数的安全进度。
func (r *advertisementReviewRunner) reportProgress(ctx context.Context, tracker progressTracker) {
	counts, err := r.store.Counts(ctx, r.cfg.TaskID)
	if err != nil {
		return
	}
	r.cfg.OnProgress(tracker.summary(time.Now(), counts))
}
