package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"sendllm/internal/dto"
	"sendllm/internal/lib/tokenizer"
)

const (
	defaultMaxBatchTokens = 80000
	maxJSONLLineSize      = 64 * 1024 * 1024
)

// prepareConfig 指定 prepare 子命令的输入和输出边界。
type prepareConfig struct {
	InputPath      string
	WorksheetPath  string
	OutputDir      string
	MaxBatchTokens int
}

// routeConfig 指定第一遍结果到第二遍输入的确定性路由边界。
type routeConfig struct {
	QueuePath      string
	BatchDir       string
	MappingPath    string
	DecisionsPath  string
	OutputDir      string
	MaxBatchTokens int
}

// applyConfig 指定最终结论应用时的输入、映射和输出保护边界。
type applyConfig struct {
	InputPath     string
	OutputPath    string
	Format        string
	BatchDir      string
	MappingPath   string
	DecisionsPath string
	Protected     []string
}

// sourceRow 保留一条广告源记录的原始 JSON 和复核所需元数据。
type sourceRow struct {
	TraceID        string
	Source         string
	Label          string
	Prompt         string
	AttackScenario string
	QualityScore   float64
	HasQuality     bool
	Raw            json.RawMessage
}

// sourceDocument 表示一份 JSON 数组或 JSONL 源数据。
type sourceDocument struct {
	Format string
	Rows   []sourceRow
}

// humanReview 保留 worksheet 中已经完成的人工结论。
type humanReview struct {
	TraceID       string `json:"trace_id"`
	HumanLabel    string `json:"human_label"`
	LegacyOverlap string `json:"human_legacy_overlap"`
}

// queueItem 表示本地队列中的一条待复核记录。
type queueItem struct {
	TraceID        string  `json:"trace_id"`
	Prompt         string  `json:"prompt"`
	Source         string  `json:"source"`
	OriginalLabel  string  `json:"original_label"`
	AttackScenario string  `json:"attack_scenario"`
	QualityScore   float64 `json:"quality_score"`
	Tier           string  `json:"tier"`
}

// batchPayloadItem 是唯一允许写入模型输入批次的语义字段。
type batchPayloadItem struct {
	Index  int    `json:"i"`
	Prompt string `json:"p"`
}

// batchPayload 是单批模型用户消息的严格结构。
type batchPayload struct {
	Items []batchPayloadItem `json:"items"`
}

// batchMappingItem 把批内短序号映射回原 trace_id。
type batchMappingItem struct {
	Index   int    `json:"i"`
	TraceID string `json:"trace_id"`
}

// batchMapping 描述一个批次文件及其本地 ID 映射。
type batchMapping struct {
	Batch string             `json:"batch"`
	Items []batchMappingItem `json:"items"`
}

// reviewDecision 是模型输出中的单条结论。
type reviewDecision struct {
	Index int    `json:"i"`
	Label int    `json:"l"`
	Type  string `json:"t"`
}

// reviewResponse 是模型输出的严格顶层结构。
type reviewResponse struct {
	Results []reviewDecision `json:"r"`
}

// prepareReport 汇总队列、批次、Token 和源文件完整性信息。
type prepareReport struct {
	InputPath          string         `json:"input_path"`
	WorksheetPath      string         `json:"worksheet_path"`
	SourceSHA256       string         `json:"source_sha256"`
	WorksheetSHA256    string         `json:"worksheet_sha256"`
	ReviewedCount      int            `json:"reviewed_count"`
	P0Count            int            `json:"p0_count"`
	P1Count            int            `json:"p1_count"`
	QueueCount         int            `json:"queue_count"`
	BatchCount         int            `json:"batch_count"`
	MinBatchTokens     int            `json:"min_batch_tokens"`
	MaxBatchTokens     int            `json:"max_batch_tokens"`
	AverageBatchTokens int            `json:"average_batch_tokens"`
	TokenDistribution  map[string]int `json:"token_distribution"`
	UniqueTraceIDs     int            `json:"unique_trace_ids"`
}

