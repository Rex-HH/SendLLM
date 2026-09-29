package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestRouteLayer2CoversDeterministicReasonsAndBlindPayload(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.json")
	layer1Path := filepath.Join(directory, "layer1.jsonl")
	writeCalibrationJSONArray(t, sourcePath, []map[string]any{
		calibrationRow("route-change", "unsafe", "变更"),
		calibrationRow("route-uncertain", "unsafe", "不确定"),
		calibrationRow("route-overlap", "unsafe", "重叠"),
		calibrationRow("route-failure", "unsafe", "失败"),
		calibrationRow("route-unchanged", "unsafe", "不变"),
	})
	writeCalibrationFile(t, layer1Path,
		`{"trace_id":"route-change","state":"succeeded","l":1,"x":""}`+"\n"+
			`{"trace_id":"route-uncertain","state":"succeeded","l":0,"x":""}`+"\n"+
			`{"trace_id":"route-overlap","state":"succeeded","l":2,"x":"jailbreak"}`+"\n"+
			`{"trace_id":"route-failure","state":"failed","error_category":"provider_failure"}`+"\n"+
			`{"trace_id":"route-unchanged","state":"succeeded","l":2,"x":""}`+"\n",
	)
	outputDirectory := filepath.Join(directory, "route")
	report, err := routeLayer2(routeConfig{
		SourcePath:          sourcePath,
		Layer1DecisionsPath: layer1Path,
		OutputDir:           outputDirectory,
		BatchSize:           32,
		BatchMaxInputTokens: 80000,
	})
	if err != nil {
		t.Fatalf("routeLayer2() error = %v", err)
	}
	if report.RoutedCount != 4 || report.UnchangedCount != 1 {
		t.Fatalf("route report = %+v", report)
	}
	if report.ReasonCounts["label_change"] != 1 ||
		report.ReasonCounts["uncertain"] != 1 ||
		report.ReasonCounts["legacy_overlap"] != 1 ||
		report.ReasonCounts["provider_failure"] != 1 {
		t.Fatalf("route reasons = %+v", report.ReasonCounts)
	}
	layer2Rows := readCalibrationJSONL(t, filepath.Join(outputDirectory, "layer2.input.jsonl"))
	if len(layer2Rows) != 4 {
		t.Fatalf("layer2 input rows = %d", len(layer2Rows))
	}
	for _, row := range layer2Rows {
		if len(row) != 2 {
			t.Fatalf("layer2 input keys = %v", row)
		}
	}
	if layer2Rows[0]["trace_id"] != "route-change" || layer2Rows[3]["trace_id"] != "route-failure" {
		t.Fatalf("layer2 order = %#v", layer2Rows)
	}
	batches := readCalibrationJSONL(t, filepath.Join(outputDirectory, "layer2.batches", "batch-001.json"))
	if len(batches) != 1 {
		t.Fatalf("layer2 batches = %d", len(batches))
	}
	batchBytes := readCalibrationFile(t, filepath.Join(outputDirectory, "layer2.batches", "batch-001.json"))
	var payload struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(batchBytes, &payload); err != nil {
		t.Fatalf("decode layer2 batch: %v", err)
	}
	if len(payload.Items) != 4 {
		t.Fatalf("layer2 batch items = %d", len(payload.Items))
	}
	for _, item := range payload.Items {
		if len(item) != 2 {
			t.Fatalf("layer2 batch keys = %v", item)
		}
		if _, ok := item["i"]; !ok {
			t.Fatalf("layer2 batch missing i: %v", item)
		}
		if _, ok := item["p"]; !ok {
			t.Fatalf("layer2 batch missing p: %v", item)
		}
	}
	if bytes.Contains(batchBytes, []byte("route-change")) || bytes.Contains(batchBytes, []byte("trace_id")) {
		t.Fatalf("layer2 batch leaked stable ID: %s", batchBytes)
	}
	mappingRows := readCalibrationJSONL(t, filepath.Join(outputDirectory, "layer2.batch-map.jsonl"))
	if len(mappingRows) != 1 {
		t.Fatalf("layer2 mapping rows = %d", len(mappingRows))
	}
}

func TestRouteLayer2RejectsMissingLayer1Coverage(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.json")
	layer1Path := filepath.Join(directory, "layer1.jsonl")
	writeCalibrationJSONArray(t, sourcePath, []map[string]any{
		calibrationRow("route-a", "unsafe", "甲"),
		calibrationRow("route-b", "unsafe", "乙"),
	})
	writeCalibrationFile(t, layer1Path, `{"trace_id":"route-a","state":"succeeded","l":2,"x":""}`+"\n")
	_, err := routeLayer2(routeConfig{
		SourcePath:          sourcePath,
		Layer1DecisionsPath: layer1Path,
		OutputDir:           filepath.Join(directory, "route"),
		BatchSize:           32,
		BatchMaxInputTokens: 80000,
	})
	if err == nil {
		t.Fatal("routeLayer2() accepted missing layer-1 coverage")
	}
}
