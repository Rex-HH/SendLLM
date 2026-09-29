package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"

	"sendllm/internal/dto"
	"sendllm/internal/lib/configs"
)

const policyOptimizerHiddenGoldPath = "Safety_Review_P04B_Hidden.jsonl"

// policyOptimizerFreshRotationExecutor 运行 base/candidate 的 fresh Safety Review 两轮 rotation。
type policyOptimizerFreshRotationExecutor struct {
	Config       *configs.PolicyOptimizerConfig
	BaseDir      string
	CandidateDir string
	Stderr       io.Writer
}

// Execute 执行一次 fresh rotation 并返回候选与 base 的逐条结果。
func (e policyOptimizerFreshRotationExecutor) Execute(
	ctx context.Context,
	rotationID string,
) (dto.PolicyOptimizerRotationResult, error) {
	if e.Config == nil || e.BaseDir == "" || e.CandidateDir == "" {
		return dto.PolicyOptimizerRotationResult{}, fmt.Errorf("policy optimizer fresh rotation is incomplete")
	}
	safetyConfig, err := configs.LoadSafetyReview(e.Config.Regression.SafetyReviewConfig)
	if err != nil {
		return dto.PolicyOptimizerRotationResult{}, err
	}
	hiddenPath := e.Config.Regression.HiddenGold
	if hiddenPath == "" {
		hiddenPath = policyOptimizerHiddenGoldPath
	}
	gold, err := loadPolicyOptimizerHiddenGold(hiddenPath)
	if err != nil {
		return dto.PolicyOptimizerRotationResult{}, err
	}
	baseTaskDir := filepath.Join(e.Config.Iteration.Dir, "regression", rotationID, "base")
	candidateTaskDir := filepath.Join(e.Config.Iteration.Dir, "regression", rotationID, "candidate")
	var baseHash, candidateHash string
	var baseCases, candidateCases []dto.PolicyOptimizerRegressionCase
	var baseErr, candidateErr error
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		baseHash, baseCases, baseErr = runPolicyOptimizerSafetyReviewRotation(
			ctx, safetyConfig, e.BaseDir, hiddenPath, gold, baseTaskDir, rotationID, e.Stderr,
		)
	}()
	go func() {
		defer wait.Done()
		candidateHash, candidateCases, candidateErr = runPolicyOptimizerSafetyReviewRotation(
			ctx, safetyConfig, e.CandidateDir, hiddenPath, gold, candidateTaskDir, rotationID, e.Stderr,
		)
	}()
	wait.Wait()
	if baseErr != nil {
		return dto.PolicyOptimizerRotationResult{}, baseErr
	}
	if candidateErr != nil {
		return dto.PolicyOptimizerRotationResult{}, candidateErr
	}
	return dto.PolicyOptimizerRotationResult{
		RotationID: rotationID, TaskDir: candidateTaskDir, OutputSHA256: candidateHash,
		Cases: candidateCases, BaseTaskDir: baseTaskDir, BaseOutputSHA256: baseHash, BaseCases: baseCases,
	}, nil
}

// policyOptimizerHiddenGold 表示 hidden Gold 的 regression 最小字段。
type policyOptimizerHiddenGold struct {
	TraceID   string
	Scene     string
	GoldLabel string
	CaseType  string
	Source    string
}

// loadPolicyOptimizerHiddenGold 读取 hidden Gold 元数据。
func loadPolicyOptimizerHiddenGold(path string) ([]policyOptimizerHiddenGold, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	result := make([]policyOptimizerHiddenGold, 0, 50)
	for lineNumber, line := range bytes.Split(raw, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		fields := map[string]any{}
		if err := json.Unmarshal(line, &fields); err != nil {
			return nil, fmt.Errorf("decode hidden gold line %d: %w", lineNumber+1, err)
		}
		record := policyOptimizerHiddenGold{
			TraceID: policyOptimizerString(fields["trace_id"]), Scene: policyOptimizerString(fields["scene"]),
			GoldLabel: policyOptimizerString(fields["gold_label"]), CaseType: policyOptimizerString(fields["case_type"]),
			Source: policyOptimizerString(fields["source"]),
		}
		if record.TraceID == "" || record.Scene == "" || record.GoldLabel == "" {
			return nil, fmt.Errorf("hidden gold line %d identity is incomplete", lineNumber+1)
		}
		result = append(result, record)
	}
	if len(result) != 50 {
		return nil, fmt.Errorf("hidden gold count is %d, want 50", len(result))
	}
	return result, nil
}

