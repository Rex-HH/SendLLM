// Package service 提供 Safety Review 的原子导出。
package service

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"sendllm/internal/dao"
)

// SafetyReviewExporterStore 是导出器需要的只读持久化边界。
type SafetyReviewExporterStore interface {
	ReadSafetyReviewDetailedSummary(ctx context.Context, taskID string) (dao.SafetyReviewDetailedSummary, error)
	ReadSafetyReviewTerminalItems(ctx context.Context, taskID string) ([]dao.SafetyReviewExportItem, error)
	ReadSafetyReviewStageSummaries(ctx context.Context, taskID string) ([]dao.SafetyReviewStageSummary, error)
	ReadSafetyReviewAttemptSummaries(ctx context.Context, taskID string) ([]dao.SafetyReviewAttemptSummary, error)
}

// SafetyReviewExporterConfig 指定导出任务的持久化与输出路径。
type SafetyReviewExporterConfig struct {
	TaskID        string
	Store         SafetyReviewExporterStore
	Policy        *SafetyReviewPolicy
	Clean         string
	Quarantine    string
	Audit         string
	QualityEvents string
	Report        string
	RunStatus     string
	Status        string
	Now           func() time.Time
}

// SafetyReviewExportStats 汇总一次导出的产物计数。
type SafetyReviewExportStats struct {
	Clean         int64
	Quarantine    int64
	Audit         int64
	QualityEvents int64
	Report        bool
	RunStatus     bool
}

// SafetyReviewExporter 从 SQLite 生成全部 Safety Review 导出。
type SafetyReviewExporter struct {
	cfg SafetyReviewExporterConfig
	ops safetyReviewFileOps
}

// safetyReviewAtomicFile 抽象可失败注入的临时文件。
type safetyReviewAtomicFile interface {
	Write([]byte) (int, error)
	Sync() error
	Close() error
	Name() string
}

// safetyReviewFileOps 抽象原子写文件操作。
type safetyReviewFileOps struct {
	createTemp func(dir, pattern string) (safetyReviewAtomicFile, error)
	rename     func(oldPath, newPath string) error
	chmod      func(path string, mode os.FileMode) error
	remove     func(path string) error
}

// NewSafetyReviewExporter 校验依赖并构造导出器。
func NewSafetyReviewExporter(cfg SafetyReviewExporterConfig) (*SafetyReviewExporter, error) {
	return newSafetyReviewExporter(cfg, newSafetyReviewFileOps())
}

