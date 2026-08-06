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

	store, err := dao.Open(ctx, cfg.Task.State)
	if err != nil {
		return fail(ctx, logger, cfg.Task.ID, "storage")
	}
	defer func() { _ = store.Close() }()
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
	runner, err := service.NewRunner(service.RunnerConfig{
		TaskID:               cfg.Task.ID,
		SystemPrompt:         cfg.SystemPrompt,
		Scene:                cfg.Prompt.Scene,
		Schema:               cfg.ResultSchema,
		Mode:                 cfg.Model.StructuredOutput,
		MaxOutputTokens:      cfg.Model.MaxTokens,
		RequestMaxAttempts:   cfg.Retry.RequestMaxAttempts,
		FormatRepairAttempts: cfg.Retry.FormatRepairAttempts,
		Store:                store,
		Completer:            completer,
		Validator:            validator,
		Limiter:              requestLimiter,
		RetryPolicy: service.RetryPolicy{
			MaxAttempts:    cfg.Retry.RequestMaxAttempts,
			InitialBackoff: cfg.Retry.InitialBackoff,
			MaxBackoff:     cfg.Retry.MaxBackoff,
		},
		OnProgress: func(summary service.Summary) {
			logger.InfoContext(
				ctx,
				"task progress",
				"task_id", cfg.Task.ID,
				"pending", summary.Pending,
				"retrying", summary.Retrying,
				"succeeded", summary.Succeeded,
				"failed", summary.Failed,
				"rate", summary.Rate,
				"eta", summary.ETA,
			)
		},
	})
	if err != nil {
		return fail(ctx, logger, cfg.Task.ID, "runner")
	}

	_, runErr := runner.Run(ctx)
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

func terminalExportContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if ctx.Err() == nil {
		return ctx, func() {}
	}
	return context.WithTimeout(context.Background(), timeout)
}

func fail(ctx context.Context, logger *slog.Logger, taskID, category string) int {
	if ctx.Err() != nil {
		logger.InfoContext(ctx, "task interrupted", "task_id", taskID, "error_category", "interrupted")
		return 130
	}
	logger.ErrorContext(ctx, "task failed", "task_id", taskID, "error_category", category)
	return 1
}
