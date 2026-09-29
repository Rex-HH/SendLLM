package dao_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sendllm/internal/dao"
)

// TestPolicyOptimizerStorePersistsRuns 验证迭代、Skill Run 和恢复状态。
func TestPolicyOptimizerStorePersistsRuns(t *testing.T) {
	store, err := dao.OpenPolicyOptimizer(context.Background(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("OpenPolicyOptimizer() error = %v", err)
	}
	defer func() { _ = store.Close() }()
	iteration := dao.PolicyOptimizerIteration{
		ID: "ITER-001", Mode: "analyze", Status: "running", CurrentStage: "preflight",
		BasePolicyVersion: "p04b-v1.0", BasePolicyHash: strings.Repeat("a", 64),
		ConfigHash: strings.Repeat("b", 64), SemanticHash: strings.Repeat("c", 64),
		ObjectiveJSON: []byte(`["discover_model_bias"]`), PermissionsJSON: []byte(`["rule_problem"]`),
	}
	if err := store.EnsureIteration(context.Background(), iteration); err != nil {
		t.Fatalf("EnsureIteration() error = %v", err)
	}
	key := dao.PolicyOptimizerRunKey{
		RunID: "RUN:one", IterationID: iteration.ID, Stage: "preflight", SkillID: "audit-source-interpreter",
		WorkID: "work-1", Executor: "model", InputSHA256: strings.Repeat("d", 64),
		ContextSHA256: strings.Repeat("e", 64), ModelProfile: "minimax_miner",
		ModelFamily: "minimax", AttemptNumber: 1,
	}
	run, ok, err := store.ClaimSkillRun(context.Background(), key)
	if err != nil || !ok || run.Status != "running" {
		t.Fatalf("ClaimSkillRun() = (%+v,%v,%v)", run, ok, err)
	}
	if err := store.CompleteSkillRun(context.Background(), dao.PolicyOptimizerSkillResult{
		RunID: key.RunID, Status: "succeeded", CompletedAt: time.Now(),
	}); err != nil {
		t.Fatalf("CompleteSkillRun() error = %v", err)
	}
	status, err := store.ReadStatus(context.Background(), iteration.ID)
	if err != nil || status.IterationID != iteration.ID {
		t.Fatalf("ReadStatus() = (%+v,%v)", status, err)
	}
}

// TestPolicyOptimizerStoreRejectsSemanticMismatch 验证迭代身份不可被静默替换。
func TestPolicyOptimizerStoreRejectsSemanticMismatch(t *testing.T) {
	store, err := dao.OpenPolicyOptimizer(context.Background(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("OpenPolicyOptimizer() error = %v", err)
	}
	defer func() { _ = store.Close() }()
	iteration := dao.PolicyOptimizerIteration{
		ID: "ITER-001", Mode: "analyze", Status: "running", CurrentStage: "preflight",
		BasePolicyVersion: "p04b-v1.0", BasePolicyHash: strings.Repeat("a", 64),
		ConfigHash: strings.Repeat("b", 64), SemanticHash: strings.Repeat("c", 64),
		ObjectiveJSON: []byte(`[]`), PermissionsJSON: []byte(`[]`),
	}
	if err := store.EnsureIteration(context.Background(), iteration); err != nil {
		t.Fatalf("EnsureIteration() error = %v", err)
	}
	iteration.SemanticHash = strings.Repeat("f", 64)
	if err := store.EnsureIteration(context.Background(), iteration); err == nil {
		t.Fatal("EnsureIteration accepted semantic hash mismatch")
	}
}
