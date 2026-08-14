package service

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"sendllm/internal/dao"
	"sendllm/internal/dto"
	"sendllm/internal/lib/limiter"
)

// AdjudicateConfig 指定差异样本综合裁决任务所需依赖。
type AdjudicateConfig struct {
	TaskID       string
	InputPath    string
	OutputPath   string
	StatePath    string
	SemanticHash string
	SystemPrompt []byte
	Scene        string
	Schema       json.RawMessage
	Mode         string
	Completer    Completer
	Validator    *Validator
	Limiter      *limiter.Limiter
	MaxTokens    int
	RetryPolicy  RetryPolicy
	MaxAttempts  int
	Shutdown     time.Duration
	OnProgress   func(Summary)
}

type adjudicateInput struct {
	ID            string           `json:"id"`
	Source        adjudicateSource `json:"source"`
	Messages      []dto.Message    `json:"messages"`
	Meta          adjudicateMeta   `json:"meta"`
	Annotation    json.RawMessage  `json:"annotation"`
	OriginalLabel adjudicateLabel  `json:"original_label"`
	ModelLabel    adjudicateLabel  `json:"model_label"`
	Raw           json.RawMessage  `json:"-"`
}

type adjudicateSource struct {
	Dataset string `json:"dataset"`
	Path    string `json:"path"`
	Index   int64  `json:"index"`
}

type adjudicateMeta struct {
	Split        string                     `json:"split"`
	Language     string                     `json:"language"`
	SourceFields adjudicateMetaSourceFields `json:"source_fields"`
}

type adjudicateMetaSourceFields struct {
	Reason string `json:"reason"`
}

type adjudicateLabel struct {
	Label     string `json:"label"`
	RiskType  string `json:"risk_type"`
	RiskLevel string `json:"risk_level"`
	CaseType  string `json:"case_type"`
	IsAttack  bool   `json:"is_attack"`
}

type adjudicatePrompt struct {
	ID            string                    `json:"id"`
	Messages      []dto.Message             `json:"messages"`
	OriginalLabel adjudicateLabel           `json:"original_label"`
	ModelLabel    adjudicateLabel           `json:"model_label"`
	ModelJudgment adjudicateModelJudgment   `json:"model_judgment"`
	Reasons       adjudicateJudgmentReasons `json:"reasons"`
}

type adjudicateModelJudgment struct {
	Explanation string `json:"explanation"`
}

type adjudicateJudgmentReasons struct {
	Original string `json:"original"`
	Model    string `json:"model"`
}

type masbOutput struct {
	TraceID      string             `json:"trace_id"`
	Source       string             `json:"source"`
	Split        string             `json:"split"`
	Language     string             `json:"language"`
	Scene        string             `json:"scene"`
	Label        string             `json:"label"`
	Prompt       string             `json:"prompt"`
	Response     string             `json:"response"`
	Explanation  string             `json:"explanation"`
	ExtendedInfo dto.ExtendedInfo   `json:"extended_info,omitempty"`
	Annotation   dto.AnnotationMeta `json:"annotation"`
}

