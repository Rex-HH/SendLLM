package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// routeSecondPass 把第一遍 safe/uncertain 结论路由到新的盲化第二遍批次。
func routeSecondPass(cfg routeConfig) (routeReport, error) {
	if cfg.QueuePath == "" || cfg.BatchDir == "" || cfg.MappingPath == "" ||
		cfg.DecisionsPath == "" || cfg.OutputDir == "" {
		return routeReport{}, fmt.Errorf("route queue, batch directory, mapping, decisions and output directory are required")
	}
	if cfg.MaxBatchTokens < 1 {
		return routeReport{}, fmt.Errorf("max batch tokens must be positive")
	}
	queueItems, err := readQueueItems(cfg.QueuePath)
	if err != nil {
		return routeReport{}, err
	}
	queueIndex := queueByTraceID(queueItems)
	mappings, err := readBatchMappings(cfg.MappingPath)
	if err != nil {
		return routeReport{}, err
	}
	if len(mappings) == 0 {
		return routeReport{}, fmt.Errorf("route has no first-pass batch mappings")
	}
	decisionLines, err := readDecisionLines(cfg.DecisionsPath)
	if err != nil {
		return routeReport{}, err
	}
	if len(decisionLines) > len(mappings) {
		return routeReport{}, fmt.Errorf("route has %d decision lines for %d batches", len(decisionLines), len(mappings))
	}

	secondPass := make([]queueItem, 0)
	seenSecondPass := make(map[string]struct{})
	for decisionIndex, raw := range decisionLines {
		mapping := mappings[decisionIndex]
		payload, err := readBatchPayload(filepath.Join(cfg.BatchDir, mapping.Batch))
		if err != nil {
			return routeReport{}, err
		}
		if len(payload.Items) != len(mapping.Items) {
			return routeReport{}, fmt.Errorf(
				"batch %s payload count %d does not match mapping count %d",
				mapping.Batch,
				len(payload.Items),
				len(mapping.Items),
			)
		}
		decisions, err := parseReviewResponse(raw, len(payload.Items))
		if err != nil {
			return routeReport{}, fmt.Errorf("batch %s: %w", mapping.Batch, err)
		}
		mappingItems := make(map[int]batchMappingItem, len(mapping.Items))
		for _, item := range mapping.Items {
			mappingItems[item.Index] = item
		}
		for _, decision := range decisions {
			if decision.Label != 0 && decision.Label != 1 {
				continue
			}
			mappingItem, ok := mappingItems[decision.Index]
			if !ok {
				return routeReport{}, fmt.Errorf("batch %s decision index %d has no mapping", mapping.Batch, decision.Index)
			}
			item, ok := queueIndex[mappingItem.TraceID]
			if !ok {
				return routeReport{}, fmt.Errorf("batch %s trace_id %q is absent from queue", mapping.Batch, mappingItem.TraceID)
			}
			if _, exists := seenSecondPass[item.TraceID]; exists {
				return routeReport{}, fmt.Errorf("second-pass trace_id %q is duplicated", item.TraceID)
			}
			seenSecondPass[item.TraceID] = struct{}{}
			secondPass = append(secondPass, item)
		}
	}
	if _, err := queueTraceIDSet(secondPass); err != nil {
		return routeReport{}, err
	}
	plans, err := packBatches(secondPass, cfg.MaxBatchTokens)
	if err != nil {
		return routeReport{}, err
	}
	absoluteOutput, err := ensureNewDirectory(cfg.OutputDir)
	if err != nil {
		return routeReport{}, err
	}
	parent := filepath.Dir(absoluteOutput)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return routeReport{}, fmt.Errorf("create route parent directory: %w", err)
	}
	stageDirectory, err := os.MkdirTemp(parent, "."+filepath.Base(absoluteOutput)+".stage-*")
	if err != nil {
		return routeReport{}, fmt.Errorf("create route stage directory: %w", err)
	}
	defer removeAllIgnoringError(stageDirectory)

	if err := writeQueueItems(filepath.Join(stageDirectory, "queue.jsonl"), secondPass); err != nil {
		return routeReport{}, err
	}
	if err := writeBatchFiles(filepath.Join(stageDirectory, "review-batches"), plans); err != nil {
		return routeReport{}, err
	}
	if err := atomicWriteFile(filepath.Join(stageDirectory, "review-result.schema.json"), schemaJSON()); err != nil {
		return routeReport{}, err
	}
	if err := writeEmptyFile(filepath.Join(stageDirectory, "decisions.jsonl")); err != nil {
		return routeReport{}, err
	}
	minimum, maximum, average := batchTokenStats(plans)
	report := routeReport{
		FirstPassBatches:   len(mappings),
		DecisionsConsumed:  len(decisionLines),
		SecondPassItems:    len(secondPass),
		BatchCount:         len(plans),
		MinBatchTokens:     minimum,
		MaxBatchTokens:     maximum,
		AverageBatchTokens: average,
		TokenDistribution:  tokenDistribution(plans),
		UniqueTraceIDs:     len(secondPass),
	}
	if err := writeJSONFile(filepath.Join(stageDirectory, "route-report.json"), report); err != nil {
		return routeReport{}, err
	}
	if err := publishStagedDirectory(stageDirectory, absoluteOutput); err != nil {
		return routeReport{}, err
	}
	return report, nil
}
