package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxJSONLLineSize = 64 * 1024 * 1024

// sourceRecord 保留广告源记录中裁决和输出所需的最小字段。
type sourceRecord struct {
	TraceID        string
	Source         string
	Label          string
	Prompt         string
	AttackScenario string
	QualityScore   *float64
	Raw            json.RawMessage
}

// batchMapItem 表示批次内短序号到原始 trace_id 的本地映射。
type batchMapItem struct {
	Index   int    `json:"i"`
	TraceID string `json:"trace_id"`
}

// batchMap 表示一个批次文件的本地映射。
type batchMap struct {
	Batch string         `json:"batch"`
	Items []batchMapItem `json:"items"`
}

// reviewDecision 表示严格校验后的模型结论。
type reviewDecision struct {
	Index int
	Label int
	Risk  string
}

// layerDecision 表示一层的终态安全结论，失败行不含语义标签。
type layerDecision struct {
	TraceID       string
	State         string
	Label         *int
	Risk          string
	ErrorCategory string
}

// loadSourceRecords 读取 JSON 数组或 JSONL 广告源记录。
func loadSourceRecords(path string) ([]sourceRecord, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read source %q: %w", path, err)
	}
	trimmed := bytes.TrimSpace(contents)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("source %q is empty", path)
	}
	if trimmed[0] == '[' {
		var rows []json.RawMessage
		if err := json.Unmarshal(trimmed, &rows); err != nil {
			return nil, fmt.Errorf("decode source array %q: %w", path, err)
		}
		return parseSourceRecords(rows)
	}
	rows, err := readRawJSONLines(bytes.NewReader(trimmed))
	if err != nil {
		return nil, fmt.Errorf("decode source JSONL %q: %w", path, err)
	}
	return parseSourceRecords(rows)
}

// parseSourceRecords 解析源行并拒绝缺失或不重复的稳定 ID。
func parseSourceRecords(rows []json.RawMessage) ([]sourceRecord, error) {
	records := make([]sourceRecord, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for index, raw := range rows {
		var meta struct {
			TraceID      string `json:"trace_id"`
			Source       string `json:"source"`
			Label        string `json:"label"`
			Prompt       string `json:"prompt"`
			ExtendedInfo struct {
				AttackScenario string `json:"attack_scenario"`
			} `json:"extended_info"`
			Annotation struct {
				QualityScore *float64 `json:"quality_score"`
			} `json:"annotation"`
		}
		if err := json.Unmarshal(raw, &meta); err != nil {
			return nil, fmt.Errorf("decode source row %d: %w", index+1, err)
		}
		if meta.TraceID == "" || meta.Prompt == "" {
			return nil, fmt.Errorf("source row %d is missing trace_id or prompt", index+1)
		}
		if meta.Label != "safe" && meta.Label != "unsafe" {
			return nil, fmt.Errorf("source row %d has invalid label", index+1)
		}
		if _, exists := seen[meta.TraceID]; exists {
			return nil, fmt.Errorf("source trace_id %q is duplicated", meta.TraceID)
		}
		seen[meta.TraceID] = struct{}{}
		records = append(records, sourceRecord{
			TraceID:        meta.TraceID,
			Source:         meta.Source,
			Label:          meta.Label,
			Prompt:         meta.Prompt,
			AttackScenario: meta.ExtendedInfo.AttackScenario,
			QualityScore:   meta.Annotation.QualityScore,
			Raw:            append(json.RawMessage(nil), raw...),
		})
	}
	return records, nil
}

// readRawJSONLines 读取严格的一条值一行的 JSONL。
func readRawJSONLines(reader io.Reader) ([]json.RawMessage, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), maxJSONLLineSize+1)
	rows := make([]json.RawMessage, 0)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		if len(line) > maxJSONLLineSize {
			return nil, fmt.Errorf("line %d exceeds %d bytes", lineNumber, maxJSONLLineSize)
		}
		if err := validateSingleJSON(line); err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNumber, err)
		}
		rows = append(rows, append(json.RawMessage(nil), line...))
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan JSONL: %w", err)
	}
	return rows, nil
}