// Adjudicate 读取差异 JSONL，调用模型综合裁决并导出 MASB 8-4 格式。
func Adjudicate(ctx context.Context, cfg AdjudicateConfig) (ExportStats, error) {
	if cfg.TaskID == "" || cfg.InputPath == "" || cfg.OutputPath == "" || cfg.StatePath == "" {
		return ExportStats{}, fmt.Errorf("adjudicate configuration is incomplete")
	}
	if len(cfg.SystemPrompt) == 0 || len(cfg.Schema) == 0 || cfg.SemanticHash == "" {
		return ExportStats{}, fmt.Errorf("adjudicate configuration is incomplete")
	}
	if cfg.Completer == nil || cfg.Validator == nil || cfg.Limiter == nil {
		return ExportStats{}, fmt.Errorf("adjudicate dependencies are incomplete")
	}
	if cfg.Shutdown <= 0 {
		return ExportStats{}, fmt.Errorf("adjudicate shutdown timeout is invalid")
	}
	store, err := dao.Open(ctx, cfg.StatePath)
	if err != nil {
		return ExportStats{}, fmt.Errorf("open adjudicate state: %w", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.EnsureTask(ctx, dao.Task{ID: cfg.TaskID, SemanticHash: cfg.SemanticHash}); err != nil {
		return ExportStats{}, fmt.Errorf("ensure adjudicate task: %w", err)
	}
	input, err := os.Open(cfg.InputPath)
	if err != nil {
		return ExportStats{}, fmt.Errorf("open adjudicate input: %w", err)
	}
	stats, importErr := ImportAdjudicate(ctx, store, cfg.TaskID, input)
	closeErr := input.Close()
	if importErr != nil {
		return ExportStats{}, importErr
	}
	if closeErr != nil {
		return ExportStats{}, fmt.Errorf("close adjudicate input: %w", closeErr)
	}
	if stats.Added > 0 || stats.Skipped > 0 {
		// 导入统计仅用于确认输入已纳入 SQLite，避免输出文件承担进度职责。
	}
	if _, err := store.ResetFailed(ctx, cfg.TaskID); err != nil {
		return ExportStats{}, fmt.Errorf("reset adjudicate failed rows: %w", err)
	}
	runner, err := NewRunner(adjudicateRunnerConfig(cfg, store))
	if err != nil {
		return ExportStats{}, fmt.Errorf("create adjudicate runner: %w", err)
	}
	if _, err := runner.Run(ctx); err != nil {
		exportCtx, cancelExport := adjudicateExportContext(ctx, cfg.Shutdown)
		exportStats, exportErr := ExportAdjudicate(exportCtx, store, cfg.TaskID, cfg.OutputPath, cfg.Scene)
		cancelExport()
		if exportErr != nil {
			return ExportStats{}, fmt.Errorf("adjudicate run failed: %w; export failed: %v", err, exportErr)
		}
		return exportStats, err
	}
	return ExportAdjudicate(ctx, store, cfg.TaskID, cfg.OutputPath, cfg.Scene)
}

// adjudicateExportContext 在人工中断后保留一小段时间完成终态导出。
func adjudicateExportContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if ctx.Err() == nil {
		return ctx, func() {}
	}
	return context.WithTimeout(context.Background(), timeout)
}

// ImportAdjudicate 将差异 JSONL 原子导入 SQLite 状态库。
func ImportAdjudicate(ctx context.Context, store *dao.Store, taskID string, reader io.Reader) (ImportStats, error) {
	imp, err := store.BeginImport(ctx, taskID)
	if err != nil {
		return ImportStats{}, fmt.Errorf("begin adjudicate import: %w", err)
	}
	defer func() { _ = imp.Rollback() }()

	var stats ImportStats
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), maxJSONLLineSize+1)
	var inputIndex int64
	for scanner.Scan() {
		inputIndex++
		raw := append([]byte(nil), scanner.Bytes()...)
		if len(raw) > maxJSONLLineSize {
			return ImportStats{}, fmt.Errorf("read adjudicate line %d: JSONL line exceeds %d bytes", inputIndex, maxJSONLLineSize)
		}
		record, err := parseAdjudicateInput(raw)
		if err != nil {
			return ImportStats{}, fmt.Errorf("parse adjudicate line %d: %w", inputIndex, err)
		}
		prompt, response := adjudicatePromptResponse(record.Messages)
		disposition, err := imp.Add(ctx, dao.Item{
			TaskID:     taskID,
			TraceID:    record.ID,
			InputIndex: inputIndex,
			RawJSON:    record.Raw,
			Prompt:     prompt,
			Response:   response,
			State:      dao.ItemPending,
		})
		if err != nil {
			return ImportStats{}, fmt.Errorf("import adjudicate line %d: %w", inputIndex, err)
		}
		if disposition == dao.ImportAdded {
			stats.Added++
			continue
		}
		stats.Skipped++
	}
	if err := scanner.Err(); err != nil {
		return ImportStats{}, fmt.Errorf("read adjudicate line %d: %w", inputIndex+1, err)
	}
	if err := imp.Commit(); err != nil {
		return ImportStats{}, fmt.Errorf("commit adjudicate import: %w", err)
	}
	return stats, nil
}

