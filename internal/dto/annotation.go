package dto

import (
	"encoding/json"
	"fmt"
)

// ExtendedInfo 表示 MASB 风险信息及 Schema 允许的扩展字段。
type ExtendedInfo struct {
	RiskType       string
	RiskLevel      string
	AttackScenario string
	CaseType       string
	IsAttack       *bool
	Other          string
	Extra          map[string]json.RawMessage
}

// Annotation 是模型返回的结构化安全标注。
type Annotation struct {
	Label        string        `json:"label"`
	Explanation  string        `json:"explanation"`
	ExtendedInfo *ExtendedInfo `json:"extended_info,omitempty"`
}

// AnnotationMeta 记录人工或自动标注的审核元数据。
type AnnotationMeta struct {
	Method       string   `json:"method"`
	ReviewedBy   string   `json:"reviewed_by,omitempty"`
	QualityScore *float64 `json:"quality_score,omitempty"`
}

// UnmarshalJSON 拒绝标注顶层未知字段。
func (a *Annotation) UnmarshalJSON(raw []byte) error {
	fields := make(map[string]json.RawMessage)
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	label, err := takeAnnotationString(fields, "label", false)
	if err != nil {
		return err
	}
	explanation, err := takeAnnotationString(fields, "explanation", false)
	if err != nil {
		return err
	}
	var extendedInfo *ExtendedInfo
	if value, ok := fields["extended_info"]; ok {
		delete(fields, "extended_info")
		if string(value) != "null" {
			extendedInfo = &ExtendedInfo{}
			if err := json.Unmarshal(value, extendedInfo); err != nil {
				return fmt.Errorf("decode extended_info: %w", err)
			}
		}
	}
	if len(fields) != 0 {
		return fmt.Errorf("annotation has unknown field %q", firstKey(fields))
	}
	a.Label = label
	a.Explanation = explanation
	a.ExtendedInfo = extendedInfo
	return nil
}

// UnmarshalJSON 读取已知 MASB 字段并保留其余扩展字段。
func (e *ExtendedInfo) UnmarshalJSON(raw []byte) error {
	fields := make(map[string]json.RawMessage)
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	riskType, err := takeAnnotationString(fields, "risk_type", true)
	if err != nil {
		return err
	}
	riskLevel, err := takeAnnotationString(fields, "risk_level", true)
	if err != nil {
		return err
	}
	attackScenario, err := takeAnnotationString(fields, "attack_scenario", true)
	if err != nil {
		return err
	}
	caseType, err := takeAnnotationString(fields, "case_type", true)
	if err != nil {
		return err
	}
	other, err := takeAnnotationString(fields, "other", true)
	if err != nil {
		return err
	}
	var isAttack *bool
	if value, ok := fields["is_attack"]; ok {
		delete(fields, "is_attack")
		if string(value) != "null" {
			var decoded bool
			if err := json.Unmarshal(value, &decoded); err != nil {
				return fmt.Errorf("is_attack must be a boolean")
			}
			isAttack = &decoded
		}
	}
	extra := make(map[string]json.RawMessage, len(fields))
	for key, value := range fields {
		extra[key] = append(json.RawMessage(nil), value...)
	}
	e.RiskType = riskType
	e.RiskLevel = riskLevel
	e.AttackScenario = attackScenario
	e.CaseType = caseType
	e.IsAttack = isAttack
	e.Other = other
	e.Extra = extra
	return nil
}

// MarshalJSON 将已知 MASB 字段和扩展字段组合为单个对象。
func (e ExtendedInfo) MarshalJSON() ([]byte, error) {
	fields := make(map[string]json.RawMessage, len(e.Extra)+6)
	for key, value := range e.Extra {
		fields[key] = append(json.RawMessage(nil), value...)
	}
	for key, value := range map[string]any{
		"risk_type":       e.RiskType,
		"risk_level":      e.RiskLevel,
		"attack_scenario": e.AttackScenario,
		"case_type":       e.CaseType,
		"other":           e.Other,
	} {
		if value != "" {
			encoded, err := json.Marshal(value)
			if err != nil {
				return nil, err
			}
			fields[key] = encoded
		}
	}
	if e.IsAttack != nil {
		encoded, err := json.Marshal(*e.IsAttack)
		if err != nil {
			return nil, err
		}
		fields["is_attack"] = encoded
	}
	return json.Marshal(fields)
}

func takeAnnotationString(fields map[string]json.RawMessage, name string, optional bool) (string, error) {
	value, ok := fields[name]
	delete(fields, name)
	if !ok && optional {
		return "", nil
	}
	if !ok {
		return "", fmt.Errorf("annotation %s is required", name)
	}
	var decoded string
	if err := json.Unmarshal(value, &decoded); err != nil {
		return "", fmt.Errorf("annotation %s must be a string", name)
	}
	return decoded, nil
}

func firstKey(values map[string]json.RawMessage) string {
	for key := range values {
		return key
	}
	return ""
}
