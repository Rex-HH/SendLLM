package dao

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// SafetyReviewStageOutcome 表示阶段完成后的目标状态。
type SafetyReviewStageOutcome string

const (
	// SafetyReviewStageSucceeded 表示阶段成功完成。
	SafetyReviewStageSucceeded SafetyReviewStageOutcome = "succeeded"
	// SafetyReviewStageTerminalFailed 表示阶段进入终态失败。
	SafetyReviewStageTerminalFailed SafetyReviewStageOutcome = "terminal_failed"
	// SafetyReviewStageRetryWait 表示阶段进入等待重试状态。
	SafetyReviewStageRetryWait SafetyReviewStageOutcome = "retry_wait"
)

// SafetyReviewClaim 表示一次按角色领取 Safety Review 阶段的请求。
type SafetyReviewClaim struct {
	TaskID        string
	Role          string
	Now           time.Time
	ModelProfile  string
	ModelFamily   string
	FallbackIndex int
}

// SafetyReviewStageWork 表示已被领取的 Safety Review 阶段。
type SafetyReviewStageWork struct {
	TaskID        string
	TraceID       string
	StageKey      string
	Role          string
	Axis          string
	Category      string
	Scene         string
	InputIndex    int64
	Prompt        string
	Response      string
	RawJSON       []byte
	ModelProfile  string
	ModelFamily   string
	FallbackIndex int
	StartedAt     time.Time
}

// SafetyReviewInitialStageResult 表示一条记录的初始阶段持久化快照。
type SafetyReviewInitialStageResult struct {
	StageKey             string
	Role                 string
	Axis                 string
	Category             string
	State                string
	ResultJSON           []byte
	IndependenceDegraded bool
}

// SafetyReviewAttempt 表示一次 Safety Review 模型调用审计记录。
type SafetyReviewAttempt struct {
	AttemptKind      string
	ModelProfile     string
	ModelFamily      string
	APIKeyEnv        string
	StartedAt        time.Time
	FinishedAt       time.Time
	HTTPStatus       int
	FinishReason     string
	ErrorCategory    string
	ErrorSummary     string
	Retryable        bool
	PromptTokens     int
	CompletionTokens int
	RawResponse      []byte
	ValidationError  []byte
}

// SafetyReviewRecordedAttempt 表示带阶段身份的一次模型调用尝试。
type SafetyReviewRecordedAttempt struct {
	TaskID   string
	TraceID  string
	StageKey string
	Attempt  SafetyReviewAttempt
}

