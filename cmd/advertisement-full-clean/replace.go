package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

// replaceConfig 指定 provisional 替换器的输入和输出边界。
type replaceConfig struct {
	FullPath        string
	CleanedPath     string
	ExpectedIDsPath string
	OutputPath      string
	Protected       []string
}

// replaceReport 汇总 provisional 替换结果。
type replaceReport struct {
	TotalRows     int `json:"total_rows"`
	ReplacedRows  int `json:"replaced_rows"`
	UnchangedRows int `json:"unchanged_rows"`
}

// replaceAdvertisementRows 只替换匹配的广告 trace_id，并保持其他行逐值不变。
func replaceAdvertisementRows(cfg replaceConfig) (replaceReport, error) {
	if cfg.FullPath == "" || cfg.CleanedPath == "" || cfg.OutputPath == "" {
		return replaceReport{}, fmt.Errorf("replace full, cleaned and output paths are required")
	}
	fullRows, err := loadRawJSONL(cfg.FullPath)
	if err != nil {
		return replaceReport{}, err
	}
	cleanedRows, err := loadRawJSONArray(cfg.CleanedPath)
	if err != nil {
		return replaceReport{}, err
	}
	cleanedByID, err := indexRawRows(cleanedRows)
	if err != nil {
		return replaceReport{}, fmt.Errorf("index cleaned advertisements: %w", err)
	}
	expectedIDs := make(map[string]struct{}, len(cleanedByID))
	for traceID := range cleanedByID {
		expectedIDs[traceID] = struct{}{}
	}
	if cfg.ExpectedIDsPath != "" {
		expectedRows, err := loadSourceRecords(cfg.ExpectedIDsPath)
		if err != nil {
			return replaceReport{}, err
		}
		expectedIDs = make(map[string]struct{}, len(expectedRows))
		for _, row := range expectedRows {
			expectedIDs[row.TraceID] = struct{}{}
		}
		if len(expectedIDs) != len(cleanedByID) {
			return replaceReport{}, fmt.Errorf("cleaned advertisement count does not match expected ID set")
		}
		for traceID := range expectedIDs {
			if _, ok := cleanedByID[traceID]; !ok {
				return replaceReport{}, fmt.Errorf("cleaned advertisements are missing trace_id %q", traceID)
			}
		}
	}
	protected := append([]string{cfg.FullPath, cfg.CleanedPath}, cfg.Protected...)
	if err := ensureOutputPathAvailable(cfg.OutputPath, protected); err != nil {
		return replaceReport{}, err
	}
	outputRows := make([]json.RawMessage, len(fullRows))
	fullCounts := make(map[string]int, len(fullRows))
	report := replaceReport{TotalRows: len(fullRows)}
	for index, raw := range fullRows {
		traceID, err := rawTraceID(raw)
		if err != nil {
			return replaceReport{}, fmt.Errorf("read full row %d: %w", index+1, err)
		}
		if fullCounts[traceID] != 0 {
			return replaceReport{}, fmt.Errorf("full trace_id %q is duplicated", traceID)
		}
		fullCounts[traceID]++
		if replacement, ok := cleanedByID[traceID]; ok {
			outputRows[index] = append(json.RawMessage(nil), replacement...)
			report.ReplacedRows++
			continue
		}
		outputRows[index] = append(json.RawMessage(nil), raw...)
		report.UnchangedRows++
	}
	for traceID := range expectedIDs {
		if fullCounts[traceID] != 1 {
			return replaceReport{}, fmt.Errorf("full trace_id %q occurs %d times", traceID, fullCounts[traceID])
		}
	}
	if len(outputRows) != len(fullRows) {
		return replaceReport{}, fmt.Errorf("replace row count changed")
	}
	if err := ensureOutputPathAvailable(cfg.OutputPath, protected); err != nil {
		return replaceReport{}, err
	}
	if err := writeRawJSONL(cfg.OutputPath, outputRows); err != nil {
		return replaceReport{}, err
	}
	return report, nil
}

// loadRawJSONL 读取完整 JSONL 行并保留原始 JSON。
func loadRawJSONL(path string) ([]json.RawMessage, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read full JSONL %q: %w", path, err)
	}
	rows, err := readRawJSONLines(bytes.NewReader(contents))
	if err != nil {
		return nil, fmt.Errorf("read full JSONL %q: %w", path, err)
	}
	return rows, nil
}

// loadRawJSONArray 读取 JSON 数组原始行。
func loadRawJSONArray(path string) ([]json.RawMessage, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read cleaned JSON array %q: %w", path, err)
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(contents, &rows); err != nil {
		return nil, fmt.Errorf("decode cleaned JSON array %q: %w", path, err)
	}
	return rows, nil
}

// indexRawRows 按 trace_id 建立原始 JSON 索引并拒绝重复。
func indexRawRows(rows []json.RawMessage) (map[string]json.RawMessage, error) {
	index := make(map[string]json.RawMessage, len(rows))
	for position, raw := range rows {
		traceID, err := rawTraceID(raw)
		if err != nil {
			return nil, fmt.Errorf("row %d: %w", position+1, err)
		}
		if _, exists := index[traceID]; exists {
			return nil, fmt.Errorf("trace_id %q is duplicated", traceID)
		}
		index[traceID] = append(json.RawMessage(nil), raw...)
	}
	return index, nil
}

// rawTraceID 读取原始 JSON 中的 trace_id。
func rawTraceID(raw []byte) (string, error) {
	var value struct {
		TraceID string `json:"trace_id"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("decode trace_id: %w", err)
	}
	if value.TraceID == "" {
		return "", fmt.Errorf("trace_id is empty")
	}
	return value.TraceID, nil
}

// writeRawJSONL 原子写入原始 JSONL 行。
func writeRawJSONL(path string, rows []json.RawMessage) error {
	var buffer bytes.Buffer
	for _, row := range rows {
		buffer.Write(row)
		buffer.WriteByte('\n')
	}
	return atomicWriteFile(path, buffer.Bytes())
}
