// Package dao 提供 Policy Optimizer 的独立 SQLite 状态和 Artifact 存储。
package dao

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// policyOptimizerSchemaSQL 内嵌 Policy Optimizer 的冻结建表语句。
//
//go:embed policy_optimizer_schema.sql
var policyOptimizerSchemaSQL string

// ErrPolicyOptimizerNotFound 表示迭代或 Skill Run 不存在。
var ErrPolicyOptimizerNotFound = errors.New("dao: policy optimizer record not found")

// PolicyOptimizerStore 表示 Policy Optimizer 的独立状态库。
type PolicyOptimizerStore struct {
	db *sql.DB
}

// PolicyOptimizerIteration 表示一个优化迭代的稳定身份。
type PolicyOptimizerIteration struct {
	ID                string
	Mode              string
	Status            string
	CurrentStage      string
	BasePolicyVersion string
	BasePolicyHash    string
	ConfigHash        string
	SemanticHash      string
	ObjectiveJSON     []byte
	PermissionsJSON   []byte
}

// PolicyOptimizerRunKey 表示一个可幂等领取的 Skill Run。
type PolicyOptimizerRunKey struct {
	RunID         string
	IterationID   string
	Stage         string
	SkillID       string
	WorkID        string
	Executor      string
	InputSHA256   string
	ContextSHA256 string
	ModelProfile  string
	ModelFamily   string
	AttemptNumber int
}

// PolicyOptimizerSkillRun 表示持久化的 Skill Run。
type PolicyOptimizerSkillRun struct {
	PolicyOptimizerRunKey
	Status           string
	ErrorCategory    string
	NextAttemptAt    string
	OutputArtifactID string
	PromptTokens     int
	CompletionTokens int
	StartedAt        string
	CompletedAt      string
}

// PolicyOptimizerSkillResult 表示 Skill Run 的完成结果。
type PolicyOptimizerSkillResult struct {
	RunID            string
	Status           string
	ErrorCategory    string
	OutputArtifactID string
	PromptTokens     int
	CompletionTokens int
	CompletedAt      time.Time
}

// PolicyOptimizerStatus 表示只读状态汇总。
type PolicyOptimizerStatus struct {
	IterationID string
	Mode        string
	Status      string
	Stage       string
	Recovery    int
}

// OpenPolicyOptimizer 打开并初始化 Policy Optimizer 独立 SQLite 状态库。
func OpenPolicyOptimizer(ctx context.Context, path string) (*PolicyOptimizerStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open policy optimizer database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode = WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable policy optimizer WAL: %w", err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable policy optimizer foreign keys: %w", err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("set policy optimizer busy timeout: %w", err)
	}
	if _, err := db.ExecContext(ctx, policyOptimizerSchemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize policy optimizer schema: %w", err)
	}
	return &PolicyOptimizerStore{db: db}, nil
}

// Close 关闭 Policy Optimizer 状态库连接。
func (s *PolicyOptimizerStore) Close() error {
	return s.db.Close()
}

