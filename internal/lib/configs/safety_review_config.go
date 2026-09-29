// Package configs 提供 Safety Review 独立配置的加载、校验和语义指纹。
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
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// SafetyReviewConfig 包含 Safety Review 任务的严格配置。
type SafetyReviewConfig struct {
	Version int                       `yaml:"version"`
	Task    SafetyReviewTaskConfig    `yaml:"task"`
	Policy  SafetyReviewPolicyConfig  `yaml:"policy"`
	Models  SafetyReviewModelsConfig  `yaml:"models"`
	Runtime SafetyReviewRuntimeConfig `yaml:"runtime"`
	Retry   SafetyReviewRetryConfig   `yaml:"retry"`
	Output  SafetyReviewOutputConfig  `yaml:"output"`
}

// SafetyReviewTaskConfig 指定任务身份、输入、目录和审查场景。
type SafetyReviewTaskConfig struct {
	ID      string `yaml:"id"`
	Input   string `yaml:"input"`
	TaskDir string `yaml:"task_dir"`
	Scene   string `yaml:"scene"`
}

// SafetyReviewPolicyConfig 指定已发布 Policy Bundle 的位置。
type SafetyReviewPolicyConfig struct {
	BundleDir string `yaml:"bundle_dir"`
}

// SafetyReviewModelsConfig 指定模型 Profile、角色链和共享配额。
type SafetyReviewModelsConfig struct {
	ExecutionMode string                              `yaml:"execution_mode"`
	Profiles      map[string]SafetyReviewModelProfile `yaml:"profiles"`
	Roles         map[string]SafetyReviewRoleConfig   `yaml:"roles"`
	QuotaGroups   map[string]SafetyReviewQuotaConfig  `yaml:"quota_groups"`
}

// SafetyReviewModelProfile 描述一个 OpenAI 兼容模型端点及其生成参数。
type SafetyReviewModelProfile struct {
	Family           string        `yaml:"family"`
	BaseURL          string        `yaml:"base_url"`
	APIKeyEnv        string        `yaml:"api_key_env"`
	Name             string        `yaml:"name"`
	StructuredOutput string        `yaml:"structured_output"`
	MaxTokens        int           `yaml:"max_tokens"`
	Timeout          time.Duration `yaml:"timeout"`
	QuotaGroup       string        `yaml:"quota_group"`
}

// SafetyReviewRoleConfig 描述角色主模型、备用模型和本地限流。
type SafetyReviewRoleConfig struct {
	Primary           string   `yaml:"primary"`
	Fallbacks         []string `yaml:"fallbacks"`
	Concurrency       int      `yaml:"concurrency"`
	RequestsPerMinute *int     `yaml:"requests_per_minute"`
	TokensPerMinute   *int     `yaml:"tokens_per_minute"`
}

// SafetyReviewQuotaConfig 描述跨角色共享的配额上限。
type SafetyReviewQuotaConfig struct {
	Concurrency       int  `yaml:"concurrency"`
	RequestsPerMinute *int `yaml:"requests_per_minute"`
	TokensPerMinute   *int `yaml:"tokens_per_minute"`
}

// SafetyReviewRuntimeConfig 控制状态输出节奏和中断排空时间。
type SafetyReviewRuntimeConfig struct {
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
	StatusInterval  time.Duration `yaml:"status_interval"`
}

// SafetyReviewRetryConfig 控制模型调用、修复和拒答重试策略。
type SafetyReviewRetryConfig struct {
	TransientAttemptsPerModel int           `yaml:"transient_attempts_per_model"`
	FormatRepairAttempts      int           `yaml:"format_repair_attempts"`
	RefusalRepromptAttempts   int           `yaml:"refusal_reprompt_attempts"`
	InitialBackoff            time.Duration `yaml:"initial_backoff"`
	MaxBackoff                time.Duration `yaml:"max_backoff"`
}

// SafetyReviewOutputConfig 指定任务目录内的六个导出文件。
type SafetyReviewOutputConfig struct {
	Clean         string `yaml:"clean"`
	Audit         string `yaml:"audit"`
	QualityEvents string `yaml:"quality_events"`
	Quarantine    string `yaml:"quarantine"`
	Report        string `yaml:"report"`
	RunStatus     string `yaml:"run_status"`
}

