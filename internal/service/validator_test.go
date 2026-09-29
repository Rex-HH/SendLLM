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
    "quality_score": {"type": "number", "minimum": 0, "maximum": 1},
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

const validatorSchemaV2 = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "additionalProperties": false,
  "required": ["is_attack", "case_type", "explanation", "quality_score", "extended_info"],
  "properties": {
    "is_attack": {"type": "boolean"},
    "case_type": {"type": "string", "enum": ["typical", "borderline", "variant", "hard_negative"]},
    "explanation": {"type": "string"},
    "quality_score": {"type": "number", "minimum": 0, "maximum": 1},
    "extended_info": {
      "type": "object",
      "required": ["attack_method", "attack_domain"],
      "properties": {
        "attack_method": {"type": "string"},
        "attack_domain": {"type": "string"},
        "risk_level": {"type": "string", "enum": ["low", "medium", "high"]},
        "attack_scenario": {"type": "string"},
        "other": {"type": "string"}
      },
      "additionalProperties": true
    }
  }
}`

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
		{name: "accepts quality score", give: `{"is_attack":false,"case_type":"typical","explanation":"内容没有攻击或规避安全控制的意图","quality_score":0.85}`},
		{name: "rejects out of range quality score", give: `{"is_attack":false,"case_type":"typical","explanation":"内容没有攻击或规避安全控制的意图","quality_score":1.2}`, wantErr: service.ErrInvalidResult},
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

func TestValidator_NewAttackLabels(t *testing.T) {
	tests := []struct {
		name    string
		give    string
		wantErr error
	}{
		{
			name: "accepts attack method",
			give: `{"is_attack":true,"case_type":"typical","explanation":"该内容包含需要拦截的攻击意图","quality_score":0.9,` +
				`"extended_info":{"attack_method":"prompt_injection","attack_domain":"","risk_level":"high"}}`,
		},
		{
			name: "accepts attack domain",
			give: `{"is_attack":true,"case_type":"typical","explanation":"该内容涉及国家安全风险","quality_score":0.9,` +
				`"extended_info":{"attack_method":"","attack_domain":"subversion_of_state_power","risk_level":"high"}}`,
		},
		{
			name: "accepts safe empty labels",
			give: `{"is_attack":false,"case_type":"typical","explanation":"内容没有攻击或规避安全控制的意图","quality_score":0.9,` +
				`"extended_info":{"attack_method":"","attack_domain":""}}`,
		},
		{
			name: "accepts hard negative empty labels",
			give: `{"is_attack":false,"case_type":"hard_negative","explanation":"内容描述风险但不包含攻击意图","quality_score":0.7,` +
				`"extended_info":{"attack_method":"","attack_domain":""}}`,
		},
		{
			name: "rejects unsafe without any label",
			give: `{"is_attack":true,"case_type":"typical","explanation":"该内容包含需要拦截的攻击意图","quality_score":0.9,` +
				`"extended_info":{"attack_method":"","attack_domain":"","risk_level":"high"}}`,
			wantErr: service.ErrInvalidResult,
		},
		{
			name: "rejects safe with attack label",
			give: `{"is_attack":false,"case_type":"typical","explanation":"内容没有攻击或规避安全控制的意图","quality_score":0.9,` +
				`"extended_info":{"attack_method":"prompt_injection","attack_domain":""}}`,
			wantErr: service.ErrInvalidResult,
		},
		{
			name: "rejects unknown attack method",
			give: `{"is_attack":true,"case_type":"typical","explanation":"该内容包含需要拦截的攻击意图","quality_score":0.9,` +
				`"extended_info":{"attack_method":"unknown","attack_domain":"","risk_level":"high"}}`,
			wantErr: service.ErrInvalidResult,
		},
		{
			name: "rejects domain in attack method",
			give: `{"is_attack":true,"case_type":"typical","explanation":"该内容包含需要拦截的攻击意图","quality_score":0.9,` +
				`"extended_info":{"attack_method":"subversion_of_state_power","attack_domain":"","risk_level":"high"}}`,
			wantErr: service.ErrInvalidResult,
		},
		{
			name: "rejects method in attack domain",
			give: `{"is_attack":true,"case_type":"typical","explanation":"该内容包含需要拦截的攻击意图","quality_score":0.9,` +
				`"extended_info":{"attack_method":"","attack_domain":"prompt_injection","risk_level":"high"}}`,
			wantErr: service.ErrInvalidResult,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			validator, err := service.NewValidator(
				[]byte(validatorSchemaV2),
				map[string]string{
					"prompt_injection":          "提示词注入攻击",
					"subversion_of_state_power": "煽动颠覆国家政权",
				},
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