// RecordSafetyReviewAttempt 持久化一次模型调用尝试的安全审计记录。
func (s *SafetyReviewStore) RecordSafetyReviewAttempt(
	ctx context.Context,
	attempt SafetyReviewRecordedAttempt,
) error {
	if attempt.TaskID == "" || attempt.TraceID == "" || attempt.StageKey == "" {
		return fmt.Errorf("safety review attempt identity is empty")
	}
	if _, err := s.db.ExecContext(
		ctx,
		`INSERT INTO review_attempts (
			task_id, trace_id, stage_key, attempt_kind, model_profile, model_family, api_key_env,
			started_at, finished_at, http_status, finish_reason, error_category, error_summary,
			retryable, prompt_tokens, completion_tokens, raw_response, validation_error
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		attempt.TaskID,
		attempt.TraceID,
		attempt.StageKey,
		attempt.Attempt.AttemptKind,
		attempt.Attempt.ModelProfile,
		attempt.Attempt.ModelFamily,
		attempt.Attempt.APIKeyEnv,
		formatNextAttemptAt(attempt.Attempt.StartedAt),
		formatNextAttemptAt(attempt.Attempt.FinishedAt),
		attempt.Attempt.HTTPStatus,
		attempt.Attempt.FinishReason,
		attempt.Attempt.ErrorCategory,
		attempt.Attempt.ErrorSummary,
		attempt.Attempt.Retryable,
		attempt.Attempt.PromptTokens,
		attempt.Attempt.CompletionTokens,
		attempt.Attempt.RawResponse,
		attempt.Attempt.ValidationError,
	); err != nil {
		return fmt.Errorf("insert safety review attempt: %w", err)
	}
	return nil
}

// SafetyReviewStageSpec 表示需要创建的下游阶段。
type SafetyReviewStageSpec struct {
	StageKey string
	Role     string
	Axis     string
	Category string
}

// SafetyReviewDecisionRecord 表示一条最终 Safety Review 决策。
type SafetyReviewDecisionRecord struct {
	FinalState          string
	Label               string
	IsAttack            bool
	AttackMethods       []byte
	AttackDomains       []byte
	PrimaryAttackMethod string
	PrimaryAttackDomain string
	PrimaryRiskType     string
	CaseType            string
	Evidence            []byte
	DecisionRules       []byte
	Rationale           string
	QuarantineReason    string
	DecidedAt           time.Time
}

// SafetyReviewStageCompletion 表示一次阶段完成事务的输入。
type SafetyReviewStageCompletion struct {
	TaskID               string
	TraceID              string
	StageKey             string
	Outcome              SafetyReviewStageOutcome
	ResultJSON           []byte
	ErrorCategory        string
	ErrorSummary         string
	NextAttemptAt        time.Time
	Attempt              SafetyReviewAttempt
	DownstreamStages     []SafetyReviewStageSpec
	ItemState            string
	Decision             *SafetyReviewDecisionRecord
	IndependenceDegraded bool
}

// ClaimStage 原子领取指定角色的待处理或到期重试阶段。
func (s *SafetyReviewStore) ClaimStage(
	ctx context.Context,
	claim SafetyReviewClaim,
) (SafetyReviewStageWork, bool, error) {
	if claim.TaskID == "" || claim.Role == "" || claim.ModelProfile == "" ||
		claim.ModelFamily == "" || claim.FallbackIndex < 0 || claim.Now.IsZero() {
		return SafetyReviewStageWork{}, false, fmt.Errorf("invalid safety review claim")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SafetyReviewStageWork{}, false, fmt.Errorf("begin safety review claim: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var work SafetyReviewStageWork
	var axis, category sql.NullString
	if err := tx.QueryRowContext(
		ctx,
		`SELECT s.task_id, s.trace_id, s.stage_key, s.role, s.axis, s.category,
			t.scene, i.input_index, i.prompt, i.response, i.raw_json
		FROM review_stages s
		JOIN review_items i ON i.task_id = s.task_id AND i.trace_id = s.trace_id
		JOIN review_tasks t ON t.task_id = i.task_id
		WHERE s.task_id = ? AND s.role = ? AND (
			s.state = 'pending' OR (s.state = 'retry_wait' AND s.next_attempt_at <= ?)
		)
		ORDER BY i.input_index
		LIMIT 1`,
		claim.TaskID,
		claim.Role,
		formatNextAttemptAt(claim.Now),
	).Scan(
		&work.TaskID,
		&work.TraceID,
		&work.StageKey,
		&work.Role,
		&axis,
		&category,
		&work.Scene,
		&work.InputIndex,
		&work.Prompt,
		&work.Response,
		&work.RawJSON,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SafetyReviewStageWork{}, false, nil
		}
		return SafetyReviewStageWork{}, false, fmt.Errorf("select safety review claim: %w", err)
	}
	if axis.Valid {
		work.Axis = axis.String
	}
	if category.Valid {
		work.Category = category.String
	}

	startedAt := time.Now().UTC()
	result, err := tx.ExecContext(
		ctx,
		`UPDATE review_stages
		SET state = 'running', model_profile = ?, model_family = ?, fallback_index = ?,
			started_at = ?, next_attempt_at = NULL, updated_at = ?
		WHERE task_id = ? AND trace_id = ? AND stage_key = ?
			AND state IN ('pending', 'retry_wait')`,
		claim.ModelProfile,
		claim.ModelFamily,
		claim.FallbackIndex,
		formatNextAttemptAt(startedAt),
		formatNextAttemptAt(startedAt),
		claim.TaskID,
		work.TraceID,
		work.StageKey,
	)
	if err != nil {
		return SafetyReviewStageWork{}, false, fmt.Errorf("claim safety review stage: %w", err)
	}
	if rows, err := result.RowsAffected(); err != nil || rows != 1 {
		return SafetyReviewStageWork{}, false, fmt.Errorf("claim safety review stage: rows = %d, err = %v", rows, err)
	}
	if work.Role == "judge_a" || work.Role == "judge_b" || work.Role == "router" {
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE review_items SET state = 'awaiting_initial', updated_at = ?
			WHERE task_id = ? AND trace_id = ? AND state = 'pending_initial'`,
			formatNextAttemptAt(startedAt),
			claim.TaskID,
			work.TraceID,
		); err != nil {
			return SafetyReviewStageWork{}, false, fmt.Errorf("advance safety review item: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return SafetyReviewStageWork{}, false, fmt.Errorf("commit safety review claim: %w", err)
	}
	work.ModelProfile = claim.ModelProfile
	work.ModelFamily = claim.ModelFamily
	work.FallbackIndex = claim.FallbackIndex
	work.StartedAt = startedAt
	return work, true, nil
}

// ReadRoleStageResults 读取一条记录中指定角色的阶段快照。
func (s *SafetyReviewStore) ReadRoleStageResults(
	ctx context.Context,
	taskID string,
	traceID string,
	role string,
) ([]SafetyReviewInitialStageResult, error) {
	if taskID == "" || traceID == "" || role == "" {
		return nil, fmt.Errorf("safety review stage identity is empty")
	}
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT stage_key, role, axis, category, state, result_json
		FROM review_stages
		WHERE task_id = ? AND trace_id = ? AND role = ?
		ORDER BY stage_key`,
		taskID,
		traceID,
		role,
	)
	if err != nil {
		return nil, fmt.Errorf("read safety review %s stages: %w", role, err)
	}
	defer rows.Close()

	var results []SafetyReviewInitialStageResult
	for rows.Next() {
		var result SafetyReviewInitialStageResult
		var axis, category sql.NullString
		var raw []byte
		if err := rows.Scan(&result.StageKey, &result.Role, &axis, &category, &result.State, &raw); err != nil {
			return nil, fmt.Errorf("scan safety review %s stage: %w", role, err)
		}
		if axis.Valid {
			result.Axis = axis.String
		}
		if category.Valid {
			result.Category = category.String
		}
		result.ResultJSON = append([]byte(nil), raw...)
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate safety review %s stages: %w", role, err)
	}
	return results, nil
}

// CompleteStage 在一个事务内写入尝试、阶段结果、下游阶段和最终决策。
func (s *SafetyReviewStore) CompleteStage(ctx context.Context, completion SafetyReviewStageCompletion) error {
	if err := validateSafetyReviewCompletion(completion); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin safety review completion: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var currentState, currentRole string
	if err := tx.QueryRowContext(
		ctx,
		`SELECT state, role FROM review_stages
		WHERE task_id = ? AND trace_id = ? AND stage_key = ?`,
		completion.TaskID,
		completion.TraceID,
		completion.StageKey,
	).Scan(&currentState, &currentRole); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("load safety review stage %q: %w", completion.StageKey, ErrInvalidTransition)
		}
		return fmt.Errorf("load safety review stage %q: %w", completion.StageKey, err)
	}
	if currentState != "running" {
		return fmt.Errorf(
			"complete safety review stage %q from %q: %w",
			completion.StageKey,
			currentState,
			ErrInvalidTransition,
		)
	}
	if completion.Decision != nil && currentRole != "arbiter" &&
		!isSafetyReviewInitialSafeShortcut(currentRole, completion) {
		return fmt.Errorf("safety review decision for non-arbiter stage %q: %w", completion.StageKey, ErrInvalidTransition)
	}
	if err := insertSafetyReviewAttempt(ctx, tx, completion); err != nil {
		return err
	}
	if err := updateSafetyReviewStage(ctx, tx, completion); err != nil {
		return err
	}
	if completion.IndependenceDegraded {
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE review_items SET independence_degraded = 1, updated_at = ?
				WHERE task_id = ? AND trace_id = ?`,
			formatNextAttemptAt(time.Now().UTC()),
			completion.TaskID,
			completion.TraceID,
		); err != nil {
			return fmt.Errorf("mark safety review independence degradation: %w", err)
		}
	}
	for _, stage := range completion.DownstreamStages {
		if err := insertSafetyReviewDownstreamStage(ctx, tx, completion, stage); err != nil {
			return err
		}
	}
	itemState := completion.ItemState
	if itemState == "" && completion.Decision != nil {
		itemState = completion.Decision.FinalState
	}
	if itemState != "" {
		if _, err := tx.ExecContext(
			ctx,
			"UPDATE review_items SET state = ?, updated_at = ? WHERE task_id = ? AND trace_id = ?",
			itemState,
			formatNextAttemptAt(time.Now().UTC()),
			completion.TaskID,
			completion.TraceID,
		); err != nil {
			return fmt.Errorf("update safety review item state: %w", err)
		}
	}
	if completion.Decision != nil {
		if err := insertSafetyReviewDecision(ctx, tx, completion); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit safety review completion: %w", err)
	}
	return nil
}