// ExportAdjudicate 从 SQLite 终态导出裁决成功和失败文件。
func ExportAdjudicate(ctx context.Context, store *dao.Store, taskID, outputPath, scene string) (ExportStats, error) {
	var stats ExportStats
	successTemp, err := stageJSONL(outputPath, func(encoder *json.Encoder) error {
		return store.ForEachSucceeded(ctx, taskID, func(record dao.ExportRecord) error {
			input, annotation, err := decodeAdjudicateExport(record)
			if err != nil {
				return err
			}
			if err := encoder.Encode(buildMASBOutput(scene, input, annotation)); err != nil {
				return fmt.Errorf("encode adjudicate succeeded record: %w", err)
			}
			stats.Succeeded++
			return nil
		})
	})
	if err != nil {
		return ExportStats{}, fmt.Errorf("stage adjudicate succeeded export: %w", err)
	}
	defer func() { _ = os.Remove(successTemp) }()

	failedPath := exportFailedPath(outputPath)
	failedTemp, err := stageJSONL(failedPath, func(encoder *json.Encoder) error {
		return store.ForEachFailed(ctx, taskID, func(record dao.FailedRecord) error {
			if err := encoder.Encode(map[string]any{
				"trace_id":       record.TraceID,
				"error_category": record.ErrorCategory,
				"error_summary":  record.ErrorSummary,
				"attempts":       record.Attempts,
			}); err != nil {
				return fmt.Errorf("encode adjudicate failed record: %w", err)
			}
			stats.Failed++
			return nil
		})
	})
	if err != nil {
		return ExportStats{}, fmt.Errorf("stage adjudicate failed export: %w", err)
	}
	defer func() { _ = os.Remove(failedTemp) }()

	if err := publishExportFiles(successTemp, outputPath, failedTemp, failedPath, newExportFileOps()); err != nil {
		return ExportStats{}, err
	}
	return stats, nil
}

func decodeAdjudicateExport(record dao.ExportRecord) (adjudicateInput, dto.Annotation, error) {
	input, err := parseAdjudicateInput(record.RawJSON)
	if err != nil {
		return adjudicateInput{}, dto.Annotation{}, fmt.Errorf("decode adjudicate export source: %w", err)
	}
	var annotation dto.Annotation
	if err := json.Unmarshal(record.Annotation, &annotation); err != nil {
		return adjudicateInput{}, dto.Annotation{}, fmt.Errorf("decode adjudicate export annotation: %w", err)
	}
	return input, annotation, nil
}

// parseAdjudicateInput 解析并校验单行差异输入。
func parseAdjudicateInput(raw []byte) (adjudicateInput, error) {
	var record adjudicateInput
	if err := json.Unmarshal(raw, &record); err != nil {
		return adjudicateInput{}, fmt.Errorf("decode JSON: %w", err)
	}
	if record.ID == "" || len(record.Messages) == 0 || record.OriginalLabel.Label == "" || record.ModelLabel.Label == "" {
		return adjudicateInput{}, fmt.Errorf("required adjudicate fields are missing")
	}
	record.Raw = append(json.RawMessage(nil), raw...)
	return record, nil
}

func adjudicateRunnerConfig(cfg AdjudicateConfig, store *dao.Store) RunnerConfig {
	return RunnerConfig{
		TaskID:               cfg.TaskID,
		SystemPrompt:         cfg.SystemPrompt,
		Scene:                cfg.Scene,
		Schema:               cfg.Schema,
		Mode:                 cfg.Mode,
		MaxOutputTokens:      cfg.MaxTokens,
		RequestMaxAttempts:   adjudicateMaxAttempts(cfg.MaxAttempts),
		FormatRepairAttempts: 0,
		ShutdownTimeout:      cfg.Shutdown,
		Store:                store,
		Completer:            cfg.Completer,
		Validator:            cfg.Validator,
		Limiter:              cfg.Limiter,
		RetryPolicy:          cfg.RetryPolicy,
		OnProgress:           cfg.OnProgress,
		BuildRequest: func(item dao.Item) (dto.CompletionRequest, error) {
			return adjudicateRequest(item, cfg.SystemPrompt, cfg.Schema, cfg.Mode)
		},
	}
}

func adjudicateMaxAttempts(configured int) int {
	if configured > 0 {
		return configured
	}
	return 1
}

