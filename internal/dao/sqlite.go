// Package dao 提供 SQLite 持久化操作。
package dao

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"

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
	return &Store{db: db}, nil
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