// routeReport 汇总第一遍路由和第二遍批次信息。
type routeReport struct {
	FirstPassBatches   int            `json:"first_pass_batches"`
	DecisionsConsumed  int            `json:"decisions_consumed"`
	SecondPassItems    int            `json:"second_pass_items"`
	BatchCount         int            `json:"batch_count"`
	MinBatchTokens     int            `json:"min_batch_tokens"`
	MaxBatchTokens     int            `json:"max_batch_tokens"`
	AverageBatchTokens int            `json:"average_batch_tokens"`
	TokenDistribution  map[string]int `json:"token_distribution"`
	UniqueTraceIDs     int            `json:"unique_trace_ids"`
}

// applyReport 汇总最终应用时的修改范围。
type applyReport struct {
	InputRows     int `json:"input_rows"`
	OutputRows    int `json:"output_rows"`
	DecisionCount int `json:"decision_count"`
	Modified      int `json:"modified"`
	Unchanged     int `json:"unchanged"`
}

// batchPlan 保存一个已打包批次的文件名、记录和 Token 估算。
type batchPlan struct {
	Name   string
	Items  []queueItem
	Tokens int
}

// loadSourceDocument 读取 JSON 数组或 JSONL，并保留每行原始 JSON。
func loadSourceDocument(path string) (sourceDocument, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return sourceDocument{}, fmt.Errorf("read source %q: %w", path, err)
	}
	if len(bytes.TrimSpace(contents)) == 0 {
		return sourceDocument{}, fmt.Errorf("source %q is empty", path)
	}
	if bytes.HasPrefix(bytes.TrimSpace(contents), []byte("[")) {
		var rawRows []json.RawMessage
		if err := json.Unmarshal(contents, &rawRows); err != nil {
			return sourceDocument{}, fmt.Errorf("decode source array %q: %w", path, err)
		}
		return buildSourceDocument("json", rawRows)
	}
	rawRows, err := readRawJSONLines(bytes.NewReader(contents), maxJSONLLineSize)
	if err != nil {
		return sourceDocument{}, fmt.Errorf("decode source JSONL %q: %w", path, err)
	}
	return buildSourceDocument("jsonl", rawRows)
}

// buildSourceDocument 把原始行解析为带元数据的源文档。
func buildSourceDocument(format string, rawRows []json.RawMessage) (sourceDocument, error) {
	if len(rawRows) == 0 {
		return sourceDocument{}, fmt.Errorf("source has no rows")
	}
	rows := make([]sourceRow, 0, len(rawRows))
	seen := make(map[string]struct{}, len(rawRows))
	for index, raw := range rawRows {
		row, err := parseSourceRow(raw)
		if err != nil {
			return sourceDocument{}, fmt.Errorf("parse source row %d: %w", index+1, err)
		}
		if _, exists := seen[row.TraceID]; exists {
			return sourceDocument{}, fmt.Errorf("duplicate trace_id %q", row.TraceID)
		}
		seen[row.TraceID] = struct{}{}
		rows = append(rows, row)
	}
	return sourceDocument{Format: format, Rows: rows}, nil
}

// parseSourceRow 解析源行元数据并保留完整原始 JSON。
func parseSourceRow(raw []byte) (sourceRow, error) {
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
		return sourceRow{}, fmt.Errorf("decode JSON: %w", err)
	}
	if meta.TraceID == "" {
		return sourceRow{}, fmt.Errorf("trace_id is empty")
	}
	if meta.Label != "safe" && meta.Label != "unsafe" {
		return sourceRow{}, fmt.Errorf("label %q is invalid", meta.Label)
	}
	if meta.Prompt == "" {
		return sourceRow{}, fmt.Errorf("prompt is empty")
	}
	row := sourceRow{
		TraceID:        meta.TraceID,
		Source:         meta.Source,
		Label:          meta.Label,
		Prompt:         meta.Prompt,
		AttackScenario: meta.ExtendedInfo.AttackScenario,
		Raw:            append(json.RawMessage(nil), raw...),
	}
	if meta.Annotation.QualityScore != nil {
		row.QualityScore = *meta.Annotation.QualityScore
		row.HasQuality = true
	}
	return row, nil
}

