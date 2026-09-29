package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"sendllm/internal/dao"
)

// AdvertisementReviewStats 汇总广告复核分流和批量请求的聚合计数。
type AdvertisementReviewStats struct {
	Clean                        int64            `json:"clean_count"`
	Issues                       int64            `json:"issue_count"`
	Failed                       int64            `json:"failed_count"`
	Uncertain                    int64            `json:"uncertain_count"`
	SafeToUnsafe                 int64            `json:"safe_to_unsafe_count"`
	UnsafeToSafe                 int64            `json:"unsafe_to_safe_count"`
	LegacyOverlapByRisk          map[string]int64 `json:"legacy_overlap_by_risk"`
	ScenarioSuspectByScenario    map[string]int64 `json:"scenario_suspect_by_scenario"`
	ProviderFailures             int64            `json:"provider_failure_count"`
	InvalidResults               int64            `json:"invalid_result_count"`
	Attempts                     int64            `json:"attempt_count"`
	Requests                     int64            `json:"request_count"`
	SuccessfulRequests           int64            `json:"successful_request_count"`
	ItemsInSuccessfulRequests    int64            `json:"items_in_successful_requests"`
	MinItemsPerSuccessfulRequest int64            `json:"min_items_per_successful_request"`
	MaxItemsPerSuccessfulRequest int64            `json:"max_items_per_successful_request"`
	InputTokens                  int64            `json:"input_tokens"`
	OutputTokens                 int64            `json:"output_tokens"`
}

// advertisementManifest 只保存问题池必要且不含载荷的审计字段。
type advertisementManifest struct {
	TraceID             string   `json:"trace_id"`
	Issues              []string `json:"issues"`
	CandidateLegacyRisk string   `json:"candidate_legacy_risk,omitempty"`
}

// advertisementReviewIssueOrder 定义问题代码的稳定输出顺序。
var advertisementReviewIssueOrder = []string{
	"uncertain",
	"label_error",
	"legacy_overlap",
	"scenario_suspect",
	"provider_failure",
	"invalid_result",
}

// ExportAdvertisementReview 按原输入顺序导出四个不含模型载荷的广告复核产物。
func ExportAdvertisementReview(
	ctx context.Context,
	store *dao.Store,
	taskID string,
	cleanPath string,
) (AdvertisementReviewStats, error) {
	return exportAdvertisementReview(ctx, store, taskID, cleanPath, AdvertisementReviewStats{})
}

// advertisementReviewIssues 从源标签和已校验模型结论推导稳定问题代码。
func advertisementReviewIssues(
	source advertisementReviewInput,
	decision advertisementReviewDecision,
) ([]string, string) {
	issues := make([]string, 0, len(advertisementReviewIssueOrder))
	if decision.Label == 0 {
		issues = append(issues, "uncertain")
	}
	if decision.Label != 0 && source.Label != advertisementDecisionLabel(decision.Label) {
		issues = append(issues, "label_error")
	}
	if decision.LegacyRisk != "" {
		issues = append(issues, "legacy_overlap")
	}
	if decision.ScenarioSuspect == 1 {
		issues = append(issues, "scenario_suspect")
	}
	if len(issues) == 0 {
		return nil, decision.LegacyRisk
	}
	return issues, decision.LegacyRisk
}

// advertisementDecisionLabel 转换紧凑标签枚举。
func advertisementDecisionLabel(label int) string {
	if label == 1 {
		return "safe"
	}
	if label == 2 {
		return "unsafe"
	}
	return "uncertain"
}

// advertisementRequestGroup 记录同一成功请求在行级标注中的可恢复统计。
type advertisementRequestGroup struct {
	Items        int64
	InputTokens  int64
	OutputTokens int64
	Rows         int64
}

