package service

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"sendllm/internal/dto"
)

// PolicyOptimizerNormalizeConfig 指定单个源的规范化输入输出。
type PolicyOptimizerNormalizeConfig struct {
	Package    *PolicyOptimizerAuditPackage
	Source     dto.PolicyOptimizerSourceManifest
	Mapping    dto.PolicyOptimizerMapping
	OutputPath string
}

// PolicyOptimizerNormalizeStats 返回规范化后的稳定统计。
type PolicyOptimizerNormalizeStats struct {
	SourceID    string
	RecordCount int
	OutputPath  string
	SHA256      string
}

// NormalizeAuditSource 将已批准 Mapping 应用于一个源并写出 JSONL。
func NormalizeAuditSource(ctx context.Context, cfg PolicyOptimizerNormalizeConfig) (PolicyOptimizerNormalizeStats, error) {
	if cfg.Package == nil || cfg.OutputPath == "" || cfg.Source.SourceID == "" {
		return PolicyOptimizerNormalizeStats{}, fmt.Errorf("policy optimizer normalize input is incomplete")
	}
	sourcePath, ok := cfg.Package.SourcePath[cfg.Source.SourceID]
	if !ok {
		return PolicyOptimizerNormalizeStats{}, fmt.Errorf("policy optimizer source %q is unknown", cfg.Source.SourceID)
	}
	sourceHash, count, err := inspectPolicyOptimizerSource(sourcePath, cfg.Source)
	if err != nil {
		return PolicyOptimizerNormalizeStats{}, err
	}
	source := PolicyOptimizerSource{Manifest: cfg.Source, Path: sourcePath, SHA256: sourceHash, Count: count}
	if err := ValidateApprovedMapping(cfg.Mapping, source); err != nil {
		return PolicyOptimizerNormalizeStats{}, err
	}
	rows, err := readPolicyOptimizerRows(sourcePath, cfg.Source.Format)
	if err != nil {
		return PolicyOptimizerNormalizeStats{}, err
	}
	records := make([]dto.PolicyOptimizerAuditRecord, 0, len(rows))
	for index, row := range rows {
		if err := ctx.Err(); err != nil {
			return PolicyOptimizerNormalizeStats{}, err
		}
		record, err := normalizePolicyOptimizerRow(
			cfg.Package.Manifest.PackageID,
			cfg.Package.Manifest.CurrentPolicyVersion,
			cfg.Source,
			cfg.Mapping,
			row,
			index,
		)
		if err != nil {
			return PolicyOptimizerNormalizeStats{}, fmt.Errorf("normalize source %q row %d: %w", cfg.Source.SourceID, index+1, err)
		}
		records = append(records, record)
	}
	raw, err := encodePolicyOptimizerRecords(records)
	if err != nil {
		return PolicyOptimizerNormalizeStats{}, err
	}
	if err := writePolicyOptimizerAtomic(cfg.OutputPath, raw); err != nil {
		return PolicyOptimizerNormalizeStats{}, err
	}
	digest := sha256.Sum256(raw)
	return PolicyOptimizerNormalizeStats{
		SourceID: cfg.Source.SourceID, RecordCount: len(records),
		OutputPath: cfg.OutputPath, SHA256: hex.EncodeToString(digest[:]),
	}, nil
}

// MineDisagreements 从结构化比较字段中选择需要后续挖掘的记录。
func MineDisagreements(records []dto.PolicyOptimizerAuditRecord, policy PolicyOptimizerSelectionPolicy) ([]string, error) {
	allowed := map[string]bool{}
	for _, value := range policy.ComparisonTypes {
		allowed[value] = true
	}
	if len(allowed) == 0 {
		allowed[string(dto.PolicyOptimizerComparisonLabelMismatch)] = true
		allowed[string(dto.PolicyOptimizerComparisonModelDisagreement)] = true
	}
	result := make([]string, 0, len(records))
	for _, record := range records {
		if !allowed[string(record.Comparison.Type)] {
			continue
		}
		left := record.Judgments[record.Comparison.Left]
		right := record.Judgments[record.Comparison.Right]
		if left.Label != right.Label || !sameStringSet(left.RiskTypes, right.RiskTypes) {
			result = append(result, record.RecordID)
		}
	}
	return result, nil
}

// PolicyOptimizerSelectionPolicy 指定可选比较类型。
type PolicyOptimizerSelectionPolicy struct {
	ComparisonTypes []string
}

