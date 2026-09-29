package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"sendllm/internal/dto"
)

// PolicyOptimizerCompileConfig 指定确定性 Prompt 编译参数。
type PolicyOptimizerCompileConfig struct {
	CompilerVersion string
	MaxPromptBytes  int
}

// ApplyPolicyChangeSet 复制基础发布包并应用封闭 patch 操作。
func ApplyPolicyChangeSet(
	baseDir string,
	candidateDir string,
	set dto.PolicyOptimizerChangeSet,
) (dto.PolicyOptimizerCandidate, error) {
	if strings.TrimSpace(set.BaseVersion) == "" || strings.TrimSpace(set.CandidateVersion) == "" {
		return dto.PolicyOptimizerCandidate{}, fmt.Errorf("policy optimizer candidate versions are required")
	}
	if err := ValidateChangeSet(set); err != nil {
		return dto.PolicyOptimizerCandidate{}, err
	}
	baseManifest, err := VerifyPolicyBundle(baseDir)
	if err != nil {
		return dto.PolicyOptimizerCandidate{}, fmt.Errorf("verify base policy bundle: %w", err)
	}
	if set.BaseSHA256 == "" || set.BaseSHA256 != baseManifest.AggregateHash {
		return dto.PolicyOptimizerCandidate{}, fmt.Errorf("policy optimizer base hash mismatch")
	}
	if set.BaseVersion != baseManifest.ReleaseVersion {
		return dto.PolicyOptimizerCandidate{}, fmt.Errorf("policy optimizer base version mismatch")
	}
	if err := copyPolicyOptimizerBundle(baseDir, candidateDir); err != nil {
		return dto.PolicyOptimizerCandidate{}, err
	}
	for _, operation := range set.AcceptedChanges {
		if err := applyPolicyOptimizerOperation(candidateDir, operation); err != nil {
			return dto.PolicyOptimizerCandidate{}, err
		}
	}
	if err := updatePolicyOptimizerReleaseVersion(candidateDir, set.CandidateVersion); err != nil {
		return dto.PolicyOptimizerCandidate{}, err
	}
	candidateHash, err := hashPolicyOptimizerDirectory(candidateDir)
	if err != nil {
		return dto.PolicyOptimizerCandidate{}, err
	}
	return dto.PolicyOptimizerCandidate{
		Version: set.CandidateVersion, BaseVersion: set.BaseVersion,
		BaseSHA256: baseManifest.AggregateHash, Path: candidateDir, SHA256: candidateHash,
	}, nil
}

// CompilePolicyPrompts 从候选策略资产确定性生成 Prompt、coverage map 和清单。
func CompilePolicyPrompts(
	candidateDir string,
	cfg PolicyOptimizerCompileConfig,
) (dto.PolicyOptimizerCompileManifest, error) {
	if strings.TrimSpace(cfg.CompilerVersion) == "" {
		return dto.PolicyOptimizerCompileManifest{}, fmt.Errorf("policy optimizer compiler version is required")
	}
	inputs, err := loadPolicyOptimizerCompileInputs(candidateDir)
	if err != nil {
		return dto.PolicyOptimizerCompileManifest{}, err
	}
	coverage, err := buildPolicyOptimizerCoverage(inputs.common, inputs.cards, inputs.decisions)
	if err != nil {
		return dto.PolicyOptimizerCompileManifest{}, err
	}
	for _, role := range policyOptimizerPromptRoles() {
		content, err := renderPolicyOptimizerPrompt(role, inputs, coverage, cfg.MaxPromptBytes)
		if err != nil {
			return dto.PolicyOptimizerCompileManifest{}, err
		}
		path := filepath.Join(candidateDir, "prompts", role+".txt")
		if err := writePolicyOptimizerAtomic(path, []byte(content)); err != nil {
			return dto.PolicyOptimizerCompileManifest{}, err
		}
	}
	coverageRaw, err := marshalPolicyOptimizerCanonicalJSON(coverage)
	if err != nil {
		return dto.PolicyOptimizerCompileManifest{}, err
	}
	if err := writePolicyOptimizerAtomic(filepath.Join(candidateDir, "coverage-map.json"), coverageRaw); err != nil {
		return dto.PolicyOptimizerCompileManifest{}, err
	}
	compileManifest, err := writePolicyOptimizerCompileManifest(candidateDir, cfg, coverage)
	if err != nil {
		return dto.PolicyOptimizerCompileManifest{}, err
	}
	if err := writePolicyOptimizerReleaseManifest(candidateDir, cfg.CompilerVersion, compileManifest.AggregateHash); err != nil {
		return dto.PolicyOptimizerCompileManifest{}, err
	}
	verified, err := VerifyPolicyBundle(candidateDir)
	if err != nil {
		return dto.PolicyOptimizerCompileManifest{}, err
	}
	compileManifest.AggregateHash = verified.AggregateHash
	return compileManifest, nil
}