// isSafetyReviewInitialSafeShortcut 校验无需 Arbiter 的三路完整安全 shortcut 决策。
func isSafetyReviewInitialSafeShortcut(role string, completion SafetyReviewStageCompletion) bool {
	switch role {
	case "judge_a", "judge_b", "router":
	default:
		return false
	}
	decision := completion.Decision
	if decision == nil || completion.Outcome != SafetyReviewStageSucceeded || completion.ItemState != "resolved_safe" {
		return false
	}
	if decision.FinalState != "resolved_safe" || decision.Label != "safe" || decision.IsAttack {
		return false
	}
	if decision.CaseType != "typical" || decision.PrimaryAttackMethod != "" ||
		decision.PrimaryAttackDomain != "" || decision.PrimaryRiskType != "" || decision.QuarantineReason != "" {
		return false
	}
	return bytes.Equal(decision.AttackMethods, []byte("[]")) && bytes.Equal(decision.AttackDomains, []byte("[]"))
}

// RecoverRunning 将中断遗留的 running 阶段恢复为 pending。
func (s *SafetyReviewStore) RecoverRunning(ctx context.Context, taskID string) (int64, error) {
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE review_stages
		SET state = 'pending', next_attempt_at = NULL, updated_at = ?
		WHERE task_id = ? AND state = 'running'`,
		formatNextAttemptAt(time.Now().UTC()),
		taskID,
	)
	if err != nil {
		return 0, fmt.Errorf("recover safety review running stages: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count recovered safety review stages: %w", err)
	}
	return count, nil
}

// ReadInitialStageResults 读取一条记录的 A、B 与 Router 阶段快照。
func (s *SafetyReviewStore) ReadInitialStageResults(
	ctx context.Context,
	taskID string,
	traceID string,
) ([]SafetyReviewInitialStageResult, error) {
	if taskID == "" || traceID == "" {
		return nil, fmt.Errorf("safety review initial stage identity is empty")
	}
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT s.stage_key, s.role, s.state, s.result_json, i.independence_degraded
		FROM review_stages s
		JOIN review_items i ON i.task_id = s.task_id AND i.trace_id = s.trace_id
		WHERE s.task_id = ? AND s.trace_id = ? AND s.stage_key IN ('judge:a', 'judge:b', 'router')
		ORDER BY s.stage_key`,
		taskID,
		traceID,
	)
	if err != nil {
		return nil, fmt.Errorf("read safety review initial stages: %w", err)
	}
	defer rows.Close()

	var results []SafetyReviewInitialStageResult
	for rows.Next() {
		var result SafetyReviewInitialStageResult
		var raw []byte
		var degraded int
		if err := rows.Scan(&result.StageKey, &result.Role, &result.State, &raw, &degraded); err != nil {
			return nil, fmt.Errorf("scan safety review initial stage: %w", err)
		}
		result.ResultJSON = append([]byte(nil), raw...)
		result.IndependenceDegraded = degraded == 1
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate safety review initial stages: %w", err)
	}
	return results, nil
}

