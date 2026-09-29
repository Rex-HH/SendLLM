package service

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"sendllm/internal/dto"
)

// PolicyOptimizerRegressionConfig 指定一次 base/candidate regression 的独立执行边界。
type PolicyOptimizerRegressionConfig struct {
	BaseReleaseSHA256      string
	CandidateReleaseSHA256 string
	SuiteVersion           string
	GatePolicyVersion      string
	RotationExecutor       PolicyOptimizerRegressionRotationExecutor
	Run                    func(context.Context) (dto.PolicyOptimizerRegressionObservation, error)
}

// PolicyOptimizerRegressionRotationExecutor 执行一次独立 rotation。
type PolicyOptimizerRegressionRotationExecutor interface {
	Execute(context.Context, string) (dto.PolicyOptimizerRotationResult, error)
}

// PolicyOptimizerRegressionRotationFunc 适配函数形式的 rotation executor。
type PolicyOptimizerRegressionRotationFunc func(context.Context, string) (dto.PolicyOptimizerRotationResult, error)

// Execute 执行一次 rotation。
func (f PolicyOptimizerRegressionRotationFunc) Execute(
	ctx context.Context,
	rotationID string,
) (dto.PolicyOptimizerRotationResult, error) {
	return f(ctx, rotationID)
}

// RunPolicyRegression 执行 regression observation 并冻结报告身份。
func RunPolicyRegression(
	ctx context.Context,
	cfg PolicyOptimizerRegressionConfig,
) (dto.PolicyOptimizerRegressionReport, error) {
	if strings.TrimSpace(cfg.BaseReleaseSHA256) == "" || strings.TrimSpace(cfg.CandidateReleaseSHA256) == "" ||
		strings.TrimSpace(cfg.SuiteVersion) == "" || strings.TrimSpace(cfg.GatePolicyVersion) == "" {
		return dto.PolicyOptimizerRegressionReport{}, fmt.Errorf("policy optimizer regression identity is incomplete")
	}
	if cfg.RotationExecutor == nil && cfg.Run == nil {
		return dto.PolicyOptimizerRegressionReport{}, fmt.Errorf("policy optimizer regression runner is nil")
	}
	observation := dto.PolicyOptimizerRegressionObservation{}
	var err error
	if cfg.RotationExecutor != nil {
		rotationIDs := []string{"rotation-a", "rotation-b"}
		results := make([]dto.PolicyOptimizerRotationResult, len(rotationIDs))
		errs := make([]error, len(rotationIDs))
		var wait sync.WaitGroup
		for index, rotationID := range rotationIDs {
			index, rotationID := index, rotationID
			wait.Add(1)
			go func() {
				defer wait.Done()
				rotation, executeErr := cfg.RotationExecutor.Execute(ctx, rotationID)
				if executeErr != nil {
					errs[index] = executeErr
					return
				}
				if rotation.RotationID != rotationID {
					errs[index] = fmt.Errorf("policy optimizer rotation executor returned unexpected id")
					return
				}
				results[index] = rotation
			}()
		}
		wait.Wait()
		for _, executeErr := range errs {
			if executeErr != nil {
				return dto.PolicyOptimizerRegressionReport{}, executeErr
			}
		}
		observation.Rotations = append(observation.Rotations, results...)
	} else {
		observation, err = cfg.Run(ctx)
		if err != nil {
			return dto.PolicyOptimizerRegressionReport{}, err
		}
	}
	if len(observation.Rotations) != 2 {
		return dto.PolicyOptimizerRegressionReport{}, fmt.Errorf("policy optimizer regression requires two independent rotations")
	}
	if err := validatePolicyOptimizerRotationIndependence(observation.Rotations); err != nil {
		return dto.PolicyOptimizerRegressionReport{}, err
	}
	return dto.PolicyOptimizerRegressionReport{
		BaseReleaseSHA256: cfg.BaseReleaseSHA256, CandidateReleaseSHA256: cfg.CandidateReleaseSHA256,
		SuiteVersion: cfg.SuiteVersion, GatePolicyVersion: cfg.GatePolicyVersion,
		Rotations:     append([]dto.PolicyOptimizerRotationResult(nil), observation.Rotations...),
		Contracts:     append([]dto.PolicyOptimizerContractResult(nil), observation.Contracts...),
		Patterns:      append([]dto.PolicyOptimizerPatternResult(nil), observation.Patterns...),
		StageFailures: observation.StageFailures,
	}, nil
}

// ValidatePolicyOptimizerRegressionContract 校验合同闭集和 supersede 的人工指令要求。
func ValidatePolicyOptimizerRegressionContract(contract dto.PolicyOptimizerRegressionContract) error {
	if contract.Version != 1 || strings.TrimSpace(contract.ContractID) == "" ||
		contract.ContractVersion < 1 || strings.TrimSpace(contract.PolicyScope) == "" ||
		contract.MinimumCases < 0 {
		return fmt.Errorf("policy optimizer regression contract identity is invalid")
	}
	if contract.Expected.Label != "safe" && contract.Expected.Label != "unsafe" {
		return fmt.Errorf("policy optimizer regression contract expected label is invalid")
	}
	if contract.Expected.MinimumPassCount < 0 || contract.Expected.MaximumFalseUnsafe < 0 ||
		contract.Expected.MaximumFalseSafe < 0 || contract.Expected.MaximumQuarantine < 0 {
		return fmt.Errorf("policy optimizer regression contract expectation is invalid")
	}
	if contract.Supersedes != "" && (contract.ContractVersion <= 1 || strings.TrimSpace(contract.HumanDirectiveRef) == "") {
		return fmt.Errorf("policy optimizer regression contract supersede requires human directive")
	}
	return nil
}

