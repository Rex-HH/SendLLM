package dto

import (
	"fmt"
	"time"
)

// Message 是 OpenAI Chat Completions 协议中的单条消息。
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// CompletionRequest 是 OpenAI 兼容聊天补全请求的最小契约。
type CompletionRequest struct {
	Model          string         `json:"model"`
	Messages       []Message      `json:"messages"`
	Temperature    *float64       `json:"temperature,omitempty"`
	TopP           *float64       `json:"top_p,omitempty"`
	MaxTokens      int            `json:"max_tokens,omitempty"`
	Seed           *int64         `json:"seed,omitempty"`
	ResponseFormat any            `json:"response_format,omitempty"`
	Stream         bool           `json:"stream"`
	ExtraBody      map[string]any `json:"-"`
}

// CompletionResponse 是 OpenAI 兼容聊天补全响应的最小契约。
type CompletionResponse struct {
	ID      string             `json:"id"`
	Choices []CompletionChoice `json:"choices"`
	Usage   CompletionUsage    `json:"usage"`
}

// CompletionChoice 表示一个模型候选结果。
type CompletionChoice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// CompletionUsage 记录供应商报告的 Token 使用量。
type CompletionUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ProviderErrorKind 描述可用于重试策略的供应商错误类别。
type ProviderErrorKind string

const (
	ProviderNetwork           ProviderErrorKind = "network"
	ProviderTimeout           ProviderErrorKind = "timeout"
	ProviderRateLimited       ProviderErrorKind = "rate_limited"
	ProviderServer            ProviderErrorKind = "server"
	ProviderAuthentication    ProviderErrorKind = "authentication"
	ProviderBadRequest        ProviderErrorKind = "bad_request"
	ProviderContentRejected   ProviderErrorKind = "content_rejected"
	ProviderMalformedResponse ProviderErrorKind = "malformed_response"
)

// ProviderError 保留重试策略所需的供应商错误信息。
type ProviderError struct {
	Kind       ProviderErrorKind
	StatusCode int
	RetryAfter time.Duration
	Err        error
}

// Error 返回不包含供应商响应载荷的错误摘要。
func (e *ProviderError) Error() string {
	if e == nil {
		return ""
	}
	if e.StatusCode == 0 {
		return fmt.Sprintf("provider error: %s", e.Kind)
	}
	return fmt.Sprintf("provider error: %s (%d)", e.Kind, e.StatusCode)
}

// Unwrap 返回底层网络或协议错误。
func (e *ProviderError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}