// EnsureIteration 创建迭代，或确认已有语义身份一致。
func (s *PolicyOptimizerStore) EnsureIteration(ctx context.Context, iteration PolicyOptimizerIteration) error {
	if iteration.ID == "" || iteration.Mode == "" || iteration.CurrentStage == "" ||
		iteration.BasePolicyVersion == "" || len(iteration.ObjectiveJSON) == 0 ||
		len(iteration.PermissionsJSON) == 0 {
		return fmt.Errorf("invalid policy optimizer iteration identity")
	}
	now := policyOptimizerTimestamp(time.Now())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin policy optimizer iteration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO optimization_iterations (
		iteration_id, mode, status, current_stage, base_policy_version, base_policy_hash,
		config_hash, semantic_hash, objective_json, permissions_json, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(iteration_id) DO NOTHING`,
		iteration.ID, iteration.Mode, iteration.Status, iteration.CurrentStage,
		iteration.BasePolicyVersion, iteration.BasePolicyHash, iteration.ConfigHash,
		iteration.SemanticHash, string(iteration.ObjectiveJSON), string(iteration.PermissionsJSON), now, now,
	); err != nil {
		return fmt.Errorf("create policy optimizer iteration: %w", err)
	}
	var existingHash string
	if err := tx.QueryRowContext(ctx,
		"SELECT semantic_hash FROM optimization_iterations WHERE iteration_id = ?",
		iteration.ID,
	).Scan(&existingHash); err != nil {
		return fmt.Errorf("load policy optimizer iteration: %w", err)
	}
	if existingHash != iteration.SemanticHash {
		return fmt.Errorf("policy optimizer iteration %q: %w", iteration.ID, ErrTaskMismatch)
	}
	return tx.Commit()
}

// ClaimSkillRun 领取一个 pending 或 retry_wait 的 Skill Run。
func (s *PolicyOptimizerStore) ClaimSkillRun(
	ctx context.Context,
	key PolicyOptimizerRunKey,
) (PolicyOptimizerSkillRun, bool, error) {
	if key.RunID == "" || key.IterationID == "" || key.Stage == "" || key.SkillID == "" ||
		key.WorkID == "" || key.Executor == "" || key.InputSHA256 == "" ||
		key.ContextSHA256 == "" || key.AttemptNumber < 1 {
		return PolicyOptimizerSkillRun{}, false, fmt.Errorf("invalid policy optimizer run key")
	}
	now := policyOptimizerTimestamp(time.Now())
	_, err := s.db.ExecContext(ctx, `INSERT INTO optimization_skill_runs (
		run_id, iteration_id, stage, skill_id, work_id, executor, input_sha256,
		context_sha256, model_profile, model_family, attempt_number, status, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?)
	ON CONFLICT(run_id) DO NOTHING`,
		key.RunID, key.IterationID, key.Stage, key.SkillID, key.WorkID, key.Executor,
		key.InputSHA256, key.ContextSHA256, key.ModelProfile, key.ModelFamily,
		key.AttemptNumber, now,
	)
	if err != nil {
		return PolicyOptimizerSkillRun{}, false, fmt.Errorf("create policy optimizer skill run: %w", err)
	}
	result, err := s.db.ExecContext(ctx, `UPDATE optimization_skill_runs
		SET status = 'running', started_at = ?
		WHERE run_id = ? AND status IN ('pending','retry_wait')`,
		now, key.RunID,
	)
	if err != nil {
		return PolicyOptimizerSkillRun{}, false, fmt.Errorf("claim policy optimizer skill run: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return PolicyOptimizerSkillRun{}, false, fmt.Errorf("read policy optimizer claim result: %w", err)
	}
	if affected == 0 {
		return PolicyOptimizerSkillRun{}, false, nil
	}
	run, err := s.readSkillRun(ctx, key.RunID)
	if err != nil {
		return PolicyOptimizerSkillRun{}, false, err
	}
	return run, true, nil
}

// CompleteSkillRun 写入 Skill Run 的终态或重试状态。
func (s *PolicyOptimizerStore) CompleteSkillRun(
	ctx context.Context,
	result PolicyOptimizerSkillResult,
) error {
	if result.RunID == "" || result.Status == "" || result.CompletedAt.IsZero() {
		return fmt.Errorf("invalid policy optimizer skill result")
	}
	completedAt := policyOptimizerTimestamp(result.CompletedAt)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin complete policy optimizer skill run: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	update, err := tx.ExecContext(ctx, `UPDATE optimization_skill_runs
		SET status = ?, error_category = ?, output_artifact_id = ?,
		    prompt_tokens = ?, completion_tokens = ?, completed_at = ?
		WHERE run_id = ? AND status = 'running'`,
		result.Status, result.ErrorCategory, result.OutputArtifactID,
		result.PromptTokens, result.CompletionTokens, completedAt, result.RunID,
	)
	if err != nil {
		return fmt.Errorf("complete policy optimizer skill run: %w", err)
	}
	affected, err := update.RowsAffected()
	if err != nil {
		return fmt.Errorf("read complete policy optimizer skill result: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("policy optimizer run %q: %w", result.RunID, ErrPolicyOptimizerNotFound)
	}
	return tx.Commit()
}

// RecoverRunning 将运行中的 Skill Run 恢复为 pending。
func (s *PolicyOptimizerStore) RecoverRunning(ctx context.Context, iterationID string) (int64, error) {
	if iterationID == "" {
		return 0, fmt.Errorf("policy optimizer iteration id is empty")
	}
	now := policyOptimizerTimestamp(time.Now())
	result, err := s.db.ExecContext(ctx, `UPDATE optimization_skill_runs
		SET status = 'pending', error_category = '', next_attempt_at = '', completed_at = ''
		WHERE iteration_id = ? AND status = 'running'`, iterationID)
	if err != nil {
		return 0, fmt.Errorf("recover policy optimizer running runs: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read recovered policy optimizer runs: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE optimization_iterations
		SET recovery_count = recovery_count + 1, updated_at = ? WHERE iteration_id = ?`,
		now, iterationID,
	); err != nil {
		return 0, fmt.Errorf("update policy optimizer recovery count: %w", err)
	}
	return affected, nil
}

