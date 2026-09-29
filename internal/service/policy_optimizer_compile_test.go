package service_test

import (
	"os"
	"path/filepath"
	"testing"

	"sendllm/internal/dto"
	"sendllm/internal/service"
)

// TestPolicyOptimizerApplyCompileAndVerify 覆盖候选策略编译和发布包验证主链路。
func TestPolicyOptimizerApplyCompileAndVerify(t *testing.T) {
	baseDir := copyBundle(t)
	baseManifest, err := service.VerifyPolicyBundle(baseDir)
	if err != nil {
		t.Fatalf("VerifyPolicyBundle(base) error = %v", err)
	}
	candidateDir := filepath.Join(t.TempDir(), "candidate")
	set := dto.PolicyOptimizerChangeSet{
		BaseVersion:      "p04b-v1.0",
		BaseSHA256:       baseManifest.AggregateHash,
		CandidateVersion: "p04b-v1.1-candidate.1",
		AcceptedChanges: []dto.PolicyOptimizerChangeOperation{{
			Operation:  "replace_yaml_field",
			TargetPath: "policy/common.yaml",
			Pointer:    "/rule_ids",
			Value:      "DISCRIMINATION-R01",
		}},
	}
	candidate, err := service.ApplyPolicyChangeSet(baseDir, candidateDir, set)
	if err != nil {
		t.Fatalf("ApplyPolicyChangeSet() error = %v", err)
	}
	if candidate.Version != set.CandidateVersion || candidate.SHA256 == "" {
		t.Fatalf("candidate = %+v", candidate)
	}
	compileManifest, err := service.CompilePolicyPrompts(candidateDir, service.PolicyOptimizerCompileConfig{
		CompilerVersion: "1",
		MaxPromptBytes:  200_000,
	})
	if err != nil {
		t.Fatalf("CompilePolicyPrompts() error = %v", err)
	}
	if len(compileManifest.Files) == 0 || compileManifest.AggregateHash == "" {
		t.Fatalf("compile manifest = %+v", compileManifest)
	}
	verified, err := service.VerifyPolicyBundle(candidateDir)
	if err != nil {
		t.Fatalf("VerifyPolicyBundle(candidate) error = %v", err)
	}
	if verified.ReleaseVersion != set.CandidateVersion {
		t.Fatalf("verified version = %q, want %q", verified.ReleaseVersion, set.CandidateVersion)
	}
	for _, path := range []string{
		"prompts/judge-a.txt",
		"prompts/judge-b.txt",
		"prompts/router.txt",
		"prompts/expert.txt",
		"prompts/arbiter.txt",
		"prompts/refusal-reprompt.txt",
		"schemas/judgment.json",
		"schemas/router.json",
		"schemas/expert.json",
		"schemas/arbiter.json",
		"coverage-map.json",
		"compile-manifest.json",
	} {
		if _, err := os.Stat(filepath.Join(candidateDir, filepath.FromSlash(path))); err != nil {
			t.Fatalf("candidate asset %s: %v", path, err)
		}
	}
}

