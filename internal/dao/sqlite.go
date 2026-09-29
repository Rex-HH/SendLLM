// Package dao 提供 SQLite 持久化操作。
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

var (
	// ErrTaskMismatch 表示恢复任务的语义指纹与已有状态不一致。
	ErrTaskMismatch = errors.New("dao: task semantic hash mismatch")
	// ErrTraceConflict 表示同一任务中的 trace_id 对应了不同源内容。
	ErrTraceConflict = errors.New("dao: trace_id source conflict")
)

// schemaSQL 内嵌迁移，确保状态文件可以独立创建。
//
//go:embed schema.sql
var schemaSQL string

// Store 表示一个 SQLite 状态库。
type Store struct {
	db *sql.DB
}

// Task 表示可恢复任务的稳定身份。
type Task struct {
	ID           string
	SemanticHash string
}

// Open 打开并迁移指定的 SQLite 状态库。
func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open SQLite database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode = WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable WAL: %w", err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}
	if _, err := db.ExecContext(ctx, schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate schema: %w", err)
	}
	if err := ensureAttemptAPIKeyEnvColumn(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := normalizeRetryTimes(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// ensureAttemptAPIKeyEnvColumn 兼容没有 api_key_env 的旧状态库。
func ensureAttemptAPIKeyEnvColumn(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info(attempts)")
	if err != nil {
		return fmt.Errorf("inspect attempts schema: %w", err)
	}
	hasColumn := false
	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan attempts schema: %w", err)
		}
		if name == "api_key_env" {
			hasColumn = true
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate attempts schema: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close attempts schema rows: %w", err)
	}
	if hasColumn {
		return nil
	}
	if _, err := db.ExecContext(ctx, "ALTER TABLE attempts ADD COLUMN api_key_env TEXT"); err != nil {
		return fmt.Errorf("add attempts api_key_env column: %w", err)
	}
	return nil
}

// Close 关闭底层 SQLite 连接池。
func (s *Store) Close() error {
	return s.db.Close()
}

// EnsureTask 创建任务，或确认已有任务具有相同语义指纹。
func (s *Store) EnsureTask(ctx context.Context, task Task) error {
	if _, err := s.db.ExecContext(
		ctx,
		"INSERT INTO tasks (id, semantic_hash) VALUES (?, ?) ON CONFLICT (id) DO NOTHING",
		task.ID,
		task.SemanticHash,
	); err != nil {
		return fmt.Errorf("create task %q: %w", task.ID, err)
	}

	var existingHash string
	if err := s.db.QueryRowContext(ctx, "SELECT semantic_hash FROM tasks WHERE id = ?", task.ID).Scan(&existingHash); err != nil {
		return fmt.Errorf("load task %q: %w", task.ID, err)
	}
	if existingHash != task.SemanticHash {
		return fmt.Errorf("task %q: %w", task.ID, ErrTaskMismatch)
	}
	return nil
}

type retryTimeUpdate struct {
	rowID   int64
	encoded string
}

// normalizeRetryTimes 无损规范旧状态库中的变长 RFC3339Nano 重试时间。
func normalizeRetryTimes(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, "SELECT rowid, next_attempt_at FROM items WHERE next_attempt_at IS NOT NULL")
	if err != nil {
		return fmt.Errorf("load retry times for migration: %w", err)
	}
	updates := make([]retryTimeUpdate, 0)
	for rows.Next() {
		var rowID int64
		var stored string
		if err := rows.Scan(&rowID, &stored); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan retry time for migration: %w", err)
		}
		parsed, err := time.Parse(time.RFC3339Nano, stored)
		if err != nil {
			_ = rows.Close()
			return fmt.Errorf("parse retry time for migration: %w", err)
		}
		encoded := formatNextAttemptAt(parsed)
		if encoded != stored {
			updates = append(updates, retryTimeUpdate{rowID: rowID, encoded: encoded})
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate retry times for migration: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close retry time migration rows: %w", err)
	}
	if len(updates) == 0 {
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin retry time migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, update := range updates {
		if _, err := tx.ExecContext(
			ctx,
			"UPDATE items SET next_attempt_at = ? WHERE rowid = ?",
			update.encoded,
			update.rowID,
		); err != nil {
			return fmt.Errorf("update retry time for migration: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit retry time migration: %w", err)
	}
	return nil
}
