// Command advertisement-clean-prep 准备广告数据的定向人工或强模型清洗输入。
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

const defaultProvisionalPath = "data/task-013/task-013.reviewed.v8.with-advertisement.provisional.jsonl"

// main 执行 advertisement-clean-prep 命令入口。
func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "advertisement-clean-prep: %v\n", err)
		os.Exit(1)
	}
}

// run 解析子命令并执行广告清洗准备、第二遍路由或最终应用。
func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: advertisement-clean-prep <prepare|route|apply> [flags]")
	}
	switch args[0] {
	case "prepare":
		return runPrepare(args[1:], stdout, stderr)
	case "route":
		return runRoute(args[1:], stdout, stderr)
	case "apply":
		return runApply(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

// runPrepare 解析 prepare 参数并生成 P0/P1 队列、批次、映射和空 decisions。
func runPrepare(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("prepare", flag.ContinueOnError)
	flags.SetOutput(stderr)
	inputPath := flags.String("input", "", "广告源 JSON 数组")
	worksheetPath := flags.String("worksheet", "", "人工复核 worksheet CSV")
	outputDir := flags.String("output-dir", "", "准备产物输出目录（必须不存在）")
	maxBatchTokens := flags.Int("max-batch-tokens", defaultMaxBatchTokens, "每批最大输入 Token")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("prepare unexpected arguments: %v", flags.Args())
	}
	report, err := prepare(prepareConfig{
		InputPath:      *inputPath,
		WorksheetPath:  *worksheetPath,
		OutputDir:      *outputDir,
		MaxBatchTokens: *maxBatchTokens,
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(
		stdout,
		"p0=%d p1=%d queue=%d batches=%d tokens=%d..%d unique_ids=%d source_sha256=%s\n",
		report.P0Count,
		report.P1Count,
		report.QueueCount,
		report.BatchCount,
		report.MinBatchTokens,
		report.MaxBatchTokens,
		report.UniqueTraceIDs,
		report.SourceSHA256,
	)
	return err
}

// runRoute 解析 route 参数并把第一遍 safe/uncertain 结果路由到第二遍。
func runRoute(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("route", flag.ContinueOnError)
	flags.SetOutput(stderr)
	queuePath := flags.String("queue", "", "prepare 生成的 queue.jsonl")
	batchDir := flags.String("batch-dir", "", "第一遍 review-batches 目录")
	mappingPath := flags.String("mapping", "", "第一遍 review-batch-map.jsonl")
	decisionsPath := flags.String("decisions", "", "第一遍 decisions JSONL")
	outputDir := flags.String("output-dir", "", "第二遍产物输出目录（必须不存在）")
	maxBatchTokens := flags.Int("max-batch-tokens", defaultMaxBatchTokens, "每批最大输入 Token")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("route unexpected arguments: %v", flags.Args())
	}
	report, err := routeSecondPass(routeConfig{
		QueuePath:      *queuePath,
		BatchDir:       *batchDir,
		MappingPath:    *mappingPath,
		DecisionsPath:  *decisionsPath,
		OutputDir:      *outputDir,
		MaxBatchTokens: *maxBatchTokens,
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(
		stdout,
		"first_pass_batches=%d decisions=%d second_pass=%d batches=%d tokens=%d..%d unique_ids=%d\n",
		report.FirstPassBatches,
		report.DecisionsConsumed,
		report.SecondPassItems,
		report.BatchCount,
		report.MinBatchTokens,
		report.MaxBatchTokens,
		report.UniqueTraceIDs,
	)
	return err
}

// runApply 解析 apply 参数并只应用最终确认的 safe 结论。
func runApply(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("apply", flag.ContinueOnError)
	flags.SetOutput(stderr)
	inputPath := flags.String("input", "", "源 JSON 数组或 JSONL")
	outputPath := flags.String("output", "", "新版本输出路径（必须不存在）")
	format := flags.String("format", "auto", "输入输出格式：auto、json 或 jsonl")
	batchDir := flags.String("batch-dir", "", "第二遍 review-batches 目录")
	mappingPath := flags.String("mapping", "", "第二遍 review-batch-map.jsonl")
	decisionsPath := flags.String("decisions", "", "第二遍 decisions JSONL")
	protected := make([]string, 0)
	flags.Func("protected", "禁止覆盖的额外文件路径，可重复", func(value string) error {
		if value == "" {
			return fmt.Errorf("protected path must not be empty")
		}
		protected = append(protected, value)
		return nil
	})
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("apply unexpected arguments: %v", flags.Args())
	}
	report, err := applyDecisions(applyConfig{
		InputPath:     *inputPath,
		OutputPath:    *outputPath,
		Format:        *format,
		BatchDir:      *batchDir,
		MappingPath:   *mappingPath,
		DecisionsPath: *decisionsPath,
		Protected:     protected,
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(
		stdout,
		"input=%d output=%d decisions=%d modified=%d unchanged=%d\n",
		report.InputRows,
		report.OutputRows,
		report.DecisionCount,
		report.Modified,
		report.Unchanged,
	)
	return err
}
