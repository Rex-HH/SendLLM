// Package cli 提供 Safety Review 子命令的进程边界。
package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v3"

	"sendllm/internal/dao"
	"sendllm/internal/dto"
	"sendllm/internal/facade"
	"sendllm/internal/lib/configs"
	"sendllm/internal/lib/limiter"
	"sendllm/internal/service"
)

// RunSafetyReview 执行 safety-review validate/run 子命令。
func RunSafetyReview(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 && args[0] == "status" {
		return runSafetyReviewStatus(ctx, args[1:], stdout, stderr)
	}
	if len(args) != 0 && args[0] == "explain" {
		return runSafetyReviewExplain(ctx, args[1:], stdout, stderr)
	}
	command, configPath, err := parseSafetyReviewArgs(args)
	if err != nil {
		writeSafetyReviewError(stderr, "", "arguments")
		return 1
	}
	cfg, err := configs.LoadSafetyReview(configPath)
	if err != nil {
		writeSafetyReviewError(stderr, "", "configuration")
		return 1
	}
	if command == "eval" && cfg.Models.ExecutionMode == "single_profile" {
		writeSafetyReviewError(stderr, cfg.Task.ID, "configuration")
		return 1
	}
	if ctx.Err() != nil {
		writeSafetyReviewInterrupted(stderr, cfg.Task.ID)
		return 130
	}
	policy, err := service.LoadSafetyReviewPolicy(cfg.Policy.BundleDir)
	if err != nil {
		writeSafetyReviewError(stderr, cfg.Task.ID, "policy")
		return 1
	}
	if err := checkSafetyReviewAPIEnv(cfg); err != nil {
		writeSafetyReviewError(stderr, cfg.Task.ID, "api_key")
		return 1
	}
	if command == "validate" {
		return runSafetyReviewValidate(ctx, cfg, stdout, stderr)
	}
	if command == "eval" {
		return runSafetyReviewEval(ctx, cfg, policy, stdout, stderr)
	}
	return runSafetyReviewRun(ctx, cfg, policy, stdout, stderr)
}

// runSafetyReviewExplain 输出指定 trace 的各角色结构化诊断结果。
func runSafetyReviewExplain(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("safety-review explain", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	taskDir := flags.String("task-dir", "", "Safety Review task directory")
	traceID := flags.String("trace-id", "", "optional trace id")
	if err := flags.Parse(args); err != nil || *taskDir == "" || flags.NArg() != 0 {
		writeSafetyReviewError(stderr, "", "arguments")
		return 1
	}
	store, err := dao.OpenSafetyReviewReadOnly(ctx, filepath.Join(*taskDir, "state.db"))
	if err != nil {
		writeSafetyReviewError(stderr, "", "storage")
		return 1
	}
	defer func() { _ = store.Close() }()
	taskID, err := store.ReadSafetyReviewTaskID(ctx)
	if err != nil {
		writeSafetyReviewError(stderr, "", "storage")
		return 1
	}
	details, err := store.ReadSafetyReviewStageDetails(ctx, taskID, *traceID)
	if err != nil {
		writeSafetyReviewError(stderr, taskID, "storage")
		return 1
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	for _, detail := range details {
		if err := encoder.Encode(buildSafetyReviewExplainRow(detail)); err != nil {
			writeSafetyReviewError(stderr, taskID, "output")
			return 1
		}
	}
	return 0
}

// buildSafetyReviewExplainRow 构造不含 payload 的阶段诊断行。
func buildSafetyReviewExplainRow(detail dao.SafetyReviewStageDetail) map[string]any {
	row := map[string]any{
		"trace_id": detail.TraceID, "stage_key": detail.StageKey, "role": detail.Role,
		"state": detail.State, "model_profile": detail.ModelProfile,
		"model_family": detail.ModelFamily, "fallback_index": detail.Fallback,
		"error_category": detail.ErrorCategory,
	}
	if len(detail.ResultJSON) == 0 {
		return row
	}
	switch detail.Role {
	case "judge_a", "judge_b":
		var result dto.SafetyReviewJudgment
		if err := json.Unmarshal(detail.ResultJSON, &result); err != nil {
			row["result_parse_error"] = true
			return row
		}
		row["verdict"] = result.Verdict
		row["evidence_complete"] = result.EvidenceComplete
		row["attack_methods"] = result.AttackMethods
		row["attack_domains"] = result.AttackDomains
		row["exclusions"] = result.Exclusions
	case "router":
		var result dto.SafetyReviewRoute
		if err := json.Unmarshal(detail.ResultJSON, &result); err != nil {
			row["result_parse_error"] = true
			return row
		}
		row["coverage_complete"] = result.CoverageComplete
		row["method_candidates"] = safetyReviewCandidateCategories(result.AttackMethodCandidates)
		row["domain_candidates"] = safetyReviewCandidateCategories(result.AttackDomainCandidates)
		row["feature_kinds"] = safetyReviewFeatureKinds(result.Features)
	case "expert":
		var result dto.SafetyReviewExpertResult
		if err := json.Unmarshal(detail.ResultJSON, &result); err != nil {
			row["result_parse_error"] = true
			return row
		}
		row["axis"] = result.Axis
		row["category"] = result.Category
		row["verdict"] = result.Verdict
		row["conditions"] = safetyReviewConditionStates(result.Conditions)
		row["decisive_exclusions"] = safetyReviewExclusionStates(result.DecisiveExclusions)
		row["evidence_source"] = result.EvidenceSource
	case "arbiter":
		var result dto.SafetyReviewDecision
		if err := json.Unmarshal(detail.ResultJSON, &result); err != nil {
			row["result_parse_error"] = true
			return row
		}
		row["verdict"] = result.Verdict
		row["label"] = result.Label
		row["is_attack"] = result.IsAttack
		row["attack_methods"] = result.AttackMethods
		row["attack_domains"] = result.AttackDomains
		row["primary_attack_method"] = result.PrimaryAttackMethod
		row["primary_attack_domain"] = result.PrimaryAttackDomain
		row["primary_risk_type"] = result.PrimaryRiskType
		row["case_type"] = result.CaseType
		row["decision_rules"] = result.DecisionRules
		row["quarantine_reason"] = result.QuarantineReason
	}
	return row
}

// safetyReviewConditionStates 仅保留条件 ID 和状态。
func safetyReviewConditionStates(values []dto.SafetyReviewCondition) []map[string]string {
	result := make([]map[string]string, 0, len(values))
	for _, value := range values {
		result = append(result, map[string]string{"id": value.ID, "state": value.State})
	}
	return result
}

// safetyReviewExclusionStates 仅保留排除 ID 和状态。
func safetyReviewExclusionStates(values []dto.SafetyReviewExclusion) []map[string]string {
	result := make([]map[string]string, 0, len(values))
	for _, value := range values {
		result = append(result, map[string]string{"id": value.ID, "state": value.State})
	}
	return result
}

// safetyReviewCandidateCategories 返回候选类别列表。
func safetyReviewCandidateCategories(values []dto.SafetyReviewCandidate) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.Category)
	}
	return result
}