// VerifyPolicyBundle 使用 Safety Review 冻结发布包契约验证候选 bundle。
func VerifyPolicyBundle(bundleDir string) (dto.PolicyOptimizerBundleManifest, error) {
	policy, err := LoadSafetyReviewPolicy(bundleDir)
	if err != nil {
		return dto.PolicyOptimizerBundleManifest{}, err
	}
	manifest, err := loadSafetyReviewManifest(filepath.Join(bundleDir, "release.yaml"))
	if err != nil {
		return dto.PolicyOptimizerBundleManifest{}, err
	}
	coverage, err := readPolicyOptimizerCoverage(bundleDir)
	if err != nil {
		return dto.PolicyOptimizerBundleManifest{}, err
	}
	if coverage != nil && !policyOptimizerCoverageEqual(coverage, policy.Coverage) {
		return dto.PolicyOptimizerBundleManifest{}, invalidSafetyReviewPolicy("coverage map does not match policy assets")
	}
	if coverage == nil {
		coverage = policy.Coverage
	}
	compileManifest, err := readPolicyOptimizerCompileManifest(bundleDir)
	if err != nil {
		return dto.PolicyOptimizerBundleManifest{}, err
	}
	if compileManifest != nil {
		if compileManifest.ReleaseVersion != policy.ReleaseVersion ||
			compileManifest.CompilerVersion != policy.CompilerVersion ||
			!policyOptimizerCoverageEqual(compileManifest.Coverage, coverage) {
			return dto.PolicyOptimizerBundleManifest{}, invalidSafetyReviewPolicy("compile manifest does not match bundle")
		}
	}
	files := make([]dto.PolicyOptimizerBundleFile, 0, len(manifest.Files))
	for _, file := range manifest.Files {
		files = append(files, dto.PolicyOptimizerBundleFile{
			Path: file.Path, Size: file.Size, SHA256: file.SHA256,
		})
	}
	return dto.PolicyOptimizerBundleManifest{
		ReleaseVersion: policy.ReleaseVersion, SourcePolicyVersion: policy.SourcePolicyVersion,
		CompilerVersion: policy.CompilerVersion, AggregateHash: policy.AggregateHash,
		Files: files, Coverage: policyOptimizerCloneCoverage(coverage),
	}, nil
}

// copyPolicyOptimizerBundle 复制基础发布包到新的候选目录。
func copyPolicyOptimizerBundle(baseDir, candidateDir string) error {
	if _, err := os.Stat(candidateDir); err == nil {
		return fmt.Errorf("policy optimizer candidate directory already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect policy optimizer candidate directory: %w", err)
	}
	return filepath.WalkDir(baseDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("policy optimizer bundle symlink is forbidden: %s", path)
		}
		relative, err := filepath.Rel(baseDir, path)
		if err != nil {
			return fmt.Errorf("resolve base bundle path: %w", err)
		}
		target := filepath.Join(candidateDir, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read base bundle file %s: %w", relative, err)
		}
		return writePolicyOptimizerAtomic(target, data)
	})
}

// applyPolicyOptimizerOperation 执行一个已冻结的候选补丁操作。
func applyPolicyOptimizerOperation(candidateDir string, operation dto.PolicyOptimizerChangeOperation) error {
	switch operation.Operation {
	case "add_yaml_field", "replace_yaml_field", "remove_yaml_field":
		return applyPolicyOptimizerYAMLField(candidateDir, operation)
	case "add_versioned_card":
		return addPolicyOptimizerVersionedCard(candidateDir, operation)
	case "add_decision":
		return addPolicyOptimizerTextAsset(candidateDir, "policy/decisions", ".md", operation)
	case "add_regression_contract":
		return addPolicyOptimizerTextAsset(candidateDir, "regression/contracts", ".yaml", operation)
	default:
		return fmt.Errorf("policy optimizer change operation %q is invalid", operation.Operation)
	}
}

// applyPolicyOptimizerYAMLField 按 RFC6901 pointer 修改目标 YAML 文件。
func applyPolicyOptimizerYAMLField(candidateDir string, operation dto.PolicyOptimizerChangeOperation) error {
	targetPath, err := securePolicyOptimizerRelativePath(operation.TargetPath)
	if err != nil {
		return err
	}
	path := filepath.Join(candidateDir, filepath.FromSlash(targetPath))
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read policy optimizer patch target: %w", err)
	}
	root := yaml.Node{}
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return fmt.Errorf("parse policy optimizer patch target: %w", err)
	}
	if len(root.Content) == 0 {
		return fmt.Errorf("policy optimizer patch target is empty")
	}
	segments, err := policyOptimizerJSONPointer(operation.Pointer)
	if err != nil {
		return err
	}
	if err := mutatePolicyOptimizerYAMLNode(root.Content[0], segments, operation); err != nil {
		return err
	}
	encoded, err := yaml.Marshal(&root)
	if err != nil {
		return fmt.Errorf("encode policy optimizer patch target: %w", err)
	}
	return writePolicyOptimizerAtomic(path, encoded)
}

