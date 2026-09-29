package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"sendllm/internal/dao"
	"sendllm/internal/dto"
	"sendllm/internal/lib/limiter"
	"sendllm/internal/lib/tokenizer"
)

const repairSystemPrompt = "仅根据给定 Schema 修复 JSON"

// RunnerConfig 指定单任务执行所需的依赖和边界。
type RunnerConfig struct {
	TaskID               string
	SystemPrompt         []byte
	Scene                string
	Schema               json.RawMessage
	Mode                 string
	MaxOutputTokens      int
	RequestMaxAttempts   int
	FormatRepairAttempts int
	ShutdownTimeout      time.Duration
	Store                *dao.Store
	Completer            Completer
	Validator            *Validator
	Limiter              *limiter.Limiter
	RetryPolicy          RetryPolicy
	Jitter               func(time.Duration) time.Duration
	OnProgress           func(Summary)
	BuildRequest         func(dao.Item) (dto.CompletionRequest, error)
}

// Runner 协调可恢复领取、模型调用和持久化状态迁移。
type Runner struct {
	cfg   RunnerConfig
	store runnerStore
}

type runnerStore interface {
	ResetProcessing(context.Context, string) (int64, error)
	Counts(context.Context, string) (dao.Counts, error)
	Claim(context.Context, string, int, time.Time) ([]dao.Item, error)
	RecordAttempt(context.Context, string, string, dao.Attempt) error
	ScheduleRetry(context.Context, string, string, dao.Attempt, time.Time, string, string) error
	MarkFailed(context.Context, string, string, dao.Attempt, string, string) error
	MarkSucceeded(context.Context, string, string, dao.Attempt, []byte) error
	NextRetryAt(context.Context, string) (time.Time, bool, error)
}

var _ runnerStore = (*dao.Store)(nil)

type workerResult struct {
	err error
}

type repairPayload struct {
	InvalidResponse  string          `json:"invalid_response"`
	ValidationErrors []string        `json:"validation_errors"`
	Schema           json.RawMessage `json:"schema"`
}

// NewRunner 校验依赖并复制会跨调用保留的数据。
func NewRunner(cfg RunnerConfig) (*Runner, error) {
	if cfg.TaskID == "" || len(cfg.SystemPrompt) == 0 || len(cfg.Schema) == 0 {
		return nil, errors.New("runner requires task ID, system prompt, and schema")
	}
	if cfg.Store == nil || cfg.Completer == nil || cfg.Validator == nil || cfg.Limiter == nil {
		return nil, errors.New("runner requires store, completer, validator, and limiter")
	}
	switch cfg.Mode {
	case "json_schema", "json_object", "prompt_only":
	default:
		return nil, errors.New("runner completion mode is invalid")
	}
	if cfg.MaxOutputTokens < 1 || cfg.RequestMaxAttempts < 1 || cfg.FormatRepairAttempts < 0 || cfg.ShutdownTimeout <= 0 {
		return nil, errors.New("runner attempt and token limits are invalid")
	}
	if cfg.Jitter == nil {
		cfg.Jitter = fullJitter
	}
	if cfg.OnProgress == nil {
		cfg.OnProgress = func(Summary) {}
	}
	cfg.SystemPrompt = append([]byte(nil), cfg.SystemPrompt...)
	cfg.Schema = append(json.RawMessage(nil), cfg.Schema...)
	if cfg.BuildRequest == nil {
		cfg.BuildRequest = defaultClassificationRequest(cfg.SystemPrompt, cfg.Scene, cfg.Schema, cfg.Mode)
	}
	return &Runner{cfg: cfg, store: cfg.Store}, nil
}

