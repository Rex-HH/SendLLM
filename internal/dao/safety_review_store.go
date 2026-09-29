// Package dao 提供 Safety Review 的独立 SQLite 状态存储。
package dao

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// safetyReviewSchemaSQL 内嵌 Safety Review 的独立建表语句。
//
//go:embed safety_review_schema.sql
var safetyReviewSchemaSQL string

// ErrSafetyReviewTaskNotFound 表示指定 Safety Review 任务不存在。
var ErrSafetyReviewTaskNotFound = errors.New("dao: safety review task not found")

// SafetyReviewStore 表示 Safety Review 的独立状态库连接。
type SafetyReviewStore struct {
	db *sql.DB
}

// SafetyReviewTask 表示 Safety Review 任务的稳定身份。
type SafetyReviewTask struct {
	ID                  string
	SemanticFingerprint string
	Scene               string
	SnapshotDir         string
}

// OpenSafetyReview 打开并初始化 Safety Review 的独立 SQLite 状态库。
func OpenSafetyReview(ctx context.Context, path string) (*SafetyReviewStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open safety review database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode = WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable safety review WAL: %w", err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable safety review foreign keys: %w", err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("set safety review busy timeout: %w", err)
	}
	if _, err := db.ExecContext(ctx, safetyReviewSchemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize safety review schema: %w", err)
	}
	return &SafetyReviewStore{db: db}, nil
}

// OpenSafetyReviewReadOnly 打开只读 Safety Review 状态库。
func OpenSafetyReviewReadOnly(ctx context.Context, path string) (*SafetyReviewStore, error) {
	if path == "" {
		return nil, fmt.Errorf("safety review database path is empty")
	}
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("inspect safety review database: %w", err)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve safety review database path: %w", err)
	}
	dsn := (&url.URL{Scheme: "file", Path: absolute, RawQuery: "mode=ro"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open safety review database read-only: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.ExecContext(ctx, "PRAGMA query_only = ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable safety review query-only mode: %w", err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("set safety review busy timeout: %w", err)
	}
	return &SafetyReviewStore{db: db}, nil
}

// Close 关闭 Safety Review 状态库连接。
func (s *SafetyReviewStore) Close() error {
	return s.db.Close()
}

// EnsureTask 创建任务，或确认已有任务身份完全一致。
func (s *SafetyReviewStore) EnsureTask(ctx context.Context, task SafetyReviewTask) error {
	if task.ID == "" || task.SemanticFingerprint == "" ||
		(task.Scene != "prompt" && task.Scene != "response") || task.SnapshotDir == "" {
		return fmt.Errorf("invalid safety review task identity")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin ensure safety review task: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO review_tasks (
			task_id, semantic_fingerprint, scene, status, snapshot_dir, created_at, updated_at
		) VALUES (?, ?, ?, 'created', ?, ?, ?)
		ON CONFLICT(task_id) DO NOTHING`,
		task.ID,
		task.SemanticFingerprint,
		task.Scene,
		task.SnapshotDir,
		now,
		now,
	); err != nil {
		return fmt.Errorf("create safety review task %q: %w", task.ID, err)
	}

	var fingerprint, scene, snapshotDir string
	if err := tx.QueryRowContext(
		ctx,
		`SELECT semantic_fingerprint, scene, snapshot_dir FROM review_tasks WHERE task_id = ?`,
		task.ID,
	).Scan(&fingerprint, &scene, &snapshotDir); err != nil {
		return fmt.Errorf("load safety review task %q: %w", task.ID, err)
	}
	if fingerprint != task.SemanticFingerprint || scene != task.Scene || snapshotDir != task.SnapshotDir {
		return fmt.Errorf("safety review task %q: %w", task.ID, ErrTaskMismatch)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit ensure safety review task: %w", err)
	}
	return nil
}
