package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"sendllm/internal/dao"
	"sendllm/internal/dto"
	"sendllm/internal/lib/limiter"
	"sendllm/internal/lib/tokenizer"
)

const advertisementFullReviewPhase = "advertisement_full_review"

// AdvertisementFullReviewConfig 指定全量级联清洗单层的执行边界。
type AdvertisementFullReviewConfig struct {
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

// AdvertisementFullReviewStats 汇总单层全量复核的聚合状态。
type AdvertisementFullReviewStats struct {
	Added                 int64 `json:"added"`
	Skipped               int64 `json:"skipped"`
	Succeeded             int64 `json:"succeeded"`
	Failed                int64 `json:"failed"`
	Uncertain             int64 `json:"uncertain"`
	Safe                  int64 `json:"safe"`
	Unsafe                int64 `json:"unsafe"`
	Requests              int64 `json:"requests"`
	SuccessfulRequests    int64 `json:"successful_requests"`
	ItemsInSuccessfulReqs int64 `json:"items_in_successful_requests"`
	InputTokens           int64 `json:"input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
	ContextOverflows      int64 `json:"context_overflows"`
}

// advertisementFullReviewBatchItem 是模型请求中唯一可见的语义字段。
type advertisementFullReviewBatchItem struct {
	Index  int    `json:"i"`
	Prompt string `json:"p"`
}

// advertisementFullReviewBatchPayload 是单批模型用户消息。
type advertisementFullReviewBatchPayload struct {
	Items []advertisementFullReviewBatchItem `json:"items"`
}

// advertisementFullReviewDecision 是严格校验后的模型结论。
type advertisementFullReviewDecision struct {
	Index int    `json:"i"`
	Label int    `json:"l"`
	Risk  string `json:"x"`
}

// advertisementFullReviewPackingConfig 指定批次打包边界。
type advertisementFullReviewPackingConfig struct {
	SystemPrompt        []byte
	Schema              json.RawMessage
	Mode                string
	BatchSize           int
	BatchMaxInputTokens int
	MaxTokens           int
	Estimate            func([]dto.Message, int) int
}

// advertisementFullReviewRunner 持有可恢复单层任务的运行状态。
type advertisementFullReviewRunner struct {
	cfg     AdvertisementFullReviewConfig
	store   *dao.Store
	stats   AdvertisementFullReviewStats
	statsMu sync.Mutex
}

// AdvertisementFullReview 执行可恢复的单层全量复核并导出安全 decisions JSONL。
func AdvertisementFullReview(
	ctx context.Context,
	cfg AdvertisementFullReviewConfig,
) (AdvertisementFullReviewStats, error) {
	if err := validateAdvertisementFullReviewConfig(cfg); err != nil {
		return AdvertisementFullReviewStats{}, err
	}
	if cfg.OnProgress == nil {
		cfg.OnProgress = func(Summary) {}
	}
	if err := os.MkdirAll(filepath.Dir(cfg.StatePath), 0o755); err != nil {
		return AdvertisementFullReviewStats{}, fmt.Errorf("create full review state directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfg.OutputPath), 0o755); err != nil {
		return AdvertisementFullReviewStats{}, fmt.Errorf("create full review output directory: %w", err)
	}
	store, err := dao.Open(ctx, cfg.StatePath)
	if err != nil {
		return AdvertisementFullReviewStats{}, fmt.Errorf("open full review state: %w", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.EnsureTask(ctx, dao.Task{ID: cfg.TaskID, SemanticHash: cfg.SemanticHash}); err != nil {
		return AdvertisementFullReviewStats{}, fmt.Errorf("ensure full review task: %w", err)
	}
	input, err := os.Open(cfg.InputPath)
	if err != nil {
		return AdvertisementFullReviewStats{}, fmt.Errorf("open full review input: %w", err)
	}
	importStats, importErr := ImportAdvertisementFullReviewInput(ctx, store, cfg.TaskID, input)
	closeErr := input.Close()
	if importErr != nil {
		return AdvertisementFullReviewStats{}, importErr
	}
	if closeErr != nil {
		return AdvertisementFullReviewStats{}, fmt.Errorf("close full review input: %w", closeErr)
	}
	runner := advertisementFullReviewRunner{cfg: cfg, store: store}
	if err := runner.Run(ctx); err != nil {
		return AdvertisementFullReviewStats{}, err
	}
	runner.stats.Added = int64(importStats.Added)
	runner.stats.Skipped = int64(importStats.Skipped)
	exportStats, err := ExportAdvertisementFullReviewDecisions(ctx, store, cfg.TaskID, cfg.OutputPath)
	if err != nil {
		return AdvertisementFullReviewStats{}, err
	}
	runner.stats.Succeeded = exportStats.Succeeded
	runner.stats.Failed = exportStats.Failed
	runner.stats.Uncertain = exportStats.Uncertain
	runner.stats.Safe = exportStats.Safe
	runner.stats.Unsafe = exportStats.Unsafe
	return runner.stats, nil
}

// validateAdvertisementFullReviewConfig 校验启动前必须稳定的任务边界。
func validateAdvertisementFullReviewConfig(cfg AdvertisementFullReviewConfig) error {
	if cfg.TaskID == "" || cfg.InputPath == "" || cfg.OutputPath == "" ||
		cfg.StatePath == "" || cfg.SemanticHash == "" {
		return fmt.Errorf("advertisement full review configuration is incomplete")
	}
	if len(cfg.SystemPrompt) == 0 || len(cfg.Schema) == 0 || cfg.Mode == "" {
		return fmt.Errorf("advertisement full review model template is incomplete")
	}
	if cfg.Completer == nil || cfg.Limiter == nil || len(cfg.RiskTypes) == 0 {
		return fmt.Errorf("advertisement full review dependencies are incomplete")
	}
	if cfg.MaxTokens < 1 || cfg.MaxAttempts < 1 || cfg.Shutdown <= 0 {
		return fmt.Errorf("advertisement full review execution bounds are invalid")
	}
	if cfg.BatchSize < 1 || cfg.BatchMaxInputTokens < 1 {
		return fmt.Errorf("advertisement full review batch bounds are invalid")
	}
	return nil
}

// ImportAdvertisementFullReviewInput 原子导入 JSON 数组或 JSONL 输入。
func ImportAdvertisementFullReviewInput(
	ctx context.Context,
	store *dao.Store,
	taskID string,
	reader io.Reader,
) (ImportStats, error) {
	contents, err := io.ReadAll(reader)
	if err != nil {
		return ImportStats{}, fmt.Errorf("read full review input: %w", err)
	}
	trimmed := bytes.TrimSpace(contents)
	if len(trimmed) == 0 {
		return ImportStats{}, fmt.Errorf("full review input is empty")
	}
	var rows []json.RawMessage
	if trimmed[0] == '[' {
		if err := json.Unmarshal(trimmed, &rows); err != nil {
			return ImportStats{}, fmt.Errorf("decode full review array: %w", err)
		}
	} else {
		rows, err = readAdvertisementFullReviewLines(bytes.NewReader(trimmed))
		if err != nil {
			return ImportStats{}, err
		}
	}
	imp, err := store.BeginImport(ctx, taskID)
	if err != nil {
		return ImportStats{}, fmt.Errorf("begin full review import: %w", err)
	}
	defer func() { _ = imp.Rollback() }()
	var stats ImportStats
	seen := make(map[string]struct{}, len(rows))
	for index, raw := range rows {
		traceID, prompt, err := parseAdvertisementFullReviewSource(raw)
		if err != nil {
			return ImportStats{}, fmt.Errorf("parse full review row %d: %w", index+1, err)
		}
		if _, exists := seen[traceID]; exists {
			return ImportStats{}, fmt.Errorf("duplicate full review trace_id %q", traceID)
		}
		seen[traceID] = struct{}{}
		disposition, err := imp.Add(ctx, dao.Item{
			TaskID:     taskID,
			TraceID:    traceID,
			InputIndex: int64(index + 1),
			RawJSON:    append([]byte(nil), raw...),
			Prompt:     prompt,
			State:      dao.ItemPending,
		})
		if err != nil {
			return ImportStats{}, fmt.Errorf("import full review row %d: %w", index+1, err)
		}
		if disposition == dao.ImportAdded {
			stats.Added++
		} else {
			stats.Skipped++
		}
	}
	if err := imp.Commit(); err != nil {
		return ImportStats{}, fmt.Errorf("commit full review import: %w", err)
	}
	return stats, nil
}

// readAdvertisementFullReviewLines 读取严格 JSONL 输入。
func readAdvertisementFullReviewLines(reader io.Reader) ([]json.RawMessage, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), maxJSONLLineSize+1)
	rows := make([]json.RawMessage, 0)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		if err := validateAdvertisementFullReviewSingleJSON(line); err != nil {
			return nil, fmt.Errorf("full review line %d: %w", lineNumber, err)
		}
		rows = append(rows, append(json.RawMessage(nil), line...))
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan full review JSONL: %w", err)
	}
	return rows, nil
}

// parseAdvertisementFullReviewSource 抽取本地稳定 ID 和原始 prompt。
func parseAdvertisementFullReviewSource(raw []byte) (string, string, error) {
	var meta struct {
		TraceID string `json:"trace_id"`
		Prompt  string `json:"prompt"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return "", "", fmt.Errorf("decode source: %w", err)
	}
	if meta.TraceID == "" || meta.Prompt == "" {
		return "", "", fmt.Errorf("trace_id or prompt is empty")
	}
	return meta.TraceID, meta.Prompt, nil
}

