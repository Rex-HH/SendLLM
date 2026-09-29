package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildCalibrationFoldsTruthAndWritesPilot(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "advertisement.json")
	worksheetPath := filepath.Join(directory, "worksheet.csv")
	layer1MappingPath := filepath.Join(directory, "layer1-map.jsonl")
	layer1DecisionsPath := filepath.Join(directory, "layer1-decisions.jsonl")
	layer2MappingPath := filepath.Join(directory, "layer2-map.jsonl")
	layer2DecisionsPath := filepath.Join(directory, "layer2-decisions.jsonl")
	outputDirectory := filepath.Join(directory, "output")

	sourceRows := []map[string]any{
		calibrationRow("ws-1", "unsafe", "工作一"),
		calibrationRow("ws-2", "safe", "工作二"),
		calibrationRow("ws-3", "unsafe", "工作三"),
		calibrationRow("ws-4", "safe", "工作四"),
		calibrationRow("dr-1", "unsafe", "定向一"),
		calibrationRow("dr-2", "unsafe", "定向二"),
		calibrationRow("dr-3", "unsafe", "定向三"),
		calibrationRow("dr-4", "unsafe", "定向四"),
	}
	writeCalibrationJSONArray(t, sourcePath, sourceRows)
	writeCalibrationWorksheet(t, worksheetPath, []map[string]string{
		{"trace_id": "ws-1", "human_label": "unsafe", "human_legacy_overlap": "yes"},
		{"trace_id": "ws-2", "human_label": "safe", "human_legacy_overlap": "no"},
		{"trace_id": "ws-3", "human_label": "safe", "human_legacy_overlap": "no"},
		{"trace_id": "ws-4", "human_label": "unsafe", "human_legacy_overlap": "yes"},
	})
	writeCalibrationFile(t, layer1MappingPath,
		`{"batch":"batch-001.json","items":[{"i":0,"trace_id":"dr-1"},{"i":1,"trace_id":"dr-2"}]}`+"\n"+
			`{"batch":"batch-002.json","items":[{"i":0,"trace_id":"dr-3"},{"i":1,"trace_id":"dr-4"}]}`+"\n",
	)
	writeCalibrationFile(t, layer1DecisionsPath,
		`{"r":[{"i":0,"l":1,"t":"news_context"},{"i":1,"l":2,"x":""}]}`+"\n"+
			`{"r":[{"i":0,"l":0,"x":""},{"i":1,"l":2,"x":"jailbreak"}]}`+"\n",
	)
	writeCalibrationFile(t, layer2MappingPath,
		`{"batch":"batch-001.json","items":[{"i":0,"trace_id":"dr-1"},{"i":1,"trace_id":"dr-3"}]}`+"\n",
	)
	writeCalibrationFile(t, layer2DecisionsPath,
		`{"r":[{"i":0,"l":2,"x":""},{"i":1,"l":2,"x":""}]}`+"\n",
	)

	report, err := buildCalibration(calibrationConfig{
		SourcePath:          sourcePath,
		WorksheetPath:       worksheetPath,
		Layer1MappingPath:   layer1MappingPath,
		Layer1DecisionsPath: layer1DecisionsPath,
		Layer2MappingPath:   layer2MappingPath,
		Layer2DecisionsPath: layer2DecisionsPath,
		OutputDir:           outputDirectory,
	})
	if err != nil {
		t.Fatalf("buildCalibration() error = %v", err)
	}
	if report.TotalCount != 8 || report.WorksheetCount != 4 || report.DirectedCount != 4 {
		t.Fatalf("calibration counts = %+v", report)
	}
	if report.HoldoutCount == 0 || report.DevelopmentCount == 0 {
		t.Fatalf("calibration split = %+v", report)
	}
	if report.UnsafeCount != 6 || report.SafeCount != 2 {
		t.Fatalf("calibration labels = %+v", report)
	}

	manifest := readCalibrationJSONL(t, filepath.Join(outputDirectory, "calibration", "calibration.jsonl"))
	if len(manifest) != 8 {
		t.Fatalf("calibration manifest rows = %d", len(manifest))
	}
	seen := make(map[string]map[string]any, len(manifest))
	for _, row := range manifest {
		seen[row["trace_id"].(string)] = row
		if _, ok := row["prompt"]; ok {
			t.Fatal("calibration manifest contains prompt")
		}
	}
	if seen["ws-1"]["legacy_overlap"] != true || seen["ws-2"]["legacy_overlap"] != false {
		t.Fatalf("worksheet overlap truth = %#v", seen)
	}
	if _, ok := seen["dr-1"]["legacy_overlap"]; ok {
		t.Fatal("directed calibration fabricated legacy-overlap truth")
	}
	if seen["dr-1"]["label"] != "unsafe" || seen["dr-2"]["label"] != "unsafe" ||
		seen["dr-3"]["label"] != "unsafe" || seen["dr-4"]["label"] != "unsafe" {
		t.Fatalf("directed folded labels = %#v", seen)
	}
	if seen["dr-1"]["truth_source"] != "directed_265" {
		t.Fatalf("directed truth source = %#v", seen["dr-1"])
	}

	pilotRows := readCalibrationJSONL(t, filepath.Join(outputDirectory, "pilot", "layer1.input.jsonl"))
	if len(pilotRows) != report.HoldoutCount {
		t.Fatalf("pilot rows = %d, holdout = %d", len(pilotRows), report.HoldoutCount)
	}
	for _, row := range pilotRows {
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
	reportBytes := readCalibrationFile(t, filepath.Join(outputDirectory, "calibration", "calibration-report.json"))
	if bytes.Contains(reportBytes, []byte("工作一")) || bytes.Contains(reportBytes, []byte("定向一")) {
		t.Fatal("calibration report leaked prompt text")
	}
}

