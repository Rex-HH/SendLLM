package service

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sendllm/internal/dao"
	"sendllm/internal/dto"
	"sendllm/internal/lib/limiter"
)

// ReconcileConfig 指定原始标签复核任务所需依赖。
type ReconcileConfig struct {
	TaskID               string
	InputPath            string
	OutputPath           string
	StatePath            string
	SemanticHash         string
	SystemPrompt         []byte
	Scene                string
	Schema               json.RawMessage
	Mode                 string
	Completer            Completer
	Validator            *Validator
	Limiter              *limiter.Limiter
	MaxTokens            int
	MaxAttempts          int
	FormatRepairAttempts int
	RetryPolicy          RetryPolicy
	Shutdown             time.Duration
	BatchSize            int
	BatchMaxInputTokens  int
	OnProgress           func(Summary)
}

type reconcileInput struct {
	ID       string               `json:"id"`
	Source   reconcileSource      `json:"source"`
	Messages []dto.Message        `json:"messages"`
	Label    reconcileSourceLabel `json:"label"`
	Meta     reconcileMeta        `json:"meta"`
	Raw      json.RawMessage      `json:"-"`
}

type reconcileSource struct {
	Dataset string `json:"dataset"`
	Path    string `json:"path"`
	Index   int64  `json:"index"`
}

type reconcileSourceLabel struct {
	Value     string `json:"value"`
	RiskType  string `json:"risk_type"`
	RiskLevel string `json:"risk_level"`
}

type reconcileMeta struct {
	Split        string `json:"split"`
	Language     string `json:"language"`
	SampleType   string `json:"sample_type"`
	Scenario     string `json:"scenario"`
	SourceFields struct {
		Reason string `json:"reason"`
	} `json:"source_fields"`
}

type reconcileOriginalLabel struct {
	Label        string `json:"label"`
	RiskType     string `json:"risk_type"`
	IsAttack     bool   `json:"is_attack"`
	RiskLevel    string `json:"risk_level"`
	CaseType     string `json:"case_type"`
	AttackMethod string `json:"attack_method"`
	AttackDomain string `json:"attack_domain"`
}

type reconcileMergedAnnotation struct {
	IsAttack       bool
	CaseType       string
	RiskLevel      string
	Explanation    string
	AttackMethod   string
	AttackDomain   string
	AttackScenario string
	QualityScore   *float64
}

type reconcileRunInfo struct {
	RunID     string
	Status    string
	StartedAt time.Time
}

type reconcileLogRecord struct {
	RunID         string                 `json:"run_id"`
	RunStatus     string                 `json:"run_status"`
	InputIndex    int64                  `json:"input_index"`
	TraceID       string                 `json:"trace_id"`
	Status        string                 `json:"status"`
	APIKeyEnv     string                 `json:"api_key_env,omitempty"`
	Changes       []reconcileFieldChange `json:"changes,omitempty"`
	ErrorCategory string                 `json:"error_category,omitempty"`
	ErrorSummary  string                 `json:"error_summary,omitempty"`
	Attempts      int                    `json:"attempts,omitempty"`
}

type reconcileFieldChange struct {
	Field    string `json:"field"`
	Original string `json:"original"`
	Final    string `json:"final"`
}

