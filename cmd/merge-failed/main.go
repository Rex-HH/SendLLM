// Command merge-failed 将 SendLLM 失败数据按原标签回填为成功格式，并与成功数据合并。
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type sourceLabel struct {
	Value     string `json:"value"`
	RiskType  string `json:"risk_type"`
	RiskLevel string `json:"risk_level"`
}

type sourceMeta struct {
	SampleType   string       `json:"sample_type"`
	SourceFields sourceFields `json:"source_fields"`
}

type sourceFields struct {
	Reason string `json:"reason"`
}

type fallbackExtendedInfo struct {
	RiskType  string `json:"risk_type"`
	RiskLevel string `json:"risk_level"`
}

type fallbackAnnotation struct {
	Method       string                `json:"method"`
	IsAttack     bool                  `json:"is_attack"`
	CaseType     string                `json:"case_type"`
	Explanation  string                `json:"explanation"`
	ExtendedInfo *fallbackExtendedInfo `json:"extended_info,omitempty"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "merge-failed: %v\n", err)
		os.Exit(1)
	}
}

// run 解析参数并执行成功数据与回填失败数据的合并。
func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("merge-failed", flag.ContinueOnError)
	flags.SetOutput(stderr)
	successPath := flags.String(
		"success",
		"data/task-004/tesk-004.jsonl",
		"正常成功输出文件",
	)
	failedPath := flags.String(
		"failed",
		"data/task-004/tesk-004.failed.jsonl",
		"失败输出文件",
	)
	outputPath := flags.String(
		"output",
		"data/task-004/tesk-004.merged.jsonl",
		"合并后的输出文件",
	)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}

	successRecords, err := readJSONL(*successPath)
	if err != nil {
		return err
	}
	failedRecords, err := readJSONL(*failedPath)
	if err != nil {
		return err
	}

	mergedRecords, duplicateCount, err := mergeRecords(successRecords, failedRecords)
	if err != nil {
		return err
	}
	if err := writeJSONL(*outputPath, mergedRecords); err != nil {
		return err
	}

	_, err = fmt.Fprintf(
		stdout,
		"success=%d failed=%d merged=%d duplicates=%d output=%s\n",
		len(successRecords),
		len(failedRecords),
		len(mergedRecords),
		duplicateCount,
		*outputPath,
	)
	return err
}

// readJSONL 读取并校验 JSONL 文件，返回每行的原始 JSON。
func readJSONL(path string) ([]json.RawMessage, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 64*1024*1024)

	records := make([]json.RawMessage, 0)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(line, &object); err != nil {
			return nil, fmt.Errorf("%s:%d: invalid JSON object: %w", path, lineNumber, err)
		}
		records = append(records, append(json.RawMessage(nil), line...))
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %q: %w", path, err)
	}
	return records, nil
}

// writeJSONL 以临时文件替换的方式写入 JSONL。
func writeJSONL(path string, records []json.RawMessage) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".merge-failed-*.jsonl")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(temporaryPath)
		}
	}()

	writer := bufio.NewWriter(temporary)
	for _, record := range records {
		if _, err := writer.Write(record); err != nil {
			_ = temporary.Close()
			return fmt.Errorf("write temporary file: %w", err)
		}
		if err := writer.WriteByte('\n'); err != nil {
			_ = temporary.Close()
			return fmt.Errorf("write temporary file: %w", err)
		}
	}
	if err := writer.Flush(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("flush temporary file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync temporary file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary file: %w", err)
	}
	cleanup = false
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace %q: %w", path, err)
	}
	return nil
}

// mergeRecords 合并成功记录与回填后的失败记录，并返回跳过的重复数量。
func mergeRecords(
	successRecords []json.RawMessage,
	failedRecords []json.RawMessage,
) ([]json.RawMessage, int, error) {
	seen := make(map[string]struct{}, len(successRecords)+len(failedRecords))
	merged := make([]json.RawMessage, 0, len(successRecords)+len(failedRecords))

	for _, record := range successRecords {
		id, err := recordID(record)
		if err != nil {
			return nil, 0, err
		}
		if _, exists := seen[id]; exists {
			return nil, 0, fmt.Errorf("duplicate success id %q", id)
		}
		seen[id] = struct{}{}
		merged = append(merged, record)
	}

	duplicateCount := 0
	for _, record := range failedRecords {
		id, err := recordID(record)
		if err != nil {
			return nil, 0, err
		}
		if _, exists := seen[id]; exists {
			duplicateCount++
			continue
		}
		transformed, err := transformFailedRecord(record)
		if err != nil {
			return nil, 0, err
		}
		seen[id] = struct{}{}
		merged = append(merged, transformed)
	}
	return merged, duplicateCount, nil
}

// transformFailedRecord 保留失败记录源字段，并替换为成功格式的 annotation。
func transformFailedRecord(record json.RawMessage) (json.RawMessage, error) {
	fields := make(map[string]json.RawMessage)
	if err := json.Unmarshal(record, &fields); err != nil {
		return nil, fmt.Errorf("decode failed record: %w", err)
	}
	annotation, err := buildAnnotation(fields)
	if err != nil {
		return nil, err
	}
	fields["annotation"] = annotation
	encoded, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("encode merged record: %w", err)
	}
	return encoded, nil
}

// buildAnnotation 根据原标签构造成功输出格式的 annotation。
func buildAnnotation(fields map[string]json.RawMessage) (json.RawMessage, error) {
	label, err := decodeLabel(fields["label"])
	if err != nil {
		return nil, err
	}
	isAttack, err := isAttackFromValue(label.Value)
	if err != nil {
		return nil, err
	}
	meta, err := decodeMeta(fields["meta"])
	if err != nil {
		return nil, err
	}
	if meta.SourceFields.Reason == "" {
		return nil, fmt.Errorf("meta.source_fields.reason is empty")
	}

	annotation := fallbackAnnotation{
		Method:      "auto",
		IsAttack:    isAttack,
		CaseType:    caseTypeForSample(meta.SampleType),
		Explanation: meta.SourceFields.Reason,
	}
	if isAttack {
		annotation.ExtendedInfo = &fallbackExtendedInfo{
			RiskType:  label.RiskType,
			RiskLevel: label.RiskLevel,
		}
	}
	encoded, err := json.Marshal(annotation)
	if err != nil {
		return nil, fmt.Errorf("encode annotation: %w", err)
	}
	return encoded, nil
}

// decodeLabel 从原始 JSON 字段解码原标签。
func decodeLabel(raw json.RawMessage) (sourceLabel, error) {
	var label sourceLabel
	if err := json.Unmarshal(raw, &label); err != nil {
		return sourceLabel{}, fmt.Errorf("decode label: %w", err)
	}
	return label, nil
}

// decodeMeta 从原始 JSON 字段解码元数据。
func decodeMeta(raw json.RawMessage) (sourceMeta, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return sourceMeta{}, nil
	}
	var meta sourceMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return sourceMeta{}, fmt.Errorf("decode meta: %w", err)
	}
	return meta, nil
}

// isAttackFromValue 将安全标签转换为攻击布尔值。
func isAttackFromValue(value string) (bool, error) {
	switch value {
	case "unsafe":
		return true, nil
	case "safe":
		return false, nil
	default:
		return false, fmt.Errorf("unknown label.value %q", value)
	}
}

// caseTypeForSample 根据样本类型返回保守的 case_type。
func caseTypeForSample(sampleType string) string {
	if sampleType == "边界正例" {
		return "borderline"
	}
	return "typical"
}

// recordID 从 JSONL 记录中读取稳定 ID。
func recordID(record json.RawMessage) (string, error) {
	var value struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(record, &value); err != nil {
		return "", fmt.Errorf("decode record id: %w", err)
	}
	if value.ID == "" {
		return "", fmt.Errorf("record id is empty")
	}
	return value.ID, nil
}