// newSafetyReviewExporter 使用可注入文件操作构造导出器。
func newSafetyReviewExporter(
	cfg SafetyReviewExporterConfig,
	ops safetyReviewFileOps,
) (*SafetyReviewExporter, error) {
	if cfg.TaskID == "" || cfg.Store == nil || cfg.Policy == nil ||
		cfg.Policy.ReleaseVersion == "" || cfg.Policy.AggregateHash == "" {
		return nil, fmt.Errorf("safety review exporter dependencies are incomplete")
	}
	paths := []string{
		cfg.Clean, cfg.Quarantine, cfg.Audit, cfg.QualityEvents, cfg.Report, cfg.RunStatus,
	}
	for _, path := range paths {
		if path == "" {
			return nil, fmt.Errorf("safety review exporter output path is empty")
		}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if ops.createTemp == nil || ops.rename == nil || ops.chmod == nil || ops.remove == nil {
		return nil, fmt.Errorf("safety review exporter file operations are incomplete")
	}
	return &SafetyReviewExporter{cfg: cfg, ops: ops}, nil
}

// newSafetyReviewFileOps 返回标准库文件操作。
func newSafetyReviewFileOps() safetyReviewFileOps {
	return safetyReviewFileOps{
		createTemp: func(dir, pattern string) (safetyReviewAtomicFile, error) {
			file, err := os.CreateTemp(dir, pattern)
			if err != nil {
				return nil, err
			}
			return file, nil
		},
		rename: os.Rename,
		chmod:  os.Chmod,
		remove: os.Remove,
	}
}

// Export 从 SQLite 生成并原子替换全部导出文件。
func (e *SafetyReviewExporter) Export(ctx context.Context, taskID string) (SafetyReviewExportStats, error) {
	if taskID != e.cfg.TaskID {
		return SafetyReviewExportStats{}, fmt.Errorf("safety review export task mismatch")
	}
	summary, err := e.cfg.Store.ReadSafetyReviewDetailedSummary(ctx, taskID)
	if err != nil {
		return SafetyReviewExportStats{}, fmt.Errorf("read export summary: %w", err)
	}
	items, err := e.cfg.Store.ReadSafetyReviewTerminalItems(ctx, taskID)
	if err != nil {
		return SafetyReviewExportStats{}, fmt.Errorf("read export items: %w", err)
	}
	stages, err := e.cfg.Store.ReadSafetyReviewStageSummaries(ctx, taskID)
	if err != nil {
		return SafetyReviewExportStats{}, fmt.Errorf("read export stages: %w", err)
	}
	attempts, err := e.cfg.Store.ReadSafetyReviewAttemptSummaries(ctx, taskID)
	if err != nil {
		return SafetyReviewExportStats{}, fmt.Errorf("read export attempts: %w", err)
	}

	stageByTrace := groupSafetyReviewStages(stages)
	attemptByTrace := groupSafetyReviewAttempts(attempts)
	var cleanRows, quarantineRows, auditRows, qualityRows []map[string]any
	categoryCounts := make(map[string]int64)
	for _, item := range items {
		countSafetyReviewCategories(categoryCounts, item)
		if item.FinalState == "quarantined" {
			quarantineRows = append(quarantineRows, buildSafetyReviewQuarantineRow(item))
		} else {
			cleanRows = append(cleanRows, buildSafetyReviewCleanRow(item))
		}
		auditRows = append(auditRows, buildSafetyReviewAuditRow(
			item, stageByTrace[item.TraceID], attemptByTrace[item.TraceID],
		))
		qualityRows = append(qualityRows, buildSafetyReviewQualityEvent(
			item, stageByTrace[item.TraceID], e.cfg.Policy,
		))
	}
	status := safetyReviewExportStatus(summary, len(items))
	if e.cfg.Status != "" {
		status = e.cfg.Status
	}
	report := buildSafetyReviewReport(summary, items, categoryCounts, status)
	runStatus := buildSafetyReviewRunStatus(summary, status)

	staged := make([]struct {
		target string
		temp   string
	}, 0, 6)
	defer func() {
		for _, entry := range staged {
			_ = e.ops.remove(entry.temp)
		}
	}()
	stageJSONL := func(target string, rows []map[string]any, counter *int64) error {
		temp, err := e.stageJSONL(target, rows)
		if err != nil {
			return err
		}
		staged = append(staged, struct {
			target string
			temp   string
		}{target: target, temp: temp})
		*counter = int64(len(rows))
		return nil
	}
	var stats SafetyReviewExportStats
	if err := stageJSONL(e.cfg.Clean, cleanRows, &stats.Clean); err != nil {
		return SafetyReviewExportStats{}, fmt.Errorf("stage clean export: %w", err)
	}
	if err := stageJSONL(e.cfg.Quarantine, quarantineRows, &stats.Quarantine); err != nil {
		return SafetyReviewExportStats{}, fmt.Errorf("stage quarantine export: %w", err)
	}
	if err := stageJSONL(e.cfg.Audit, auditRows, &stats.Audit); err != nil {
		return SafetyReviewExportStats{}, fmt.Errorf("stage audit export: %w", err)
	}
	if err := stageJSONL(e.cfg.QualityEvents, qualityRows, &stats.QualityEvents); err != nil {
		return SafetyReviewExportStats{}, fmt.Errorf("stage quality events export: %w", err)
	}
	reportTemp, err := e.stageJSON(e.cfg.Report, report)
	if err != nil {
		return SafetyReviewExportStats{}, fmt.Errorf("stage report export: %w", err)
	}
	staged = append(staged, struct {
		target string
		temp   string
	}{target: e.cfg.Report, temp: reportTemp})
	runStatusTemp, err := e.stageJSON(e.cfg.RunStatus, runStatus)
	if err != nil {
		return SafetyReviewExportStats{}, fmt.Errorf("stage run status export: %w", err)
	}
	staged = append(staged, struct {
		target string
		temp   string
	}{target: e.cfg.RunStatus, temp: runStatusTemp})

	for _, entry := range staged {
		if err := e.ops.rename(entry.temp, entry.target); err != nil {
			return SafetyReviewExportStats{}, fmt.Errorf("publish %s: %w", filepath.Base(entry.target), err)
		}
		if err := e.ops.chmod(entry.target, 0o600); err != nil {
			return SafetyReviewExportStats{}, fmt.Errorf("chmod %s: %w", filepath.Base(entry.target), err)
		}
	}
	stats.Report = true
	stats.RunStatus = true
	return stats, nil
}

// stageJSONL 将对象切片写入同目录临时 JSONL 文件。
func (e *SafetyReviewExporter) stageJSONL(
	target string,
	rows []map[string]any,
) (string, error) {
	return e.stage(target, func(encoder *json.Encoder) error {
		for _, row := range rows {
			if err := encoder.Encode(row); err != nil {
				return fmt.Errorf("encode JSONL row: %w", err)
			}
		}
		return nil
	})
}

// stageJSON 将对象写入同目录临时 JSON 文件。
func (e *SafetyReviewExporter) stageJSON(
	target string,
	value any,
) (string, error) {
	return e.stage(target, func(encoder *json.Encoder) error {
		if err := encoder.Encode(value); err != nil {
			return fmt.Errorf("encode JSON value: %w", err)
		}
		return nil
	})
}

// stage 执行临时文件写入、缓冲、同步与关闭。
func (e *SafetyReviewExporter) stage(
	target string,
	write func(*json.Encoder) error,
) (string, error) {
	directory := filepath.Dir(target)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("create export directory: %w", err)
	}
	file, err := e.ops.createTemp(directory, ".safety-review-*")
	if err != nil {
		return "", fmt.Errorf("create temporary export: %w", err)
	}
	tempPath := file.Name()
	buffered := bufio.NewWriter(file)
	encodeErr := write(json.NewEncoder(buffered))
	if encodeErr == nil {
		encodeErr = buffered.Flush()
	}
	if encodeErr == nil {
		encodeErr = file.Sync()
	}
	closeErr := file.Close()
	if encodeErr == nil {
		encodeErr = closeErr
	}
	if encodeErr != nil {
		_ = e.ops.remove(tempPath)
		return "", encodeErr
	}
	return tempPath, nil
}

