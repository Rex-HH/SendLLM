package service

import (
	"context"
	"os"

	"sendllm/internal/dao"
)

// ExportFileOpsForTest 提供外部测试所需的文件操作失败注入边界。
type ExportFileOpsForTest struct {
	Rename    func(string, string) error
	Link      func(string, string) error
	Open      func(string) (*os.File, error)
	OpenFile  func(string, int, os.FileMode) (*os.File, error)
	Remove    func(string) error
	RemoveAll func(string) error
}

// ExportWithFileOpsForTest 仅向外部测试暴露文件操作失败注入边界。
func ExportWithFileOpsForTest(
	ctx context.Context,
	store *dao.Store,
	taskID string,
	outputPath string,
	overrides ExportFileOpsForTest,
) (ExportStats, error) {
	fileOps := newExportFileOps()
	if overrides.Rename != nil {
		fileOps.rename = overrides.Rename
	}
	if overrides.Link != nil {
		fileOps.link = overrides.Link
	}
	if overrides.Open != nil {
		fileOps.open = overrides.Open
	}
	if overrides.OpenFile != nil {
		fileOps.openFile = overrides.OpenFile
	}
	if overrides.Remove != nil {
		fileOps.remove = overrides.Remove
	}
	if overrides.RemoveAll != nil {
		fileOps.removeAll = overrides.RemoveAll
	}
	return exportWithFileOps(ctx, store, taskID, outputPath, fileOps)
}
