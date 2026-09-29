package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

const (
	calibrationSplitDevelopment = "prompt-development"
	calibrationSplitHoldout     = "holdout"
	truthSourceWorksheet        = "worksheet_512"
	truthSourceDirected         = "directed_265"
)

// calibrationConfig 指定校准真值构建所需的全部只读输入和输出目录。
type calibrationConfig struct {
	SourcePath          string
	WorksheetPath       string
	Layer1MappingPath   string
	Layer1DecisionsPath string
	Layer2MappingPath   string
	Layer2DecisionsPath string
	OutputDir           string
}

// calibrationRecord 表示一条无 prompt 的冻结校准真值。
type calibrationRecord struct {
	TraceID       string `json:"trace_id"`
	Label         string `json:"label"`
	LegacyOverlap *bool  `json:"legacy_overlap,omitempty"`
	TruthSource   string `json:"truth_source"`
	Split         string `json:"split"`
}

// calibrationReport 汇总校准集计数和稳定哈希。
type calibrationReport struct {
	SourceSHA256      string         `json:"source_sha256"`
	WorksheetSHA256   string         `json:"worksheet_sha256"`
	TotalCount        int            `json:"total_count"`
	WorksheetCount    int            `json:"worksheet_count"`
	DirectedCount     int            `json:"directed_count"`
	UnsafeCount       int            `json:"unsafe_count"`
	SafeCount         int            `json:"safe_count"`
	DevelopmentCount  int            `json:"development_count"`
	HoldoutCount      int            `json:"holdout_count"`
	OverlapYesCount   int            `json:"overlap_yes_count"`
	OverlapNoCount    int            `json:"overlap_no_count"`
	LabelCounts       map[string]int `json:"label_counts"`
	SplitCounts       map[string]int `json:"split_counts"`
	TruthSourceCounts map[string]int `json:"truth_source_counts"`
	CalibrationSHA256 string         `json:"calibration_sha256"`
	PilotSHA256       string         `json:"pilot_sha256"`
}

// worksheetTruth 表示 worksheet 中已经确认的一条人工真值。
type worksheetTruth struct {
	TraceID       string
	Label         string
	LegacyOverlap *bool
}

// pilotInputRecord 是冻结 pilot 的本地输入行，模型请求仍只取其中的 prompt。
type pilotInputRecord struct {
	TraceID string `json:"trace_id"`
	Prompt  string `json:"prompt"`
}

