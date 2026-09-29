package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvaluatePilotThresholds(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func([]map[string]any, []map[string]any)
		layer1    map[string]any
		wantError bool
	}{
		{name: "passes"},
		{
			name: "provider failure",
			mutate: func(calibration, predictions []map[string]any) {
				predictions[0] = map[string]any{
					"trace_id":       predictions[0]["trace_id"],
					"state":          "failed",
					"error_category": "provider_failure",
				}
			},
			wantError: true,
		},
		{
			name: "accuracy below threshold",
			mutate: func(calibration, predictions []map[string]any) {
				changePrediction(predictions, 0, 1)
				changePrediction(predictions, 1, 1)
				changePrediction(predictions, 2, 1)
			},
			wantError: true,
		},
		{
			name: "unsafe recall below threshold",
			mutate: func(calibration, predictions []map[string]any) {
				changePrediction(predictions, 0, 1)
				changePrediction(predictions, 1, 1)
			},
			wantError: true,
		},
		{
			name: "safe recall below threshold",
			mutate: func(calibration, predictions []map[string]any) {
				for index := 60; index < 62; index++ {
					changePrediction(predictions, index, 2)
				}
			},
			wantError: true,
		},
		{
			name: "source error leak",
			mutate: func(calibration, predictions []map[string]any) {
				changePrediction(predictions, 0, 1)
				clearRisk(predictions, 0)
			},
			wantError: true,
		},
		{
			name: "overlap routing below threshold",
			mutate: func(calibration, predictions []map[string]any) {
				clearRisk(predictions, 0)
				clearRisk(predictions, 1)
			},
			wantError: true,
		},
		{
			name:      "average occupancy below threshold",
			layer1:    map[string]any{"successful_requests": 13, "items_in_successful_requests": 100, "context_overflows": 0},
			wantError: true,
		},
		{
			name:      "context overflow",
			layer1:    map[string]any{"successful_requests": 12, "items_in_successful_requests": 100, "context_overflows": 1},
			wantError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			directory := t.TempDir()
			sourcePath := filepath.Join(directory, "source.json")
			calibrationPath := filepath.Join(directory, "calibration.jsonl")
			predictionsPath := filepath.Join(directory, "predictions.jsonl")
			layer1Path := filepath.Join(directory, "layer1-report.json")
			reportPath := filepath.Join(directory, "pilot-report.json")
			sourceRows, calibrationRows, predictions := evaluateFixture()
			if tt.mutate != nil {
				tt.mutate(calibrationRows, predictions)
			}
			writeCalibrationJSONArray(t, sourcePath, sourceRows)
			writeCalibrationFile(t, calibrationPath, jsonLines(calibrationRows))
			writeCalibrationFile(t, predictionsPath, jsonLines(predictions))
			layer1 := tt.layer1
			if layer1 == nil {
				layer1 = map[string]any{
					"successful_requests":          12,
					"items_in_successful_requests": 100,
					"context_overflows":            0,
				}
			}
			writeJSONFileForTest(t, layer1Path, layer1)
			report, err := evaluatePilot(evaluationConfig{
				CalibrationPath:  calibrationPath,
				SourcePath:       sourcePath,
				PredictionsPath:  predictionsPath,
				Layer1ReportPath: layer1Path,
				OutputPath:       reportPath,
			})
			if (err != nil) != tt.wantError {
				t.Fatalf("evaluatePilot() error = %v, wantError %v; report = %+v", err, tt.wantError, report)
			}
			if report.HoldoutCount != 100 {
				t.Fatalf("holdout count = %d", report.HoldoutCount)
			}
			reportBytes := readCalibrationFile(t, reportPath)
			if bytesContains(reportBytes, "不得打印") {
				t.Fatal("pilot report leaked prompt")
			}
		})
	}
}

// evaluateFixture 构造可通过全部固定阈值的 100 条 holdout 真值和预测。
func evaluateFixture() ([]map[string]any, []map[string]any, []map[string]any) {
	sourceRows := make([]map[string]any, 0, 100)
	calibrationRows := make([]map[string]any, 0, 100)
	predictions := make([]map[string]any, 0, 100)
	for index := 0; index < 100; index++ {
		traceID := "evaluate-" + string(rune('A'+index%26)) + string(rune('a'+index/26))
		humanLabel := "unsafe"
		if index >= 60 {
			humanLabel = "safe"
		}
		sourceLabel := humanLabel
		if index < 10 {
			sourceLabel = "safe"
		}
		overlap := index < 20
		sourceRows = append(sourceRows, map[string]any{
			"trace_id": traceID,
			"label":    sourceLabel,
			"prompt":   "不得打印",
		})
		calibrationRows = append(calibrationRows, map[string]any{
			"trace_id":       traceID,
			"label":          humanLabel,
			"legacy_overlap": overlap,
			"truth_source":   truthSourceWorksheet,
			"split":          calibrationSplitHoldout,
		})
		risk := ""
		if overlap {
			risk = "jailbreak"
		}
		label := 2
		if humanLabel == "safe" {
			label = 1
		}
		predictions = append(predictions, map[string]any{
			"trace_id": traceID,
			"state":    "succeeded",
			"l":        label,
			"x":        risk,
		})
	}
	return sourceRows, calibrationRows, predictions
}

// changePrediction 修改指定预测的 label。
func changePrediction(predictions []map[string]any, index int, label int) {
	predictions[index]["l"] = label
}

// clearRisk 清除指定预测的旧风险候选。
func clearRisk(predictions []map[string]any, index int) {
	predictions[index]["x"] = ""
}

// jsonLines 把对象切片编码为 JSONL 文本。
func jsonLines(rows []map[string]any) string {
	var builder strings.Builder
	for _, row := range rows {
		encoded, err := json.Marshal(row)
		if err != nil {
			panic(err)
		}
		builder.Write(encoded)
		builder.WriteByte('\n')
	}
	return builder.String()
}

// writeJSONFileForTest 写入测试 JSON 文件。
func writeJSONFileForTest(t *testing.T, path string, value any) {
	t.Helper()
	if err := writeJSONFile(path, value); err != nil {
		t.Fatalf("writeJSONFile(%q) error = %v", path, err)
	}
}

// bytesContains 判断字节内容是否包含文本。
func bytesContains(contents []byte, text string) bool {
	return bytes.Contains(contents, []byte(text))
}
