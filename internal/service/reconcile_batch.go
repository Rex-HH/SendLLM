package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"sendllm/internal/dao"
	"sendllm/internal/dto"
	"sendllm/internal/lib/limiter"
	"sendllm/internal/lib/tokenizer"
)

const reconcileBatchPhase = "batch_classification"

type reconcileBatchRunner struct {
	cfg   ReconcileConfig
	store *dao.Store
}

type reconcileBatchItem struct {
	TraceID             string                 `json:"trace_id"`
	Scene               string                 `json:"scene"`
	Prompt              string                 `json:"prompt"`
	Response            string                 `json:"response"`
	OriginalLabel       reconcileOriginalLabel `json:"original_label"`
	OriginalExplanation string                 `json:"original_explanation"`
}

type reconcileBatchPayload struct {
	Items []reconcileBatchItem `json:"items"`
}

type reconcileBatchEnvelope struct {
	Results []json.RawMessage `json:"results"`
}

// ReconcileBatch 以多条样本一次请求的方式复用 reconcile 导入、导出和校验逻辑。
func ReconcileBatch(ctx context.Context, cfg ReconcileConfig) (ExportStats, error) {
	if cfg.BatchSize < 1 {
		return ExportStats{}, fmt.Errorf("reconcile batch size is invalid")
	}
	if cfg.OnProgress == nil {
		cfg.OnProgress = func(Summary) {}
	}
	stats, err := reconcilePrepareAndRun(ctx, cfg, func(store *dao.Store) error {
		runner := reconcileBatchRunner{cfg: cfg, store: store}
		return runner.Run(ctx)
	})
	if err != nil {
		return stats, err
	}
	return stats, nil
}