// Reconcile 读取原始模型标记输入，请模型复核标签并导出 8-4 新标签格式。
func Reconcile(ctx context.Context, cfg ReconcileConfig) (ExportStats, error) {
	if cfg.TaskID == "" || cfg.InputPath == "" || cfg.OutputPath == "" || cfg.StatePath == "" {
		return ExportStats{}, fmt.Errorf("reconcile configuration is incomplete")
	}
	if len(cfg.SystemPrompt) == 0 || len(cfg.Schema) == 0 || cfg.SemanticHash == "" {
		return ExportStats{}, fmt.Errorf("reconcile configuration is incomplete")
	}
	if cfg.Completer == nil || cfg.Validator == nil || cfg.Limiter == nil {
		return ExportStats{}, fmt.Errorf("reconcile dependencies are incomplete")
	}
	if cfg.Shutdown <= 0 {
		return ExportStats{}, fmt.Errorf("reconcile shutdown timeout is invalid")
	}
	runInfo := reconcileRunInfo{
		RunID:     reconcileRunID(time.Now()),
		Status:    "completed",
		StartedAt: time.Now(),
	}

	store, err := dao.Open(ctx, cfg.StatePath)
	if err != nil {
		return ExportStats{}, fmt.Errorf("open reconcile state: %w", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.EnsureTask(ctx, dao.Task{ID: cfg.TaskID, SemanticHash: cfg.SemanticHash}); err != nil {
		return ExportStats{}, fmt.Errorf("ensure reconcile task: %w", err)
	}

	input, err := os.Open(cfg.InputPath)
	if err != nil {
		return ExportStats{}, fmt.Errorf("open reconcile input: %w", err)
	}
	stats, importErr := ImportReconcile(ctx, store, cfg.TaskID, input)
	closeErr := input.Close()
	if importErr != nil {
		return ExportStats{}, importErr
	}
	if closeErr != nil {
		return ExportStats{}, fmt.Errorf("close reconcile input: %w", closeErr)
	}
	if stats.Added > 0 || stats.Skipped > 0 {
		// 导入统计用于确认输入已进入 SQLite，进度仍以状态库为准。
	}

	if _, err := store.ResetFailed(ctx, cfg.TaskID); err != nil {
		return ExportStats{}, fmt.Errorf("reset reconcile failed rows: %w", err)
	}

	runner, err := NewRunner(reconcileRunnerConfig(cfg, store))
	if err != nil {
		return ExportStats{}, fmt.Errorf("create reconcile runner: %w", err)
	}
	if _, err := runner.Run(ctx); err != nil {
		if ctx.Err() != nil {
			runInfo.Status = "interrupted"
		} else {
			runInfo.Status = "failed"
		}
		exportCtx, cancelExport := adjudicateExportContext(ctx, cfg.Shutdown)
		exportStats, exportErr := exportReconcile(exportCtx, store, cfg.TaskID, cfg.OutputPath, cfg.Scene, runInfo)
		cancelExport()
		if exportErr != nil {
			return ExportStats{}, fmt.Errorf("reconcile run failed: %w; export failed: %v", err, exportErr)
		}
		return exportStats, err
	}

	return exportReconcile(ctx, store, cfg.TaskID, cfg.OutputPath, cfg.Scene, runInfo)
}

// ImportReconcile 将原始模型标记输入原子导入 SQLite 状态库。
func ImportReconcile(ctx context.Context, store *dao.Store, taskID string, reader io.Reader) (ImportStats, error) {
	imp, err := store.BeginImport(ctx, taskID)
	if err != nil {
		return ImportStats{}, fmt.Errorf("begin reconcile import: %w", err)
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
			return ImportStats{}, fmt.Errorf("read reconcile line %d: JSONL line exceeds %d bytes", inputIndex, maxJSONLLineSize)
		}
		record, err := parseReconcileInput(raw)
		if err != nil {
			return ImportStats{}, fmt.Errorf("parse reconcile line %d: %w", inputIndex, err)
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
			return ImportStats{}, fmt.Errorf("import reconcile line %d: %w", inputIndex, err)
		}
		if disposition == dao.ImportAdded {
			stats.Added++
			continue
		}
		stats.Skipped++
	}
	if err := scanner.Err(); err != nil {
		return ImportStats{}, fmt.Errorf("read reconcile line %d: %w", inputIndex+1, err)
	}
	if err := imp.Commit(); err != nil {
		return ImportStats{}, fmt.Errorf("commit reconcile import: %w", err)
	}
	return stats, nil
}

// exportReconcile 从 SQLite 终态导出 8-4 新标签结果和单次运行日志。
func exportReconcile(
	ctx context.Context,
	store *dao.Store,
	taskID string,
	outputPath string,
	scene string,
	runInfo reconcileRunInfo,
) (ExportStats, error) {
	var stats ExportStats
	successTemp, err := stageJSONL(outputPath, func(encoder *json.Encoder) error {
		return store.ForEachSucceeded(ctx, taskID, func(record dao.ExportRecord) error {
			input, annotation, err := decodeReconcileExport(record)
			if err != nil {
				return err
			}
			if err := encoder.Encode(buildReconcileOutput(scene, input, annotation)); err != nil {
				return fmt.Errorf("encode reconcile succeeded record: %w", err)
			}
			stats.Succeeded++
			return nil
		})
	})
	if err != nil {
		return ExportStats{}, fmt.Errorf("stage reconcile succeeded export: %w", err)
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
				return fmt.Errorf("encode reconcile failed record: %w", err)
			}
			stats.Failed++
			return nil
		})
	})
	if err != nil {
		return ExportStats{}, fmt.Errorf("stage reconcile failed export: %w", err)
	}
	defer func() { _ = os.Remove(failedTemp) }()

	if err := publishExportFiles(successTemp, outputPath, failedTemp, failedPath, newExportFileOps()); err != nil {
		return ExportStats{}, err
	}
	if err := writeReconcileLog(ctx, store, taskID, outputPath, runInfo); err != nil {
		return ExportStats{}, err
	}
	return stats, nil
}

