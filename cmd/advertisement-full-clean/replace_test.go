package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReplaceOnlyAdvertisementIDsInProvisional(t *testing.T) {
	directory := t.TempDir()
	fullPath := filepath.Join(directory, "full.jsonl")
	cleanedPath := filepath.Join(directory, "cleaned.json")
	outputPath := filepath.Join(directory, "replaced.jsonl")
	baseRow := map[string]any{"trace_id": "base-1", "source": "v2_authoritative", "label": "safe"}
	adRow := map[string]any{"trace_id": "ad-1", "source": "advertisement", "label": "unsafe"}
	baseEncoded, _ := json.Marshal(baseRow)
	adEncoded, _ := json.Marshal(adRow)
	writeCalibrationFile(t, fullPath, string(baseEncoded)+"\n"+string(adEncoded)+"\n")
	cleanedRow := map[string]any{"trace_id": "ad-1", "source": "advertisement", "label": "safe"}
	writeCalibrationJSONArray(t, cleanedPath, []map[string]any{cleanedRow})
	report, err := replaceAdvertisementRows(replaceConfig{
		FullPath:    fullPath,
		CleanedPath: cleanedPath,
		OutputPath:  outputPath,
		Protected:   []string{fullPath},
	})
	if err != nil {
		t.Fatalf("replaceAdvertisementRows() error = %v", err)
	}
	if report.TotalRows != 2 || report.ReplacedRows != 1 || report.UnchangedRows != 1 {
		t.Fatalf("replace report = %+v", report)
	}
	rows := readCalibrationJSONL(t, outputPath)
	if len(rows) != 2 || rows[0]["trace_id"] != "base-1" || rows[1]["trace_id"] != "ad-1" {
		t.Fatalf("replaced rows = %#v", rows)
	}
	if !reflect.DeepEqual(rows[0], baseRow) {
		t.Fatalf("non-ad row changed = %#v", rows[0])
	}
	if rows[1]["label"] != "safe" {
		t.Fatalf("ad row was not replaced = %#v", rows[1])
	}
}

func TestReplaceRejectsMissingDuplicateAndExtraIDs(t *testing.T) {
	directory := t.TempDir()
	fullPath := filepath.Join(directory, "full.jsonl")
	cleanedPath := filepath.Join(directory, "cleaned.json")
	writeCalibrationFile(t, fullPath,
		`{"trace_id":"base-1","label":"safe"}`+"\n"+
			`{"trace_id":"ad-1","label":"unsafe"}`+"\n",
	)
	writeCalibrationJSONArray(t, cleanedPath, []map[string]any{
		{"trace_id": "missing", "label": "safe"},
	})
	_, err := replaceAdvertisementRows(replaceConfig{
		FullPath:    fullPath,
		CleanedPath: cleanedPath,
		OutputPath:  filepath.Join(directory, "missing.out.jsonl"),
	})
	if err == nil {
		t.Fatal("replaceAdvertisementRows() accepted missing ID")
	}
	writeCalibrationJSONArray(t, cleanedPath, []map[string]any{
		{"trace_id": "ad-1", "label": "safe"},
		{"trace_id": "ad-1", "label": "unsafe"},
	})
	_, err = replaceAdvertisementRows(replaceConfig{
		FullPath:    fullPath,
		CleanedPath: cleanedPath,
		OutputPath:  filepath.Join(directory, "duplicate.out.jsonl"),
	})
	if err == nil {
		t.Fatal("replaceAdvertisementRows() accepted duplicate cleaned ID")
	}
	writeCalibrationJSONArray(t, cleanedPath, []map[string]any{
		{"trace_id": "base-1", "label": "safe"},
	})
	writeCalibrationFile(t, fullPath,
		`{"trace_id":"base-1","label":"safe"}`+"\n"+
			`{"trace_id":"base-1","label":"unsafe"}`+"\n")
	_, err = replaceAdvertisementRows(replaceConfig{
		FullPath:    fullPath,
		CleanedPath: cleanedPath,
		OutputPath:  filepath.Join(directory, "extra.out.jsonl"),
	})
	if err == nil {
		t.Fatal("replaceAdvertisementRows() accepted duplicate full ID")
	}
}

func TestReplaceRefusesProtectedOutput(t *testing.T) {
	directory := t.TempDir()
	fullPath := filepath.Join(directory, "full.jsonl")
	cleanedPath := filepath.Join(directory, "cleaned.json")
	writeCalibrationFile(t, fullPath, `{"trace_id":"ad-1","label":"unsafe"}`+"\n")
	writeCalibrationJSONArray(t, cleanedPath, []map[string]any{{"trace_id": "ad-1", "label": "safe"}})
	before, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatalf("read full: %v", err)
	}
	_, err = replaceAdvertisementRows(replaceConfig{
		FullPath:    fullPath,
		CleanedPath: cleanedPath,
		OutputPath:  fullPath,
		Protected:   []string{fullPath},
	})
	if err == nil {
		t.Fatal("replaceAdvertisementRows() overwrote protected input")
	}
	after, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatalf("read full after: %v", err)
	}
	if !strings.EqualFold(string(before), string(after)) {
		t.Fatal("protected input changed")
	}
}
