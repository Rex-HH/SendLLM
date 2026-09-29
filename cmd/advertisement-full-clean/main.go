// Command advertisement-full-clean 实现广告数据两层全量复核和确定性清洗。
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"sendllm/internal/facade"
	"sendllm/internal/lib/configs"
	"sendllm/internal/lib/limiter"
	"sendllm/internal/service"
)

const defaultProvisionalPath = "data/task-013/task-013.reviewed.v8.with-advertisement.provisional.jsonl"

// main 执行 advertisement-full-clean 命令入口。
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runWithContext(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "advertisement-full-clean: %v\n", err)
		os.Exit(1)
	}
}

// run 解析并执行全量清洗子命令。
func run(args []string, stdout, stderr io.Writer) error {
	return runWithContext(context.Background(), args, stdout, stderr)
}

// runWithContext 在给定生命周期内解析并执行全量清洗子命令。
func runWithContext(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: advertisement-full-clean <calibration|select-risk|run|route|adjudicate|apply|replace|evaluate> [flags]")
	}
	switch args[0] {
	case "calibration":
		return runCalibration(args[1:], stdout, stderr)
	case "select-risk":
		return runSelectRisk(args[1:], stdout, stderr)
	case "run":
		return runLayer(ctx, args[1:], stdout, stderr)
	case "route":
		return runRoute(args[1:], stdout, stderr)
	case "adjudicate":
		return runAdjudicate(args[1:], stdout, stderr)
	case "apply":
		return runApply(args[1:], stdout, stderr)
	case "replace":
		return runReplace(args[1:], stdout, stderr)
	case "evaluate":
		return runEvaluate(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

// runSelectRisk 生成风险优先 pilot 的高风险 cohort 和分层对照 cohort。
func runSelectRisk(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("select-risk", flag.ContinueOnError)
	flags.SetOutput(stderr)
	sourcePath := flags.String("source", "", "广告源 JSON 数组")
	calibrationPath := flags.String("calibration", "", "冻结 calibration.jsonl")
	outputDir := flags.String("output-dir", "", "风险优先选择输出目录")
	highRiskLimit := flags.Int("high-risk-limit", 3000, "高风险 cohort 条数")
	controlLimit := flags.Int("control-limit", 1000, "分层对照 cohort 条数")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("select-risk unexpected arguments: %v", flags.Args())
	}
	report, err := selectRiskPriorityPilot(riskPilotConfig{
		SourcePath:      *sourcePath,
		CalibrationPath: *calibrationPath,
		OutputDir:       *outputDir,
		HighRiskLimit:   *highRiskLimit,
		ControlLimit:    *controlLimit,
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(
		stdout,
		"source=%d reviewed=%d high_risk=%d control=%d total=%d\n",
		report.SourceCount,
		report.ReviewedCount,
		report.HighRiskCount,
		report.ControlCount,
		report.TotalCount,
	)
	return err
}

// runCalibration 执行冻结校准 manifest 生成。
func runCalibration(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("calibration", flag.ContinueOnError)
	flags.SetOutput(stderr)
	sourcePath := flags.String("source", "", "广告源 JSON 数组或 JSONL")
	worksheetPath := flags.String("worksheet", "", "512 条人工 worksheet")
	layer1Mapping := flags.String("layer1-mapping", "", "第一层批次映射")
	layer1Decisions := flags.String("layer1-decisions", "", "第一层 decisions JSONL")
	layer2Mapping := flags.String("layer2-mapping", "", "第二层批次映射")
	layer2Decisions := flags.String("layer2-decisions", "", "第二层 decisions JSONL")
	outputDir := flags.String("output-dir", "", "校准产物输出目录")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("calibration unexpected arguments: %v", flags.Args())
	}
	report, err := buildCalibration(calibrationConfig{
		SourcePath:          *sourcePath,
		WorksheetPath:       *worksheetPath,
		Layer1MappingPath:   *layer1Mapping,
		Layer1DecisionsPath: *layer1Decisions,
		Layer2MappingPath:   *layer2Mapping,
		Layer2DecisionsPath: *layer2Decisions,
		OutputDir:           *outputDir,
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(
		stdout,
		"total=%d worksheet=%d directed=%d development=%d holdout=%d unsafe=%d safe=%d overlap_yes=%d\n",
		report.TotalCount,
		report.WorksheetCount,
		report.DirectedCount,
		report.DevelopmentCount,
		report.HoldoutCount,
		report.UnsafeCount,
		report.SafeCount,
		report.OverlapYesCount,
	)
	return err
}

// runLayer 装配并执行单层全量复核。
func runLayer(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "冻结 pilot 配置")
	reportPath := flags.String("report", "", "聚合报告输出路径")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("run unexpected arguments: %v", flags.Args())
	}
	if *configPath == "" || *reportPath == "" {
		return fmt.Errorf("run config and report are required")
	}
	cfg, err := configs.Load(*configPath)
	if err != nil {
		return err
	}
	if err := rejectProtectedRunPaths(cfg); err != nil {
		return err
	}
	apiKeys, err := cfg.APIKeys()
	if err != nil {
		return err
	}
	semanticHash, err := cfg.SemanticFingerprint()
	if err != nil {
		return err
	}
	completer, err := facade.NewOpenAI(facade.Config{
		BaseURL:        cfg.Model.BaseURL,
		APIKeys:        fullCleanFacadeAPIKeys(apiKeys),
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
		return err
	}
	requestLimiter, err := limiter.New(limiter.Config{
		Concurrency:       cfg.Runtime.Concurrency,
		RequestsPerMinute: cfg.Runtime.RequestsPerMinute,
		TokensPerMinute:   cfg.Runtime.TokensPerMinute,
	})
	if err != nil {
		return err
	}
	riskTypes := make(map[string]struct{}, len(cfg.RiskTypes))
	for name := range cfg.RiskTypes {
		riskTypes[name] = struct{}{}
	}
	stats, err := service.AdvertisementFullReview(ctx, service.AdvertisementFullReviewConfig{
		TaskID:       cfg.Task.ID,
		InputPath:    cfg.Task.Input,
		OutputPath:   cfg.Task.Output,
		StatePath:    cfg.Task.State,
		SemanticHash: semanticHash,
		SystemPrompt: cfg.SystemPrompt,
		Schema:       cfg.ResultSchema,
		Mode:         cfg.Model.StructuredOutput,
		Completer:    completer,
		Limiter:      requestLimiter,
		RiskTypes:    riskTypes,
		MaxTokens:    cfg.Model.MaxTokens,
		MaxAttempts:  cfg.Retry.RequestMaxAttempts,
		RetryPolicy: service.RetryPolicy{
			MaxAttempts:    cfg.Retry.RequestMaxAttempts,
			InitialBackoff: cfg.Retry.InitialBackoff,
			MaxBackoff:     cfg.Retry.MaxBackoff,
		},
		Shutdown:            cfg.Runtime.ShutdownTimeout,
		BatchSize:           cfg.Runtime.BatchSize,
		BatchMaxInputTokens: cfg.Runtime.BatchMaxInputTokens,
	})
	if err != nil {
		return err
	}
	if err := writeJSONFile(*reportPath, stats); err != nil {
		return err
	}
	_, err = fmt.Fprintf(
		stdout,
		"succeeded=%d failed=%d uncertain=%d safe=%d unsafe=%d requests=%d average_occupancy=%.2f\n",
		stats.Succeeded,
		stats.Failed,
		stats.Uncertain,
		stats.Safe,
		stats.Unsafe,
		stats.Requests,
		averageOccupancy(stats),
	)
	return err
}

// runRoute 执行确定性第二层路由。
func runRoute(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("route", flag.ContinueOnError)
	flags.SetOutput(stderr)
	sourcePath := flags.String("source", "", "广告源或 pilot 输入")
	layer1Decisions := flags.String("layer1-decisions", "", "第一层 decisions JSONL")
	outputDir := flags.String("output-dir", "", "第二层路由输出目录")
	batchSize := flags.Int("batch-size", 32, "最大批条数")
	maxTokens := flags.Int("batch-max-input-tokens", 80000, "最大输入 Token")
	if err := flags.Parse(args); err != nil {
		return err
	}
	report, err := routeLayer2(routeConfig{
		SourcePath:          *sourcePath,
		Layer1DecisionsPath: *layer1Decisions,
		OutputDir:           *outputDir,
		BatchSize:           *batchSize,
		BatchMaxInputTokens: *maxTokens,
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(
		stdout,
		"source=%d routed=%d unchanged=%d batches=%d tokens=%d..%d\n",
		report.SourceCount,
		report.RoutedCount,
		report.UnchangedCount,
		report.BatchCount,
		report.MinBatchTokens,
		report.MaxBatchTokens,
	)
	return err
}

// runAdjudicate 执行确定性两层裁决。
func runAdjudicate(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("adjudicate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	sourcePath := flags.String("source", "", "广告源或 pilot 输入")
	layer1Decisions := flags.String("layer1-decisions", "", "第一层 decisions JSONL")
	layer2Decisions := flags.String("layer2-decisions", "", "第二层 decisions JSONL")
	outputDir := flags.String("output-dir", "", "裁决 manifest 输出目录")
	if err := flags.Parse(args); err != nil {
		return err
	}
	report, err := adjudicate(adjudicateConfig{
		SourcePath:          *sourcePath,
		Layer1DecisionsPath: *layer1Decisions,
		Layer2DecisionsPath: *layer2Decisions,
		OutputDir:           *outputDir,
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(
		stdout,
		"approved=%d overlap=%d quarantine=%d unchanged=%d\n",
		report.ApprovedCount,
		report.OverlapCount,
		report.QuarantineCount,
		report.UnchangedCount,
	)
	return err
}

// runApply 应用 approved manifest 并写出新广告版本。
func runApply(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("apply", flag.ContinueOnError)
	flags.SetOutput(stderr)
	sourcePath := flags.String("source", "", "广告源")
	approvedPath := flags.String("approved", "", "approved-changes.jsonl")
	outputPath := flags.String("output", "", "新广告版本输出路径")
	if err := flags.Parse(args); err != nil {
		return err
	}
	report, err := applyApprovedChanges(applyConfig{
		SourcePath:          *sourcePath,
		ApprovedChangesPath: *approvedPath,
		OutputPath:          *outputPath,
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(
		stdout,
		"input=%d modified=%d unchanged=%d\n",
		report.InputRows,
		report.Modified,
		report.Unchanged,
	)
	return err
}

// runReplace 把 cleaned 广告替换进 provisional 新版本。
func runReplace(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("replace", flag.ContinueOnError)
	flags.SetOutput(stderr)
	fullPath := flags.String("full", "", "旧 provisional JSONL")
	cleanedPath := flags.String("cleaned", "", "cleaned 广告 JSON 数组")
	expectedIDsPath := flags.String("expected-ids", "", "可选广告源 ID 集合")
	outputPath := flags.String("output", "", "新 provisional 输出路径")
	if err := flags.Parse(args); err != nil {
		return err
	}
	report, err := replaceAdvertisementRows(replaceConfig{
		FullPath:        *fullPath,
		CleanedPath:     *cleanedPath,
		ExpectedIDsPath: *expectedIDsPath,
		OutputPath:      *outputPath,
		Protected:       []string{defaultProvisionalPath},
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(
		stdout,
		"total=%d replaced=%d unchanged=%d\n",
		report.TotalRows,
		report.ReplacedRows,
		report.UnchangedRows,
	)
	return err
}

// runEvaluate 评估冻结 holdout 阈值。
func runEvaluate(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("evaluate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	calibrationPath := flags.String("calibration", "", "冻结 calibration.jsonl")
	sourcePath := flags.String("source", "", "广告源或 pilot 输入")
	predictionsPath := flags.String("predictions", "", "最终 decisions JSONL")
	layer1ReportPath := flags.String("layer1-report", "", "第一层聚合报告")
	outputPath := flags.String("output", "", "评估报告输出路径")
	if err := flags.Parse(args); err != nil {
		return err
	}
	report, err := evaluatePilot(evaluationConfig{
		CalibrationPath:  *calibrationPath,
		SourcePath:       *sourcePath,
		PredictionsPath:  *predictionsPath,
		Layer1ReportPath: *layer1ReportPath,
		OutputPath:       *outputPath,
	})
	_, printErr := fmt.Fprintf(
		stdout,
		"holdout=%d accuracy=%.4f unsafe_recall=%.4f unsafe_to_safe=%d safe_recall=%.4f overlap=%.4f failed=%d passed=%t\n",
		report.HoldoutCount,
		report.Accuracy,
		report.HumanUnsafeRecall,
		report.UnsafePredictedSafe,
		report.HumanSafeRecall,
		report.OverlapRouting,
		report.FailedPredictions,
		report.Passed,
	)
	if printErr != nil {
		return printErr
	}
	return err
}

// fullCleanFacadeAPIKeys 转换配置层凭据，避免 facade 依赖 configs 类型。
func fullCleanFacadeAPIKeys(keys []configs.APIKey) []facade.APIKey {
	converted := make([]facade.APIKey, 0, len(keys))
	for _, key := range keys {
		converted = append(converted, facade.APIKey{Env: key.Env, Value: key.Value})
	}
	return converted
}

// rejectProtectedRunPaths 阻止单层运行覆盖受保护数据。
func rejectProtectedRunPaths(cfg *configs.Config) error {
	protected := []string{
		"data/ad/advertisement_dataset_final.json",
		"data/task-013/task-013.reviewed.v8.jsonl",
		defaultProvisionalPath,
	}
	for _, path := range protected {
		if err := ensureNotProtectedPath(cfg.Task.Output, []string{path}); err != nil {
			return err
		}
	}
	return nil
}

// ensureNotProtectedPath 只检查输出是否等于保护路径，允许恢复已有任务输出。
func ensureNotProtectedPath(path string, protected []string) error {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve path %q: %w", path, err)
	}
	for _, protectedPath := range protected {
		absoluteProtected, err := filepath.Abs(protectedPath)
		if err != nil {
			return fmt.Errorf("resolve protected path %q: %w", protectedPath, err)
		}
		if absolutePath == absoluteProtected {
			return fmt.Errorf("path %q is protected", path)
		}
	}
	return nil
}

// averageOccupancy 计算成功请求平均装载条数。
func averageOccupancy(stats service.AdvertisementFullReviewStats) float64 {
	if stats.SuccessfulRequests == 0 {
		return 0
	}
	return float64(stats.ItemsInSuccessfulReqs) / float64(stats.SuccessfulRequests)
}
