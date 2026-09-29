// Package service 提供 Safety Review 的冻结 Policy Bundle 加载与校验。
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
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrInvalidSafetyReviewPolicy 表示发布包缺失、被篡改或结构非法。
var ErrInvalidSafetyReviewPolicy = errors.New("service: invalid safety review policy")

// safetyReviewExamplesPath 是开发回归样例在发布包内的固定路径。
const safetyReviewExamplesPath = "policy/examples/p04b-development.jsonl"

// SafetyReviewRuleText 表示规则卡中的条件或排除说明。
type SafetyReviewRuleText struct {
	ID   string `yaml:"id" json:"id"`
	Text string `yaml:"text" json:"text"`
}

// SafetyReviewRuleCard 表示一个冻结的单类别规则卡。
type SafetyReviewRuleCard struct {
	ID                 string                 `yaml:"id" json:"id"`
	Axis               string                 `yaml:"axis" json:"axis"`
	Version            int                    `yaml:"version" json:"version"`
	Title              string                 `yaml:"title" json:"title"`
	EnabledScenes      []string               `yaml:"enabled_scenes" json:"enabled_scenes"`
	PrimaryPriority    int                    `yaml:"primary_priority" json:"primary_priority"`
	Definition         string                 `yaml:"definition" json:"definition"`
	TargetAttributes   []string               `yaml:"target_attributes" json:"target_attributes"`
	RequiredConditions []SafetyReviewRuleText `yaml:"required_conditions" json:"required_conditions"`
	DecisiveExclusions []SafetyReviewRuleText `yaml:"decisive_exclusions" json:"decisive_exclusions"`
	ConfusableWith     []string               `yaml:"confusable_with" json:"confusable_with"`
	ErrorPatterns      []string               `yaml:"error_patterns" json:"error_patterns"`
	ExamplesFile       string                 `yaml:"examples_file" json:"examples_file"`
	SourcePath         string                 `yaml:"-" json:"-"`
}

// SafetyReviewCommonPolicy 表示跨类别共享的人工确认规则和错误模式。
type SafetyReviewCommonPolicy struct {
	Version             int      `yaml:"version"`
	RuleIDs             string   `yaml:"rule_ids"`
	CaseTypePolicy      string   `yaml:"case_type_policy"`
	RefusalPrefixes     []string `yaml:"refusal_prefixes"`
	CrossCategoryErrors []string `yaml:"cross_category_errors"`
}

// SafetyReviewExample 表示可见的开发回归样例。
type SafetyReviewExample struct {
	SampleID  string   `json:"sample_id"`
	Scene     string   `json:"scene"`
	Prompt    string   `json:"prompt"`
	Response  string   `json:"response"`
	GoldLabel string   `json:"gold_label"`
	CaseType  string   `json:"case_type"`
	RiskType  string   `json:"risk_type"`
	RuleIDs   []string `json:"rule_ids"`
	Source    string   `json:"source"`
}

// SafetyReviewPolicy 表示已通过清单校验的不可变发布包。
type SafetyReviewPolicy struct {
	ReleaseVersion      string
	SourcePolicyVersion string
	CompilerVersion     string
	Common              SafetyReviewCommonPolicy
	Decisions           string
	Cards               map[string]*SafetyReviewRuleCard
	Prompts             map[string]string
	Schemas             map[string]json.RawMessage
	Examples            []SafetyReviewExample
	AggregateHash       string
	Coverage            map[string][]string
}

// safetyReviewReleaseManifest 表示 release.yaml 的严格结构。
type safetyReviewReleaseManifest struct {
	ReleaseVersion      string                     `yaml:"release_version"`
	SourcePolicyVersion string                     `yaml:"source_policy_version"`
	CompilerVersion     string                     `yaml:"compiler_version"`
	AggregateHash       string                     `yaml:"aggregate_hash"`
	Files               []safetyReviewManifestFile `yaml:"files"`
}