// exportAdvertisementReview 写出原样分区、问题清单和聚合并原子发布四个文件。
func exportAdvertisementReview(
	ctx context.Context,
	store *dao.Store,
	taskID string,
	cleanPath string,
	requestStats AdvertisementReviewStats,
) (AdvertisementReviewStats, error) {
	stats := requestStats
	stats.LegacyOverlapByRisk = make(map[string]int64)
	stats.ScenarioSuspectByScenario = make(map[string]int64)
	requestGroups := make(map[[3]int64]*advertisementRequestGroup)
	counts, err := store.Counts(ctx, taskID)
	if err != nil {
		return AdvertisementReviewStats{}, fmt.Errorf("count advertisement review terminal rows: %w", err)
	}
	if counts.Pending != 0 || counts.Processing != 0 || counts.RetryWait != 0 {
		return AdvertisementReviewStats{}, fmt.Errorf("%w: advertisement review task is not terminal", ErrInvalidResult)
	}
	outputDir := filepath.Dir(cleanPath)
	cleanTemp, err := stageJSONL(cleanPath, func(encoder *json.Encoder) error {
		return store.ForEachSucceeded(ctx, taskID, func(record dao.ExportRecord) error {
			issues, _, decision, err := advertisementSucceededIssues(dao.ItemLogRecord{
				TraceID:    "",
				RawJSON:    record.RawJSON,
				Annotation: record.Annotation,
			})
			if err != nil {
				return err
			}
			if len(issues) != 0 {
				return nil
			}
			attemptCount := decision.AttemptCount
			if attemptCount == 0 {
				attemptCount = 1
			}
			stats.Attempts += int64(attemptCount)
			if err := encoder.Encode(json.RawMessage(record.RawJSON)); err != nil {
				return fmt.Errorf("encode clean source: %w", err)
			}
			stats.Clean++
			return nil
		})
	})
	if err != nil {
		return AdvertisementReviewStats{}, fmt.Errorf("stage clean advertisement partition: %w", err)
	}
	defer func() { _ = os.Remove(cleanTemp) }()

	issuesPath := filepath.Join(outputDir, "issues.original.jsonl")
	issuesTemp, err := stageJSONL(issuesPath, func(encoder *json.Encoder) error {
		if err := store.ForEachSucceeded(ctx, taskID, func(record dao.ExportRecord) error {
			issues, _, _, err := advertisementSucceededIssues(dao.ItemLogRecord{
				RawJSON:    record.RawJSON,
				Annotation: record.Annotation,
			})
			if err != nil {
				return err
			}
			if len(issues) == 0 {
				return nil
			}
			return encoder.Encode(json.RawMessage(record.RawJSON))
		}); err != nil {
			return err
		}
		return store.ForEachFailed(ctx, taskID, func(record dao.FailedRecord) error {
			if record.ErrorCategory == "invalid_result" {
				stats.InvalidResults++
			} else {
				stats.ProviderFailures++
			}
			stats.Failed++
			stats.Attempts += int64(record.Attempts)
			return encoder.Encode(json.RawMessage(record.RawJSON))
		})
	})
	if err != nil {
		return AdvertisementReviewStats{}, fmt.Errorf("stage issue advertisement partition: %w", err)
	}
	defer func() { _ = os.Remove(issuesTemp) }()

	manifestPath := filepath.Join(outputDir, "issues.manifest.jsonl")
	manifestTemp, err := stageJSONL(manifestPath, func(encoder *json.Encoder) error {
		if err := store.ForEachSucceeded(ctx, taskID, func(record dao.ExportRecord) error {
			issues, candidateRisk, decision, err := advertisementSucceededIssues(dao.ItemLogRecord{
				RawJSON:    record.RawJSON,
				Annotation: record.Annotation,
			})
			if err != nil {
				return err
			}
			key := [3]int64{int64(decision.RequestItems), int64(decision.InputTokens), int64(decision.OutputTokens)}
			group := requestGroups[key]
			if group == nil {
				group = &advertisementRequestGroup{Items: key[0], InputTokens: key[1], OutputTokens: key[2]}
				requestGroups[key] = group
			}
			group.Rows++
			if len(issues) == 0 {
				return nil
			}
			source, err := parseAdvertisementReviewInput(record.RawJSON)
			if err != nil {
				return err
			}
			attemptCount := decision.AttemptCount
			if attemptCount == 0 {
				attemptCount = 1
			}
			stats.Attempts += int64(attemptCount)
			accumulateAdvertisementIssues(&stats, source, issues, candidateRisk)
			return encoder.Encode(advertisementManifest{
				TraceID:             source.TraceID,
				Issues:              issues,
				CandidateLegacyRisk: candidateRisk,
			})
		}); err != nil {
			return err
		}
		return store.ForEachFailed(ctx, taskID, func(record dao.FailedRecord) error {
			issue := "provider_failure"
			if record.ErrorCategory == "invalid_result" {
				issue = "invalid_result"
			}
			stats.Issues++
			return encoder.Encode(advertisementManifest{
				TraceID: record.TraceID,
				Issues:  []string{issue},
			})
		})
	})
	if err != nil {
		return AdvertisementReviewStats{}, fmt.Errorf("stage advertisement issue manifest: %w", err)
	}
	defer func() { _ = os.Remove(manifestTemp) }()

	for _, group := range requestGroups {
		requests := group.Rows / group.Items
		stats.Requests += requests
		stats.SuccessfulRequests += requests
		stats.ItemsInSuccessfulRequests += group.Rows
		stats.InputTokens += requests * group.InputTokens
		stats.OutputTokens += requests * group.OutputTokens
		if stats.MinItemsPerSuccessfulRequest == 0 || group.Items < stats.MinItemsPerSuccessfulRequest {
			stats.MinItemsPerSuccessfulRequest = group.Items
		}
		if group.Items > stats.MaxItemsPerSuccessfulRequest {
			stats.MaxItemsPerSuccessfulRequest = group.Items
		}
	}

	reportPath := filepath.Join(outputDir, "review-report.json")
	reportTemp, err := stageJSONL(reportPath, func(encoder *json.Encoder) error {
		return encoder.Encode(stats)
	})
	if err != nil {
		return AdvertisementReviewStats{}, fmt.Errorf("stage advertisement report: %w", err)
	}
	defer func() { _ = os.Remove(reportTemp) }()

	targets := []*exportTarget{
		{name: "clean", tempPath: cleanTemp, targetPath: cleanPath},
		{name: "issues", tempPath: issuesTemp, targetPath: issuesPath},
		{name: "manifest", tempPath: manifestTemp, targetPath: manifestPath},
		{name: "report", tempPath: reportTemp, targetPath: reportPath},
	}
	if err := publishAdvertisementExport(targets); err != nil {
		return AdvertisementReviewStats{}, err
	}
	return stats, nil
}

