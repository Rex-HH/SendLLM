package configs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	defaultConcurrency          = 64
	defaultShutdownTimeout      = 30 * time.Second
	defaultRequestMaxAttempts   = 5
	defaultFormatRepairAttempts = 2
	defaultInitialBackoff       = time.Second
	defaultMaxBackoff           = time.Minute
	defaultExplanationMinLength = 10
	defaultExplanationMaxLength = 70
	defaultModelTimeout         = time.Minute
	riskTypesPlaceholder        = "{{RISK_TYPES}}"
	resultSchemaPlaceholder     = "{{RESULT_SCHEMA}}"
)

// Load 加载配置及其提示词、风险闭集和结果 Schema。
func Load(path string) (*Config, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, invalid("read config: %v", err)
	}
	config := Config{}
	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		return nil, invalid("parse config: %v", err)
	}
	if err := ensureSingleYAMLDocument(decoder); err != nil {
		return nil, invalid("parse config: %v", err)
	}
	if err := markExplicitFields(contents, &config); err != nil {
		return nil, invalid("inspect config fields: %v", err)
	}
	applyDefaults(&config)
	resolvePaths(&config, filepath.Dir(path))
	if err := loadAttachments(&config); err != nil {
		return nil, err
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &config, nil
}

func ensureSingleYAMLDocument(decoder *yaml.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("multiple YAML documents are not supported")
}

func applyDefaults(config *Config) {
	if !config.Runtime.concurrencySet {
		config.Runtime.Concurrency = defaultConcurrency
	}
	if !config.Runtime.shutdownTimeoutSet {
		config.Runtime.ShutdownTimeout = defaultShutdownTimeout
	}
	if !config.Runtime.batchSizeSet {
		config.Runtime.BatchSize = 1
	}
	if !config.Retry.requestMaxAttemptsSet {
		config.Retry.RequestMaxAttempts = defaultRequestMaxAttempts
	}
	if !config.Retry.initialBackoffSet {
		config.Retry.InitialBackoff = defaultInitialBackoff
	}
	if !config.Retry.maxBackoffSet {
		config.Retry.MaxBackoff = defaultMaxBackoff
	}
	if !config.Output.explanationMinLengthSet {
		config.Output.ExplanationMinLength = defaultExplanationMinLength
	}
	if !config.Output.explanationMaxLengthSet {
		config.Output.ExplanationMaxLength = defaultExplanationMaxLength
	}
	if config.Model.Timeout == 0 {
		config.Model.Timeout = defaultModelTimeout
	}
	if !config.Retry.formatRepairAttemptsSet {
		config.Retry.FormatRepairAttempts = defaultFormatRepairAttempts
	}
}

func markExplicitFields(contents []byte, config *Config) error {
	var document yaml.Node
	if err := yaml.Unmarshal(contents, &document); err != nil {
		return err
	}
	if len(document.Content) == 0 {
		return nil
	}
	runtime := mappingValue(document.Content[0], "runtime")
	config.Runtime.concurrencySet = mappingValue(runtime, "concurrency") != nil
	config.Runtime.shutdownTimeoutSet = mappingValue(runtime, "shutdown_timeout") != nil
	config.Runtime.coverConcurrencySet = mappingValue(runtime, "cover_concurrency") != nil
	config.Runtime.coverRequestsPerMinuteSet = mappingValue(runtime, "cover_requests_per_minute") != nil
	config.Runtime.batchSizeSet = mappingValue(runtime, "batch_size") != nil
	retry := mappingValue(document.Content[0], "retry")
	config.Retry.requestMaxAttemptsSet = mappingValue(retry, "request_max_attempts") != nil
	config.Retry.formatRepairAttemptsSet = mappingValue(retry, "format_repair_attempts") != nil
	config.Retry.initialBackoffSet = mappingValue(retry, "initial_backoff") != nil
	config.Retry.maxBackoffSet = mappingValue(retry, "max_backoff") != nil
	output := mappingValue(document.Content[0], "output")
	config.Output.explanationMinLengthSet = mappingValue(output, "explanation_min_length") != nil
	config.Output.explanationMaxLengthSet = mappingValue(output, "explanation_max_length") != nil
	return nil
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index < len(node.Content); index += 2 {
		if node.Content[index].Value == key {
			return node.Content[index+1]
		}
	}
	return nil
}

func resolvePaths(config *Config, directory string) {
	config.Task.Input = resolvePath(directory, config.Task.Input)
	config.Task.Output = resolvePath(directory, config.Task.Output)
	config.Task.State = resolvePath(directory, config.Task.State)
	config.Prompt.SystemFile = resolvePath(directory, config.Prompt.SystemFile)
	config.Prompt.RiskTypesFile = resolvePath(directory, config.Prompt.RiskTypesFile)
	config.Output.SchemaFile = resolvePath(directory, config.Output.SchemaFile)
}

func resolvePath(directory, path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(directory, path)
}

func loadAttachments(config *Config) error {
	riskTypes, err := loadRiskTypes(config.Prompt.RiskTypesFile)
	if err != nil {
		return err
	}
	resultSchema, err := os.ReadFile(config.Output.SchemaFile)
	if err != nil {
		return invalid("read result schema: %v", err)
	}
	if !json.Valid(resultSchema) {
		return invalid("result schema is not valid JSON")
	}
	systemTemplate, err := os.ReadFile(config.Prompt.SystemFile)
	if err != nil {
		return invalid("read system prompt: %v", err)
	}
	riskTypesJSON, err := stableStringMapJSON(riskTypes)
	if err != nil {
		return invalid("encode risk types: %v", err)
	}
	systemPrompt, err := substitutePrompt(systemTemplate, riskTypesJSON, resultSchema)
	if err != nil {
		return err
	}
	config.RiskTypes = riskTypes
	config.ResultSchema = append(json.RawMessage(nil), resultSchema...)
	config.SystemPrompt = systemPrompt
	return nil
}

func loadRiskTypes(path string) (map[string]string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, invalid("read risk types: %v", err)
	}
	values := make(map[string]string)
	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	decoder.KnownFields(true)
	if err := decoder.Decode(&values); err != nil {
		return nil, invalid("parse risk types: %v", err)
	}
	if len(values) == 0 {
		return nil, invalid("risk types must not be empty")
	}
	for key, value := range values {
		if key == "" || value == "" {
			return nil, invalid("risk types must have non-empty keys and values")
		}
	}
	return values, nil
}

func substitutePrompt(template, riskTypes, resultSchema []byte) ([]byte, error) {
	prompt := string(template)
	if strings.Count(prompt, riskTypesPlaceholder) != 1 || strings.Count(prompt, resultSchemaPlaceholder) != 1 {
		return nil, invalid("system prompt must contain each placeholder exactly once")
	}
	prompt = strings.Replace(prompt, riskTypesPlaceholder, string(riskTypes), 1)
	prompt = strings.Replace(prompt, resultSchemaPlaceholder, string(resultSchema), 1)
	return []byte(prompt), nil
}