// buildSafetyReviewCleanRow 构造 clean.jsonl 的兼容投影行。
func buildSafetyReviewCleanRow(item dao.SafetyReviewExportItem) map[string]any {
	fields := make(map[string]json.RawMessage)
	if err := json.Unmarshal(item.RawJSON, &fields); err != nil {
		fields = make(map[string]json.RawMessage)
	}
	annotation := map[string]any{
		"method":         "auto",
		"label":          item.Label,
		"is_attack":      item.IsAttack,
		"attack_methods": decodeSafetyReviewStringArray(item.AttackMethods),
		"attack_domains": decodeSafetyReviewStringArray(item.AttackDomains),
		"case_type":      item.CaseType,
	}
	if item.PrimaryAttackMethod != "" {
		annotation["attack_method"] = item.PrimaryAttackMethod
	}
	if item.PrimaryAttackDomain != "" {
		annotation["attack_domain"] = item.PrimaryAttackDomain
	}
	if item.PrimaryRiskType != "" {
		annotation["risk_type"] = item.PrimaryRiskType
	}
	encoded, _ := json.Marshal(annotation)
	fields["annotation"] = encoded
	return decodeSafetyReviewRawMap(fields)
}

// buildSafetyReviewQuarantineRow 构造 quarantine.jsonl 行。
func buildSafetyReviewQuarantineRow(item dao.SafetyReviewExportItem) map[string]any {
	fields := make(map[string]json.RawMessage)
	if err := json.Unmarshal(item.RawJSON, &fields); err != nil {
		fields = make(map[string]json.RawMessage)
	}
	annotation := map[string]any{
		"method":            "manual_required",
		"label":             item.Label,
		"is_attack":         item.IsAttack,
		"attack_methods":    decodeSafetyReviewStringArray(item.AttackMethods),
		"attack_domains":    decodeSafetyReviewStringArray(item.AttackDomains),
		"case_type":         item.CaseType,
		"quarantine_reason": item.QuarantineReason,
	}
	encoded, _ := json.Marshal(annotation)
	fields["annotation"] = encoded
	return decodeSafetyReviewRawMap(fields)
}