// addPolicyOptimizerVersionedCard 写入新的版本化规则卡。
func addPolicyOptimizerVersionedCard(candidateDir string, operation dto.PolicyOptimizerChangeOperation) error {
	value, ok := operation.Value.(map[string]any)
	if !ok {
		return fmt.Errorf("policy optimizer versioned card value must be an object")
	}
	id, _ := value["id"].(string)
	axis, _ := value["axis"].(string)
	if !policyOptimizerCompileIDPattern.MatchString(id) || id != operation.Target ||
		(axis != "attack_method" && axis != "attack_domain") {
		return fmt.Errorf("policy optimizer versioned card identity is invalid")
	}
	root := filepath.Join(candidateDir, "policy", "rules")
	duplicate, err := policyOptimizerCardIDExists(root, id)
	if err != nil {
		return err
	}
	if duplicate {
		return fmt.Errorf("policy optimizer versioned card id is duplicate: %s", id)
	}
	raw, err := yaml.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode policy optimizer versioned card: %w", err)
	}
	path := filepath.Join(root, axis, id+".yaml")
	if relative, err := filepath.Rel(root, path); err != nil || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("policy optimizer versioned card path escapes rules root")
	}
	return writePolicyOptimizerAtomic(path, raw)
}

// addPolicyOptimizerTextAsset 写入新增 decision 或 regression contract。
func addPolicyOptimizerTextAsset(
	candidateDir string,
	root string,
	extension string,
	operation dto.PolicyOptimizerChangeOperation,
) error {
	if strings.TrimSpace(operation.Target) == "" {
		return fmt.Errorf("policy optimizer change target is required")
	}
	if _, err := securePolicyOptimizerRelativePath(operation.Target); err != nil {
		return err
	}
	path := filepath.Join(candidateDir, filepath.FromSlash(root), operation.Target+extension)
	raw, err := yaml.Marshal(operation.Value)
	if err != nil {
		return fmt.Errorf("encode policy optimizer change asset: %w", err)
	}
	return writePolicyOptimizerAtomic(path, raw)
}

// mutatePolicyOptimizerYAMLNode 按 pointer 在 YAML 节点上执行 add/replace/remove。
func mutatePolicyOptimizerYAMLNode(
	node *yaml.Node,
	segments []string,
	operation dto.PolicyOptimizerChangeOperation,
) error {
	if len(segments) == 0 {
		return fmt.Errorf("policy optimizer change pointer targets document root")
	}
	current := node
	for _, segment := range segments[:len(segments)-1] {
		next, err := policyOptimizerChildNode(current, segment)
		if err != nil {
			return err
		}
		current = next
	}
	last := segments[len(segments)-1]
	switch current.Kind {
	case yaml.MappingNode:
		return mutatePolicyOptimizerYAMLMap(current, last, operation)
	case yaml.SequenceNode:
		return mutatePolicyOptimizerYAMLSequence(current, last, operation)
	default:
		return fmt.Errorf("policy optimizer change pointer parent is not a map or list")
	}
}

// policyOptimizerChildNode 返回 pointer 中间段对应的子节点。
func policyOptimizerChildNode(node *yaml.Node, segment string) (*yaml.Node, error) {
	switch node.Kind {
	case yaml.MappingNode:
		for index := 0; index+1 < len(node.Content); index += 2 {
			if node.Content[index].Value == segment {
				return node.Content[index+1], nil
			}
		}
	case yaml.SequenceNode:
		index, err := strconv.Atoi(segment)
		if err != nil || index < 0 || index >= len(node.Content) {
			return nil, fmt.Errorf("policy optimizer change list pointer is invalid")
		}
		return node.Content[index], nil
	}
	return nil, fmt.Errorf("policy optimizer change pointer segment %q was not found", segment)
}

// mutatePolicyOptimizerYAMLMap 修改 mapping 的最后一个 pointer 段。
func mutatePolicyOptimizerYAMLMap(
	node *yaml.Node,
	key string,
	operation dto.PolicyOptimizerChangeOperation,
) error {
	keyIndex := -1
	for index := 0; index+1 < len(node.Content); index += 2 {
		if node.Content[index].Value == key {
			keyIndex = index
			break
		}
	}
	if operation.Operation == "add_yaml_field" && keyIndex >= 0 {
		return fmt.Errorf("policy optimizer add field already exists")
	}
	if operation.Operation != "add_yaml_field" && keyIndex < 0 {
		return fmt.Errorf("policy optimizer change field was not found")
	}
	if operation.Operation == "remove_yaml_field" {
		node.Content = append(node.Content[:keyIndex], node.Content[keyIndex+2:]...)
		return nil
	}
	value, err := policyOptimizerYAMLValueNode(operation.Value)
	if err != nil {
		return err
	}
	if keyIndex >= 0 {
		node.Content[keyIndex+1] = value
		return nil
	}
	node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, value)
	return nil
}

