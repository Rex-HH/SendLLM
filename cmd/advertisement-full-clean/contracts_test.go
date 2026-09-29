package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sendllm/internal/lib/configs"
)

func TestFrozenFullReviewConfigsLoad(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	tests := []struct {
		name      string
		path      string
		model     string
		taskID    string
		wantExtra string
	}{
		{
			name:      "qwen",
			path:      filepath.Join(root, "config", "task.advertisement-full-review.qwen3.5-plus.yaml"),
			model:     "qwen3.5-plus",
			taskID:    "advertisement-full-review-qwen35-plus-pilot-v1",
			wantExtra: "enable_thinking",
		},
		{
			name:   "deepseek",
			path:   filepath.Join(root, "config", "task.advertisement-full-review.deepseek-v4-pro.yaml"),
			model:  "deepseek-v4-pro",
			taskID: "advertisement-full-review-deepseek-v4-pro-pilot-v1",
		},
		{
			name:      "risk priority qwen",
			path:      filepath.Join(root, "config", "task.advertisement-full-review.risk-priority.qwen3.5-plus.yaml"),
			model:     "qwen3.5-plus",
			taskID:    "advertisement-full-review-risk-priority-qwen35-plus-v1",
			wantExtra: "enable_thinking",
		},
		{
			name:   "risk priority deepseek",
			path:   filepath.Join(root, "config", "task.advertisement-full-review.risk-priority.deepseek-v4-pro.yaml"),
			model:  "deepseek-v4-pro",
			taskID: "advertisement-full-review-risk-priority-deepseek-v4-pro-v1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := configs.Load(tt.path)
			if err != nil {
				t.Fatalf("configs.Load() error = %v", err)
			}
			if cfg.Task.ID != tt.taskID || cfg.Model.Name != tt.model {
				t.Fatalf("config identity = %+v", cfg)
			}
			if cfg.Model.BaseURL != "https://aigateway.venusgroup.com.cn/ai/aliyun/openai" ||
				cfg.Model.APIKeyEnv != "AI_GATEWAY_API_KEY" ||
				cfg.Model.StructuredOutput != "json_object" {
				t.Fatalf("model config = %+v", cfg.Model)
			}
			if cfg.Runtime.BatchSize != 32 || cfg.Runtime.BatchMaxInputTokens != 80000 {
				t.Fatalf("batch config = %+v", cfg.Runtime)
			}
			if cfg.Runtime.Concurrency != 4 || cfg.Runtime.RequestsPerMinute != 30 {
				t.Fatalf("runtime config = %+v", cfg.Runtime)
			}
			if tt.wantExtra != "" {
				if _, ok := cfg.Model.ExtraBody[tt.wantExtra]; !ok {
					t.Fatalf("extra body missing %s: %v", tt.wantExtra, cfg.Model.ExtraBody)
				}
			}
			if !strings.Contains(string(cfg.SystemPrompt), `{"items":[{"i":0,"p":"待判断文本"}]}`) {
				t.Fatal("loaded prompt is missing the frozen compact input example")
			}
			if !strings.Contains(string(cfg.SystemPrompt), `{"r":[{"i":0,"l":1,"x":""}]}`) {
				t.Fatal("loaded prompt is missing the frozen compact output example")
			}
		})
	}
}

func TestFrozenFullReviewSchemaShape(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	contents, err := os.ReadFile(filepath.Join(root, "config", "advertisement-full-review-result-schema.json"))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	var schema struct {
		AdditionalProperties bool     `json:"additionalProperties"`
		Required             []string `json:"required"`
		Properties           struct {
			Results struct {
				Items struct {
					AdditionalProperties bool     `json:"additionalProperties"`
					Required             []string `json:"required"`
					Properties           struct {
						Label struct {
							Enum []int `json:"enum"`
						} `json:"l"`
					} `json:"properties"`
				} `json:"items"`
			} `json:"r"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(contents, &schema); err != nil {
		t.Fatalf("decode schema: %v", err)
	}
	if schema.AdditionalProperties || schema.Properties.Results.Items.AdditionalProperties {
		t.Fatal("frozen schema allows additional properties")
	}
	if len(schema.Properties.Results.Items.Properties.Label.Enum) != 3 {
		t.Fatalf("frozen label enum = %v", schema.Properties.Results.Items.Properties.Label.Enum)
	}
}