// validateSafetyReviewCompletion 校验阶段完成输入的必要字段。
func validateSafetyReviewCompletion(completion SafetyReviewStageCompletion) error {
	if completion.TaskID == "" || completion.TraceID == "" || completion.StageKey == "" {
		return fmt.Errorf("safety review completion identity is empty")
	}
	switch completion.Outcome {
	case SafetyReviewStageSucceeded:
		if len(completion.ResultJSON) == 0 {
			return fmt.Errorf("succeeded safety review stage %q lacks result", completion.StageKey)
		}
	case SafetyReviewStageTerminalFailed:
		if completion.ErrorCategory == "" || completion.ErrorSummary == "" {
			return fmt.Errorf("terminal safety review stage %q lacks error", completion.StageKey)
		}
	case SafetyReviewStageRetryWait:
		if completion.NextAttemptAt.IsZero() {
			return fmt.Errorf("retry safety review stage %q lacks next attempt time", completion.StageKey)
		}
	default:
		return fmt.Errorf("invalid safety review stage outcome %q", completion.Outcome)
	}
	attempt := completion.Attempt
	if attempt.AttemptKind == "" || attempt.ModelProfile == "" || attempt.ModelFamily == "" ||
		attempt.APIKeyEnv == "" || attempt.StartedAt.IsZero() || attempt.FinishedAt.IsZero() {
		return fmt.Errorf("safety review stage %q attempt is incomplete", completion.StageKey)
	}
	return nil
}