// mutatePolicyOptimizerYAMLSequence 修改 list 的最后一个 pointer 段。
func mutatePolicyOptimizerYAMLSequence(
	node *yaml.Node,
	segment string,
	operation dto.PolicyOptimizerChangeOperation,
) error {
	index, err := strconv.Atoi(segment)
	if err != nil || index < 0 {
		return fmt.Errorf("policy optimizer change list pointer is invalid")
	}
	if operation.Operation == "add_yaml_field" {
		if index > len(node.Content) {
			return fmt.Errorf("policy optimizer change list pointer is out of range")
		}
		value, err := policyOptimizerYAMLValueNode(operation.Value)
		if err != nil {
			return err
		}
		node.Content = append(node.Content, nil)
		copy(node.Content[index+1:], node.Content[index:])
		node.Content[index] = value
		return nil
	}
	if index >= len(node.Content) {
		return fmt.Errorf("policy optimizer change list pointer is out of range")
	}
	if operation.Operation == "remove_yaml_field" {
		node.Content = append(node.Content[:index], node.Content[index+1:]...)
		return nil
	}
	value, err := policyOptimizerYAMLValueNode(operation.Value)
	if err != nil {
		return err
	}
	node.Content[index] = value
	return nil
}

// policyOptimizerYAMLValueNode 将 JSON/YAML 值转换为 YAML 节点。
func policyOptimizerYAMLValueNode(value any) (*yaml.Node, error) {
	node := &yaml.Node{}
	if err := node.Encode(value); err != nil {
		return nil, fmt.Errorf("encode policy optimizer change value: %w", err)
	}
	return node, nil
}

// policyOptimizerJSONPointer 解析并反转义 RFC6901 pointer。
func policyOptimizerJSONPointer(pointer string) ([]string, error) {
	if pointer == "" || !strings.HasPrefix(pointer, "/") {
		return nil, fmt.Errorf("policy optimizer change pointer is invalid")
	}
	parts := strings.Split(pointer[1:], "/")
	for index, part := range parts {
		part = strings.ReplaceAll(part, "~1", "/")
		part = strings.ReplaceAll(part, "~0", "~")
		if part == "" {
			return nil, fmt.Errorf("policy optimizer change pointer segment is empty")
		}
		parts[index] = part
	}
	return parts, nil
}

// securePolicyOptimizerRelativePath 拒绝绝对路径和目录逃逸。
func securePolicyOptimizerRelativePath(path string) (string, error) {
	if path == "" || filepath.IsAbs(path) {
		return "", fmt.Errorf("policy optimizer target path is invalid")
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("policy optimizer target path escapes candidate")
	}
	return clean, nil
}

// policyOptimizerCardIDExists 检查规则卡 ID 是否已经存在。
func policyOptimizerCardIDExists(root, id string) (bool, error) {
	found := false
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || found {
			return walkErr
		}
		if filepath.Ext(path) != ".yaml" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		card := SafetyReviewRuleCard{}
		if err := decodeSafetyReviewYAML(raw, &card); err != nil {
			return err
		}
		if card.ID == id {
			found = true
		}
		return nil
	})
	return found, err
}

// updatePolicyOptimizerReleaseVersion 更新候选 release 版本，等待编译阶段重签 manifest。
func updatePolicyOptimizerReleaseVersion(candidateDir, version string) error {
	path := filepath.Join(candidateDir, "release.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read candidate release manifest: %w", err)
	}
	root := yaml.Node{}
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return fmt.Errorf("parse candidate release manifest: %w", err)
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("candidate release manifest is invalid")
	}
	for index := 0; index+1 < len(root.Content[0].Content); index += 2 {
		key := root.Content[0].Content[index]
		if key.Value == "release_version" {
			root.Content[0].Content[index+1].Value = version
			encoded, err := yaml.Marshal(&root)
			if err != nil {
				return fmt.Errorf("encode candidate release manifest: %w", err)
			}
			return writePolicyOptimizerAtomic(path, encoded)
		}
	}
	return fmt.Errorf("candidate release manifest lacks release_version")
}

