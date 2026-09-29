package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// applyConfig 指定批准变更应用到新广告版本的边界。
type applyConfig struct {
	SourcePath          string
	ApprovedChangesPath string
	OutputPath          string
	Protected           []string
}

// applyReport 汇总批准变更应用结果。
type applyReport struct {
	InputRows int `json:"input_rows"`
	Modified  int `json:"modified"`
	Unchanged int `json:"unchanged"`
}

// applyApprovedChanges 只把 approved manifest 中的标签变更写入新 JSON 数组。
func applyApprovedChanges(cfg applyConfig) (applyReport, error) {
	if cfg.SourcePath == "" || cfg.ApprovedChangesPath == "" || cfg.OutputPath == "" {
		return applyReport{}, fmt.Errorf("apply source, approved changes and output path are required")
	}
	sourceRows, err := loadSourceRecords(cfg.SourcePath)
	if err != nil {
		return applyReport{}, err
	}
	approved, err := loadApprovedChanges(cfg.ApprovedChangesPath)
	if err != nil {
		return applyReport{}, err
	}
	protected := append([]string{cfg.SourcePath}, cfg.Protected...)
	if err := ensureOutputPathAvailable(cfg.OutputPath, protected); err != nil {
		return applyReport{}, err
	}
	outputRows := make([]json.RawMessage, len(sourceRows))
	report := applyReport{InputRows: len(sourceRows)}
	for index, source := range sourceRows {
		change, ok := approved[source.TraceID]
		if !ok {
			outputRows[index] = append(json.RawMessage(nil), source.Raw...)
			report.Unchanged++
			continue
		}
		if change.OldLabel != source.Label {
			return applyReport{}, fmt.Errorf("approved trace_id %q old label does not match source", source.TraceID)
		}
		transformed, err := applyApprovedLabel(source.Raw, change.NewLabel)
		if err != nil {
			return applyReport{}, fmt.Errorf("apply trace_id %q: %w", source.TraceID, err)
		}
		outputRows[index] = transformed
		report.Modified++
	}
	if err := ensureOutputPathAvailable(cfg.OutputPath, protected); err != nil {
		return applyReport{}, err
	}
	encoded, err := json.Marshal(outputRows)
	if err != nil {
		return applyReport{}, fmt.Errorf("encode cleaned advertisement array: %w", err)
	}
	if err := atomicWriteFile(cfg.OutputPath, append(encoded, '\n')); err != nil {
		return applyReport{}, err
	}
	return report, nil
}

// loadApprovedChanges 严格读取批准变更 manifest 并拒绝重复 ID。
func loadApprovedChanges(path string) (map[string]approvedChange, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read approved changes %q: %w", path, err)
	}
	rows, err := readRawJSONLines(bytes.NewReader(contents))
	if err != nil {
		return nil, fmt.Errorf("read approved changes JSONL %q: %w", path, err)
	}
	changes := make(map[string]approvedChange, len(rows))
	for index, raw := range rows {
		var change approvedChange
		if err := decodeStrictJSON(raw, &change); err != nil {
			return nil, fmt.Errorf("decode approved change %d: %w", index+1, err)
		}
		if change.TraceID == "" || (change.OldLabel != "safe" && change.OldLabel != "unsafe") ||
			(change.NewLabel != "safe" && change.NewLabel != "unsafe") || change.OldLabel == change.NewLabel {
			return nil, fmt.Errorf("approved change %d is invalid", index+1)
		}
		if _, exists := changes[change.TraceID]; exists {
			return nil, fmt.Errorf("approved trace_id %q is duplicated", change.TraceID)
		}
		changes[change.TraceID] = change
	}
	return changes, nil
}

// applyApprovedLabel 按冻结应用规则只修改已批准的广告记录。
func applyApprovedLabel(raw []byte, newLabel string) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("decode source object: %w", err)
	}
	fields["label"] = json.RawMessage(`"` + newLabel + `"`)
	extended := make(map[string]json.RawMessage)
	if rawExtended, ok := fields["extended_info"]; ok && len(rawExtended) != 0 {
		if err := json.Unmarshal(rawExtended, &extended); err != nil {
			return nil, fmt.Errorf("decode extended_info: %w", err)
		}
	}
	if newLabel == "safe" {
		extended["risk_type"] = json.RawMessage(`""`)
		extended["risk_level"] = json.RawMessage(`""`)
		extended["attack_domain"] = json.RawMessage(`""`)
		extended["attack_method"] = json.RawMessage(`""`)
		extended["is_attack"] = json.RawMessage(`false`)
	} else {
		extended["attack_domain"] = json.RawMessage(`"advertisement"`)
		extended["attack_method"] = json.RawMessage(`""`)
		extended["is_attack"] = json.RawMessage(`true`)
	}
	encodedExtended, err := json.Marshal(extended)
	if err != nil {
		return nil, fmt.Errorf("encode extended_info: %w", err)
	}
	fields["extended_info"] = encodedExtended
	encoded, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("encode source object: %w", err)
	}
	return encoded, nil
}

// ensureOutputPathAvailable 检查输出不存在且不覆盖任何保护路径。
func ensureOutputPathAvailable(outputPath string, protected []string) error {
	absoluteOutput, err := filepath.Abs(outputPath)
	if err != nil {
		return fmt.Errorf("resolve output path: %w", err)
	}
	if _, err := os.Stat(outputPath); err == nil {
		return fmt.Errorf("output %q already exists", outputPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat output %q: %w", outputPath, err)
	}
	for _, protectedPath := range protected {
		absoluteProtected, err := filepath.Abs(protectedPath)
		if err != nil {
			return fmt.Errorf("resolve protected path %q: %w", protectedPath, err)
		}
		if absoluteOutput == absoluteProtected {
			return fmt.Errorf("output %q is protected", outputPath)
		}
	}
	return nil
}
