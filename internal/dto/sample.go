// Package dto 定义输入、标注和模型协议的数据契约。
package dto

import (
	"encoding/json"
	"errors"
	"fmt"
)

var (
	// ErrInvalidSource 表示输入样本不满足导入契约。
	ErrInvalidSource = errors.New("dto: invalid source sample")
)

// SourceSample 表示保留未知字段的原始输入样本。
type SourceSample struct {
	TraceID  string
	Prompt   string
	Response string
	Extra    map[string]json.RawMessage
	Raw      json.RawMessage
}

// ModelInput 是发送给模型的最小结构化用户消息。
type ModelInput struct {
	TraceID  string `json:"trace_id"`
	Scene    string `json:"scene"`
	Prompt   string `json:"prompt"`
	Response string `json:"response"`
}

// ParseSource 解析并校验输入样本，同时保留未识别字段。
func ParseSource(raw []byte) (SourceSample, error) {
	fields := make(map[string]json.RawMessage)
	if err := json.Unmarshal(raw, &fields); err != nil {
		return SourceSample{}, fmt.Errorf("%w: decode JSON: %v", ErrInvalidSource, err)
	}
	traceID, err := takeRequiredString(fields, "trace_id")
	if err != nil {
		return SourceSample{}, err
	}
	prompt, err := takeOptionalString(fields, "prompt", false)
	if err != nil {
		return SourceSample{}, err
	}
	response, err := takeOptionalString(fields, "response", true)
	if err != nil {
		return SourceSample{}, err
	}
	if prompt == "" && response == "" {
		return SourceSample{}, fmt.Errorf("%w: prompt and response are both empty", ErrInvalidSource)
	}
	extra := make(map[string]json.RawMessage, len(fields))
	for key, value := range fields {
		extra[key] = append(json.RawMessage(nil), value...)
	}
	return SourceSample{
		TraceID:  traceID,
		Prompt:   prompt,
		Response: response,
		Extra:    extra,
		Raw:      append(json.RawMessage(nil), raw...),
	}, nil
}

// ModelInput 返回当前样本在给定场景下的模型输入。
func (s SourceSample) ModelInput(scene string) ModelInput {
	if scene == "auto" {
		scene = sourceScene(s.Prompt, s.Response)
	}
	return ModelInput{
		TraceID:  s.TraceID,
		Scene:    scene,
		Prompt:   s.Prompt,
		Response: s.Response,
	}
}

// MarshalJSON 将已知字段和保留字段组合为可导出的源样本。
func (s SourceSample) MarshalJSON() ([]byte, error) {
	fields := make(map[string]json.RawMessage, len(s.Extra)+3)
	for key, value := range s.Extra {
		fields[key] = append(json.RawMessage(nil), value...)
	}
	traceID, err := json.Marshal(s.TraceID)
	if err != nil {
		return nil, fmt.Errorf("encode trace_id: %w", err)
	}
	prompt, err := json.Marshal(s.Prompt)
	if err != nil {
		return nil, fmt.Errorf("encode prompt: %w", err)
	}
	response, err := json.Marshal(s.Response)
	if err != nil {
		return nil, fmt.Errorf("encode response: %w", err)
	}
	fields["trace_id"] = traceID
	fields["prompt"] = prompt
	fields["response"] = response
	encoded, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("encode source sample: %w", err)
	}
	return encoded, nil
}

func takeRequiredString(fields map[string]json.RawMessage, name string) (string, error) {
	value, ok := fields[name]
	delete(fields, name)
	if !ok {
		return "", fmt.Errorf("%w: %s is required", ErrInvalidSource, name)
	}
	var decoded string
	if err := json.Unmarshal(value, &decoded); err != nil || decoded == "" {
		return "", fmt.Errorf("%w: %s must be a non-empty string", ErrInvalidSource, name)
	}
	return decoded, nil
}

func takeOptionalString(fields map[string]json.RawMessage, name string, allowNull bool) (string, error) {
	value, ok := fields[name]
	delete(fields, name)
	if !ok {
		return "", nil
	}
	if allowNull && string(value) == "null" {
		return "", nil
	}
	var decoded string
	if err := json.Unmarshal(value, &decoded); err != nil {
		return "", fmt.Errorf("%w: %s must be a string", ErrInvalidSource, name)
	}
	return decoded, nil
}

func sourceScene(prompt, response string) string {
	if prompt != "" && response != "" {
		return "pair"
	}
	if prompt != "" {
		return "prompt"
	}
	return "response"
}
