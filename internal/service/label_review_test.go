package service_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sendllm/internal/dto"
	"sendllm/internal/lib/limiter"
	"sendllm/internal/service"
)

func TestLabelReviewBatchUsesUnifiedOriginalLabels(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.jsonl")
	outputPath := filepath.Join(directory, "final.jsonl")
	statePath := filepath.Join(directory, "state.db")
	writeAdjudicateFile(t, inputPath,
		labelReviewInputLine(
			"prompt-row",
			"prompt",
			"unsafe",
			"原始提示词风险理由。",
			map[string]any{
				"attack_method": "prompt_injection",
				"attack_domain": "",
				"risk_level":    "medium",
				"case_type":     "typical",
				"is_attack":     true,
			},
		),
		labelReviewInputLine(
			"response-row",
			"response",
			"unsafe",
			"原始回复风险理由。",
			map[string]any{
				"attack_method": "",
				"attack_domain": "privacy_right_infringement",
				"risk_level":    "high",
				"case_type":     "borderline",
				"is_attack":     true,
			},
		),
	)

	var payload struct {
		Items []struct {
			TraceID       string `json:"trace_id"`
			Scene         string `json:"scene"`
			OriginalLabel struct {
				Label        string `json:"label"`
				AttackMethod string `json:"attack_method"`
				AttackDomain string `json:"attack_domain"`
				RiskLevel    string `json:"risk_level"`
				CaseType     string `json:"case_type"`
				IsAttack     bool   `json:"is_attack"`
			} `json:"original_label"`
			OriginalExplanation string `json:"original_explanation"`
		} `json:"items"`
	}
	completer := reconcileCompleterFunc(func(ctx context.Context, request dto.CompletionRequest) (dto.CompletionResponse, error) {
		if err := json.Unmarshal([]byte(request.Messages[1].Content), &payload); err != nil {
			t.Fatalf("model payload is not JSON: %v", err)
		}
		return reconcileCompletion(`{
			"results":[
				{
					"trace_id":"prompt-row",
					"is_attack":true,
					"case_type":"typical",
					"explanation":"模型确认提示词注入风险。",
					"quality_score":0.9,
					"extended_info":{"attack_method":"prompt_injection","attack_domain":"","risk_level":"medium"}
				},
				{
					"trace_id":"response-row",
					"is_attack":true,
					"case_type":"borderline",
					"explanation":"模型确认隐私侵权风险。",
					"quality_score":0.8,
					"extended_info":{"attack_method":"","attack_domain":"privacy_right_infringement","risk_level":"high"}
				}
			]
		}`), nil
	})

	stats, err := service.LabelReviewBatch(context.Background(), labelReviewTestConfig(
		t,
		inputPath,
		outputPath,
		statePath,
		completer,
	))
	if err != nil {
		t.Fatalf("LabelReviewBatch() error = %v", err)
	}
	if stats.Succeeded != 2 || stats.Failed != 0 {
		t.Fatalf("stats = %#v, want 2 succeeded and 0 failed", stats)
	}
	if len(payload.Items) != 2 {
		t.Fatalf("payload items = %d, want 2", len(payload.Items))
	}
	if payload.Items[0].OriginalLabel.AttackMethod != "prompt_injection" {
		t.Fatalf("prompt original label = %#v, want mapped attack_method", payload.Items[0].OriginalLabel)
	}
	if payload.Items[1].Scene != "response" ||
		payload.Items[1].OriginalLabel.AttackDomain != "privacy_right_infringement" {
		t.Fatalf("response original label = %#v, scene=%q", payload.Items[1].OriginalLabel, payload.Items[1].Scene)
	}

	lines := readAdjudicateLines(t, outputPath)
	if len(lines) != 2 {
		t.Fatalf("output lines = %d, want 2", len(lines))
	}
	if !strings.Contains(lines[0], `"explanation":"原始提示词风险理由。"`) {
		t.Fatalf("first line = %s, want original explanation reused", lines[0])
	}
	if !strings.Contains(lines[1], `"scene":"response"`) ||
		!strings.Contains(lines[1], `"attack_domain":"privacy_right_infringement"`) {
		t.Fatalf("second line = %s, want response scene and domain", lines[1])
	}
}