// validateAdvertisementFullReviewSingleJSON 确认输入正好一个 JSON 值。
func validateAdvertisementFullReviewSingleJSON(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var value any
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

// advertisementFullReviewRequest 构造只包含 i 和原始 prompt 的模型请求。
func advertisementFullReviewRequest(
	items []dao.Item,
	systemPrompt []byte,
	schema json.RawMessage,
	mode string,
) (dto.CompletionRequest, error) {
	payload := advertisementFullReviewBatchPayload{Items: make([]advertisementFullReviewBatchItem, 0, len(items))}
	for index, item := range items {
		_, prompt, err := parseAdvertisementFullReviewSource(item.RawJSON)
		if err != nil {
			return dto.CompletionRequest{}, fmt.Errorf("parse full review source for trace_id %q: %w", item.TraceID, err)
		}
		payload.Items = append(payload.Items, advertisementFullReviewBatchItem{Index: index, Prompt: prompt})
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return dto.CompletionRequest{}, fmt.Errorf("encode full review model input: %w", err)
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

// advertisementFullReviewResults 严格校验一批模型结果。
func advertisementFullReviewResults(
	raw []byte,
	count int,
	riskTypes map[string]struct{},
) ([]advertisementFullReviewDecision, error) {
	var wire struct {
		Results []struct {
			Index *int    `json:"i"`
			Label *int    `json:"l"`
			Risk  *string `json:"x"`
		} `json:"r"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return nil, fmt.Errorf("decode full review response: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("full review response has multiple JSON values")
		}
		return nil, fmt.Errorf("read full review trailing response: %w", err)
	}
	if len(wire.Results) != count {
		return nil, fmt.Errorf("full review result count %d, want %d", len(wire.Results), count)
	}
	decisions := make([]advertisementFullReviewDecision, count)
	for position, result := range wire.Results {
		if result.Index == nil || result.Label == nil || result.Risk == nil {
			return nil, fmt.Errorf("full review result fields are missing")
		}
		if *result.Index != position {
			return nil, fmt.Errorf("full review index %d at position %d", *result.Index, position)
		}
		if *result.Label < 0 || *result.Label > 2 {
			return nil, fmt.Errorf("full review label %d is invalid", *result.Label)
		}
		if *result.Risk != "" {
			if _, ok := riskTypes[*result.Risk]; !ok {
				return nil, fmt.Errorf("full review risk is outside the 38-class closed set")
			}
			if *result.Label == 1 {
				return nil, fmt.Errorf("safe full review result cannot carry risk")
			}
		}
		decisions[position] = advertisementFullReviewDecision{
			Index: *result.Index,
			Label: *result.Label,
			Risk:  *result.Risk,
		}
	}
	return decisions, nil
}

// packAdvertisementFullReviewBatches 按连续顺序贪心打包，单条超限直接失败。
func packAdvertisementFullReviewBatches(
	items []dao.Item,
	cfg advertisementFullReviewPackingConfig,
) ([][]dao.Item, error) {
	if cfg.BatchSize < 1 || cfg.BatchMaxInputTokens < 1 {
		return nil, fmt.Errorf("full review packing bounds are invalid")
	}
	if len(cfg.SystemPrompt) == 0 || len(cfg.Schema) == 0 {
		return nil, fmt.Errorf("full review request template is incomplete")
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
		request, err := advertisementFullReviewRequest(candidate, cfg.SystemPrompt, cfg.Schema, cfg.Mode)
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

// Run 驱动单层全量 Worker 直到所有输入进入终态。
func (r *advertisementFullReviewRunner) Run(ctx context.Context) error {
	if _, err := r.store.ResetProcessing(ctx, r.cfg.TaskID); err != nil {
		return fmt.Errorf("reset interrupted full review task: %w", err)
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
		if !jobsClosed {
			close(jobs)
			jobsClosed = true
		}
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
			batches, err := packAdvertisementFullReviewBatches(claimed, advertisementFullReviewPackingConfig{
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

// worker 顺序处理调度器拥有的一个批次。
func (r *advertisementFullReviewRunner) worker(
	ctx context.Context,
	jobs <-chan []dao.Item,
	results chan<- workerResult,
) {
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

// processBatch 执行一次紧凑请求并逐条持久化结果。
func (r *advertisementFullReviewRunner) processBatch(ctx context.Context, items []dao.Item) error {
	request, err := advertisementFullReviewRequest(items, r.cfg.SystemPrompt, r.cfg.Schema, r.cfg.Mode)
	if err != nil {
		return err
	}
	estimatedTokens := tokenizer.Estimate(request.Messages, r.cfg.MaxTokens)
	if estimatedTokens > r.cfg.BatchMaxInputTokens && len(items) > 1 {
		r.addContextOverflow()
		return r.splitBatch(ctx, items)
	}
	startedAt := time.Now()
	release, err := r.cfg.Limiter.Acquire(ctx, estimatedTokens)
	if err != nil {
		if errors.Is(err, limiter.ErrTokenBudgetExceeded) && len(items) > 1 {
			r.addContextOverflow()
			return r.splitBatch(ctx, items)
		}
		return fmt.Errorf("acquire full review request permit: %w", err)
	}
	response, callErr := r.cfg.Completer.Complete(ctx, request)
	release()
	finishedAt := time.Now()
	attempt := dao.Attempt{
		Phase:        advertisementFullReviewPhase,
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
		if isAdvertisementContextFailure(callErr) {
			if len(items) > 1 {
				r.addContextOverflow()
				return r.splitBatch(ctx, items)
			}
			attempt.RequestNumber = r.nextAttemptNumber(items[0])
			return r.finishInvalid(ctx, items[0], attempt, "context_length", "single item exceeds context limit")
		}
		return r.handleCallFailure(ctx, items, attempt, callErr)
	}
	decisions, parseErr := advertisementFullReviewResults(response.Content, len(items), r.cfg.RiskTypes)
	if parseErr != nil {
		if len(items) > 1 {
			return r.splitInvalidBatch(ctx, items, attempt)
		}
		attempt.RequestNumber = r.nextAttemptNumber(items[0])
		return r.finishInvalid(ctx, items[0], attempt, "invalid_result", "batch response is invalid")
	}
	r.recordSuccessfulRequest(len(items), response.Usage)
	for index, item := range items {
		itemAttempt := attempt
		itemAttempt.RequestNumber = r.nextAttemptNumber(item)
		encoded, err := json.Marshal(decisions[index])
		if err != nil {
			return fmt.Errorf("encode full review decision for trace_id %q: %w", item.TraceID, err)
		}
		if err := r.store.MarkSucceeded(ctx, r.cfg.TaskID, item.TraceID, itemAttempt, encoded); err != nil {
			return err
		}
	}
	return nil
}

// isAdvertisementContextFailure 识别供应商明确的上下文长度拒绝。
func isAdvertisementContextFailure(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	var providerErr *dto.ProviderError
	if errors.As(err, &providerErr) && providerErr.Err != nil {
		message = strings.ToLower(providerErr.Err.Error())
	}
	return strings.Contains(message, "context length") ||
		strings.Contains(message, "context window") ||
		strings.Contains(message, "maximum context")
}

// handleCallFailure 记录供应商或任务级错误。
func (r *advertisementFullReviewRunner) handleCallFailure(
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
		return fmt.Errorf("task-level full review failure: %w", callErr)
	}
	return nil
}

// finishInvalid 根据尝试上限调度重试或标记最终失败。
func (r *advertisementFullReviewRunner) finishInvalid(
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

// splitInvalidBatch 记录非法整批尝试并二分重试。
func (r *advertisementFullReviewRunner) splitInvalidBatch(
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

// splitBatch 将上下文或格式问题二分到单条。
func (r *advertisementFullReviewRunner) splitBatch(ctx context.Context, items []dao.Item) error {
	if len(items) == 1 {
		attempt := dao.Attempt{
			Phase:         advertisementFullReviewPhase,
			RequestNumber: r.nextAttemptNumber(items[0]),
			StartedAt:     time.Now(),
			FinishedAt:    time.Now(),
			ErrorCategory: "context_length",
		}
		return r.store.MarkFailed(
			ctx,
			r.cfg.TaskID,
			items[0].TraceID,
			attempt,
			"context_length",
			"single item exceeds context limit",
		)
	}
	mid := len(items) / 2
	if err := r.processBatch(ctx, items[:mid]); err != nil {
		return err
	}
	return r.processBatch(ctx, items[mid:])
}

// nextAttemptNumber 为一条记录分配连续尝试序号。
func (r *advertisementFullReviewRunner) nextAttemptNumber(item dao.Item) int {
	number := item.RequestAttempts + 1
	if number < 1 {
		number = 1
	}
	return number
}

// recordSuccessfulRequest 记录成功请求的聚合统计。
func (r *advertisementFullReviewRunner) recordSuccessfulRequest(itemCount int, usage dto.Usage) {
	r.statsMu.Lock()
	defer r.statsMu.Unlock()
	r.stats.Requests++
	r.stats.SuccessfulRequests++
	r.stats.ItemsInSuccessfulReqs += int64(itemCount)
	r.stats.InputTokens += int64(usage.PromptTokens)
	r.stats.OutputTokens += int64(usage.CompletionTokens)
}

// addContextOverflow 记录未解决的上下文拆分。
func (r *advertisementFullReviewRunner) addContextOverflow() {
	r.statsMu.Lock()
	defer r.statsMu.Unlock()
	r.stats.ContextOverflows++
}

// waitForRetry 等待最早的重试时间。
func (r *advertisementFullReviewRunner) waitForRetry(ctx context.Context) error {
	next, ok, err := r.store.NextRetryAt(ctx, r.cfg.TaskID)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("full review has unfinished items without retry schedule")
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

// reportProgress 输出只包含安全聚合字段的进度。
func (r *advertisementFullReviewRunner) reportProgress(ctx context.Context, tracker progressTracker) {
	counts, err := r.store.Counts(ctx, r.cfg.TaskID)
	if err != nil {
		return
	}
	r.cfg.OnProgress(tracker.summary(time.Now(), counts))
}

// ExportAdvertisementFullReviewDecisions 从 SQLite 终态导出无 prompt 的 decisions JSONL。
func ExportAdvertisementFullReviewDecisions(
	ctx context.Context,
	store *dao.Store,
	taskID string,
	outputPath string,
) (AdvertisementFullReviewStats, error) {
	if outputPath == "" {
		return AdvertisementFullReviewStats{}, fmt.Errorf("full review output path is empty")
	}
	temporary, err := os.CreateTemp(filepath.Dir(outputPath), ".advertisement-full-review-*.jsonl")
	if err != nil {
		return AdvertisementFullReviewStats{}, fmt.Errorf("create full review output: %w", err)
	}
	tempPath := temporary.Name()
	remove := true
	defer func() {
		if temporary != nil {
			_ = temporary.Close()
		}
		if remove {
			_ = os.Remove(tempPath)
		}
	}()
	buffered := bufio.NewWriter(temporary)
	encoder := json.NewEncoder(buffered)
	var stats AdvertisementFullReviewStats
	if err := store.ForEachItemLog(ctx, taskID, func(record dao.ItemLogRecord) error {
		decision := map[string]any{"trace_id": record.TraceID}
		switch record.State {
		case dao.ItemSucceeded:
			var result advertisementFullReviewDecision
			if err := json.Unmarshal(record.Annotation, &result); err != nil {
				return fmt.Errorf("decode full review annotation: %w", err)
			}
			decision["state"] = "succeeded"
			decision["l"] = result.Label
			decision["x"] = result.Risk
			stats.Succeeded++
			switch result.Label {
			case 0:
				stats.Uncertain++
			case 1:
				stats.Safe++
			case 2:
				stats.Unsafe++
			}
		case dao.ItemFailed:
			decision["state"] = "failed"
			decision["error_category"] = record.ErrorCategory
			stats.Failed++
		default:
			return fmt.Errorf("full review trace_id %q is not terminal", record.TraceID)
		}
		if err := encoder.Encode(decision); err != nil {
			return fmt.Errorf("encode full review decision: %w", err)
		}
		return nil
	}); err != nil {
		return AdvertisementFullReviewStats{}, fmt.Errorf("export full review decisions: %w", err)
	}
	if err := buffered.Flush(); err != nil {
		return AdvertisementFullReviewStats{}, fmt.Errorf("flush full review decisions: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return AdvertisementFullReviewStats{}, fmt.Errorf("sync full review decisions: %w", err)
	}
	if err := temporary.Close(); err != nil {
		temporary = nil
		return AdvertisementFullReviewStats{}, fmt.Errorf("close full review decisions: %w", err)
	}
	temporary = nil
	if err := os.Rename(tempPath, outputPath); err != nil {
		return AdvertisementFullReviewStats{}, fmt.Errorf("publish full review decisions: %w", err)
	}
	remove = false
	return stats, nil
}
