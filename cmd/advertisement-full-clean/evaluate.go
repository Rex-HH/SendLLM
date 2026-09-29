package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const (
	minPilotAccuracy     = 0.98
	minHumanUnsafeRecall = 0.99
	minHumanSafeRecall   = 0.97
	minOverlapRouting    = 0.95
	minAverageOccupancy  = 8.0
)

// evaluationConfig 指定冻结 pilot 评估所需的只读输入。
type evaluationConfig struct {
	CalibrationPath  string
	SourcePath       string
	PredictionsPath  string
	Layer1ReportPath string
	OutputPath       string
}

// evaluationReport 只输出聚合指标和门禁结论，不包含 prompt。
type evaluationReport struct {
	HoldoutCount        int      `json:"holdout_count"`
	Accuracy            float64  `json:"accuracy"`
	HumanUnsafeRecall   float64  `json:"human_unsafe_recall"`
	UnsafePredictedSafe int      `json:"unsafe_predicted_safe"`
	HumanSafeRecall     float64  `json:"human_safe_recall"`
	SourceErrorRouting  float64  `json:"source_error_routing"`
	OverlapRouting      float64  `json:"legacy_overlap_routing"`
	CleanOverlapLeaks   int      `json:"clean_overlap_leaks"`
	FailedPredictions   int      `json:"failed_predictions"`
	ContextOverflows    int64    `json:"context_overflows"`
	AverageOccupancy    float64  `json:"average_occupancy"`
	Passed              bool     `json:"passed"`
	Failures            []string `json:"failures"`
}

// layer1PilotReport 是 pilot 第一层运行报告的聚合子集。
type layer1PilotReport struct {
	SuccessfulRequests        int64 `json:"successful_requests"`
	ItemsInSuccessfulRequests int64 `json:"items_in_successful_requests"`
	ContextOverflows          int64 `json:"context_overflows"`
}

// evaluatePilot 评估 holdout 指标；任一冻结阈值失败时返回错误。
func evaluatePilot(cfg evaluationConfig) (evaluationReport, error) {
	if cfg.CalibrationPath == "" || cfg.SourcePath == "" || cfg.PredictionsPath == "" ||
		cfg.Layer1ReportPath == "" || cfg.OutputPath == "" {
		return evaluationReport{}, fmt.Errorf("evaluation inputs and output path are required")
	}
	calibrationRows, err := loadCalibrationRecords(cfg.CalibrationPath)
	if err != nil {
		return evaluationReport{}, err
	}
	sourceByID, err := loadSourceLabelMap(cfg.SourcePath)
	if err != nil {
		return evaluationReport{}, err
	}
	predictions, err := loadLayerDecisions(cfg.PredictionsPath)
	if err != nil {
		return evaluationReport{}, err
	}
	layer1Report, err := loadLayer1PilotReport(cfg.Layer1ReportPath)
	if err != nil {
		return evaluationReport{}, err
	}
	holdout := make([]calibrationRecord, 0)
	for _, record := range calibrationRows {
		if record.Split == calibrationSplitHoldout {
			holdout = append(holdout, record)
		}
	}
	if len(holdout) == 0 {
		return evaluationReport{}, fmt.Errorf("calibration holdout is empty")
	}
	report := evaluationReport{HoldoutCount: len(holdout)}
	correct := 0
	humanUnsafe := 0
	humanUnsafeCorrect := 0
	humanSafe := 0
	humanSafeCorrect := 0
	sourceErrors := 0
	sourceErrorsRouted := 0
	overlaps := 0
	overlapsRouted := 0
	for _, truth := range holdout {
		sourceLabel, ok := sourceByID[truth.TraceID]
		if !ok {
			return evaluationReport{}, fmt.Errorf("calibration trace_id %q is absent from source", truth.TraceID)
		}
		prediction, exists := predictions[truth.TraceID]
		if !exists || prediction.State != "succeeded" || prediction.Label == nil {
			report.FailedPredictions++
			if truth.Label == "unsafe" {
				humanUnsafe++
			} else {
				humanSafe++
			}
			continue
		}
		switch truth.Label {
		case "unsafe":
			humanUnsafe++
			if *prediction.Label == 2 {
				humanUnsafeCorrect++
			}
			if *prediction.Label == 1 {
				report.UnsafePredictedSafe++
			}
		case "safe":
			humanSafe++
			if *prediction.Label == 1 {
				humanSafeCorrect++
			}
		}
		if (*prediction.Label == 1 && truth.Label == "safe") ||
			(*prediction.Label == 2 && truth.Label == "unsafe") {
			correct++
		}
		if truth.Label != sourceLabel {
			sourceErrors++
			if predictionIsRouted(sourceLabel, prediction) {
				sourceErrorsRouted++
			}
		}
		if truth.LegacyOverlap != nil && *truth.LegacyOverlap {
			overlaps++
			if prediction.Risk != "" || *prediction.Label == 0 {
				overlapsRouted++
			}
			if *prediction.Label != 0 && prediction.Risk == "" {
				report.CleanOverlapLeaks++
			}
		}
	}
	if humanUnsafe != 0 {
		report.HumanUnsafeRecall = float64(humanUnsafeCorrect) / float64(humanUnsafe)
	}
	if humanSafe != 0 {
		report.HumanSafeRecall = float64(humanSafeCorrect) / float64(humanSafe)
	}
	report.Accuracy = float64(correct) / float64(len(holdout))
	if sourceErrors != 0 {
		report.SourceErrorRouting = float64(sourceErrorsRouted) / float64(sourceErrors)
	} else {
		report.SourceErrorRouting = 1
	}
	if overlaps != 0 {
		report.OverlapRouting = float64(overlapsRouted) / float64(overlaps)
	} else {
		report.OverlapRouting = 1
	}
	if layer1Report.SuccessfulRequests != 0 {
		report.AverageOccupancy = float64(layer1Report.ItemsInSuccessfulRequests) /
			float64(layer1Report.SuccessfulRequests)
	}
	report.ContextOverflows = layer1Report.ContextOverflows
	report.Failures = evaluateFailures(report)
	report.Passed = len(report.Failures) == 0
	if err := writeJSONFile(cfg.OutputPath, report); err != nil {
		return evaluationReport{}, err
	}
	if !report.Passed {
		return report, fmt.Errorf("pilot thresholds failed: %s", strings.Join(report.Failures, ", "))
	}
	return report, nil
}