// hashPolicyOptimizerDirectory 计算目录内所有文件的稳定聚合哈希。
func hashPolicyOptimizerDirectory(dir string) (string, error) {
	files, _, err := policyOptimizerDirectoryFiles(dir)
	if err != nil {
		return "", err
	}
	hashInput := make([]byte, 0)
	for _, file := range files {
		raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(file.Path)))
		if err != nil {
			return "", fmt.Errorf("read policy optimizer bundle file: %w", err)
		}
		hashInput = append(hashInput, file.Path...)
		hashInput = append(hashInput, 0)
		hashInput = append(hashInput, raw...)
	}
	digest := sha256.Sum256(hashInput)
	return hex.EncodeToString(digest[:]), nil
}

// policyOptimizerDirectoryFiles 返回目录文件的稳定清单和聚合哈希。
func policyOptimizerDirectoryFiles(dir string) ([]dto.PolicyOptimizerBundleFile, string, error) {
	return policyOptimizerDirectoryFilesExcluding(dir, nil)
}

// policyOptimizerDirectoryFilesExcluding 返回目录文件清单，排除指定相对路径。
func policyOptimizerDirectoryFilesExcluding(
	dir string,
	excluded map[string]bool,
) ([]dto.PolicyOptimizerBundleFile, string, error) {
	files := make([]dto.PolicyOptimizerBundleFile, 0)
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("policy optimizer bundle symlink is forbidden: %s", path)
		}
		relative, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == "release.yaml" {
			return nil
		}
		if excluded[relative] {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(raw)
		files = append(files, dto.PolicyOptimizerBundleFile{
			Path: relative, Size: int64(len(raw)), SHA256: hex.EncodeToString(digest[:]),
		})
		return nil
	})
	if err != nil {
		return nil, "", fmt.Errorf("walk policy optimizer bundle: %w", err)
	}
	sort.Slice(files, func(left, right int) bool { return files[left].Path < files[right].Path })
	hashInput := make([]byte, 0)
	for _, file := range files {
		raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(file.Path)))
		if err != nil {
			return nil, "", err
		}
		hashInput = append(hashInput, file.Path...)
		hashInput = append(hashInput, 0)
		hashInput = append(hashInput, raw...)
	}
	digest := sha256.Sum256(hashInput)
	return files, hex.EncodeToString(digest[:]), nil
}

// writePolicyOptimizerReleaseManifest 重写 release.yaml 的版本、文件哈希和聚合哈希。
func writePolicyOptimizerReleaseManifest(candidateDir, compilerVersion, _ string) error {
	path := filepath.Join(candidateDir, "release.yaml")
	existing, err := loadSafetyReviewManifest(path)
	if err != nil {
		return err
	}
	files, aggregate, err := policyOptimizerDirectoryFilesExcluding(
		candidateDir,
		map[string]bool{"compile-manifest.json": true},
	)
	if err != nil {
		return err
	}
	manifest := safetyReviewReleaseManifest{
		ReleaseVersion: existing.ReleaseVersion, SourcePolicyVersion: existing.SourcePolicyVersion,
		CompilerVersion: compilerVersion, AggregateHash: aggregate,
		Files: make([]safetyReviewManifestFile, 0, len(files)),
	}
	for _, file := range files {
		manifest.Files = append(manifest.Files, safetyReviewManifestFile{
			Path: file.Path, Size: file.Size, SHA256: file.SHA256,
		})
	}
	raw, err := yaml.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("encode policy optimizer release manifest: %w", err)
	}
	return writePolicyOptimizerAtomic(path, raw)
}

// writePolicyOptimizerCompileManifest 写入 compile-manifest.json。
func writePolicyOptimizerCompileManifest(
	candidateDir string,
	cfg PolicyOptimizerCompileConfig,
	coverage map[string][]string,
) (dto.PolicyOptimizerCompileManifest, error) {
	release, err := loadSafetyReviewManifest(filepath.Join(candidateDir, "release.yaml"))
	if err != nil {
		return dto.PolicyOptimizerCompileManifest{}, err
	}
	files, aggregate, err := policyOptimizerDirectoryFiles(candidateDir)
	if err != nil {
		return dto.PolicyOptimizerCompileManifest{}, err
	}
	manifest := dto.PolicyOptimizerCompileManifest{
		ReleaseVersion: release.ReleaseVersion, SourcePolicyVersion: release.SourcePolicyVersion,
		CompilerVersion: cfg.CompilerVersion, AggregateHash: aggregate,
		Files: files, Coverage: policyOptimizerCloneCoverage(coverage),
	}
	raw, err := marshalPolicyOptimizerCanonicalJSON(manifest)
	if err != nil {
		return dto.PolicyOptimizerCompileManifest{}, err
	}
	if err := writePolicyOptimizerAtomic(filepath.Join(candidateDir, "compile-manifest.json"), raw); err != nil {
		return dto.PolicyOptimizerCompileManifest{}, err
	}
	return manifest, nil
}