// insertSafetyReviewAttempt 写入一次模型调用审计记录。
func insertSafetyReviewAttempt(ctx context.Context, tx *sql.Tx, completion SafetyReviewStageCompletion) error {
	attempt := completion.Attempt
	var httpStatus any
	if attempt.HTTPStatus != 0 {
		httpStatus = attempt.HTTPStatus
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO review_attempts (
			task_id, trace_id, stage_key, attempt_kind, model_profile, model_family, api_key_env,
			started_at, finished_at, http_status, finish_reason, error_category, error_summary,
			retryable, prompt_tokens, completion_tokens, raw_response, validation_error
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		completion.TaskID,
		completion.TraceID,
		completion.StageKey,
		attempt.AttemptKind,
		attempt.ModelProfile,
		attempt.ModelFamily,
		attempt.APIKeyEnv,
		formatNextAttemptAt(attempt.StartedAt),
		formatNextAttemptAt(attempt.FinishedAt),
		httpStatus,
		attempt.FinishReason,
		attempt.ErrorCategory,
		attempt.ErrorSummary,
		attempt.Retryable,
		attempt.PromptTokens,
		attempt.CompletionTokens,
		attempt.RawResponse,
		attempt.ValidationError,
	); err != nil {
		return fmt.Errorf("insert safety review attempt: %w", err)
	}
	return nil
}

// updateSafetyReviewStage 更新阶段状态并累计对应尝试计数。
func updateSafetyReviewStage(ctx context.Context, tx *sql.Tx, completion SafetyReviewStageCompletion) error {
	now := formatNextAttemptAt(time.Now().UTC())
	var nextState, nextAttempt, result, errorCategory, errorSummary any
	switch completion.Outcome {
	case SafetyReviewStageSucceeded:
		nextState = string(SafetyReviewStageSucceeded)
		result = completion.ResultJSON
	case SafetyReviewStageTerminalFailed:
		nextState = string(SafetyReviewStageTerminalFailed)
		errorCategory = completion.ErrorCategory
		errorSummary = completion.ErrorSummary
	case SafetyReviewStageRetryWait:
		nextState = string(SafetyReviewStageRetryWait)
		nextAttempt = formatNextAttemptAt(completion.NextAttemptAt)
		errorCategory = completion.ErrorCategory
		errorSummary = completion.ErrorSummary
	}
	attemptColumn := safetyReviewAttemptColumn(completion.Attempt.AttemptKind)
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE review_stages
		SET state = ?, result_json = ?, error_category = ?, error_summary = ?,
			next_attempt_at = ?, finished_at = ?, updated_at = ?, `+attemptColumn+` = `+attemptColumn+` + 1
		WHERE task_id = ? AND trace_id = ? AND stage_key = ? AND state = 'running'`,
		nextState,
		result,
		errorCategory,
		errorSummary,
		nextAttempt,
		now,
		now,
		completion.TaskID,
		completion.TraceID,
		completion.StageKey,
	); err != nil {
		return fmt.Errorf("update safety review stage: %w", err)
	}
	return nil
}

// safetyReviewAttemptColumn 返回尝试类型对应的计数列。
func safetyReviewAttemptColumn(attemptKind string) string {
	switch attemptKind {
	case "format_repair":
		return "repair_attempts"
	case "refusal_reprompt":
		return "refusal_attempts"
	default:
		return "request_attempts"
	}
}

// insertSafetyReviewDownstreamStage 幂等创建一个下游阶段。
func insertSafetyReviewDownstreamStage(
	ctx context.Context,
	tx *sql.Tx,
	completion SafetyReviewStageCompletion,
	stage SafetyReviewStageSpec,
) error {
	if err := validateSafetyReviewDownstreamStage(stage); err != nil {
		return err
	}
	now := formatNextAttemptAt(time.Now().UTC())
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO review_stages (
			task_id, trace_id, stage_key, role, axis, category, state, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, 'pending', ?)
		ON CONFLICT(task_id, trace_id, stage_key) DO NOTHING`,
		completion.TaskID,
		completion.TraceID,
		stage.StageKey,
		stage.Role,
		safetyReviewNullableString(stage.Axis),
		safetyReviewNullableString(stage.Category),
		now,
	); err != nil {
		return fmt.Errorf("insert safety review downstream stage %q: %w", stage.StageKey, err)
	}
	return nil
}