// safetyReviewFeatureKinds 返回 Router 特征类型列表，不输出 span。
func safetyReviewFeatureKinds(values []dto.SafetyReviewFeature) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.ID+":"+value.Kind)
	}
	return result
}

// runSafetyReviewStatus 执行只读 status/watch 命令。
func runSafetyReviewStatus(
	ctx context.Context,
	args []string,
	stdout, stderr io.Writer,
) int {
	flags := flag.NewFlagSet("safety-review status", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	taskDir := flags.String("task-dir", "", "Safety Review task directory")
	watch := flags.Bool("watch", false, "continue printing status")
	if err := flags.Parse(args); err != nil || *taskDir == "" || flags.NArg() != 0 {
		writeSafetyReviewError(stderr, "", "arguments")
		return 1
	}
	store, err := dao.OpenSafetyReviewReadOnly(ctx, filepath.Join(*taskDir, "state.db"))
	if err != nil {
		writeSafetyReviewError(stderr, "", "storage")
		return 1
	}
	defer func() { _ = store.Close() }()
	taskID, err := store.ReadSafetyReviewTaskID(ctx)
	if err != nil {
		writeSafetyReviewError(stderr, "", "storage")
		return 1
	}
	reporter, err := service.NewSafetyReviewStatusReporter(service.SafetyReviewStatusConfig{
		TaskID: taskID, Store: store, Interval: 50 * time.Millisecond, TTY: *watch,
	})
	if err != nil {
		writeSafetyReviewError(stderr, taskID, "status")
		return 1
	}
	if *watch {
		if err := reporter.Watch(ctx, taskID, stdout); err != nil {
			writeSafetyReviewError(stderr, taskID, "status")
			return 1
		}
		return 0
	}
	snapshot, err := reporter.Snapshot(ctx, taskID)
	if err != nil {
		writeSafetyReviewError(stderr, taskID, "status")
		return 1
	}
	if _, err := fmt.Fprintln(stdout, service.FormatSafetyReviewStatus(snapshot)); err != nil {
		writeSafetyReviewError(stderr, taskID, "status")
		return 1
	}
	return 0
}

// parseSafetyReviewArgs 解析固定命令形态。
func parseSafetyReviewArgs(args []string) (string, string, error) {
	if len(args) == 0 {
		return "", "", errors.New("safety review command is required")
	}
	command := args[0]
	flags := flag.NewFlagSet("safety-review "+command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "", "Safety Review task configuration")
	if err := flags.Parse(args[1:]); err != nil {
		return "", "", err
	}
	if *configPath == "" || flags.NArg() != 0 {
		return "", "", errors.New("safety review requires exactly --config")
	}
	switch command {
	case "validate", "run", "eval":
		return command, *configPath, nil
	default:
		return "", "", fmt.Errorf("unsupported safety review command %q", command)
	}
}

// runSafetyReviewEval 执行两轮 A/B 互换的独立评估。
func runSafetyReviewEval(
	ctx context.Context,
	cfg *configs.SafetyReviewConfig,
	policy *service.SafetyReviewPolicy,
	stdout, stderr io.Writer,
) int {
	if err := validateSafetyReviewTaskPaths(cfg); err != nil {
		if ctx.Err() != nil {
			writeSafetyReviewInterrupted(stderr, cfg.Task.ID)
			return 130
		}
		writeSafetyReviewError(stderr, cfg.Task.ID, "path")
		return 1
	}
	splitInputs, err := splitSafetyReviewHiddenInput(cfg.Task.Input, cfg.Task.TaskDir)
	if err != nil {
		if ctx.Err() != nil {
			writeSafetyReviewInterrupted(stderr, cfg.Task.ID)
			return 130
		}
		writeSafetyReviewError(stderr, cfg.Task.ID, "input")
		return 1
	}

	runs := make([]service.SafetyReviewEvalRun, 0, 4)
	for _, rotation := range []string{"rotation-a", "rotation-b"} {
		for _, scene := range []string{"prompt", "response"} {
			if ctx.Err() != nil {
				writeSafetyReviewInterrupted(stderr, cfg.Task.ID)
				return 130
			}
			runCfg := cloneSafetyReviewConfig(cfg)
			if rotation == "rotation-b" {
				swapSafetyReviewJudges(runCfg)
			}
			runCfg.Task.Scene = scene
			runCfg.Task.Input = splitInputs[scene]
			runCfg.Task.ID = fmt.Sprintf("%s-%s-%s", cfg.Task.ID, rotation, scene)
			runCfg.Task.TaskDir = filepath.Join(cfg.Task.TaskDir, rotation, scene)
			rebaseSafetyReviewOutputs(runCfg, cfg.Task.TaskDir)
			code := runSafetyReviewRun(ctx, runCfg, policy, io.Discard, stderr)
			if code != 0 {
				return code
			}
			runs = append(runs, service.SafetyReviewEvalRun{
				Rotation: rotation, Scene: scene,
				TaskDir: runCfg.Task.TaskDir, TaskID: runCfg.Task.ID,
			})
		}
	}

	schema, err := loadSafetyReviewResultSchema()
	if err != nil {
		writeSafetyReviewError(stderr, cfg.Task.ID, "eval")
		return 1
	}
	summary, err := service.EvaluateSafetyReviewOutputs(ctx, service.SafetyReviewEvaluationConfig{
		GoldPath: cfg.Task.Input,
		Schema:   schema,
		Runs:     runs,
	})
	if err != nil {
		writeSafetyReviewError(stderr, cfg.Task.ID, "eval")
		return 1
	}
	if err := writeSafetyReviewEvaluationSummary(stdout, summary); err != nil {
		writeSafetyReviewError(stderr, cfg.Task.ID, "eval")
		return 1
	}
	if !summary.Passed {
		writeSafetyReviewError(stderr, cfg.Task.ID, "eval")
		return 1
	}
	return 0
}

// writeSafetyReviewEvaluationSummary 输出每轮实际计算的验收指标。
func writeSafetyReviewEvaluationSummary(
	stdout io.Writer,
	summary service.SafetyReviewEvaluationSummary,
) error {
	if _, err := fmt.Fprintf(
		stdout, "eval=%s rotations=%d\n", passedLabel(summary.Passed), len(summary.Rotations),
	); err != nil {
		return err
	}
	for _, rotation := range summary.Rotations {
		clean := rotation.OutputCount - rotation.TotalQuarantined
		if clean < 0 {
			clean = 0
		}
		if _, err := fmt.Fprintf(
			stdout,
			"rotation=%s input=%d clean=%d quarantine=%d unsafe_resolved=%d unsafe_safe=%d "+
				"unsafe_quarantine=%d boundary_acceptable=%d\n",
			rotation.Rotation, rotation.OutputCount, clean, rotation.TotalQuarantined,
			rotation.UnsafeGoldResolvedUnsafe, rotation.UnsafeGoldFalseSafe,
			rotation.UnsafeGoldQuarantined, rotation.BoundaryAcceptable,
		); err != nil {
			return err
		}
	}
	return nil
}

// splitSafetyReviewHiddenInput 按 scene 拆分隐藏输入且不读取 Gold 标签。
func splitSafetyReviewHiddenInput(inputPath, taskDir string) (map[string]string, error) {
	raw, err := os.ReadFile(inputPath)
	if err != nil {
		return nil, fmt.Errorf("read hidden input: %w", err)
	}
	rows := map[string][]string{"prompt": {}, "response": {}}
	for lineNumber, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := map[string]json.RawMessage{}
		if err := json.Unmarshal([]byte(line), &fields); err != nil {
			return nil, fmt.Errorf("decode hidden input line %d: %w", lineNumber+1, err)
		}
		scene, err := safetyReviewStringField(fields, "scene")
		if err != nil {
			return nil, err
		}
		if scene != "prompt" && scene != "response" {
			return nil, fmt.Errorf("hidden input line %d has invalid scene", lineNumber+1)
		}
		copied, err := safetyReviewHiddenInputRow(fields)
		if err != nil {
			return nil, fmt.Errorf("hidden input line %d: %w", lineNumber+1, err)
		}
		rows[scene] = append(rows[scene], copied)
	}
	if len(rows["prompt"])+len(rows["response"]) != 50 {
		return nil, fmt.Errorf("hidden input count is %d, want 50", len(rows["prompt"])+len(rows["response"]))
	}
	result := map[string]string{}
	for scene, lines := range rows {
		path := filepath.Join(taskDir, "inputs", scene+".jsonl")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("create split input directory: %w", err)
		}
		if err := os.WriteFile(path, []byte(strings.Join(lines, "")), 0o600); err != nil {
			return nil, fmt.Errorf("write split input: %w", err)
		}
		result[scene] = path
	}
	return result, nil
}