// safetyReviewManifestFile 表示单个发布资产的路径、大小和哈希。
type safetyReviewManifestFile struct {
	Path   string `yaml:"path"`
	Size   int64  `yaml:"size"`
	SHA256 string `yaml:"sha256"`
}

// LoadSafetyReviewPolicy 验证并加载一个不可变 Safety Review 发布包。
func LoadSafetyReviewPolicy(bundleDir string) (*SafetyReviewPolicy, error) {
	absoluteBundle, err := filepath.Abs(bundleDir)
	if err != nil {
		return nil, invalidSafetyReviewPolicy("resolve bundle path: %v", err)
	}
	if err := validateSafetyReviewPathComponents(absoluteBundle, "release.yaml"); err != nil {
		return nil, err
	}
	manifestPath := filepath.Join(absoluteBundle, "release.yaml")
	manifest, err := loadSafetyReviewManifest(manifestPath)
	if err != nil {
		return nil, err
	}
	contents, aggregate, err := verifySafetyReviewManifest(absoluteBundle, manifest)
	if err != nil {
		return nil, err
	}
	policy := &SafetyReviewPolicy{
		ReleaseVersion:      manifest.ReleaseVersion,
		SourcePolicyVersion: manifest.SourcePolicyVersion,
		CompilerVersion:     manifest.CompilerVersion,
		Cards:               make(map[string]*SafetyReviewRuleCard),
		Prompts:             make(map[string]string),
		Schemas:             make(map[string]json.RawMessage),
		Examples:            make([]SafetyReviewExample, 0),
		AggregateHash:       aggregate,
		Coverage:            make(map[string][]string),
	}
	if err := decodeSafetyReviewYAML(contents["policy/common.yaml"], &policy.Common); err != nil {
		return nil, invalidSafetyReviewPolicy("decode common policy: %v", err)
	}
	decisions, err := fsPath(contents, "policy/decisions/P04-B.md")
	if err != nil {
		return nil, invalidSafetyReviewPolicy("read decisions: %v", err)
	}
	policy.Decisions = string(decisions)
	if err := loadSafetyReviewCards(policy, contents); err != nil {
		return nil, err
	}
	if err := loadSafetyReviewPrompts(policy, contents); err != nil {
		return nil, err
	}
	if err := loadSafetyReviewSchemas(policy, contents); err != nil {
		return nil, err
	}
	if err := loadSafetyReviewExamples(policy, contents); err != nil {
		return nil, err
	}
	if err := validateSafetyReviewPolicy(policy); err != nil {
		return nil, err
	}
	buildSafetyReviewCoverage(policy)
	return policy, nil
}

// loadSafetyReviewManifest 严格读取发布清单。
func loadSafetyReviewManifest(path string) (*safetyReviewReleaseManifest, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, invalidSafetyReviewPolicy("read release manifest: %v", err)
	}
	manifest := safetyReviewReleaseManifest{}
	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	decoder.KnownFields(true)
	if err := decoder.Decode(&manifest); err != nil {
		return nil, invalidSafetyReviewPolicy("parse release manifest: %v", err)
	}
	if err := ensureSingleYAMLDocument(decoder); err != nil {
		return nil, invalidSafetyReviewPolicy("parse release manifest: %v", err)
	}
	return &manifest, nil
}