func reconcileRunID(startedAt time.Time) string {
	return "reconcile-" + startedAt.UTC().Format("20060102T150405.000000000Z")
}

func reconcileLogPath(outputPath string) string {
	if strings.HasSuffix(outputPath, ".jsonl") {
		return strings.TrimSuffix(outputPath, ".jsonl") + ".reconcile-log.jsonl"
	}
	return outputPath + ".reconcile-log.jsonl"
}

func writeReconcileLog(
	ctx context.Context,
	store *dao.Store,
	taskID string,
	outputPath string,
	runInfo reconcileRunInfo,
) error {
	targetPath := reconcileLogPath(outputPath)
	temporary, err := os.CreateTemp(filepath.Dir(targetPath), ".reconcile-log-*.jsonl")
	if err != nil {
		return fmt.Errorf("create reconcile log temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	remove := true
	defer func() {
		if temporary != nil {
			_ = temporary.Close()
		}
		if remove {
			_ = os.Remove(temporaryPath)
		}
	}()

	encoder := json.NewEncoder(temporary)
	if err := store.ForEachItemLog(ctx, taskID, func(record dao.ItemLogRecord) error {
		line, err := buildReconcileLogLine(record, runInfo)
		if err != nil {
			return err
		}
		return encoder.Encode(line)
	}); err != nil {
		return fmt.Errorf("write reconcile log: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync reconcile log: %w", err)
	}
	if err := temporary.Close(); err != nil {
		temporary = nil
		return fmt.Errorf("close reconcile log: %w", err)
	}
	temporary = nil
	remove = false
	if err := os.Rename(temporaryPath, targetPath); err != nil {
		return fmt.Errorf("replace reconcile log: %w", err)
	}
	return nil
}

func buildReconcileLogLine(record dao.ItemLogRecord, runInfo reconcileRunInfo) (reconcileLogRecord, error) {
	line := reconcileLogRecord{
		RunID:      runInfo.RunID,
		RunStatus:  runInfo.Status,
		InputIndex: record.InputIndex,
		TraceID:    record.TraceID,
		Status:     string(record.State),
		APIKeyEnv:  record.APIKeyEnv,
	}

	if record.State != dao.ItemSucceeded {
		line.ErrorCategory = record.ErrorCategory
		line.ErrorSummary = record.ErrorSummary
		line.Attempts = record.Attempts
		return line, nil
	}

	input, annotation, err := decodeReconcileExport(dao.ExportRecord{
		RawJSON:    record.RawJSON,
		Annotation: record.Annotation,
	})
	if err != nil {
		return reconcileLogRecord{}, err
	}
	original := mappedReconcileLabel(input)
	model := modelReconcileLabel(annotation)
	merged := mergeReconcileAnnotation(input, original, annotation)
	changes := reconcileFieldChanges(input, original, model, merged)
	if len(changes) == 0 {
		line.Status = "succeeded_consistent"
		return line, nil
	}

	line.Status = "succeeded_changed"
	line.Changes = changes
	return line, nil
}

func reconcileFieldChanges(
	input reconcileInput,
	original reconcileOriginalLabel,
	model reconcileModelLabel,
	merged reconcileMergedAnnotation,
) []reconcileFieldChange {
	riskTypeMatches := reconcileRiskTypeMatches(original, model)
	changes := make([]reconcileFieldChange, 0, 5)

	if !riskTypeMatches {
		changes = append(changes, reconcileFieldChange{
			Field:    "risk_type",
			Original: input.Label.RiskType,
			Final:    reconcileRiskLabelsValue(model.AttackMethod, model.AttackDomain),
		})
	}
	if original.RiskLevel != model.RiskLevel {
		changes = append(changes, reconcileFieldChange{
			Field:    "risk_level",
			Original: original.RiskLevel,
			Final:    model.RiskLevel,
		})
	}
	if original.CaseType != model.CaseType {
		changes = append(changes, reconcileFieldChange{
			Field:    "case_type",
			Original: original.CaseType,
			Final:    model.CaseType,
		})
	}
	if original.IsAttack != model.IsAttack {
		changes = append(changes, reconcileFieldChange{
			Field:    "is_attack",
			Original: strconvBool(original.IsAttack),
			Final:    strconvBool(model.IsAttack),
		})
	}

	if input.Meta.SourceFields.Reason != merged.Explanation {
		changes = append(changes, reconcileFieldChange{
			Field:    "explanation",
			Original: input.Meta.SourceFields.Reason,
			Final:    merged.Explanation,
		})
	}
	return changes
}

func reconcileRiskLabelsValue(method, domain string) string {
	if method != "" && domain != "" {
		return "attack_method=" + method + ";attack_domain=" + domain
	}
	if method != "" {
		return "attack_method=" + method
	}
	if domain != "" {
		return "attack_domain=" + domain
	}
	return ""
}

func strconvBool(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func parseReconcileInput(raw []byte) (reconcileInput, error) {
	var record reconcileInput
	if err := json.Unmarshal(raw, &record); err != nil {
		return reconcileInput{}, fmt.Errorf("decode JSON: %w", err)
	}
	if record.ID == "" || len(record.Messages) == 0 || record.Label.Value == "" {
		return reconcileInput{}, fmt.Errorf("required reconcile fields are missing")
	}
	record.Raw = append(json.RawMessage(nil), raw...)
	return record, nil
}

func reconcileRunnerConfig(cfg ReconcileConfig, store *dao.Store) RunnerConfig {
	return RunnerConfig{
		TaskID:               cfg.TaskID,
		SystemPrompt:         cfg.SystemPrompt,
		Scene:                cfg.Scene,
		Schema:               cfg.Schema,
		Mode:                 cfg.Mode,
		MaxOutputTokens:      cfg.MaxTokens,
		RequestMaxAttempts:   adjudicateMaxAttempts(cfg.MaxAttempts),
		FormatRepairAttempts: cfg.FormatRepairAttempts,
		ShutdownTimeout:      cfg.Shutdown,
		Store:                store,
		Completer:            cfg.Completer,
		Validator:            cfg.Validator,
		Limiter:              cfg.Limiter,
		RetryPolicy:          cfg.RetryPolicy,
		OnProgress:           cfg.OnProgress,
		BuildRequest: func(item dao.Item) (dto.CompletionRequest, error) {
			return reconcileRequest(item, cfg)
		},
	}
}

func reconcileRequest(item dao.Item, cfg ReconcileConfig) (dto.CompletionRequest, error) {
	input, err := parseReconcileInput(item.RawJSON)
	if err != nil {
		return dto.CompletionRequest{}, fmt.Errorf("parse reconcile raw record for trace_id %q: %w", item.TraceID, err)
	}
	original := mappedReconcileLabel(input)
	prompt, response := adjudicatePromptResponse(input.Messages)
	payload := struct {
		TraceID             string                 `json:"trace_id"`
		Scene               string                 `json:"scene"`
		Prompt              string                 `json:"prompt"`
		Response            string                 `json:"response"`
		OriginalLabel       reconcileOriginalLabel `json:"original_label"`
		OriginalExplanation string                 `json:"original_explanation"`
	}{
		TraceID:             input.ID,
		Scene:               reconcileInputScene(cfg.Scene, prompt, response),
		Prompt:              prompt,
		Response:            response,
		OriginalLabel:       original,
		OriginalExplanation: input.Meta.SourceFields.Reason,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return dto.CompletionRequest{}, fmt.Errorf("encode reconcile model input: %w", err)
	}
	return dto.CompletionRequest{
		Messages: []dto.Message{
			{Role: "system", Content: string(cfg.SystemPrompt)},
			{Role: "user", Content: string(encoded)},
		},
		Schema: append(json.RawMessage(nil), cfg.Schema...),
		Mode:   cfg.Mode,
	}, nil
}

func reconcileInputScene(configured, prompt, response string) string {
	return dto.SourceSample{
		Prompt:   prompt,
		Response: response,
	}.ModelInput(configured).Scene
}

func decodeReconcileExport(record dao.ExportRecord) (reconcileInput, dto.Annotation, error) {
	input, err := parseReconcileInput(record.RawJSON)
	if err != nil {
		return reconcileInput{}, dto.Annotation{}, fmt.Errorf("decode reconcile export source: %w", err)
	}
	var annotation dto.Annotation
	if err := json.Unmarshal(record.Annotation, &annotation); err != nil {
		return reconcileInput{}, dto.Annotation{}, fmt.Errorf("decode reconcile export annotation: %w", err)
	}
	return input, annotation, nil
}

func buildReconcileOutput(scene string, record reconcileInput, annotation dto.Annotation) masbOutput {
	prompt, response := adjudicatePromptResponse(record.Messages)
	original := mappedReconcileLabel(record)
	merged := mergeReconcileAnnotation(record, original, annotation)
	extended := dto.ExtendedInfo{
		CaseType:       merged.CaseType,
		IsAttack:       &merged.IsAttack,
		RiskLevel:      merged.RiskLevel,
		AttackMethod:   merged.AttackMethod,
		AttackDomain:   merged.AttackDomain,
		AttackScenario: merged.AttackScenario,
	}
	return masbOutput{
		TraceID:      record.ID,
		Source:       reconcileSourceName(record),
		Split:        adjudicateValueOrDefault(record.Meta.Split, "train"),
		Language:     adjudicateValueOrDefault(record.Meta.Language, "zh"),
		Scene:        adjudicateScene(scene, prompt, response),
		Label:        annotationLabel(merged.IsAttack),
		Prompt:       prompt,
		Response:     response,
		Explanation:  merged.Explanation,
		ExtendedInfo: extended,
		Annotation: dto.AnnotationMeta{
			Method:       "auto",
			QualityScore: merged.QualityScore,
		},
	}
}

func mergeReconcileAnnotation(
	record reconcileInput,
	original reconcileOriginalLabel,
	annotation dto.Annotation,
) reconcileMergedAnnotation {
	model := modelReconcileLabel(annotation)
	riskTypeMatches := reconcileRiskTypeMatches(original, model)
	allFieldsAgree := original.IsAttack == model.IsAttack &&
		original.CaseType == model.CaseType &&
		original.RiskLevel == model.RiskLevel &&
		riskTypeMatches

	merged := reconcileMergedAnnotation{
		IsAttack:       model.IsAttack,
		CaseType:       model.CaseType,
		RiskLevel:      model.RiskLevel,
		Explanation:    model.Explanation,
		AttackMethod:   model.AttackMethod,
		AttackDomain:   model.AttackDomain,
		AttackScenario: model.AttackScenario,
		QualityScore:   annotation.QualityScore,
	}

	if original.IsAttack == model.IsAttack {
		merged.IsAttack = original.IsAttack
	}
	if original.CaseType == model.CaseType {
		merged.CaseType = original.CaseType
	}
	if original.RiskLevel == model.RiskLevel {
		merged.RiskLevel = original.RiskLevel
	}

	if riskTypeMatches {
		if original.AttackMethod != "" && merged.AttackMethod == "" {
			merged.AttackMethod = original.AttackMethod
		}
		if original.AttackDomain != "" && merged.AttackDomain == "" {
			merged.AttackDomain = original.AttackDomain
		}
	}

	if allFieldsAgree && record.Meta.SourceFields.Reason != "" {
		merged.Explanation = record.Meta.SourceFields.Reason
	}

	return merged
}

type reconcileModelLabel struct {
	IsAttack       bool
	RiskLevel      string
	CaseType       string
	AttackMethod   string
	AttackDomain   string
	AttackScenario string
	Explanation    string
}

func modelReconcileLabel(annotation dto.Annotation) reconcileModelLabel {
	label := reconcileModelLabel{
		IsAttack:    annotation.IsAttack,
		CaseType:    annotation.CaseType,
		Explanation: annotation.Explanation,
	}
	if annotation.ExtendedInfo != nil {
		label.RiskLevel = annotation.ExtendedInfo.RiskLevel
		label.AttackMethod = annotation.ExtendedInfo.AttackMethod
		label.AttackDomain = annotation.ExtendedInfo.AttackDomain
		label.AttackScenario = annotation.ExtendedInfo.AttackScenario
	}
	return label
}

func mappedReconcileLabel(record reconcileInput) reconcileOriginalLabel {
	var attackMethod string
	var attackDomain string
	if record.Label.Value == "unsafe" {
		attackMethod, attackDomain = reconcileRiskLabels(record.Source.Path)
	}
	return reconcileOriginalLabel{
		Label:        record.Label.Value,
		RiskType:     record.Label.RiskType,
		IsAttack:     record.Label.Value == "unsafe",
		RiskLevel:    record.Label.RiskLevel,
		CaseType:     reconcileCaseType(record.Meta.SampleType),
		AttackMethod: attackMethod,
		AttackDomain: attackDomain,
	}
}

func reconcileRiskTypeMatches(original reconcileOriginalLabel, model reconcileModelLabel) bool {
	if original.AttackMethod != "" {
		return original.AttackMethod == model.AttackMethod
	}
	if original.AttackDomain != "" {
		return original.AttackDomain == model.AttackDomain
	}
	return original.AttackMethod == "" &&
		original.AttackDomain == "" &&
		model.AttackMethod == "" &&
		model.AttackDomain == ""
}

func reconcileRiskLabels(path string) (string, string) {
	switch {
	case strings.Contains(path, "/in-1-prompt-injection/"):
		return "prompt_injection", ""
	case strings.Contains(path, "/in-2-jailbreak/"):
		return "jailbreak", ""
	case strings.Contains(path, "/in-3-encoding_obfusaction_attack/"):
		return "encoding_obfuscation", ""
	case strings.Contains(path, "/in-4-cross_language_input_attack/"):
		return "cross_language_attack", ""
	case strings.Contains(path, "/in-5-cross_modal_attack/"):
		return "cross_modal_attack", ""
	case strings.Contains(path, "/in-5-multimodal/"):
		return "cross_modal_attack", ""
	case strings.Contains(path, "/in-6-multi_turn_jailbreak/"):
		return "multi_turn_jailbreak", ""
	case strings.Contains(path, "/in-7-business_application_manual/"):
		return "financial_domain_attack", ""
	default:
		return "", reconcileAttackDomain(path)
	}
}

func reconcileAttackDomain(path string) string {
	domains := []struct {
		fragment string
		value    string
	}{
		{"out-A.1-a-subversion_and_overthrow", "subversion_of_state_power"},
		{"out-A.1-b-national_security_interests_and_image_harm", "harm_to_national_security_or_image"},
		{"out-A.1-c-secession_unity_and_stability_harm", "separatism_and_social_instability"},
		{"out-A.1-d-terrorism_and_extremism_promotion", "terrorism_or_extremism"},
		{"out-A.1-e-national_hatred_promotion", "ethnic_hatred"},
		{"out-A.1-f-violence_and_pornography_promotion", "violence_or_explicit_content"},
		{"out-A.1-g-false_harmful_information", "harmful_misinformation"},
		{"out-A.1-h-other_illegal_prohibited_content", "other_illegal_or_prohibited_content"},
		{"out-A.2-a-ethnic_discrimination", "ethnic_discrimination"},
		{"out-A.2-b-belief_discrimination", "religious_discrimination"},
		{"out-A.2-c-nationality_discrimination", "nationality_discrimination"},
		{"out-A.2-d-regional_discrimination", "regional_discrimination"},
		{"out-A.2-e-gender_discrimination", "gender_discrimination"},
		{"out-A.2-f-age_discrimination", "age_discrimination"},
		{"out-A.2-g-occupational_discrimination", "occupation_discrimination"},
		{"out-A.2-h-health_discrimination", "health_discrimination"},
		{"out-A.2-i-other_discrimination", "other_discrimination"},
		{"out-A.3-a-intellectual_property_infringement", "intellectual_property_infringement"},
		{"out-A.3-b-business_ethics_violation", "business_ethics_violation"},
		{"out-A.3-c-trade_secret_disclosure", "trade_secret_leakage"},
		{"out-A.3-d-algorithmic_unfair_competition", "algorithmic_unfair_competition"},
		{"out-A.3-e-other_business_illegal_violations", "other_business_illegal_activity"},
		{"out-A.4-a-harm_to_others_physical_and_mental_health", "harm_to_physical_or_mental_health"},
		{"out-A.4-b-portrait_right_infringement", "portrait_right_infringement"},
		{"out-A.4-c-reputation_right_infringement", "reputation_right_infringement"},
		{"out-A.4-d-honor_right_infringement", "honor_right_infringement"},
		{"out-A.4-e-privacy_right_infringement", "privacy_right_infringement"},
		{"out-A.4-f-personal_information_rights_infringement", "personal_data_rights_infringement"},
		{"out-A.4-g-other_lawful_rights_infringement", "other_legal_rights_infringement"},
		{"out-A.5-a-content_inaccuracy", "factual_inaccuracy"},
		{"out-A.5-b-content_unreliability", "unreliable_content"},
	}
	for _, domain := range domains {
		if strings.Contains(path, domain.fragment) {
			return domain.value
		}
	}
	return ""
}

func reconcileCaseType(sampleType string) string {
	switch sampleType {
	case "硬负例":
		return "hard_negative"
	case "边界正例", "边界负例", "困难负例":
		return "borderline"
	case "对抗正例", "对抗负例":
		return "variant"
	default:
		return "typical"
	}
}

func reconcileSourceName(record reconcileInput) string {
	if record.Source.Dataset != "" {
		return record.Source.Dataset
	}
	return "sendllm_reconcile"
}