// Run 恢复遗留状态并运行到全部记录终态、取消或任务级错误。
func (r *Runner) Run(ctx context.Context) (Summary, error) {
	if _, err := r.store.ResetProcessing(ctx, r.cfg.TaskID); err != nil {
		return Summary{}, fmt.Errorf("reset interrupted task: %w", err)
	}
	initialCounts, err := r.store.Counts(ctx, r.cfg.TaskID)
	if err != nil {
		return Summary{}, err
	}
	tracker := newProgressTracker(time.Now(), initialCounts)
	claimCtx, cancelClaim := context.WithCancelCause(ctx)
	workCtx, cancelWork := context.WithCancelCause(context.WithoutCancel(ctx))
	cancelTask := func(cause error) {
		cancelClaim(cause)
		cancelWork(cause)
	}
	jobs := make(chan dao.Item)
	results := make(chan workerResult)
	var workers sync.WaitGroup
	workerCount := r.cfg.Limiter.Concurrency()
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			r.worker(workCtx, cancelTask, jobs, results)
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
	abortWorkers := func(cause error) {
		cancelTask(cause)
		closeJobs()
		workers.Wait()
	}
	finishWorkers := func() {
		closeJobs()
		workers.Wait()
		cancelWork(nil)
	}
	drainWorkers := func(cause error) error {
		closeJobs()
		timer := time.NewTimer(r.cfg.ShutdownTimeout)
		defer timer.Stop()
		for active > 0 {
			select {
			case <-workCtx.Done():
				workers.Wait()
				return context.Cause(workCtx)
			case <-timer.C:
				cancelWork(cause)
				workers.Wait()
				return cause
			case <-results:
				active--
			}
		}
		finishWorkers()
		return cause
	}
	defer cancelWork(nil)
	defer cancelClaim(nil)

	for {
		if cause := context.Cause(ctx); cause != nil {
			return Summary{}, drainWorkers(cause)
		}
		if cause := context.Cause(workCtx); cause != nil {
			abortWorkers(cause)
			return Summary{}, cause
		}

		capacity := workerCount - active
		if capacity > 0 {
			claimed, err := r.claim(claimCtx, capacity, time.Now())
			if err != nil {
				if cause := context.Cause(ctx); cause != nil {
					return Summary{}, drainWorkers(cause)
				}
				claimErr := runContextError(workCtx, err)
				abortWorkers(claimErr)
				return Summary{}, claimErr
			}
			for _, item := range claimed {
				if cause := context.Cause(ctx); cause != nil {
					return Summary{}, drainWorkers(cause)
				}
				sent := false
				for !sent {
					select {
					case jobs <- item:
						active++
						sent = true
					case result := <-results:
						active--
						if result.err != nil {
							resultErr := runContextError(workCtx, result.err)
							abortWorkers(resultErr)
							return Summary{}, resultErr
						}
						if err := r.reportProgress(claimCtx, tracker); err != nil {
							if cause := context.Cause(ctx); cause != nil {
								return Summary{}, drainWorkers(cause)
							}
							abortWorkers(err)
							return Summary{}, err
						}
					case <-ctx.Done():
						return Summary{}, drainWorkers(context.Cause(ctx))
					case <-workCtx.Done():
						cause := context.Cause(workCtx)
						abortWorkers(cause)
						return Summary{}, cause
					}
				}
			}
			if len(claimed) > 0 {
				continue
			}
		}

		if active > 0 {
			select {
			case result := <-results:
				active--
				if result.err != nil {
					resultErr := runContextError(workCtx, result.err)
					abortWorkers(resultErr)
					return Summary{}, resultErr
				}
				if err := r.reportProgress(claimCtx, tracker); err != nil {
					if cause := context.Cause(ctx); cause != nil {
						return Summary{}, drainWorkers(cause)
					}
					abortWorkers(err)
					return Summary{}, err
				}
			case <-ctx.Done():
				return Summary{}, drainWorkers(context.Cause(ctx))
			case <-workCtx.Done():
				cause := context.Cause(workCtx)
				abortWorkers(cause)
				return Summary{}, cause
			}
			continue
		}

		counts, err := r.store.Counts(claimCtx, r.cfg.TaskID)
		if err != nil {
			if cause := context.Cause(ctx); cause != nil {
				return Summary{}, drainWorkers(cause)
			}
			countsErr := runContextError(workCtx, err)
			abortWorkers(countsErr)
			return Summary{}, countsErr
		}
		if counts.Pending == 0 && counts.Processing == 0 && counts.RetryWait == 0 {
			finishWorkers()
			summary := tracker.summary(time.Now(), counts)
			r.cfg.OnProgress(summary)
			return summary, nil
		}
		if err := r.waitForRetry(claimCtx); err != nil {
			if cause := context.Cause(ctx); cause != nil {
				return Summary{}, drainWorkers(cause)
			}
			retryErr := runContextError(workCtx, err)
			abortWorkers(retryErr)
			return Summary{}, retryErr
		}
	}
}

func (r *Runner) claim(ctx context.Context, limit int, now time.Time) ([]dao.Item, error) {
	items, err := r.store.Claim(ctx, r.cfg.TaskID, limit, now)
	if err != nil {
		return nil, runContextError(ctx, err)
	}
	return items, nil
}

func (r *Runner) worker(
	ctx context.Context,
	cancel context.CancelCauseFunc,
	jobs <-chan dao.Item,
	results chan<- workerResult,
) {
	for {
		select {
		case <-ctx.Done():
			return
		case item, ok := <-jobs:
			if !ok {
				return
			}
			if ctx.Err() != nil {
				return
			}
			err := r.processItem(ctx, item)
			if isTaskLevelRunError(err) {
				cancel(err)
				return
			}
			select {
			case results <- workerResult{err: err}:
			case <-ctx.Done():
				return
			}
		}
	}
}

