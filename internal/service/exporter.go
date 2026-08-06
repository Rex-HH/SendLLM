package service

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	return exportWithFileOps(ctx, store, taskID, outputPath, newExportFileOps())
}

type exportFileOps struct {
	rename    func(string, string) error
	link      func(string, string) error
	open      func(string) (*os.File, error)
	openFile  func(string, int, os.FileMode) (*os.File, error)
	remove    func(string) error
	removeAll func(string) error
}

func newExportFileOps() exportFileOps {
	return exportFileOps{
		rename:    os.Rename,
		link:      os.Link,
		open:      os.Open,
		openFile:  os.OpenFile,
		remove:    os.Remove,
		removeAll: os.RemoveAll,
	}
}

func exportWithFileOps(
	ctx context.Context,
	store *dao.Store,
	taskID string,
	outputPath string,
	fileOps exportFileOps,
) (ExportStats, error) {
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

	if err := publishExportFiles(successTemp, outputPath, failedTemp, failedPath, fileOps); err != nil {
		return ExportStats{}, err
	}
	return stats, nil
}

type exportTarget struct {
	name       string
	tempPath   string
	targetPath string
	backupPath string
	existed    bool
	published  bool
}

func publishExportFiles(
	successTemp string,
	outputPath string,
	failedTemp string,
	failedPath string,
	fileOps exportFileOps,
) error {
	backupDir, err := os.MkdirTemp(filepath.Dir(outputPath), ".sendllm-export-backup-*")
	if err != nil {
		return fmt.Errorf("create export backup directory: %w", err)
	}
	targets := []*exportTarget{
		{
			name:       "succeeded",
			tempPath:   successTemp,
			targetPath: outputPath,
			backupPath: filepath.Join(backupDir, "succeeded"),
		},
		{
			name:       "failed",
			tempPath:   failedTemp,
			targetPath: failedPath,
			backupPath: filepath.Join(backupDir, "failed"),
		},
	}

	for _, target := range targets {
		if err := backupExportTarget(target, fileOps); err != nil {
			cause := fmt.Errorf("backup %s export: %w", target.name, err)
			return rollbackExportFiles(cause, targets, backupDir, fileOps)
		}
	}
	for _, target := range targets {
		if err := fileOps.rename(target.tempPath, target.targetPath); err != nil {
			cause := fmt.Errorf("replace %s export: %w", target.name, err)
			return rollbackExportFiles(cause, targets, backupDir, fileOps)
		}
		target.published = true
	}
	if err := fileOps.removeAll(backupDir); err != nil {
		return fmt.Errorf("remove export backups after publish: %w", err)
	}
	return nil
}

func backupExportTarget(target *exportTarget, fileOps exportFileOps) error {
	info, err := os.Lstat(target.targetPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect target: %w", err)
	}
	linkErr := fileOps.link(target.targetPath, target.backupPath)
	if linkErr == nil {
		target.existed = true
		return nil
	}
	if err := copyExportTarget(target.targetPath, target.backupPath, info.Mode(), fileOps); err != nil {
		return errors.Join(
			fmt.Errorf("link target backup: %w", linkErr),
			fmt.Errorf("copy target backup: %w", err),
		)
	}
	target.existed = true
	return nil
}

func copyExportTarget(sourcePath, backupPath string, mode os.FileMode, fileOps exportFileOps) error {
	source, err := fileOps.open(sourcePath)
	if err != nil {
		return fmt.Errorf("open target: %w", err)
	}
	defer func() { _ = source.Close() }()

	backup, err := fileOps.openFile(backupPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode.Perm())
	if err != nil {
		return fmt.Errorf("create backup: %w", err)
	}
	remove := true
	defer func() {
		if backup != nil {
			_ = backup.Close()
		}
		if remove {
			_ = fileOps.remove(backupPath)
		}
	}()

	if _, err := io.Copy(backup, source); err != nil {
		return fmt.Errorf("copy target: %w", err)
	}
	if err := backup.Sync(); err != nil {
		return fmt.Errorf("sync backup: %w", err)
	}
	if err := backup.Close(); err != nil {
		backup = nil
		return fmt.Errorf("close backup: %w", err)
	}
	backup = nil
	remove = false
	return nil
}

func rollbackExportFiles(
	cause error,
	targets []*exportTarget,
	backupDir string,
	fileOps exportFileOps,
) error {
	errs := []error{cause}
	for index := len(targets) - 1; index >= 0; index-- {
		if err := restoreExportTarget(targets[index], fileOps); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 1 {
		if err := os.Remove(backupDir); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("remove export backup directory: %w", err))
		}
	}
	return errors.Join(errs...)
}

func restoreExportTarget(target *exportTarget, fileOps exportFileOps) error {
	if !target.published {
		if !target.existed {
			return nil
		}
		if err := fileOps.remove(target.backupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove unused %s backup: %w", target.name, err)
		}
		return nil
	}

	var errs []error
	if err := fileOps.remove(target.targetPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, fmt.Errorf("remove new %s export: %w", target.name, err))
	}
	if target.existed {
		if err := fileOps.rename(target.backupPath, target.targetPath); err != nil {
			errs = append(errs, fmt.Errorf("restore old %s export: %w", target.name, err))
		}
	}
	return errors.Join(errs...)
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