// validateSingleJSON 确认输入正好包含一个 JSON 值。
func validateSingleJSON(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var value any
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

// loadBatchMaps 读取本地批次映射并校验序号连续、唯一。
func loadBatchMaps(path string) ([]batchMap, error) {
	if path == "" {
		return nil, nil
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read batch mapping %q: %w", path, err)
	}
	rows, err := readRawJSONLines(bytes.NewReader(contents))
	if err != nil {
		return nil, fmt.Errorf("read batch mapping JSONL %q: %w", path, err)
	}
	maps := make([]batchMap, 0, len(rows))
	for index, raw := range rows {
		var mapping batchMap
		if err := decodeStrictJSON(raw, &mapping); err != nil {
			return nil, fmt.Errorf("decode batch mapping %d: %w", index+1, err)
		}
		if mapping.Batch == "" || filepath.Base(mapping.Batch) != mapping.Batch {
			return nil, fmt.Errorf("batch mapping %d has invalid batch name", index+1)
		}
		if len(mapping.Items) == 0 {
			return nil, fmt.Errorf("batch mapping %q has no items", mapping.Batch)
		}
		seen := make([]bool, len(mapping.Items))
		for _, item := range mapping.Items {
			if item.Index < 0 || item.Index >= len(mapping.Items) || seen[item.Index] || item.TraceID == "" {
				return nil, fmt.Errorf("batch mapping %q has invalid items", mapping.Batch)
			}
			seen[item.Index] = true
		}
		maps = append(maps, mapping)
	}
	return maps, nil
}

// loadReviewDecisionLines 读取模型响应行并按映射预期数量严格校验。
func loadReviewDecisionLines(path string, mappings []batchMap) ([][]reviewDecision, error) {
	if path == "" {
		return nil, nil
	}
	if len(mappings) == 0 {
		return nil, fmt.Errorf("review decisions require non-empty batch mappings")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read review decisions %q: %w", path, err)
	}
	rows, err := readRawJSONLines(bytes.NewReader(contents))
	if err != nil {
		return nil, fmt.Errorf("read review decisions JSONL %q: %w", path, err)
	}
	if len(rows) != len(mappings) {
		return nil, fmt.Errorf("review decisions count %d, mappings %d", len(rows), len(mappings))
	}
	results := make([][]reviewDecision, 0, len(rows))
	for index, raw := range rows {
		decisions, err := parseReviewDecisions(raw, len(mappings[index].Items))
		if err != nil {
			return nil, fmt.Errorf("parse review decisions %d: %w", index+1, err)
		}
		results = append(results, decisions)
	}
	return results, nil
}

// loadLayerDecisions 读取单层服务导出的 decisions JSONL 并按 trace_id 建索引。
func loadLayerDecisions(path string) (map[string]layerDecision, error) {
	if path == "" {
		return map[string]layerDecision{}, nil
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read layer decisions %q: %w", path, err)
	}
	rows, err := readRawJSONLines(bytes.NewReader(contents))
	if err != nil {
		return nil, fmt.Errorf("read layer decisions JSONL %q: %w", path, err)
	}
	decisions := make(map[string]layerDecision, len(rows))
	for index, raw := range rows {
		var wire struct {
			TraceID       string  `json:"trace_id"`
			State         string  `json:"state"`
			Label         *int    `json:"l"`
			Risk          *string `json:"x"`
			ErrorCategory string  `json:"error_category"`
		}
		if err := decodeStrictJSON(raw, &wire); err != nil {
			return nil, fmt.Errorf("decode layer decision %d: %w", index+1, err)
		}
		if wire.TraceID == "" {
			return nil, fmt.Errorf("layer decision %d has empty trace_id", index+1)
		}
		if _, exists := decisions[wire.TraceID]; exists {
			return nil, fmt.Errorf("layer decision trace_id %q is duplicated", wire.TraceID)
		}
		switch wire.State {
		case "succeeded":
			if wire.Label == nil || wire.Risk == nil || *wire.Label < 0 || *wire.Label > 2 {
				return nil, fmt.Errorf("layer decision %q is incomplete", wire.TraceID)
			}
			decisions[wire.TraceID] = layerDecision{
				TraceID: wire.TraceID,
				State:   wire.State,
				Label:   wire.Label,
				Risk:    *wire.Risk,
			}
		case "failed":
			if wire.ErrorCategory == "" || wire.Label != nil || wire.Risk != nil {
				return nil, fmt.Errorf("failed layer decision %q is malformed", wire.TraceID)
			}
			decisions[wire.TraceID] = layerDecision{
				TraceID:       wire.TraceID,
				State:         wire.State,
				ErrorCategory: wire.ErrorCategory,
			}
		default:
			return nil, fmt.Errorf("layer decision %q has invalid state", wire.TraceID)
		}
	}
	return decisions, nil
}

// parseReviewDecisions 严格解析一个 {"r":[...]} 响应。
func parseReviewDecisions(raw []byte, expectedCount int) ([]reviewDecision, error) {
	var wire struct {
		Results []struct {
			Index *int    `json:"i"`
			Label *int    `json:"l"`
			Risk  *string `json:"x"`
			Type  *string `json:"t"`
		} `json:"r"`
	}
	if err := decodeStrictJSON(raw, &wire); err != nil {
		return nil, err
	}
	if len(wire.Results) != expectedCount {
		return nil, fmt.Errorf("result count %d, want %d", len(wire.Results), expectedCount)
	}
	decisions := make([]reviewDecision, expectedCount)
	for position, result := range wire.Results {
		if result.Index == nil || result.Label == nil {
			return nil, fmt.Errorf("result fields are missing")
		}
		if result.Type != nil && !validDirectedDecisionType(*result.Type) {
			return nil, fmt.Errorf("result type %q is invalid", *result.Type)
		}
		if *result.Index != position {
			return nil, fmt.Errorf("result index %d at position %d", *result.Index, position)
		}
		if *result.Label < 0 || *result.Label > 2 {
			return nil, fmt.Errorf("result label %d is invalid", *result.Label)
		}
		risk := ""
		if result.Risk != nil {
			risk = *result.Risk
		}
		decisions[position] = reviewDecision{Index: *result.Index, Label: *result.Label, Risk: risk}
	}
	return decisions, nil
}

// validDirectedDecisionType 校验历史定向强审结果的可选 t 枚举。
func validDirectedDecisionType(value string) bool {
	switch value {
	case "news_context", "actual_ad", "quoted_ad", "insufficient":
		return true
	default:
		return false
	}
}

// decodeStrictJSON 解码单值 JSON 并拒绝未知字段和尾随内容。
func decodeStrictJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

// writeJSONLines 原子写入本地 JSONL 行。
func writeJSONLines(path string, values any) error {
	encoded, err := json.Marshal(values)
	if err != nil {
		return fmt.Errorf("encode JSONL values: %w", err)
	}
	var buffer bytes.Buffer
	if bytes.HasPrefix(encoded, []byte("[")) {
		var rows []json.RawMessage
		if err := json.Unmarshal(encoded, &rows); err != nil {
			return fmt.Errorf("decode JSONL rows: %w", err)
		}
		for _, row := range rows {
			buffer.Write(row)
			buffer.WriteByte('\n')
		}
	} else {
		buffer.Write(encoded)
		buffer.WriteByte('\n')
	}
	return atomicWriteFile(path, buffer.Bytes())
}

// writeJSONFile 原子写入缩进 JSON。
func writeJSONFile(path string, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode JSON file %q: %w", path, err)
	}
	return atomicWriteFile(path, append(encoded, '\n'))
}

