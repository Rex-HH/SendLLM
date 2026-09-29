package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// riskPilotConfig 指定风险优先 pilot 的本地选择边界。
type riskPilotConfig struct {
	SourcePath      string
	CalibrationPath string
	OutputDir       string
	HighRiskLimit   int
	ControlLimit    int
}

// riskPilotReport 汇总风险优先 pilot 选择结果。
type riskPilotReport struct {
	SourceCount     int            `json:"source_count"`
	ReviewedCount   int            `json:"reviewed_count"`
	HighRiskCount   int            `json:"high_risk_count"`
	ControlCount    int            `json:"control_count"`
	TotalCount      int            `json:"total_count"`
	CohortCounts    map[string]int `json:"cohort_counts"`
	LabelCounts     map[string]int `json:"label_counts"`
	SourceSHA256    string         `json:"source_sha256"`
	SelectionSHA256 string         `json:"selection_sha256"`
	PilotSHA256     string         `json:"pilot_sha256"`
}

// riskSelectionRecord 记录本地 cohort 选择依据，不包含 prompt。
type riskSelectionRecord struct {
	TraceID        string  `json:"trace_id"`
	Cohort         string  `json:"cohort"`
	Source         string  `json:"source"`
	OriginalLabel  string  `json:"original_label"`
	AttackScenario string  `json:"attack_scenario"`
	QualityScore   float64 `json:"quality_score"`
	PromptLength   int     `json:"prompt_length"`
	RiskScore      int     `json:"risk_score"`
}

// riskCandidate 保存选择过程使用的源记录和风险分。
type riskCandidate struct {
	Source sourceRecord
	Score  int
}

// selectRiskPriorityPilot 确定性选择高风险 cohort 和分层对照 cohort。
func selectRiskPriorityPilot(cfg riskPilotConfig) (riskPilotReport, error) {
	if cfg.SourcePath == "" || cfg.CalibrationPath == "" || cfg.OutputDir == "" {
		return riskPilotReport{}, fmt.Errorf("risk pilot source, calibration and output directory are required")
	}
	if cfg.HighRiskLimit < 1 || cfg.ControlLimit < 0 {
		return riskPilotReport{}, fmt.Errorf("risk pilot cohort limits are invalid")
	}
	source, err := loadSourceRecords(cfg.SourcePath)
	if err != nil {
		return riskPilotReport{}, err
	}
	calibration, err := loadCalibrationRecords(cfg.CalibrationPath)
	if err != nil {
		return riskPilotReport{}, err
	}
	reviewed := make(map[string]struct{}, len(calibration))
	for _, record := range calibration {
		reviewed[record.TraceID] = struct{}{}
	}
	remaining := make([]sourceRecord, 0, len(source)-len(reviewed))
	for _, record := range source {
		if _, exists := reviewed[record.TraceID]; exists {
			continue
		}
		remaining = append(remaining, record)
	}
	if len(remaining) == 0 {
		return riskPilotReport{}, fmt.Errorf("risk pilot has no unreviewed rows")
	}
	highRiskPool := make([]riskCandidate, 0)
	for _, record := range remaining {
		score, eligible := riskPriorityScore(record)
		if !eligible {
			continue
		}
		highRiskPool = append(highRiskPool, riskCandidate{Source: record, Score: score})
	}
	sort.Slice(highRiskPool, func(left, right int) bool {
		if highRiskPool[left].Score != highRiskPool[right].Score {
			return highRiskPool[left].Score > highRiskPool[right].Score
		}
		return stableTraceIDHash(highRiskPool[left].Source.TraceID) <
			stableTraceIDHash(highRiskPool[right].Source.TraceID)
	})
	highRiskLimit := min(cfg.HighRiskLimit, len(highRiskPool))
	selectedHighRisk := highRiskPool[:highRiskLimit]
	selectedIDs := make(map[string]struct{}, highRiskLimit+cfg.ControlLimit)
	for _, candidate := range selectedHighRisk {
		selectedIDs[candidate.Source.TraceID] = struct{}{}
	}
	controlPool := make([]sourceRecord, 0, len(remaining)-len(selectedHighRisk))
	for _, record := range remaining {
		if _, selected := selectedIDs[record.TraceID]; selected {
			continue
		}
		controlPool = append(controlPool, record)
	}
	selectedControl := selectRiskControl(controlPool, cfg.ControlLimit)
	selection := make([]riskSelectionRecord, 0, len(selectedHighRisk)+len(selectedControl))
	pilot := make([]pilotInputRecord, 0, len(selectedHighRisk)+len(selectedControl))
	for _, candidate := range selectedHighRisk {
		selection = append(selection, riskSelection(candidate.Source, "high_risk", candidate.Score))
		pilot = append(pilot, pilotInputRecord{TraceID: candidate.Source.TraceID, Prompt: candidate.Source.Prompt})
	}
	for _, record := range selectedControl {
		selection = append(selection, riskSelection(record, "control", 0))
		pilot = append(pilot, pilotInputRecord{TraceID: record.TraceID, Prompt: record.Prompt})
	}
	report := summarizeRiskPilot(source, calibration, selection)
	sourceHash, err := fileSHA256(cfg.SourcePath)
	if err != nil {
		return riskPilotReport{}, err
	}
	report.SourceSHA256 = sourceHash
	absoluteOutput, err := ensureNewOutputDirectory(cfg.OutputDir)
	if err != nil {
		return riskPilotReport{}, err
	}
	parent := filepath.Dir(absoluteOutput)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return riskPilotReport{}, fmt.Errorf("create risk pilot parent: %w", err)
	}
	stage, err := os.MkdirTemp(parent, "."+filepath.Base(absoluteOutput)+".stage-*")
	if err != nil {
		return riskPilotReport{}, fmt.Errorf("create risk pilot stage: %w", err)
	}
	defer func() { _ = os.RemoveAll(stage) }()
	selectionPath := filepath.Join(stage, "selection.manifest.jsonl")
	if err := writeJSONLines(selectionPath, selection); err != nil {
		return riskPilotReport{}, err
	}
	pilotPath := filepath.Join(stage, "pilot.input.jsonl")
	if err := writeJSONLines(pilotPath, pilot); err != nil {
		return riskPilotReport{}, err
	}
	report.SelectionSHA256, err = fileSHA256(selectionPath)
	if err != nil {
		return riskPilotReport{}, err
	}
	report.PilotSHA256, err = fileSHA256(pilotPath)
	if err != nil {
		return riskPilotReport{}, err
	}
	if err := writeJSONFile(filepath.Join(stage, "selection-report.json"), report); err != nil {
		return riskPilotReport{}, err
	}
	if err := os.Rename(stage, absoluteOutput); err != nil {
		return riskPilotReport{}, fmt.Errorf("publish risk pilot selection: %w", err)
	}
	return report, nil
}