func reconcilePrepareAndRun(
	ctx context.Context,
	cfg ReconcileConfig,
	run func(*dao.Store) error,
) (ExportStats, error) {
	if cfg.TaskID == "" || cfg.InputPath == "" || cfg.OutputPath == "" || cfg.StatePath == "" {
		return ExportStats{}, fmt.Errorf("reconcile configuration is incomplete")
	}
	if len(cfg.SystemPrompt) == 0 || len(cfg.Schema) == 0 || cfg.SemanticHash == "" {
		return ExportStats{}, fmt.Errorf("reconcile configuration is incomplete")
	}
	if cfg.Completer == nil || cfg.Validator == nil || cfg.Limiter == nil {
		return ExportStats{}, fmt.Errorf("reconcile dependencies are incomplete")
	}
	if cfg.Shutdown <= 0 {
		return ExportStats{}, fmt.Errorf("reconcile shutdown timeout is invalid")
	}
	runInfo := reconcileRunInfo{
		RunID:     reconcileRunID(time.Now()),
		Status:    "completed",
		StartedAt: time.Now(),
	}

	store, err := dao.Open(ctx, cfg.StatePath)
	if err != nil {
		return ExportStats{}, fmt.Errorf("open reconcile state: %w", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.EnsureTask(ctx, dao.Task{ID: cfg.TaskID, SemanticHash: cfg.SemanticHash}); err != nil {
		return ExportStats{}, fmt.Errorf("ensure reconcile task: %w", err)
	}

	stats, err := importReconcileFile(ctx, store, cfg.TaskID, cfg.InputPath)
	if err != nil {
		return ExportStats{}, err
	}
	if stats.Added > 0 || stats.Skipped > 0 {
		// 导入统计只用于确认输入已进入 SQLite，执行进度以状态库为准。
	}
	if _, err := store.ResetFailed(ctx, cfg.TaskID); err != nil {
		return ExportStats{}, fmt.Errorf("reset reconcile failed rows: %w", err)
	}

	if err := run(store); err != nil {
		if ctx.Err() != nil {
			runInfo.Status = "interrupted"
		} else {
			runInfo.Status = "failed"
		}
		exportCtx, cancelExport := adjudicateExportContext(ctx, cfg.Shutdown)
		exportStats, exportErr := exportReconcile(exportCtx, store, cfg.TaskID, cfg.OutputPath, cfg.Scene, runInfo)
		cancelExport()
		if exportErr != nil {
			return ExportStats{}, fmt.Errorf("reconcile run failed: %w; export failed: %v", err, exportErr)
		}
		return exportStats, err
	}

	return exportReconcile(ctx, store, cfg.TaskID, cfg.OutputPath, cfg.Scene, runInfo)
}

func importReconcileFile(ctx context.Context, store *dao.Store, taskID string, inputPath string) (ImportStats, error) {
	input, err := os.Open(inputPath)
	if err != nil {
		return ImportStats{}, fmt.Errorf("open reconcile input: %w", err)
	}
	stats, importErr := ImportReconcile(ctx, store, taskID, input)
	closeErr := input.Close()
	if importErr != nil {
		return ImportStats{}, importErr
	}
	if closeErr != nil {
		return ImportStats{}, fmt.Errorf("close reconcile input: %w", closeErr)
	}
	return stats, nil
}

func (r reconcileBatchRunner) Run(ctx context.Context) error {
	if _, err := r.store.ResetProcessing(ctx, r.cfg.TaskID); err != nil {
		return fmt.Errorf("reset interrupted reconcile batch task: %w", err)
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
			items, err := r.store.Claim(ctx, r.cfg.TaskID, r.cfg.BatchSize, time.Now())
			if err != nil {
				cancelWork(err)
				closeJobs()
				workers.Wait()
				return err
			}
			if len(items) == 0 {
				break
			}
			select {
			case jobs <- items:
				active++
			case result := <-results:
				active--
				if result.err != nil {
					cancelWork(result.err)
					closeJobs()
					workers.Wait()
					return result.err
				}
				r.reportProgress(ctx, tracker)
				capacity++
			case <-ctx.Done():
				return drainWorkers(context.Cause(ctx))
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

func (r reconcileBatchRunner) worker(ctx context.Context, jobs <-chan []dao.Item, results chan<- workerResult) {
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

func (r reconcileBatchRunner) processBatch(ctx context.Context, items []dao.Item) error {
	request, err := reconcileBatchRequest(items, r.cfg)
	if err != nil {
		return err
	}
	estimatedTokens := tokenizer.Estimate(request.Messages, r.cfg.MaxTokens)
	if r.cfg.BatchMaxInputTokens > 0 && estimatedTokens > r.cfg.BatchMaxInputTokens {
		return r.splitOrFailLargeBatch(ctx, items, "context_length", "batch request exceeds configured token limit")
	}

	startedAt := time.Now()
	release, err := r.cfg.Limiter.Acquire(ctx, estimatedTokens)
	if err != nil {
		if errors.Is(err, limiter.ErrTokenBudgetExceeded) && len(items) > 1 {
			return r.splitOrFailLargeBatch(ctx, items, "context_length", "batch request exceeds token budget")
		}
		return fmt.Errorf("acquire reconcile batch request permit: %w", err)
	}
	response, err := r.cfg.Completer.Complete(ctx, request)
	release()
	finishedAt := time.Now()
	attempt := dao.Attempt{
		Phase:        reconcileBatchPhase,
		StartedAt:    startedAt,
		FinishedAt:   finishedAt,
		APIKeyEnv:    response.APIKeyEnv,
		RawResponse:  append([]byte(nil), response.RawResponse...),
		InputTokens:  response.Usage.PromptTokens,
		OutputTokens: response.Usage.CompletionTokens,
	}
	var providerErr *dto.ProviderError
	if errors.As(err, &providerErr) {
		attempt.HTTPStatus = providerErr.StatusCode
	}
	if err != nil {
		return r.handleBatchCallFailure(ctx, items, attempt, err)
	}

	results, parseErr := reconcileBatchResults(response.Content)
	if parseErr != nil {
		if len(items) > 1 {
			return r.splitOrFailLargeBatch(ctx, items, "invalid_result", "batch response is invalid")
		}
		return r.finishMissingOrInvalid(ctx, items[0], attempt, "invalid_result", "batch response is invalid")
	}
	for _, item := range items {
		itemAttempt := attempt
		itemAttempt.RequestNumber = item.RequestAttempts + 1
		result, ok := results[item.TraceID]
		if !ok {
			if err := r.finishMissingOrInvalid(ctx, item, itemAttempt, "invalid_result", "batch response missing item"); err != nil {
				return err
			}
			continue
		}
		annotation, validationErr := r.cfg.Validator.Validate(result)
		if validationErr != nil {
			setValidationFailure(&itemAttempt, validationErr)
			if err := r.finishInvalidBatchResult(ctx, item, itemAttempt, result, validationErr); err != nil {
				return err
			}
			continue
		}
		encoded, err := json.Marshal(annotation)
		if err != nil {
			return fmt.Errorf("encode annotation for trace_id %q: %w", item.TraceID, err)
		}
		if err := r.store.MarkSucceeded(ctx, r.cfg.TaskID, item.TraceID, itemAttempt, encoded); err != nil {
			return err
		}
	}
	return nil
}

func reconcileBatchRequest(items []dao.Item, cfg ReconcileConfig) (dto.CompletionRequest, error) {
	payload := reconcileBatchPayload{Items: make([]reconcileBatchItem, 0, len(items))}
	for _, item := range items {
		input, err := parseReconcileInput(item.RawJSON)
		if err != nil {
			return dto.CompletionRequest{}, fmt.Errorf("parse reconcile raw record for trace_id %q: %w", item.TraceID, err)
		}
		prompt, response := adjudicatePromptResponse(input.Messages)
		payload.Items = append(payload.Items, reconcileBatchItem{
			TraceID:             input.ID,
			Scene:               reconcileInputScene(cfg.Scene, prompt, response),
			Prompt:              prompt,
			Response:            response,
			OriginalLabel:       mappedReconcileLabel(input),
			OriginalExplanation: input.Meta.SourceFields.Reason,
		})
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return dto.CompletionRequest{}, fmt.Errorf("encode reconcile batch model input: %w", err)
	}
	return dto.CompletionRequest{
		Messages: []dto.Message{
			{Role: "system", Content: string(cfg.SystemPrompt)},
			{Role: "user", Content: string(encoded)},
		},
		Schema: append(json.RawMessage(nil), cfg.Schema...),
		Mode:   cfg.Mode,
	}, nil
}

func reconcileBatchResults(raw []byte) (map[string][]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var envelope reconcileBatchEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode batch envelope: %w", err)
	}
	if len(envelope.Results) == 0 {
		return nil, fmt.Errorf("batch envelope has no results")
	}
	results := make(map[string][]byte, len(envelope.Results))
	for _, rawResult := range envelope.Results {
		traceID, annotation, err := reconcileBatchAnnotation(rawResult)
		if err != nil {
			return nil, err
		}
		results[traceID] = annotation
	}
	return results, nil
}

func reconcileBatchAnnotation(raw []byte) (string, []byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return "", nil, fmt.Errorf("decode batch result: %w", err)
	}
	var traceID string
	if err := json.Unmarshal(fields["trace_id"], &traceID); err != nil || traceID == "" {
		return "", nil, fmt.Errorf("batch result trace_id is missing")
	}
	delete(fields, "trace_id")
	annotation, err := json.Marshal(fields)
	if err != nil {
		return "", nil, fmt.Errorf("encode batch annotation for trace_id %q: %w", traceID, err)
	}
	return traceID, annotation, nil
}

func (r reconcileBatchRunner) handleBatchCallFailure(
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
		itemAttempt.RequestNumber = item.RequestAttempts + 1
		if isTaskLevelFailure(callErr) {
			if err := r.store.RecordAttempt(ctx, r.cfg.TaskID, item.TraceID, itemAttempt); err != nil {
				return err
			}
			continue
		}
		if err := r.finishMissingOrInvalid(ctx, item, itemAttempt, decision.Category, "provider request failed"); err != nil {
			return err
		}
	}
	if isTaskLevelFailure(callErr) {
		return fmt.Errorf("task-level model failure: %w", callErr)
	}
	return nil
}

func (r reconcileBatchRunner) finishInvalidBatchResult(
	ctx context.Context,
	item dao.Item,
	attempt dao.Attempt,
	invalidResponse []byte,
	validationErr error,
) error {
	if item.RepairAttempts < r.cfg.FormatRepairAttempts {
		if err := r.store.RecordAttempt(ctx, r.cfg.TaskID, item.TraceID, attempt); err != nil {
			return err
		}
		runner, err := NewRunner(reconcileRunnerConfig(r.cfg, r.store))
		if err != nil {
			return err
		}
		return runner.repair(ctx, item, invalidResponse, validationErr, attempt.RequestNumber)
	}
	return r.finishMissingOrInvalid(ctx, item, attempt, "invalid_result", "model result validation failed")
}

func (r reconcileBatchRunner) finishMissingOrInvalid(
	ctx context.Context,
	item dao.Item,
	attempt dao.Attempt,
	category string,
	summary string,
) error {
	attempt.ErrorCategory = category
	if attempt.RequestNumber < adjudicateMaxAttempts(r.cfg.MaxAttempts) {
		attempt.Retryable = true
		delay := r.cfg.RetryPolicy.Delay(attempt.RequestNumber, 0, fullJitter)
		return r.store.ScheduleRetry(ctx, r.cfg.TaskID, item.TraceID, attempt, time.Now().Add(delay), category, summary)
	}
	attempt.Retryable = false
	return r.store.MarkFailed(ctx, r.cfg.TaskID, item.TraceID, attempt, category, summary)
}

func (r reconcileBatchRunner) splitOrFailLargeBatch(
	ctx context.Context,
	items []dao.Item,
	category string,
	summary string,
) error {
	if len(items) == 1 {
		attempt := dao.Attempt{
			Phase:         reconcileBatchPhase,
			RequestNumber: items[0].RequestAttempts + 1,
			StartedAt:     time.Now(),
			FinishedAt:    time.Now(),
			ErrorCategory: category,
		}
		return r.store.MarkFailed(ctx, r.cfg.TaskID, items[0].TraceID, attempt, category, summary)
	}
	mid := len(items) / 2
	if err := r.processBatch(ctx, items[:mid]); err != nil {
		return err
	}
	return r.processBatch(ctx, items[mid:])
}

func (r reconcileBatchRunner) waitForRetry(ctx context.Context) error {
	next, ok, err := r.store.NextRetryAt(ctx, r.cfg.TaskID)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("reconcile batch has unfinished items without retry schedule")
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

func (r reconcileBatchRunner) reportProgress(ctx context.Context, tracker progressTracker) {
	counts, err := r.store.Counts(ctx, r.cfg.TaskID)
	if err != nil {
		return
	}
	r.cfg.OnProgress(tracker.summary(time.Now(), counts))
}
