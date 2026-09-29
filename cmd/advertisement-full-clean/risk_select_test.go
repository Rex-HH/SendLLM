package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelectRiskPriorityPilotIsDeterministicAndBlind(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.json")
	calibrationPath := filepath.Join(directory, "calibration.jsonl")
	rows := make([]map[string]any, 0, 120)
	calibrationRows := make([]map[string]any, 0, 4)
	for index := 0; index < 120; index++ {
		label := "unsafe"
		source := "FGRC-SCD"
		scenario := "other_external_site"
		quality := 0.95
		if index < 40 {
			source = "Chinese-Spam-Dataset"
			scenario = "part_time_recruitment"
			quality = 0.70
		}
		if index >= 80 {
			label = "safe"
			source = "THUCNews"
			scenario = ""
			quality = 0.90
		}
		rows = append(rows, map[string]any{
			"trace_id": "risk-" + string(rune('A'+index%26)) + string(rune('a'+index/26)),
			"source":   source,
			"label":    label,
			"prompt":   strings.Repeat("x", 100+index),
			"extended_info": map[string]any{
				"attack_scenario": scenario,
				"is_attack":       label == "unsafe",
			},
			"annotation": map[string]any{"quality_score": quality},
		})
	}
	for index := 0; index < 4; index++ {
		calibrationRows = append(calibrationRows, map[string]any{
			"trace_id":       rows[index]["trace_id"],
			"label":          "unsafe",
			"truth_source":   truthSourceWorksheet,
			"split":          calibrationSplitHoldout,
			"legacy_overlap": false,
		})
	}
	writeCalibrationJSONArray(t, sourcePath, rows)
	writeCalibrationFile(t, calibrationPath, jsonLines(calibrationRows))
	outputDirectory := filepath.Join(directory, "risk-priority")
	report, err := selectRiskPriorityPilot(riskPilotConfig{
		SourcePath:      sourcePath,
		CalibrationPath: calibrationPath,
		OutputDir:       outputDirectory,
		HighRiskLimit:   30,
		ControlLimit:    10,
	})
	if err != nil {
		t.Fatalf("selectRiskPriorityPilot() error = %v", err)
	}
	if report.HighRiskCount != 30 || report.ControlCount != 10 || report.TotalCount != 40 {
		t.Fatalf("selection report = %+v", report)
	}
	pilot := readCalibrationJSONL(t, filepath.Join(outputDirectory, "pilot.input.jsonl"))
	if len(pilot) != 40 {
		t.Fatalf("pilot rows = %d", len(pilot))
	}
	for _, row := range pilot {
		if len(row) != 2 {
			t.Fatalf("pilot row keys = %v", row)
		}
		if _, ok := row["trace_id"]; !ok {
			t.Fatalf("pilot row missing trace_id: %v", row)
		}
		if _, ok := row["prompt"]; !ok {
			t.Fatalf("pilot row missing prompt: %v", row)
		}
		if _, ok := row["label"]; ok {
			t.Fatalf("pilot row leaked label: %v", row)
		}
	}
	manifest := readCalibrationJSONL(t, filepath.Join(outputDirectory, "selection.manifest.jsonl"))
	if len(manifest) != 40 {
		t.Fatalf("selection manifest rows = %d", len(manifest))
	}
	cohorts := map[string]int{}
	seen := map[string]bool{}
	for _, row := range manifest {
		cohorts[row["cohort"].(string)]++
		traceID := row["trace_id"].(string)
		if seen[traceID] {
			t.Fatalf("selection duplicate %q", traceID)
		}
		seen[traceID] = true
	}
	if cohorts["high_risk"] != 30 || cohorts["control"] != 10 {
		t.Fatalf("cohorts = %#v", cohorts)
	}
	reportBytes := readCalibrationFile(t, filepath.Join(outputDirectory, "selection-report.json"))
	if bytes.Contains(reportBytes, []byte("xxxx")) {
		t.Fatal("selection report leaked prompt")
	}

	secondOutput := filepath.Join(directory, "risk-priority-repeat")
	secondReport, err := selectRiskPriorityPilot(riskPilotConfig{
		SourcePath:      sourcePath,
		CalibrationPath: calibrationPath,
		OutputDir:       secondOutput,
		HighRiskLimit:   30,
		ControlLimit:    10,
	})
	if err != nil {
		t.Fatalf("second selectRiskPriorityPilot() error = %v", err)
	}
	if secondReport.PilotSHA256 != report.PilotSHA256 ||
		secondReport.SelectionSHA256 != report.SelectionSHA256 {
		t.Fatalf("selection is not deterministic: %+v vs %+v", report, secondReport)
	}
}

func TestSelectRiskPriorityPilotExcludesReviewedIDs(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.json")
	calibrationPath := filepath.Join(directory, "calibration.jsonl")
	rows := []map[string]any{
		{
			"trace_id":      "reviewed-high-risk",
			"source":        "Chinese-Spam-Dataset",
			"label":         "unsafe",
			"prompt":        "x",
			"extended_info": map[string]any{"attack_scenario": "part_time_recruitment"},
			"annotation":    map[string]any{"quality_score": 0.7},
		},
		{
			"trace_id":      "fresh-high-risk",
			"source":        "Chinese-Spam-Dataset",
			"label":         "unsafe",
			"prompt":        "x",
			"extended_info": map[string]any{"attack_scenario": "part_time_recruitment"},
			"annotation":    map[string]any{"quality_score": 0.7},
		},
		{
			"trace_id":      "control-safe",
			"source":        "THUCNews",
			"label":         "safe",
			"prompt":        "x",
			"extended_info": map[string]any{"attack_scenario": ""},
			"annotation":    map[string]any{"quality_score": 0.9},
		},
	}
	writeCalibrationJSONArray(t, sourcePath, rows)
	writeCalibrationFile(t, calibrationPath, jsonLines([]map[string]any{{
		"trace_id":       "reviewed-high-risk",
		"label":          "unsafe",
		"truth_source":   truthSourceWorksheet,
		"split":          calibrationSplitHoldout,
		"legacy_overlap": false,
	}}))
	outputDirectory := filepath.Join(directory, "risk-priority")
	if _, err := selectRiskPriorityPilot(riskPilotConfig{
		SourcePath:      sourcePath,
		CalibrationPath: calibrationPath,
		OutputDir:       outputDirectory,
		HighRiskLimit:   1,
		ControlLimit:    1,
	}); err != nil {
		t.Fatalf("selectRiskPriorityPilot() error = %v", err)
	}
	pilot := readCalibrationJSONL(t, filepath.Join(outputDirectory, "pilot.input.jsonl"))
	for _, row := range pilot {
		if row["trace_id"] == "reviewed-high-risk" {
			t.Fatal("selection included reviewed trace_id")
		}
	}
}

// ensure risk selection test uses standard JSON helpers.
var _ = json.Marshal