// LoadSafetyReview 严格加载 Safety Review 配置并把相对路径解析为绝对路径。
func LoadSafetyReview(path string) (*SafetyReviewConfig, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, invalid("read safety review config: %v", err)
	}
	config := SafetyReviewConfig{}
	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		return nil, invalid("parse safety review config: %v", err)
	}
	if config.Models.ExecutionMode == "" {
		config.Models.ExecutionMode = "independent_profiles"
	}
	if err := ensureSingleYAMLDocument(decoder); err != nil {
		return nil, invalid("parse safety review config: %v", err)
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, invalid("resolve safety review config path: %v", err)
	}
	resolveSafetyReviewPaths(&config, filepath.Dir(absolutePath))
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &config, nil
}

// resolveSafetyReviewPaths 把配置路径和输出路径解析为可执行的绝对路径。
func resolveSafetyReviewPaths(config *SafetyReviewConfig, configDir string) {
	config.Task.Input = resolvePath(configDir, config.Task.Input)
	config.Task.TaskDir = resolvePath(configDir, config.Task.TaskDir)
	config.Policy.BundleDir = resolvePath(configDir, config.Policy.BundleDir)
	config.Output.Clean = resolvePath(config.Task.TaskDir, config.Output.Clean)
	config.Output.Audit = resolvePath(config.Task.TaskDir, config.Output.Audit)
	config.Output.QualityEvents = resolvePath(config.Task.TaskDir, config.Output.QualityEvents)
	config.Output.Quarantine = resolvePath(config.Task.TaskDir, config.Output.Quarantine)
	config.Output.Report = resolvePath(config.Task.TaskDir, config.Output.Report)
	config.Output.RunStatus = resolvePath(config.Task.TaskDir, config.Output.RunStatus)
}

// Validate 检查 Safety Review 配置的闭集、引用、限额和输出边界。
func (c *SafetyReviewConfig) Validate() error {
	if c.Version != 1 {
		return invalid("safety review config version must be 1")
	}
	if c.Task.ID == "" || c.Task.Input == "" || c.Task.TaskDir == "" {
		return invalid("safety review task id and paths are required")
	}
	if c.Task.Scene != "prompt" && c.Task.Scene != "response" {
		return invalid("safety review scene must be prompt or response")
	}
	if c.Policy.BundleDir == "" {
		return invalid("safety review policy bundle_dir is required")
	}
	if c.Models.ExecutionMode != "independent_profiles" && c.Models.ExecutionMode != "single_profile" {
		return invalid("safety review execution_mode must be independent_profiles or single_profile")
	}
	if err := c.validateProfiles(); err != nil {
		return err
	}
	if err := c.validateRoles(); err != nil {
		return err
	}
	if err := c.validateQuotaGroups(); err != nil {
		return err
	}
	if c.Runtime.ShutdownTimeout <= 0 || c.Runtime.StatusInterval <= 0 {
		return invalid("safety review runtime intervals must be positive")
	}
	if err := c.validateRetry(); err != nil {
		return err
	}
	return c.validateOutputs()
}

// validateOperationalProfile 校验唯一运营 Profile 及其共享配额引用。
func (c *SafetyReviewConfig) validateOperationalProfile() error {
	if len(c.Models.Profiles) != 1 {
		return invalid("single_profile requires exactly one operational profile")
	}
	profile, exists := c.Models.Profiles["operational"]
	if !exists {
		return invalid("single_profile requires the operational profile")
	}
	if profile.Family == "" || profile.Name == "" {
		return invalid("operational profile family and model name are required")
	}
	if strings.TrimSpace(profile.APIKeyEnv) == "" {
		return invalid("operational profile requires a non-empty api_key_env")
	}
	if profile.StructuredOutput != "json_object" {
		return invalid("operational profile requires json_object output")
	}
	if profile.MaxTokens <= 0 || profile.Timeout <= 0 {
		return invalid("operational profile has invalid generation limits")
	}
	if err := validateSafetyReviewBaseURL(profile.BaseURL); err != nil {
		return invalid("operational profile base_url: %v", err)
	}
	if profile.QuotaGroup == "" {
		return invalid("operational profile requires a shared quota group")
	}
	if _, ok := c.Models.QuotaGroups[profile.QuotaGroup]; !ok {
		return invalid("operational profile references unknown quota group %q", profile.QuotaGroup)
	}
	return nil
}

