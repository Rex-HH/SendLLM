package main

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
)

func TestApplyApprovedChangesPreservesUnchangedRowsAndScenario(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.json")
	approvedPath := filepath.Join(directory, "approved.jsonl")
	outputPath := filepath.Join(directory, "cleaned.json")
	sourceRows := []map[string]any{
		applyRow("approve-safe", "unsafe", "other_spam", true),
		applyRow("approve-unsafe", "safe", "inducement_advertisement", false),
		applyRow("unchanged", "unsafe", "wechat_contact", true),
	}
	writeCalibrationJSONArray(t, sourcePath, sourceRows)
	writeCalibrationFile(t, approvedPath,
		`{"trace_id":"approve-safe","old_label":"unsafe","new_label":"safe","issue_codes":["label_change"]}`+"\n"+
			`{"trace_id":"approve-unsafe","old_label":"safe","new_label":"unsafe","issue_codes":["label_change"]}`+"\n",
	)
	report, err := applyApprovedChanges(applyConfig{
		SourcePath:          sourcePath,
		ApprovedChangesPath: approvedPath,
		OutputPath:          outputPath,
		Protected:           []string{sourcePath},
	})
	if err != nil {
		t.Fatalf("applyApprovedChanges() error = %v", err)
	}
	if report.Modified != 2 || report.Unchanged != 1 {
		t.Fatalf("apply report = %+v", report)
	}
	output := readCalibrationJSONArray(t, outputPath)
	if len(output) != 3 {
		t.Fatalf("output rows = %d", len(output))
	}
	if output[0]["trace_id"] != "approve-safe" ||
		output[1]["trace_id"] != "approve-unsafe" ||
		output[2]["trace_id"] != "unchanged" {
		t.Fatalf("output order = %#v", output)
	}
	safeExtended := output[0]["extended_info"].(map[string]any)
	if output[0]["label"] != "safe" ||
		safeExtended["risk_type"] != "" ||
		safeExtended["risk_level"] != "" ||
		safeExtended["attack_domain"] != "" ||
		safeExtended["attack_method"] != "" ||
		safeExtended["is_attack"] != false ||
		safeExtended["attack_scenario"] != "other_spam" {
		t.Fatalf("safe output = %#v", output[0])
	}
	unsafeExtended := output[1]["extended_info"].(map[string]any)
	if output[1]["label"] != "unsafe" ||
		unsafeExtended["attack_domain"] != "advertisement" ||
		unsafeExtended["attack_method"] != "" ||
		unsafeExtended["attack_scenario"] != "inducement_advertisement" {
		t.Fatalf("unsafe output = %#v", output[1])
	}
	if !reflect.DeepEqual(output[2], sourceRows[2]) {
		t.Fatalf("unchanged row changed = %#v", output[2])
	}
}

func TestApplyRefusesProtectedOutput(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.json")
	approvedPath := filepath.Join(directory, "approved.jsonl")
	writeCalibrationJSONArray(t, sourcePath, []map[string]any{
		applyRow("protected", "unsafe", "other_spam", true),
	})
	writeCalibrationFile(t, approvedPath, "")
	_, err := applyApprovedChanges(applyConfig{
		SourcePath:          sourcePath,
		ApprovedChangesPath: approvedPath,
		OutputPath:          sourcePath,
		Protected:           []string{sourcePath},
	})
	if err == nil {
		t.Fatal("applyApprovedChanges() overwrote protected source")
	}
}

// applyRow 构造 apply 测试使用的广告记录。
func applyRow(traceID, label, scenario string, unsafe bool) map[string]any {
	return map[string]any{
		"trace_id": traceID,
		"label":    label,
		"prompt":   "不得打印",
		"extended_info": map[string]any{
			"risk_type":       "spam_advertisement",
			"risk_level":      "medium",
			"attack_domain":   "external_site_advertisement",
			"attack_method":   "promotion",
			"attack_scenario": scenario,
			"is_attack":       unsafe,
			"unknown":         map[string]any{"keep": true},
		},
		"unknown_top": map[string]any{"keep": true},
	}
}

// readCalibrationJSONArray 读取测试 JSON 数组。
func readCalibrationJSONArray(t *testing.T, path string) []map[string]any {
	t.Helper()
	var rows []map[string]any
	if err := json.Unmarshal(readCalibrationFile(t, path), &rows); err != nil {
		t.Fatalf("decode JSON array %s: %v", path, err)
	}
	return rows
}
