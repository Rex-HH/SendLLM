package service

import (
	"encoding/json"
	"testing"

	"sendllm/internal/dao"
	"sendllm/internal/dto"
)

func TestParseReconcileInput(t *testing.T) {
	raw := []byte(`{
		"id":"sample-1",
		"source":{"dataset":"v2_authoritative","path":"datasets/benchmarkV2.0/38-Categories/in-1-prompt-injection/samples.jsonl","index":0},
		"messages":[{"role":"user","content":"待审查提示词"}],
		"label":{"value":"unsafe","risk_type":"RT01","risk_level":"medium"},
		"meta":{"sample_type":"明确正例","split":"train","source_fields":{"reason":"原始理由。"}}
	}`)

	record, err := parseReconcileInput(raw)
	if err != nil {
		t.Fatalf("parseReconcileInput() error = %v", err)
	}
	if record.ID != "sample-1" {
		t.Fatalf("ID = %q, want sample-1", record.ID)
	}
	if record.Label.RiskLevel != "medium" {
		t.Fatalf("RiskLevel = %q, want medium", record.Label.RiskLevel)
	}
	if record.Meta.SourceFields.Reason != "原始理由。" {
		t.Fatalf("Reason = %q, want original reason", record.Meta.SourceFields.Reason)
	}
}

func TestReconcileRiskLabels(t *testing.T) {
	tests := []struct {
		givePath   string
		wantMethod string
		wantDomain string
	}{
		{
			givePath:   "datasets/benchmarkV2.0/38-Categories/in-1-prompt-injection/samples.jsonl",
			wantMethod: "prompt_injection",
		},
		{
			givePath:   "datasets/benchmarkV2.0/38-Categories/in-5-multimodal/in_05_multimodal_gold_2500.jsonl",
			wantMethod: "cross_modal_attack",
		},
		{
			givePath:   "datasets/benchmarkV2.0/38-Categories/out-A.1-g-false_harmful_information/samples.jsonl",
			wantDomain: "harmful_misinformation",
		},
		{
			givePath:   "datasets/benchmarkV2.0/38-Categories/out-A.4-e-privacy_right_infringement/最终样本.json",
			wantDomain: "privacy_right_infringement",
		},
	}

	for _, tt := range tests {
		t.Run(tt.givePath, func(t *testing.T) {
			method, domain := reconcileRiskLabels(tt.givePath)
			if method != tt.wantMethod || domain != tt.wantDomain {
				t.Fatalf(
					"reconcileRiskLabels() = (%q, %q), want (%q, %q)",
					method,
					domain,
					tt.wantMethod,
					tt.wantDomain,
				)
			}
		})
	}
}

func TestMappedReconcileLabelSafeEmptiesRiskFields(t *testing.T) {
	record := reconcileInput{
		Source: reconcileSource{
			Path: "datasets/benchmarkV2.0/38-Categories/in-1-prompt-injection/samples.jsonl",
		},
		Label: reconcileSourceLabel{Value: "safe"},
	}
	got := mappedReconcileLabel(record)
	if got.AttackMethod != "" || got.AttackDomain != "" {
		t.Fatalf(
			"mappedReconcileLabel() = (%q, %q), want empty safe labels",
			got.AttackMethod,
			got.AttackDomain,
		)
	}
}

func TestReconcileCaseType(t *testing.T) {
	tests := []struct {
		give string
		want string
	}{
		{give: "明确正例", want: "typical"},
		{give: "攻击正例", want: "typical"},
		{give: "边界正例", want: "borderline"},
		{give: "边界负例", want: "borderline"},
		{give: "困难负例", want: "borderline"},
		{give: "对抗正例", want: "variant"},
		{give: "对抗负例", want: "variant"},
		{give: "硬负例", want: "hard_negative"},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			if got := reconcileCaseType(tt.give); got != tt.want {
				t.Fatalf("reconcileCaseType(%q) = %q, want %q", tt.give, got, tt.want)
			}
		})
	}
}