// validateOperationalRoles 校验五个角色都唯一绑定运营模型且没有备用链。
func (c *SafetyReviewConfig) validateOperationalRoles() error {
	if c.Models.Roles == nil || len(c.Models.Roles) != 5 {
		return invalid("safety review roles must contain exactly five closed entries")
	}
	for id, role := range c.Models.Roles {
		if !isSafetyReviewRole(id) {
			return invalid("unknown safety review role %q", id)
		}
		if role.Primary != "operational" {
			return invalid("single_profile role %q must use operational primary", id)
		}
		if len(role.Fallbacks) != 0 {
			return invalid("single_profile role %q must not define fallbacks", id)
		}
		if role.Concurrency <= 0 {
			return invalid("safety review role %q concurrency must be positive", id)
		}
		if role.RequestsPerMinute == nil || *role.RequestsPerMinute < 0 ||
			role.TokensPerMinute == nil || *role.TokensPerMinute < 0 {
			return invalid("safety review role %q rate limits are invalid", id)
		}
	}
	return nil
}

// validateProfiles 校验固定 Profile 集合、端点、参数和配额引用。
func (c *SafetyReviewConfig) validateProfiles() error {
	if c.Models.ExecutionMode == "single_profile" {
		return c.validateOperationalProfile()
	}
	if c.Models.Profiles == nil || len(c.Models.Profiles) != 4 {
		return invalid("safety review profiles must contain exactly four closed entries")
	}
	for _, id := range sortedSafetyReviewKeys(c.Models.Profiles) {
		profile := c.Models.Profiles[id]
		family, ok := safetyReviewProfileFamily(id)
		if !ok {
			return invalid("unknown safety review model profile %q", id)
		}
		name, ok := safetyReviewProfileModelName(id)
		if !ok || profile.Name != name {
			return invalid("safety review profile %q has an invalid model name", id)
		}
		if profile.Family != family {
			return invalid("safety review profile %q has an invalid family", id)
		}
		if strings.TrimSpace(profile.APIKeyEnv) == "" {
			return invalid("safety review profile %q requires a non-empty api_key_env", id)
		}
		if profile.StructuredOutput != "json_object" {
			return invalid("safety review profile %q requires json_object output", id)
		}
		if profile.MaxTokens <= 0 || profile.Timeout <= 0 {
			return invalid("safety review profile %q has invalid generation limits", id)
		}
		if err := validateSafetyReviewBaseURL(profile.BaseURL); err != nil {
			return invalid("safety review profile %q base_url: %v", id, err)
		}
		if profile.QuotaGroup != "" {
			if _, ok := c.Models.QuotaGroups[profile.QuotaGroup]; !ok {
				return invalid("safety review profile %q references unknown quota group %q", id, profile.QuotaGroup)
			}
		}
	}
	return nil
}

// validateRoles 校验固定角色集合、模型引用、备用模型家族和角色限额。
func (c *SafetyReviewConfig) validateRoles() error {
	if c.Models.ExecutionMode == "single_profile" {
		return c.validateOperationalRoles()
	}
	if c.Models.Roles == nil || len(c.Models.Roles) != 5 {
		return invalid("safety review roles must contain exactly five closed entries")
	}
	for _, id := range sortedSafetyReviewKeys(c.Models.Roles) {
		role := c.Models.Roles[id]
		if !isSafetyReviewRole(id) {
			return invalid("unknown safety review role %q", id)
		}
		primary, ok := c.Models.Profiles[role.Primary]
		if !ok {
			return invalid("safety review role %q references unknown primary %q", id, role.Primary)
		}
		if len(role.Fallbacks) == 0 {
			return invalid("safety review role %q requires at least one fallback", id)
		}
		seen := make(map[string]bool, len(role.Fallbacks))
		for _, fallbackID := range role.Fallbacks {
			fallback, ok := c.Models.Profiles[fallbackID]
			if !ok {
				return invalid("safety review role %q references unknown fallback %q", id, fallbackID)
			}
			if seen[fallbackID] {
				return invalid("safety review role %q contains duplicate fallback %q", id, fallbackID)
			}
			seen[fallbackID] = true
			if fallback.Family == primary.Family {
				return invalid("safety review role %q fallback must use a different family", id)
			}
		}
		if role.Concurrency <= 0 {
			return invalid("safety review role %q concurrency must be positive", id)
		}
		if role.RequestsPerMinute == nil || *role.RequestsPerMinute < 0 ||
			role.TokensPerMinute == nil || *role.TokensPerMinute < 0 {
			return invalid("safety review role %q rate limits are invalid", id)
		}
	}
	return nil
}