func TestBuildCalibrationRejectsConflictsAndMissingFolding(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "advertisement.json")
	worksheetPath := filepath.Join(directory, "worksheet.csv")
	layer1MappingPath := filepath.Join(directory, "layer1-map.jsonl")
	layer1DecisionsPath := filepath.Join(directory, "layer1-decisions.jsonl")
	writeCalibrationJSONArray(t, sourcePath, []map[string]any{
		calibrationRow("same-id", "unsafe", "内容"),
		calibrationRow("other-id", "unsafe", "内容"),
	})
	writeCalibrationWorksheet(t, worksheetPath, []map[string]string{{
		"trace_id": "same-id", "human_label": "unsafe", "human_legacy_overlap": "no",
	}})
	writeCalibrationFile(t, layer1MappingPath,
		`{"batch":"batch-001.json","items":[{"i":0,"trace_id":"same-id"}]}`+"\n")
	writeCalibrationFile(t, layer1DecisionsPath, `{"r":[{"i":0,"l":2,"x":""}]}`+"\n")
	_, err := buildCalibration(calibrationConfig{
		SourcePath:          sourcePath,
		WorksheetPath:       worksheetPath,
		Layer1MappingPath:   layer1MappingPath,
		Layer1DecisionsPath: layer1DecisionsPath,
		OutputDir:           filepath.Join(directory, "conflict"),
	})
	if err == nil {
		t.Fatal("buildCalibration() accepted duplicate truth source")
	}

	layer1DecisionsPath = filepath.Join(directory, "layer1-missing.jsonl")
	writeCalibrationFile(t, layer1DecisionsPath, `{"r":[{"i":0,"l":1,"x":""}]}`+"\n")
	_, err = buildCalibration(calibrationConfig{
		SourcePath:          sourcePath,
		WorksheetPath:       worksheetPath,
		Layer1MappingPath:   layer1MappingPath,
		Layer1DecisionsPath: layer1DecisionsPath,
		OutputDir:           filepath.Join(directory, "missing"),
	})
	if err == nil {
		t.Fatal("buildCalibration() accepted unresolved second-pass routing")
	}
}