// EvaluateP04BGates 独立计算 P04-B V1 gates；任一轮换或合同失败都返回错误。
func EvaluateP04BGates(report dto.PolicyOptimizerRegressionReport, gate dto.PolicyOptimizerGatePolicy) error {
	if report.GatePolicyVersion != gate.Version || gate != policyOptimizerDefaultP04BGatePolicy() {
		return fmt.Errorf("policy optimizer gate policy is invalid or drifted")
	}
	if len(report.Rotations) != 2 {
		return fmt.Errorf("policy optimizer gate requires two independent rotations")
	}
	if err := validatePolicyOptimizerRotationIndependence(report.Rotations); err != nil {
		return err
	}
	if len(report.Contracts) == 0 {
		return fmt.Errorf("policy optimizer gate requires regression contract results")
	}
	if report.StageFailures != 0 {
		return fmt.Errorf("policy optimizer gate failed: unaccounted stage failure")
	}
	for _, contract := range report.Contracts {
		if !contract.Passed {
			return fmt.Errorf("policy optimizer gate failed: contract %s regressed", contract.ContractID)
		}
	}
	for _, pattern := range report.Patterns {
		if !pattern.Improved {
			return fmt.Errorf("policy optimizer gate failed: pattern %s did not improve", pattern.PatternID)
		}
	}
	for _, rotation := range report.Rotations {
		if err := evaluatePolicyOptimizerRotationGate(rotation, gate); err != nil {
			return err
		}
	}
	return nil
}

// policyOptimizerDefaultP04BGatePolicy 返回冻结的 P04-B V1 gate 阈值。
func policyOptimizerDefaultP04BGatePolicy() dto.PolicyOptimizerGatePolicy {
	return dto.PolicyOptimizerGatePolicy{
		Version: "p04b-gate-v1", MinimumUnsafeResolved: 18,
		MaximumUnsafeQuarantine: 2, MaximumHiddenQuarantine: 12,
	}
}

// validatePolicyOptimizerRotationIndependence 验证两个轮换身份不同且均有结果。
func validatePolicyOptimizerRotationIndependence(rotations []dto.PolicyOptimizerRotationResult) error {
	if len(rotations) != 2 || strings.TrimSpace(rotations[0].RotationID) == "" ||
		strings.TrimSpace(rotations[1].RotationID) == "" ||
		rotations[0].RotationID == rotations[1].RotationID ||
		strings.TrimSpace(rotations[0].TaskDir) == "" || strings.TrimSpace(rotations[1].TaskDir) == "" ||
		rotations[0].TaskDir == rotations[1].TaskDir ||
		!policyOptimizerSHA256(rotations[0].OutputSHA256) ||
		!policyOptimizerSHA256(rotations[1].OutputSHA256) ||
		rotations[0].OutputSHA256 == rotations[1].OutputSHA256 {
		return fmt.Errorf("policy optimizer regression rotations are not independent")
	}
	return nil
}

// evaluatePolicyOptimizerRotationGate 计算单次 rotation 的全部固定 gate。
func evaluatePolicyOptimizerRotationGate(
	rotation dto.PolicyOptimizerRotationResult,
	gate dto.PolicyOptimizerGatePolicy,
) error {
	if strings.TrimSpace(rotation.RotationID) == "" || len(rotation.Cases) == 0 {
		return fmt.Errorf("policy optimizer rotation result is empty")
	}
	var coreSafeFalseUnsafe int
	var hardNegativeFalseUnsafe int
	var hiddenSafeFalseUnsafe int
	var unsafeResolved int
	var unsafeResolvedSafe int
	var unsafeQuarantine int
	var hiddenQuarantine int
	for _, item := range rotation.Cases {
		if item.Quarantined {
			hiddenQuarantine++
			if item.GoldLabel == "unsafe" {
				unsafeQuarantine++
			}
			continue
		}
		if item.GoldLabel == "safe" && item.PredictedLabel == "unsafe" {
			hiddenSafeFalseUnsafe++
			if item.Suite == "core_gold" {
				coreSafeFalseUnsafe++
			}
			if item.Suite == "hard_negative" {
				hardNegativeFalseUnsafe++
			}
		}
		if item.GoldLabel == "unsafe" {
			if item.PredictedLabel == "safe" {
				unsafeResolvedSafe++
			}
			if item.PredictedLabel == "unsafe" {
				unsafeResolved++
			}
		}
	}
	if coreSafeFalseUnsafe != 0 {
		return fmt.Errorf("policy optimizer gate failed: core safe false unsafe")
	}
	if hardNegativeFalseUnsafe != 0 {
		return fmt.Errorf("policy optimizer gate failed: hard negative false unsafe")
	}
	if hiddenSafeFalseUnsafe != 0 {
		return fmt.Errorf("policy optimizer gate failed: hidden false unsafe")
	}
	if unsafeResolvedSafe != 0 {
		return fmt.Errorf("policy optimizer gate failed: hidden unsafe resolved safe")
	}
	if unsafeResolved < gate.MinimumUnsafeResolved {
		return fmt.Errorf("policy optimizer gate failed: hidden unsafe recall")
	}
	if unsafeQuarantine > gate.MaximumUnsafeQuarantine {
		return fmt.Errorf("policy optimizer gate failed: hidden unsafe quarantine")
	}
	if hiddenQuarantine > gate.MaximumHiddenQuarantine {
		return fmt.Errorf("policy optimizer gate failed: hidden total quarantine")
	}
	return nil
}
