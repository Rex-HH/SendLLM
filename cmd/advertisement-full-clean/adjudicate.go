package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// adjudicateConfig 指定两层终态裁决所需的源和 decisions。
type adjudicateConfig struct {
	SourcePath          string
	Layer1DecisionsPath string
	Layer2DecisionsPath string
	OutputDir           string
}

// adjudicateReport 汇总互斥 manifest 的聚合计数。
type adjudicateReport struct {
	SourceCount     int            `json:"source_count"`
	ApprovedCount   int            `json:"approved_count"`
	OverlapCount    int            `json:"overlap_count"`
	QuarantineCount int            `json:"quarantine_count"`
	UnchangedCount  int            `json:"unchanged_count"`
	QuarantineCodes map[string]int `json:"quarantine_codes"`
}

// approvedChange 只记录批准标签变更所需的最小字段。
type approvedChange struct {
	TraceID    string   `json:"trace_id"`
	OldLabel   string   `json:"old_label"`
	NewLabel   string   `json:"new_label"`
	IssueCodes []string `json:"issue_codes"`
}

// legacyOverlapRecord 只记录需要保留的旧风险重叠候选。
type legacyOverlapRecord struct {
	TraceID    string   `json:"trace_id"`
	Risk       string   `json:"risk"`
	IssueCodes []string `json:"issue_codes"`
}

// quarantineRecord 只记录保持原样的隔离原因。
type quarantineRecord struct {
	TraceID    string   `json:"trace_id"`
	IssueCodes []string `json:"issue_codes"`
}

