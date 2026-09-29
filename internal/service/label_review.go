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
	"sync"
	"time"

	"sendllm/internal/dao"
	"sendllm/internal/dto"
	"sendllm/internal/lib/limiter"
	"sendllm/internal/lib/tokenizer"
)

const labelReviewBatchPhase = "label_review_batch"

// LabelReviewConfig 指定标准标签复核流水线所需依赖。
type LabelReviewConfig struct {
	TaskID              string
	InputPath           string
	OutputPath          string
	StatePath           string
	SemanticHash        string
	SystemPrompt        []byte
	Schema              json.RawMessage
	Mode                string
	Completer           Completer
	Validator           *Validator
	Limiter             *limiter.Limiter
	MaxTokens           int
	MaxAttempts         int
	RetryPolicy         RetryPolicy
	Shutdown            time.Duration
	BatchSize           int
	BatchMaxInputTokens int
	OnProgress          func(Summary)
}

type labelReviewInput struct {
	TraceID      string             `json:"trace_id"`
	Source       string             `json:"source"`
	Split        string             `json:"split"`
	Language     string             `json:"language"`
	Scene        string             `json:"scene"`
	Label        string             `json:"label"`
	Prompt       string             `json:"prompt"`
	Response     string             `json:"response"`
	Explanation  string             `json:"explanation"`
	ExtendedInfo dto.ExtendedInfo   `json:"extended_info"`
	Annotation   dto.AnnotationMeta `json:"annotation"`
	Raw          json.RawMessage    `json:"-"`
}

type labelReviewOriginalLabel struct {
	Label        string `json:"label"`
	IsAttack     bool   `json:"is_attack"`
	RiskLevel    string `json:"risk_level"`
	CaseType     string `json:"case_type"`
	AttackMethod string `json:"attack_method"`
	AttackDomain string `json:"attack_domain"`
}

type labelReviewBatchItem struct {
	TraceID             string                   `json:"trace_id"`
	Scene               string                   `json:"scene"`
	Prompt              string                   `json:"prompt"`
	Response            string                   `json:"response"`
	OriginalLabel       labelReviewOriginalLabel `json:"original_label"`
	OriginalExplanation string                   `json:"original_explanation"`
}

type labelReviewBatchPayload struct {
	Items []labelReviewBatchItem `json:"items"`
}

type labelReviewBatchRunner struct {
	cfg   LabelReviewConfig
	store *dao.Store
}

// LabelReviewBatch 复核已统一为最终标签字段的 JSONL 输入。
func LabelReviewBatch(ctx context.Context, cfg LabelReviewConfig) (ExportStats, error) {
	if cfg.BatchSize < 1 {
		return ExportStats{}, fmt.Errorf("label review batch size is invalid")
	}
	if cfg.OnProgress == nil {
		cfg.OnProgress = func(Summary) {}
	}
	stats, err := labelReviewPrepareAndRun(ctx, cfg, func(store *dao.Store) error {
		runner := labelReviewBatchRunner{cfg: cfg, store: store}
		return runner.Run(ctx)
	})
	if err != nil {
		return stats, err
	}
	return stats, nil
}