// atomicWriteFile 使用临时文件加重命名发布文件。
func atomicWriteFile(path string, contents []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent directory for %q: %w", path, err)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".advertisement-full-clean-*")
	if err != nil {
		return fmt.Errorf("create temporary file for %q: %w", path, err)
	}
	tempPath := temp.Name()
	remove := true
	defer func() {
		if temp != nil {
			_ = temp.Close()
		}
		if remove {
			_ = os.Remove(tempPath)
		}
	}()
	if _, err := temp.Write(contents); err != nil {
		return fmt.Errorf("write temporary file for %q: %w", path, err)
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("sync temporary file for %q: %w", path, err)
	}
	if err := temp.Close(); err != nil {
		temp = nil
		return fmt.Errorf("close temporary file for %q: %w", path, err)
	}
	temp = nil
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("publish %q: %w", path, err)
	}
	remove = false
	return nil
}

// fileSHA256 计算文件 SHA-256。
func fileSHA256(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %q: %w", path, err)
	}
	sum := sha256.Sum256(contents)
	return hex.EncodeToString(sum[:]), nil
}

// stableTraceIDHash 返回 trace_id 的稳定排序键。
func stableTraceIDHash(traceID string) string {
	sum := sha256.Sum256([]byte(traceID))
	return hex.EncodeToString(sum[:])
}

// normalizedText 去除首尾空白，便于校验 truth source。
func normalizedText(value string) string {
	return strings.TrimSpace(value)
}