// safetyReviewHiddenInputRow 仅保留运行所需的非 Gold 字段。
func safetyReviewHiddenInputRow(fields map[string]json.RawMessage) (string, error) {
	copied := map[string]json.RawMessage{}
	for _, name := range []string{"trace_id", "prompt", "response"} {
		value, ok := fields[name]
		if !ok {
			return "", fmt.Errorf("field %s is required", name)
		}
		copied[name] = value
	}
	raw, err := json.Marshal(copied)
	if err != nil {
		return "", err
	}
	return string(raw) + "\n", nil
}

// cloneSafetyReviewConfig 深拷贝严格配置。
func cloneSafetyReviewConfig(cfg *configs.SafetyReviewConfig) *configs.SafetyReviewConfig {
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		panic(err)
	}
	copied := &configs.SafetyReviewConfig{}
	if err := yaml.Unmarshal(raw, copied); err != nil {
		panic(err)
	}
	return copied
}

// swapSafetyReviewJudges 互换 A/B 主备模型链。
func swapSafetyReviewJudges(cfg *configs.SafetyReviewConfig) {
	cfg.Models.Roles["judge_a"], cfg.Models.Roles["judge_b"] =
		cfg.Models.Roles["judge_b"], cfg.Models.Roles["judge_a"]
}

// rebaseSafetyReviewOutputs 将导出路径移动到派生 task dir。
func rebaseSafetyReviewOutputs(cfg *configs.SafetyReviewConfig, originalTaskDir string) {
	relative := strings.TrimPrefix(cfg.Task.TaskDir, originalTaskDir)
	_ = relative
	cfg.Output.Clean = filepath.Join(cfg.Task.TaskDir, filepath.Base(cfg.Output.Clean))
	cfg.Output.Audit = filepath.Join(cfg.Task.TaskDir, filepath.Base(cfg.Output.Audit))
	cfg.Output.QualityEvents = filepath.Join(cfg.Task.TaskDir, filepath.Base(cfg.Output.QualityEvents))
	cfg.Output.Quarantine = filepath.Join(cfg.Task.TaskDir, filepath.Base(cfg.Output.Quarantine))
	cfg.Output.Report = filepath.Join(cfg.Task.TaskDir, filepath.Base(cfg.Output.Report))
	cfg.Output.RunStatus = filepath.Join(cfg.Task.TaskDir, filepath.Base(cfg.Output.RunStatus))
}

// loadSafetyReviewResultSchema 加载 Safety Review 正式结果 Schema。
func loadSafetyReviewResultSchema() (json.RawMessage, error) {
	current, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	for {
		path := filepath.Join(current, "config", "safety-review-result-schema.json")
		if _, err := os.Stat(path); err == nil {
			raw, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			return json.RawMessage(raw), nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil, fmt.Errorf("safety review result schema is missing")
		}
		current = parent
	}
}

// passedLabel 返回通过或失败标签。
func passedLabel(passed bool) string {
	if passed {
		return "PASS"
	}
	return "FAIL"
}

// runSafetyReviewValidate 执行零网络配置校验。
func runSafetyReviewValidate(
	ctx context.Context,
	cfg *configs.SafetyReviewConfig,
	stdout, stderr io.Writer,
) int {
	if err := validateSafetyReviewInput(cfg.Task.Input, cfg.Task.Scene); err != nil {
		writeSafetyReviewInputError(stderr, cfg.Task.ID, err)
		return 1
	}
	if err := validateSafetyReviewTaskPaths(cfg); err != nil {
		writeSafetyReviewError(stderr, cfg.Task.ID, "path")
		return 1
	}
	if ctx.Err() != nil {
		writeSafetyReviewInterrupted(stderr, cfg.Task.ID)
		return 130
	}
	_, _ = fmt.Fprintf(stdout, "validation=PASS task_id=%s\n", cfg.Task.ID)
	return 0
}