// loadHumanWorksheet 读取并校验已完成的人工 worksheet。
func loadHumanWorksheet(path string) ([]humanReview, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open worksheet %q: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read worksheet %q: %w", path, err)
	}
	if len(records) < 1 {
		return nil, fmt.Errorf("worksheet %q has no header", path)
	}
	header := make(map[string]int, len(records[0]))
	for index, name := range records[0] {
		header[name] = index
	}
	for _, name := range []string{"trace_id", "human_label", "human_legacy_overlap"} {
		if _, ok := header[name]; !ok {
			return nil, fmt.Errorf("worksheet %q is missing column %q", path, name)
		}
	}
	reviews := make([]humanReview, 0, len(records)-1)
	seen := make(map[string]struct{}, len(records)-1)
	for rowIndex, record := range records[1:] {
		review := humanReview{
			TraceID:       record[header["trace_id"]],
			HumanLabel:    record[header["human_label"]],
			LegacyOverlap: record[header["human_legacy_overlap"]],
		}
		if review.TraceID == "" {
			return nil, fmt.Errorf("worksheet row %d has empty trace_id", rowIndex+2)
		}
		if _, exists := seen[review.TraceID]; exists {
			return nil, fmt.Errorf("worksheet trace_id %q is duplicated", review.TraceID)
		}
		seen[review.TraceID] = struct{}{}
		if review.HumanLabel != "safe" && review.HumanLabel != "unsafe" {
			return nil, fmt.Errorf("worksheet trace_id %q has invalid human_label", review.TraceID)
		}
		if review.LegacyOverlap == "" {
			return nil, fmt.Errorf("worksheet trace_id %q has empty human_legacy_overlap", review.TraceID)
		}
		reviews = append(reviews, review)
	}
	return reviews, nil
}

// readRawJSONLines 读取严格的一条值一行的 JSONL，并返回原始 JSON。
func readRawJSONLines(reader io.Reader, maxLineSize int) ([]json.RawMessage, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), maxLineSize+1)
	rows := make([]json.RawMessage, 0)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		if len(line) > maxLineSize {
			return nil, fmt.Errorf("line %d exceeds %d bytes", lineNumber, maxLineSize)
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
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return fmt.Errorf("read trailing JSON: %w", err)
	}
	return nil
}

// readBatchMappings 读取批次映射并严格校验批内序号连续且唯一。
func readBatchMappings(path string) ([]batchMapping, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read batch mapping %q: %w", path, err)
	}
	rawRows, err := readRawJSONLines(bytes.NewReader(contents), maxJSONLLineSize)
	if err != nil {
		return nil, fmt.Errorf("read batch mapping JSONL %q: %w", path, err)
	}
	mappings := make([]batchMapping, 0, len(rawRows))
	seenBatch := make(map[string]struct{}, len(rawRows))
	for index, raw := range rawRows {
		var mapping batchMapping
		if err := decodeStrictJSON(raw, &mapping); err != nil {
			return nil, fmt.Errorf("decode batch mapping line %d: %w", index+1, err)
		}
		if mapping.Batch == "" || filepath.Base(mapping.Batch) != mapping.Batch {
			return nil, fmt.Errorf("batch mapping line %d has invalid batch name", index+1)
		}
		if _, exists := seenBatch[mapping.Batch]; exists {
			return nil, fmt.Errorf("batch %q is duplicated", mapping.Batch)
		}
		seenBatch[mapping.Batch] = struct{}{}
		if len(mapping.Items) == 0 {
			return nil, fmt.Errorf("batch %q has no mapping items", mapping.Batch)
		}
		seenIndex := make([]bool, len(mapping.Items))
		seenTrace := make(map[string]struct{}, len(mapping.Items))
		for _, item := range mapping.Items {
			if item.Index < 0 || item.Index >= len(mapping.Items) || seenIndex[item.Index] {
				return nil, fmt.Errorf("batch %q has invalid or duplicate index %d", mapping.Batch, item.Index)
			}
			if item.TraceID == "" {
				return nil, fmt.Errorf("batch %q has empty trace_id", mapping.Batch)
			}
			if _, exists := seenTrace[item.TraceID]; exists {
				return nil, fmt.Errorf("batch %q has duplicate trace_id", mapping.Batch)
			}
			seenIndex[item.Index] = true
			seenTrace[item.TraceID] = struct{}{}
		}
		mappings = append(mappings, mapping)
	}
	return mappings, nil
}