// runPolicyOptimizerSafetyReviewRotation 对一个 bundle 运行 prompt/response Safety Review。
func runPolicyOptimizerSafetyReviewRotation(
	ctx context.Context,
	baseConfig *configs.SafetyReviewConfig,
	bundleDir string,
	hiddenPath string,
	gold []policyOptimizerHiddenGold,
	taskRoot string,
	rotationID string,
	stderr io.Writer,
) (string, []dto.PolicyOptimizerRegressionCase, error) {
	if err := splitSafetyReviewForOptimizer(hiddenPath, taskRoot); err != nil {
		return "", nil, err
	}
	type sceneResult struct {
		hash        string
		cases       []dto.PolicyOptimizerRegressionCase
		outputBytes []byte
		err         error
	}
	sceneResults := make([]sceneResult, 2)
	scenes := []string{"prompt", "response"}
	var wait sync.WaitGroup
	for index, scene := range scenes {
		index, scene := index, scene
		wait.Add(1)
		go func() {
			defer wait.Done()
			cfg := cloneSafetyReviewConfig(baseConfig)
			if rotationID == "rotation-b" {
				swapSafetyReviewJudges(cfg)
			}
			cfg.Task.ID = fmt.Sprintf("policy-optimizer-%s-%s", rotationID, scene)
			cfg.Task.Input = filepath.Join(taskRoot, "inputs", scene+".jsonl")
			cfg.Task.TaskDir = filepath.Join(taskRoot, scene)
			cfg.Task.Scene = scene
			cfg.Policy.BundleDir = bundleDir
			rebaseSafetyReviewOutputs(cfg, cfg.Task.TaskDir)
			configPath := filepath.Join(taskRoot, scene+".yaml")
			if err := writeSafetyReviewOptimizerConfig(configPath, cfg); err != nil {
				sceneResults[index].err = err
				return
			}
			if code := RunSafetyReview(ctx, []string{"run", "--config", configPath}, io.Discard, stderr); code != 0 {
				sceneResults[index].err = fmt.Errorf("policy optimizer safety review %s %s failed", rotationID, scene)
				return
			}
			cleanRaw, err := os.ReadFile(cfg.Output.Clean)
			if err != nil {
				sceneResults[index].err = err
				return
			}
			quarantineRaw, err := os.ReadFile(cfg.Output.Quarantine)
			if err != nil {
				sceneResults[index].err = err
				return
			}
			predictions := map[string]dto.PolicyOptimizerRegressionCase{}
			if err := collectPolicyOptimizerPredictions(cleanRaw, false, predictions); err != nil {
				sceneResults[index].err = err
				return
			}
			if err := collectPolicyOptimizerPredictions(quarantineRaw, true, predictions); err != nil {
				sceneResults[index].err = err
				return
			}
			cases := make([]dto.PolicyOptimizerRegressionCase, 0)
			for _, item := range gold {
				if item.Scene != scene {
					continue
				}
				prediction, ok := predictions[item.TraceID]
				if !ok {
					sceneResults[index].err = fmt.Errorf("policy optimizer regression missing trace_id %s", item.TraceID)
					return
				}
				prediction.GoldLabel = item.GoldLabel
				prediction.Suite = policyOptimizerGoldSuite(item)
				cases = append(cases, prediction)
			}
			sceneResults[index].cases = cases
			sceneResults[index].outputBytes = append(append([]byte(nil), cleanRaw...), quarantineRaw...)
		}()
	}
	wait.Wait()
	outputHashInput := make([]byte, 0)
	cases := make([]dto.PolicyOptimizerRegressionCase, 0, len(gold))
	for _, result := range sceneResults {
		if result.err != nil {
			return "", nil, result.err
		}
		outputHashInput = append(outputHashInput, result.outputBytes...)
		cases = append(cases, result.cases...)
	}
	digest := sha256.Sum256(outputHashInput)
	return hex.EncodeToString(digest[:]), cases, nil
}

// splitSafetyReviewForOptimizer 复用 Safety Review hidden split，但允许任务根目录。
func splitSafetyReviewForOptimizer(hiddenPath, taskRoot string) error {
	_, err := splitSafetyReviewHiddenInput(hiddenPath, taskRoot)
	return err
}

// collectPolicyOptimizerPredictions 从 clean/quarantine 输出收集预测。
func collectPolicyOptimizerPredictions(
	raw []byte,
	quarantined bool,
	result map[string]dto.PolicyOptimizerRegressionCase,
) error {
	for lineNumber, line := range bytes.Split(raw, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		fields := map[string]any{}
		if err := json.Unmarshal(line, &fields); err != nil {
			return fmt.Errorf("decode safety review output line %d: %w", lineNumber+1, err)
		}
		traceID := policyOptimizerString(fields["trace_id"])
		if traceID == "" {
			return fmt.Errorf("safety review output line %d lacks trace_id", lineNumber+1)
		}
		label := ""
		if annotation, ok := fields["annotation"].(map[string]any); ok {
			label = policyOptimizerString(annotation["label"])
		}
		result[traceID] = dto.PolicyOptimizerRegressionCase{
			TraceID: traceID, PredictedLabel: label, Quarantined: quarantined,
		}
	}
	return nil
}

// policyOptimizerGoldSuite 返回 gate population suite。
func policyOptimizerGoldSuite(item policyOptimizerHiddenGold) string {
	if item.CaseType == "hard_negative" {
		return "hard_negative"
	}
	if item.Source == "approved_gold" || item.Source == "core_gold" {
		return "core_gold"
	}
	return "hidden"
}

// writeSafetyReviewOptimizerConfig 写入派生 Safety Review 配置。
func writeSafetyReviewOptimizerConfig(path string, cfg *configs.SafetyReviewConfig) error {
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

// policyOptimizerString 提取 JSON 字符串。
func policyOptimizerString(value any) string {
	text, _ := value.(string)
	return text
}