// runSafetyReviewRun 按固定顺序装配并执行 Safety Review 任务。
func runSafetyReviewRun(
	ctx context.Context,
	cfg *configs.SafetyReviewConfig,
	policy *service.SafetyReviewPolicy,
	stdout, stderr io.Writer,
) int {
	runtime, err := buildSafetyReviewRuntime(cfg)
	if err != nil {
		if ctx.Err() != nil {
			writeSafetyReviewInterrupted(stderr, cfg.Task.ID)
			return 130
		}
		writeSafetyReviewError(stderr, cfg.Task.ID, "model")
		return 1
	}
	if err := os.MkdirAll(cfg.Task.TaskDir, 0o700); err != nil {
		if ctx.Err() != nil {
			writeSafetyReviewInterrupted(stderr, cfg.Task.ID)
			return 130
		}
		writeSafetyReviewError(stderr, cfg.Task.ID, "path")
		return 1
	}
	store, err := dao.OpenSafetyReview(ctx, filepath.Join(cfg.Task.TaskDir, "state.db"))
	if err != nil {
		if ctx.Err() != nil {
			writeSafetyReviewInterrupted(stderr, cfg.Task.ID)
			return 130
		}
		writeSafetyReviewError(stderr, cfg.Task.ID, "storage")
		return 1
	}
	defer func() { _ = store.Close() }()

	snapshotDir, snapshotFiles, err := snapshotSafetyReviewPolicy(cfg.Policy.BundleDir, cfg.Task.TaskDir)
	if err != nil {
		if ctx.Err() != nil {
			writeSafetyReviewInterrupted(stderr, cfg.Task.ID)
			return 130
		}
		writeSafetyReviewError(stderr, cfg.Task.ID, "snapshot")
		return 1
	}
	fingerprint, err := cfg.SemanticFingerprint(snapshotFiles)
	if err != nil {
		if ctx.Err() != nil {
			writeSafetyReviewInterrupted(stderr, cfg.Task.ID)
			return 130
		}
		writeSafetyReviewError(stderr, cfg.Task.ID, "configuration")
		return 1
	}
	if err := store.EnsureTask(ctx, dao.SafetyReviewTask{
		ID: cfg.Task.ID, SemanticFingerprint: fingerprint,
		Scene: cfg.Task.Scene, SnapshotDir: snapshotDir,
	}); err != nil {
		if ctx.Err() != nil {
			writeSafetyReviewInterrupted(stderr, cfg.Task.ID)
			return 130
		}
		writeSafetyReviewError(stderr, cfg.Task.ID, "storage")
		return 1
	}
	input, err := os.Open(cfg.Task.Input)
	if err != nil {
		if ctx.Err() != nil {
			writeSafetyReviewInterrupted(stderr, cfg.Task.ID)
			return 130
		}
		writeSafetyReviewError(stderr, cfg.Task.ID, "input")
		return 1
	}
	importStats, importErr := store.ImportJSONL(ctx, cfg.Task.ID, input)
	closeErr := input.Close()
	if importErr != nil || closeErr != nil {
		if ctx.Err() != nil {
			writeSafetyReviewInterrupted(stderr, cfg.Task.ID)
			return 130
		}
		writeSafetyReviewError(stderr, cfg.Task.ID, "import")
		return 1
	}
	_, _ = fmt.Fprintf(
		stdout, "task_id=%s status=preflight added=%d skipped=%d\n",
		cfg.Task.ID, importStats.Added, importStats.Skipped,
	)

	preflightRequests, err := buildSafetyReviewPreflightRequests(policy, cfg.Task.Scene)
	if err != nil {
		if ctx.Err() != nil {
			writeSafetyReviewInterrupted(stderr, cfg.Task.ID)
			return 130
		}
		writeSafetyReviewError(stderr, cfg.Task.ID, "preflight")
		return 1
	}
	if err := service.RunSafetyReviewPreflight(ctx, service.SafetyReviewPreflightConfig{
		Registry: runtime.registry, Scene: cfg.Task.Scene, Schema: policy.Schemas["judgment"],
		Requests: preflightRequests,
		Retry: service.RetryPolicy{
			MaxAttempts:    cfg.Retry.TransientAttemptsPerModel,
			InitialBackoff: cfg.Retry.InitialBackoff,
			MaxBackoff:     cfg.Retry.MaxBackoff,
		},
		OnProbe: func(event service.SafetyReviewPreflightEvent) {
			if event.State == "running" {
				_, _ = fmt.Fprintf(
					stdout, "preflight role=%s profile=%s state=running\n",
					event.Role, event.Profile,
				)
				return
			}
			_, _ = fmt.Fprintf(
				stdout, "preflight role=%s profile=%s state=%s duration=%s error=%s\n",
				event.Role, event.Profile, event.State,
				event.Duration.Round(100*time.Millisecond), event.ErrorCategory,
			)
		},
	}); err != nil {
		status := "failed"
		exitCode := 1
		if ctx.Err() != nil {
			status = "interrupted"
			exitCode = 130
			writeSafetyReviewInterrupted(stderr, cfg.Task.ID)
		} else {
			writeSafetyReviewError(stderr, cfg.Task.ID, "preflight")
		}
		if err := writeSafetyReviewStatusFile(cfg, store, status); err != nil && exitCode != 130 {
			writeSafetyReviewError(stderr, cfg.Task.ID, "status")
			return 1
		}
		return exitCode
	}
	_, _ = fmt.Fprintf(stdout, "task_id=%s status=running\n", cfg.Task.ID)
	if _, err := store.RecoverRunning(ctx, cfg.Task.ID); err != nil {
		if ctx.Err() != nil {
			writeSafetyReviewInterrupted(stderr, cfg.Task.ID)
			return 130
		}
		writeSafetyReviewError(stderr, cfg.Task.ID, "storage")
		return 1
	}

	stoppingStore := &safetyReviewStoppingStore{SafetyReviewStore: store}
	runner, runErr := buildSafetyReviewRunner(cfg, policy, stoppingStore, runtime, stdout)
	if runErr != nil {
		if ctx.Err() != nil {
			writeSafetyReviewInterrupted(stderr, cfg.Task.ID)
			return 130
		}
		writeSafetyReviewError(stderr, cfg.Task.ID, "runner")
		return 1
	}
	runErr = executeSafetyReviewRunner(ctx, cfg, stoppingStore, runner)
	status := "completed"
	exitCode := 0
	if ctx.Err() != nil {
		status = "interrupted"
		exitCode = 130
		writeSafetyReviewInterrupted(stderr, cfg.Task.ID)
	} else if runErr != nil {
		status = "failed"
		exitCode = 1
		writeSafetyReviewError(stderr, cfg.Task.ID, "runner")
	}
	exporter, err := service.NewSafetyReviewExporter(service.SafetyReviewExporterConfig{
		TaskID: cfg.Task.ID, Store: store, Policy: policy,
		Clean: cfg.Output.Clean, Quarantine: cfg.Output.Quarantine,
		Audit: cfg.Output.Audit, QualityEvents: cfg.Output.QualityEvents,
		Report: cfg.Output.Report, RunStatus: cfg.Output.RunStatus,
		Status: status, Now: time.Now,
	})
	_, _ = fmt.Fprintf(stdout, "task_id=%s status=exporting\n", cfg.Task.ID)
	if err != nil {
		writeSafetyReviewError(stderr, cfg.Task.ID, "export")
		if exitCode == 0 {
			exitCode = 1
		}
	}
	var exportStats service.SafetyReviewExportStats
	if exporter != nil {
		exportStats, err = exporter.Export(context.WithoutCancel(ctx), cfg.Task.ID)
		if err != nil {
			writeSafetyReviewError(stderr, cfg.Task.ID, "export")
			if exitCode == 0 {
				exitCode = 1
			}
		}
	}
	summary, err := store.ReadSummary(context.WithoutCancel(ctx), cfg.Task.ID)
	if err != nil {
		writeSafetyReviewError(stderr, cfg.Task.ID, "storage")
		if exitCode == 0 {
			exitCode = 1
		}
	}
	if exitCode == 0 {
		_, _ = fmt.Fprintf(
			stdout,
			"task_id=%s status=completed added=%d skipped=%d decisions=%d\n",
			cfg.Task.ID, importStats.Added, importStats.Skipped, summary.Decisions,
		)
		_, _ = fmt.Fprintf(
			stdout,
			"clean=%d quarantine=%d audit=%d quality_events=%d\n",
			exportStats.Clean, exportStats.Quarantine, exportStats.Audit, exportStats.QualityEvents,
		)
	}
	return exitCode
}

