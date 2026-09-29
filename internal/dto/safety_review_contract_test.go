package dto_test

import (
	"encoding/json"
	"strings"
	"testing"

	"sendllm/internal/dto"
)

// TestSafetyReviewContractEmptyCollections 验证空集合序列化为数组而不是 null。
func TestSafetyReviewContractEmptyCollections(t *testing.T) {
	judgment, err := json.Marshal(dto.SafetyReviewJudgment{})
	if err != nil {
		t.Fatalf("marshal judgment error = %v", err)
	}
	route, err := json.Marshal(dto.SafetyReviewRoute{})
	if err != nil {
		t.Fatalf("marshal route error = %v", err)
	}
	expert, err := json.Marshal(dto.SafetyReviewExpertResult{})
	if err != nil {
		t.Fatalf("marshal expert error = %v", err)
	}
	decision, err := json.Marshal(dto.SafetyReviewDecision{})
	if err != nil {
		t.Fatalf("marshal decision error = %v", err)
	}

	assertContains(t, judgment, `"evidence":[]`, `"attack_methods":[]`, `"attack_domains":[]`, `"exclusions":[]`)
	assertContains(t, route, `"features":[]`, `"attack_method_candidates":[]`, `"attack_domain_candidates":[]`)
	assertContains(t, expert, `"conditions":[]`, `"decisive_exclusions":[]`, `"evidence":[]`, `"evidence_source":[]`)
	assertContains(t, decision, `"attack_methods":[]`, `"attack_domains":[]`, `"evidence_basis":[]`, `"decision_rules":[]`)
	candidate, err := json.Marshal(dto.SafetyReviewCandidate{})
	if err != nil {
		t.Fatalf("marshal candidate error = %v", err)
	}
	condition, err := json.Marshal(dto.SafetyReviewCondition{})
	if err != nil {
		t.Fatalf("marshal condition error = %v", err)
	}
	exclusion, err := json.Marshal(dto.SafetyReviewExclusion{})
	if err != nil {
		t.Fatalf("marshal exclusion error = %v", err)
	}
	assertContains(t, candidate, `"evidence_ids":[]`)
	assertContains(t, condition, `"evidence_refs":[]`)
	assertContains(t, exclusion, `"evidence_refs":[]`)
	if strings.Contains(string(decision), "risk_level") {
		t.Fatal("decision contract must not contain risk_level")
	}
	if !strings.Contains(string(decision), `"primary_attack_method":""`) ||
		!strings.Contains(string(decision), `"primary_attack_domain":""`) ||
		!strings.Contains(string(decision), `"primary_risk_type":""`) {
		t.Fatalf("empty primary fields must serialize as empty strings: %s", decision)
	}
}

// assertContains 逐项断言 JSON 输出包含必需片段。
func assertContains(t *testing.T, raw []byte, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("JSON %s does not contain %s", raw, want)
		}
	}
}