// ImportQualityEvents 读取严格的无 payload Quality Event JSONL。
func ImportQualityEvents(r io.Reader) ([]dto.PolicyOptimizerQualityEvent, error) {
	if r == nil {
		return nil, fmt.Errorf("quality event reader is nil")
	}
	scanner := bufio.NewScanner(r)
	result := make([]dto.PolicyOptimizerQualityEvent, 0)
	for line := 1; scanner.Scan(); line++ {
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}
		event := dto.PolicyOptimizerQualityEvent{}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&event); err != nil {
			return nil, fmt.Errorf("decode quality event line %d: %w", line, err)
		}
		if event.TraceID == "" || (event.Scene != "prompt" && event.Scene != "response") {
			return nil, fmt.Errorf("quality event line %d identity is incomplete", line)
		}
		result = append(result, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan quality events: %w", err)
	}
	return result, nil
}

// normalizePolicyOptimizerRow 将单行源数据转换为 Canonical Audit Record。
func normalizePolicyOptimizerRow(
	packageID string,
	policyVersion string,
	source dto.PolicyOptimizerSourceManifest,
	mapping dto.PolicyOptimizerMapping,
	row map[string]any,
	order int,
) (dto.PolicyOptimizerAuditRecord, error) {
	sampleID, err := policyOptimizerMappedString(row, mapping.Fields["sample_id"])
	if err != nil || sampleID == "" {
		return dto.PolicyOptimizerAuditRecord{}, fmt.Errorf("sample_id mapping is invalid")
	}
	sceneValue, err := policyOptimizerMappedString(row, mapping.Fields["scene"])
	if err != nil {
		return dto.PolicyOptimizerAuditRecord{}, err
	}
	recordID := "AR:" + policyOptimizerCanonicalID(packageID, source.SourceID, sampleID)
	judgments := make([]dto.PolicyOptimizerJudgment, 0, len(mapping.Judgments))
	for _, mapped := range mapping.Judgments {
		label, err := policyOptimizerMappedString(row, mapped.Label)
		if err != nil || (label != "safe" && label != "unsafe" && label != "uncertain") {
			return dto.PolicyOptimizerAuditRecord{}, fmt.Errorf("judgment label mapping is invalid")
		}
		riskTypes, err := policyOptimizerMappedStrings(row, mapped.RiskTypes)
		if err != nil {
			return dto.PolicyOptimizerAuditRecord{}, err
		}
		judgments = append(judgments, dto.PolicyOptimizerJudgment{
			ActorType: mapped.ActorType, ActorID: mapped.ActorID, Label: label,
			RiskTypes: riskTypes, Authority: dto.PolicyOptimizerAuthority(mapped.Authority),
			SourceRef: fmt.Sprintf("%s:%d", source.SourceID, order+1),
		})
	}
	prompt, _ := policyOptimizerMappedString(row, mapping.Fields["prompt"])
	response, _ := policyOptimizerMappedString(row, mapping.Fields["response"])
	sourceTask, _ := policyOptimizerMappedString(row, mapping.Fields["source_task"])
	comparison := dto.PolicyOptimizerComparison{
		Left: mapping.Comparison.Left, Right: mapping.Comparison.Right,
		Type: dto.PolicyOptimizerComparisonType(mapping.Comparison.Type),
	}
	metadata := map[string]any{"source_fields": row}
	record := dto.PolicyOptimizerAuditRecord{
		RecordID: recordID, SampleID: sampleID, SourceID: source.SourceID,
		SourceType: source.Type, SourceTask: sourceTask, SelectionReason: source.SelectionReason,
		Scene: dto.PolicyOptimizerScene(sceneValue), Prompt: prompt, Response: response,
		Judgments: judgments, PolicyVersion: policyVersion,
		Comparison: comparison, Metadata: metadata,
	}
	return record, dto.ValidateAuditRecord(record)
}

// readPolicyOptimizerRows 读取 JSONL、CSV 或 Markdown 表。
func readPolicyOptimizerRows(path, format string) ([]map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read policy optimizer rows: %w", err)
	}
	switch format {
	case "jsonl":
		return readPolicyOptimizerJSONL(raw)
	case "csv":
		return readPolicyOptimizerCSV(raw)
	case "markdown":
		return readPolicyOptimizerMarkdown(raw)
	default:
		return nil, fmt.Errorf("unsupported policy optimizer source format %q", format)
	}
}

// readPolicyOptimizerJSONL 读取一行一个对象的源。
func readPolicyOptimizerJSONL(raw []byte) ([]map[string]any, error) {
	result := make([]map[string]any, 0)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	for {
		row := map[string]any{}
		if err := decoder.Decode(&row); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decode policy optimizer JSONL: %w", err)
		}
		result = append(result, row)
	}
	return result, nil
}