func TestMergeReconcileAnnotation(t *testing.T) {
	record := reconcileInput{
		Source: reconcileSource{
			Path: "datasets/benchmarkV2.0/38-Categories/in-1-prompt-injection/samples.jsonl",
		},
		Label: reconcileSourceLabel{
			Value:     "unsafe",
			RiskLevel: "medium",
		},
		Meta: reconcileMeta{
			SampleType: "明确正例",
			SourceFields: struct {
				Reason string `json:"reason"`
			}{Reason: "原始标签理由。"},
		},
	}

	t.Run("all fields agree reuses original reason", func(t *testing.T) {
		annotation := dto.Annotation{
			IsAttack:    true,
			CaseType:    "typical",
			Explanation: "模型生成理由。",
			ExtendedInfo: &dto.ExtendedInfo{
				AttackMethod: "prompt_injection",
				AttackDomain: "harmful_misinformation",
				RiskLevel:    "medium",
			},
		}
		got := mergeReconcileAnnotation(record, mappedReconcileLabel(record), annotation)
		if got.Explanation != "原始标签理由。" {
			t.Fatalf("Explanation = %q, want original reason", got.Explanation)
		}
		if got.AttackMethod != "prompt_injection" || got.AttackDomain != "harmful_misinformation" {
			t.Fatalf("risk labels = (%q, %q), want original method plus model domain", got.AttackMethod, got.AttackDomain)
		}
	})

	t.Run("risk type matches and supplements missing model domain", func(t *testing.T) {
		annotation := dto.Annotation{
			IsAttack:    true,
			CaseType:    "typical",
			Explanation: "模型生成理由。",
			ExtendedInfo: &dto.ExtendedInfo{
				AttackMethod: "prompt_injection",
				RiskLevel:    "medium",
			},
		}
		got := mergeReconcileAnnotation(record, mappedReconcileLabel(record), annotation)
		if got.AttackMethod != "prompt_injection" {
			t.Fatalf("AttackMethod = %q, want prompt_injection", got.AttackMethod)
		}
		if got.AttackDomain != "" {
			t.Fatalf("AttackDomain = %q, want empty", got.AttackDomain)
		}
	})

	t.Run("mapped risk matches corresponding model risk field with supplement", func(t *testing.T) {
		domainRecord := record
		domainRecord.Source.Path = "datasets/benchmarkV2.0/38-Categories/out-A.1-g-false_harmful_information/samples.jsonl"
		annotation := dto.Annotation{
			IsAttack:    true,
			CaseType:    "typical",
			Explanation: "模型生成理由。",
			ExtendedInfo: &dto.ExtendedInfo{
				AttackMethod: "prompt_injection",
				AttackDomain: "harmful_misinformation",
				RiskLevel:    "medium",
			},
		}
		original := mappedReconcileLabel(domainRecord)
		merged := mergeReconcileAnnotation(domainRecord, original, annotation)
		if merged.Explanation != "原始标签理由。" {
			t.Fatalf("Explanation = %q, want original reason", merged.Explanation)
		}
		changes := reconcileFieldChanges(domainRecord, original, modelReconcileLabel(annotation), merged)
		for _, change := range changes {
			if change.Field == "risk_type" {
				t.Fatalf("changes contain risk_type mismatch: %#v", changes)
			}
		}
		if merged.AttackMethod != "prompt_injection" ||
			merged.AttackDomain != "harmful_misinformation" {
			t.Fatalf("risk labels = (%q, %q), want model labels", merged.AttackMethod, merged.AttackDomain)
		}
	})

	t.Run("mapped risk does not match wrong model risk field", func(t *testing.T) {
		domainRecord := record
		domainRecord.Source.Path = "datasets/benchmarkV2.0/38-Categories/out-A.1-g-false_harmful_information/samples.jsonl"
		annotation := dto.Annotation{
			IsAttack:    true,
			CaseType:    "typical",
			Explanation: "模型生成理由。",
			ExtendedInfo: &dto.ExtendedInfo{
				AttackMethod: "harmful_misinformation",
				RiskLevel:    "medium",
			},
		}
		original := mappedReconcileLabel(domainRecord)
		merged := mergeReconcileAnnotation(domainRecord, original, annotation)
		if merged.Explanation != "模型生成理由。" {
			t.Fatalf("Explanation = %q, want model reason", merged.Explanation)
		}
		changes := reconcileFieldChanges(domainRecord, original, modelReconcileLabel(annotation), merged)
		hasRiskChange := false
		for _, change := range changes {
			if change.Field == "risk_type" {
				hasRiskChange = true
			}
		}
		if !hasRiskChange {
			t.Fatalf("changes = %#v, want risk_type mismatch", changes)
		}
	})

	t.Run("different risk level uses model explanation", func(t *testing.T) {
		annotation := dto.Annotation{
			IsAttack:    true,
			CaseType:    "typical",
			Explanation: "模型认为风险更高。",
			ExtendedInfo: &dto.ExtendedInfo{
				AttackMethod: "prompt_injection",
				RiskLevel:    "high",
			},
		}
		got := mergeReconcileAnnotation(record, mappedReconcileLabel(record), annotation)
		if got.RiskLevel != "high" {
			t.Fatalf("RiskLevel = %q, want high", got.RiskLevel)
		}
		if got.Explanation != "模型认为风险更高。" {
			t.Fatalf("Explanation = %q, want model explanation", got.Explanation)
		}
	})
}

