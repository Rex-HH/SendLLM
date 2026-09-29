package service_test

import (
	"strings"
	"testing"

	"sendllm/internal/service"
)

// TestPolicyOptimizerContextBuildsDeterministicPackage 验证上下文哈希和最小权限字段。
func TestPolicyOptimizerContextBuildsDeterministicPackage(t *testing.T) {
	input := service.PolicyOptimizerContextInput{
		SkillID: "local-error-pattern-miner", SkillVersion: 1, IterationID: "ITER-001",
		Objective: "discover_model_bias", Permissions: []string{"rule_problem"},
		PolicyVersion: "p04b-v1.0",
		ArtifactRefs: []service.PolicyOptimizerArtifactRef{{
			ID: "ART:one", Path: "batches/B-001.json", SHA256: strings.Repeat("a", 64),
		}},
		BatchID: "B-001", Records: nil,
		OutputSchema:   []byte(`{"type":"object"}`),
		MaxInputTokens: 1000, MaxArtifacts: 10,
	}
	first, err := service.BuildPolicyOptimizerContext(input)
	if err != nil {
		t.Fatalf("BuildPolicyOptimizerContext() error = %v", err)
	}
	second, err := service.BuildPolicyOptimizerContext(input)
	if err != nil {
		t.Fatalf("BuildPolicyOptimizerContext(second) error = %v", err)
	}
	if first.ContextHash == "" || first.ContextHash != second.ContextHash {
		t.Fatalf("context hash = %q/%q", first.ContextHash, second.ContextHash)
	}
}

// TestPolicyOptimizerContextRejectsOverflow 验证上下文不做静默截断。
func TestPolicyOptimizerContextRejectsOverflow(t *testing.T) {
	input := service.PolicyOptimizerContextInput{
		SkillID: "local-error-pattern-miner", SkillVersion: 1, IterationID: "ITER-001",
		Objective: "discover_model_bias", PolicyVersion: "p04b-v1.0",
		OutputSchema: []byte(`{"type":"object"}`), MaxInputTokens: 1, MaxArtifacts: 1,
	}
	if _, err := service.BuildPolicyOptimizerContext(input); err == nil {
		t.Fatal("BuildPolicyOptimizerContext accepted token overflow")
	}
}