// buildCalibration 折叠人工 worksheet 和两遍定向强审真值，生成冻结校准集。
func buildCalibration(cfg calibrationConfig) (calibrationReport, error) {
	if cfg.SourcePath == "" || cfg.WorksheetPath == "" || cfg.OutputDir == "" {
		return calibrationReport{}, fmt.Errorf("calibration source, worksheet and output directory are required")
	}
	sourceRecords, err := loadSourceRecords(cfg.SourcePath)
	if err != nil {
		return calibrationReport{}, err
	}
	sourceByID := make(map[string]sourceRecord, len(sourceRecords))
	for _, record := range sourceRecords {
		sourceByID[record.TraceID] = record
	}
	worksheet, err := loadWorksheetTruth(cfg.WorksheetPath)
	if err != nil {
		return calibrationReport{}, err
	}
	directed, err := foldDirectedTruth(cfg, sourceByID)
	if err != nil {
		return calibrationReport{}, err
	}
	records, err := combineCalibrationTruth(sourceByID, worksheet, directed)
	if err != nil {
		return calibrationReport{}, err
	}
	assignCalibrationSplits(records)
	sort.Slice(records, func(left, right int) bool {
		return stableTraceIDHash(records[left].TraceID) < stableTraceIDHash(records[right].TraceID)
	})

	sourceHash, err := fileSHA256(cfg.SourcePath)
	if err != nil {
		return calibrationReport{}, err
	}
	worksheetHash, err := fileSHA256(cfg.WorksheetPath)
	if err != nil {
		return calibrationReport{}, err
	}
	report := summarizeCalibration(records, sourceHash, worksheetHash)
	absoluteOutput, err := ensureNewOutputDirectory(cfg.OutputDir)
	if err != nil {
		return calibrationReport{}, err
	}
	parent := filepath.Dir(absoluteOutput)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return calibrationReport{}, fmt.Errorf("create calibration parent: %w", err)
	}
	stage, err := os.MkdirTemp(parent, "."+filepath.Base(absoluteOutput)+".stage-*")
	if err != nil {
		return calibrationReport{}, fmt.Errorf("create calibration stage: %w", err)
	}
	defer func() { _ = os.RemoveAll(stage) }()

	calibrationDir := filepath.Join(stage, "calibration")
	pilotDir := filepath.Join(stage, "pilot")
	if err := os.MkdirAll(calibrationDir, 0o755); err != nil {
		return calibrationReport{}, fmt.Errorf("create calibration directory: %w", err)
	}
	if err := os.MkdirAll(pilotDir, 0o755); err != nil {
		return calibrationReport{}, fmt.Errorf("create pilot directory: %w", err)
	}
	calibrationPath := filepath.Join(calibrationDir, "calibration.jsonl")
	if err := writeJSONLines(calibrationPath, records); err != nil {
		return calibrationReport{}, err
	}
	calibrationHash, err := fileSHA256(calibrationPath)
	if err != nil {
		return calibrationReport{}, err
	}
	pilotRows := make([]pilotInputRecord, 0)
	for _, record := range records {
		if record.Split != calibrationSplitHoldout {
			continue
		}
		source, ok := sourceByID[record.TraceID]
		if !ok {
			return calibrationReport{}, fmt.Errorf("calibration trace_id %q is absent from source", record.TraceID)
		}
		pilotRows = append(pilotRows, pilotInputRecord{TraceID: record.TraceID, Prompt: source.Prompt})
	}
	pilotPath := filepath.Join(pilotDir, "layer1.input.jsonl")
	if err := writeJSONLines(pilotPath, pilotRows); err != nil {
		return calibrationReport{}, err
	}
	pilotHash, err := fileSHA256(pilotPath)
	if err != nil {
		return calibrationReport{}, err
	}
	report.CalibrationSHA256 = calibrationHash
	report.PilotSHA256 = pilotHash
	if err := writeJSONFile(filepath.Join(calibrationDir, "calibration-report.json"), report); err != nil {
		return calibrationReport{}, err
	}
	if err := os.Rename(stage, absoluteOutput); err != nil {
		return calibrationReport{}, fmt.Errorf("publish calibration output: %w", err)
	}
	return report, nil
}

// loadWorksheetTruth 读取 512 条 worksheet 的 label 和 overlap 真值。
func loadWorksheetTruth(path string) ([]worksheetTruth, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open worksheet %q: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read worksheet %q: %w", path, err)
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("worksheet %q has no truth rows", path)
	}
	header := make(map[string]int, len(records[0]))
	for index, name := range records[0] {
		header[name] = index
	}
	for _, name := range []string{"trace_id", "human_label", "human_legacy_overlap"} {
		if _, ok := header[name]; !ok {
			return nil, fmt.Errorf("worksheet %q is missing %s", path, name)
		}
	}
	truth := make([]worksheetTruth, 0, len(records)-1)
	seen := make(map[string]struct{}, len(records)-1)
	for index, record := range records[1:] {
		traceID := normalizedText(record[header["trace_id"]])
		label := normalizedText(record[header["human_label"]])
		overlapText := normalizedText(record[header["human_legacy_overlap"]])
		if traceID == "" {
			return nil, fmt.Errorf("worksheet row %d has empty trace_id", index+2)
		}
		if _, exists := seen[traceID]; exists {
			return nil, fmt.Errorf("worksheet trace_id %q is duplicated", traceID)
		}
		seen[traceID] = struct{}{}
		if label != "safe" && label != "unsafe" {
			return nil, fmt.Errorf("worksheet trace_id %q has invalid human_label", traceID)
		}
		overlap, err := parseOverlapTruth(overlapText)
		if err != nil {
			return nil, fmt.Errorf("worksheet trace_id %q: %w", traceID, err)
		}
		truth = append(truth, worksheetTruth{TraceID: traceID, Label: label, LegacyOverlap: overlap})
	}
	return truth, nil
}

