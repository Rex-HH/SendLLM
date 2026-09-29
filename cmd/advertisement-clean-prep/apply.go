package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
)

// applyDecisions 只把最终 l=safe 的确认结论应用到新的数据版本。
func applyDecisions(cfg applyConfig) (applyReport, error) {
	if cfg.InputPath == "" || cfg.OutputPath == "" || cfg.BatchDir == "" ||
		cfg.MappingPath == "" || cfg.DecisionsPath == "" {
		return applyReport{}, fmt.Errorf("apply input, output, batch directory, mapping and decisions are required")
	}
	source, err := loadSourceDocument(cfg.InputPath)
	if err != nil {
		return applyReport{}, err
	}
	format, err := ensureJSONFormat(cfg.Format, source.Format)
	if err != nil {
		return applyReport{}, err
	}
	protected := make([]string, 0, len(cfg.Protected)+1)
	protected = append(protected, cfg.InputPath)
	protected = append(protected, cfg.Protected...)
	protected = append(protected, defaultProvisionalPath)
	if err := ensureOutputPathAvailable(cfg.OutputPath, protected); err != nil {
		return applyReport{}, err
	}
	mappings, err := readBatchMappings(cfg.MappingPath)
	if err != nil {
		return applyReport{}, err
	}
	decisionLines, err := readDecisionLines(cfg.DecisionsPath)
	if err != nil {
		return applyReport{}, err
	}
	if len(decisionLines) > len(mappings) {
		return applyReport{}, fmt.Errorf("apply has %d decision lines for %d batches", len(decisionLines), len(mappings))
	}
	sourceByTraceID := make(map[string]sourceRow, len(source.Rows))
	for _, row := range source.Rows {
		sourceByTraceID[row.TraceID] = row
	}
	safeDecisions := make(map[string]struct{})
	decisionCount := 0
	for decisionIndex, raw := range decisionLines {
		mapping := mappings[decisionIndex]
		payload, err := readBatchPayload(filepath.Join(cfg.BatchDir, mapping.Batch))
		if err != nil {
			return applyReport{}, err
		}
		if len(payload.Items) != len(mapping.Items) {
			return applyReport{}, fmt.Errorf(
				"batch %s payload count %d does not match mapping count %d",
				mapping.Batch,
				len(payload.Items),
				len(mapping.Items),
			)
		}
		decisions, err := parseReviewResponse(raw, len(payload.Items))
		if err != nil {
			return applyReport{}, fmt.Errorf("batch %s: %w", mapping.Batch, err)
		}
		mappingItems := make(map[int]batchMappingItem, len(mapping.Items))
		for _, item := range mapping.Items {
			mappingItems[item.Index] = item
		}
		for _, decision := range decisions {
			mappingItem, ok := mappingItems[decision.Index]
			if !ok {
				return applyReport{}, fmt.Errorf("batch %s decision index %d has no mapping", mapping.Batch, decision.Index)
			}
			if _, ok := sourceByTraceID[mappingItem.TraceID]; !ok {
				return applyReport{}, fmt.Errorf("batch %s trace_id %q is absent from source", mapping.Batch, mappingItem.TraceID)
			}
			decisionCount++
			if decision.Label == 1 {
				safeDecisions[mappingItem.TraceID] = struct{}{}
			}
		}
	}
	outputRows := make([]json.RawMessage, len(source.Rows))
	modified := 0
	for index, row := range source.Rows {
		if _, ok := safeDecisions[row.TraceID]; !ok {
			outputRows[index] = append(json.RawMessage(nil), row.Raw...)
			continue
		}
		transformed, err := transformUnsafeToSafe(row.Raw)
		if err != nil {
			return applyReport{}, fmt.Errorf("transform trace_id %q: %w", row.TraceID, err)
		}
		outputRows[index] = transformed
		modified++
	}
	if len(outputRows) != len(source.Rows) {
		return applyReport{}, fmt.Errorf("apply row count changed from %d to %d", len(source.Rows), len(outputRows))
	}
	output, err := encodeSourceDocument(format, outputRows)
	if err != nil {
		return applyReport{}, err
	}
	if err := atomicWriteFile(cfg.OutputPath, output); err != nil {
		return applyReport{}, err
	}
	return applyReport{
		InputRows:     len(source.Rows),
		OutputRows:    len(outputRows),
		DecisionCount: decisionCount,
		Modified:      modified,
		Unchanged:     len(source.Rows) - modified,
	}, nil
}

// transformUnsafeToSafe 只调整确认 safe 所需的最小标签字段，保留 attack_scenario。
func transformUnsafeToSafe(raw json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("decode source object: %w", err)
	}
	fields["label"] = json.RawMessage(`"safe"`)
	extended := make(map[string]json.RawMessage)
	if rawExtended, ok := fields["extended_info"]; ok && len(rawExtended) != 0 {
		if err := json.Unmarshal(rawExtended, &extended); err != nil {
			return nil, fmt.Errorf("decode extended_info: %w", err)
		}
	}
	extended["is_attack"] = json.RawMessage(`false`)
	extended["risk_type"] = json.RawMessage(`""`)
	extended["risk_level"] = json.RawMessage(`""`)
	encodedExtended, err := json.Marshal(extended)
	if err != nil {
		return nil, fmt.Errorf("encode extended_info: %w", err)
	}
	fields["extended_info"] = encodedExtended
	encoded, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("encode source object: %w", err)
	}
	return encoded, nil
}

// encodeSourceDocument 按指定格式编码输出数据。
func encodeSourceDocument(format string, rows []json.RawMessage) ([]byte, error) {
	if format == "json" {
		encoded, err := json.Marshal(rows)
		if err != nil {
			return nil, fmt.Errorf("encode JSON array: %w", err)
		}
		return append(encoded, '\n'), nil
	}
	var buffer bytes.Buffer
	for _, row := range rows {
		buffer.Write(row)
		buffer.WriteByte('\n')
	}
	return buffer.Bytes(), nil
}
