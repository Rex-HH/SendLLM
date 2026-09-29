package dao_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sendllm/internal/dao"
)

// TestPolicyOptimizerArtifactPutIsImmutable 验证 Artifact 原子写入、哈希和重复拒绝。
func TestPolicyOptimizerArtifactPutIsImmutable(t *testing.T) {
	dir := t.TempDir()
	store, err := dao.OpenPolicyOptimizer(context.Background(), filepath.Join(dir, "state.db"))
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
	artifacts, err := dao.NewPolicyOptimizerArtifactStore(store, filepath.Join(dir, "ITER-001"))
	if err != nil {
		t.Fatalf("NewPolicyOptimizerArtifactStore() error = %v", err)
	}
	write := dao.PolicyOptimizerArtifactWrite{
		ID: "ART:one", IterationID: iteration.ID, Type: "preflight_report",
		RelativePath: "preflight/preflight-report.json", MediaType: "application/json",
		Bytes: []byte(`{"ok":true}`), ProducerStage: "preflight", Sensitivity: "review_metadata", Active: true,
	}
	artifact, err := artifacts.Put(context.Background(), write)
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ITER-001", write.RelativePath)); err != nil {
		t.Fatalf("artifact file missing: %v", err)
	}
	if artifact.SHA256 == "" || artifact.ByteSize != int64(len(write.Bytes)) {
		t.Fatalf("artifact metadata = %+v", artifact)
	}
	if _, err := artifacts.Put(context.Background(), write); err == nil {
		t.Fatal("Put() overwrote existing artifact")
	}
}
