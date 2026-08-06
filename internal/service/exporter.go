package service

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"sendllm/internal/dao"
)

// ExportStats 表示一次导出的成功和失败记录数量。
type ExportStats struct {
	Succeeded int64
	Failed    int64
}

// Export 按输入顺序分别原子替换成功和失败 JSONL 文件。
func Export(ctx context.Context, store *dao.Store, taskID, outputPath string) (ExportStats, error) {
	var stats ExportStats
	successTemp, err := stageJSONL(outputPath, func(encoder *json.Encoder) error {
		return store.ForEachSucceeded(ctx, taskID, func(record dao.ExportRecord) error {
			fields, err := mergeExportRecord(record)
			if err != nil {
				return err
			}
			if err := encoder.Encode(fields); err != nil {
				return fmt.Errorf("encode succeeded record: %w", err)
			}
			stats.Succeeded++
			return nil
		})
	})
	if err != nil {
		return ExportStats{}, fmt.Errorf("stage succeeded export: %w", err)
	}
	defer func() { _ = os.Remove(successTemp) }()

	failedPath := exportFailedPath(outputPath)
	failedTemp, err := stageJSONL(failedPath, func(encoder *json.Encoder) error {
		return store.ForEachFailed(ctx, taskID, func(record dao.FailedRecord) error {
			if err := encoder.Encode(struct {
				TraceID       string `json:"trace_id"`
				ErrorCategory string `json:"error_category"`
				ErrorSummary  string `json:"error_summary"`
				Attempts      int    `json:"attempts"`
			}{
				TraceID:       record.TraceID,
				ErrorCategory: record.ErrorCategory,
				ErrorSummary:  record.ErrorSummary,
				Attempts:      record.Attempts,
			}); err != nil {
				return fmt.Errorf("encode failed record: %w", err)
			}
			stats.Failed++
			return nil
		})
	})
	if err != nil {
		return ExportStats{}, fmt.Errorf("stage failed export: %w", err)
	}
	defer func() { _ = os.Remove(failedTemp) }()

	if err := os.Rename(successTemp, outputPath); err != nil {
		return ExportStats{}, fmt.Errorf("replace succeeded export: %w", err)
	}
	if err := os.Rename(failedTemp, failedPath); err != nil {
		return ExportStats{}, fmt.Errorf("replace failed export: %w", err)
	}
	return stats, nil
}

func mergeExportRecord(record dao.ExportRecord) (map[string]json.RawMessage, error) {
	fields := make(map[string]json.RawMessage)
	if err := json.Unmarshal(record.RawJSON, &fields); err != nil {
		return nil, fmt.Errorf("decode source record: %w", err)
	}
	annotation := make(map[string]json.RawMessage)
	if err := json.Unmarshal(record.Annotation, &annotation); err != nil {
		return nil, fmt.Errorf("decode annotation: %w", err)
	}
	for _, name := range []string{"label", "explanation", "extended_info"} {
		value, ok := annotation[name]
		if !ok {
			delete(fields, name)
			continue
		}
		fields[name] = append(json.RawMessage(nil), value...)
	}
	fields["annotation"] = json.RawMessage(`{"method":"auto"}`)
	return fields, nil
}

func stageJSONL(targetPath string, write func(*json.Encoder) error) (string, error) {
	temporary, err := os.CreateTemp(filepath.Dir(targetPath), ".sendllm-export-*")
	if err != nil {
		return "", fmt.Errorf("create temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	remove := true
	defer func() {
		if temporary != nil {
			_ = temporary.Close()
		}
		if remove {
			_ = os.Remove(temporaryPath)
		}
	}()

	buffered := bufio.NewWriter(temporary)
	if err := write(json.NewEncoder(buffered)); err != nil {
		return "", err
	}
	if err := buffered.Flush(); err != nil {
		return "", fmt.Errorf("flush temporary file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return "", fmt.Errorf("sync temporary file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		temporary = nil
		return "", fmt.Errorf("close temporary file: %w", err)
	}
	temporary = nil
	remove = false
	return temporaryPath, nil
}

func exportFailedPath(outputPath string) string {
	if strings.HasSuffix(outputPath, ".jsonl") {
		return strings.TrimSuffix(outputPath, ".jsonl") + ".failed.jsonl"
	}
	return outputPath + ".failed.jsonl"
}
