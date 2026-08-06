// Package facade 提供外部模型协议的适配实现。
package facade

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"sendllm/internal/dto"
	"sendllm/internal/service"
)

const maxResponseBytes = 4 * 1024 * 1024

// Config 指定 OpenAI 兼容客户端的模型和连接参数。
type Config struct {
	BaseURL        string
	APIKey         string
	Model          string
	Temperature    *float64
	TopP           *float64
	MaxTokens      int
	Seed           *int64
	ExtraBody      map[string]any
	Timeout        time.Duration
	MaxConnections int
}

// OpenAI 是 OpenAI Chat Completions 协议的标准库客户端。
type OpenAI struct {
	baseURL     string
	apiKey      string
	model       string
	temperature *float64
	topP        *float64
	maxTokens   int
	seed        *int64
	extraBody   map[string]any
	client      *http.Client
}

var _ service.Completer = (*OpenAI)(nil)

// NewOpenAI 校验配置并创建可复用的 HTTP 客户端。
func NewOpenAI(cfg Config) (*OpenAI, error) {
	if cfg.BaseURL == "" || cfg.APIKey == "" || cfg.Model == "" {
		return nil, errors.New("openai configuration requires base URL, API key, and model")
	}
	if cfg.Timeout <= 0 || cfg.MaxConnections < 1 {
		return nil, errors.New("openai configuration has invalid timeout or max connections")
	}
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if _, err := url.ParseRequestURI(baseURL); err != nil {
		return nil, fmt.Errorf("parse OpenAI base URL: %w", err)
	}
	extraBody, err := cloneExtraBody(cfg.ExtraBody)
	if err != nil {
		return nil, err
	}

	transport := &http.Transport{
		MaxIdleConns:        cfg.MaxConnections,
		MaxIdleConnsPerHost: cfg.MaxConnections,
		IdleConnTimeout:     90 * time.Second,
	}
	return &OpenAI{
		baseURL:     baseURL,
		apiKey:      cfg.APIKey,
		model:       cfg.Model,
		temperature: cfg.Temperature,
		topP:        cfg.TopP,
		maxTokens:   cfg.MaxTokens,
		seed:        cfg.Seed,
		extraBody:   extraBody,
		client: &http.Client{
			Transport: transport,
			Timeout:   cfg.Timeout,
		},
	}, nil
}

// Complete 调用 Chat Completions 并转换为内部 DTO。
func (o *OpenAI) Complete(ctx context.Context, req dto.CompletionRequest) (dto.CompletionResponse, error) {
	body, err := o.requestBody(req)
	if err != nil {
		return dto.CompletionResponse{}, err
	}
	httpRequest, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		o.baseURL+"/chat/completions",
		bytes.NewReader(body),
	)
	if err != nil {
		return dto.CompletionResponse{}, &dto.ProviderError{Kind: dto.ProviderBadRequest, Err: err}
	}
	httpRequest.Header.Set("Authorization", "Bearer "+o.apiKey)
	httpRequest.Header.Set("Content-Type", "application/json")

	response, err := o.client.Do(httpRequest)
	if err != nil {
		return dto.CompletionResponse{}, providerNetworkError(err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		rawResponse, err := readResponse(response.Body)
		if err != nil {
			return dto.CompletionResponse{}, &dto.ProviderError{
				Kind:       dto.ProviderMalformedResponse,
				StatusCode: response.StatusCode,
				Err:        err,
			}
		}
		return dto.CompletionResponse{RawResponse: rawResponse}, providerStatusError(response)
	}

	rawResponse, err := readResponse(response.Body)
	if err != nil {
		return dto.CompletionResponse{}, &dto.ProviderError{Kind: dto.ProviderMalformedResponse, Err: err}
	}
	completion := dto.CompletionResponse{RawResponse: append([]byte(nil), rawResponse...)}
	var decoded completionWireResponse
	if err := json.Unmarshal(rawResponse, &decoded); err != nil {
		return completion, &dto.ProviderError{Kind: dto.ProviderMalformedResponse, Err: err}
	}
	if len(decoded.Choices) == 0 {
		return completion, &dto.ProviderError{Kind: dto.ProviderMalformedResponse, Err: errors.New("missing completion choices")}
	}
	choice := decoded.Choices[0]
	if choice.FinishReason == "content_filter" {
		return completion, &dto.ProviderError{Kind: dto.ProviderContentRejected}
	}
	if choice.Message.Content == "" {
		return completion, &dto.ProviderError{Kind: dto.ProviderMalformedResponse, Err: errors.New("missing completion content")}
	}
	completion.Content = []byte(choice.Message.Content)
	completion.FinishReason = choice.FinishReason
	completion.Usage = decoded.Usage
	return completion, nil
}

