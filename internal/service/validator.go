package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"sendllm/internal/dto"
)

// ErrInvalidResult 表示模型结果不满足任务校验契约。
var ErrInvalidResult = errors.New("service: invalid model result")

// ValidationError 表示可通过修复模型结果解决的校验失败。
type ValidationError struct {
	Problems []string
}

// Error 返回不包含模型原始内容的校验摘要。
func (e *ValidationError) Error() string {
	if e == nil || len(e.Problems) == 0 {
		return ErrInvalidResult.Error()
	}
	return ErrInvalidResult.Error() + ": " + strings.Join(e.Problems, "; ")
}

// Unwrap 允许调用方将校验失败识别为可修复结果错误。
func (e *ValidationError) Unwrap() error {
	return ErrInvalidResult
}

// Validator 持有任务 Schema 和 MASB 结果约束。
type Validator struct {
	schema         *jsonschema.Schema
	riskTypes      map[string]string
	minExplanation int
	maxExplanation int
}

// NewValidator 编译 Schema 并复制风险分类闭集。
func NewValidator(
	schema json.RawMessage,
	riskTypes map[string]string,
	minExplanation int,
	maxExplanation int,
) (*Validator, error) {
	if minExplanation < 1 || maxExplanation < minExplanation {
		return nil, fmt.Errorf("validator explanation length bounds are invalid")
	}

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("result-schema.json", bytes.NewReader(schema)); err != nil {
		return nil, fmt.Errorf("add result schema: %w", err)
	}
	compiled, err := compiler.Compile("result-schema.json")
	if err != nil {
		return nil, fmt.Errorf("compile result schema: %w", err)
	}

	clonedRiskTypes := make(map[string]string, len(riskTypes))
	for name, description := range riskTypes {
		clonedRiskTypes[name] = description
	}
	return &Validator{
		schema:         compiled,
		riskTypes:      clonedRiskTypes,
		minExplanation: minExplanation,
		maxExplanation: maxExplanation,
	}, nil
}

// Validate 校验单个 JSON 标注结果并返回严格解码后的结构。
func (v *Validator) Validate(raw []byte) (dto.Annotation, error) {
	decoded, err := decodeSingleJSON(raw)
	if err != nil {
		return dto.Annotation{}, invalidResult("invalid JSON")
	}
	if err := v.schema.Validate(decoded); err != nil {
		return dto.Annotation{}, invalidResult("schema validation failed")
	}

	var annotation dto.Annotation
	if err := json.Unmarshal(raw, &annotation); err != nil {
		return dto.Annotation{}, invalidResult("annotation decoding failed")
	}
	problems := v.businessProblems(annotation, decoded)
	if len(problems) != 0 {
		return dto.Annotation{}, &ValidationError{Problems: problems}
	}
	return annotation, nil
}

func decodeSingleJSON(raw []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("multiple JSON values")
		}
		return nil, err
	}
	return decoded, nil
}

func (v *Validator) businessProblems(annotation dto.Annotation, decoded any) []string {
	problems := make([]string, 0, 3)
	if annotation.Label != "safe" && annotation.Label != "unsafe" {
		problems = append(problems, "label is invalid")
	}
	explanationLength := utf8.RuneCountInString(annotation.Explanation)
	if explanationLength < v.minExplanation || explanationLength > v.maxExplanation {
		problems = append(problems, "explanation length is out of range")
	}

	extended, _ := decoded.(map[string]any)["extended_info"].(map[string]any)
	if annotation.ExtendedInfo != nil && annotation.ExtendedInfo.RiskLevel != "" {
		switch annotation.ExtendedInfo.RiskLevel {
		case "low", "medium", "high":
		default:
			problems = append(problems, "risk_level is invalid")
		}
	}
	if annotation.Label == "unsafe" {
		if annotation.ExtendedInfo == nil || annotation.ExtendedInfo.RiskType == "" {
			problems = append(problems, "unsafe result requires risk_type")
		} else if _, ok := v.riskTypes[annotation.ExtendedInfo.RiskType]; !ok {
			problems = append(problems, "risk_type is not configured")
		}
		if annotation.ExtendedInfo == nil || annotation.ExtendedInfo.RiskLevel == "" {
			problems = append(problems, "unsafe result requires risk_level")
		}
	}
	if annotation.Label == "safe" && annotation.ExtendedInfo != nil {
		if _, ok := extended["risk_type"]; ok {
			problems = append(problems, "safe result must not include risk_type")
		}
		if _, ok := extended["risk_level"]; ok {
			problems = append(problems, "safe result must not include risk_level")
		}
		if annotation.ExtendedInfo.IsAttack == nil || *annotation.ExtendedInfo.IsAttack {
			problems = append(problems, "safe result requires is_attack false")
		}
	}
	if annotation.ExtendedInfo != nil && annotation.ExtendedInfo.CaseType == "hard_negative" && annotation.Label != "safe" {
		problems = append(problems, "hard_negative requires safe label")
	}
	return problems
}

func invalidResult(problem string) error {
	return &ValidationError{Problems: []string{problem}}
}