// validateQuotaGroups 校验共享配额的必填零值和正并发上限。
func (c *SafetyReviewConfig) validateQuotaGroups() error {
	if c.Models.QuotaGroups == nil {
		return invalid("safety review quota_groups mapping is required")
	}
	for _, id := range sortedSafetyReviewKeys(c.Models.QuotaGroups) {
		quota := c.Models.QuotaGroups[id]
		if quota.Concurrency <= 0 {
			return invalid("safety review quota group %q concurrency must be positive", id)
		}
		if quota.RequestsPerMinute == nil || *quota.RequestsPerMinute < 0 ||
			quota.TokensPerMinute == nil || *quota.TokensPerMinute < 0 {
			return invalid("safety review quota group %q rate limits are invalid", id)
		}
	}
	return nil
}

// validateRetry 校验每模型尝试次数和退避时间边界。
func (c *SafetyReviewConfig) validateRetry() error {
	if c.Retry.TransientAttemptsPerModel <= 0 || c.Retry.FormatRepairAttempts <= 0 ||
		c.Retry.RefusalRepromptAttempts <= 0 {
		return invalid("safety review retry attempt counts must be positive")
	}
	if c.Retry.InitialBackoff <= 0 || c.Retry.MaxBackoff < c.Retry.InitialBackoff {
		return invalid("safety review retry backoff settings are invalid")
	}
	return nil
}

// validateOutputs 校验六个导出路径都存在且被任务目录包含。
func (c *SafetyReviewConfig) validateOutputs() error {
	outputs := map[string]string{
		"clean":          c.Output.Clean,
		"audit":          c.Output.Audit,
		"quality_events": c.Output.QualityEvents,
		"quarantine":     c.Output.Quarantine,
		"report":         c.Output.Report,
		"run_status":     c.Output.RunStatus,
	}
	for _, name := range sortedSafetyReviewKeys(outputs) {
		path := outputs[name]
		if path == "" || !safetyReviewPathInside(c.Task.TaskDir, path) {
			return invalid("safety review output %s must stay inside task_dir", name)
		}
	}
	return nil
}

// validateSafetyReviewBaseURL 校验 HTTP(S) 端点并拒绝保留示例主机。
func validateSafetyReviewBaseURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("must be a valid HTTP(S) URL")
	}
	if parsed.User != nil {
		return invalid("must not contain URL credentials")
	}
	if strings.EqualFold(parsed.Hostname(), "provider.invalid") {
		return fmt.Errorf("reserved example host provider.invalid must be replaced")
	}
	return nil
}

// safetyReviewPathInside 判断路径在词法上位于 base 目录内。
func safetyReviewPathInside(base, path string) bool {
	relative, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return false
	}
	return true
}

