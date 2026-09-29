package dto_test

import (
	"encoding/json"
	"testing"

	"sendllm/internal/dto"
)

func TestAnnotationRejectsUnknownTopLevelField(t *testing.T) {
	var annotation dto.Annotation
	err := json.Unmarshal(
		[]byte(`{"is_attack":false,"case_type":"typical","explanation":"符合安全要求且没有攻击意图","unexpected":true}`),
		&annotation,
	)
	if err == nil {
		t.Fatal("Unmarshal() error = nil, want error")
	}
}

func TestAnnotationPreservesExtendedInfoFields(t *testing.T) {
	var annotation dto.Annotation
	give := []byte(`{"is_attack":true,"case_type":"typical","explanation":"该内容包含需要拦截的攻击意图","extended_info":{"risk_type":"jailbreak","risk_level":"high","custom_reason":"example"}}`)
	if err := json.Unmarshal(give, &annotation); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if !annotation.IsAttack || annotation.CaseType != "typical" {
		t.Errorf("Annotation core = (%v, %q), want attack typical", annotation.IsAttack, annotation.CaseType)
	}
	if string(annotation.ExtendedInfo.Extra["custom_reason"]) != `"example"` {
		t.Errorf("Extra[custom_reason] = %s, want example", annotation.ExtendedInfo.Extra["custom_reason"])
	}
	encoded, err := json.Marshal(annotation)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if !json.Valid(encoded) || string(encoded) == "" {
		t.Errorf("Marshal() = %s, want valid JSON", encoded)
	}
	var roundTrip struct {
		ExtendedInfo map[string]json.RawMessage `json:"extended_info"`
	}
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatalf("Unmarshal(round trip) error = %v", err)
	}
	if got := string(roundTrip.ExtendedInfo["custom_reason"]); got != `"example"` {
		t.Errorf("round-trip custom_reason = %s, want example", got)
	}
}

func TestAnnotationPreservesQualityScore(t *testing.T) {
	var annotation dto.Annotation
	give := []byte(`{"is_attack":true,"case_type":"typical","explanation":"该内容包含需要拦截的攻击意图","quality_score":0.92}`)
	if err := json.Unmarshal(give, &annotation); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if annotation.QualityScore == nil || *annotation.QualityScore != 0.92 {
		t.Fatalf("QualityScore = %v, want 0.92", annotation.QualityScore)
	}

	encoded, err := json.Marshal(annotation)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var roundTrip map[string]any
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatalf("Unmarshal(round trip) error = %v", err)
	}
	if roundTrip["quality_score"] != 0.92 {
		t.Fatalf("round-trip quality_score = %#v, want 0.92", roundTrip["quality_score"])
	}
}

func TestAnnotationRejectsNonNumberQualityScore(t *testing.T) {
	var annotation dto.Annotation
	give := []byte(`{"is_attack":true,"case_type":"typical","explanation":"该内容包含需要拦截的攻击意图","quality_score":"high"}`)
	if err := json.Unmarshal(give, &annotation); err == nil {
		t.Fatal("Unmarshal() error = nil, want non-number quality_score error")
	}
}

func TestAnnotationPreservesAttackLabels(t *testing.T) {
	var annotation dto.Annotation
	give := []byte(`{
		"is_attack":true,
		"case_type":"typical",
		"explanation":"该内容包含需要拦截的攻击意图",
		"extended_info":{
			"attack_method":"prompt_injection",
			"attack_domain":"subversion_of_state_power",
			"risk_level":"high"
		}
	}`)
	if err := json.Unmarshal(give, &annotation); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if annotation.ExtendedInfo == nil {
		t.Fatal("ExtendedInfo = nil, want attack labels")
	}
	if !annotation.ExtendedInfo.HasAttackMethod() || !annotation.ExtendedInfo.HasAttackDomain() {
		t.Fatalf("attack label presence = (%v, %v), want true", annotation.ExtendedInfo.HasAttackMethod(), annotation.ExtendedInfo.HasAttackDomain())
	}
	if annotation.ExtendedInfo.AttackMethod != "prompt_injection" {
		t.Fatalf("AttackMethod = %q, want prompt_injection", annotation.ExtendedInfo.AttackMethod)
	}
	if annotation.ExtendedInfo.AttackDomain != "subversion_of_state_power" {
		t.Fatalf("AttackDomain = %q, want subversion_of_state_power", annotation.ExtendedInfo.AttackDomain)
	}
}