// buildSafetyReviewPreflightRequests 构建每个角色真实提示词和 Schema 的无害探测请求。
func buildSafetyReviewPreflightRequests(
	policy *service.SafetyReviewPolicy,
	scene string,
) (map[dto.SafetyReviewRole]dto.CompletionRequest, error) {
	requests := make(map[dto.SafetyReviewRole]dto.CompletionRequest, 5)
	for _, role := range []dto.SafetyReviewRole{
		dto.SafetyReviewJudgeA,
		dto.SafetyReviewJudgeB,
		dto.SafetyReviewRouter,
		dto.SafetyReviewExpert,
		dto.SafetyReviewArbiter,
	} {
		request, err := buildSafetyReviewPreflightRequest(policy, scene, role)
		if err != nil {
			return nil, err
		}
		requests[role] = request
	}
	return requests, nil
}

// buildSafetyReviewPreflightRequest 为单个角色生成无害但契约真实的探测请求。
func buildSafetyReviewPreflightRequest(
	policy *service.SafetyReviewPolicy,
	scene string,
	role dto.SafetyReviewRole,
) (dto.CompletionRequest, error) {
	work := dao.SafetyReviewStageWork{
		TaskID: "preflight", TraceID: "preflight", Scene: scene,
		Prompt:   "synthetic harmless preflight prompt",
		Response: "synthetic harmless preflight response",
	}
	switch role {
	case dto.SafetyReviewJudgeA:
		work.Role, work.StageKey = string(dto.SafetyReviewJudgeA), "judge:a"
		call, err := buildSafetyReviewJudgeRequest(work, role, policy)
		return safetyReviewPreflightCompletionRequest(call, err)
	case dto.SafetyReviewJudgeB:
		work.Role, work.StageKey = string(dto.SafetyReviewJudgeB), "judge:b"
		call, err := buildSafetyReviewJudgeRequest(work, role, policy)
		return safetyReviewPreflightCompletionRequest(call, err)
	case dto.SafetyReviewRouter:
		work.Role, work.StageKey = string(dto.SafetyReviewRouter), "router"
		call, err := service.BuildSafetyReviewRouterRequest(work, policy)
		return safetyReviewPreflightCompletionRequest(call, err)
	case dto.SafetyReviewExpert:
		card := safetyReviewPreflightCard(policy, scene)
		if card == nil {
			return dto.CompletionRequest{}, errors.New("safety review preflight lacks enabled expert card")
		}
		work.Role = string(dto.SafetyReviewExpert)
		work.Axis, work.Category = card.Axis, card.ID
		work.StageKey = "expert:" + card.Axis + ":" + card.ID
		call, err := service.BuildSafetyReviewExpertRequest(work, policy)
		return safetyReviewPreflightCompletionRequest(call, err)
	case dto.SafetyReviewArbiter:
		work.Role, work.StageKey = string(dto.SafetyReviewArbiter), "arbiter"
		call, err := service.BuildSafetyReviewArbiterRequest(
			work,
			service.SafetyReviewArbiterPriorOutputs{},
			policy,
		)
		return safetyReviewPreflightCompletionRequest(call, err)
	default:
		return dto.CompletionRequest{}, fmt.Errorf("unsupported preflight role %q", role)
	}
}

// safetyReviewPreflightCompletionRequest 转换调用请求为底层 Completion 契约。
func safetyReviewPreflightCompletionRequest(
	call service.SafetyReviewCallRequest,
	err error,
) (dto.CompletionRequest, error) {
	if err != nil {
		return dto.CompletionRequest{}, err
	}
	return dto.CompletionRequest{
		Messages: append([]dto.Message(nil), call.Messages...),
		Schema:   append([]byte(nil), call.Schema...),
		Mode:     call.Mode,
	}, nil
}

