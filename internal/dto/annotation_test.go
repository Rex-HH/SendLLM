package dto_test

import (
	"encoding/json"
	"testing"

	"sendllm/internal/dto"
)

func TestAnnotationRejectsUnknownTopLevelField(t *testing.T) {
	var annotation dto.Annotation
	err := json.Unmarshal([]byte(`{"label":"safe","explanation":"符合安全要求且没有攻击意图","unexpected":true}`), &annotation)
	if err == nil {
		t.Fatal("Unmarshal() error = nil, want error")
	}
}

func TestAnnotationPreservesExtendedInfoFields(t *testing.T) {
	var annotation dto.Annotation
	give := []byte(`{"label":"unsafe","explanation":"该内容包含需要拦截的攻击意图","extended_info":{"risk_type":"jailbreak","risk_level":"high","custom_reason":"example"}}`)
	if err := json.Unmarshal(give, &annotation); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
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
}