// buildSafetyReviewAuditRow 构造不含载荷的审计行。
func buildSafetyReviewAuditRow(
	item dao.SafetyReviewExportItem,
	stages []dao.SafetyReviewStageSummary,
	attempts []dao.SafetyReviewAttemptSummary,
) map[string]any {
	return map[string]any{
		"trace_id": item.TraceID, "scene": item.Scene, "final_state": item.FinalState,
		"label": item.Label, "is_attack": item.IsAttack,
		"attack_methods":        decodeSafetyReviewStringArray(item.AttackMethods),
		"attack_domains":        decodeSafetyReviewStringArray(item.AttackDomains),
		"primary_attack_method": item.PrimaryAttackMethod,
		"primary_attack_domain": item.PrimaryAttackDomain,
		"risk_type":             item.PrimaryRiskType, "case_type": item.CaseType,
		"decision_rules":    decodeSafetyReviewStringArray(item.DecisionRules),
		"quarantine_reason": item.QuarantineReason,
		"stages":            buildSafetyReviewStageRows(stages),
		"attempt_count":     len(attempts),
	}
}

// buildSafetyReviewQualityEvent 构造 Policy Optimization 元数据事件。
func buildSafetyReviewQualityEvent(
	item dao.SafetyReviewExportItem,
	stages []dao.SafetyReviewStageSummary,
	policy *SafetyReviewPolicy,
) map[string]any {
	fallbackUsed := false
	for _, stage := range stages {
		if stage.FallbackIndex > 0 {
			fallbackUsed = true
		}
	}
	disagreement := "none"
	if item.QuarantineReason == "category_conflict" {
		disagreement = "category_conflict"
	} else if item.QuarantineReason == "irreducible_uncertainty" {
		disagreement = "uncertainty"
	}
	return map[string]any{
		"trace_id": item.TraceID, "scene": item.Scene,
		"policy_version": policy.ReleaseVersion, "policy_hash": policy.AggregateHash,
		"terminal_state": item.FinalState,
		"categories": map[string][]string{
			"attack_methods": decodeSafetyReviewStringArray(item.AttackMethods),
			"attack_domains": decodeSafetyReviewStringArray(item.AttackDomains),
		},
		"stages": buildSafetyReviewStageRows(stages), "disagreement_type": disagreement,
		"fallback_used": fallbackUsed, "independence_degraded": item.IndependenceDegraded,
		"quarantine_reason": item.QuarantineReason, "error_pattern_ids": []string{},
	}
}

// buildSafetyReviewStageRows 构造阶段摘要行。
func buildSafetyReviewStageRows(stages []dao.SafetyReviewStageSummary) []map[string]any {
	rows := make([]map[string]any, 0, len(stages))
	for _, stage := range stages {
		rows = append(rows, map[string]any{
			"stage_key": stage.StageKey, "role": stage.Role, "axis": stage.Axis,
			"category": stage.Category, "state": stage.State,
			"error_category": stage.ErrorCategory,
		})
	}
	return rows
}