// safetyReviewPreflightCard 选择当前场景可用的确定性规则卡。
func safetyReviewPreflightCard(
	policy *service.SafetyReviewPolicy,
	scene string,
) *service.SafetyReviewRuleCard {
	if policy == nil {
		return nil
	}
	var selected *service.SafetyReviewRuleCard
	for _, card := range policy.Cards {
		if !cardEnabledForSafetyReviewPreflight(card, scene) {
			continue
		}
		if selected == nil || card.ID < selected.ID {
			selected = card
		}
	}
	return selected
}

// cardEnabledForSafetyReviewPreflight 判断规则卡是否支持当前探测场景。
func cardEnabledForSafetyReviewPreflight(card *service.SafetyReviewRuleCard, scene string) bool {
	if card == nil {
		return false
	}
	for _, enabled := range card.EnabledScenes {
		if enabled == scene {
			return true
		}
	}
	return false
}

// checkSafetyReviewAPIEnv 校验全部 API env 名存在且值非空。
func checkSafetyReviewAPIEnv(cfg *configs.SafetyReviewConfig) error {
	seen := make(map[string]bool, len(cfg.Models.Profiles))
	for _, profile := range cfg.Models.Profiles {
		if seen[profile.APIKeyEnv] {
			continue
		}
		seen[profile.APIKeyEnv] = true
		value, ok := os.LookupEnv(profile.APIKeyEnv)
		if !ok || strings.TrimSpace(value) == "" {
			return fmt.Errorf("api environment %s is empty", profile.APIKeyEnv)
		}
	}
	return nil
}

// validateSafetyReviewInput 校验 JSONL 每行都是合法输入对象。
func validateSafetyReviewInput(path, scene string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open safety review input: %w", err)
	}
	defer func() { _ = file.Close() }()

	reader := bufio.NewReader(file)
	lineNumber := 0
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) != 0 {
			lineNumber++
			if err := validateSafetyReviewInputLine(line, scene); err != nil {
				return fmt.Errorf("input line %d: %w", lineNumber, err)
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return fmt.Errorf("read safety review input: %w", readErr)
		}
	}
}

// validateSafetyReviewInputLine 校验单行 JSON 输入。
func validateSafetyReviewInputLine(line []byte, scene string) error {
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(bytes.TrimSpace(line), &fields); err != nil || fields == nil {
		return errors.New("safety review input line is not a JSON object")
	}
	traceID, err := safetyReviewStringField(fields, "trace_id")
	if err != nil || traceID == "" {
		return errors.New("safety review input lacks trace_id")
	}
	prompt, err := safetyReviewStringField(fields, "prompt")
	if err != nil {
		return err
	}
	response, err := safetyReviewStringField(fields, "response")
	if err != nil {
		return err
	}
	if prompt == "" && response == "" {
		return errors.New("safety review input has empty prompt and response")
	}
	if scene == "prompt" && prompt == "" {
		return errors.New("prompt scene requires prompt")
	}
	if scene == "response" && response == "" {
		return errors.New("response scene requires response")
	}
	return nil
}

// safetyReviewStringField 提取字符串输入字段。
func safetyReviewStringField(fields map[string]json.RawMessage, name string) (string, error) {
	raw, ok := fields[name]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("decode safety review field %s: %w", name, err)
	}
	return value, nil
}

// validateSafetyReviewTaskPaths 校验任务路径不指向意外文件类型。
func validateSafetyReviewTaskPaths(cfg *configs.SafetyReviewConfig) error {
	if info, err := os.Stat(cfg.Task.TaskDir); err == nil && !info.IsDir() {
		return errors.New("task_dir is not a directory")
	}
	return nil
}

// safetyReviewRuntime 保存已构建的模型与配额依赖。
type safetyReviewRuntime struct {
	registry    service.SafetyReviewModelRegistry
	quota       service.SafetyReviewRunnerQuota
	quotaGroups map[dto.SafetyReviewRole]string
	workers     map[dto.SafetyReviewRole]int
}

// safetyReviewExecutionStore 是 Runner 和调用审计需要的持久化边界。
type safetyReviewExecutionStore interface {
	service.SafetyReviewRunnerStore
	service.SafetyReviewAttemptRecorder
}

// buildSafetyReviewRuntime 构建模型客户端、角色链和配额。
func buildSafetyReviewRuntime(cfg *configs.SafetyReviewConfig) (*safetyReviewRuntime, error) {
	connections := make(map[string]int, len(cfg.Models.Profiles))
	for _, role := range cfg.Models.Roles {
		profiles := append([]string{role.Primary}, role.Fallbacks...)
		for _, profileID := range profiles {
			connections[profileID] += role.Concurrency
		}
	}
	clients := make(map[string]service.Completer, len(cfg.Models.Profiles))
	for profileID, profile := range cfg.Models.Profiles {
		if connections[profileID] == 0 {
			continue
		}
		value, _ := os.LookupEnv(profile.APIKeyEnv)
		client, err := facade.NewOpenAI(facade.Config{
			BaseURL: profile.BaseURL, APIKeys: []facade.APIKey{{
				Env: profile.APIKeyEnv, Value: value,
			}}, Model: profile.Name, MaxTokens: profile.MaxTokens,
			Timeout: profile.Timeout, MaxConnections: connections[profileID],
		})
		if err != nil {
			return nil, fmt.Errorf("build profile %s: %w", profileID, err)
		}
		clients[profileID] = client
	}

	chains := make(map[dto.SafetyReviewRole][]service.SafetyReviewModel, len(cfg.Models.Roles))
	quotaGroups := make(map[dto.SafetyReviewRole]string, len(cfg.Models.Roles))
	workers := make(map[dto.SafetyReviewRole]int, len(cfg.Models.Roles))
	roleLimits := make(map[string]limiter.SafetyReviewRoleQuota, len(cfg.Models.Roles))
	for roleID, roleConfig := range cfg.Models.Roles {
		role := dto.SafetyReviewRole(roleID)
		chain := make([]service.SafetyReviewModel, 0, len(roleConfig.Fallbacks)+1)
		profileIDs := append([]string{roleConfig.Primary}, roleConfig.Fallbacks...)
		for _, profileID := range profileIDs {
			profile := cfg.Models.Profiles[profileID]
			chain = append(chain, service.SafetyReviewModel{
				Profile: profileID, Family: profile.Family, Completer: clients[profileID],
			})
		}
		chains[role] = chain
		workers[role] = roleConfig.Concurrency
		roleLimits[roleID] = limiter.SafetyReviewRoleQuota{
			Concurrency:       roleConfig.Concurrency,
			RequestsPerMinute: *roleConfig.RequestsPerMinute,
			TokensPerMinute:   *roleConfig.TokensPerMinute,
		}
		primaryProfile := cfg.Models.Profiles[roleConfig.Primary]
		if primaryProfile.QuotaGroup != "" {
			quotaGroups[role] = primaryProfile.QuotaGroup
		}
	}
	registry, err := service.NewSafetyReviewModelRegistry(chains)
	if err != nil {
		return nil, err
	}
	groupLimits := make(map[string]limiter.SafetyReviewGroupQuota, len(cfg.Models.QuotaGroups))
	for groupID, group := range cfg.Models.QuotaGroups {
		groupLimits[groupID] = limiter.SafetyReviewGroupQuota{
			Concurrency:       group.Concurrency,
			RequestsPerMinute: *group.RequestsPerMinute,
			TokensPerMinute:   *group.TokensPerMinute,
		}
	}
	quota, err := limiter.NewSafetyReviewQuota(limiter.SafetyReviewQuotaConfig{
		Roles: roleLimits, Groups: groupLimits,
	})
	if err != nil {
		return nil, err
	}
	return &safetyReviewRuntime{
		registry: registry, quota: quota, quotaGroups: quotaGroups, workers: workers,
	}, nil
}