// predictionIsRouted 判断一条有效预测是否已从 clean 分流。
func predictionIsRouted(sourceLabel string, prediction layerDecision) bool {
	if prediction.Label == nil {
		return true
	}
	if *prediction.Label == 0 || prediction.Risk != "" {
		return true
	}
	return labelFromDecision(*prediction.Label) != sourceLabel
}

// evaluateFailures 返回全部未通过的固定阈值名称。
func evaluateFailures(report evaluationReport) []string {
	failures := make([]string, 0)
	if report.FailedPredictions != 0 {
		failures = append(failures, "provider_or_format_failures")
	}
	if report.Accuracy < minPilotAccuracy {
		failures = append(failures, "accuracy")
	}
	if report.HumanUnsafeRecall < minHumanUnsafeRecall || report.UnsafePredictedSafe != 0 {
		failures = append(failures, "human_unsafe_recall")
	}
	if report.HumanSafeRecall < minHumanSafeRecall {
		failures = append(failures, "human_safe_recall")
	}
	if report.SourceErrorRouting < 1 {
		failures = append(failures, "source_error_routing")
	}
	if report.OverlapRouting < minOverlapRouting || report.CleanOverlapLeaks != 0 {
		failures = append(failures, "legacy_overlap_routing")
	}
	if report.AverageOccupancy < minAverageOccupancy {
		failures = append(failures, "average_occupancy")
	}
	if report.ContextOverflows != 0 {
		failures = append(failures, "context_overflow")
	}
	return failures
}

// loadCalibrationRecords 严格读取冻结校准 manifest。
func loadCalibrationRecords(path string) ([]calibrationRecord, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read calibration %q: %w", path, err)
	}
	rows, err := readRawJSONLines(bytes.NewReader(contents))
	if err != nil {
		return nil, fmt.Errorf("read calibration JSONL %q: %w", path, err)
	}
	records := make([]calibrationRecord, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for index, raw := range rows {
		var record calibrationRecord
		if err := decodeStrictJSON(raw, &record); err != nil {
			return nil, fmt.Errorf("decode calibration row %d: %w", index+1, err)
		}
		if record.TraceID == "" || (record.Label != "safe" && record.Label != "unsafe") ||
			(record.Split != calibrationSplitDevelopment && record.Split != calibrationSplitHoldout) {
			return nil, fmt.Errorf("calibration row %d is invalid", index+1)
		}
		if _, exists := seen[record.TraceID]; exists {
			return nil, fmt.Errorf("calibration trace_id %q is duplicated", record.TraceID)
		}
		seen[record.TraceID] = struct{}{}
		records = append(records, record)
	}
	return records, nil
}

// loadSourceLabelMap 读取原始标签用于评估源标签错误路由。
func loadSourceLabelMap(path string) (map[string]string, error) {
	rows, err := loadSourceRecords(path)
	if err != nil {
		return nil, err
	}
	labels := make(map[string]string, len(rows))
	for _, row := range rows {
		labels[row.TraceID] = row.Label
	}
	return labels, nil
}

// loadLayer1PilotReport 读取第一层聚合报告。
func loadLayer1PilotReport(path string) (layer1PilotReport, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return layer1PilotReport{}, fmt.Errorf("read layer1 report %q: %w", path, err)
	}
	var report layer1PilotReport
	if err := json.Unmarshal(contents, &report); err != nil {
		return layer1PilotReport{}, fmt.Errorf("decode layer1 report: %w", err)
	}
	return report, nil
}
