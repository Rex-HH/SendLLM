package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// prepare 生成 P0/P1 队列、盲化批次、本地映射、空 decisions 和严格 Schema。
func prepare(cfg prepareConfig) (prepareReport, error) {
	if cfg.InputPath == "" || cfg.WorksheetPath == "" || cfg.OutputDir == "" {
		return prepareReport{}, fmt.Errorf("prepare input, worksheet and output directory are required")
	}
	if cfg.MaxBatchTokens < 1 {
		return prepareReport{}, fmt.Errorf("max batch tokens must be positive")
	}
	source, err := loadSourceDocument(cfg.InputPath)
	if err != nil {
		return prepareReport{}, err
	}
	reviews, err := loadHumanWorksheet(cfg.WorksheetPath)
	if err != nil {
		return prepareReport{}, err
	}
	sourceSHA256, err := fileSHA256(cfg.InputPath)
	if err != nil {
		return prepareReport{}, err
	}
	worksheetSHA256, err := fileSHA256(cfg.WorksheetPath)
	if err != nil {
		return prepareReport{}, err
	}

	reviewed := reviewedIDSet(reviews)
	p0Rows := selectP0(source.Rows, reviewed)
	p1Rows := selectP1Sample(source.Rows, reviewed, 40)
	queue := make([]queueItem, 0, len(p0Rows)+len(p1Rows))
	for _, row := range p0Rows {
		queue = append(queue, sourceRowToQueueItem(row, "P0"))
	}
	for _, row := range p1Rows {
		queue = append(queue, sourceRowToQueueItem(row, "P1"))
	}
	if len(queue) == 0 {
		return prepareReport{}, fmt.Errorf("prepare queue is empty")
	}
	if _, err := queueTraceIDSet(queue); err != nil {
		return prepareReport{}, err
	}
	plans, err := packBatches(queue, cfg.MaxBatchTokens)
	if err != nil {
		return prepareReport{}, err
	}
	if len(plans) == 0 {
		return prepareReport{}, fmt.Errorf("prepare produced no review batches")
	}

	absoluteOutput, err := ensureNewDirectory(cfg.OutputDir)
	if err != nil {
		return prepareReport{}, err
	}
	parent := filepath.Dir(absoluteOutput)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return prepareReport{}, fmt.Errorf("create prepare parent directory: %w", err)
	}
	stageDirectory, err := os.MkdirTemp(parent, "."+filepath.Base(absoluteOutput)+".stage-*")
	if err != nil {
		return prepareReport{}, fmt.Errorf("create prepare stage directory: %w", err)
	}
	defer removeAllIgnoringError(stageDirectory)

	if err := writeQueueItems(filepath.Join(stageDirectory, "p0.queue.jsonl"), queueItemsByTier(queue, "P0")); err != nil {
		return prepareReport{}, err
	}
	if err := writeQueueItems(filepath.Join(stageDirectory, "p1.queue.jsonl"), queueItemsByTier(queue, "P1")); err != nil {
		return prepareReport{}, err
	}
	if err := writeQueueItems(filepath.Join(stageDirectory, "queue.jsonl"), queue); err != nil {
		return prepareReport{}, err
	}
	if err := writeJSONLines(filepath.Join(stageDirectory, "human-decisions.jsonl"), reviews); err != nil {
		return prepareReport{}, err
	}
	if err := writeBatchFiles(filepath.Join(stageDirectory, "review-batches"), plans); err != nil {
		return prepareReport{}, err
	}
	if err := atomicWriteFile(filepath.Join(stageDirectory, "review-result.schema.json"), schemaJSON()); err != nil {
		return prepareReport{}, err
	}
	if err := writeEmptyFile(filepath.Join(stageDirectory, "decisions.jsonl")); err != nil {
		return prepareReport{}, err
	}
	if err := os.MkdirAll(filepath.Join(stageDirectory, "second-pass"), 0o755); err != nil {
		return prepareReport{}, fmt.Errorf("create second-pass directory: %w", err)
	}
	if err := writeEmptyFile(filepath.Join(stageDirectory, "second-pass", "decisions.jsonl")); err != nil {
		return prepareReport{}, err
	}

	minimum, maximum, average := batchTokenStats(plans)
	report := prepareReport{
		InputPath:          cfg.InputPath,
		WorksheetPath:      cfg.WorksheetPath,
		SourceSHA256:       sourceSHA256,
		WorksheetSHA256:    worksheetSHA256,
		ReviewedCount:      len(reviews),
		P0Count:            len(p0Rows),
		P1Count:            len(p1Rows),
		QueueCount:         len(queue),
		BatchCount:         len(plans),
		MinBatchTokens:     minimum,
		MaxBatchTokens:     maximum,
		AverageBatchTokens: average,
		TokenDistribution:  tokenDistribution(plans),
		UniqueTraceIDs:     len(queue),
	}
	if err := writeJSONFile(filepath.Join(stageDirectory, "prepare-report.json"), report); err != nil {
		return prepareReport{}, err
	}
	if err := publishStagedDirectory(stageDirectory, absoluteOutput); err != nil {
		return prepareReport{}, err
	}
	return report, nil
}

// queueItemsByTier 过滤指定 tier 的本地队列项。
func queueItemsByTier(items []queueItem, tier string) []queueItem {
	filtered := make([]queueItem, 0)
	for _, item := range items {
		if item.Tier == tier {
			filtered = append(filtered, item)
		}
	}
	return filtered
}