// parseOverlapTruth 把 yes/no 人工重叠真值转换为布尔值。
func parseOverlapTruth(value string) (*bool, error) {
	switch value {
	case "yes":
		result := true
		return &result, nil
	case "no":
		result := false
		return &result, nil
	default:
		return nil, fmt.Errorf("human_legacy_overlap %q is invalid", value)
	}
}

// foldDirectedTruth 把第一遍和第二遍定向结果折叠为 265 条 label 真值。
func foldDirectedTruth(cfg calibrationConfig, sourceByID map[string]sourceRecord) (map[string]string, error) {
	if cfg.Layer1MappingPath == "" || cfg.Layer1DecisionsPath == "" {
		return map[string]string{}, nil
	}
	layer1Mappings, err := loadBatchMaps(cfg.Layer1MappingPath)
	if err != nil {
		return nil, err
	}
	layer1Decisions, err := loadReviewDecisionLines(cfg.Layer1DecisionsPath, layer1Mappings)
	if err != nil {
		return nil, err
	}
	layer1ByTrace := make(map[string]reviewDecision, 0)
	for batchIndex, mapping := range layer1Mappings {
		for _, item := range mapping.Items {
			if _, ok := sourceByID[item.TraceID]; !ok {
				return nil, fmt.Errorf("directed trace_id %q is absent from source", item.TraceID)
			}
			if _, exists := layer1ByTrace[item.TraceID]; exists {
				return nil, fmt.Errorf("directed trace_id %q is duplicated", item.TraceID)
			}
			layer1ByTrace[item.TraceID] = layer1Decisions[batchIndex][item.Index]
		}
	}
	layer2ByTrace := make(map[string]reviewDecision)
	if len(layer1ByTrace) != 0 && cfg.Layer2MappingPath != "" && cfg.Layer2DecisionsPath != "" {
		layer2Mappings, err := loadBatchMaps(cfg.Layer2MappingPath)
		if err != nil {
			return nil, err
		}
		layer2Decisions, err := loadReviewDecisionLines(cfg.Layer2DecisionsPath, layer2Mappings)
		if err != nil {
			return nil, err
		}
		for batchIndex, mapping := range layer2Mappings {
			for _, item := range mapping.Items {
				if _, exists := layer2ByTrace[item.TraceID]; exists {
					return nil, fmt.Errorf("second-pass trace_id %q is duplicated", item.TraceID)
				}
				layer2ByTrace[item.TraceID] = layer2Decisions[batchIndex][item.Index]
			}
		}
	}
	truth := make(map[string]string, len(layer1ByTrace))
	for traceID, first := range layer1ByTrace {
		final := first
		if first.Label == 0 || first.Label == 1 {
			second, ok := layer2ByTrace[traceID]
			if !ok {
				return nil, fmt.Errorf("directed trace_id %q has no second-pass truth", traceID)
			}
			final = second
		}
		switch final.Label {
		case 1:
			truth[traceID] = "safe"
		case 2:
			truth[traceID] = "unsafe"
		default:
			return nil, fmt.Errorf("directed trace_id %q has unresolved final label", traceID)
		}
	}
	for traceID := range layer2ByTrace {
		first, ok := layer1ByTrace[traceID]
		if !ok || (first.Label != 0 && first.Label != 1) {
			return nil, fmt.Errorf("second-pass trace_id %q was not routed", traceID)
		}
	}
	return truth, nil
}