func (o *OpenAI) requestBody(req dto.CompletionRequest) ([]byte, error) {
	body := map[string]any{
		"model":    o.model,
		"messages": req.Messages,
	}
	if o.temperature != nil {
		body["temperature"] = *o.temperature
	}
	if o.topP != nil {
		body["top_p"] = *o.topP
	}
	if o.maxTokens > 0 {
		body["max_tokens"] = o.maxTokens
	}
	if o.seed != nil {
		body["seed"] = *o.seed
	}
	responseFormat, err := responseFormat(req)
	if err != nil {
		return nil, &dto.ProviderError{Kind: dto.ProviderBadRequest, Err: err}
	}
	if responseFormat != nil {
		body["response_format"] = responseFormat
	}
	for key, value := range o.extraBody {
		body[key] = value
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, &dto.ProviderError{Kind: dto.ProviderBadRequest, Err: fmt.Errorf("encode completion request: %w", err)}
	}
	return encoded, nil
}

func cloneExtraBody(values map[string]any) (map[string]any, error) {
	if len(values) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("encode OpenAI extra body: %w", err)
	}
	cloned := make(map[string]any, len(values))
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		return nil, fmt.Errorf("decode OpenAI extra body: %w", err)
	}
	return cloned, nil
}

func responseFormat(req dto.CompletionRequest) (map[string]any, error) {
	switch req.Mode {
	case "json_schema":
		if !json.Valid(req.Schema) {
			return nil, errors.New("invalid completion schema")
		}
		return map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "safety_annotation",
				"strict": true,
				"schema": json.RawMessage(req.Schema),
			},
		}, nil
	case "json_object":
		return map[string]any{"type": "json_object"}, nil
	case "prompt_only":
		return nil, nil
	default:
		return nil, errors.New("unsupported structured output mode")
	}
}

func providerNetworkError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return &dto.ProviderError{Kind: dto.ProviderTimeout, Err: err}
	}
	return &dto.ProviderError{Kind: dto.ProviderNetwork, Err: err}
}

func providerStatusError(response *http.Response) error {
	providerErr := &dto.ProviderError{StatusCode: response.StatusCode}
	switch response.StatusCode {
	case http.StatusRequestTimeout:
		providerErr.Kind = dto.ProviderTimeout
	case http.StatusTooManyRequests:
		providerErr.Kind = dto.ProviderRateLimited
		providerErr.RetryAfter = retryAfter(response.Header.Get("Retry-After"), time.Now())
	case http.StatusUnauthorized, http.StatusForbidden:
		providerErr.Kind = dto.ProviderAuthentication
	case http.StatusBadRequest:
		providerErr.Kind = dto.ProviderBadRequest
	default:
		if response.StatusCode >= http.StatusInternalServerError {
			providerErr.Kind = dto.ProviderServer
		} else {
			providerErr.Kind = dto.ProviderMalformedResponse
		}
	}
	return providerErr
}

func retryAfter(value string, now time.Time) time.Duration {
	seconds, err := strconv.Atoi(value)
	if err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	date, err := http.ParseTime(value)
	if err != nil || !date.After(now) {
		return 0
	}
	return date.Sub(now)
}

func readResponse(body io.Reader) ([]byte, error) {
	response, err := io.ReadAll(io.LimitReader(body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read completion response: %w", err)
	}
	if len(response) > maxResponseBytes {
		return nil, errors.New("completion response exceeds size limit")
	}
	return response, nil
}

type completionWireResponse struct {
	Choices []struct {
		Message      dto.Message `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage dto.Usage `json:"usage"`
}
