package dao

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const nextAttemptLayout = "2006-01-02T15:04:05.000000000Z07:00"

var (
	// ErrInvalidTransition 表示样本当前状态不允许目标迁移。
	ErrInvalidTransition = errors.New("dao: invalid item state transition")
)

// ItemState 表示样本在持久化处理流程中的状态。
type ItemState string

const (
	// ItemPending 表示样本尚未领取。
	ItemPending ItemState = "pending"
	// ItemProcessing 表示样本已被处理器领取。
	ItemProcessing ItemState = "processing"
	// ItemRetryWait 表示样本正在等待下次重试。
	ItemRetryWait ItemState = "retry_wait"
	// ItemSucceeded 表示样本已成功完成。
	ItemSucceeded ItemState = "succeeded"
	// ItemFailed 表示样本已最终失败。
	ItemFailed ItemState = "failed"
)

// Item 表示一条待处理或已处理的源样本状态。
type Item struct {
	TaskID          string
	TraceID         string
	InputIndex      int64
	SourceHash      string
	RawJSON         []byte
	Prompt          string
	Response        string
	State           ItemState
	RequestAttempts int
	RepairAttempts  int
	NextAttemptAt   time.Time
}

// Attempt 表示一次独立的分类或格式修复调用。
type Attempt struct {
	Phase            string
	RequestNumber    int
	RepairNumber     int
	StartedAt        time.Time
	FinishedAt       time.Time
	HTTPStatus       int
	ErrorCategory    string
	Retryable        bool
	RawResponse      []byte
	ValidationErrors []string
	InputTokens      int
	OutputTokens     int
}

// Counts 汇总任务各持久化状态的记录数。
type Counts struct {
	Pending    int64
	Processing int64
	RetryWait  int64
	Succeeded  int64
	Failed     int64
}

// ExportRecord 包含合并成功输出所需的源记录和合法标注。
type ExportRecord struct {
	RawJSON    []byte
	Annotation []byte
}

// FailedRecord 包含失败文件允许公开的安全诊断字段。
type FailedRecord struct {
	RawJSON       []byte
	TraceID       string
	ErrorCategory string
	ErrorSummary  string
	Attempts      int
}

// ImportDisposition 表示一条样本在导入中的处理结果。
type ImportDisposition int

const (
	// ImportAdded 表示样本已写入当前导入事务。
	ImportAdded ImportDisposition = iota + 1
	// ImportSkipped 表示内容相同的样本已经存在。
	ImportSkipped
)

