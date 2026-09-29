package dao

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// SafetyReviewSummary 表示 Safety Review 任务的聚合状态摘要。
type SafetyReviewSummary struct {
	TaskID     string
	TaskStatus string
	Scene      string
	Items      map[string]int64
	Stages     map[string]int64
	Decisions  int64
}

// SafetyReviewDetailedSummary 表示带时间与错误聚合的状态摘要。
type SafetyReviewDetailedSummary struct {
	TaskID      string
	TaskStatus  string
	Scene       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Items       map[string]int64
	Stages      map[string]int64
	Decisions   int64
	ErrorCounts map[string]int64
}

// SafetyReviewExportItem 表示一条可导出的终态条目。
type SafetyReviewExportItem struct {
	TraceID              string
	InputIndex           int64
	Scene                string
	RawJSON              []byte
	FinalState           string
	Label                string
	IsAttack             bool
	AttackMethods        []byte
	AttackDomains        []byte
	PrimaryAttackMethod  string
	PrimaryAttackDomain  string
	PrimaryRiskType      string
	CaseType             string
	Evidence             []byte
	DecisionRules        []byte
	Rationale            string
	QuarantineReason     string
	DecidedAt            time.Time
	IndependenceDegraded bool
}

// SafetyReviewStageSummary 表示不含载荷的阶段摘要。
type SafetyReviewStageSummary struct {
	TraceID       string
	StageKey      string
	Role          string
	Axis          string
	Category      string
	State         string
	ErrorCategory string
	FallbackIndex int
}

// SafetyReviewStageDetail 表示诊断视图需要的单阶段结构化结果。
type SafetyReviewStageDetail struct {
	TraceID       string
	StageKey      string
	Role          string
	State         string
	ModelProfile  string
	ModelFamily   string
	Fallback      int
	ErrorCategory string
	ResultJSON    []byte
}

// SafetyReviewAttemptSummary 表示不含载荷的尝试摘要。
type SafetyReviewAttemptSummary struct {
	TraceID       string
	StageKey      string
	AttemptKind   string
	ModelProfile  string
	ModelFamily   string
	APIKeyEnv     string
	ErrorCategory string
}

// ReadSummary 返回不包含任何载荷的 Safety Review 任务摘要。
func (s *SafetyReviewStore) ReadSummary(ctx context.Context, taskID string) (SafetyReviewSummary, error) {
	var summary SafetyReviewSummary
	if err := s.db.QueryRowContext(
		ctx,
		"SELECT task_id, status, scene FROM review_tasks WHERE task_id = ?",
		taskID,
	).Scan(&summary.TaskID, &summary.TaskStatus, &summary.Scene); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SafetyReviewSummary{}, fmt.Errorf("load safety review task %q: %w", taskID, ErrSafetyReviewTaskNotFound)
		}
		return SafetyReviewSummary{}, fmt.Errorf("load safety review task %q: %w", taskID, err)
	}

	summary.Items = emptySafetyReviewItemCounts()
	summary.Stages = emptySafetyReviewStageCounts()
	if err := fillSafetyReviewCounts(
		ctx,
		s.db,
		"SELECT state, count(*) FROM review_items WHERE task_id = ? GROUP BY state",
		taskID,
		summary.Items,
	); err != nil {
		return SafetyReviewSummary{}, err
	}
	if err := fillSafetyReviewCounts(
		ctx,
		s.db,
		"SELECT state, count(*) FROM review_stages WHERE task_id = ? GROUP BY state",
		taskID,
		summary.Stages,
	); err != nil {
		return SafetyReviewSummary{}, err
	}
	if err := s.db.QueryRowContext(
		ctx,
		"SELECT count(*) FROM review_decisions WHERE task_id = ?",
		taskID,
	).Scan(&summary.Decisions); err != nil {
		return SafetyReviewSummary{}, fmt.Errorf("count safety review decisions: %w", err)
	}
	return summary, nil
}

