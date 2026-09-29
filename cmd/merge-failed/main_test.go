package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestBuildAnnotationUnsafe(t *testing.T) {
	fields := map[string]json.RawMessage{
		"label": json.RawMessage(
			`{"value":"unsafe","risk_type":"cross_modal_attack","risk_level":"high"}`,
		),
		"meta": json.RawMessage(
			`{"sample_type":"边界正例","source_fields":{"reason":"原始原因一"}}`,
		),
	}

	got, err := buildAnnotation(fields)
	if err != nil {
		t.Fatalf("buildAnnotation error = %v", err)
	}

	var annotation fallbackAnnotation
	if err := json.Unmarshal(got, &annotation); err != nil {
		t.Fatalf("decode annotation error = %v", err)
	}
	want := fallbackAnnotation{
		Method:      "auto",
		IsAttack:    true,
		CaseType:    "borderline",
		Explanation: "原始原因一",
		ExtendedInfo: &fallbackExtendedInfo{
			RiskType:  "cross_modal_attack",
			RiskLevel: "high",
		},
	}
	if !reflect.DeepEqual(annotation, want) {
		t.Fatalf("annotation = %#v, want %#v", annotation, want)
	}
}

func TestBuildAnnotationSafe(t *testing.T) {
	fields := map[string]json.RawMessage{
		"label": json.RawMessage(`{"value":"safe"}`),
		"meta": json.RawMessage(
			`{"sample_type":"明确正例","source_fields":{"reason":"原始原因二"}}`,
		),
	}

	got, err := buildAnnotation(fields)
	if err != nil {
		t.Fatalf("buildAnnotation error = %v", err)
	}

	var annotation fallbackAnnotation
	if err := json.Unmarshal(got, &annotation); err != nil {
		t.Fatalf("decode annotation error = %v", err)
	}
	if annotation.IsAttack ||
		annotation.CaseType != "typical" ||
		annotation.ExtendedInfo != nil ||
		annotation.Explanation != "原始原因二" {
		t.Fatalf("annotation = %#v, want safe typical annotation", annotation)
	}
}

func TestBuildAnnotationRejectsUnknownLabel(t *testing.T) {
	fields := map[string]json.RawMessage{
		"label": json.RawMessage(`{"value":"unknown"}`),
	}

	if _, err := buildAnnotation(fields); err == nil {
		t.Fatal("buildAnnotation error = nil, want unknown label error")
	}
}

func TestTransformFailedRecordPreservesSource(t *testing.T) {
	input := json.RawMessage(
		`{
			"id":"sample-1",
			"source":{"dataset":"test","path":"samples.jsonl","index":0},
			"messages":[{"role":"user","content":"测试内容"}],
			"label":{"value":"unsafe","risk_type":"jailbreak","risk_level":"high"},
			"meta":{
				"sample_type":"明确正例",
				"source_fields":{"reason":"原始原因三"}
			},
			"annotation":{"method":"manual_required","is_attack":null,"error_category":"content_rejected"}
		}`,
	)

	got, err := transformFailedRecord(input)
	if err != nil {
		t.Fatalf("transformFailedRecord error = %v", err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(got, &fields); err != nil {
		t.Fatalf("decode transformed record error = %v", err)
	}
	for _, key := range []string{"id", "source", "messages", "label", "meta"} {
		if len(fields[key]) == 0 {
			t.Errorf("transformed record missing source field %q", key)
		}
	}

	var annotation map[string]json.RawMessage
	if err := json.Unmarshal(fields["annotation"], &annotation); err != nil {
		t.Fatalf("decode annotation error = %v", err)
	}
	if string(annotation["method"]) != `"auto"` {
		t.Fatalf("annotation.method = %s, want auto", annotation["method"])
	}
	if _, exists := annotation["error_category"]; exists {
		t.Fatalf("annotation still contains error_category: %s", annotation["error_category"])
	}
}

func TestMergeRecordsSkipsDuplicateFailedID(t *testing.T) {
	success := []json.RawMessage{
		json.RawMessage(`{"id":"sample-1"}`),
	}
	failed := []json.RawMessage{
		json.RawMessage(
			`{"id":"sample-1","label":{"value":"unsafe"},"meta":{"source_fields":{"reason":"原因一"}}}`,
		),
		json.RawMessage(
			`{"id":"sample-2","label":{"value":"safe"},"meta":{"source_fields":{"reason":"原因二"}}}`,
		),
	}

	merged, duplicateCount, err := mergeRecords(success, failed)
	if err != nil {
		t.Fatalf("mergeRecords error = %v", err)
	}
	if len(merged) != 2 {
		t.Fatalf("merged length = %d, want 2", len(merged))
	}
	if duplicateCount != 1 {
		t.Fatalf("duplicateCount = %d, want 1", duplicateCount)
	}

	var last struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(merged[1], &last); err != nil {
		t.Fatalf("decode last merged record error = %v", err)
	}
	if last.ID != "sample-2" {
		t.Fatalf("last merged id = %q, want sample-2", last.ID)
	}
}
