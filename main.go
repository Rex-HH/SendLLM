package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"sendllm/internal/dao"
	"sendllm/internal/facade"
	"sendllm/internal/lib/configs"
	"sendllm/internal/lib/limiter"
	"sendllm/internal/service"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	logger := slog.New(slog.NewTextHandler(stderr, nil))
	flags := flag.NewFlagSet("sendllm", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "任务配置文件")
	mode := flags.String("mode", "annotate", "任务模式")
	if err := flags.Parse(args); err != nil || *configPath == "" || flags.NArg() != 0 {
		return fail(ctx, logger, "", "arguments")
	}

	cfg, err := configs.Load(*configPath)
	if err != nil {
		return fail(ctx, logger, "", "configuration")
	}
	if ctx.Err() != nil {
		return fail(ctx, logger, cfg.Task.ID, "interrupted")
	}
	apiKey, err := cfg.APIKey()
	if err != nil {
		return fail(ctx, logger, cfg.Task.ID, "api_key")
	}
	semanticHash, err := cfg.SemanticFingerprint()
	if err != nil {
		return fail(ctx, logger, cfg.Task.ID, "configuration")
	}
	validator, err := service.NewValidator(
		cfg.ResultSchema,
		cfg.RiskTypes,
		cfg.Output.ExplanationMinLength,
		cfg.Output.ExplanationMaxLength,
	)
	if err != nil {
		return fail(ctx, logger, cfg.Task.ID, "validation")
	}
	completer, err := facade.NewOpenAI(facade.Config{
		BaseURL:        cfg.Model.BaseURL,
		APIKey:         apiKey,
		Model:          cfg.Model.Name,
		Temperature:    cfg.Model.Temperature,
		TopP:           cfg.Model.TopP,
		MaxTokens:      cfg.Model.MaxTokens,
		Seed:           cfg.Model.Seed,
		ExtraBody:      cfg.Model.ExtraBody,
		Timeout:        cfg.Model.Timeout,
		MaxConnections: cfg.Runtime.Concurrency,
	})
	if err != nil {
		return fail(ctx, logger, cfg.Task.ID, "model")
	}
	requestLimiter, err := limiter.New(limiter.Config{
		Concurrency:       cfg.Runtime.Concurrency,
		RequestsPerMinute: cfg.Runtime.RequestsPerMinute,
		TokensPerMinute:   cfg.Runtime.TokensPerMinute,
	})
	if err != nil {
		return fail(ctx, logger, cfg.Task.ID, "limiter")
	}
	if *mode == "adjudicate" {
		stats, err := service.Adjudicate(ctx, service.AdjudicateConfig{
			TaskID:       cfg.Task.ID,
			InputPath:    cfg.Task.Input,
			OutputPath:   cfg.Task.Output,
			StatePath:    cfg.Task.State,
			SemanticHash: semanticHash,
			SystemPrompt: cfg.SystemPrompt,
			Scene:        cfg.Prompt.Scene,
			Schema:       cfg.ResultSchema,
			Mode:         cfg.Model.StructuredOutput,
			Completer:    completer,
			Validator:    validator,
			Limiter:      requestLimiter,
			MaxTokens:    cfg.Model.MaxTokens,
			MaxAttempts:  cfg.Retry.RequestMaxAttempts,
			Shutdown:     cfg.Runtime.ShutdownTimeout,
			OnProgress:   progressLogger(ctx, cfg.Task.ID, logger),
			RetryPolicy: service.RetryPolicy{
				MaxAttempts:    cfg.Retry.RequestMaxAttempts,
				InitialBackoff: cfg.Retry.InitialBackoff,
				MaxBackoff:     cfg.Retry.MaxBackoff,
			},
		})
		if err != nil {
			return failWithError(ctx, logger, cfg.Task.ID, "adjudicate", err)
		}
		_, _ = fmt.Fprintf(stdout, "succeeded=%d failed=%d\n", stats.Succeeded, stats.Failed)
		if stats.Failed > 0 {
			return 2
		}
		return 0
	}
	if *mode != "annotate" {
		return fail(ctx, logger, cfg.Task.ID, "arguments")
	}
	store, err := dao.Open(ctx, cfg.Task.State)
	if err != nil {
		return fail(ctx, logger, cfg.Task.ID, "storage")
	}
	defer func() { _ = store.Close() }()
	runner, err := newTaskRunner(ctx, cfg, store, completer, validator, requestLimiter, logger)
	if err != nil {
		return fail(ctx, logger, cfg.Task.ID, "runner")
	}
	if err := store.EnsureTask(ctx, dao.Task{ID: cfg.Task.ID, SemanticHash: semanticHash}); err != nil {
		return fail(ctx, logger, cfg.Task.ID, "storage")
	}
	exportOnFailure := true
	defer func() {
		if !exportOnFailure {
			return
		}
		exportCtx, cancelExport := terminalExportContext(ctx, cfg.Runtime.ShutdownTimeout)
		defer cancelExport()
		if _, err := service.Export(exportCtx, store, cfg.Task.ID, cfg.Task.Output); err != nil {
			logger.ErrorContext(exportCtx, "task export failed", "task_id", cfg.Task.ID, "error_category", "export")
		}
	}()

	input, err := os.Open(cfg.Task.Input)
	if err != nil {
		return fail(ctx, logger, cfg.Task.ID, "input")
	}
	importStats, importErr := service.Import(ctx, store, cfg.Task.ID, input)
	closeErr := input.Close()
	if importErr != nil || closeErr != nil {
		return fail(ctx, logger, cfg.Task.ID, "import")
	}

	_, runErr := runner.Run(ctx)
	if runErr == nil {
		runErr = coverFailed(ctx, cfg, store, completer, validator, logger)
	}
	exportOnFailure = false
	exportCtx, cancelExport := terminalExportContext(ctx, cfg.Runtime.ShutdownTimeout)
	exportStats, exportErr := service.Export(exportCtx, store, cfg.Task.ID, cfg.Task.Output)
	cancelExport()
	if exportErr != nil {
		return fail(ctx, logger, cfg.Task.ID, "export")
	}
	if runErr != nil {
		return fail(ctx, logger, cfg.Task.ID, "runner")
	}

	_, _ = fmt.Fprintf(
		stdout,
		"added=%d skipped=%d succeeded=%d failed=%d\n",
		importStats.Added,
		importStats.Skipped,
		exportStats.Succeeded,
		exportStats.Failed,
	)
	if exportStats.Failed > 0 {
		return 2
	}
	return 0
}