// SemanticFingerprint 返回只覆盖模型语义、角色链、场景、重试次数和冻结快照的稳定指纹。
func (c *SafetyReviewConfig) SemanticFingerprint(snapshotFiles map[string][]byte) (string, error) {
	profiles := make(map[string]safetyReviewProfileFingerprint, len(c.Models.Profiles))
	for id, profile := range c.Models.Profiles {
		profiles[id] = safetyReviewProfileFingerprint{
			Family:           profile.Family,
			BaseURL:          profile.BaseURL,
			Name:             profile.Name,
			StructuredOutput: profile.StructuredOutput,
			MaxTokens:        profile.MaxTokens,
			Timeout:          profile.Timeout,
		}
	}
	roles := make(map[string]safetyReviewRoleFingerprint, len(c.Models.Roles))
	for id, role := range c.Models.Roles {
		roles[id] = safetyReviewRoleFingerprint{
			Primary:   role.Primary,
			Fallbacks: append([]string(nil), role.Fallbacks...),
		}
	}
	snapshots := make([]safetyReviewSnapshotFingerprint, 0, len(snapshotFiles))
	for path, contents := range snapshotFiles {
		digest := sha256.Sum256(contents)
		snapshots = append(snapshots, safetyReviewSnapshotFingerprint{
			Path:   path,
			SHA256: hex.EncodeToString(digest[:]),
		})
	}
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].Path < snapshots[j].Path
	})
	fingerprintInput := struct {
		Scene         string                                    `json:"scene"`
		ExecutionMode string                                    `json:"execution_mode"`
		Profiles      map[string]safetyReviewProfileFingerprint `json:"profiles"`
		Roles         map[string]safetyReviewRoleFingerprint    `json:"roles"`
		Retry         safetyReviewRetryFingerprint              `json:"retry"`
		Snapshots     []safetyReviewSnapshotFingerprint         `json:"snapshots"`
	}{
		Scene:         c.Task.Scene,
		ExecutionMode: c.Models.ExecutionMode,
		Profiles:      profiles,
		Roles:         roles,
		Retry: safetyReviewRetryFingerprint{
			TransientAttemptsPerModel: c.Retry.TransientAttemptsPerModel,
			FormatRepairAttempts:      c.Retry.FormatRepairAttempts,
			RefusalRepromptAttempts:   c.Retry.RefusalRepromptAttempts,
		},
		Snapshots: snapshots,
	}
	encoded, err := json.Marshal(fingerprintInput)
	if err != nil {
		return "", fmt.Errorf("encode safety review fingerprint: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// safetyReviewProfileFingerprint 只包含会影响生成语义的 Profile 字段。
type safetyReviewProfileFingerprint struct {
	Family           string        `json:"family"`
	BaseURL          string        `json:"base_url"`
	Name             string        `json:"name"`
	StructuredOutput string        `json:"structured_output"`
	MaxTokens        int           `json:"max_tokens"`
	Timeout          time.Duration `json:"timeout"`
}

// safetyReviewRoleFingerprint 只包含角色主备模型链。
type safetyReviewRoleFingerprint struct {
	Primary   string   `json:"primary"`
	Fallbacks []string `json:"fallbacks"`
}

// safetyReviewRetryFingerprint 只包含会影响终止语义的尝试次数。
type safetyReviewRetryFingerprint struct {
	TransientAttemptsPerModel int `json:"transient_attempts_per_model"`
	FormatRepairAttempts      int `json:"format_repair_attempts"`
	RefusalRepromptAttempts   int `json:"refusal_reprompt_attempts"`
}

// safetyReviewSnapshotFingerprint 记录冻结快照文件的路径和内容哈希。
type safetyReviewSnapshotFingerprint struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// sortedSafetyReviewKeys 返回任意字符串键映射的稳定排序结果。
func sortedSafetyReviewKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// safetyReviewProfileFamily 返回固定 Profile ID 对应的模型家族。
func safetyReviewProfileFamily(id string) (string, bool) {
	switch id {
	case "glm_5_2":
		return "glm", true
	case "qwen3_max":
		return "qwen", true
	case "minimax_m2_5":
		return "minimax", true
	case "deepseek_v4_pro":
		return "deepseek", true
	default:
		return "", false
	}
}

// safetyReviewProfileModelName 返回固定 Profile ID 对应的闭集模型名。
func safetyReviewProfileModelName(id string) (string, bool) {
	switch id {
	case "glm_5_2":
		return "GLM-5.3-Flash", true
	case "qwen3_max":
		return "qwen3-max", true
	case "minimax_m2_5":
		return "MiniMax-M2.5", true
	case "deepseek_v4_pro":
		return "deepseek-v4-pro", true
	default:
		return "", false
	}
}

// isSafetyReviewRole 判断角色 ID 是否属于五个固定角色。
func isSafetyReviewRole(id string) bool {
	switch id {
	case "judge_a", "judge_b", "router", "expert", "arbiter":
		return true
	default:
		return false
	}
}