// readQueueItems 读取本地队列 JSONL。
func readQueueItems(path string) ([]queueItem, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read queue %q: %w", path, err)
	}
	rawRows, err := readRawJSONLines(bytes.NewReader(contents), maxJSONLLineSize)
	if err != nil {
		return nil, fmt.Errorf("read queue JSONL %q: %w", path, err)
	}
	items := make([]queueItem, 0, len(rawRows))
	seen := make(map[string]struct{}, len(rawRows))
	for index, raw := range rawRows {
		var item queueItem
		if err := decodeStrictJSON(raw, &item); err != nil {
			return nil, fmt.Errorf("decode queue line %d: %w", index+1, err)
		}
		if item.TraceID == "" || item.Prompt == "" {
			return nil, fmt.Errorf("queue line %d is incomplete", index+1)
		}
		if _, exists := seen[item.TraceID]; exists {
			return nil, fmt.Errorf("queue trace_id %q is duplicated", item.TraceID)
		}
		seen[item.TraceID] = struct{}{}
		items = append(items, item)
	}
	return items, nil
}

// decodeStrictJSON 解码单个 JSON 值并拒绝未知字段和尾随内容。
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
		return fmt.Errorf("read trailing JSON: %w", err)
	}
	return nil
}

// parseReviewResponse 严格校验一批模型结论并要求索引完整。
func parseReviewResponse(raw []byte, expectedCount int) ([]reviewDecision, error) {
	var wire struct {
		Results []struct {
			Index *int    `json:"i"`
			Label *int    `json:"l"`
			Type  *string `json:"t"`
		} `json:"r"`
	}
	if err := decodeStrictJSON(raw, &wire); err != nil {
		return nil, fmt.Errorf("decode review response: %w", err)
	}
	if len(wire.Results) != expectedCount {
		return nil, fmt.Errorf("review response count %d, want %d", len(wire.Results), expectedCount)
	}
	decisions := make([]reviewDecision, expectedCount)
	seen := make([]bool, expectedCount)
	for _, result := range wire.Results {
		if result.Index == nil || result.Label == nil || result.Type == nil {
			return nil, fmt.Errorf("review response fields are missing")
		}
		if *result.Index < 0 || *result.Index >= expectedCount || seen[*result.Index] {
			return nil, fmt.Errorf("review response index %d is invalid or duplicated", *result.Index)
		}
		if *result.Label < 0 || *result.Label > 2 {
			return nil, fmt.Errorf("review response label %d is invalid", *result.Label)
		}
		if !validDecisionType(*result.Type) {
			return nil, fmt.Errorf("review response type %q is invalid", *result.Type)
		}
		seen[*result.Index] = true
		decisions[*result.Index] = reviewDecision{
			Index: *result.Index,
			Label: *result.Label,
			Type:  *result.Type,
		}
	}
	for index, value := range seen {
		if !value {
			return nil, fmt.Errorf("review response index %d is missing", index)
		}
	}
	return decisions, nil
}

// readDecisionLines 读取 decisions JSONL；空文件表示尚未有最终结论。
func readDecisionLines(path string) ([]json.RawMessage, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read decisions %q: %w", path, err)
	}
	rawRows, err := readRawJSONLines(bytes.NewReader(contents), maxJSONLLineSize)
	if err != nil {
		return nil, fmt.Errorf("read decisions JSONL %q: %w", path, err)
	}
	return rawRows, nil
}