// ReadSafetyReviewDetailedSummary 返回带吞吐计算输入的详细摘要。
func (s *SafetyReviewStore) ReadSafetyReviewDetailedSummary(
	ctx context.Context,
	taskID string,
) (SafetyReviewDetailedSummary, error) {
	var summary SafetyReviewDetailedSummary
	var createdAt, updatedAt string
	if err := s.db.QueryRowContext(
		ctx,
		"SELECT task_id, status, scene, created_at, updated_at FROM review_tasks WHERE task_id = ?",
		taskID,
	).Scan(&summary.TaskID, &summary.TaskStatus, &summary.Scene, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SafetyReviewDetailedSummary{}, fmt.Errorf(
				"load safety review task %q: %w", taskID, ErrSafetyReviewTaskNotFound,
			)
		}
		return SafetyReviewDetailedSummary{}, fmt.Errorf("load safety review task %q: %w", taskID, err)
	}
	var err error
	if summary.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return SafetyReviewDetailedSummary{}, fmt.Errorf("parse task created time: %w", err)
	}
	if summary.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt); err != nil {
		return SafetyReviewDetailedSummary{}, fmt.Errorf("parse task updated time: %w", err)
	}
	summary.Items = emptySafetyReviewItemCounts()
	summary.Stages = emptySafetyReviewStageCounts()
	summary.ErrorCounts = make(map[string]int64)
	if err := fillSafetyReviewCounts(
		ctx, s.db,
		"SELECT state, count(*) FROM review_items WHERE task_id = ? GROUP BY state",
		taskID, summary.Items,
	); err != nil {
		return SafetyReviewDetailedSummary{}, err
	}
	if err := fillSafetyReviewCounts(
		ctx, s.db,
		"SELECT state, count(*) FROM review_stages WHERE task_id = ? GROUP BY state",
		taskID, summary.Stages,
	); err != nil {
		return SafetyReviewDetailedSummary{}, err
	}
	if err := s.db.QueryRowContext(
		ctx,
		"SELECT count(*) FROM review_decisions WHERE task_id = ?",
		taskID,
	).Scan(&summary.Decisions); err != nil {
		return SafetyReviewDetailedSummary{}, fmt.Errorf("count safety review decisions: %w", err)
	}
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT COALESCE(error_category, ''), count(*)
		FROM review_attempts
		WHERE task_id = ? AND error_category IS NOT NULL AND error_category != ''
		GROUP BY error_category ORDER BY error_category`,
		taskID,
	)
	if err != nil {
		return SafetyReviewDetailedSummary{}, fmt.Errorf("query safety review errors: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var category string
		var count int64
		if err := rows.Scan(&category, &count); err != nil {
			return SafetyReviewDetailedSummary{}, fmt.Errorf("scan safety review error count: %w", err)
		}
		summary.ErrorCounts[category] = count
	}
	if err := rows.Err(); err != nil {
		return SafetyReviewDetailedSummary{}, fmt.Errorf("iterate safety review errors: %w", err)
	}
	return summary, nil
}

// ReadSafetyReviewTaskID 返回任务目录中的唯一任务 ID。
func (s *SafetyReviewStore) ReadSafetyReviewTaskID(ctx context.Context) (string, error) {
	var taskID string
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM review_tasks").Scan(&count); err != nil {
		return "", fmt.Errorf("count safety review tasks: %w", err)
	}
	if count == 0 || count > 1 {
		return "", fmt.Errorf("read safety review task ID: %w", ErrSafetyReviewTaskNotFound)
	}
	if err := s.db.QueryRowContext(
		ctx,
		"SELECT task_id FROM review_tasks ORDER BY created_at LIMIT 1",
	).Scan(&taskID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("read safety review task ID: %w", ErrSafetyReviewTaskNotFound)
		}
		return "", fmt.Errorf("read safety review task ID: %w", err)
	}
	return taskID, nil
}

// ReadSafetyReviewStageDetails 读取指定任务的阶段诊断结果。
func (s *SafetyReviewStore) ReadSafetyReviewStageDetails(
	ctx context.Context,
	taskID string,
	traceID string,
) ([]SafetyReviewStageDetail, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT trace_id, stage_key, role, state,
		COALESCE(model_profile, ''), COALESCE(model_family, ''), fallback_index,
		COALESCE(error_category, ''), COALESCE(result_json, X'')
		FROM review_stages
		WHERE task_id = ? AND (? = '' OR trace_id = ?)
		ORDER BY trace_id,
			CASE role WHEN 'judge_a' THEN 1 WHEN 'judge_b' THEN 2 WHEN 'router' THEN 3
				WHEN 'expert' THEN 4 ELSE 5 END,
			stage_key`, taskID, traceID, traceID)
	if err != nil {
		return nil, fmt.Errorf("query safety review stage details: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := make([]SafetyReviewStageDetail, 0)
	for rows.Next() {
		var detail SafetyReviewStageDetail
		if err := rows.Scan(
			&detail.TraceID, &detail.StageKey, &detail.Role, &detail.State,
			&detail.ModelProfile, &detail.ModelFamily, &detail.Fallback,
			&detail.ErrorCategory, &detail.ResultJSON,
		); err != nil {
			return nil, fmt.Errorf("scan safety review stage detail: %w", err)
		}
		result = append(result, detail)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate safety review stage details: %w", err)
	}
	return result, nil
}

// ReadSafetyReviewTerminalItems 按输入顺序读取全部终态条目。
func (s *SafetyReviewStore) ReadSafetyReviewTerminalItems(
	ctx context.Context,
	taskID string,
) ([]SafetyReviewExportItem, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT i.trace_id, i.input_index, t.scene, i.raw_json,
			d.final_state, COALESCE(d.label, ''), d.is_attack,
			d.attack_methods_json, d.attack_domains_json,
			COALESCE(d.primary_attack_method, ''), COALESCE(d.primary_attack_domain, ''),
			COALESCE(d.primary_risk_type, ''), COALESCE(d.case_type, ''),
			d.evidence_json, d.decision_rules_json, d.rationale,
			COALESCE(d.quarantine_reason, ''),
			d.decided_at, i.independence_degraded
		FROM review_items i
		JOIN review_decisions d ON d.task_id = i.task_id AND d.trace_id = i.trace_id
		JOIN review_tasks t ON t.task_id = i.task_id
		WHERE i.task_id = ? AND i.state IN ('resolved_safe', 'resolved_unsafe', 'quarantined')
		ORDER BY i.input_index`,
		taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("read safety review terminal items: %w", err)
	}
	defer rows.Close()

	var result []SafetyReviewExportItem
	for rows.Next() {
		var item SafetyReviewExportItem
		var decidedAt string
		var degraded int
		if err := rows.Scan(
			&item.TraceID, &item.InputIndex, &item.Scene, &item.RawJSON,
			&item.FinalState, &item.Label, &item.IsAttack, &item.AttackMethods, &item.AttackDomains,
			&item.PrimaryAttackMethod, &item.PrimaryAttackDomain, &item.PrimaryRiskType, &item.CaseType,
			&item.Evidence, &item.DecisionRules, &item.Rationale, &item.QuarantineReason,
			&decidedAt, &degraded,
		); err != nil {
			return nil, fmt.Errorf("scan safety review terminal item: %w", err)
		}
		decidedTime, err := time.Parse(time.RFC3339Nano, decidedAt)
		if err != nil {
			return nil, fmt.Errorf("parse safety review decision time: %w", err)
		}
		item.DecidedAt = decidedTime
		item.IndependenceDegraded = degraded == 1
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate safety review terminal items: %w", err)
	}
	return result, nil
}

// ReadSafetyReviewStageSummaries 按输入顺序读取阶段摘要。
func (s *SafetyReviewStore) ReadSafetyReviewStageSummaries(
	ctx context.Context,
	taskID string,
) ([]SafetyReviewStageSummary, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT i.trace_id, s.stage_key, s.role, COALESCE(s.axis, ''),
			COALESCE(s.category, ''), s.state, COALESCE(s.error_category, ''), s.fallback_index
		FROM review_stages s
		JOIN review_items i ON i.task_id = s.task_id AND i.trace_id = s.trace_id
		WHERE s.task_id = ?
		ORDER BY i.input_index, s.stage_key`,
		taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("read safety review stage summaries: %w", err)
	}
	defer rows.Close()

	var result []SafetyReviewStageSummary
	for rows.Next() {
		var summary SafetyReviewStageSummary
		if err := rows.Scan(
			&summary.TraceID, &summary.StageKey, &summary.Role, &summary.Axis,
			&summary.Category, &summary.State, &summary.ErrorCategory, &summary.FallbackIndex,
		); err != nil {
			return nil, fmt.Errorf("scan safety review stage summary: %w", err)
		}
		result = append(result, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate safety review stage summaries: %w", err)
	}
	return result, nil
}

// ReadSafetyReviewAttemptSummaries 按输入顺序读取尝试摘要。
func (s *SafetyReviewStore) ReadSafetyReviewAttemptSummaries(
	ctx context.Context,
	taskID string,
) ([]SafetyReviewAttemptSummary, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT i.trace_id, a.stage_key, a.attempt_kind, a.model_profile, a.model_family,
			a.api_key_env, COALESCE(a.error_category, '')
		FROM review_attempts a
		JOIN review_items i ON i.task_id = a.task_id AND i.trace_id = a.trace_id
		WHERE a.task_id = ?
		ORDER BY i.input_index, a.attempt_id`,
		taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("read safety review attempt summaries: %w", err)
	}
	defer rows.Close()

	var result []SafetyReviewAttemptSummary
	for rows.Next() {
		var summary SafetyReviewAttemptSummary
		if err := rows.Scan(
			&summary.TraceID, &summary.StageKey, &summary.AttemptKind,
			&summary.ModelProfile, &summary.ModelFamily, &summary.APIKeyEnv,
			&summary.ErrorCategory,
		); err != nil {
			return nil, fmt.Errorf("scan safety review attempt summary: %w", err)
		}
		result = append(result, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate safety review attempt summaries: %w", err)
	}
	return result, nil
}

// fillSafetyReviewCounts 将 SQL 分组计数填入预初始化的映射。
func fillSafetyReviewCounts(
	ctx context.Context,
	db *sql.DB,
	query string,
	taskID string,
	counts map[string]int64,
) error {
	rows, err := db.QueryContext(ctx, query, taskID)
	if err != nil {
		return fmt.Errorf("query safety review counts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var state string
		var count int64
		if err := rows.Scan(&state, &count); err != nil {
			return fmt.Errorf("scan safety review count: %w", err)
		}
		counts[state] = count
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate safety review counts: %w", err)
	}
	return nil
}

// emptySafetyReviewItemCounts 返回全部条目状态的零值计数。
func emptySafetyReviewItemCounts() map[string]int64 {
	return map[string]int64{
		"pending_initial":  0,
		"awaiting_initial": 0,
		"awaiting_experts": 0,
		"pending_arbiter":  0,
		"resolved_safe":    0,
		"resolved_unsafe":  0,
		"quarantined":      0,
	}
}

// emptySafetyReviewStageCounts 返回全部阶段状态的零值计数。
func emptySafetyReviewStageCounts() map[string]int64 {
	return map[string]int64{
		"pending":         0,
		"running":         0,
		"retry_wait":      0,
		"succeeded":       0,
		"terminal_failed": 0,
		"skipped":         0,
	}
}
