package dto_test

import (
	"encoding/json"
	"errors"
	"testing"

	"sendllm/internal/dto"
)

func TestParseSourceCompactJSONL(t *testing.T) {
	give := []byte(`{"id":"dataset:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",` +
		`"source":{"dataset":"dataset","path":"source.json","index":7},` +
		`"messages":[{"role":"system","content":"ignored"},{"role":"user","content":"review this prompt"},` +
		`{"role":"assistant","content":"review this response"},{"role":"user","content":"later user"}],` +
		`"label":{"value":"unsafe","risk_type":"RT10_对抗性攻击","risk_level":"high"},` +
		`"meta":{"sample_id":"sample-7"}}`)

	got, err := dto.ParseSource(give)
	if err != nil {
		t.Fatalf("ParseSource() error = %v", err)
	}
	if got.TraceID != "dataset:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("TraceID = %q, want compact id", got.TraceID)
	}
	if got.Prompt != "review this prompt" || got.Response != "review this response" {
		t.Errorf("Prompt/Response = (%q, %q), want first user and assistant content", got.Prompt, got.Response)
	}

	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("Unmarshal(round trip) error = %v", err)
	}
	if string(fields["label"]) != `{"value":"unsafe","risk_type":"RT10_对抗性攻击","risk_level":"high"}` {
		t.Errorf("label = %s, want original compact label preserved", fields["label"])
	}
	if _, ok := fields["trace_id"]; ok {
		t.Errorf("round trip added legacy trace_id field: %s", fields["trace_id"])
	}
}

func TestParseSourceRejectsInvalidContent(t *testing.T) {
	tests := []struct {
		name string
		give []byte
	}{
		{name: "missing trace id", give: []byte(`{"prompt":"hello"}`)},
		{name: "empty content", give: []byte(`{"trace_id":"id-1","prompt":"","response":""}`)},
		{name: "response has wrong type", give: []byte(`{"trace_id":"id-1","response":1}`)},
		{
			name: "compact without usable messages",
			give: []byte(`{"id":"dataset:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",` +
				`"source":{"dataset":"dataset","path":"source.json","index":8},` +
				`"messages":[{"role":"tool","content":"not reviewed"}],"label":{"value":"safe"}}`),
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := dto.ParseSource(tt.give)
			if !errors.Is(err, dto.ErrInvalidSource) {
				t.Fatalf("ParseSource() error = %v, want %v", err, dto.ErrInvalidSource)
			}
		})
	}
}

func TestSourceSampleModelInput(t *testing.T) {
	tests := []struct {
		name      string
		sample    dto.SourceSample
		scene     string
		wantScene string
	}{
		{name: "uses explicit scene", sample: dto.SourceSample{TraceID: "id-1", Prompt: "p"}, scene: "response", wantScene: "response"},
		{name: "auto prompt", sample: dto.SourceSample{TraceID: "id-1", Prompt: "p"}, scene: "auto", wantScene: "prompt"},
		{name: "auto response", sample: dto.SourceSample{TraceID: "id-1", Response: "r"}, scene: "auto", wantScene: "response"},
		{name: "auto pair", sample: dto.SourceSample{TraceID: "id-1", Prompt: "p", Response: "r"}, scene: "auto", wantScene: "pair"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.sample.ModelInput(tt.scene)
			if got.Scene != tt.wantScene {
				t.Errorf("ModelInput().Scene = %q, want %q", got.Scene, tt.wantScene)
			}
		})
	}
}

func TestSourceSampleMarshalPreservesUnknownFields(t *testing.T) {
	sample, err := dto.ParseSource(
		[]byte(`{"trace_id":"id-1","prompt":"hello","metadata":{"sequence":9007199254740993}}`),
	)
	if err != nil {
		t.Fatalf("ParseSource() error = %v", err)
	}
	encoded, err := json.Marshal(sample)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("Unmarshal(round trip) error = %v", err)
	}
	if got := string(fields["metadata"]); got != `{"sequence":9007199254740993}` {
		t.Errorf("metadata = %s, want preserved unknown field", got)
	}
}
