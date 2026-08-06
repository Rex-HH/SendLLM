// Package service 协调任务级业务操作。
package service

import (
	"bufio"
	"context"
	"fmt"
	"io"

	"sendllm/internal/dao"
	"sendllm/internal/dto"
)

const maxJSONLLineSize = 16 << 20

// ImportStats 表示一次 JSONL 导入的写入和跳过数量。
type ImportStats struct {
	Added   int
	Skipped int
}

// Import 流式校验并原子导入一个 JSONL 输入。
func Import(ctx context.Context, store *dao.Store, taskID string, reader io.Reader) (ImportStats, error) {
	imp, err := store.BeginImport(ctx, taskID)
	if err != nil {
		return ImportStats{}, fmt.Errorf("begin import: %w", err)
	}
	defer func() { _ = imp.Rollback() }()

	var stats ImportStats
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), maxJSONLLineSize+1)
	var inputIndex int64
	for scanner.Scan() {
		inputIndex++
		raw := append([]byte(nil), scanner.Bytes()...)
		if len(raw) > maxJSONLLineSize {
			return ImportStats{}, fmt.Errorf("read line %d: JSONL line exceeds %d bytes", inputIndex, maxJSONLLineSize)
		}
		sample, err := dto.ParseSource(raw)
		if err != nil {
			return ImportStats{}, fmt.Errorf("parse line %d: %w", inputIndex, err)
		}
		disposition, err := imp.Add(ctx, dao.Item{
			TaskID:     taskID,
			TraceID:    sample.TraceID,
			InputIndex: inputIndex,
			RawJSON:    sample.Raw,
			Prompt:     sample.Prompt,
			Response:   sample.Response,
			State:      dao.ItemPending,
		})
		if err != nil {
			return ImportStats{}, fmt.Errorf("import line %d: %w", inputIndex, err)
		}
		if disposition == dao.ImportAdded {
			stats.Added++
			continue
		}
		stats.Skipped++
	}
	if err := scanner.Err(); err != nil {
		return ImportStats{}, fmt.Errorf("read line %d: %w", inputIndex+1, err)
	}
	if err := imp.Commit(); err != nil {
		return ImportStats{}, fmt.Errorf("commit import: %w", err)
	}
	return stats, nil
}
