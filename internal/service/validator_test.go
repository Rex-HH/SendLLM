package service_test

import (
	"errors"
	"testing"

	"sendllm/internal/service"
)

const validatorSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "additionalProperties": false,
  "required": ["is_attack", "case_type", "explanation"],
  "properties": {
    "is_attack": {"type": "boolean"},
    "case_type": {"type": "string", "enum": ["typical", "borderline", "variant", "hard_negative"]},
    "explanation": {"type": "string"},
    "extended_info": {
      "type": "object",
      "properties": {
        "risk_type": {"type": "string"},
        "risk_level": {"type": "string", "enum": ["low", "medium", "high"]},
        "attack_scenario": {"type": "string"},
        "other": {"type": "string"}
      },
      "additionalProperties": true
    }
  }
}`

const validUnsafe = `{"is_attack":true,"case_type":"typical","explanation":"内容具有明确的攻击意图","extended_info":{"risk_type":"jailbreak","risk_level":"high"}}`
const validSafe = `{"is_attack":false,"case_type":"typical","explanation":"内容没有攻击或规避安全控制的意图"}`
const validHardNegative = `{"is_attack":false,"case_type":"hard_negative","explanation":"内容描述风险但不包含攻击意图","extended_info":{"risk_type":"jailbreak"}}`

func TestValidator_Validate(t *testing.T) {
	tests := []struct {
		name    string
		give    string
		wantErr error
	}{
		{name: "accepts unsafe", give: validUnsafe},
		{name: "accepts safe", give: validSafe},
		{name: "accepts hard negative", give: validHardNegative},
		{name: "rejects invalid JSON", give: `{`, wantErr: service.ErrInvalidResult},
		{name: "rejects surrounding prose", give: "result: " + validUnsafe, wantErr: service.ErrInvalidResult},
		{name: "rejects trailing prose", give: validUnsafe + " done", wantErr: service.ErrInvalidResult},
		{name: "rejects unknown top level field", give: `{"is_attack":false,"case_type":"typical","explanation":"内容没有攻击或规避安全控制的意图","unknown":true}`, wantErr: service.ErrInvalidResult},
		{name: "rejects unknown risk type", give: `{"is_attack":true,"case_type":"typical","explanation":"内容具有明确的攻击意图","extended_info":{"risk_type":"unknown","risk_level":"high"}}`, wantErr: service.ErrInvalidResult},
		{name: "rejects attack without risk level", give: `{"is_attack":true,"case_type":"typical","explanation":"内容具有明确的攻击意图","extended_info":{"risk_type":"jailbreak"}}`, wantErr: service.ErrInvalidResult},
		{name: "rejects non attack risk level", give: `{"is_attack":false,"case_type":"typical","explanation":"内容没有攻击或规避安全控制的意图","extended_info":{"risk_level":"low"}}`, wantErr: service.ErrInvalidResult},
		{name: "rejects hard negative without risk type", give: `{"is_attack":false,"case_type":"hard_negative","explanation":"内容描述风险但不包含攻击意图"}`, wantErr: service.ErrInvalidResult},
		{name: "rejects short explanation", give: `{"is_attack":false,"case_type":"typical","explanation":"太短"}`, wantErr: service.ErrInvalidResult},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			validator, err := service.NewValidator(
				[]byte(validatorSchema),
				map[string]string{"jailbreak": "越狱"},
				10,
				70,
			)
			if err != nil {
				t.Fatalf("NewValidator() error = %v", err)
			}

			_, err = validator.Validate([]byte(test.give))
			if test.wantErr == nil {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				return
			}
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Validate() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestValidator_EnforcesCoreEnumsWithPermissiveSchema(t *testing.T) {
	validator, err := service.NewValidator(
		[]byte(`{"type":"object"}`),
		map[string]string{"jailbreak": "越狱"},
		10,
		70,
	)
	if err != nil {
		t.Fatalf("NewValidator() error = %v", err)
	}
	tests := []struct {
		name string
		give string
	}{
		{
			name: "rejects unknown case type",
			give: `{"is_attack":false,"case_type":"unknown","explanation":"内容没有攻击或规避安全控制的意图"}`,
		},
		{
			name: "rejects unknown risk level",
			give: `{"is_attack":true,"case_type":"typical","explanation":"内容具有明确的攻击意图",` +
				`"extended_info":{"risk_type":"jailbreak","risk_level":"critical"}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := validator.Validate([]byte(test.give))
			if !errors.Is(err, service.ErrInvalidResult) {
				t.Fatalf("Validate() error = %v, want %v", err, service.ErrInvalidResult)
			}
		})
	}
}