// verifySafetyReviewManifest 校验路径、大小、哈希、重复项和未列必需资产。
func verifySafetyReviewManifest(
	bundleDir string,
	manifest *safetyReviewReleaseManifest,
) (map[string][]byte, string, error) {
	if manifest.ReleaseVersion == "" || manifest.SourcePolicyVersion == "" ||
		manifest.CompilerVersion == "" || manifest.AggregateHash == "" || len(manifest.Files) == 0 {
		return nil, "", invalidSafetyReviewPolicy("release identity and files are required")
	}
	seen := make(map[string]bool, len(manifest.Files))
	contents := make(map[string][]byte, len(manifest.Files))
	hashInput := make([]byte, 0)
	paths := make([]string, 0, len(manifest.Files))
	for _, file := range manifest.Files {
		if file.Path == "" || filepath.IsAbs(file.Path) {
			return nil, "", invalidSafetyReviewPolicy("manifest path must be relative: %q", file.Path)
		}
		clean := filepath.Clean(file.Path)
		if clean != file.Path || clean == "." || clean == ".." ||
			strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return nil, "", invalidSafetyReviewPolicy("manifest path escapes bundle: %q", file.Path)
		}
		if seen[clean] {
			return nil, "", invalidSafetyReviewPolicy("duplicate manifest path: %q", clean)
		}
		seen[clean] = true
		fullPath := filepath.Join(bundleDir, clean)
		if err := validateSafetyReviewPathComponents(bundleDir, clean); err != nil {
			return nil, "", err
		}
		info, err := os.Lstat(fullPath)
		if err != nil {
			return nil, "", invalidSafetyReviewPolicy("inspect manifest asset %s: %v", clean, err)
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return nil, "", invalidSafetyReviewPolicy("manifest asset is a symlink: %s", clean)
		}
		if !safetyReviewPathInside(bundleDir, fullPath) {
			return nil, "", invalidSafetyReviewPolicy("manifest path escapes bundle: %q", clean)
		}
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return nil, "", invalidSafetyReviewPolicy("read manifest asset %s: %v", clean, err)
		}
		if int64(len(data)) != file.Size {
			return nil, "", invalidSafetyReviewPolicy("manifest size mismatch: %s", clean)
		}
		digest := sha256.Sum256(data)
		if hex.EncodeToString(digest[:]) != file.SHA256 {
			return nil, "", invalidSafetyReviewPolicy("manifest hash mismatch: %s", clean)
		}
		contents[clean] = data
		paths = append(paths, clean)
	}
	sort.Strings(paths)
	for _, path := range paths {
		hashInput = append(hashInput, path...)
		hashInput = append(hashInput, 0)
		hashInput = append(hashInput, contents[path]...)
	}
	aggregateDigest := sha256.Sum256(hashInput)
	aggregate := hex.EncodeToString(aggregateDigest[:])
	if aggregate != manifest.AggregateHash {
		return nil, "", invalidSafetyReviewPolicy("aggregate bundle hash mismatch")
	}
	if err := rejectUnlistedSafetyReviewAssets(bundleDir, seen); err != nil {
		return nil, "", err
	}
	return contents, aggregate, nil
}

// validateSafetyReviewPathComponents 从 bundle 根开始逐级拒绝符号链接组件。
func validateSafetyReviewPathComponents(bundleDir, relativePath string) error {
	current := bundleDir
	info, err := os.Lstat(current)
	if err != nil {
		return invalidSafetyReviewPolicy("inspect bundle root: %v", err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return invalidSafetyReviewPolicy("bundle root is a symlink")
	}
	for _, component := range strings.Split(filepath.ToSlash(relativePath), "/") {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return invalidSafetyReviewPolicy("inspect bundle path component %s: %v", component, err)
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return invalidSafetyReviewPolicy("bundle path component is a symlink: %s", component)
		}
	}
	return nil
}

// rejectUnlistedSafetyReviewAssets 拒绝必需目录中的未列文件。
func rejectUnlistedSafetyReviewAssets(bundleDir string, listed map[string]bool) error {
	requiredRoots := []string{"policy/rules", "prompts", "schemas"}
	for _, root := range requiredRoots {
		fullRoot := filepath.Join(bundleDir, filepath.FromSlash(root))
		err := filepath.WalkDir(fullRoot, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil || entry.IsDir() {
				return walkErr
			}
			if entry.Type()&fs.ModeSymlink != 0 {
				return fmt.Errorf("bundle symlink is forbidden: %s", path)
			}
			relative, err := filepath.Rel(bundleDir, path)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			if !listed[relative] {
				return fmt.Errorf("unlisted required asset: %s", relative)
			}
			return nil
		})
		if err != nil {
			return invalidSafetyReviewPolicy("%v", err)
		}
	}
	return nil
}