// buildSafetyReviewReport 构造聚合报告。
func buildSafetyReviewReport(
	summary dao.SafetyReviewDetailedSummary,
	items []dao.SafetyReviewExportItem,
	categoryCounts map[string]int64,
	status string,
) map[string]any {
	var clean, quarantine int64
	var total int64
	for _, count := range summary.Items {
		total += count
	}
	for _, item := range items {
		if item.FinalState == "quarantined" {
			quarantine++
			continue
		}
		clean++
	}
	return map[string]any{
		"task_id": summary.TaskID, "status": status, "acceptance_state": "unvalidated",
		"total": total, "clean": clean, "quarantine": quarantine,
		"items": summary.Items, "stages": summary.Stages, "decisions": summary.Decisions,
		"categories": categoryCounts, "errors": summary.ErrorCounts,
	}
}

// buildSafetyReviewRunStatus 构造 run-status JSON。
func buildSafetyReviewRunStatus(
	summary dao.SafetyReviewDetailedSummary,
	status string,
) map[string]any {
	return map[string]any{
		"task_id": summary.TaskID, "status": status, "acceptance_state": "unvalidated",
		"items":  summary.Items,
		"stages": summary.Stages, "decisions": summary.Decisions,
		"errors": summary.ErrorCounts, "updated_at": summary.UpdatedAt,
	}
}

// safetyReviewExportStatus 根据 SQLite 摘要推导导出状态。
func safetyReviewExportStatus(summary dao.SafetyReviewDetailedSummary, terminal int) string {
	if summary.TaskStatus == "failed" || summary.TaskStatus == "interrupted" {
		return summary.TaskStatus
	}
	total := int64(0)
	for _, count := range summary.Items {
		total += count
	}
	if total == 0 || int64(terminal) == total {
		return "completed"
	}
	return "running"
}

// countSafetyReviewCategories 统计最终类别分布。
func countSafetyReviewCategories(counts map[string]int64, item dao.SafetyReviewExportItem) {
	for _, category := range decodeSafetyReviewStringArray(item.AttackMethods) {
		counts[category]++
	}
	for _, category := range decodeSafetyReviewStringArray(item.AttackDomains) {
		counts[category]++
	}
}

// groupSafetyReviewStages 按 trace 聚合阶段摘要。
func groupSafetyReviewStages(
	stages []dao.SafetyReviewStageSummary,
) map[string][]dao.SafetyReviewStageSummary {
	result := make(map[string][]dao.SafetyReviewStageSummary, len(stages))
	for _, stage := range stages {
		result[stage.TraceID] = append(result[stage.TraceID], stage)
	}
	return result
}

// groupSafetyReviewAttempts 按 trace 聚合尝试摘要。
func groupSafetyReviewAttempts(
	attempts []dao.SafetyReviewAttemptSummary,
) map[string][]dao.SafetyReviewAttemptSummary {
	result := make(map[string][]dao.SafetyReviewAttemptSummary, len(attempts))
	for _, attempt := range attempts {
		result[attempt.TraceID] = append(result[attempt.TraceID], attempt)
	}
	return result
}

// decodeSafetyReviewStringArray 解码 JSON 字符串数组并保证非 nil。
func decodeSafetyReviewStringArray(raw []byte) []string {
	result := []string{}
	if len(raw) == 0 {
		return result
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return []string{}
	}
	if result == nil {
		return []string{}
	}
	sort.Strings(result)
	return result
}

// decodeSafetyReviewRawMap 解码 RawMessage map 为通用 map。
func decodeSafetyReviewRawMap(fields map[string]json.RawMessage) map[string]any {
	raw, err := json.Marshal(fields)
	if err != nil {
		return map[string]any{}
	}
	result := map[string]any{}
	if err := json.Unmarshal(raw, &result); err != nil {
		return map[string]any{}
	}
	return result
}
