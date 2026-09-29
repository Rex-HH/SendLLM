// Package configs 提供 Policy Optimizer 的严格配置加载与语义指纹。
package configs

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// PolicyOptimizerConfig 是 Policy Optimizer 的冻结配置。
type PolicyOptimizerConfig struct {
	Version    int                             `yaml:"version"`
	Iteration  PolicyOptimizerIterationConfig  `yaml:"iteration"`
	Policy     PolicyOptimizerPolicyConfig     `yaml:"policy"`
	Skills     PolicyOptimizerSkillsConfig     `yaml:"skills"`
	Models     PolicyOptimizerModelsConfig     `yaml:"models"`
	Batching   PolicyOptimizerBatchingConfig   `yaml:"batching"`
	Context    PolicyOptimizerContextConfig    `yaml:"context"`
	Retry      PolicyOptimizerRetryConfig      `yaml:"retry"`
	Regression PolicyOptimizerRegressionConfig `yaml:"regression"`
	Output     PolicyOptimizerOutputConfig     `yaml:"output"`
}

// PolicyOptimizerIterationConfig 指定迭代身份与目录。
type PolicyOptimizerIterationConfig struct {
	ID  string `yaml:"id"`
	Dir string `yaml:"dir"`
}

// PolicyOptimizerPolicyConfig 指定基础版本和目标版本。
type PolicyOptimizerPolicyConfig struct {
	ReleasesDir   string `yaml:"releases_dir"`
	BaseVersion   string `yaml:"base_version"`
	TargetVersion string `yaml:"target_version"`
}

// PolicyOptimizerSkillsConfig 指定 Skill 目录与 manifest。
type PolicyOptimizerSkillsConfig struct {
	Root     string `yaml:"root"`
	Manifest string `yaml:"manifest"`
}

// PolicyOptimizerModelsConfig 指定模型 Profile、角色绑定和共享配额。
type PolicyOptimizerModelsConfig struct {
	Profiles    map[string]PolicyOptimizerModelProfile `yaml:"profiles"`
	Roles       map[string]PolicyOptimizerRoleConfig   `yaml:"roles"`
	QuotaGroups map[string]PolicyOptimizerQuotaConfig  `yaml:"quota_groups"`
}

// PolicyOptimizerModelProfile 描述一个 OpenAI-compatible 模型端点。
type PolicyOptimizerModelProfile struct {
	Family           string        `yaml:"family"`
	BaseURL          string        `yaml:"base_url"`
	APIKeyEnv        string        `yaml:"api_key_env"`
	Name             string        `yaml:"name"`
	StructuredOutput string        `yaml:"structured_output"`
	MaxTokens        int           `yaml:"max_tokens"`
	Timeout          time.Duration `yaml:"timeout"`
	QuotaGroup       string        `yaml:"quota_group"`
}

// PolicyOptimizerRoleConfig 描述逻辑角色自己的模型链与限额。
type PolicyOptimizerRoleConfig struct {
	Primary           string   `yaml:"primary"`
	Fallbacks         []string `yaml:"fallbacks"`
	Concurrency       int      `yaml:"concurrency"`
	RequestsPerMinute *int     `yaml:"requests_per_minute"`
	TokensPerMinute   *int     `yaml:"tokens_per_minute"`
}

// PolicyOptimizerQuotaConfig 描述共享模型配额。
type PolicyOptimizerQuotaConfig struct {
	Concurrency       int  `yaml:"concurrency"`
	RequestsPerMinute *int `yaml:"requests_per_minute"`
	TokensPerMinute   *int `yaml:"tokens_per_minute"`
}

// PolicyOptimizerBatchingConfig 描述 70/20/10 分层批处理参数。
type PolicyOptimizerBatchingConfig struct {
	HomogeneousPercent int `yaml:"homogeneous_percent"`
	ConflictPercent    int `yaml:"conflict_percent"`
	RandomPercent      int `yaml:"random_percent"`
	TargetSize         int `yaml:"target_size"`
	MinSize            int `yaml:"min_size"`
	MaxSize            int `yaml:"max_size"`
}

// PolicyOptimizerContextConfig 描述模型上下文预算。
type PolicyOptimizerContextConfig struct {
	MaxInputTokens int `yaml:"max_input_tokens"`
	MaxArtifacts   int `yaml:"max_artifacts"`
}