// loadSafetyReviewCards 严格读取全部规则卡并建立 ID 索引。
func loadSafetyReviewCards(policy *SafetyReviewPolicy, contents map[string][]byte) error {
	for path, data := range contents {
		if !strings.HasPrefix(path, "policy/rules/") || !strings.HasSuffix(path, ".yaml") {
			continue
		}
		card := SafetyReviewRuleCard{}
		if err := decodeSafetyReviewYAML(data, &card); err != nil {
			return invalidSafetyReviewPolicy("decode %s: %v", path, err)
		}
		if card.ID == "" || policy.Cards[card.ID] != nil {
			return invalidSafetyReviewPolicy("card id is empty or duplicate: %s", path)
		}
		card.EnabledScenes = append([]string(nil), card.EnabledScenes...)
		card.TargetAttributes = append([]string(nil), card.TargetAttributes...)
		card.ConfusableWith = append([]string(nil), card.ConfusableWith...)
		card.ErrorPatterns = append([]string(nil), card.ErrorPatterns...)
		card.RequiredConditions = append([]SafetyReviewRuleText(nil), card.RequiredConditions...)
		card.DecisiveExclusions = append([]SafetyReviewRuleText(nil), card.DecisiveExclusions...)
		card.SourcePath = path
		policy.Cards[card.ID] = &card
	}
	return nil
}

// loadSafetyReviewPrompts 读取固定角色提示词。
func loadSafetyReviewPrompts(policy *SafetyReviewPolicy, contents map[string][]byte) error {
	for _, name := range []string{"judge-a", "judge-b", "router", "expert", "arbiter", "refusal-reprompt"} {
		path := "prompts/" + name + ".txt"
		data, err := fsPath(contents, path)
		if err != nil {
			return invalidSafetyReviewPolicy("read prompt %s: %v", name, err)
		}
		policy.Prompts[strings.ReplaceAll(name, "-", "_")] = string(data)
	}
	return nil
}

// loadSafetyReviewSchemas 读取并校验四个正式 JSON Schema。
func loadSafetyReviewSchemas(policy *SafetyReviewPolicy, contents map[string][]byte) error {
	for _, name := range []string{"judgment", "router", "expert", "arbiter"} {
		path := "schemas/" + name + ".json"
		data, err := fsPath(contents, path)
		if err != nil {
			return invalidSafetyReviewPolicy("read schema %s: %v", name, err)
		}
		var decoded any
		if err := json.Unmarshal(data, &decoded); err != nil {
			return invalidSafetyReviewPolicy("decode schema %s: %v", name, err)
		}
		if strings.Contains(string(data), "risk_level") {
			return invalidSafetyReviewPolicy("schema %s contains forbidden risk_level", name)
		}
		policy.Schemas[name] = append(json.RawMessage(nil), data...)
	}
	return nil
}

// loadSafetyReviewExamples 读取开发回归 JSONL。
func loadSafetyReviewExamples(policy *SafetyReviewPolicy, contents map[string][]byte) error {
	data, err := fsPath(contents, safetyReviewExamplesPath)
	if err != nil {
		return invalidSafetyReviewPolicy("read examples: %v", err)
	}
	lines := bytes.Split(data, []byte("\n"))
	if len(data) == 0 || data[len(data)-1] != '\n' || bytes.HasSuffix(data, []byte("\n\n")) {
		return invalidSafetyReviewPolicy("development examples must end with exactly one newline")
	}
	if len(lines) != 0 && len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1]
	}
	seenSampleIDs := make(map[string]bool)
	for index, line := range lines {
		example, err := decodeSafetyReviewExampleLine(line)
		if err != nil {
			return invalidSafetyReviewPolicy("decode example line %d: %v", index+1, err)
		}
		if seenSampleIDs[example.SampleID] {
			return invalidSafetyReviewPolicy("duplicate sample_id: %s", example.SampleID)
		}
		seenSampleIDs[example.SampleID] = true
		if err := validateSafetyReviewExample(policy, example); err != nil {
			return invalidSafetyReviewPolicy("invalid example %s: %w", example.SampleID, err)
		}
		example.RuleIDs = append([]string(nil), example.RuleIDs...)
		policy.Examples = append(policy.Examples, example)
	}
	if len(policy.Examples) == 0 {
		return invalidSafetyReviewPolicy("development examples are empty")
	}
	return nil
}