// policyOptimizerCompileInputs 表示 Prompt 编译所需的候选资产。
type policyOptimizerCompileInputs struct {
	common    SafetyReviewCommonPolicy
	decisions string
	cards     map[string]*SafetyReviewRuleCard
	examples  []SafetyReviewExample
	prompts   map[string]string
	schemas   map[string]json.RawMessage
}

// loadPolicyOptimizerCompileInputs 从候选目录读取编译资产并检查基本结构。
func loadPolicyOptimizerCompileInputs(candidateDir string) (policyOptimizerCompileInputs, error) {
	inputs := policyOptimizerCompileInputs{
		cards: make(map[string]*SafetyReviewRuleCard), prompts: make(map[string]string),
		schemas: make(map[string]json.RawMessage),
	}
	commonRaw, err := os.ReadFile(filepath.Join(candidateDir, "policy", "common.yaml"))
	if err != nil {
		return inputs, fmt.Errorf("read policy optimizer common policy: %w", err)
	}
	if err := decodeSafetyReviewYAML(commonRaw, &inputs.common); err != nil {
		return inputs, fmt.Errorf("decode policy optimizer common policy: %w", err)
	}
	decisionRaw, err := os.ReadFile(filepath.Join(candidateDir, "policy", "decisions", "P04-B.md"))
	if err != nil {
		return inputs, fmt.Errorf("read policy optimizer decisions: %w", err)
	}
	inputs.decisions = string(decisionRaw)
	if err := loadPolicyOptimizerCompileCards(candidateDir, &inputs); err != nil {
		return inputs, err
	}
	if err := loadPolicyOptimizerCompileExamples(candidateDir, &inputs); err != nil {
		return inputs, err
	}
	for _, role := range policyOptimizerPromptRoles() {
		raw, err := os.ReadFile(filepath.Join(candidateDir, "prompts", role+".txt"))
		if err != nil {
			return inputs, fmt.Errorf("read policy optimizer prompt %s: %w", role, err)
		}
		inputs.prompts[role] = string(raw)
	}
	for _, name := range []string{"judgment", "router", "expert", "arbiter"} {
		raw, err := os.ReadFile(filepath.Join(candidateDir, "schemas", name+".json"))
		if err != nil {
			return inputs, fmt.Errorf("read policy optimizer schema %s: %w", name, err)
		}
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return inputs, fmt.Errorf("decode policy optimizer schema %s: %w", name, err)
		}
		inputs.schemas[name] = append(json.RawMessage(nil), raw...)
	}
	return inputs, nil
}