func TestBuildCalibrationSplitIsStable(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "advertisement.json")
	worksheetPath := filepath.Join(directory, "worksheet.csv")
	rows := make([]map[string]any, 0, 20)
	worksheetRows := make([]map[string]string, 0, 20)
	for index := range 20 {
		traceID := "stable-" + string(rune('A'+index))
		label := "unsafe"
		if index%2 == 0 {
			label = "safe"
		}
		rows = append(rows, calibrationRow(traceID, label, "内容"))
		worksheetRows = append(worksheetRows, map[string]string{
			"trace_id": traceID, "human_label": label, "human_legacy_overlap": "no",
		})
	}
	writeCalibrationJSONArray(t, sourcePath, rows)
	writeCalibrationWorksheet(t, worksheetPath, worksheetRows)
	first := filepath.Join(directory, "first")
	second := filepath.Join(directory, "second")
	cfg := calibrationConfig{
		SourcePath:    sourcePath,
		WorksheetPath: worksheetPath,
		OutputDir:     first,
	}
	if _, err := buildCalibration(cfg); err != nil {
		t.Fatalf("first buildCalibration() error = %v", err)
	}
	cfg.OutputDir = second
	if _, err := buildCalibration(cfg); err != nil {
		t.Fatalf("second buildCalibration() error = %v", err)
	}
	for _, relative := range []string{
		filepath.Join("calibration", "calibration.jsonl"),
		filepath.Join("pilot", "layer1.input.jsonl"),
	} {
		firstBytes := readCalibrationFile(t, filepath.Join(first, relative))
		secondBytes := readCalibrationFile(t, filepath.Join(second, relative))
		if !bytes.Equal(firstBytes, secondBytes) {
			t.Fatalf("calibration output %s is not deterministic", relative)
		}
	}
}

// calibrationRow 构造校准测试使用的广告源记录。
func calibrationRow(traceID, label, prompt string) map[string]any {
	return map[string]any{
		"trace_id": traceID,
		"source":   "synthetic",
		"scene":    "prompt",
		"label":    label,
		"prompt":   prompt,
		"extended_info": map[string]any{
			"attack_scenario": "other_spam",
			"is_attack":       label == "unsafe",
		},
	}
}

// writeCalibrationJSONArray 写入校准测试 JSON 数组。
func writeCalibrationJSONArray(t *testing.T, path string, rows []map[string]any) {
	t.Helper()
	encoded, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("marshal calibration source: %v", err)
	}
	writeCalibrationFile(t, path, string(encoded))
}

// writeCalibrationWorksheet 写入校准测试 worksheet CSV。
func writeCalibrationWorksheet(t *testing.T, path string, rows []map[string]string) {
	t.Helper()
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	if err := writer.Write([]string{
		"trace_id",
		"source_json",
		"original_label",
		"attack_scenario",
		"model_label",
		"model_legacy_risk",
		"model_scenario_suspect",
		"issues",
		"human_label",
		"human_legacy_overlap",
		"human_notes",
	}); err != nil {
		t.Fatalf("write worksheet header: %v", err)
	}
	for _, row := range rows {
		if err := writer.Write([]string{
			row["trace_id"],
			"{}",
			"unsafe",
			"other_spam",
			"unsafe",
			"",
			"0",
			"",
			row["human_label"],
			row["human_legacy_overlap"],
			"",
		}); err != nil {
			t.Fatalf("write worksheet row: %v", err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		t.Fatalf("flush worksheet: %v", err)
	}
	writeCalibrationFile(t, path, buffer.String())
}

// writeCalibrationFile 写入校准测试文件。
func writeCalibrationFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

// readCalibrationFile 读取校准测试文件。
func readCalibrationFile(t *testing.T, path string) []byte {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	return contents
}

// readCalibrationJSONL 读取校准测试 JSONL。
func readCalibrationJSONL(t *testing.T, path string) []map[string]any {
	t.Helper()
	rows := make([]map[string]any, 0)
	for _, line := range strings.Split(strings.TrimSpace(string(readCalibrationFile(t, path))), "\n") {
		if line == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("decode calibration JSONL %s: %v", path, err)
		}
		rows = append(rows, row)
	}
	return rows
}