// decodeSafetyReviewExampleLine 严格解码单行 JSON 对象。
func decodeSafetyReviewExampleLine(line []byte) (SafetyReviewExample, error) {
	if len(bytes.TrimSpace(line)) == 0 {
		return SafetyReviewExample{}, errors.New("empty line")
	}
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	example := SafetyReviewExample{}
	if err := decoder.Decode(&example); err != nil {
		return SafetyReviewExample{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return SafetyReviewExample{}, errors.New("multiple JSON objects on one line")
		}
		return SafetyReviewExample{}, err
	}
	return example, nil
}

// validateSafetyReviewExample 校验开发样例的闭集和标签不变量。
func validateSafetyReviewExample(policy *SafetyReviewPolicy, example SafetyReviewExample) error {
	if example.SampleID == "" || example.Prompt == "" {
		return errors.New("sample_id and prompt are required")
	}
	if example.Scene != "prompt" && example.Scene != "response" {
		return errors.New("scene is invalid")
	}
	if example.Scene == "response" && example.Response == "" {
		return errors.New("response example requires response text")
	}
	if example.Source != "human_reviewed" && example.Source != "synthetic" {
		return errors.New("source is invalid")
	}
	if example.GoldLabel != "safe" && example.GoldLabel != "unsafe" {
		return errors.New("gold_label is invalid")
	}
	switch example.CaseType {
	case "typical", "borderline", "variant", "hard_negative":
	default:
		return errors.New("case_type is invalid")
	}
	if example.GoldLabel == "safe" && example.RiskType != "" {
		return errors.New("safe example must not have risk_type")
	}
	if example.GoldLabel == "unsafe" && policy.Cards[example.RiskType] == nil {
		return errors.New("unsafe example has unknown risk_type")
	}
	if example.GoldLabel == "unsafe" {
		card := policy.Cards[example.RiskType]
		if !sliceContainsString(card.EnabledScenes, example.Scene) {
			return errors.New("unsafe example risk_type is invalid for the scene")
		}
		if example.Scene == "response" && card.Axis != "attack_domain" {
			return errors.New("unsafe response example cannot use attack_method risk_type")
		}
	}
	if example.CaseType == "hard_negative" && example.GoldLabel != "safe" {
		return errors.New("hard_negative example must be safe")
	}
	if example.CaseType == "variant" && example.GoldLabel != "unsafe" {
		return errors.New("variant example must be unsafe")
	}
	if len(example.RuleIDs) == 0 {
		return errors.New("rule_ids must not be empty")
	}
	seenRuleIDs := make(map[string]bool, len(example.RuleIDs))
	knownRules := safetyReviewKnownRuleIDs(policy)
	for _, ruleID := range example.RuleIDs {
		if seenRuleIDs[ruleID] {
			return errors.New("rule_id is duplicate")
		}
		seenRuleIDs[ruleID] = true
		if !knownRules[ruleID] {
			return errors.New("rule_id is unknown")
		}
	}
	return nil
}

// safetyReviewKnownRuleIDs 返回开发样例可引用的全部规则 ID。
func safetyReviewKnownRuleIDs(policy *SafetyReviewPolicy) map[string]bool {
	known := map[string]bool{"DISCRIMINATION-R01": true, "P04B-DECISION-001": true}
	for _, id := range strings.Split(policy.Common.RuleIDs, ",") {
		known[strings.TrimSpace(id)] = true
	}
	for _, card := range policy.Cards {
		for _, condition := range card.RequiredConditions {
			known[condition.ID] = true
		}
		for _, exclusion := range card.DecisiveExclusions {
			known[exclusion.ID] = true
		}
	}
	return known
}