// loadPolicyOptimizerCompileCards 读取并检查全部规则卡。
func loadPolicyOptimizerCompileCards(candidateDir string, inputs *policyOptimizerCompileInputs) error {
	return filepath.WalkDir(filepath.Join(candidateDir, "policy", "rules"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		if filepath.Ext(path) != ".yaml" {
			return fmt.Errorf("unknown policy optimizer card asset: %s", path)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		card := SafetyReviewRuleCard{}
		if err := decodeSafetyReviewYAML(raw, &card); err != nil {
			return err
		}
		if card.ID == "" || inputs.cards[card.ID] != nil {
			return fmt.Errorf("policy optimizer card id is empty or duplicate: %s", card.ID)
		}
		relative, err := filepath.Rel(candidateDir, path)
		if err != nil {
			return err
		}
		card.SourcePath = filepath.ToSlash(relative)
		inputs.cards[card.ID] = &card
		return nil
	})
}

// loadPolicyOptimizerCompileExamples 读取可见开发样例并按 sample_id 稳定排序。
func loadPolicyOptimizerCompileExamples(candidateDir string, inputs *policyOptimizerCompileInputs) error {
	raw, err := os.ReadFile(filepath.Join(candidateDir, "policy", "examples", "p04b-development.jsonl"))
	if err != nil {
		return fmt.Errorf("read policy optimizer examples: %w", err)
	}
	lines := bytes.Split(bytes.TrimSuffix(raw, []byte("\n")), []byte("\n"))
	for index, line := range lines {
		example, err := decodeSafetyReviewExampleLine(line)
		if err != nil {
			return fmt.Errorf("decode policy optimizer example %d: %w", index+1, err)
		}
		inputs.examples = append(inputs.examples, example)
	}
	sort.Slice(inputs.examples, func(left, right int) bool {
		return inputs.examples[left].SampleID < inputs.examples[right].SampleID
	})
	return nil
}

// buildPolicyOptimizerCoverage 建立所有启用策略 ID 到角色的确定性覆盖映射。
func buildPolicyOptimizerCoverage(
	common SafetyReviewCommonPolicy,
	cards map[string]*SafetyReviewRuleCard,
	decisions string,
) (map[string][]string, error) {
	coverage := map[string][]string{
		"judge_a": {}, "judge_b": {}, "router": {}, "expert": {}, "arbiter": {},
	}
	commonIDs := strings.Split(common.RuleIDs, ",")
	for index, id := range commonIDs {
		commonIDs[index] = strings.TrimSpace(id)
	}
	decisionIDs := policyOptimizerDecisionIDs(decisions)
	commonIDs = append(commonIDs, decisionIDs...)
	for _, id := range sortDedupStrings(commonIDs) {
		if id == "" {
			return nil, fmt.Errorf("policy optimizer common rule id is empty")
		}
		coverage["judge_a"] = append(coverage["judge_a"], id)
		coverage["judge_b"] = append(coverage["judge_b"], id)
		coverage["arbiter"] = append(coverage["arbiter"], id)
	}
	cardIDs := make([]string, 0, len(cards))
	for id := range cards {
		cardIDs = append(cardIDs, id)
	}
	sort.Strings(cardIDs)
	for _, id := range cardIDs {
		card := cards[id]
		if card == nil {
			return nil, fmt.Errorf("policy optimizer card is nil: %s", id)
		}
		coverage["router"] = append(coverage["router"], id)
		coverage["arbiter"] = append(coverage["arbiter"], id)
		for _, condition := range card.RequiredConditions {
			coverage["expert"] = append(coverage["expert"], condition.ID)
			coverage["arbiter"] = append(coverage["arbiter"], condition.ID)
		}
		for _, exclusion := range card.DecisiveExclusions {
			coverage["expert"] = append(coverage["expert"], exclusion.ID)
			coverage["arbiter"] = append(coverage["arbiter"], exclusion.ID)
		}
	}
	for role, ids := range coverage {
		coverage[role] = sortDedupStrings(ids)
	}
	return coverage, nil
}

// policyOptimizerDecisionIDs 提取已批准 decision ID。
func policyOptimizerDecisionIDs(decisions string) []string {
	fields := strings.FieldsFunc(decisions, func(r rune) bool {
		return !(r == '-' || r == '_' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')
	})
	result := make([]string, 0)
	for _, field := range fields {
		if strings.HasPrefix(field, "P04B-DECISION-") {
			result = append(result, field)
		}
	}
	return sortDedupStrings(result)
}

// renderPolicyOptimizerPrompt 按冻结 section 顺序渲染单个角色 Prompt。
func renderPolicyOptimizerPrompt(
	role string,
	inputs policyOptimizerCompileInputs,
	coverage map[string][]string,
	maxBytes int,
) (string, error) {
	template := policyOptimizerRoleTemplate(inputs.prompts[role])
	if strings.TrimSpace(template) == "" {
		return "", fmt.Errorf("policy optimizer prompt template is empty: %s", role)
	}
	if role == "refusal-reprompt" {
		return template, nil
	}
	roleKey := strings.ReplaceAll(role, "-", "_")
	if role == "judge-a" {
		roleKey = "judge_a"
	}
	if role == "judge-b" {
		roleKey = "judge_b"
	}
	requiredCoverage, ok := coverage[roleKey]
	if !ok {
		return "", fmt.Errorf("policy optimizer role coverage is missing: %s", role)
	}
	commonRaw, err := yaml.Marshal(inputs.common)
	if err != nil {
		return "", err
	}
	examplesRaw, err := marshalPolicyOptimizerExamples(inputs.examples)
	if err != nil {
		return "", err
	}
	schemaName := policyOptimizerRoleSchema(role)
	schema := inputs.schemas[schemaName]
	if len(schema) == 0 {
		return "", fmt.Errorf("policy optimizer role schema is missing: %s", role)
	}
	index, err := policyOptimizerRuleIndex(inputs.cards)
	if err != nil {
		return "", err
	}
	sections := []string{
		"[ROLE]\n" + template,
		"[AUTHORITY_BOUNDARY]\nOnly deterministic policy assets may establish policy semantics. Models may not approve or release.",
		"[SCENE_AND_EVIDENCE_OWNERSHIP]\nUse only the evaluated scene and declared evidence ownership.",
		"[COMMON_POLICY]\n" + string(commonRaw),
		"[ENABLED_RULE_INDEX]\n" + strings.Join(requiredCoverage, "\n") + "\n" + index,
		"[ROLE_SPECIFIC_RULES]\n" + policyOptimizerRoleRules(role),
		"[CASE_TYPE_POLICY]\n" + inputs.common.CaseTypePolicy,
		"[DECISIONS]\n" + inputs.decisions,
		"[APPROVED_EXAMPLES]\n" + string(examplesRaw),
		"[OUTPUT_SCHEMA]\n" + string(schema),
		"[PROHIBITIONS]\nDo not invent IDs, categories, evidence, approval, release state, or hidden policy text.",
	}
	result := strings.Join(sections, "\n\n")
	if result == "" || result[len(result)-1] != '\n' {
		result += "\n"
	}
	if maxBytes > 0 && len(result) > maxBytes {
		return "", fmt.Errorf("policy optimizer prompt %s exceeds size budget", role)
	}
	return result, nil
}

// policyOptimizerRoleTemplate 从重复编译的 prompt 中恢复原始角色模板。
func policyOptimizerRoleTemplate(prompt string) string {
	const marker = "\n\n[AUTHORITY_BOUNDARY]\n"
	index := strings.Index(prompt, marker)
	if index < 0 {
		return prompt
	}
	return strings.TrimPrefix(prompt[:index], "[ROLE]\n")
}

// policyOptimizerRuleIndex 生成稳定规则卡索引。
func policyOptimizerRuleIndex(cards map[string]*SafetyReviewRuleCard) (string, error) {
	ids := make([]string, 0, len(cards))
	for id := range cards {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	lines := make([]string, 0, len(ids))
	for _, id := range ids {
		lines = append(lines, fmt.Sprintf("%s: %s", id, cards[id].Definition))
	}
	return strings.Join(lines, "\n"), nil
}

// policyOptimizerRoleRules 返回角色专属的非权威渲染说明。
func policyOptimizerRoleRules(role string) string {
	switch role {
	case "judge-a":
		return "Perform risk discovery only and return concise structured evidence."
	case "judge-b":
		return "Apply decisive exclusions and false-positive checks before any risk signal."
	case "router":
		return "Extract observable facts and recall candidates only; do not decide policy."
	case "expert":
		return "Validate every condition and exclusion for exactly one supplied rule card."
	case "arbiter":
		return "Select only established Expert categories; unresolved uncertainty is quarantine."
	default:
		return "Follow the supplied role contract."
	}
}

// policyOptimizerRoleSchema 返回角色使用的输出 Schema 名。
func policyOptimizerRoleSchema(role string) string {
	if role == "expert" {
		return "expert"
	}
	if role == "router" {
		return "router"
	}
	if role == "arbiter" {
		return "arbiter"
	}
	return "judgment"
}

// marshalPolicyOptimizerExamples 将样例按输入顺序编码为 JSONL。
func marshalPolicyOptimizerExamples(examples []SafetyReviewExample) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	for _, example := range examples {
		if err := encoder.Encode(example); err != nil {
			return nil, err
		}
	}
	return buffer.Bytes(), nil
}

// readPolicyOptimizerCoverage 读取可选 coverage-map.json 并严格解码。
func readPolicyOptimizerCoverage(bundleDir string) (map[string][]string, error) {
	path := filepath.Join(bundleDir, "coverage-map.json")
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read policy optimizer coverage map: %w", err)
	}
	coverage := map[string][]string{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&coverage); err != nil {
		return nil, fmt.Errorf("decode policy optimizer coverage map: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode policy optimizer coverage map: trailing data")
	}
	return coverage, nil
}

// readPolicyOptimizerCompileManifest 读取可选 compile-manifest.json 并严格解码。
func readPolicyOptimizerCompileManifest(bundleDir string) (*dto.PolicyOptimizerCompileManifest, error) {
	path := filepath.Join(bundleDir, "compile-manifest.json")
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read policy optimizer compile manifest: %w", err)
	}
	manifest := dto.PolicyOptimizerCompileManifest{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode policy optimizer compile manifest: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode policy optimizer compile manifest: trailing data")
	}
	return &manifest, nil
}

// policyOptimizerCoverageEqual 比较两份 coverage 映射。
func policyOptimizerCoverageEqual(left, right map[string][]string) bool {
	if len(left) != len(right) {
		return false
	}
	for role, leftIDs := range left {
		rightIDs, ok := right[role]
		if !ok || !equalStringSlices(leftIDs, rightIDs) {
			return false
		}
	}
	return true
}

// policyOptimizerCloneCoverage 深拷贝 coverage 映射。
func policyOptimizerCloneCoverage(coverage map[string][]string) map[string][]string {
	result := make(map[string][]string, len(coverage))
	for role, ids := range coverage {
		result[role] = append([]string(nil), ids...)
	}
	return result
}

// marshalPolicyOptimizerCanonicalJSON 输出稳定、无 HTML 转义的 JSON。
func marshalPolicyOptimizerCanonicalJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, fmt.Errorf("encode policy optimizer JSON: %w", err)
	}
	return buffer.Bytes(), nil
}

// policyOptimizerPromptRoles 返回冻结 Prompt 文件名集合。
func policyOptimizerPromptRoles() []string {
	return []string{"judge-a", "judge-b", "router", "expert", "arbiter", "refusal-reprompt"}
}

var policyOptimizerCompileIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