func (r *Runner) processItem(ctx context.Context, item dao.Item) error {
	requestNumber := item.RequestAttempts + 1
	request, err := r.cfg.BuildRequest(item)
	if err != nil {
		return err
	}
	response, attempt, err := r.complete(ctx, request, "classification", requestNumber, item.RepairAttempts)
	if err != nil {
		return r.handleCallFailure(ctx, item, attempt, requestNumber, err)
	}
	annotation, validationErr := r.cfg.Validator.Validate(response.Content)
	if validationErr == nil {
		return r.markSucceeded(ctx, item, attempt, annotation)
	}
	setValidationFailure(&attempt, validationErr)
	if item.RepairAttempts >= r.cfg.FormatRepairAttempts {
		return r.finishInvalidResult(ctx, item, attempt, requestNumber)
	}
	if err := r.store.RecordAttempt(ctx, r.cfg.TaskID, item.TraceID, attempt); err != nil {
		return err
	}
	return r.repair(ctx, item, response.Content, validationErr, requestNumber)
}

func (r *Runner) repair(
	ctx context.Context,
	item dao.Item,
	invalidResponse []byte,
	validationErr error,
	requestNumber int,
) error {
	for repairNumber := item.RepairAttempts + 1; repairNumber <= r.cfg.FormatRepairAttempts; repairNumber++ {
		request, err := r.repairRequest(invalidResponse, validationProblems(validationErr))
		if err != nil {
			return err
		}
		response, attempt, err := r.complete(ctx, request, "repair", requestNumber, repairNumber)
		if err != nil {
			return r.handleCallFailure(ctx, item, attempt, requestNumber, err)
		}
		annotation, nextValidationErr := r.cfg.Validator.Validate(response.Content)
		if nextValidationErr == nil {
			return r.markSucceeded(ctx, item, attempt, annotation)
		}
		setValidationFailure(&attempt, nextValidationErr)
		if repairNumber == r.cfg.FormatRepairAttempts {
			return r.finishInvalidResult(ctx, item, attempt, requestNumber)
		}
		if err := r.store.RecordAttempt(ctx, r.cfg.TaskID, item.TraceID, attempt); err != nil {
			return err
		}
		invalidResponse = response.Content
		validationErr = nextValidationErr
	}
	return nil
}

func (r *Runner) handleCallFailure(
	ctx context.Context,
	item dao.Item,
	attempt dao.Attempt,
	requestNumber int,
	callErr error,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if errors.Is(callErr, limiter.ErrTokenBudgetExceeded) {
		return fmt.Errorf("task token budget cannot admit request: %w", callErr)
	}
	decision := ClassifyFailure(callErr)
	attempt.ErrorCategory = decision.Category
	attempt.Retryable = decision.Retry
	if isTaskLevelFailure(callErr) {
		if err := r.store.RecordAttempt(ctx, r.cfg.TaskID, item.TraceID, attempt); err != nil {
			return err
		}
		return fmt.Errorf("task-level model failure: %w", callErr)
	}
	var delay time.Duration
	if decision.Retry {
		delay = r.cfg.RetryPolicy.Delay(requestNumber, decision.RetryAfter, r.cfg.Jitter)
	}
	if decision.GlobalCooldown {
		r.cfg.Limiter.Cooldown(time.Now().Add(delay))
	}
	if decision.Retry && requestNumber < r.cfg.RequestMaxAttempts {
		return r.store.ScheduleRetry(
			ctx,
			r.cfg.TaskID,
			item.TraceID,
			attempt,
			time.Now().Add(delay),
			decision.Category,
			"provider request failed",
		)
	}
	return r.store.MarkFailed(
		ctx,
		r.cfg.TaskID,
		item.TraceID,
		attempt,
		decision.Category,
		"provider request failed",
	)
}

func (r *Runner) finishInvalidResult(
	ctx context.Context,
	item dao.Item,
	attempt dao.Attempt,
	requestNumber int,
) error {
	if requestNumber < r.cfg.RequestMaxAttempts {
		delay := r.cfg.RetryPolicy.Delay(requestNumber, 0, r.cfg.Jitter)
		return r.store.ScheduleRetry(
			ctx,
			r.cfg.TaskID,
			item.TraceID,
			attempt,
			time.Now().Add(delay),
			"invalid_result",
			"model result validation failed",
		)
	}
	return r.store.MarkFailed(
		ctx,
		r.cfg.TaskID,
		item.TraceID,
		attempt,
		"invalid_result",
		"model result validation failed",
	)
}