// validateSafetyReviewPolicy 校验发布包的闭集、引用和内容不变量。
func validateSafetyReviewPolicy(policy *SafetyReviewPolicy) error {
	if len(policy.Cards) != 16 {
		return invalidSafetyReviewPolicy("policy must contain exactly 16 cards")
	}
	if err := validateSafetyReviewCommonPolicy(policy.Common); err != nil {
		return err
	}
	priorities := make(map[string]map[int]bool)
	expected := safetyReviewExpectedCardContracts()
	for id, card := range policy.Cards {
		if card.ID != id {
			return invalidSafetyReviewPolicy("card path and id mismatch: %s", id)
		}
		if card.Axis != "attack_method" && card.Axis != "attack_domain" {
			return invalidSafetyReviewPolicy("card %s has invalid axis", id)
		}
		if card.Version != 1 || card.Title == "" || card.Definition == "" || card.PrimaryPriority <= 0 {
			return invalidSafetyReviewPolicy("card %s has invalid metadata", id)
		}
		if len(card.EnabledScenes) == 0 || len(card.RequiredConditions) == 0 ||
			len(card.DecisiveExclusions) == 0 || card.ExamplesFile == "" {
			return invalidSafetyReviewPolicy("card %s lacks required content", id)
		}
		if card.ExamplesFile != "../../examples/p04b-development.jsonl" || len(policy.Examples) == 0 {
			return invalidSafetyReviewPolicy("card %s references missing examples", id)
		}
		for _, scene := range card.EnabledScenes {
			if scene != "prompt" && scene != "response" {
				return invalidSafetyReviewPolicy("card %s has invalid scene", id)
			}
		}
		for _, related := range card.ConfusableWith {
			if policy.Cards[related] == nil {
				return invalidSafetyReviewPolicy("card %s references unknown category %s", id, related)
			}
		}
		if priorities[card.Axis] == nil {
			priorities[card.Axis] = make(map[int]bool)
		}
		if priorities[card.Axis][card.PrimaryPriority] {
			return invalidSafetyReviewPolicy("duplicate priority in axis %s", card.Axis)
		}
		priorities[card.Axis][card.PrimaryPriority] = true
		wantPath := "policy/rules/" + card.Axis + "/" + card.ID + ".yaml"
		if card.SourcePath != wantPath {
			return invalidSafetyReviewPolicy("card path %s does not match axis and id", card.SourcePath)
		}
		want := expected[id]
		if want == nil {
			return invalidSafetyReviewPolicy("unknown card contract %s", id)
		}
		if card.PrimaryPriority != want.priority || !equalStringSlices(card.EnabledScenes, want.scenes) ||
			!equalStringSlices(card.TargetAttributes, want.targetAttributes) ||
			!equalStringSlices(conditionIDsFromCards(card.RequiredConditions), want.conditions) ||
			!equalStringSlices(exclusionIDsFromCards(card.DecisiveExclusions), want.exclusions) {
			return invalidSafetyReviewPolicy("card %s violates its frozen contract", id)
		}
	}
	if len(policy.Cards) != len(expected) {
		return invalidSafetyReviewPolicy("card set does not match the frozen 16-card contract")
	}
	if policy.Cards["occupation_discrimination"] == nil ||
		policy.Cards["other_discrimination"] == nil ||
		policy.Cards["ethnic_hatred"] == nil || policy.Cards["financial_domain_attack"] != nil {
		return invalidSafetyReviewPolicy("required P04-B category set is invalid")
	}
	if !strings.Contains(policy.Common.RuleIDs, "DISCRIMINATION-R01") ||
		!strings.Contains(policy.Decisions, "P04B-DECISION-001") {
		return invalidSafetyReviewPolicy("approved human rule IDs are missing")
	}
	return nil
}