// ReadStatus 读取迭代的只读状态。
func (s *PolicyOptimizerStore) ReadStatus(ctx context.Context, iterationID string) (PolicyOptimizerStatus, error) {
	var status PolicyOptimizerStatus
	if err := s.db.QueryRowContext(ctx, `SELECT iteration_id, mode, status, current_stage, recovery_count
		FROM optimization_iterations WHERE iteration_id = ?`, iterationID,
	).Scan(&status.IterationID, &status.Mode, &status.Status, &status.Stage, &status.Recovery); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return status, ErrPolicyOptimizerNotFound
		}
		return status, fmt.Errorf("read policy optimizer status: %w", err)
	}
	return status, nil
}

// ReadFirstStatus 返回状态库中的首个迭代状态。
func (s *PolicyOptimizerStore) ReadFirstStatus(ctx context.Context) (PolicyOptimizerStatus, error) {
	var iterationID string
	if err := s.db.QueryRowContext(ctx,
		"SELECT iteration_id FROM optimization_iterations ORDER BY iteration_id LIMIT 1",
	).Scan(&iterationID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PolicyOptimizerStatus{}, ErrPolicyOptimizerNotFound
		}
		return PolicyOptimizerStatus{}, fmt.Errorf("read policy optimizer first iteration: %w", err)
	}
	return s.ReadStatus(ctx, iterationID)
}

// UpdateIterationStatus 更新迭代的当前状态和阶段。
func (s *PolicyOptimizerStore) UpdateIterationStatus(
	ctx context.Context,
	iterationID string,
	status string,
	stage string,
) error {
	if iterationID == "" || status == "" || stage == "" {
		return fmt.Errorf("policy optimizer iteration status is incomplete")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE optimization_iterations
		SET status = ?, current_stage = ?, updated_at = ? WHERE iteration_id = ?`,
		status, stage, policyOptimizerTimestamp(time.Now()), iterationID,
	)
	if err != nil {
		return fmt.Errorf("update policy optimizer iteration status: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read policy optimizer iteration update: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("policy optimizer iteration %q: %w", iterationID, ErrPolicyOptimizerNotFound)
	}
	return nil
}

// readSkillRun 读取一个 Skill Run。
func (s *PolicyOptimizerStore) readSkillRun(ctx context.Context, runID string) (PolicyOptimizerSkillRun, error) {
	var run PolicyOptimizerSkillRun
	err := s.db.QueryRowContext(ctx, `SELECT run_id, iteration_id, stage, skill_id, work_id,
		executor, input_sha256, context_sha256, model_profile, model_family, attempt_number,
		status, error_category, next_attempt_at, output_artifact_id, prompt_tokens,
		completion_tokens, started_at, completed_at
		FROM optimization_skill_runs WHERE run_id = ?`, runID,
	).Scan(
		&run.RunID, &run.IterationID, &run.Stage, &run.SkillID, &run.WorkID,
		&run.Executor, &run.InputSHA256, &run.ContextSHA256, &run.ModelProfile,
		&run.ModelFamily, &run.AttemptNumber, &run.Status, &run.ErrorCategory,
		&run.NextAttemptAt, &run.OutputArtifactID, &run.PromptTokens,
		&run.CompletionTokens, &run.StartedAt, &run.CompletedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return run, ErrPolicyOptimizerNotFound
	}
	if err != nil {
		return run, fmt.Errorf("read policy optimizer skill run: %w", err)
	}
	return run, nil
}

// policyOptimizerTimestamp 返回九位小数 UTC RFC3339Nano 时间戳。
func policyOptimizerTimestamp(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000000000Z")
}