// ResetProcessing 将上次中断遗留的处理中记录恢复为待处理。
func (s *Store) ResetProcessing(ctx context.Context, taskID string) (int64, error) {
	result, err := s.db.ExecContext(
		ctx,
		"UPDATE items SET state = ? WHERE task_id = ? AND state = ?",
		ItemPending,
		taskID,
		ItemProcessing,
	)
	if err != nil {
		return 0, fmt.Errorf("reset processing items for task %q: %w", taskID, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count reset items for task %q: %w", taskID, err)
	}
	return count, nil
}

// ResetFailed 将最终失败记录恢复为待处理，用于任务结束前的保守补跑。
func (s *Store) ResetFailed(ctx context.Context, taskID string) (int64, error) {
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE items SET state = ?, request_attempts = 0, repair_attempts = 0,
			next_attempt_at = NULL, annotation = NULL, error_category = NULL, error_summary = NULL
		WHERE task_id = ? AND state = ?`,
		ItemPending,
		taskID,
		ItemFailed,
	)
	if err != nil {
		return 0, fmt.Errorf("reset failed items for task %q: %w", taskID, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count reset failed items for task %q: %w", taskID, err)
	}
	return count, nil
}

// Claim 按输入顺序原子领取已到期的待处理或重试记录。
func (s *Store) Claim(ctx context.Context, taskID string, limit int, now time.Time) ([]Item, error) {
	if limit < 1 {
		return nil, fmt.Errorf("claim items for task %q: limit must be positive", taskID)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin claim for task %q: %w", taskID, err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(
		ctx,
		`SELECT task_id, trace_id, input_index, source_hash, raw_json, prompt, response, state,
			request_attempts, repair_attempts, next_attempt_at
		FROM items
		WHERE task_id = ? AND (
			state = ? OR (state = ? AND next_attempt_at <= ?)
		)
		ORDER BY input_index
		LIMIT ?`,
		taskID,
		ItemPending,
		ItemRetryWait,
		formatNextAttemptAt(now),
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("select claimable items for task %q: %w", taskID, err)
	}
	items, err := scanItems(rows)
	if err != nil {
		return nil, err
	}
	for index := range items {
		if err := transitionState(ctx, tx, taskID, items[index].TraceID, items[index].State, ItemProcessing); err != nil {
			return nil, err
		}
		items[index].State = ItemProcessing
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit claim for task %q: %w", taskID, err)
	}
	return items, nil
}

// MarkSucceeded 原子记录尝试和通过校验的标注。
func (s *Store) MarkSucceeded(
	ctx context.Context,
	taskID string,
	traceID string,
	attempt Attempt,
	annotation []byte,
) error {
	return s.finishAttempt(ctx, taskID, traceID, attempt, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(
			ctx,
			`UPDATE items SET state = ?, request_attempts = max(request_attempts, ?),
				repair_attempts = max(repair_attempts, ?), next_attempt_at = NULL,
				annotation = ?, error_category = NULL, error_summary = NULL
			WHERE task_id = ? AND trace_id = ? AND state = ?`,
			ItemSucceeded,
			attempt.RequestNumber,
			attempt.RepairNumber,
			annotation,
			taskID,
			traceID,
			ItemProcessing,
		)
		return checkTransition(result, err, taskID, traceID, ItemProcessing, ItemSucceeded)
	})
}

// ScheduleRetry 原子记录尝试并安排下一次领取时间。
func (s *Store) ScheduleRetry(
	ctx context.Context,
	taskID string,
	traceID string,
	attempt Attempt,
	next time.Time,
	category string,
	summary string,
) error {
	return s.finishAttempt(ctx, taskID, traceID, attempt, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(
			ctx,
			`UPDATE items SET state = ?, request_attempts = max(request_attempts, ?),
				repair_attempts = max(repair_attempts, ?), next_attempt_at = ?,
				error_category = ?, error_summary = ?
			WHERE task_id = ? AND trace_id = ? AND state = ?`,
			ItemRetryWait,
			attempt.RequestNumber,
			attempt.RepairNumber,
			formatNextAttemptAt(next),
			category,
			summary,
			taskID,
			traceID,
			ItemProcessing,
		)
		return checkTransition(result, err, taskID, traceID, ItemProcessing, ItemRetryWait)
	})
}

// MarkFailed 原子记录尝试和记录级最终失败。
func (s *Store) MarkFailed(
	ctx context.Context,
	taskID string,
	traceID string,
	attempt Attempt,
	category string,
	summary string,
) error {
	return s.finishAttempt(ctx, taskID, traceID, attempt, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(
			ctx,
			`UPDATE items SET state = ?, request_attempts = max(request_attempts, ?),
				repair_attempts = max(repair_attempts, ?), next_attempt_at = NULL,
				error_category = ?, error_summary = ?
			WHERE task_id = ? AND trace_id = ? AND state = ?`,
			ItemFailed,
			attempt.RequestNumber,
			attempt.RepairNumber,
			category,
			summary,
			taskID,
			traceID,
			ItemProcessing,
		)
		return checkTransition(result, err, taskID, traceID, ItemProcessing, ItemFailed)
	})
}

// RecordAttempt 原子记录未结束当前处理状态的一次调用。
func (s *Store) RecordAttempt(ctx context.Context, taskID string, traceID string, attempt Attempt) error {
	return s.finishAttempt(ctx, taskID, traceID, attempt, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(
			ctx,
			`UPDATE items SET request_attempts = max(request_attempts, ?),
				repair_attempts = max(repair_attempts, ?)
			WHERE task_id = ? AND trace_id = ? AND state = ?`,
			attempt.RequestNumber,
			attempt.RepairNumber,
			taskID,
			traceID,
			ItemProcessing,
		)
		return checkTransition(result, err, taskID, traceID, ItemProcessing, ItemProcessing)
	})
}

// Counts 返回任务各状态的持久化计数。
func (s *Store) Counts(ctx context.Context, taskID string) (Counts, error) {
	var counts Counts
	err := s.db.QueryRowContext(
		ctx,
		`SELECT
			COALESCE(SUM(state = 'pending'), 0),
			COALESCE(SUM(state = 'processing'), 0),
			COALESCE(SUM(state = 'retry_wait'), 0),
			COALESCE(SUM(state = 'succeeded'), 0),
			COALESCE(SUM(state = 'failed'), 0)
		FROM items WHERE task_id = ?`,
		taskID,
	).Scan(&counts.Pending, &counts.Processing, &counts.RetryWait, &counts.Succeeded, &counts.Failed)
	if err != nil {
		return Counts{}, fmt.Errorf("count items for task %q: %w", taskID, err)
	}
	return counts, nil
}

// ForEachSucceeded 按输入顺序访问任务的全部成功记录。
func (s *Store) ForEachSucceeded(
	ctx context.Context,
	taskID string,
	visit func(ExportRecord) error,
) error {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT raw_json, annotation FROM items
		WHERE task_id = ? AND state = ? ORDER BY input_index`,
		taskID,
		ItemSucceeded,
	)
	if err != nil {
		return fmt.Errorf("query succeeded exports for task %q: %w", taskID, err)
	}
	defer rows.Close()

	for rows.Next() {
		var record ExportRecord
		if err := rows.Scan(&record.RawJSON, &record.Annotation); err != nil {
			return fmt.Errorf("scan succeeded export for task %q: %w", taskID, err)
		}
		if err := visit(record); err != nil {
			return fmt.Errorf("visit succeeded export for task %q: %w", taskID, err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate succeeded exports for task %q: %w", taskID, err)
	}
	return nil
}

// ForEachFailed 按输入顺序访问任务的全部最终失败记录。
func (s *Store) ForEachFailed(
	ctx context.Context,
	taskID string,
	visit func(FailedRecord) error,
) error {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT raw_json, trace_id, error_category, error_summary, request_attempts + repair_attempts
		FROM items WHERE task_id = ? AND state = ? ORDER BY input_index`,
		taskID,
		ItemFailed,
	)
	if err != nil {
		return fmt.Errorf("query failed exports for task %q: %w", taskID, err)
	}
	defer rows.Close()

	for rows.Next() {
		var record FailedRecord
		if err := rows.Scan(
			&record.RawJSON,
			&record.TraceID,
			&record.ErrorCategory,
			&record.ErrorSummary,
			&record.Attempts,
		); err != nil {
			return fmt.Errorf("scan failed export for task %q: %w", taskID, err)
		}
		if err := visit(record); err != nil {
			return fmt.Errorf("visit failed export for task %q: %w", taskID, err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate failed exports for task %q: %w", taskID, err)
	}
	return nil
}

// NextRetryAt 返回最早等待重试时间。
func (s *Store) NextRetryAt(ctx context.Context, taskID string) (time.Time, bool, error) {
	var encoded sql.NullString
	err := s.db.QueryRowContext(
		ctx,
		"SELECT MIN(next_attempt_at) FROM items WHERE task_id = ? AND state = ?",
		taskID,
		ItemRetryWait,
	).Scan(&encoded)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("find next retry for task %q: %w", taskID, err)
	}
	if !encoded.Valid {
		return time.Time{}, false, nil
	}
	next, err := time.Parse(time.RFC3339Nano, encoded.String)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("parse next retry for task %q: %w", taskID, err)
	}
	return next, true, nil
}

func scanItems(rows *sql.Rows) ([]Item, error) {
	defer rows.Close()
	items := make([]Item, 0)
	for rows.Next() {
		var item Item
		var nextAttemptAt sql.NullString
		if err := rows.Scan(
			&item.TaskID,
			&item.TraceID,
			&item.InputIndex,
			&item.SourceHash,
			&item.RawJSON,
			&item.Prompt,
			&item.Response,
			&item.State,
			&item.RequestAttempts,
			&item.RepairAttempts,
			&nextAttemptAt,
		); err != nil {
			return nil, fmt.Errorf("scan claimed item: %w", err)
		}
		if nextAttemptAt.Valid {
			parsed, err := time.Parse(time.RFC3339Nano, nextAttemptAt.String)
			if err != nil {
				return nil, fmt.Errorf("parse next attempt for trace_id %q: %w", item.TraceID, err)
			}
			item.NextAttemptAt = parsed
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate claimed items: %w", err)
	}
	return items, nil
}

func transitionState(
	ctx context.Context,
	tx *sql.Tx,
	taskID string,
	traceID string,
	expected ItemState,
	target ItemState,
) error {
	result, err := tx.ExecContext(
		ctx,
		"UPDATE items SET state = ? WHERE task_id = ? AND trace_id = ? AND state = ?",
		target,
		taskID,
		traceID,
		expected,
	)
	return checkTransition(result, err, taskID, traceID, expected, target)
}

func checkTransition(
	result sql.Result,
	err error,
	taskID string,
	traceID string,
	expected ItemState,
	target ItemState,
) error {
	if err != nil {
		return fmt.Errorf("transition trace_id %q from %s to %s: %w", traceID, expected, target, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count transition for trace_id %q in task %q: %w", traceID, taskID, err)
	}
	if count != 1 {
		return fmt.Errorf("trace_id %q in task %q: %w", traceID, taskID, ErrInvalidTransition)
	}
	return nil
}

func (s *Store) finishAttempt(
	ctx context.Context,
	taskID string,
	traceID string,
	attempt Attempt,
	transition func(*sql.Tx) error,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin attempt for trace_id %q: %w", traceID, err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := insertAttempt(ctx, tx, taskID, traceID, attempt); err != nil {
		return err
	}
	if err := transition(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit attempt for trace_id %q: %w", traceID, err)
	}
	return nil
}

func insertAttempt(ctx context.Context, tx *sql.Tx, taskID string, traceID string, attempt Attempt) error {
	validationErrors, err := json.Marshal(attempt.ValidationErrors)
	if err != nil {
		return fmt.Errorf("encode validation errors for trace_id %q: %w", traceID, err)
	}
	var finishedAt any
	if !attempt.FinishedAt.IsZero() {
		finishedAt = attempt.FinishedAt.UTC().Format(time.RFC3339Nano)
	}
	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO attempts (
			task_id, trace_id, phase, started_at, finished_at, http_status, error_category,
			retryable, raw_response, validation_error, prompt_tokens, completion_tokens
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		taskID,
		traceID,
		attempt.Phase,
		attempt.StartedAt.UTC().Format(time.RFC3339Nano),
		finishedAt,
		attempt.HTTPStatus,
		attempt.ErrorCategory,
		attempt.Retryable,
		attempt.RawResponse,
		validationErrors,
		attempt.InputTokens,
		attempt.OutputTokens,
	)
	if err != nil {
		return fmt.Errorf("insert attempt for trace_id %q: %w", traceID, err)
	}
	return nil
}

func formatNextAttemptAt(value time.Time) string {
	return value.UTC().Format(nextAttemptLayout)
}