// validateSafetyReviewCommonPolicy 校验共享策略的版本和固定语义配置。
func validateSafetyReviewCommonPolicy(common SafetyReviewCommonPolicy) error {
	if common.Version != 1 {
		return invalidSafetyReviewPolicy("common policy version must be 1")
	}
	ruleIDs := strings.Split(common.RuleIDs, ",")
	for index, id := range ruleIDs {
		ruleIDs[index] = strings.TrimSpace(id)
	}
	if len(ruleIDs) != 1 || ruleIDs[0] != "DISCRIMINATION-R01" {
		return invalidSafetyReviewPolicy("common policy rule ID set is invalid")
	}
	if common.CaseTypePolicy == "" || len(common.RefusalPrefixes) == 0 ||
		len(common.CrossCategoryErrors) == 0 {
		return invalidSafetyReviewPolicy("common policy configuration is incomplete")
	}
	return nil
}

// safetyReviewCardContract 表示冻结规则卡的路径无关契约。
type safetyReviewCardContract struct {
	priority         int
	scenes           []string
	conditions       []string
	exclusions       []string
	targetAttributes []string
}

// safetyReviewExpectedCardContracts 返回 16 张规则卡的固定契约。
func safetyReviewExpectedCardContracts() map[string]*safetyReviewCardContract {
	specificConditions := []string{"D-GROUP-TARGET", "D-IDENTITY-CAUSALITY", "D-HARMFUL-ACT"}
	specificExclusions := []string{
		"X-DESCRIPTION-CRITICISM", "X-INDIVIDUAL-ONLY", "X-OBJECTIVE-CRITERIA",
		"X-RESPONSE-NONCOMPLIANCE", "X-UNRESOLVED-PROXY",
	}
	methodBase := []string{"M-OPERATIONAL-ACTION", "M-MODEL-OR-CONTROL-TARGET", "M-EVASION-FUNCTION"}
	methodExclusions := []string{"MX-DISCUSSION-OR-QUOTATION"}
	domain := func(priority int, conditions []string) *safetyReviewCardContract {
		return &safetyReviewCardContract{
			priority: priority, scenes: []string{"prompt", "response"},
			conditions: conditions, exclusions: specificExclusions,
		}
	}
	method := func(priority int, condition string) *safetyReviewCardContract {
		return &safetyReviewCardContract{
			priority: priority, scenes: []string{"prompt"},
			conditions: append(methodBase, condition), exclusions: methodExclusions,
		}
	}
	return map[string]*safetyReviewCardContract{
		"ethnic_discrimination": withTargets(domain(100, specificConditions), "民族", "种族"),
		"religious_discrimination": withTargets(
			domain(101, specificConditions), "宗教", "信仰",
		),
		"nationality_discrimination": withTargets(
			domain(102, specificConditions), "国籍", "国别身份",
		),
		"regional_discrimination": withTargets(
			domain(103, specificConditions), "地域", "籍贯", "稳定地域身份",
		),
		"gender_discrimination": withTargets(
			domain(104, specificConditions), "性别", "性别身份",
		),
		"age_discrimination": withTargets(
			domain(105, specificConditions), "年龄", "年龄群体",
		),
		"occupation_discrimination": withTargets(
			domain(106, specificConditions),
			"职业", "职业类别", "稳定用工身份", "正式员工", "合同制",
			"劳务派遣", "外包", "临时工", "稳定兼职身份",
		),
		"health_discrimination": withTargets(
			domain(107, specificConditions), "疾病", "残障", "健康状况",
		),
		"other_discrimination": withTargets(
			domain(900, []string{
				"D-GROUP-TARGET", "D-STABLE-IDENTITY", "D-IDENTITY-CAUSALITY",
				"D-SERIOUS-MATERIAL-HARM", "D-HARMFUL-ACT",
			}),
			"财富", "贫困", "社会阶层", "家庭经济背景", "稳定文化身份", "其他稳定社会身份",
		),
		"ethnic_hatred": withTargets(&safetyReviewCardContract{
			priority: 10, scenes: []string{"prompt", "response"},
			conditions: []string{"H-ETHNIC-TARGET", "H-ACTIVE-INCITEMENT", "H-SEVERE-OUTCOME"},
			exclusions: []string{"X-DESCRIPTION-CRITICISM", "X-INDIVIDUAL-ONLY"},
		}, "民族", "种族"),
		"prompt_injection":      method(100, "M-INSTRUCTION-OVERRIDE"),
		"jailbreak":             method(101, "M-SAFETY-BYPASS"),
		"encoding_obfuscation":  method(102, "M-OBFUSCATION-EVASION"),
		"cross_language_attack": method(103, "M-LANGUAGE-EVASION"),
		"cross_modal_attack":    method(104, "M-MODAL-EVASION"),
		"multi_turn_jailbreak":  method(105, "M-STAGED-INDUCEMENT"),
	}
}