func (r *Runner) complete(
	ctx context.Context,
	request dto.CompletionRequest,
	phase string,
	requestNumber int,
	repairNumber int,
) (dto.CompletionResponse, dao.Attempt, error) {
	startedAt := time.Now()
	attempt := dao.Attempt{
		Phase:         phase,
		RequestNumber: requestNumber,
		RepairNumber:  repairNumber,
		StartedAt:     startedAt,
	}
	release, err := r.cfg.Limiter.Acquire(ctx, tokenizer.Estimate(request.Messages, r.cfg.MaxOutputTokens))
	if err != nil {
		attempt.FinishedAt = time.Now()
		return dto.CompletionResponse{}, attempt, fmt.Errorf("acquire model request permit: %w", err)
	}
	response, err := r.cfg.Completer.Complete(ctx, request)
	release()
	attempt.FinishedAt = time.Now()
	attempt.APIKeyEnv = response.APIKeyEnv
	attempt.RawResponse = append([]byte(nil), response.RawResponse...)
	attempt.InputTokens = response.Usage.PromptTokens
	attempt.OutputTokens = response.Usage.CompletionTokens
	var providerErr *dto.ProviderError
	if errors.As(err, &providerErr) {
		attempt.HTTPStatus = providerErr.StatusCode
	}
	return response, attempt, err
}

func defaultClassificationRequest(
	systemPrompt []byte,
	scene string,
	schema json.RawMessage,
	mode string,
) func(dao.Item) (dto.CompletionRequest, error) {
	systemPrompt = append([]byte(nil), systemPrompt...)
	schema = append(json.RawMessage(nil), schema...)
	return func(item dao.Item) (dto.CompletionRequest, error) {
		return classificationRequest(item, systemPrompt, scene, schema, mode)
	}
}

func classificationRequest(
	item dao.Item,
	systemPrompt []byte,
	scene string,
	schema json.RawMessage,
	mode string,
) (dto.CompletionRequest, error) {
	input := dto.SourceSample{
		TraceID:  item.TraceID,
		Prompt:   item.Prompt,
		Response: item.Response,
	}.ModelInput(scene)
	encoded, err := json.Marshal(input)
	if err != nil {
		return dto.CompletionRequest{}, fmt.Errorf("encode model input for trace_id %q: %w", item.TraceID, err)
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

func (r *Runner) repairRequest(invalidResponse []byte, problems []string) (dto.CompletionRequest, error) {
	payload := repairPayload{
		InvalidResponse:  string(invalidResponse),
		ValidationErrors: append([]string(nil), problems...),
		Schema:           append(json.RawMessage(nil), r.cfg.Schema...),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return dto.CompletionRequest{}, fmt.Errorf("encode format repair request: %w", err)
	}
	return dto.CompletionRequest{
		Messages: []dto.Message{
			{Role: "system", Content: repairSystemPrompt},
			{Role: "user", Content: string(encoded)},
		},
		Schema: append(json.RawMessage(nil), r.cfg.Schema...),
		Mode:   r.cfg.Mode,
	}, nil
}

func (r *Runner) markSucceeded(
	ctx context.Context,
	item dao.Item,
	attempt dao.Attempt,
	annotation dto.Annotation,
) error {
	encoded, err := json.Marshal(annotation)
	if err != nil {
		return fmt.Errorf("encode annotation for trace_id %q: %w", item.TraceID, err)
	}
	return r.store.MarkSucceeded(ctx, r.cfg.TaskID, item.TraceID, attempt, encoded)
}

func (r *Runner) reportProgress(ctx context.Context, tracker progressTracker) error {
	counts, err := r.store.Counts(ctx, r.cfg.TaskID)
	if err != nil {
		return runContextError(ctx, err)
	}
	r.cfg.OnProgress(tracker.summary(time.Now(), counts))
	return nil
}

func runContextError(ctx context.Context, err error) error {
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	return err
}

func (r *Runner) waitForRetry(ctx context.Context) error {
	next, ok, err := r.store.NextRetryAt(ctx, r.cfg.TaskID)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("runner has unfinished items without active workers or retry schedule")
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

func setValidationFailure(attempt *dao.Attempt, err error) {
	attempt.ErrorCategory = "invalid_result"
	attempt.Retryable = true
	attempt.ValidationErrors = validationProblems(err)
}

func validationProblems(err error) []string {
	var validationErr *ValidationError
	if errors.As(err, &validationErr) {
		return append([]string(nil), validationErr.Problems...)
	}
	return []string{"model result validation failed"}
}

func isTaskLevelFailure(err error) bool {
	var providerErr *dto.ProviderError
	if !errors.As(err, &providerErr) {
		return false
	}
	return providerErr.Kind == dto.ProviderAuthentication || providerErr.Kind == dto.ProviderBadRequest
}

func isTaskLevelRunError(err error) bool {
	return isTaskLevelFailure(err) || errors.Is(err, limiter.ErrTokenBudgetExceeded)
}

func fullJitter(limit time.Duration) time.Duration {
	if limit <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(int64(limit)))
}