// validDecisionType 判断 t 是否属于冻结闭集。
func validDecisionType(value string) bool {
	switch value {
	case "news_context", "actual_ad", "quoted_ad", "insufficient":
		return true
	default:
		return false
	}
}

// estimateReviewBatch 使用保守 Token 估算器计算批输入大小。
func estimateReviewBatch(items []queueItem) (int, []byte, error) {
	payload := batchPayload{Items: make([]batchPayloadItem, 0, len(items))}
	for index, item := range items {
		payload.Items = append(payload.Items, batchPayloadItem{Index: index, Prompt: item.Prompt})
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, fmt.Errorf("encode review batch: %w", err)
	}
	tokens := tokenizer.Estimate([]dto.Message{{Role: "user", Content: string(encoded)}}, 0)
	return tokens, encoded, nil
}

// packBatches 按队列顺序贪心打包批次，单条超限直接失败。
func packBatches(items []queueItem, maxBatchTokens int) ([]batchPlan, error) {
	if maxBatchTokens < 1 {
		return nil, fmt.Errorf("max batch tokens must be positive")
	}
	plans := make([]batchPlan, 0)
	current := make([]queueItem, 0)
	currentTokens := 0
	for _, item := range items {
		candidate := make([]queueItem, 0, len(current)+1)
		candidate = append(candidate, current...)
		candidate = append(candidate, item)
		tokens, _, err := estimateReviewBatch(candidate)
		if err != nil {
			return nil, err
		}
		if tokens <= maxBatchTokens {
			current = candidate
			currentTokens = tokens
			continue
		}
		if len(current) == 0 {
			return nil, fmt.Errorf("single review item exceeds %d tokens", maxBatchTokens)
		}
		plans = append(plans, batchPlan{
			Name:   fmt.Sprintf("batch-%03d.json", len(plans)+1),
			Items:  current,
			Tokens: currentTokens,
		})
		current = []queueItem{item}
		currentTokens = 0
		singleTokens, _, err := estimateReviewBatch(current)
		if err != nil {
			return nil, err
		}
		if singleTokens > maxBatchTokens {
			return nil, fmt.Errorf("single review item exceeds %d tokens", maxBatchTokens)
		}
		currentTokens = singleTokens
	}
	if len(current) != 0 {
		plans = append(plans, batchPlan{
			Name:   fmt.Sprintf("batch-%03d.json", len(plans)+1),
			Items:  current,
			Tokens: currentTokens,
		})
	}
	return plans, nil
}

// writeBatchFiles 将盲化批次和本地 ID 映射写入目标目录。
func writeBatchFiles(batchDir string, plans []batchPlan) error {
	if err := os.MkdirAll(batchDir, 0o755); err != nil {
		return fmt.Errorf("create batch directory: %w", err)
	}
	mappingRows := make([]batchMapping, 0, len(plans))
	for _, plan := range plans {
		payload := batchPayload{Items: make([]batchPayloadItem, 0, len(plan.Items))}
		mapping := batchMapping{Batch: plan.Name, Items: make([]batchMappingItem, 0, len(plan.Items))}
		for index, item := range plan.Items {
			payload.Items = append(payload.Items, batchPayloadItem{Index: index, Prompt: item.Prompt})
			mapping.Items = append(mapping.Items, batchMappingItem{Index: index, TraceID: item.TraceID})
		}
		contents, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode batch %s: %w", plan.Name, err)
		}
		if err := atomicWriteFile(filepath.Join(batchDir, plan.Name), contents); err != nil {
			return err
		}
		mappingRows = append(mappingRows, mapping)
	}
	if err := writeJSONLines(filepath.Join(filepath.Dir(batchDir), "review-batch-map.jsonl"), mappingRows); err != nil {
		return err
	}
	return nil
}