// withTargets 返回带固定目标属性的规则卡契约。
func withTargets(contract *safetyReviewCardContract, targets ...string) *safetyReviewCardContract {
	contract.targetAttributes = targets
	return contract
}

// conditionIDsFromCards 提取规则卡条件 ID。
func conditionIDsFromCards(values []SafetyReviewRuleText) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.ID)
	}
	return result
}

// exclusionIDsFromCards 提取规则卡排除 ID。
func exclusionIDsFromCards(values []SafetyReviewRuleText) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.ID)
	}
	return result
}

// equalStringSlices 判断两个字符串切片逐项相等。
func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// buildSafetyReviewCoverage 建立角色到必需规则 ID 的稳定覆盖映射。
func buildSafetyReviewCoverage(policy *SafetyReviewPolicy) {
	ruleIDs := make([]string, 0)
	for _, card := range policy.Cards {
		for _, condition := range card.RequiredConditions {
			ruleIDs = append(ruleIDs, condition.ID)
		}
		for _, exclusion := range card.DecisiveExclusions {
			ruleIDs = append(ruleIDs, exclusion.ID)
		}
	}
	ruleIDs = sortDedupStrings(ruleIDs)
	categories := make([]string, 0, len(policy.Cards))
	for category := range policy.Cards {
		categories = append(categories, category)
	}
	policy.Coverage = map[string][]string{
		"judge_a": {"DISCRIMINATION-R01", "P04B-DECISION-001"},
		"judge_b": {"DISCRIMINATION-R01", "P04B-DECISION-001"},
		"router":  sortDedupStrings(categories),
		"expert":  append([]string(nil), ruleIDs...),
		"arbiter": sortDedupStrings(append(
			append([]string{"DISCRIMINATION-R01", "P04B-DECISION-001"}, ruleIDs...),
			categories...,
		)),
	}
}

// decodeSafetyReviewYAML 严格解码单个 YAML 文档。
func decodeSafetyReviewYAML(data []byte, target any) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return ensureSingleYAMLDocument(decoder)
}

// ensureSingleYAMLDocument 拒绝第二个 YAML 文档。
func ensureSingleYAMLDocument(decoder *yaml.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.New("multiple YAML documents are not supported")
}

// fsPath 返回发布包内指定路径的字节。
func fsPath(contents map[string][]byte, path string) ([]byte, error) {
	data, ok := contents[path]
	if !ok {
		return nil, fmt.Errorf("path is not listed: %s", path)
	}
	return append([]byte(nil), data...), nil
}

// safetyReviewPathInside 判断路径词法上位于 bundle 目录内。
func safetyReviewPathInside(base, path string) bool {
	relative, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

// invalidSafetyReviewPolicy 返回不包含资产内容的策略错误。
func invalidSafetyReviewPolicy(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalidSafetyReviewPolicy}, args...)...)
}
