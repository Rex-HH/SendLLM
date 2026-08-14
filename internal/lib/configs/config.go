// Package configs 提供任务配置的加载、校验和语义指纹。
package configs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// MaxConcurrency 是当前账号并发上限。
const MaxConcurrency = 500

var (
	// ErrInvalidConfig 表示配置无法安全执行当前任务。
	ErrInvalidConfig = errors.New("configs: invalid configuration")
)

// Config 包含运行一个标注任务所需的配置和加载后的附属数据。
type Config struct {
	Task    TaskConfig    `yaml:"task"`
	Model   ModelConfig   `yaml:"model"`
	Prompt  PromptConfig  `yaml:"prompt"`
	Runtime RuntimeConfig `yaml:"runtime"`
	Retry   RetryConfig   `yaml:"retry"`
	Output  OutputConfig  `yaml:"output"`

	SystemPrompt []byte            `yaml:"-"`
	RiskTypes    map[string]string `yaml:"-"`
	ResultSchema json.RawMessage   `yaml:"-"`
}

// TaskConfig 指定任务身份和本地数据路径。
type TaskConfig struct {
	ID     string `yaml:"id"`
	Input  string `yaml:"input"`
	Output string `yaml:"output"`
	State  string `yaml:"state"`
}

// ModelConfig 指定 OpenAI 兼容模型和可影响生成结果的参数。
type ModelConfig struct {
	BaseURL          string         `yaml:"base_url"`
	APIKeyEnv        string         `yaml:"api_key_env"`
	Name             string         `yaml:"name"`
	StructuredOutput string         `yaml:"structured_output"`
	Temperature      *float64       `yaml:"temperature"`
	TopP             *float64       `yaml:"top_p"`
	MaxTokens        int            `yaml:"max_tokens"`
	Seed             *int64         `yaml:"seed"`
	Timeout          time.Duration  `yaml:"timeout"`
	ExtraBody        map[string]any `yaml:"extra_body"`
}

// PromptConfig 指定系统提示词、审查场景和风险闭集。
type PromptConfig struct {
	SystemFile    string `yaml:"system_file"`
	Scene         string `yaml:"scene"`
	RiskTypesFile string `yaml:"risk_types_file"`
}

// RuntimeConfig 控制可调整的本地吞吐和退出行为。
type RuntimeConfig struct {
	Concurrency            int           `yaml:"concurrency"`
	RequestsPerMinute      int           `yaml:"requests_per_minute"`
	TokensPerMinute        int           `yaml:"tokens_per_minute"`
	ShutdownTimeout        time.Duration `yaml:"shutdown_timeout"`
	CoverConcurrency       int           `yaml:"cover_concurrency"`
	CoverRequestsPerMinute int           `yaml:"cover_requests_per_minute"`

	concurrencySet            bool
	shutdownTimeoutSet        bool
	coverConcurrencySet       bool
	coverRequestsPerMinuteSet bool
}

// RetryConfig 控制每条记录的调用和修复尝试。
type RetryConfig struct {
	RequestMaxAttempts   int           `yaml:"request_max_attempts"`
	FormatRepairAttempts int           `yaml:"format_repair_attempts"`
	InitialBackoff       time.Duration `yaml:"initial_backoff"`
	MaxBackoff           time.Duration `yaml:"max_backoff"`

	requestMaxAttemptsSet   bool
	formatRepairAttemptsSet bool
	initialBackoffSet       bool
	maxBackoffSet           bool
}

// OutputConfig 指定结果 Schema 和解释长度范围。
type OutputConfig struct {
	SchemaFile           string `yaml:"schema_file"`
	ExplanationMinLength int    `yaml:"explanation_min_length"`
	ExplanationMaxLength int    `yaml:"explanation_max_length"`

	explanationMinLengthSet bool
	explanationMaxLengthSet bool
}