// combineCalibrationTruth 合并两部分真值并拒绝重复 ID。
func combineCalibrationTruth(
	sourceByID map[string]sourceRecord,
	worksheet []worksheetTruth,
	directed map[string]string,
) ([]calibrationRecord, error) {
	records := make([]calibrationRecord, 0, len(worksheet)+len(directed))
	seen := make(map[string]struct{}, len(worksheet)+len(directed))
	for _, truth := range worksheet {
		if _, ok := sourceByID[truth.TraceID]; !ok {
			return nil, fmt.Errorf("worksheet trace_id %q is absent from source", truth.TraceID)
		}
		if _, exists := seen[truth.TraceID]; exists {
			return nil, fmt.Errorf("calibration trace_id %q is duplicated", truth.TraceID)
		}
		seen[truth.TraceID] = struct{}{}
		records = append(records, calibrationRecord{
			TraceID:       truth.TraceID,
			Label:         truth.Label,
			LegacyOverlap: truth.LegacyOverlap,
			TruthSource:   truthSourceWorksheet,
		})
	}
	directedIDs := make([]string, 0, len(directed))
	for traceID := range directed {
		directedIDs = append(directedIDs, traceID)
	}
	sort.Strings(directedIDs)
	for _, traceID := range directedIDs {
		if _, ok := sourceByID[traceID]; !ok {
			return nil, fmt.Errorf("directed trace_id %q is absent from source", traceID)
		}
		if _, exists := seen[traceID]; exists {
			return nil, fmt.Errorf("calibration trace_id %q conflicts across truth sources", traceID)
		}
		seen[traceID] = struct{}{}
		records = append(records, calibrationRecord{
			TraceID:     traceID,
			Label:       directed[traceID],
			TruthSource: truthSourceDirected,
		})
	}
	return records, nil
}

// assignCalibrationSplits 按来源、标签和 overlap 真值确定性分层切分。
func assignCalibrationSplits(records []calibrationRecord) {
	groups := make(map[string][]*calibrationRecord)
	for index := range records {
		record := &records[index]
		overlapKey := "unknown"
		if record.LegacyOverlap != nil {
			overlapKey = strconv.FormatBool(*record.LegacyOverlap)
		}
		key := record.TruthSource + "|" + record.Label + "|" + overlapKey
		groups[key] = append(groups[key], record)
	}
	for _, group := range groups {
		sort.Slice(group, func(left, right int) bool {
			return stableTraceIDHash(group[left].TraceID) < stableTraceIDHash(group[right].TraceID)
		})
		for index, record := range group {
			if index%2 == 0 {
				record.Split = calibrationSplitDevelopment
			} else {
				record.Split = calibrationSplitHoldout
			}
		}
	}
}

// summarizeCalibration 计算无 prompt 的聚合报告。
func summarizeCalibration(records []calibrationRecord, sourceHash string, worksheetHash string) calibrationReport {
	report := calibrationReport{
		SourceSHA256:      sourceHash,
		WorksheetSHA256:   worksheetHash,
		TotalCount:        len(records),
		LabelCounts:       map[string]int{},
		SplitCounts:       map[string]int{},
		TruthSourceCounts: map[string]int{},
	}
	for _, record := range records {
		report.LabelCounts[record.Label]++
		report.SplitCounts[record.Split]++
		report.TruthSourceCounts[record.TruthSource]++
		switch record.TruthSource {
		case truthSourceWorksheet:
			report.WorksheetCount++
		case truthSourceDirected:
			report.DirectedCount++
		}
		switch record.Label {
		case "unsafe":
			report.UnsafeCount++
		case "safe":
			report.SafeCount++
		}
		if record.LegacyOverlap != nil {
			if *record.LegacyOverlap {
				report.OverlapYesCount++
			} else {
				report.OverlapNoCount++
			}
		}
	}
	report.DevelopmentCount = report.SplitCounts[calibrationSplitDevelopment]
	report.HoldoutCount = report.SplitCounts[calibrationSplitHoldout]
	return report
}

// ensureNewOutputDirectory 校验输出目录尚不存在并返回绝对路径。
func ensureNewOutputDirectory(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve output directory: %w", err)
	}
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("output directory %q already exists", path)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat output directory: %w", err)
	}
	return absolute, nil
}
