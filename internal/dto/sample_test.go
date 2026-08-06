package dto_test

import (
	"encoding/json"
	"errors"
	"testing"

	"sendllm/internal/dto"
)

func TestParseSourceResponseOptional(t *testing.T) {
	tests := []struct {
		name string
		give []byte
	}{
		{name: "missing", give: []byte(`{"trace_id":"id-1","prompt":"hello","source":"CERT"}`)},
		{name: "null", give: []byte(`{"trace_id":"id-1","prompt":"hello","response":null}`)},
		{name: "empty", give: []byte(`{"trace_id":"id-1","prompt":"hello","response":""}`)},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := dto.ParseSource(tt.give)
			if err != nil {
				t.Fatalf("ParseSource() error = %v", err)
			}
			if got.Response != "" {
				t.Errorf("Response = %q, want empty", got.Response)
			}
			if tt.name == "missing" && string(got.Extra["source"]) != `"CERT"` {
				t.Errorf("Extra[source] = %s, want CERT", got.Extra["source"])
			}
		})
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