// buildSafetyReviewRunner 构建持久化 Safety Review 调度器。
func buildSafetyReviewRunner(
	cfg *configs.SafetyReviewConfig,
	policy *service.SafetyReviewPolicy,
	store safetyReviewExecutionStore,
	runtime *safetyReviewRuntime,
	stdout io.Writer,
) (*service.SafetyReviewRunner, error) {
	var progressMu sync.Mutex
	requestValidator, err := service.NewSafetyReviewRequestValidator(policy)
	if err != nil {
		return nil, err
	}
	caller, err := service.NewSafetyReviewCaller(service.SafetyReviewCallerConfig{
		Registry: runtime.registry, Store: store,
		Retry: service.RetryPolicy{
			MaxAttempts:    cfg.Retry.TransientAttemptsPerModel,
			InitialBackoff: cfg.Retry.InitialBackoff,
			MaxBackoff:     cfg.Retry.MaxBackoff,
		},
		RequestValidator: requestValidator,
		RefusalPrefixes:  policy.Common.RefusalPrefixes,
	})
	if err != nil {
		return nil, err
	}
	return service.NewSafetyReviewRunner(service.SafetyReviewRunnerConfig{
		TaskID: cfg.Task.ID, Store: store, Caller: caller, Quota: runtime.quota,
		QuotaGroups: runtime.quotaGroups, Workers: runtime.workers, Policy: policy,
		ModelProfile: "configured-profiles", ModelFamily: "configured-families",
		APIKeyEnv: "configured-env", ShutdownTimeout: cfg.Runtime.ShutdownTimeout,
		Now: time.Now, BuildRequest: buildSafetyReviewStageRequest(policy),
		OnStage: func(event service.SafetyReviewStageEvent) {
			progressMu.Lock()
			defer progressMu.Unlock()
			if event.State == "running" {
				_, _ = fmt.Fprintf(
					stdout, "stage trace_id=%s role=%s stage=%s state=running profile=%s\n",
					event.TraceID, event.Role, event.StageKey, event.ModelProfile,
				)
				return
			}
			_, _ = fmt.Fprintf(
				stdout,
				"stage trace_id=%s role=%s stage=%s state=%s profile=%s duration=%s error=%s\n",
				event.TraceID, event.Role, event.StageKey, event.State, event.ModelProfile,
				event.Duration.Round(100*time.Millisecond), event.ErrorCategory,
			)
		},
	})
}

// buildSafetyReviewStageRequest 构建初始和 Expert 阶段请求。
func buildSafetyReviewStageRequest(
	policy *service.SafetyReviewPolicy,
) func(dao.SafetyReviewStageWork) (service.SafetyReviewCallRequest, error) {
	return func(work dao.SafetyReviewStageWork) (service.SafetyReviewCallRequest, error) {
		role := dto.SafetyReviewRole(work.Role)
		switch role {
		case dto.SafetyReviewRouter:
			return service.BuildSafetyReviewRouterRequest(work, policy)
		case dto.SafetyReviewExpert:
			return service.BuildSafetyReviewExpertRequest(work, policy)
		case dto.SafetyReviewJudgeA, dto.SafetyReviewJudgeB:
			return buildSafetyReviewJudgeRequest(work, role, policy)
		default:
			return service.SafetyReviewCallRequest{}, fmt.Errorf("unsupported role %q", work.Role)
		}
	}
}

// buildSafetyReviewJudgeRequest 构建 Judge A/B 请求。
func buildSafetyReviewJudgeRequest(
	work dao.SafetyReviewStageWork,
	role dto.SafetyReviewRole,
	policy *service.SafetyReviewPolicy,
) (service.SafetyReviewCallRequest, error) {
	policyJSON, err := json.Marshal(policy.Common)
	if err != nil {
		return service.SafetyReviewCallRequest{}, fmt.Errorf("encode judge policy: %w", err)
	}
	promptName := "judge_a"
	if role == dto.SafetyReviewJudgeB {
		promptName = "judge_b"
	}
	messages, err := service.BuildSafetyReviewMessages(role, dto.SafetyReviewRoleInput{
		TraceID: work.TraceID, Scene: work.Scene, Prompt: work.Prompt, Response: work.Response,
		Policy: string(policyJSON), Schema: policy.Schemas["judgment"],
		SystemPrompt: policy.Prompts[promptName],
	})
	if err != nil {
		return service.SafetyReviewCallRequest{}, err
	}
	return service.SafetyReviewCallRequest{
		TaskID: work.TaskID, TraceID: work.TraceID, StageKey: work.StageKey, Role: role,
		Scene:    work.Scene,
		Messages: messages, Schema: append([]byte(nil), policy.Schemas["judgment"]...),
		Mode: "json_object",
	}, nil
}