func TestLabelReviewBatchKeepsClaimedItemsAcrossConcurrentResults(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.jsonl")
	outputPath := filepath.Join(directory, "final.jsonl")
	statePath := filepath.Join(directory, "state.db")

	const rowCount = 128
	lines := make([]string, 0, rowCount)
	for index := range rowCount {
		lines = append(lines, labelReviewInputLine(
			fmt.Sprintf("row-%03d", index),
			"prompt",
			"unsafe",
			"原始提示词风险理由。",
			map[string]any{
				"attack_method": "prompt_injection",
				"attack_domain": "",
				"risk_level":    "medium",
				"case_type":     "typical",
				"is_attack":     true,
			},
		))
	}
	writeAdjudicateFile(t, inputPath, lines...)

	var payload struct {
		Items []struct {
			TraceID string `json:"trace_id"`
		} `json:"items"`
	}
	completer := reconcileCompleterFunc(func(ctx context.Context, request dto.CompletionRequest) (dto.CompletionResponse, error) {
		if err := json.Unmarshal([]byte(request.Messages[1].Content), &payload); err != nil {
			t.Fatalf("model payload is not JSON: %v", err)
		}
		if len(payload.Items) != 1 {
			t.Fatalf("payload items = %d, want 1", len(payload.Items))
		}
		traceID := payload.Items[0].TraceID
		return reconcileCompletion(fmt.Sprintf(`{
			"results":[
				{
					"trace_id":%q,
					"is_attack":true,
					"case_type":"typical",
					"explanation":"模型确认提示词注入风险。",
					"quality_score":0.9,
					"extended_info":{"attack_method":"prompt_injection","attack_domain":"","risk_level":"medium"}
				}
			]
		}`, traceID)), nil
	})

	config := labelReviewTestConfig(t, inputPath, outputPath, statePath, completer)
	config.BatchSize = 1
	requestLimiter, err := limiter.New(limiter.Config{Concurrency: 8})
	if err != nil {
		t.Fatalf("limiter.New() error = %v", err)
	}
	config.Limiter = requestLimiter

	stats, err := service.LabelReviewBatch(context.Background(), config)
	if err != nil {
		t.Fatalf("LabelReviewBatch() error = %v", err)
	}
	if stats.Succeeded != rowCount || stats.Failed != 0 {
		t.Fatalf("stats = %#v, want %d succeeded and 0 failed", stats, rowCount)
	}
	if lines := readAdjudicateLines(t, outputPath); len(lines) != rowCount {
		t.Fatalf("output lines = %d, want %d", len(lines), rowCount)
	}
}

// labelReviewInputLine 生成标准标签复核测试使用的统一入口行。
func labelReviewInputLine(
	traceID string,
	scene string,
	label string,
	explanation string,
	extendedInfo map[string]any,
) string {
	record := map[string]any{
		"trace_id":      traceID,
		"source":        "v1",
		"split":         "train",
		"language":      "zh",
		"scene":         scene,
		"label":         label,
		"prompt":        "合成提示词",
		"response":      "",
		"explanation":   explanation,
		"extended_info": extendedInfo,
		"annotation": map[string]any{
			"method":        "source_mapping",
			"quality_score": nil,
		},
	}
	if scene == "response" {
		record["response"] = "合成回复"
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		panic(err)
	}
	return string(encoded) + "\n"
}

// labelReviewTestConfig 构造不调用真实模型的标准标签复核配置。
func labelReviewTestConfig(
	t *testing.T,
	inputPath string,
	outputPath string,
	statePath string,
	completer service.Completer,
) service.LabelReviewConfig {
	t.Helper()
	validator, err := service.NewValidator(
		[]byte(reconcileSchema),
		map[string]string{
			"prompt_injection":                  "提示词注入攻击",
			"privacy_right_infringement":        "隐私权侵害",
			"harm_to_physical_or_mental_health": "身心健康损害",
		},
		10,
		70,
	)
	if err != nil {
		t.Fatalf("NewValidator() error = %v", err)
	}
	requestLimiter, err := limiter.New(limiter.Config{Concurrency: 1})
	if err != nil {
		t.Fatalf("limiter.New() error = %v", err)
	}
	return service.LabelReviewConfig{
		TaskID:              "label-review-task",
		InputPath:           inputPath,
		OutputPath:          outputPath,
		StatePath:           statePath,
		SemanticHash:        "synthetic-label-review-hash",
		SystemPrompt:        []byte("synthetic label review batch system prompt"),
		Schema:              []byte(reconcileSchema),
		Mode:                "json_object",
		Completer:           completer,
		Validator:           validator,
		Limiter:             requestLimiter,
		MaxTokens:           100,
		MaxAttempts:         1,
		Shutdown:            time.Second,
		BatchSize:           2,
		BatchMaxInputTokens: 0,
		RetryPolicy: service.RetryPolicy{
			MaxAttempts:    1,
			InitialBackoff: time.Millisecond,
			MaxBackoff:     time.Millisecond,
		},
	}
}
