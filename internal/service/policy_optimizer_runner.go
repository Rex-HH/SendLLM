package service

import (
	"context"
	"fmt"

	"sendllm/internal/dao"
	"sendllm/internal/dto"
)

// PolicyOptimizerRunnerConfig 指定 workflow 状态库和注入阶段执行器。
type PolicyOptimizerRunnerConfig struct {
	Store     *dao.PolicyOptimizerStore
	Preflight func(context.Context) error
	Stages    map[dto.PolicyOptimizerStage]func(context.Context) error
}

// PolicyOptimizerRunner 执行冻结的 Policy Optimizer mode graph。
type PolicyOptimizerRunner struct {
	cfg PolicyOptimizerRunnerConfig
}

// NewPolicyOptimizerRunner 构造 workflow runner。
func NewPolicyOptimizerRunner(cfg PolicyOptimizerRunnerConfig) (*PolicyOptimizerRunner, error) {
	if cfg.Store == nil || cfg.Preflight == nil {
		return nil, fmt.Errorf("policy optimizer runner requires store and preflight")
	}
	return &PolicyOptimizerRunner{cfg: cfg}, nil
}

// Run 执行 analyze 或 compile mode graph。
func (r *PolicyOptimizerRunner) Run(
	ctx context.Context,
	iterationID string,
	mode dto.PolicyOptimizerMode,
) (dto.PolicyOptimizerRunStats, error) {
	stageGraph, err := policyOptimizerStageGraph(mode)
	if err != nil {
		return dto.PolicyOptimizerRunStats{}, err
	}
	if err := r.cfg.Preflight(ctx); err != nil {
		return dto.PolicyOptimizerRunStats{}, err
	}
	recovered, err := r.cfg.Store.RecoverRunning(ctx, iterationID)
	if err != nil {
		return dto.PolicyOptimizerRunStats{}, err
	}
	stats := dto.PolicyOptimizerRunStats{Mode: string(mode), Recovered: recovered}
	for _, stage := range stageGraph {
		if err := ctx.Err(); err != nil {
			_ = r.cfg.Store.UpdateIterationStatus(ctx, iterationID, "interrupted", string(stage))
			return stats, err
		}
		if err := r.cfg.Store.UpdateIterationStatus(ctx, iterationID, "running", string(stage)); err != nil {
			return stats, err
		}
		executor := r.cfg.Stages[stage]
		if executor == nil {
			return stats, fmt.Errorf("policy optimizer stage %q is not configured", stage)
		}
		if err := executor(ctx); err != nil {
			return stats, err
		}
		stats.Stages = append(stats.Stages, string(stage))
	}
	stats.Status = "completed"
	return stats, nil
}

// Status 返回只读迭代状态。
func (r *PolicyOptimizerRunner) Status(ctx context.Context, iterationID string) (dao.PolicyOptimizerStatus, error) {
	return r.cfg.Store.ReadStatus(ctx, iterationID)
}

// policyOptimizerStageGraph 返回冻结 mode graph。
func policyOptimizerStageGraph(mode dto.PolicyOptimizerMode) ([]dto.PolicyOptimizerStage, error) {
	switch mode {
	case dto.PolicyOptimizerModeAnalyze:
		return []dto.PolicyOptimizerStage{
			dto.PolicyOptimizerStageInspectSources, dto.PolicyOptimizerStageNormalize,
			dto.PolicyOptimizerStageDisagreements, dto.PolicyOptimizerStageStratify,
			dto.PolicyOptimizerStageLocalMining, dto.PolicyOptimizerStageGlobalMerge,
			dto.PolicyOptimizerStageAttachCases, dto.PolicyOptimizerStageAdjudication,
			dto.PolicyOptimizerStageDiagnosis, dto.PolicyOptimizerStageRuleAuthoring,
			dto.PolicyOptimizerStageCritic, dto.PolicyOptimizerStageResolveChanges,
			dto.PolicyOptimizerStageCandidatePolicy, dto.PolicyOptimizerStagePromptCompile,
			dto.PolicyOptimizerStageRegression,
		}, nil
	case dto.PolicyOptimizerModeCompile:
		return []dto.PolicyOptimizerStage{
			dto.PolicyOptimizerStageChangeRequests, dto.PolicyOptimizerStageCritic,
			dto.PolicyOptimizerStageResolveChanges, dto.PolicyOptimizerStageCandidatePolicy,
			dto.PolicyOptimizerStagePromptCompile,
		}, nil
	default:
		return nil, fmt.Errorf("policy optimizer mode %q is invalid", mode)
	}
}