// validateSafetyReviewDownstreamStage 校验下游阶段 key 与角色、轴和类别一致。
func validateSafetyReviewDownstreamStage(stage SafetyReviewStageSpec) error {
	if stage.StageKey == "arbiter" && stage.Role == "arbiter" && stage.Axis == "" && stage.Category == "" {
		return nil
	}
	if stage.Role != "expert" || stage.Category == "" {
		return fmt.Errorf("invalid safety review downstream stage %q: %w", stage.StageKey, ErrInvalidTransition)
	}
	switch stage.Axis {
	case "attack_method", "attack_domain":
		if stage.StageKey == "expert:"+stage.Axis+":"+stage.Category {
			return nil
		}
	}
	return fmt.Errorf("invalid safety review downstream stage %q: %w", stage.StageKey, ErrInvalidTransition)
}

// insertSafetyReviewDecision 幂等写入最终决策。
func insertSafetyReviewDecision(ctx context.Context, tx *sql.Tx, completion SafetyReviewStageCompletion) error {
	decision := completion.Decision
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO review_decisions (
			task_id, trace_id, final_state, label, is_attack, attack_methods_json, attack_domains_json,
			primary_attack_method, primary_attack_domain, primary_risk_type, case_type,
			evidence_json, decision_rules_json, rationale, quarantine_reason, decided_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(task_id, trace_id) DO NOTHING`,
		completion.TaskID,
		completion.TraceID,
		decision.FinalState,
		safetyReviewNullableString(decision.Label),
		decision.IsAttack,
		decision.AttackMethods,
		decision.AttackDomains,
		safetyReviewNullableString(decision.PrimaryAttackMethod),
		safetyReviewNullableString(decision.PrimaryAttackDomain),
		safetyReviewNullableString(decision.PrimaryRiskType),
		safetyReviewNullableString(decision.CaseType),
		decision.Evidence,
		decision.DecisionRules,
		decision.Rationale,
		safetyReviewNullableString(decision.QuarantineReason),
		formatNextAttemptAt(decision.DecidedAt),
	); err != nil {
		return fmt.Errorf("insert safety review decision: %w", err)
	}
	return nil
}

// safetyReviewNullableString 将空字符串转换为 SQLite NULL。
func safetyReviewNullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
