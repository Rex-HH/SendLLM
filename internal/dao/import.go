package dao

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Import 表示一批原子写入的样本导入事务。
type Import struct {
	tx *sql.Tx
}

// BeginImport 开始指定任务的一批原子样本导入。
func (s *Store) BeginImport(ctx context.Context, taskID string) (*Import, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin import for task %q: %w", taskID, err)
	}
	return &Import{tx: tx}, nil
}

// Add 写入样本，或跳过已经存在的同内容样本。
func (i *Import) Add(ctx context.Context, item Item) (ImportDisposition, error) {
	sourceHash, err := normalizedSourceHash(item.RawJSON)
	if err != nil {
		return 0, fmt.Errorf("hash source %q: %w", item.TraceID, err)
	}

	var existingHash string
	err = i.tx.QueryRowContext(
		ctx,
		"SELECT source_hash FROM items WHERE task_id = ? AND trace_id = ?",
		item.TaskID,
		item.TraceID,
	).Scan(&existingHash)
	if err == nil {
		if existingHash == sourceHash {
			return ImportSkipped, nil
		}
		return 0, fmt.Errorf("trace_id %q: %w", item.TraceID, ErrTraceConflict)
	}
	if err != sql.ErrNoRows {
		return 0, fmt.Errorf("find trace_id %q: %w", item.TraceID, err)
	}

	var nextAttemptAt any
	if !item.NextAttemptAt.IsZero() {
		nextAttemptAt = formatNextAttemptAt(item.NextAttemptAt)
	}
	if _, err := i.tx.ExecContext(
		ctx,
		`INSERT INTO items (
			task_id, trace_id, input_index, source_hash, raw_json, prompt, response, state,
			request_attempts, repair_attempts, next_attempt_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.TaskID,
		item.TraceID,
		item.InputIndex,
		sourceHash,
		item.RawJSON,
		item.Prompt,
		item.Response,
		item.State,
		item.RequestAttempts,
		item.RepairAttempts,
		nextAttemptAt,
	); err != nil {
		return 0, fmt.Errorf("insert trace_id %q: %w", item.TraceID, err)
	}
	return ImportAdded, nil
}

// Commit 提交导入事务。
func (i *Import) Commit() error {
	if i.tx == nil {
		return nil
	}
	err := i.tx.Commit()
	i.tx = nil
	if err != nil {
		return fmt.Errorf("commit import: %w", err)
	}
	return nil
}

// Rollback 回滚尚未提交的导入事务。
func (i *Import) Rollback() error {
	if i.tx == nil {
		return nil
	}
	err := i.tx.Rollback()
	i.tx = nil
	if err != nil {
		return fmt.Errorf("rollback import: %w", err)
	}
	return nil
}

func normalizedSourceHash(raw []byte) (string, error) {
	var source any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&source); err != nil {
		return "", fmt.Errorf("decode JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("decode JSON: multiple values")
	}
	normalized, err := json.Marshal(source)
	if err != nil {
		return "", fmt.Errorf("encode JSON: %w", err)
	}
	hash := sha256.Sum256(normalized)
	return hex.EncodeToString(hash[:]), nil
}