// ImportLabelReview 将标准标签复核输入原子导入 SQLite 状态库。
func ImportLabelReview(ctx context.Context, store *dao.Store, taskID string, reader io.Reader) (ImportStats, error) {
	imp, err := store.BeginImport(ctx, taskID)
	if err != nil {
		return ImportStats{}, fmt.Errorf("begin label review import: %w", err)
	}
	defer func() { _ = imp.Rollback() }()

	var stats ImportStats
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), maxJSONLLineSize+1)
	var inputIndex int64
	for scanner.Scan() {
		inputIndex++
		raw := append([]byte(nil), scanner.Bytes()...)
		if len(raw) > maxJSONLLineSize {
			return ImportStats{}, fmt.Errorf("read label review line %d: JSONL line exceeds %d bytes", inputIndex, maxJSONLLineSize)
		}
		record, err := parseLabelReviewInput(raw)
		if err != nil {
			return ImportStats{}, fmt.Errorf("parse label review line %d: %w", inputIndex, err)
		}
		disposition, err := imp.Add(ctx, dao.Item{
			TaskID:     taskID,
			TraceID:    record.TraceID,
			InputIndex: inputIndex,
			RawJSON:    record.Raw,
			Prompt:     record.Prompt,
			Response:   record.Response,
			State:      dao.ItemPending,
		})
		if err != nil {
			return ImportStats{}, fmt.Errorf("import label review line %d: %w", inputIndex, err)
		}
		if disposition == dao.ImportAdded {
			stats.Added++
			continue
		}
		stats.Skipped++
	}
	if err := scanner.Err(); err != nil {
		return ImportStats{}, fmt.Errorf("read label review line %d: %w", inputIndex+1, err)
	}
	if err := imp.Commit(); err != nil {
		return ImportStats{}, fmt.Errorf("commit label review import: %w", err)
	}
	return stats, nil
}