// PolicyOptimizerRetryConfig 描述瞬态模型调用重试策略。
type PolicyOptimizerRetryConfig struct {
	TransientAttemptsPerModel int           `yaml:"transient_attempts_per_model"`
	FormatRepairAttempts      int           `yaml:"format_repair_attempts"`
	RefusalRepromptAttempts   int           `yaml:"refusal_reprompt_attempts"`
	InitialBackoff            time.Duration `yaml:"initial_backoff"`
	MaxBackoff                time.Duration `yaml:"max_backoff"`
}

// PolicyOptimizerRegressionConfig 指定 Regression、Gold 和 Gate 资产。
type PolicyOptimizerRegressionConfig struct {
	ContractsDir       string `yaml:"contracts_dir"`
	ApprovedGoldDir    string `yaml:"approved_gold_dir"`
	HiddenGold         string `yaml:"hidden_gold"`
	GatePolicy         string `yaml:"gate_policy"`
	SafetyReviewConfig string `yaml:"safety_review_config"`
}

// PolicyOptimizerOutputConfig 指定运行时状态与退出参数。
type PolicyOptimizerOutputConfig struct {
	StatusInterval  time.Duration `yaml:"status_interval"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
}

var (
	policyOptimizerVersionPattern = regexp.MustCompile(`^p04b-v[1-9][0-9]*\.[0-9]+$`)
	policyOptimizerIDPattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
)

var policyOptimizerRoleFamilies = map[string]string{
	"source_interpreter": "minimax",
	"local_miner":        "minimax",
	"global_merger":      "qwen",
	"case_adjudicator":   "deepseek",
	"policy_diagnoser":   "deepseek",
	"rule_author":        "deepseek",
	"critic":             "qwen",
	"change_resolver":    "deepseek",
}

// LoadPolicyOptimizer 严格读取 Policy Optimizer 配置并解析相对路径。
func LoadPolicyOptimizer(path string) (*PolicyOptimizerConfig, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read policy optimizer config: %w", err)
	}
	config := PolicyOptimizerConfig{}
	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("parse policy optimizer config: %w", err)
	}
	if err := ensureSingleYAMLDocument(decoder); err != nil {
		return nil, fmt.Errorf("parse policy optimizer config: %w", err)
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve policy optimizer config path: %w", err)
	}
	if err := validatePolicyOptimizerRawPaths(&config, filepath.Dir(absolutePath)); err != nil {
		return nil, err
	}
	resolvePolicyOptimizerPaths(&config, filepath.Dir(absolutePath))
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &config, nil
}

// validatePolicyOptimizerRawPaths 拒绝绝对路径和逃逸配置目录的相对路径。
func validatePolicyOptimizerRawPaths(config *PolicyOptimizerConfig, configDir string) error {
	paths := []string{
		config.Iteration.Dir,
		config.Policy.ReleasesDir,
		config.Skills.Root,
		config.Skills.Manifest,
		config.Regression.ContractsDir,
		config.Regression.ApprovedGoldDir,
		config.Regression.HiddenGold,
		config.Regression.GatePolicy,
		config.Regression.SafetyReviewConfig,
	}
	for _, raw := range paths {
		if strings.TrimSpace(raw) == "" || filepath.IsAbs(raw) {
			return fmt.Errorf("policy optimizer path must be relative and non-empty")
		}
		resolved := filepath.Clean(filepath.Join(configDir, raw))
		relative, err := filepath.Rel(configDir, resolved)
		if err != nil || relative == ".." ||
			strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("policy optimizer path escapes config directory")
		}
	}
	return nil
}

// resolvePolicyOptimizerPaths 将配置中的相对路径转换为绝对路径。
func resolvePolicyOptimizerPaths(config *PolicyOptimizerConfig, configDir string) {
	config.Iteration.Dir = resolvePath(configDir, config.Iteration.Dir)
	config.Policy.ReleasesDir = resolvePath(configDir, config.Policy.ReleasesDir)
	config.Skills.Root = resolvePath(configDir, config.Skills.Root)
	config.Skills.Manifest = resolvePath(configDir, config.Skills.Manifest)
	config.Regression.ContractsDir = resolvePath(configDir, config.Regression.ContractsDir)
	config.Regression.ApprovedGoldDir = resolvePath(configDir, config.Regression.ApprovedGoldDir)
	config.Regression.HiddenGold = resolvePath(configDir, config.Regression.HiddenGold)
	config.Regression.GatePolicy = resolvePath(configDir, config.Regression.GatePolicy)
	config.Regression.SafetyReviewConfig = resolvePath(configDir, config.Regression.SafetyReviewConfig)
}

// Validate 校验冻结闭集、路径、批处理比例和模型家族约束。
func (c *PolicyOptimizerConfig) Validate() error {
	if c.Version != 1 {
		return fmt.Errorf("policy optimizer config version must be 1")
	}
	if !policyOptimizerIDPattern.MatchString(c.Iteration.ID) || c.Iteration.Dir == "" {
		return fmt.Errorf("policy optimizer iteration id and dir are required")
	}
	if c.Policy.ReleasesDir == "" || c.Policy.BaseVersion == "" || c.Policy.TargetVersion == "" {
		return fmt.Errorf("policy optimizer policy paths and versions are required")
	}
	if !policyOptimizerVersionPattern.MatchString(c.Policy.BaseVersion) ||
		!policyOptimizerVersionPattern.MatchString(c.Policy.TargetVersion) ||
		c.Policy.BaseVersion == c.Policy.TargetVersion {
		return fmt.Errorf("policy optimizer base and target versions are invalid")
	}
	if c.Skills.Root == "" || c.Skills.Manifest == "" {
		return fmt.Errorf("policy optimizer skills root and manifest are required")
	}
	if err := c.validateModels(); err != nil {
		return err
	}
	if c.Batching.HomogeneousPercent+c.Batching.ConflictPercent+c.Batching.RandomPercent != 100 {
		return fmt.Errorf("policy optimizer batching percentages must sum to 100")
	}
	if c.Batching.MinSize != 30 || c.Batching.TargetSize != 50 || c.Batching.MaxSize != 100 {
		return fmt.Errorf("policy optimizer batching sizes must be 30/50/100")
	}
	if c.Batching.HomogeneousPercent < 0 || c.Batching.ConflictPercent < 0 || c.Batching.RandomPercent < 0 {
		return fmt.Errorf("policy optimizer batching percentages must be non-negative")
	}
	if c.Context.MaxInputTokens <= 0 || c.Context.MaxArtifacts <= 0 {
		return fmt.Errorf("policy optimizer context limits must be positive")
	}
	if c.Retry.TransientAttemptsPerModel != 3 || c.Retry.FormatRepairAttempts != 1 ||
		c.Retry.RefusalRepromptAttempts != 1 {
		return fmt.Errorf("policy optimizer retry counts must be exactly 3/1/1")
	}
	if c.Retry.InitialBackoff <= 0 || c.Retry.MaxBackoff < c.Retry.InitialBackoff {
		return fmt.Errorf("policy optimizer retry backoff is invalid")
	}
	if c.Regression.ContractsDir == "" || c.Regression.ApprovedGoldDir == "" ||
		c.Regression.HiddenGold == "" || c.Regression.GatePolicy == "" ||
		c.Regression.SafetyReviewConfig == "" {
		return fmt.Errorf("policy optimizer regression assets are required")
	}
	if c.Output.StatusInterval <= 0 || c.Output.ShutdownTimeout <= 0 {
		return fmt.Errorf("policy optimizer output durations must be positive")
	}
	return nil
}

// validateModels 校验模型 Profile、角色闭集、家族和 Critic 独立性。
func (c *PolicyOptimizerConfig) validateModels() error {
	if len(c.Models.Profiles) == 0 || len(c.Models.Roles) != len(policyOptimizerRoleFamilies) {
		return fmt.Errorf("policy optimizer profiles and roles are required")
	}
	for profileID, profile := range c.Models.Profiles {
		if !policyOptimizerIDPattern.MatchString(profileID) || profile.Family == "" ||
			profile.Name == "" || profile.APIKeyEnv == "" || profile.StructuredOutput != "json_object" ||
			profile.MaxTokens <= 0 || profile.Timeout <= 0 {
			return fmt.Errorf("policy optimizer profile %q is invalid", profileID)
		}
		if err := validatePolicyOptimizerURL(profile.BaseURL); err != nil {
			return fmt.Errorf("policy optimizer profile %q base_url: %w", profileID, err)
		}
		if profile.QuotaGroup != "" {
			if _, ok := c.Models.QuotaGroups[profile.QuotaGroup]; !ok {
				return fmt.Errorf("policy optimizer profile %q references unknown quota group", profileID)
			}
		}
	}
	criticFamilies := map[string]bool{}
	authorFamilies := map[string]bool{}
	for role, wantFamily := range policyOptimizerRoleFamilies {
		config, ok := c.Models.Roles[role]
		if !ok {
			return fmt.Errorf("policy optimizer role %q is missing", role)
		}
		primary, ok := c.Models.Profiles[config.Primary]
		if !ok || primary.Family != wantFamily {
			return fmt.Errorf("policy optimizer role %q primary family is invalid", role)
		}
		if config.Concurrency <= 0 || config.RequestsPerMinute == nil || *config.RequestsPerMinute < 0 ||
			config.TokensPerMinute == nil || *config.TokensPerMinute < 0 {
			return fmt.Errorf("policy optimizer role %q limits are invalid", role)
		}
		if role == "critic" {
			criticFamilies[primary.Family] = true
		}
		if role == "rule_author" || role == "change_resolver" {
			authorFamilies[primary.Family] = true
		}
		seen := map[string]bool{}
		for _, fallbackID := range config.Fallbacks {
			fallback, ok := c.Models.Profiles[fallbackID]
			if !ok || seen[fallbackID] {
				return fmt.Errorf("policy optimizer role %q fallback is invalid", role)
			}
			seen[fallbackID] = true
			if role == "critic" {
				criticFamilies[fallback.Family] = true
			}
			if role == "rule_author" || role == "change_resolver" {
				authorFamilies[fallback.Family] = true
			}
		}
	}
	for family := range criticFamilies {
		if authorFamilies[family] {
			return fmt.Errorf("policy optimizer critic family must differ from author and resolver")
		}
	}
	for groupID, quota := range c.Models.QuotaGroups {
		if !policyOptimizerIDPattern.MatchString(groupID) || quota.Concurrency <= 0 ||
			quota.RequestsPerMinute == nil || *quota.RequestsPerMinute < 0 ||
			quota.TokensPerMinute == nil || *quota.TokensPerMinute < 0 {
			return fmt.Errorf("policy optimizer quota group %q is invalid", groupID)
		}
	}
	return nil
}

// validatePolicyOptimizerURL 校验模型基址为不含凭据的 HTTP(S) URL。
func validatePolicyOptimizerURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("must be a valid HTTP(S) URL")
	}
	if parsed.User != nil {
		return fmt.Errorf("must not contain URL credentials")
	}
	return nil
}

// CandidateVersion 返回非 preview 的固定候选版本。
func (c *PolicyOptimizerConfig) CandidateVersion() string {
	return c.Policy.TargetVersion + "-candidate.1"
}

// PreviewVersion 返回固定 preview 版本。
func (c *PolicyOptimizerConfig) PreviewVersion() string {
	return c.Policy.TargetVersion + "-preview.1"
}

// SemanticFingerprint 计算仅覆盖语义输入的稳定指纹。
func (c *PolicyOptimizerConfig) SemanticFingerprint(files map[string][]byte) (string, error) {
	profiles := make(map[string]any, len(c.Models.Profiles))
	for id, profile := range c.Models.Profiles {
		profiles[id] = map[string]any{
			"family": profile.Family, "base_url": profile.BaseURL, "name": profile.Name,
			"structured_output": profile.StructuredOutput, "max_tokens": profile.MaxTokens,
			"timeout": profile.Timeout,
		}
	}
	roles := make(map[string]any, len(c.Models.Roles))
	for id, role := range c.Models.Roles {
		roles[id] = map[string]any{"primary": role.Primary, "fallbacks": append([]string(nil), role.Fallbacks...)}
	}
	hashes := make(map[string]string, len(files))
	paths := make([]string, 0, len(files))
	for path, data := range files {
		digest := sha256.Sum256(data)
		hashes[path] = hex.EncodeToString(digest[:])
		paths = append(paths, path)
	}
	sort.Strings(paths)
	orderedHashes := make([][]string, 0, len(paths))
	for _, path := range paths {
		orderedHashes = append(orderedHashes, []string{path, hashes[path]})
	}
	payload := map[string]any{
		"base_version": c.Policy.BaseVersion, "target_version": c.Policy.TargetVersion,
		"profiles": profiles, "roles": roles, "batching": c.Batching, "context": c.Context,
		"retry_attempts": []int{
			c.Retry.TransientAttemptsPerModel, c.Retry.FormatRepairAttempts, c.Retry.RefusalRepromptAttempts,
		},
		"snapshots": orderedHashes,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode policy optimizer semantic fingerprint: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// policyOptimizerPaths 返回用于诊断的非秘密路径集合。
func policyOptimizerPaths(config *PolicyOptimizerConfig) []string {
	return []string{
		config.Iteration.Dir, config.Policy.ReleasesDir, config.Skills.Root,
		config.Skills.Manifest, config.Regression.ContractsDir,
	}
}

// normalizePolicyOptimizerPath 清理配置路径中的空白。
func normalizePolicyOptimizerPath(path string) string {
	return strings.TrimSpace(path)
}