// writeJSONLines 以原子方式写入 JSONL 行。
func writeJSONLines(path string, values any) error {
	encoded, err := json.Marshal(values)
	if err != nil {
		return fmt.Errorf("encode JSONL values: %w", err)
	}
	var buffer bytes.Buffer
	if bytes.HasPrefix(encoded, []byte("[")) {
		var rows []json.RawMessage
		if err := json.Unmarshal(encoded, &rows); err != nil {
			return fmt.Errorf("decode JSONL array: %w", err)
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

// writeQueueItems 写入本地队列 JSONL。
func writeQueueItems(path string, items []queueItem) error {
	var buffer bytes.Buffer
	for _, item := range items {
		encoded, err := json.Marshal(item)
		if err != nil {
			return fmt.Errorf("encode queue item: %w", err)
		}
		buffer.Write(encoded)
		buffer.WriteByte('\n')
	}
	return atomicWriteFile(path, buffer.Bytes())
}

// writeEmptyFile 原子写入空文件。
func writeEmptyFile(path string) error {
	return atomicWriteFile(path, nil)
}

// atomicWriteFile 以临时文件加重命名方式发布文件。
func atomicWriteFile(path string, contents []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent directory for %q: %w", path, err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".advertisement-clean-prep-*")
	if err != nil {
		return fmt.Errorf("create temporary file for %q: %w", path, err)
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
	if _, err := temporary.Write(contents); err != nil {
		return fmt.Errorf("write temporary file for %q: %w", path, err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary file for %q: %w", path, err)
	}
	if err := temporary.Close(); err != nil {
		temporary = nil
		return fmt.Errorf("close temporary file for %q: %w", path, err)
	}
	temporary = nil
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish %q: %w", path, err)
	}
	remove = false
	return nil
}

// schemaJSON 返回严格模型结果 Schema。
func schemaJSON() []byte {
	return []byte(`{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "additionalProperties": false,
  "required": ["r"],
  "properties": {
    "r": {
      "type": "array",
      "minItems": 1,
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["i", "l", "t"],
        "properties": {
          "i": {"type": "integer", "minimum": 0},
          "l": {"type": "integer", "enum": [0, 1, 2]},
          "t": {"type": "string", "enum": ["news_context", "actual_ad", "quoted_ad", "insufficient"]}
        }
      },
      "uniqueItems": true
    }
  }
}
`)
}

// tokenDistribution 统计批次 Token 分布。
func tokenDistribution(plans []batchPlan) map[string]int {
	distribution := map[string]int{
		"0-10000":     0,
		"10001-40000": 0,
		"40001-80000": 0,
	}
	for _, plan := range plans {
		switch {
		case plan.Tokens <= 10000:
			distribution["0-10000"]++
		case plan.Tokens <= 40000:
			distribution["10001-40000"]++
		default:
			distribution["40001-80000"]++
		}
	}
	return distribution
}

// batchTokenStats 计算批次 Token 的最小值、最大值和平均值。
func batchTokenStats(plans []batchPlan) (int, int, int) {
	if len(plans) == 0 {
		return 0, 0, 0
	}
	minimum := plans[0].Tokens
	maximum := plans[0].Tokens
	total := 0
	for _, plan := range plans {
		if plan.Tokens < minimum {
			minimum = plan.Tokens
		}
		if plan.Tokens > maximum {
			maximum = plan.Tokens
		}
		total += plan.Tokens
	}
	return minimum, maximum, total / len(plans)
}

// fileSHA256 计算文件 SHA-256。
func fileSHA256(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %q for SHA-256: %w", path, err)
	}
	sum := sha256.Sum256(contents)
	return hex.EncodeToString(sum[:]), nil
}

// reviewedIDSet 把人工结论转换为排除集合。
func reviewedIDSet(reviews []humanReview) map[string]struct{} {
	ids := make(map[string]struct{}, len(reviews))
	for _, review := range reviews {
		ids[review.TraceID] = struct{}{}
	}
	return ids
}

// lengthBand 返回 P1 分层使用的文本长度档位。
func lengthBand(length int) int {
	switch {
	case length <= 80:
		return 0
	case length <= 160:
		return 1
	case length <= 320:
		return 2
	default:
		return 3
	}
}

// qualityBand 返回 P1 分层使用的质量分档位。
func qualityBand(score float64) int {
	switch {
	case score < 0.8:
		return 0
	case score <= 0.9:
		return 1
	default:
		return 2
	}
}

// selectP0 保持源顺序选择所有未审核的 THUCNews/Wikipedia unsafe 记录。
func selectP0(rows []sourceRow, reviewed map[string]struct{}) []sourceRow {
	selected := make([]sourceRow, 0)
	for _, row := range rows {
		if row.Label != "unsafe" || (row.Source != "THUCNews" && row.Source != "Wikipedia") {
			continue
		}
		if _, exists := reviewed[row.TraceID]; exists {
			continue
		}
		selected = append(selected, row)
	}
	return selected
}

// selectP1Sample 按 attack_scenario、长度和质量分确定性分层抽取 P1。
func selectP1Sample(rows []sourceRow, reviewed map[string]struct{}, limit int) []sourceRow {
	if limit < 1 {
		return nil
	}
	scenarioRows := make(map[string][]sourceRow)
	for _, row := range rows {
		if row.Label != "unsafe" || row.Source != "ChineseSafe" {
			continue
		}
		if _, exists := reviewed[row.TraceID]; exists {
			continue
		}
		scenarioRows[row.AttackScenario] = append(scenarioRows[row.AttackScenario], row)
	}
	scenarios := make([]string, 0, len(scenarioRows))
	for scenario := range scenarioRows {
		scenarios = append(scenarios, scenario)
	}
	sort.Strings(scenarios)

	type stratum struct {
		key  [2]int
		rows []sourceRow
	}
	type scenarioCursor struct {
		strata   []*stratum
		position int
	}
	cursors := make([]*scenarioCursor, 0, len(scenarios))
	for _, scenario := range scenarios {
		grouped := make(map[[2]int][]sourceRow)
		for _, row := range scenarioRows[scenario] {
			key := [2]int{lengthBand(len(row.Prompt)), qualityBand(row.QualityScore)}
			grouped[key] = append(grouped[key], row)
		}
		keys := make([][2]int, 0, len(grouped))
		for key := range grouped {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(left, right int) bool {
			if keys[left][0] != keys[right][0] {
				return keys[left][0] < keys[right][0]
			}
			return keys[left][1] < keys[right][1]
		})
		cursor := &scenarioCursor{}
		for _, key := range keys {
			values := grouped[key]
			sort.Slice(values, func(left, right int) bool {
				return traceIDHash(values[left].TraceID) < traceIDHash(values[right].TraceID)
			})
			cursor.strata = append(cursor.strata, &stratum{key: key, rows: values})
		}
		if len(cursor.strata) != 0 {
			cursors = append(cursors, cursor)
		}
	}

	selected := make([]sourceRow, 0, limit)
	for len(selected) < limit {
		progressed := false
		for _, cursor := range cursors {
			if len(selected) >= limit {
				break
			}
			for attempts := 0; attempts < len(cursor.strata); attempts++ {
				if cursor.position >= len(cursor.strata) {
					cursor.position = 0
				}
				stratum := cursor.strata[cursor.position]
				cursor.position = (cursor.position + 1) % len(cursor.strata)
				if len(stratum.rows) == 0 {
					continue
				}
				selected = append(selected, stratum.rows[0])
				stratum.rows = stratum.rows[1:]
				progressed = true
				break
			}
		}
		if !progressed {
			break
		}
	}
	return selected
}

// traceIDHash 返回 trace_id 的确定性排序键。
func traceIDHash(traceID string) string {
	sum := sha256.Sum256([]byte(traceID))
	return hex.EncodeToString(sum[:])
}

// sourceRowToQueueItem 把源记录转换为本地队列项。
func sourceRowToQueueItem(row sourceRow, tier string) queueItem {
	return queueItem{
		TraceID:        row.TraceID,
		Prompt:         row.Prompt,
		Source:         row.Source,
		OriginalLabel:  row.Label,
		AttackScenario: row.AttackScenario,
		QualityScore:   row.QualityScore,
		Tier:           tier,
	}
}

// queueTraceIDSet 返回队列 trace_id 的唯一集合。
func queueTraceIDSet(items []queueItem) (map[string]struct{}, error) {
	ids := make(map[string]struct{}, len(items))
	for _, item := range items {
		if item.TraceID == "" {
			return nil, fmt.Errorf("queue item has empty trace_id")
		}
		if _, exists := ids[item.TraceID]; exists {
			return nil, fmt.Errorf("queue trace_id %q is duplicated", item.TraceID)
		}
		ids[item.TraceID] = struct{}{}
	}
	return ids, nil
}

// queueByTraceID 构造本地队列索引。
func queueByTraceID(items []queueItem) map[string]queueItem {
	index := make(map[string]queueItem, len(items))
	for _, item := range items {
		index[item.TraceID] = item
	}
	return index
}

// readBatchPayload 读取并校验一个盲化批次文件。
func readBatchPayload(path string) (batchPayload, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return batchPayload{}, fmt.Errorf("read batch %q: %w", path, err)
	}
	var payload batchPayload
	if err := decodeStrictJSON(contents, &payload); err != nil {
		return batchPayload{}, fmt.Errorf("decode batch %q: %w", path, err)
	}
	if len(payload.Items) == 0 {
		return batchPayload{}, fmt.Errorf("batch %q is empty", path)
	}
	for index, item := range payload.Items {
		if item.Index != index {
			return batchPayload{}, fmt.Errorf("batch %q item index %d, want %d", path, item.Index, index)
		}
		if item.Prompt == "" {
			return batchPayload{}, fmt.Errorf("batch %q item %d has empty prompt", path, index)
		}
	}
	return payload, nil
}

// ensureOutputPathAvailable 检查输出路径不存在且不在保护列表内。
func ensureOutputPathAvailable(outputPath string, protected []string) error {
	if outputPath == "" {
		return fmt.Errorf("output path is required")
	}
	absoluteOutput, err := filepath.Abs(outputPath)
	if err != nil {
		return fmt.Errorf("resolve output path: %w", err)
	}
	if _, err := os.Stat(outputPath); err == nil {
		return fmt.Errorf("output %q already exists", outputPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat output %q: %w", outputPath, err)
	}
	for _, protectedPath := range protected {
		if protectedPath == "" {
			continue
		}
		absoluteProtected, err := filepath.Abs(protectedPath)
		if err != nil {
			return fmt.Errorf("resolve protected path %q: %w", protectedPath, err)
		}
		if absoluteOutput == absoluteProtected {
			return fmt.Errorf("output %q is protected", outputPath)
		}
	}
	return nil
}

// ensureNewDirectory 检查目录不存在并返回绝对路径。
func ensureNewDirectory(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("output directory is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve output directory: %w", err)
	}
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("output directory %q already exists", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("stat output directory %q: %w", path, err)
	}
	return absolute, nil
}

// publishStagedDirectory 原子发布临时目录。
func publishStagedDirectory(stageDirectory, outputDirectory string) error {
	if err := os.Rename(stageDirectory, outputDirectory); err != nil {
		return fmt.Errorf("publish output directory %q: %w", outputDirectory, err)
	}
	return nil
}

// ensureJSONFormat 把 auto 格式解析为 json 或 jsonl。
func ensureJSONFormat(format string, sourceFormat string) (string, error) {
	if format == "" || format == "auto" {
		return sourceFormat, nil
	}
	if format != "json" && format != "jsonl" {
		return "", fmt.Errorf("format %q is invalid", format)
	}
	return format, nil
}

// writeJSONFile 以缩进 JSON 原子写入文件。
func writeJSONFile(path string, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode JSON file %q: %w", path, err)
	}
	encoded = append(encoded, '\n')
	return atomicWriteFile(path, encoded)
}

// removeAllIgnoringError 清理临时目录并忽略清理错误。
func removeAllIgnoringError(path string) {
	_ = os.RemoveAll(path)
}