// labelReviewPrepareAndRun 完成状态库准备、输入导入、失败重置和终态导出。
func labelReviewPrepareAndRun(
	ctx context.Context,
	cfg LabelReviewConfig,
	run func(*dao.Store) error,
) (ExportStats, error) {
	if cfg.TaskID == "" || cfg.InputPath == "" || cfg.OutputPath == "" || cfg.StatePath == "" {
		return ExportStats{}, fmt.Errorf("label review configuration is incomplete")
	}
	if len(cfg.SystemPrompt) == 0 || len(cfg.Schema) == 0 || cfg.SemanticHash == "" {
		return ExportStats{}, fmt.Errorf("label review configuration is incomplete")
	}
	if cfg.Completer == nil || cfg.Validator == nil || cfg.Limiter == nil {
		return ExportStats{}, fmt.Errorf("label review dependencies are incomplete")
	}
	if cfg.Shutdown <= 0 {
		return ExportStats{}, fmt.Errorf("label review shutdown timeout is invalid")
	}

	store, err := dao.Open(ctx, cfg.StatePath)
	if err != nil {
		return ExportStats{}, fmt.Errorf("open label review state: %w", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.EnsureTask(ctx, dao.Task{ID: cfg.TaskID, SemanticHash: cfg.SemanticHash}); err != nil {
		return ExportStats{}, fmt.Errorf("ensure label review task: %w", err)
	}

	input, err := os.Open(cfg.InputPath)
	if err != nil {
		return ExportStats{}, fmt.Errorf("open label review input: %w", err)
	}
	stats, importErr := ImportLabelReview(ctx, store, cfg.TaskID, input)
	closeErr := input.Close()
	if importErr != nil {
		return ExportStats{}, importErr
	}
	if closeErr != nil {
		return ExportStats{}, fmt.Errorf("close label review input: %w", closeErr)
	}
	if stats.Added > 0 || stats.Skipped > 0 {
		// 导入统计只确认输入已进入 SQLite，后续进度以状态库为准。
	}
	if _, err := store.ResetFailed(ctx, cfg.TaskID); err != nil {
		return ExportStats{}, fmt.Errorf("reset label review failed rows: %w", err)
	}

	if err := run(store); err != nil {
		exportCtx, cancelExport := adjudicateExportContext(ctx, cfg.Shutdown)
		exportStats, exportErr := ExportLabelReview(exportCtx, store, cfg.TaskID, cfg.OutputPath)
		cancelExport()
		if exportErr != nil {
			return ExportStats{}, fmt.Errorf("label review run failed: %w; export failed: %v", err, exportErr)
		}
		return exportStats, err
	}
	return ExportLabelReview(ctx, store, cfg.TaskID, cfg.OutputPath)
}

// ExportLabelReview 从 SQLite 终态导出标准标签复核结果。
func ExportLabelReview(ctx context.Context, store *dao.Store, taskID string, outputPath string) (ExportStats, error) {
	var stats ExportStats
	successTemp, err := stageJSONL(outputPath, func(encoder *json.Encoder) error {
		return store.ForEachSucceeded(ctx, taskID, func(record dao.ExportRecord) error {
			input, annotation, err := decodeLabelReviewExport(record)
			if err != nil {
				return err
			}
			if err := encoder.Encode(buildLabelReviewOutput(input, annotation)); err != nil {
				return fmt.Errorf("encode label review succeeded record: %w", err)
			}
			stats.Succeeded++
			return nil
		})
	})
	if err != nil {
		return ExportStats{}, fmt.Errorf("stage label review succeeded export: %w", err)
	}
	defer func() { _ = os.Remove(successTemp) }()

	failedPath := exportFailedPath(outputPath)
	failedTemp, err := stageJSONL(failedPath, func(encoder *json.Encoder) error {
		return store.ForEachFailed(ctx, taskID, func(record dao.FailedRecord) error {
			if err := encoder.Encode(map[string]any{
				"trace_id":       record.TraceID,
				"error_category": record.ErrorCategory,
				"error_summary":  record.ErrorSummary,
				"attempts":       record.Attempts,
			}); err != nil {
				return fmt.Errorf("encode label review failed record: %w", err)
			}
			stats.Failed++
			return nil
		})
	})
	if err != nil {
		return ExportStats{}, fmt.Errorf("stage label review failed export: %w", err)
	}
	defer func() { _ = os.Remove(failedTemp) }()

	if err := publishExportFiles(successTemp, outputPath, failedTemp, failedPath, newExportFileOps()); err != nil {
		return ExportStats{}, err
	}
	return stats, nil
}

// Run 批量领取标准标签复核样本并驱动 Worker 直到终态。
func (r labelReviewBatchRunner) Run(ctx context.Context) error {
	if _, err := r.store.ResetProcessing(ctx, r.cfg.TaskID); err != nil {
		return fmt.Errorf("reset interrupted label review task: %w", err)
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

// worker 顺序处理分配到当前 Worker 的批次，并把批次错误交还调度器。
func (r labelReviewBatchRunner) worker(ctx context.Context, jobs <-chan []dao.Item, results chan<- workerResult) {
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

// processBatch 构造一次批量请求，并把每条返回结果独立写入 SQLite 状态。
func (r labelReviewBatchRunner) processBatch(ctx context.Context, items []dao.Item) error {
	request, err := labelReviewBatchRequest(items, r.cfg)
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
		return fmt.Errorf("acquire label review batch request permit: %w", err)
	}
	response, err := r.cfg.Completer.Complete(ctx, request)
	release()
	finishedAt := time.Now()
	attempt := dao.Attempt{
		Phase:        labelReviewBatchPhase,
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

	results, parseErr := labelReviewBatchResults(response.Content)
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
			if err := r.finishMissingOrInvalid(ctx, item, itemAttempt, "invalid_result", "model result validation failed"); err != nil {
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

// parseLabelReviewInput 解析统一入口格式，并校验模型复核必需字段。
func parseLabelReviewInput(raw []byte) (labelReviewInput, error) {
	var record labelReviewInput
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&record); err != nil {
		return labelReviewInput{}, fmt.Errorf("decode JSON: %w", err)
	}
	if record.TraceID == "" || record.Source == "" || record.Scene == "" || record.Label == "" {
		return labelReviewInput{}, fmt.Errorf("required label review fields are missing")
	}
	if record.Prompt == "" && record.Response == "" {
		return labelReviewInput{}, fmt.Errorf("prompt and response are both empty")
	}
	if record.Scene != "prompt" && record.Scene != "response" && record.Scene != "pair" {
		return labelReviewInput{}, fmt.Errorf("scene is invalid")
	}
	if record.Label != "safe" && record.Label != "unsafe" {
		return labelReviewInput{}, fmt.Errorf("label is invalid")
	}
	record.Raw = append(json.RawMessage(nil), raw...)
	return record, nil
}

// labelReviewBatchRequest 构造不依赖数据集原始结构的批量模型请求。
func labelReviewBatchRequest(items []dao.Item, cfg LabelReviewConfig) (dto.CompletionRequest, error) {
	payload := labelReviewBatchPayload{Items: make([]labelReviewBatchItem, 0, len(items))}
	for _, item := range items {
		input, err := parseLabelReviewInput(item.RawJSON)
		if err != nil {
			return dto.CompletionRequest{}, fmt.Errorf("parse label review raw record for trace_id %q: %w", item.TraceID, err)
		}
		payload.Items = append(payload.Items, labelReviewBatchItem{
			TraceID:             input.TraceID,
			Scene:               input.Scene,
			Prompt:              input.Prompt,
			Response:            input.Response,
			OriginalLabel:       labelReviewOriginal(input),
			OriginalExplanation: input.Explanation,
		})
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return dto.CompletionRequest{}, fmt.Errorf("encode label review batch model input: %w", err)
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

// labelReviewBatchResults 复用 results[] 批量响应协议解析。
func labelReviewBatchResults(raw []byte) (map[string][]byte, error) {
	return reconcileBatchResults(raw)
}

// handleBatchCallFailure 将一次批量调用失败按条目记录为重试或失败。
func (r labelReviewBatchRunner) handleBatchCallFailure(
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

// finishMissingOrInvalid 根据尝试次数把缺失或非法结果调度重试或标记失败。
func (r labelReviewBatchRunner) finishMissingOrInvalid(
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

// splitOrFailLargeBatch 在上下文过大时递归拆分批次，单条仍过大则失败。
func (r labelReviewBatchRunner) splitOrFailLargeBatch(
	ctx context.Context,
	items []dao.Item,
	category string,
	summary string,
) error {
	if len(items) == 1 {
		attempt := dao.Attempt{
			Phase:         labelReviewBatchPhase,
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

// waitForRetry 等待下一条 retry_wait 样本到达可领取时间。
func (r labelReviewBatchRunner) waitForRetry(ctx context.Context) error {
	next, ok, err := r.store.NextRetryAt(ctx, r.cfg.TaskID)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("label review batch has unfinished items without retry schedule")
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

// reportProgress 读取当前状态计数并发送安全进度摘要。
func (r labelReviewBatchRunner) reportProgress(ctx context.Context, tracker progressTracker) {
	counts, err := r.store.Counts(ctx, r.cfg.TaskID)
	if err != nil {
		return
	}
	r.cfg.OnProgress(tracker.summary(time.Now(), counts))
}

// decodeLabelReviewExport 解析原始输入和已校验标注，用于终态导出。
func decodeLabelReviewExport(record dao.ExportRecord) (labelReviewInput, dto.Annotation, error) {
	input, err := parseLabelReviewInput(record.RawJSON)
	if err != nil {
		return labelReviewInput{}, dto.Annotation{}, fmt.Errorf("decode label review source: %w", err)
	}
	var annotation dto.Annotation
	if err := json.Unmarshal(record.Annotation, &annotation); err != nil {
		return labelReviewInput{}, dto.Annotation{}, fmt.Errorf("decode label review annotation: %w", err)
	}
	return input, annotation, nil
}

// buildLabelReviewOutput 合并原始统一标签和模型复核结果，产出最终格式。
func buildLabelReviewOutput(record labelReviewInput, annotation dto.Annotation) masbOutput {
	original := labelReviewOriginal(record)
	merged := mergeLabelReviewAnnotation(record, original, annotation)
	extended := dto.ExtendedInfo{
		CaseType:       merged.CaseType,
		IsAttack:       &merged.IsAttack,
		RiskLevel:      merged.RiskLevel,
		AttackMethod:   merged.AttackMethod,
		AttackDomain:   merged.AttackDomain,
		AttackScenario: merged.AttackScenario,
	}
	return masbOutput{
		TraceID:      record.TraceID,
		Source:       record.Source,
		Split:        adjudicateValueOrDefault(record.Split, "train"),
		Language:     adjudicateValueOrDefault(record.Language, "zh"),
		Scene:        record.Scene,
		Label:        annotationLabel(merged.IsAttack),
		Prompt:       record.Prompt,
		Response:     record.Response,
		Explanation:  merged.Explanation,
		ExtendedInfo: extended,
		Annotation: dto.AnnotationMeta{
			Method:       "auto",
			QualityScore: merged.QualityScore,
		},
	}
}

// labelReviewOriginal 从统一输入中抽取已经映射好的原始标签。
func labelReviewOriginal(record labelReviewInput) labelReviewOriginalLabel {
	isAttack := record.Label == "unsafe"
	if record.ExtendedInfo.IsAttack != nil {
		isAttack = *record.ExtendedInfo.IsAttack
	}
	return labelReviewOriginalLabel{
		Label:        record.Label,
		IsAttack:     isAttack,
		RiskLevel:    record.ExtendedInfo.RiskLevel,
		CaseType:     record.ExtendedInfo.CaseType,
		AttackMethod: record.ExtendedInfo.AttackMethod,
		AttackDomain: record.ExtendedInfo.AttackDomain,
	}
}

// mergeLabelReviewAnnotation 在模型与原始标签一致时保留原始字段和理由。
func mergeLabelReviewAnnotation(
	record labelReviewInput,
	original labelReviewOriginalLabel,
	annotation dto.Annotation,
) reconcileMergedAnnotation {
	model := modelReconcileLabel(annotation)
	riskLabelsMatch := labelReviewRiskLabelsMatch(original, model)
	allFieldsAgree := original.IsAttack == model.IsAttack &&
		original.CaseType == model.CaseType &&
		original.RiskLevel == model.RiskLevel &&
		riskLabelsMatch

	merged := reconcileMergedAnnotation{
		IsAttack:       model.IsAttack,
		CaseType:       model.CaseType,
		RiskLevel:      model.RiskLevel,
		Explanation:    model.Explanation,
		AttackMethod:   model.AttackMethod,
		AttackDomain:   model.AttackDomain,
		AttackScenario: model.AttackScenario,
		QualityScore:   annotation.QualityScore,
	}
	if original.IsAttack == model.IsAttack {
		merged.IsAttack = original.IsAttack
	}
	if original.CaseType == model.CaseType {
		merged.CaseType = original.CaseType
	}
	if original.RiskLevel == model.RiskLevel {
		merged.RiskLevel = original.RiskLevel
	}
	if riskLabelsMatch {
		if original.AttackMethod != "" && merged.AttackMethod == "" {
			merged.AttackMethod = original.AttackMethod
		}
		if original.AttackDomain != "" && merged.AttackDomain == "" {
			merged.AttackDomain = original.AttackDomain
		}
	}
	if allFieldsAgree && record.Explanation != "" {
		merged.Explanation = record.Explanation
	}
	return merged
}

// labelReviewRiskLabelsMatch 判断原始风险字段是否被模型对应字段确认。
func labelReviewRiskLabelsMatch(original labelReviewOriginalLabel, model reconcileModelLabel) bool {
	if original.AttackMethod != "" && original.AttackMethod != model.AttackMethod {
		return false
	}
	if original.AttackDomain != "" && original.AttackDomain != model.AttackDomain {
		return false
	}
	if original.AttackMethod == "" && original.AttackDomain == "" {
		return model.AttackMethod == "" && model.AttackDomain == ""
	}
	return true
}