// TestPolicyOptimizerApplyRejectsInvalidChange 验证非法 patch、路径逃逸和重复卡片会失败。
func TestPolicyOptimizerApplyRejectsInvalidChange(t *testing.T) {
	tests := []struct {
		name string
		op   dto.PolicyOptimizerChangeOperation
	}{
		{
			name: "invalid pointer",
			op: dto.PolicyOptimizerChangeOperation{
				Operation: "replace_yaml_field", TargetPath: "policy/common.yaml", Pointer: "bad",
			},
		},
		{
			name: "path escape",
			op: dto.PolicyOptimizerChangeOperation{
				Operation: "replace_yaml_field", TargetPath: "../release.yaml", Pointer: "/rule_ids",
			},
		},
		{
			name: "unknown operation",
			op: dto.PolicyOptimizerChangeOperation{
				Operation: "overwrite_everything", TargetPath: "policy/common.yaml", Pointer: "/rule_ids",
			},
		},
		{
			name: "duplicate card",
			op: dto.PolicyOptimizerChangeOperation{
				Operation: "add_versioned_card",
				Target:    "ethnic_discrimination",
				Value: map[string]any{
					"id": "ethnic_discrimination", "axis": "attack_domain",
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			baseDir := copyBundle(t)
			candidateDir := filepath.Join(t.TempDir(), "candidate")
			set := dto.PolicyOptimizerChangeSet{
				BaseVersion: "p04b-v1.0", CandidateVersion: "p04b-v1.1-candidate.1",
				AcceptedChanges: []dto.PolicyOptimizerChangeOperation{test.op},
			}
			if _, err := service.ApplyPolicyChangeSet(baseDir, candidateDir, set); err == nil {
				t.Fatal("ApplyPolicyChangeSet() expected error, got nil")
			}
		})
	}
}

// TestPolicyOptimizerCompileIsByteStable 验证同一输入重复编译得到相同字节和清单。
func TestPolicyOptimizerCompileIsByteStable(t *testing.T) {
	candidateDir := compiledPolicyOptimizerCandidate(t)
	before, err := os.ReadFile(filepath.Join(candidateDir, "prompts", "judge-a.txt"))
	if err != nil {
		t.Fatalf("read first prompt: %v", err)
	}
	first, err := service.VerifyPolicyBundle(candidateDir)
	if err != nil {
		t.Fatalf("VerifyPolicyBundle() error = %v", err)
	}
	second, err := service.CompilePolicyPrompts(candidateDir, service.PolicyOptimizerCompileConfig{
		CompilerVersion: "1",
		MaxPromptBytes:  200_000,
	})
	if err != nil {
		t.Fatalf("CompilePolicyPrompts(second) error = %v", err)
	}
	after, err := os.ReadFile(filepath.Join(candidateDir, "prompts", "judge-a.txt"))
	if err != nil {
		t.Fatalf("read second prompt: %v", err)
	}
	if string(before) != string(after) || first.AggregateHash != second.AggregateHash {
		t.Fatal("compiled bundle changed across identical runs")
	}
}

// TestPolicyOptimizerVerifyRejectsCoverageAndSchemaMutation 验证 coverage 或 Schema 被改后无法通过验证。
func TestPolicyOptimizerVerifyRejectsCoverageAndSchemaMutation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, dir string)
	}{
		{
			name: "coverage",
			mutate: func(t *testing.T, dir string) {
				t.Helper()
				writeFile(t, filepath.Join(dir, "coverage-map.json"), `{"UNKNOWN-ID":["judge-a"]}`)
			},
		},
		{
			name: "schema",
			mutate: func(t *testing.T, dir string) {
				t.Helper()
				writeFile(t, filepath.Join(dir, "schemas", "judgment.json"), `{"type":`)
			},
		},
		{
			name: "compile manifest",
			mutate: func(t *testing.T, dir string) {
				t.Helper()
				writeFile(t, filepath.Join(dir, "compile-manifest.json"), `{}`)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidateDir := compiledPolicyOptimizerCandidate(t)
			test.mutate(t, candidateDir)
			refreshReleaseManifest(t, candidateDir)
			if _, err := service.VerifyPolicyBundle(candidateDir); err == nil {
				t.Fatal("VerifyPolicyBundle() expected mutation error, got nil")
			}
		})
	}
}

// TestPolicyOptimizerVerifyRejectsPromptMutation 验证已发布 prompt 字节被改变后验证失败。
func TestPolicyOptimizerVerifyRejectsPromptMutation(t *testing.T) {
	candidateDir := compiledPolicyOptimizerCandidate(t)
	path := filepath.Join(candidateDir, "prompts", "judge-a.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read prompt: %v", err)
	}
	if err := os.WriteFile(path, append(data, []byte("changed\n")...), 0o600); err != nil {
		t.Fatalf("mutate prompt: %v", err)
	}
	_, err = service.VerifyPolicyBundle(candidateDir)
	if err == nil {
		t.Fatal("VerifyPolicyBundle() expected prompt mutation error, got nil")
	}
}

// compiledPolicyOptimizerCandidate 构造可用于验证的候选发布包。
func compiledPolicyOptimizerCandidate(t *testing.T) string {
	t.Helper()
	baseDir := copyBundle(t)
	baseManifest, err := service.VerifyPolicyBundle(baseDir)
	if err != nil {
		t.Fatalf("VerifyPolicyBundle(base) error = %v", err)
	}
	candidateDir := filepath.Join(t.TempDir(), "candidate")
	set := dto.PolicyOptimizerChangeSet{
		BaseVersion: "p04b-v1.0", BaseSHA256: baseManifest.AggregateHash,
		CandidateVersion: "p04b-v1.1-candidate.1",
		AcceptedChanges: []dto.PolicyOptimizerChangeOperation{{
			Operation: "replace_yaml_field", TargetPath: "policy/common.yaml",
			Pointer: "/rule_ids", Value: "DISCRIMINATION-R01",
		}},
	}
	if _, err := service.ApplyPolicyChangeSet(baseDir, candidateDir, set); err != nil {
		t.Fatalf("ApplyPolicyChangeSet() error = %v", err)
	}
	if _, err := service.CompilePolicyPrompts(candidateDir, service.PolicyOptimizerCompileConfig{
		CompilerVersion: "1", MaxPromptBytes: 200_000,
	}); err != nil {
		t.Fatalf("CompilePolicyPrompts() error = %v", err)
	}
	return candidateDir
}