// newTaskRunner 根据配置装配默认标注 Runner。
func newTaskRunner(
	ctx context.Context,
	cfg *configs.Config,
	store *dao.Store,
	completer service.Completer,
	validator *service.Validator,
	requestLimiter *limiter.Limiter,
	logger *slog.Logger,
) (*service.Runner, error) {
	return service.NewRunner(service.RunnerConfig{
		TaskID:               cfg.Task.ID,
		SystemPrompt:         cfg.SystemPrompt,
		Scene:                cfg.Prompt.Scene,
		Schema:               cfg.ResultSchema,
		Mode:                 cfg.Model.StructuredOutput,
		MaxOutputTokens:      cfg.Model.MaxTokens,
		RequestMaxAttempts:   cfg.Retry.RequestMaxAttempts,
		FormatRepairAttempts: cfg.Retry.FormatRepairAttempts,
		ShutdownTimeout:      cfg.Runtime.ShutdownTimeout,
		Store:                store,
		Completer:            completer,
		Validator:            validator,
		Limiter:              requestLimiter,
		RetryPolicy: service.RetryPolicy{
			MaxAttempts:    cfg.Retry.RequestMaxAttempts,
			InitialBackoff: cfg.Retry.InitialBackoff,
			MaxBackoff:     cfg.Retry.MaxBackoff,
		},
		OnProgress: progressLogger(ctx, cfg.Task.ID, logger),
	})
}