// advertisementSucceededIssues 解析成功标注并推导问题代码。
func advertisementSucceededIssues(record dao.ItemLogRecord) ([]string, string, advertisementReviewDecision, error) {
	source, err := parseAdvertisementReviewInput(record.RawJSON)
	if err != nil {
		return nil, "", advertisementReviewDecision{}, fmt.Errorf("decode advertisement source for trace_id %q: %w", record.TraceID, err)
	}
	var decision advertisementReviewDecision
	if err := json.Unmarshal(record.Annotation, &decision); err != nil {
		return nil, "", advertisementReviewDecision{}, fmt.Errorf("decode advertisement decision for trace_id %q: %w", record.TraceID, err)
	}
	if decision.AttemptCount == 0 {
		decision.AttemptCount = 1
	}
	if decision.RequestItems == 0 {
		decision.RequestItems = 1
	}
	issues, risk := advertisementReviewIssues(source, decision)
	return issues, risk, decision, nil
}

// accumulateAdvertisementIssues 更新聚合统计；调用方已确认记录属于问题池。
func accumulateAdvertisementIssues(
	stats *AdvertisementReviewStats,
	source advertisementReviewInput,
	issues []string,
	candidateRisk string,
) {
	stats.Issues++
	for _, issue := range issues {
		switch issue {
		case "uncertain":
			stats.Uncertain++
		case "label_error":
			if source.Label == "safe" {
				stats.SafeToUnsafe++
			} else {
				stats.UnsafeToSafe++
			}
		case "legacy_overlap":
			stats.LegacyOverlapByRisk[candidateRisk]++
		case "scenario_suspect":
			stats.ScenarioSuspectByScenario[source.ExtendedInfo.AttackScenario]++
		}
	}
}

// publishAdvertisementExport 带备份回滚地发布四个最终产物。
func publishAdvertisementExport(targets []*exportTarget) error {
	backupDir, err := os.MkdirTemp(filepath.Dir(targets[0].targetPath), ".advertisement-review-backup-*")
	if err != nil {
		return fmt.Errorf("create advertisement export backup directory: %w", err)
	}
	for index := range targets {
		targets[index].backupPath = filepath.Join(backupDir, targets[index].name)
	}
	for _, target := range targets {
		if err := backupExportTarget(target, newExportFileOps()); err != nil {
			cause := fmt.Errorf("backup %s advertisement export: %w", target.name, err)
			return rollbackExportFiles(cause, targets, backupDir, newExportFileOps())
		}
	}
	for _, target := range targets {
		if err := os.Rename(target.tempPath, target.targetPath); err != nil {
			cause := fmt.Errorf("replace %s advertisement export: %w", target.name, err)
			return rollbackExportFiles(cause, targets, backupDir, newExportFileOps())
		}
		target.published = true
	}
	if err := os.RemoveAll(backupDir); err != nil {
		return fmt.Errorf("remove advertisement export backups: %w", err)
	}
	return nil
}