// Validate 检查已加载配置的外部输入和执行不变量。
func (c *Config) Validate() error {
	if c.Task.ID == "" || c.Task.Input == "" || c.Task.Output == "" || c.Task.State == "" {
		return invalid("task id and paths are required")
	}
	if c.Model.BaseURL == "" || c.Model.APIKeyEnv == "" || c.Model.Name == "" {
		return invalid("model base_url, api_key_env and name are required")
	}
	if c.Model.StructuredOutput != "json_schema" && c.Model.StructuredOutput != "json_object" && c.Model.StructuredOutput != "prompt_only" {
		return invalid("model structured_output is invalid")
	}
	if c.Model.MaxTokens <= 0 || c.Model.Timeout <= 0 {
		return invalid("model max_tokens and timeout must be positive")
	}
	if c.Prompt.SystemFile == "" || c.Prompt.RiskTypesFile == "" {
		return invalid("prompt paths are required")
	}
	if c.Prompt.Scene != "prompt" && c.Prompt.Scene != "response" && c.Prompt.Scene != "pair" && c.Prompt.Scene != "auto" {
		return invalid("prompt scene is invalid")
	}
	if c.Runtime.Concurrency < 1 || c.Runtime.Concurrency > MaxConcurrency {
		return invalid("runtime concurrency must be between 1 and %d", MaxConcurrency)
	}
	if c.Runtime.RequestsPerMinute < 0 || c.Runtime.TokensPerMinute < 0 || c.Runtime.ShutdownTimeout <= 0 {
		return invalid("runtime limits are invalid")
	}
	if c.Runtime.coverConcurrencySet && (c.Runtime.CoverConcurrency < 1 || c.Runtime.CoverConcurrency > MaxConcurrency) {
		return invalid("runtime cover retry limits are invalid")
	}
	if c.Runtime.coverRequestsPerMinuteSet && c.Runtime.CoverRequestsPerMinute < 0 {
		return invalid("runtime cover retry limits are invalid")
	}
	if c.Retry.RequestMaxAttempts < 1 || c.Retry.FormatRepairAttempts < 0 || c.Retry.InitialBackoff <= 0 || c.Retry.MaxBackoff < c.Retry.InitialBackoff {
		return invalid("retry settings are invalid")
	}
	if c.Output.SchemaFile == "" || c.Output.ExplanationMinLength < 1 || c.Output.ExplanationMaxLength < c.Output.ExplanationMinLength {
		return invalid("output settings are invalid")
	}
	if len(c.SystemPrompt) == 0 || len(c.RiskTypes) == 0 || !json.Valid(c.ResultSchema) {
		return invalid("loaded prompt, risk types or schema is invalid")
	}
	for key := range c.Model.ExtraBody {
		if managedExtraBodyKeys[key] {
			return invalid("model extra_body contains managed key %q", key)
		}
	}

	return nil
}

// SemanticFingerprint 返回只包含标注语义的稳定 SHA-256 指纹。
func (c *Config) SemanticFingerprint() (string, error) {
	riskTypes, err := stableStringMapJSON(c.RiskTypes)
	if err != nil {
		return "", fmt.Errorf("encode risk types: %w", err)
	}
	fingerprintInput := struct {
		Name             string          `json:"name"`
		Temperature      *float64        `json:"temperature"`
		TopP             *float64        `json:"top_p"`
		MaxTokens        int             `json:"max_tokens"`
		Seed             *int64          `json:"seed"`
		ExtraBody        map[string]any  `json:"extra_body"`
		StructuredOutput string          `json:"structured_output"`
		Scene            string          `json:"scene"`
		SystemPrompt     string          `json:"system_prompt"`
		RiskTypes        json.RawMessage `json:"risk_types"`
		ResultSchema     json.RawMessage `json:"result_schema"`
	}{
		Name:             c.Model.Name,
		Temperature:      c.Model.Temperature,
		TopP:             c.Model.TopP,
		MaxTokens:        c.Model.MaxTokens,
		Seed:             c.Model.Seed,
		ExtraBody:        c.Model.ExtraBody,
		StructuredOutput: c.Model.StructuredOutput,
		Scene:            c.Prompt.Scene,
		SystemPrompt:     string(c.SystemPrompt),
		RiskTypes:        riskTypes,
		ResultSchema:     c.ResultSchema,
	}
	encoded, err := json.Marshal(fingerprintInput)
	if err != nil {
		return "", fmt.Errorf("encode semantic fingerprint: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// APIKey 从配置指定的环境变量读取 API Key。
func (c *Config) APIKey() (string, error) {
	value, ok := os.LookupEnv(c.Model.APIKeyEnv)
	if !ok || strings.TrimSpace(value) == "" {
		return "", invalid("API key environment variable %q is empty", c.Model.APIKeyEnv)
	}
	return value, nil
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalidConfig}, args...)...)
}

func stableStringMapJSON(values map[string]string) (json.RawMessage, error) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var builder strings.Builder
	builder.WriteByte('{')
	for index, key := range keys {
		if index > 0 {
			builder.WriteByte(',')
		}
		encodedKey, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		encodedValue, err := json.Marshal(values[key])
		if err != nil {
			return nil, err
		}
		builder.Write(encodedKey)
		builder.WriteByte(':')
		builder.Write(encodedValue)
	}
	builder.WriteByte('}')
	return json.RawMessage(builder.String()), nil
}

var managedExtraBodyKeys = map[string]bool{
	"model":           true,
	"messages":        true,
	"response_format": true,
	"stream":          true,
	"temperature":     true,
	"top_p":           true,
	"max_tokens":      true,
	"seed":            true,
}
