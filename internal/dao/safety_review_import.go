package dao

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// SafetyReviewImportStats 表示一次 Safety Review JSONL 导入的计数。
type SafetyReviewImportStats struct {
	Added   int64
	Skipped int64
}

// ImportJSONL 原子导入固定字段的 Safety Review JSONL，并创建三条初始阶段。
func (s *SafetyReviewStore) ImportJSONL(
	ctx context.Context,
	taskID string,
	r io.Reader,
) (SafetyReviewImportStats, error) {
	var stats SafetyReviewImportStats
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return stats, fmt.Errorf("begin safety review import: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var scene string
	if err := tx.QueryRowContext(
		ctx,
		"SELECT scene FROM review_tasks WHERE task_id = ?",
		taskID,
	).Scan(&scene); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return stats, fmt.Errorf("load safety review task %q: %w", taskID, ErrSafetyReviewTaskNotFound)
		}
		return stats, fmt.Errorf("load safety review task %q scene: %w", taskID, err)
	}

	var nextIndex int64
	if err := tx.QueryRowContext(
		ctx,
		"SELECT COALESCE(MAX(input_index), -1) + 1 FROM review_items WHERE task_id = ?",
		taskID,
	).Scan(&nextIndex); err != nil {
		return stats, fmt.Errorf("allocate safety review input index: %w", err)
	}

	reader := bufio.NewReader(r)
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) != 0 {
			if err := importSafetyReviewLine(ctx, tx, taskID, scene, &nextIndex, line, &stats); err != nil {
				return SafetyReviewImportStats{}, err
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return SafetyReviewImportStats{}, fmt.Errorf("read safety review JSONL: %w", readErr)
		}
	}
	if err := tx.Commit(); err != nil {
		return SafetyReviewImportStats{}, fmt.Errorf("commit safety review import: %w", err)
	}
	return stats, nil
}

// importSafetyReviewLine 导入单行 JSON，并保持整个导入事务的原子性。
func importSafetyReviewLine(
	ctx context.Context,
	tx *sql.Tx,
	taskID string,
	scene string,
	nextIndex *int64,
	line []byte,
	stats *SafetyReviewImportStats,
) error {
	if len(bytes.TrimSpace(line)) == 0 {
		return fmt.Errorf("safety review JSONL contains an empty line")
	}
	fields, canonical, err := decodeSafetyReviewSource(line)
	if err != nil {
		return err
	}
	traceID, err := safetyReviewStringField(fields, "trace_id")
	if err != nil {
		return err
	}
	if traceID == "" {
		return fmt.Errorf("safety review trace_id is empty")
	}
	prompt, err := safetyReviewStringField(fields, "prompt")
	if err != nil {
		return err
	}
	response, err := safetyReviewStringField(fields, "response")
	if err != nil {
		return err
	}
	if prompt == "" && response == "" {
		return fmt.Errorf("safety review item %q has empty prompt and response", traceID)
	}
	if scene == "response" && response == "" {
		return fmt.Errorf("safety review item %q does not match response scene", traceID)
	}

	sourceHash := safetyReviewSourceHash(canonical)
	var existingHash string
	err = tx.QueryRowContext(
		ctx,
		"SELECT source_hash FROM review_items WHERE task_id = ? AND trace_id = ?",
		taskID,
		traceID,
	).Scan(&existingHash)
	if err == nil {
		if existingHash != sourceHash {
			return fmt.Errorf("safety review trace_id %q: %w", traceID, ErrTraceConflict)
		}
		stats.Skipped++
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("find safety review trace_id %q: %w", traceID, err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO review_items (
			task_id, trace_id, input_index, source_hash, raw_json,
			prompt, response, state, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, 'pending_initial', ?, ?)`,
		taskID,
		traceID,
		*nextIndex,
		sourceHash,
		canonical,
		prompt,
		response,
		now,
		now,
	); err != nil {
		return fmt.Errorf("insert safety review item %q: %w", traceID, err)
	}
	if err := insertSafetyReviewInitialStages(ctx, tx, taskID, traceID, now); err != nil {
		return err
	}
	(*nextIndex)++
	stats.Added++
	return nil
}

// decodeSafetyReviewSource 解码并规范化一行 JSON 对象。
func decodeSafetyReviewSource(line []byte) (map[string]json.RawMessage, []byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.UseNumber()
	fields := map[string]json.RawMessage{}
	if err := decoder.Decode(&fields); err != nil {
		return nil, nil, fmt.Errorf("decode safety review JSON: %w", err)
	}
	if fields == nil {
		return nil, nil, fmt.Errorf("safety review JSON line is not an object")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, nil, fmt.Errorf("safety review JSON line contains multiple values")
		}
		return nil, nil, fmt.Errorf("decode safety review JSON tail: %w", err)
	}
	canonical, err := json.Marshal(fields)
	if err != nil {
		return nil, nil, fmt.Errorf("canonicalize safety review JSON: %w", err)
	}
	return fields, canonical, nil
}

// safetyReviewStringField 提取必须为字符串的固定输入字段。
func safetyReviewStringField(fields map[string]json.RawMessage, name string) (string, error) {
	raw, ok := fields[name]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("decode safety review field %s: %w", name, err)
	}
	return value, nil
}

// safetyReviewSourceHash 计算规范化 JSON 的 SHA-256。
func safetyReviewSourceHash(canonical []byte) string {
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:])
}

// insertSafetyReviewInitialStages 在导入事务内创建三条初始阶段。
func insertSafetyReviewInitialStages(
	ctx context.Context,
	tx *sql.Tx,
	taskID string,
	traceID string,
	now string,
) error {
	stages := []struct {
		key  string
		role string
	}{
		{key: "judge:a", role: "judge_a"},
		{key: "judge:b", role: "judge_b"},
		{key: "router", role: "router"},
	}
	for _, stage := range stages {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO review_stages (
				task_id, trace_id, stage_key, role, state, updated_at
			) VALUES (?, ?, ?, ?, 'pending', ?)`,
			taskID,
			traceID,
			stage.key,
			stage.role,
			now,
		); err != nil {
			return fmt.Errorf("insert safety review stage %s: %w", stage.key, err)
		}
	}
	return nil
}
