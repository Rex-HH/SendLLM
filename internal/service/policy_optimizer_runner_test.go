package service_test

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"sendllm/internal/dao"
	"sendllm/internal/dto"
	"sendllm/internal/service"
)

// TestPolicyOptimizerRunnerWorkflowGraphs 验证 analyze 和 compile 的冻结阶段图。
func TestPolicyOptimizerRunnerWorkflowGraphs(t *testing.T) {
	tests := []struct {
		name     string
		mode     dto.PolicyOptimizerMode
		wantLast dto.PolicyOptimizerStage
	}{
		{name: "analyze", mode: dto.PolicyOptimizerModeAnalyze, wantLast: dto.PolicyOptimizerStageRegression},
		{name: "compile", mode: dto.PolicyOptimizerModeCompile, wantLast: dto.PolicyOptimizerStagePromptCompile},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newPolicyOptimizerRunnerStore(t, "ITER-RUNNER-"+test.name)
			executed := make([]dto.PolicyOptimizerStage, 0)
			stages := map[dto.PolicyOptimizerStage]func(context.Context) error{}
			for _, stage := range []dto.PolicyOptimizerStage{
				dto.PolicyOptimizerStageInspectSources, dto.PolicyOptimizerStageNormalize,
				dto.PolicyOptimizerStageDisagreements, dto.PolicyOptimizerStageStratify,
				dto.PolicyOptimizerStageLocalMining, dto.PolicyOptimizerStageGlobalMerge,
				dto.PolicyOptimizerStageAttachCases, dto.PolicyOptimizerStageAdjudication,
				dto.PolicyOptimizerStageDiagnosis, dto.PolicyOptimizerStageRuleAuthoring,
				dto.PolicyOptimizerStageChangeRequests, dto.PolicyOptimizerStageCritic,
				dto.PolicyOptimizerStageResolveChanges, dto.PolicyOptimizerStageCandidatePolicy,
				dto.PolicyOptimizerStagePromptCompile, dto.PolicyOptimizerStageRegression,
			} {
				stage := stage
				stages[stage] = func(context.Context) error {
					executed = append(executed, stage)
					return nil
				}
			}
			runner, err := service.NewPolicyOptimizerRunner(service.PolicyOptimizerRunnerConfig{
				Store: store, Stages: stages, Preflight: func(context.Context) error { return nil },
			})
			if err != nil {
				t.Fatalf("NewPolicyOptimizerRunner() error = %v", err)
			}
			stats, err := runner.Run(context.Background(), "ITER-RUNNER-"+test.name, test.mode)
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if stats.Stages[len(stats.Stages)-1] != string(test.wantLast) {
				t.Fatalf("last stage = %q, want %q", stats.Stages[len(stats.Stages)-1], test.wantLast)
			}
			if test.mode == dto.PolicyOptimizerModeCompile &&
				reflect.DeepEqual(executed, []dto.PolicyOptimizerStage{dto.PolicyOptimizerStageLocalMining}) {
				t.Fatal("compile unexpectedly executed mining stage")
			}
		})
	}
}

// TestPolicyOptimizerRunnerPreflightBeforeClaims 验证 preflight 失败不会执行 stage。
func TestPolicyOptimizerRunnerPreflightBeforeClaims(t *testing.T) {
	store := newPolicyOptimizerRunnerStore(t, "ITER-PREFLIGHT")
	called := false
	runner, err := service.NewPolicyOptimizerRunner(service.PolicyOptimizerRunnerConfig{
		Store: store, Preflight: func(context.Context) error { return context.Canceled },
		Stages: map[dto.PolicyOptimizerStage]func(context.Context) error{
			dto.PolicyOptimizerStageChangeRequests: func(context.Context) error { called = true; return nil },
		},
	})
	if err != nil {
		t.Fatalf("NewPolicyOptimizerRunner() error = %v", err)
	}
	if _, err := runner.Run(context.Background(), "ITER-PREFLIGHT", dto.PolicyOptimizerModeCompile); err == nil {
		t.Fatal("Run() expected preflight error, got nil")
	}
	if called {
		t.Fatal("stage executed after failed preflight")
	}
}

func newPolicyOptimizerRunnerStore(t *testing.T, iterationID string) *dao.PolicyOptimizerStore {
	t.Helper()
	store, err := dao.OpenPolicyOptimizer(context.Background(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("OpenPolicyOptimizer() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.EnsureIteration(context.Background(), dao.PolicyOptimizerIteration{
		ID: iterationID, Mode: "analyze", Status: "created", CurrentStage: "preflight",
		BasePolicyVersion: "p04b-v1.0", BasePolicyHash: hashForPolicyOptimizerTest("base"),
		ConfigHash: hashForPolicyOptimizerTest("config"), SemanticHash: hashForPolicyOptimizerTest(iterationID),
		ObjectiveJSON: []byte(`["test"]`), PermissionsJSON: []byte(`["rule_problem"]`),
	}); err != nil {
		t.Fatalf("EnsureIteration() error = %v", err)
	}
	return store
}
