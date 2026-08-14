package facade_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"sendllm/internal/dto"
	"sendllm/internal/facade"
	"sendllm/internal/lib/configs"
)

func TestOpenAI_LiveConnectivity(t *testing.T) {
	if os.Getenv("SENDLLM_MODEL_CONNECTIVITY") != "1" {
		t.Skip("set SENDLLM_MODEL_CONNECTIVITY=1 to call the real model")
	}

	configPath := connectivityConfigPath(t)
	cfg, err := configs.Load(configPath)
	if err != nil {
		t.Fatalf("Load(%q) error = %v", configPath, err)
	}
	apiKey, err := cfg.APIKey()
	if err != nil {
		t.Fatalf("APIKey() error = %v", err)
	}
	client, err := facade.NewOpenAI(facade.Config{
		BaseURL:        cfg.Model.BaseURL,
		APIKey:         apiKey,
		Model:          cfg.Model.Name,
		Temperature:    cfg.Model.Temperature,
		TopP:           cfg.Model.TopP,
		MaxTokens:      cfg.Model.MaxTokens,
		Seed:           cfg.Model.Seed,
		ExtraBody:      cfg.Model.ExtraBody,
		Timeout:        cfg.Model.Timeout,
		MaxConnections: 1,
	})
	if err != nil {
		t.Fatalf("NewOpenAI() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Model.Timeout)
	defer cancel()
	response, err := client.Complete(ctx, dto.CompletionRequest{
		Messages: []dto.Message{
			{Role: "system", Content: "你是连接测试助手。只能输出一个 JSON 对象，不要输出 Markdown。"},
			{Role: "user", Content: `{"ping":"pong"}`},
		},
		Schema: json.RawMessage(`{
			"type":"object",
			"additionalProperties":true,
			"properties":{"ok":{"type":"boolean"}}
		}`),
		Mode: cfg.Model.StructuredOutput,
	})
	if err != nil {
		var providerErr *dto.ProviderError
		if errors.As(err, &providerErr) {
			t.Fatalf(
				"Complete() provider error kind=%s status=%d retry_after=%s",
				providerErr.Kind,
				providerErr.StatusCode,
				providerErr.RetryAfter,
			)
		}
		t.Fatalf("Complete() error = %v", err)
	}
	if !json.Valid(response.Content) {
		t.Fatalf("model response is not valid JSON, finish_reason=%q", response.FinishReason)
	}
	t.Logf(
		"model_connectivity=PASS config=%s finish_reason=%s prompt_tokens=%d completion_tokens=%d",
		configPath,
		response.FinishReason,
		response.Usage.PromptTokens,
		response.Usage.CompletionTokens,
	)
}

func connectivityConfigPath(t *testing.T) string {
	t.Helper()
	if value := os.Getenv("SENDLLM_CONNECTIVITY_CONFIG"); value != "" {
		return resolveConnectivityPath(t, value)
	}
	return resolveConnectivityPath(t, "config/task.example.yaml")
}

func resolveConnectivityPath(t *testing.T, value string) string {
	t.Helper()
	if filepath.IsAbs(value) {
		return value
	}
	directory, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	for {
		candidate := filepath.Join(directory, value)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			break
		}
		directory = parent
	}
	return value
}