// readPolicyOptimizerCSV 读取首行表头的 CSV。
func readPolicyOptimizerCSV(raw []byte) ([]map[string]any, error) {
	reader := csv.NewReader(bytes.NewReader(raw))
	rows, err := reader.ReadAll()
	if err != nil || len(rows) < 2 {
		return nil, fmt.Errorf("decode policy optimizer CSV")
	}
	result := make([]map[string]any, 0, len(rows)-1)
	for _, row := range rows[1:] {
		item := map[string]any{}
		for index, value := range row {
			if index < len(rows[0]) {
				item[rows[0][index]] = value
			}
		}
		result = append(result, item)
	}
	return result, nil
}

// readPolicyOptimizerMarkdown 读取单个 GitHub-style table。
func readPolicyOptimizerMarkdown(raw []byte) ([]map[string]any, error) {
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) < 3 || !strings.Contains(lines[0], "|") || !strings.Contains(lines[1], "---") {
		return nil, fmt.Errorf("decode policy optimizer Markdown table")
	}
	header := policyOptimizerMarkdownCells(lines[0])
	result := make([]map[string]any, 0, len(lines)-2)
	for _, line := range lines[2:] {
		values := policyOptimizerMarkdownCells(line)
		if len(values) != len(header) {
			return nil, fmt.Errorf("policy optimizer Markdown row width is invalid")
		}
		item := map[string]any{}
		for index, value := range values {
			item[header[index]] = value
		}
		result = append(result, item)
	}
	return result, nil
}

// policyOptimizerMarkdownCells 拆分固定 Markdown 表格单元。
func policyOptimizerMarkdownCells(line string) []string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
	for index := range parts {
		parts[index] = strings.TrimSpace(parts[index])
	}
	return parts
}

// policyOptimizerMappedString 读取 JSON Pointer 或表头映射值。
func policyOptimizerMappedString(row map[string]any, path string) (string, error) {
	if path == "" {
		return "", nil
	}
	value, ok := policyOptimizerMappedValue(row, path)
	if !ok {
		return "", fmt.Errorf("mapped path %q is missing", path)
	}
	switch typed := value.(type) {
	case string:
		return typed, nil
	case json.Number:
		return typed.String(), nil
	default:
		return "", fmt.Errorf("mapped path %q is not a string", path)
	}
}

// policyOptimizerMappedStrings 读取字符串数组映射值。
func policyOptimizerMappedStrings(row map[string]any, path string) ([]string, error) {
	if path == "" {
		return []string{}, nil
	}
	value, ok := policyOptimizerMappedValue(row, path)
	if !ok {
		return nil, fmt.Errorf("mapped path %q is missing", path)
	}
	values, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("mapped path %q is not an array", path)
	}
	result := make([]string, 0, len(values))
	for _, item := range values {
		text, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("mapped path %q contains non-string value", path)
		}
		result = append(result, text)
	}
	return result, nil
}

// policyOptimizerMappedValue 解析 RFC6901 指针或精确列名。
func policyOptimizerMappedValue(row map[string]any, path string) (any, bool) {
	if !strings.HasPrefix(path, "/") {
		value, ok := row[path]
		return value, ok
	}
	var current any = row
	for _, segment := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		segment = strings.ReplaceAll(strings.ReplaceAll(segment, "~1", "/"), "~0", "~")
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[segment]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

// policyOptimizerCanonicalID 计算 Canonical Record ID 的哈希部分。
func policyOptimizerCanonicalID(packageID, sourceID, sampleID string) string {
	raw, _ := json.Marshal([]string{packageID, sourceID, sampleID})
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

// encodePolicyOptimizerRecords 输出稳定 JSONL。
func encodePolicyOptimizerRecords(records []dto.PolicyOptimizerAuditRecord) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	for _, record := range records {
		if err := encoder.Encode(record); err != nil {
			return nil, fmt.Errorf("encode policy optimizer record: %w", err)
		}
	}
	return buffer.Bytes(), nil
}

// writePolicyOptimizerAtomic 原子写入规范输出。
func writePolicyOptimizerAtomic(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create policy optimizer output dir: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".policy-optimizer-normalized-*")
	if err != nil {
		return fmt.Errorf("create policy optimizer normalized temp: %w", err)
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if _, err := temp.Write(raw); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write policy optimizer normalized temp: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync policy optimizer normalized temp: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close policy optimizer normalized temp: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("publish policy optimizer normalized output: %w", err)
	}
	return nil
}