// adjudicate 生成互斥的 approved、legacy-overlap 和 quarantine manifest。
func adjudicate(cfg adjudicateConfig) (adjudicateReport, error) {
	if cfg.SourcePath == "" || cfg.Layer1DecisionsPath == "" || cfg.OutputDir == "" {
		return adjudicateReport{}, fmt.Errorf("adjudicate source, layer1 decisions and output directory are required")
	}
	sources, err := loadSourceRecords(cfg.SourcePath)
	if err != nil {
		return adjudicateReport{}, err
	}
	layer1, err := loadLayerDecisions(cfg.Layer1DecisionsPath)
	if err != nil {
		return adjudicateReport{}, err
	}
	layer2 := map[string]layerDecision{}
	if cfg.Layer2DecisionsPath != "" {
		layer2, err = loadLayerDecisions(cfg.Layer2DecisionsPath)
		if err != nil {
			return adjudicateReport{}, err
		}
	}
	if len(layer1) != len(sources) {
		return adjudicateReport{}, fmt.Errorf("layer1 decisions %d, source rows %d", len(layer1), len(sources))
	}
	report := adjudicateReport{
		SourceCount:     len(sources),
		QuarantineCodes: map[string]int{},
	}
	approved := make([]approvedChange, 0)
	overlaps := make([]legacyOverlapRecord, 0)
	quarantined := make([]quarantineRecord, 0)
	for _, source := range sources {
		first, ok := layer1[source.TraceID]
		if !ok {
			return adjudicateReport{}, fmt.Errorf("layer1 is missing trace_id %q", source.TraceID)
		}
		if first.State == "failed" {
			record := quarantineRecord{TraceID: source.TraceID, IssueCodes: []string{"provider_failure"}}
			quarantined = append(quarantined, record)
			report.QuarantineCount++
			report.QuarantineCodes["provider_failure"]++
			continue
		}
		if first.Label == nil {
			return adjudicateReport{}, fmt.Errorf("layer1 trace_id %q has no label", source.TraceID)
		}
		if first.Risk != "" {
			overlaps = append(overlaps, legacyOverlapRecord{
				TraceID:    source.TraceID,
				Risk:       first.Risk,
				IssueCodes: []string{"legacy_overlap"},
			})
			report.OverlapCount++
			continue
		}
		if *first.Label == 0 {
			record := quarantineRecord{TraceID: source.TraceID, IssueCodes: []string{"uncertain"}}
			quarantined = append(quarantined, record)
			report.QuarantineCount++
			report.QuarantineCodes["uncertain"]++
			continue
		}
		if (*first.Label == 1 && source.Label == "safe") ||
			(*first.Label == 2 && source.Label == "unsafe") {
			report.UnchangedCount++
			continue
		}
		second, ok := layer2[source.TraceID]
		if !ok {
			record := quarantineRecord{TraceID: source.TraceID, IssueCodes: []string{"missing_result"}}
			quarantined = append(quarantined, record)
			report.QuarantineCount++
			report.QuarantineCodes["missing_result"]++
			continue
		}
		if second.State == "failed" {
			record := quarantineRecord{TraceID: source.TraceID, IssueCodes: []string{"provider_failure"}}
			quarantined = append(quarantined, record)
			report.QuarantineCount++
			report.QuarantineCodes["provider_failure"]++
			continue
		}
		if second.Label == nil {
			return adjudicateReport{}, fmt.Errorf("layer2 trace_id %q has no label", source.TraceID)
		}
		if second.Risk != "" {
			overlaps = append(overlaps, legacyOverlapRecord{
				TraceID:    source.TraceID,
				Risk:       second.Risk,
				IssueCodes: []string{"legacy_overlap"},
			})
			report.OverlapCount++
			continue
		}
		if *second.Label == 0 || *first.Label != *second.Label {
			code := "disagreement"
			if *second.Label == 0 {
				code = "uncertain"
			}
			record := quarantineRecord{TraceID: source.TraceID, IssueCodes: []string{code}}
			quarantined = append(quarantined, record)
			report.QuarantineCount++
			report.QuarantineCodes[code]++
			continue
		}
		newLabel := labelFromDecision(*first.Label)
		if newLabel == source.Label {
			report.UnchangedCount++
			continue
		}
		approved = append(approved, approvedChange{
			TraceID:    source.TraceID,
			OldLabel:   source.Label,
			NewLabel:   newLabel,
			IssueCodes: []string{"label_change"},
		})
		report.ApprovedCount++
	}
	for traceID := range layer2 {
		first, ok := layer1[traceID]
		if !ok || first.State != "succeeded" || first.Label == nil ||
			(*first.Label != 0 && *first.Label != 1 && *first.Label != 2) {
			return adjudicateReport{}, fmt.Errorf("layer2 trace_id %q was not routed", traceID)
		}
	}
	absoluteOutput, err := ensureNewOutputDirectory(cfg.OutputDir)
	if err != nil {
		return adjudicateReport{}, err
	}
	parent := filepath.Dir(absoluteOutput)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return adjudicateReport{}, fmt.Errorf("create adjudication parent: %w", err)
	}
	stage, err := os.MkdirTemp(parent, "."+filepath.Base(absoluteOutput)+".stage-*")
	if err != nil {
		return adjudicateReport{}, fmt.Errorf("create adjudication stage: %w", err)
	}
	defer func() { _ = os.RemoveAll(stage) }()
	if err := writeJSONLines(filepath.Join(stage, "approved-changes.jsonl"), approved); err != nil {
		return adjudicateReport{}, err
	}
	if err := writeJSONLines(filepath.Join(stage, "legacy-overlap.jsonl"), overlaps); err != nil {
		return adjudicateReport{}, err
	}
	if err := writeJSONLines(filepath.Join(stage, "quarantine.jsonl"), quarantined); err != nil {
		return adjudicateReport{}, err
	}
	if err := writeJSONFile(filepath.Join(stage, "adjudication-report.json"), report); err != nil {
		return adjudicateReport{}, err
	}
	if err := os.Rename(stage, absoluteOutput); err != nil {
		return adjudicateReport{}, fmt.Errorf("publish adjudication output: %w", err)
	}
	return report, nil
}

// labelFromDecision 转换确定性的模型标签枚举。
func labelFromDecision(label int) string {
	if label == 1 {
		return "safe"
	}
	return "unsafe"
}