// adjudicateRequest 构造只包含裁决必要字段的模型请求。
func adjudicateRequest(
	item dao.Item,
	systemPrompt []byte,
	schema json.RawMessage,
	mode string,
) (dto.CompletionRequest, error) {
	record, err := parseAdjudicateInput(item.RawJSON)
	if err != nil {
		return dto.CompletionRequest{}, fmt.Errorf("parse adjudicate raw record for trace_id %q: %w", item.TraceID, err)
	}
	modelReason, err := adjudicateModelReason(record.Annotation)
	if err != nil {
		return dto.CompletionRequest{}, err
	}
	prompt, err := json.Marshal(adjudicatePrompt{
		ID:            record.ID,
		Messages:      record.Messages,
		OriginalLabel: record.OriginalLabel,
		ModelLabel:    record.ModelLabel,
		ModelJudgment: adjudicateModelJudgment{Explanation: modelReason},
		Reasons: adjudicateJudgmentReasons{
			Original: record.Meta.SourceFields.Reason,
			Model:    modelReason,
		},
	})
	if err != nil {
		return dto.CompletionRequest{}, fmt.Errorf("encode adjudicate prompt: %w", err)
	}
	return dto.CompletionRequest{
		Messages: []dto.Message{
			{Role: "system", Content: string(systemPrompt)},
			{Role: "user", Content: string(prompt)},
		},
		Schema: append(json.RawMessage(nil), schema...),
		Mode:   mode,
	}, nil
}

// adjudicateModelReason 只抽取模型判断理由，避免把完整 annotation 送入裁决模型。
func adjudicateModelReason(raw json.RawMessage) (string, error) {
	var annotation struct {
		Explanation string `json:"explanation"`
	}
	if err := json.Unmarshal(raw, &annotation); err != nil {
		return "", fmt.Errorf("decode adjudicate model judgment: %w", err)
	}
	return annotation.Explanation, nil
}

// buildMASBOutput 将最终裁决结果转换为样本格式 8-4。
func buildMASBOutput(scene string, record adjudicateInput, annotation dto.Annotation) masbOutput {
	prompt, response := adjudicatePromptResponse(record.Messages)
	extended := dto.ExtendedInfo{
		CaseType: annotation.CaseType,
		IsAttack: &annotation.IsAttack,
	}
	if annotation.ExtendedInfo != nil {
		extended.RiskType = annotation.ExtendedInfo.RiskType
		extended.RiskLevel = annotation.ExtendedInfo.RiskLevel
		extended.AttackScenario = annotation.ExtendedInfo.AttackScenario
		extended.Other = annotation.ExtendedInfo.Other
		extended.Extra = annotation.ExtendedInfo.Extra
	}
	return masbOutput{
		TraceID:      record.ID,
		Source:       adjudicateSourceName(record),
		Split:        adjudicateValueOrDefault(record.Meta.Split, "train"),
		Language:     adjudicateValueOrDefault(record.Meta.Language, "zh"),
		Scene:        adjudicateScene(scene, prompt, response),
		Label:        annotationLabel(annotation.IsAttack),
		Prompt:       prompt,
		Response:     response,
		Explanation:  annotation.Explanation,
		ExtendedInfo: extended,
		Annotation:   dto.AnnotationMeta{Method: "auto"},
	}
}

// adjudicatePromptResponse 从消息列表抽取首个 user 和 assistant 内容。
func adjudicatePromptResponse(messages []dto.Message) (string, string) {
	var prompt string
	var response string
	for _, message := range messages {
		switch message.Role {
		case "user":
			if prompt == "" {
				prompt = message.Content
			}
		case "assistant":
			if response == "" {
				response = message.Content
			}
		}
	}
	return prompt, response
}

// adjudicateSourceName 返回 8-4 输出中的来源名称。
func adjudicateSourceName(record adjudicateInput) string {
	if record.Source.Dataset != "" {
		return record.Source.Dataset
	}
	return "sendllm_difference"
}

// adjudicateValueOrDefault 返回非空配置值或默认值。
func adjudicateValueOrDefault(value string, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

// adjudicateScene 按配置或消息形态推导审查场景。
func adjudicateScene(configured string, prompt string, response string) string {
	if configured != "auto" && configured != "" {
		return configured
	}
	if prompt != "" && response != "" {
		return "pair"
	}
	if prompt != "" {
		return "prompt"
	}
	return "response"
}

// annotationLabel 将攻击布尔值转换为 8-4 标签。
func annotationLabel(isAttack bool) string {
	if isAttack {
		return "unsafe"
	}
	return "safe"
}