// coverFailed 对最终失败记录执行一次保守补跑。
func coverFailed(
	ctx context.Context,
	cfg *configs.Config,
	store *dao.Store,
	completer service.Completer,
	validator *service.Validator,
	logger *slog.Logger,
) error {
	counts, err := store.Counts(ctx, cfg.Task.ID)
	if err != nil {
		return err
	}
	if counts.Failed == 0 {
		return nil
	}
	reset, err := store.ResetFailed(ctx, cfg.Task.ID)
	if err != nil {
		return err
	}
	if reset == 0 {
		return nil
	}
	logger.InfoContext(ctx, "task cover retry", "task_id", cfg.Task.ID, "failed", reset)
	coverLimiter, err := limiter.New(limiter.Config{
		Concurrency:       coverConcurrency(cfg.Runtime),
		RequestsPerMinute: coverRequestsPerMinute(cfg.Runtime),
		TokensPerMinute:   cfg.Runtime.TokensPerMinute,
	})
	if err != nil {
		return err
	}
	runner, err := service.NewRunner(service.RunnerConfig{
		TaskID:               cfg.Task.ID,
		SystemPrompt:         cfg.SystemPrompt,
		Scene:                cfg.Prompt.Scene,
		Schema:               cfg.ResultSchema,
		Mode:                 cfg.Model.StructuredOutput,
		MaxOutputTokens:      cfg.Model.MaxTokens,
		RequestMaxAttempts:   coverRequestAttempts(cfg.Retry.RequestMaxAttempts),
		FormatRepairAttempts: cfg.Retry.FormatRepairAttempts,
		ShutdownTimeout:      cfg.Runtime.ShutdownTimeout,
		Store:                store,
		Completer:            completer,
		Validator:            validator,
		Limiter:              coverLimiter,
		RetryPolicy: service.RetryPolicy{
			MaxAttempts:    coverRequestAttempts(cfg.Retry.RequestMaxAttempts),
			InitialBackoff: cfg.Retry.InitialBackoff,
			MaxBackoff:     cfg.Retry.MaxBackoff,
		},
		OnProgress: progressLogger(ctx, cfg.Task.ID, logger),
	})
	if err != nil {
		return err
	}
	_, err = runner.Run(ctx)
	return err
}

// coverRequestAttempts 返回补跑使用的请求次数下限。
func coverRequestAttempts(configured int) int {
	if configured >= 8 {
		return configured
	}
	return 8
}

// coverConcurrency 返回补跑专用并发配置。
func coverConcurrency(runtime configs.RuntimeConfig) int {
	if runtime.CoverConcurrency > 0 {
		return runtime.CoverConcurrency
	}
	return 1
}

// coverRequestsPerMinute 返回补跑专用 RPM 配置。
func coverRequestsPerMinute(runtime configs.RuntimeConfig) int {
	if runtime.CoverRequestsPerMinute > 0 {
		return runtime.CoverRequestsPerMinute
	}
	if runtime.RequestsPerMinute > 0 && runtime.RequestsPerMinute < 10 {
		return runtime.RequestsPerMinute
	}
	return 10
}

// progressLogger 构造不包含样本载荷的进度日志回调。
func progressLogger(ctx context.Context, taskID string, logger *slog.Logger) func(service.Summary) {
	return func(summary service.Summary) {
		logger.InfoContext(
			ctx,
			"task progress",
			"task_id", taskID,
			"pending", summary.Pending,
			"retrying", summary.Retrying,
			"succeeded", summary.Succeeded,
			"failed", summary.Failed,
			"rate", summary.Rate,
			"eta", summary.ETA,
		)
	}
}

func terminalExportContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if ctx.Err() == nil {
		return ctx, func() {}
	}
	return context.WithTimeout(context.Background(), timeout)
}

func fail(ctx context.Context, logger *slog.Logger, taskID, category string) int {
	return failWithError(ctx, logger, taskID, category, nil)
}

func failWithError(ctx context.Context, logger *slog.Logger, taskID, category string, err error) int {
	if ctx.Err() != nil {
		logger.InfoContext(ctx, "task interrupted", "task_id", taskID, "error_category", "interrupted")
		return 130
	}
	if err != nil {
		logger.ErrorContext(ctx, "task failed", "task_id", taskID, "error_category", category, "error", err)
		return 1
	}
	logger.ErrorContext(ctx, "task failed", "task_id", taskID, "error_category", category)
	return 1
}
