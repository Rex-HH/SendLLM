package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// PolicyOptimizerSkill 表示冻结的 Skill 定义。
type PolicyOptimizerSkill struct {
	ID           string
	Executor     string
	Dir          string
	Instructions string
	InputSchema  json.RawMessage
	OutputSchema json.RawMessage
}

// PolicyOptimizerSkillRegistry 是 13 个固定 Skill 的只读注册表。
type PolicyOptimizerSkillRegistry struct {
	skills map[string]PolicyOptimizerSkill
}

// PolicyOptimizerSkillManifest 表示 Skill manifest 的严格结构。
type PolicyOptimizerSkillManifest struct {
	Version int                         `yaml:"version"`
	Skills  []PolicyOptimizerSkillEntry `yaml:"skills"`
}

// PolicyOptimizerSkillEntry 表示 manifest 中的单个 Skill。
type PolicyOptimizerSkillEntry struct {
	ID       string `yaml:"id"`
	Executor string `yaml:"executor"`
}

var policyOptimizerSkillExecutors = map[string]string{
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

var policyOptimizerSkillHeadings = []string{
	"Objective",
	"Applicable Input",
	"Procedure",
	"Mandatory Checks",
	"Prohibitions",
	"Output Contract",
	"Failure Handling",
}

// LoadPolicyOptimizerSkills 严格加载 manifest 和 13 个固定 Skill。
func LoadPolicyOptimizerSkills(root, manifestPath string) (*PolicyOptimizerSkillRegistry, error) {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(manifestPath) == "" {
		return nil, fmt.Errorf("policy optimizer skill root and manifest are required")
	}
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read policy optimizer skill manifest: %w", err)
	}
	manifest := PolicyOptimizerSkillManifest{}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("parse policy optimizer skill manifest: %w", err)
	}
	if manifest.Version != 1 || len(manifest.Skills) != len(policyOptimizerSkillExecutors) {
		return nil, fmt.Errorf("policy optimizer skill manifest must contain 13 skills")
	}
	entries := make(map[string]string, len(manifest.Skills))
	for _, entry := range manifest.Skills {
		wantExecutor, ok := policyOptimizerSkillExecutors[entry.ID]
		if !ok || entry.Executor != wantExecutor {
			return nil, fmt.Errorf("policy optimizer skill %q executor is invalid", entry.ID)
		}
		if _, exists := entries[entry.ID]; exists {
			return nil, fmt.Errorf("policy optimizer skill %q is duplicated", entry.ID)
		}
		entries[entry.ID] = entry.Executor
	}
	registry := &PolicyOptimizerSkillRegistry{skills: make(map[string]PolicyOptimizerSkill, len(entries))}
	for _, id := range sortedPolicyOptimizerSkillIDs(entries) {
		skill, err := loadPolicyOptimizerSkill(root, id, entries[id])
		if err != nil {
			return nil, err
		}
		registry.skills[id] = skill
	}
	return registry, nil
}

// Get 返回指定 Skill。
func (r *PolicyOptimizerSkillRegistry) Get(id string) (PolicyOptimizerSkill, bool) {
	skill, ok := r.skills[id]
	return skill, ok
}

// List 返回按 ID 排序的 Skill 副本。
func (r *PolicyOptimizerSkillRegistry) List() []PolicyOptimizerSkill {
	ids := make([]string, 0, len(r.skills))
	for id := range r.skills {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := make([]PolicyOptimizerSkill, 0, len(ids))
	for _, id := range ids {
		result = append(result, r.skills[id])
	}
	return result
}

// loadPolicyOptimizerSkill 校验并加载一个 Skill 目录。
func loadPolicyOptimizerSkill(root, id, executor string) (PolicyOptimizerSkill, error) {
	dir := filepath.Join(root, id)
	if err := validatePolicyOptimizerSkillDir(dir); err != nil {
		return PolicyOptimizerSkill{}, fmt.Errorf("policy optimizer skill %q: %w", id, err)
	}
	instructions, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return PolicyOptimizerSkill{}, fmt.Errorf("read policy optimizer skill %q instructions: %w", id, err)
	}
	for _, heading := range policyOptimizerSkillHeadings {
		if !strings.Contains(string(instructions), "## "+heading) {
			return PolicyOptimizerSkill{}, fmt.Errorf("policy optimizer skill %q lacks heading %q", id, heading)
		}
	}
	inputSchema, err := os.ReadFile(filepath.Join(dir, "input.schema.json"))
	if err != nil {
		return PolicyOptimizerSkill{}, fmt.Errorf("read policy optimizer skill %q input schema: %w", id, err)
	}
	outputSchema, err := os.ReadFile(filepath.Join(dir, "output.schema.json"))
	if err != nil {
		return PolicyOptimizerSkill{}, fmt.Errorf("read policy optimizer skill %q output schema: %w", id, err)
	}
	if !json.Valid(inputSchema) || !json.Valid(outputSchema) {
		return PolicyOptimizerSkill{}, fmt.Errorf("policy optimizer skill %q schema is invalid JSON", id)
	}
	return PolicyOptimizerSkill{
		ID: id, Executor: executor, Dir: dir,
		Instructions: string(instructions),
		InputSchema:  append(json.RawMessage(nil), inputSchema...),
		OutputSchema: append(json.RawMessage(nil), outputSchema...),
	}, nil
}

// validatePolicyOptimizerSkillDir 拒绝可执行文件和符号链接。
func validatePolicyOptimizerSkillDir(dir string) error {
	return filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink is forbidden: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&0o111 != 0 {
			return fmt.Errorf("executable is forbidden: %s", path)
		}
		return nil
	})
}

// sortedPolicyOptimizerSkillIDs 返回稳定 Skill ID 顺序。
func sortedPolicyOptimizerSkillIDs(values map[string]string) []string {
	ids := make([]string, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