// riskPriorityScore 返回高风险 cohort 的确定性分值和资格。
func riskPriorityScore(record sourceRecord) (int, bool) {
	if record.Label != "unsafe" {
		return 0, false
	}
	score := 0
	quality := sourceQualityScore(record)
	switch {
	case quality < 0.8:
		score += 4
	case quality < 0.9:
		score += 3
	}
	if len(record.Prompt) > 400 {
		score += 3
	}
	if isRareRiskScenario(record.AttackScenario) {
		score += 3
	}
	if record.Source == "ChineseSafe" {
		score++
	}
	eligible := quality < 0.9 || len(record.Prompt) > 400 ||
		isRareRiskScenario(record.AttackScenario) || record.Source == "ChineseSafe"
	return score, eligible
}

// selectRiskControl 按来源、标签、场景、长度和质量分层选择对照样本。
func selectRiskControl(rows []sourceRecord, limit int) []sourceRecord {
	if limit < 1 {
		return nil
	}
	groups := make(map[string][]sourceRecord)
	for _, row := range rows {
		key := fmt.Sprintf("%s|%s|%s|%d|%d",
			row.Source,
			row.Label,
			row.AttackScenario,
			riskLengthBand(len(row.Prompt)),
			riskQualityBand(sourceQualityScore(row)),
		)
		groups[key] = append(groups[key], row)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		sort.Slice(groups[key], func(left, right int) bool {
			return stableTraceIDHash(groups[key][left].TraceID) <
				stableTraceIDHash(groups[key][right].TraceID)
		})
	}
	selected := make([]sourceRecord, 0, limit)
	for len(selected) < limit {
		progressed := false
		for _, key := range keys {
			if len(selected) >= limit {
				break
			}
			if len(groups[key]) == 0 {
				continue
			}
			selected = append(selected, groups[key][0])
			groups[key] = groups[key][1:]
			progressed = true
		}
		if !progressed {
			break
		}
	}
	return selected
}

// riskSelection 把源记录转换为无 prompt 的选择记录。
func riskSelection(record sourceRecord, cohort string, score int) riskSelectionRecord {
	return riskSelectionRecord{
		TraceID:        record.TraceID,
		Cohort:         cohort,
		Source:         record.Source,
		OriginalLabel:  record.Label,
		AttackScenario: record.AttackScenario,
		QualityScore:   sourceQualityScore(record),
		PromptLength:   len(record.Prompt),
		RiskScore:      score,
	}
}

// summarizeRiskPilot 计算选择 cohort 的聚合计数。
func summarizeRiskPilot(
	source []sourceRecord,
	calibration []calibrationRecord,
	selection []riskSelectionRecord,
) riskPilotReport {
	report := riskPilotReport{
		SourceCount:   len(source),
		ReviewedCount: len(calibration),
		CohortCounts:  map[string]int{},
		LabelCounts:   map[string]int{},
	}
	for _, record := range selection {
		report.CohortCounts[record.Cohort]++
		report.LabelCounts[record.OriginalLabel]++
	}
	report.HighRiskCount = report.CohortCounts["high_risk"]
	report.ControlCount = report.CohortCounts["control"]
	report.TotalCount = len(selection)
	return report
}

// sourceQualityScore 返回源记录质量分，缺失时按零分处理。
func sourceQualityScore(record sourceRecord) float64 {
	if record.QualityScore == nil {
		return 0
	}
	return *record.QualityScore
}

// isRareRiskScenario 判断是否属于低频广告小类。
func isRareRiskScenario(scenario string) bool {
	switch scenario {
	case "account_trading", "blackmarket_tool_advertisement", "boosting_service",
		"discount_recharge_advertisement", "group_contact", "item_trading",
		"other_contact", "part_time_recruitment", "phone_contact", "qq_contact",
		"refund_scam", "other_spam", "wechat_contact":
		return true
	default:
		return false
	}
}

// riskLengthBand 返回对照分层使用的文本长度档位。
func riskLengthBand(length int) int {
	switch {
	case length <= 40:
		return 0
	case length <= 120:
		return 1
	case length <= 400:
		return 2
	default:
		return 3
	}
}

// riskQualityBand 返回对照分层使用的质量分档位。
func riskQualityBand(score float64) int {
	switch {
	case score < 0.8:
		return 0
	case score < 0.9:
		return 1
	default:
		return 2
	}
}
