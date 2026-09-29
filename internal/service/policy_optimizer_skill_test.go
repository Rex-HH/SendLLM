package service_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"sendllm/internal/service"
)

// TestPolicyOptimizerSkillRegistryLoadsClosedSet 验证 manifest 与 13 个固定 Skill。
func TestPolicyOptimizerSkillRegistryLoadsClosedSet(t *testing.T) {
	root := t.TempDir()
	manifest := filepath.Join(root, "manifest.yaml")
	entries := []string{
		"audit-source-interpreter", "audit-normalizer", "disagreement-miner",
		"local-error-pattern-miner", "global-pattern-merger", "case-adjudicator",
		"policy-diagnoser", "policy-rule-author", "policy-critic", "change-resolver",
		"policy-prompt-compiler", "safety-regression-evaluator", "policy-release-manager",
	}
	executors := map[string]string{
		"audit-source-interpreter":    "model",
		"audit-normalizer":            "deterministic",
		"disagreement-miner":          "deterministic",
		"local-error-pattern-miner":   "model",
		"global-pattern-merger":       "model_with_deterministic_verifier",
		"case-adjudicator":            "model",
		"policy-diagnoser":            "model",
		"policy-rule-author":          "model",
		"policy-critic":               "model",
		"change-resolver":             "model_with_deterministic_verifier",
		"policy-prompt-compiler":      "deterministic",
		"safety-regression-evaluator": "deterministic",
		"policy-release-manager":      "deterministic",
	}
	writePolicyOptimizerTestFile(t, manifest, "version: 1\nskills:\n"+
		func() string {
			var out string
			for _, id := range entries {
				out += fmt.Sprintf("  - id: %s\n    executor: %s\n", id, executors[id])
			}
			return out
		}())
	for _, id := range entries {
		dir := filepath.Join(root, id)
		writePolicyOptimizerTestFile(t, filepath.Join(dir, "SKILL.md"), policyOptimizerSkillMarkdown())
		writePolicyOptimizerTestFile(t, filepath.Join(dir, "input.schema.json"), `{"type":"object"}`)
		writePolicyOptimizerTestFile(t, filepath.Join(dir, "output.schema.json"), `{"type":"object"}`)
	}
	registry, err := service.LoadPolicyOptimizerSkills(root, manifest)
	if err != nil {
		t.Fatalf("LoadPolicyOptimizerSkills() error = %v", err)
	}
	if len(registry.List()) != len(entries) {
		t.Fatalf("skills = %d, want %d", len(registry.List()), len(entries))
	}
	if _, ok := registry.Get("policy-critic"); !ok {
		t.Fatal("policy-critic missing")
	}
}

// TestPolicyOptimizerSkillRegistryRejectsUnknownExecutor 验证 executor 闭集。
func TestPolicyOptimizerSkillRegistryRejectsUnknownExecutor(t *testing.T) {
	root := t.TempDir()
	manifest := filepath.Join(root, "manifest.yaml")
	writePolicyOptimizerTestFile(t, manifest, "version: 1\nskills:\n  - id: policy-critic\n    executor: shell\n")
	if _, err := service.LoadPolicyOptimizerSkills(root, manifest); err == nil {
		t.Fatal("LoadPolicyOptimizerSkills accepted unknown executor")
	}
}

// TestPolicyOptimizerRepositorySkillsLoad 验证仓库内 13 个正式 Skill 资产可加载。
func TestPolicyOptimizerRepositorySkillsLoad(t *testing.T) {
	root := filepath.Join("..", "..", "policy-optimization", "skills")
	registry, err := service.LoadPolicyOptimizerSkills(root, filepath.Join(root, "manifest.yaml"))
	if err != nil {
		t.Fatalf("LoadPolicyOptimizerSkills(repository) error = %v", err)
	}
	if len(registry.List()) != 13 {
		t.Fatalf("repository skills = %d, want 13", len(registry.List()))
	}
}

// writePolicyOptimizerTestFile 写入测试资产。
func writePolicyOptimizerTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// policyOptimizerSkillMarkdown 返回满足固定标题的 Skill 文本。
func policyOptimizerSkillMarkdown() string {
	return `# Test Skill

## Objective
test

## Applicable Input
test

## Procedure
test

## Mandatory Checks
test

## Prohibitions
test

## Output Contract
test

## Failure Handling
test
`
}
