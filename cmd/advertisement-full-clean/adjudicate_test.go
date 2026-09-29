package main

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestAdjudicateProducesMutuallyExclusiveManifests(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.json")
	layer1Path := filepath.Join(directory, "layer1.jsonl")
	layer2Path := filepath.Join(directory, "layer2.jsonl")
	writeCalibrationJSONArray(t, sourcePath, []map[string]any{
		calibrationRow("adj-unsafe-safe", "unsafe", "改安全"),
		calibrationRow("adj-safe-unsafe", "safe", "改不安全"),
		calibrationRow("adj-disagree", "unsafe", "不一致"),
		calibrationRow("adj-uncertain", "unsafe", "不确定"),
		calibrationRow("adj-overlap", "unsafe", "重叠"),
		calibrationRow("adj-unchanged", "unsafe", "不变"),
	})
	writeCalibrationFile(t, layer1Path,
		`{"trace_id":"adj-unsafe-safe","state":"succeeded","l":1,"x":""}`+"\n"+
			`{"trace_id":"adj-safe-unsafe","state":"succeeded","l":2,"x":""}`+"\n"+
			`{"trace_id":"adj-disagree","state":"succeeded","l":1,"x":""}`+"\n"+
			`{"trace_id":"adj-uncertain","state":"succeeded","l":0,"x":""}`+"\n"+
			`{"trace_id":"adj-overlap","state":"succeeded","l":2,"x":"jailbreak"}`+"\n"+
			`{"trace_id":"adj-unchanged","state":"succeeded","l":2,"x":""}`+"\n",
	)
	writeCalibrationFile(t, layer2Path,
		`{"trace_id":"adj-unsafe-safe","state":"succeeded","l":1,"x":""}`+"\n"+
			`{"trace_id":"adj-safe-unsafe","state":"succeeded","l":2,"x":""}`+"\n"+
			`{"trace_id":"adj-disagree","state":"succeeded","l":2,"x":""}`+"\n"+
			`{"trace_id":"adj-uncertain","state":"succeeded","l":1,"x":""}`+"\n"+
			`{"trace_id":"adj-overlap","state":"succeeded","l":2,"x":"jailbreak"}`+"\n",
	)
	outputDirectory := filepath.Join(directory, "adjudicate")
	report, err := adjudicate(adjudicateConfig{
		SourcePath:          sourcePath,
		Layer1DecisionsPath: layer1Path,
		Layer2DecisionsPath: layer2Path,
		OutputDir:           outputDirectory,
	})
	if err != nil {
		t.Fatalf("adjudicate() error = %v", err)
	}
	if report.ApprovedCount != 2 || report.OverlapCount != 1 ||
		report.QuarantineCount != 2 || report.UnchangedCount != 1 {
		t.Fatalf("adjudication report = %+v", report)
	}
	approved := readCalibrationJSONL(t, filepath.Join(outputDirectory, "approved-changes.jsonl"))
	overlap := readCalibrationJSONL(t, filepath.Join(outputDirectory, "legacy-overlap.jsonl"))
	quarantine := readCalibrationJSONL(t, filepath.Join(outputDirectory, "quarantine.jsonl"))
	if len(approved) != 2 || len(overlap) != 1 || len(quarantine) != 2 {
		t.Fatalf("manifest counts = %d/%d/%d", len(approved), len(overlap), len(quarantine))
	}
	approvedIDs := map[string]string{}
	for _, row := range approved {
		approvedIDs[row["trace_id"].(string)] = row["new_label"].(string)
	}
	if approvedIDs["adj-unsafe-safe"] != "safe" || approvedIDs["adj-safe-unsafe"] != "unsafe" {
		t.Fatalf("approved IDs = %#v", approvedIDs)
	}
	if overlap[0]["trace_id"] != "adj-overlap" || overlap[0]["risk"] != "jailbreak" {
		t.Fatalf("overlap manifest = %#v", overlap)
	}
	quarantineIDs := map[string]bool{}
	for _, row := range quarantine {
		quarantineIDs[row["trace_id"].(string)] = true
	}
	if !quarantineIDs["adj-disagree"] || !quarantineIDs["adj-uncertain"] {
		t.Fatalf("quarantine IDs = %#v", quarantineIDs)
	}
	all := append(append(approved, overlap...), quarantine...)
	seen := map[string]bool{}
	for _, row := range all {
		traceID := row["trace_id"].(string)
		if seen[traceID] {
			t.Fatalf("manifest trace_id %q is not unique", traceID)
		}
		seen[traceID] = true
	}
	for _, path := range []string{
		filepath.Join(outputDirectory, "approved-changes.jsonl"),
		filepath.Join(outputDirectory, "legacy-overlap.jsonl"),
		filepath.Join(outputDirectory, "quarantine.jsonl"),
		filepath.Join(outputDirectory, "adjudication-report.json"),
	} {
		if bytes.Contains(readCalibrationFile(t, path), []byte("改安全")) {
			t.Fatalf("manifest leaked prompt: %s", path)
		}
	}
}

func TestAdjudicateMissingLayer2BecomesQuarantine(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.json")
	layer1Path := filepath.Join(directory, "layer1.jsonl")
	layer2Path := filepath.Join(directory, "layer2.jsonl")
	writeCalibrationJSONArray(t, sourcePath, []map[string]any{
		calibrationRow("adj-missing", "unsafe", "缺项"),
	})
	writeCalibrationFile(t, layer1Path, `{"trace_id":"adj-missing","state":"succeeded","l":1,"x":""}`+"\n")
	writeCalibrationFile(t, layer2Path, "")
	report, err := adjudicate(adjudicateConfig{
		SourcePath:          sourcePath,
		Layer1DecisionsPath: layer1Path,
		Layer2DecisionsPath: layer2Path,
		OutputDir:           filepath.Join(directory, "adjudicate"),
	})
	if err != nil {
		t.Fatalf("adjudicate() error = %v", err)
	}
	if report.QuarantineCount != 1 {
		t.Fatalf("missing layer2 report = %+v", report)
	}
	quarantine := readCalibrationJSONL(t, filepath.Join(directory, "adjudicate", "quarantine.jsonl"))
	if len(quarantine) != 1 || quarantine[0]["trace_id"] != "adj-missing" {
		t.Fatalf("missing layer2 quarantine = %#v", quarantine)
	}
}