// executeSafetyReviewRunner 在取消时停止新领取并有限排空在途请求。
func executeSafetyReviewRunner(
	ctx context.Context,
	cfg *configs.SafetyReviewConfig,
	stoppingStore *safetyReviewStoppingStore,
	runner *service.SafetyReviewRunner,
) error {
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := runner.Run(runCtx)
		done <- err
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		stoppingStore.stop()
		timer := time.NewTimer(cfg.Runtime.ShutdownTimeout)
		defer timer.Stop()
		select {
		case err := <-done:
			return err
		case <-timer.C:
			cancel()
			select {
			case err := <-done:
				return err
			case <-time.After(cfg.Runtime.ShutdownTimeout):
				return errors.New("safety review shutdown timed out")
			}
		}
	}
}

// safetyReviewStoppingStore 在收到停止信号后拒绝新的阶段领取。
type safetyReviewStoppingStore struct {
	*dao.SafetyReviewStore
	stopped atomic.Bool
}

// stop 标记后续阶段领取应立即停止。
func (s *safetyReviewStoppingStore) stop() {
	s.stopped.Store(true)
}

// ClaimStage 在停止后不再领取新阶段。
func (s *safetyReviewStoppingStore) ClaimStage(
	ctx context.Context,
	claim dao.SafetyReviewClaim,
) (dao.SafetyReviewStageWork, bool, error) {
	if s.stopped.Load() {
		return dao.SafetyReviewStageWork{}, false, nil
	}
	return s.SafetyReviewStore.ClaimStage(ctx, claim)
}

// snapshotSafetyReviewPolicy 将冻结发布包复制为任务快照。
func snapshotSafetyReviewPolicy(bundleDir, taskDir string) (string, map[string][]byte, error) {
	snapshotDir := filepath.Join(taskDir, "snapshots")
	manifestPath := filepath.Join(bundleDir, "release.yaml")
	manifestRaw, err := os.ReadFile(manifestPath)
	if err != nil {
		return "", nil, fmt.Errorf("read release manifest: %w", err)
	}
	manifest := struct {
		Files []struct {
			Path string `yaml:"path"`
		} `yaml:"files"`
	}{}
	if err := yaml.Unmarshal(manifestRaw, &manifest); err != nil {
		return "", nil, fmt.Errorf("decode release manifest: %w", err)
	}
	files := map[string][]byte{"release.yaml": manifestRaw}
	for _, file := range manifest.Files {
		relative, err := validateSafetyReviewSnapshotPath(file.Path)
		if err != nil {
			return "", nil, err
		}
		raw, err := os.ReadFile(filepath.Join(bundleDir, file.Path))
		if err != nil {
			return "", nil, fmt.Errorf("read snapshot asset %s: %w", file.Path, err)
		}
		files[relative] = raw
	}
	if err := writeSafetyReviewSnapshot(snapshotDir, files); err != nil {
		return "", nil, err
	}
	return snapshotDir, files, nil
}

// validateSafetyReviewSnapshotPath 校验清单相对路径。
func validateSafetyReviewSnapshotPath(path string) (string, error) {
	clean := filepath.Clean(path)
	if path == "" || filepath.IsAbs(path) || clean == ".." ||
		strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid snapshot path %q", path)
	}
	return filepath.ToSlash(clean), nil
}

// writeSafetyReviewSnapshot 幂等写入并校验快照文件。
func writeSafetyReviewSnapshot(snapshotDir string, files map[string][]byte) error {
	if err := os.MkdirAll(snapshotDir, 0o700); err != nil {
		return fmt.Errorf("create snapshot directory: %w", err)
	}
	for relative, raw := range files {
		target := filepath.Join(snapshotDir, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return fmt.Errorf("create snapshot parent: %w", err)
		}
		existing, err := os.ReadFile(target)
		if err == nil {
			if !bytes.Equal(existing, raw) {
				return fmt.Errorf("snapshot %s changed", relative)
			}
			continue
		}
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("read snapshot %s: %w", relative, err)
		}
		temp, err := os.CreateTemp(filepath.Dir(target), ".snapshot-*")
		if err != nil {
			return fmt.Errorf("create snapshot temp: %w", err)
		}
		if _, err := temp.Write(raw); err == nil {
			err = temp.Sync()
		}
		closeErr := temp.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			err = os.Rename(temp.Name(), target)
		}
		if err != nil {
			_ = os.Remove(temp.Name())
			return fmt.Errorf("write snapshot %s: %w", relative, err)
		}
		if err := os.Chmod(target, 0o600); err != nil {
			return fmt.Errorf("chmod snapshot %s: %w", relative, err)
		}
	}
	return nil
}

// writeSafetyReviewStatusFile 原子写入不含载荷的 run-status。
func writeSafetyReviewStatusFile(
	cfg *configs.SafetyReviewConfig,
	store *dao.SafetyReviewStore,
	status string,
) error {
	summary, err := store.ReadSummary(context.WithoutCancel(context.Background()), cfg.Task.ID)
	if err != nil {
		return fmt.Errorf("read status summary: %w", err)
	}
	payload := map[string]any{
		"task_id": cfg.Task.ID, "status": status,
		"items": summary.Items, "stages": summary.Stages,
		"decisions": summary.Decisions, "updated_at": time.Now().UTC(),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode status: %w", err)
	}
	return writeSafetyReviewAtomicJSON(cfg.Output.RunStatus, raw)
}

// writeSafetyReviewAtomicJSON 使用同目录临时文件原子写入 JSON。
func writeSafetyReviewAtomicJSON(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create status directory: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".run-status-*")
	if err != nil {
		return fmt.Errorf("create status temp: %w", err)
	}
	if _, err := temp.Write(raw); err == nil {
		err = temp.Sync()
	}
	closeErr := temp.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(temp.Name(), path)
	}
	if err != nil {
		_ = os.Remove(temp.Name())
		return fmt.Errorf("write status: %w", err)
	}
	return os.Chmod(path, 0o600)
}

// writeSafetyReviewError 输出安全错误摘要。
func writeSafetyReviewError(stderr io.Writer, taskID, category string) {
	_, _ = fmt.Fprintf(stderr, "safety-review task_id=%s error_category=%s\n", taskID, category)
}

// writeSafetyReviewInputError 输出不含 payload 的输入校验摘要。
func writeSafetyReviewInputError(stderr io.Writer, taskID string, err error) {
	_, _ = fmt.Fprintf(
		stderr, "safety-review task_id=%s error_category=input error_summary=%q\n", taskID, err.Error(),
	)
}

// writeSafetyReviewInterrupted 输出安全中断摘要。
func writeSafetyReviewInterrupted(stderr io.Writer, taskID string) {
	_, _ = fmt.Fprintf(stderr, "safety-review task_id=%s status=interrupted\n", taskID)
}