func TestBuildReconcileOutputMarshalsNewLabels(t *testing.T) {
	raw := json.RawMessage(`{
		"id":"sample-1",
		"source":{"dataset":"v2_authoritative","path":"datasets/benchmarkV2.0/38-Categories/in-1-prompt-injection/samples.jsonl","index":0},
		"messages":[{"role":"user","content":"待审查提示词"}],
		"label":{"value":"unsafe","risk_type":"RT01","risk_level":"medium"},
		"meta":{"sample_type":"明确正例","split":"train","source_fields":{"reason":"原始理由。"}}
	}`)
	record, err := parseReconcileInput(raw)
	if err != nil {
		t.Fatalf("parseReconcileInput() error = %v", err)
	}
	annotation := dto.Annotation{
		IsAttack:    true,
		CaseType:    "typical",
		Explanation: "模型理由。",
		ExtendedInfo: &dto.ExtendedInfo{
			AttackMethod: "prompt_injection",
			AttackDomain: "harmful_misinformation",
			RiskLevel:    "medium",
		},
	}
	encoded, err := json.Marshal(buildReconcileOutput("auto", record, annotation))
	if err != nil {
		t.Fatalf("Marshal(buildReconcileOutput()) error = %v", err)
	}
	var output map[string]any
	if err := json.Unmarshal(encoded, &output); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	extended, ok := output["extended_info"].(map[string]any)
	if !ok {
		t.Fatalf("extended_info = %#v, want object", output["extended_info"])
	}
	if extended["attack_method"] != "prompt_injection" || extended["attack_domain"] != "harmful_misinformation" {
		t.Fatalf("extended_info = %#v, want new risk labels", extended)
	}
	if _, exists := extended["risk_type"]; exists {
		t.Fatalf("extended_info contains legacy risk_type: %#v", extended)
	}
}

func TestReconcileRequestIncludesOriginalLabelAndMappedRisk(t *testing.T) {
	raw := []byte(`{
		"id":"sample-1",
		"source":{"dataset":"v2_authoritative","path":"datasets/benchmarkV2.0/38-Categories/in-1-prompt-injection/samples.jsonl","index":0},
		"messages":[{"role":"user","content":"待审查提示词"},{"role":"assistant","content":"模型回复"}],
		"label":{"value":"unsafe","risk_type":"RT01","risk_level":"medium"},
		"meta":{"sample_type":"明确正例","split":"train","source_fields":{"reason":"原始解释。"}}
	}`)
	item := dao.Item{
		TraceID:  "sample-1",
		RawJSON:  raw,
		Prompt:   "待审查提示词",
		Response: "模型回复",
	}
	request, err := reconcileRequest(item, ReconcileConfig{
		SystemPrompt: []byte("synthetic system prompt"),
		Scene:        "pair",
		Schema:       []byte(`{"type":"object"}`),
		Mode:         "json_object",
	})
	if err != nil {
		t.Fatalf("reconcileRequest() error = %v", err)
	}
	var payload struct {
		OriginalLabel       reconcileOriginalLabel `json:"original_label"`
		OriginalExplanation string                 `json:"original_explanation"`
	}
	if err := json.Unmarshal([]byte(request.Messages[1].Content), &payload); err != nil {
		t.Fatalf("reconcile user message is not JSON: %v", err)
	}
	if payload.OriginalExplanation != "原始解释。" {
		t.Fatalf("original_explanation = %q, want original reason", payload.OriginalExplanation)
	}
	if payload.OriginalLabel.Label != "unsafe" ||
		payload.OriginalLabel.RiskType != "RT01" ||
		payload.OriginalLabel.RiskLevel != "medium" ||
		payload.OriginalLabel.AttackMethod != "prompt_injection" {
		t.Fatalf("original_label = %#v, want original and mapped labels", payload.OriginalLabel)
	}
}
