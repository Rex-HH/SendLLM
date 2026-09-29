package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"sendllm/internal/dto"
	"sendllm/internal/lib/tokenizer"
)

var layer2ReasonOrder = []string{"label_change", "legacy_overlap", "uncertain", "provider_failure"}

// routeConfig 指定第一层终态到第二层盲化输入的确定性路由边界。
type routeConfig struct {
	SourcePath          string
	Layer1DecisionsPath string
	OutputDir           string
	BatchSize           int
	BatchMaxInputTokens int
}

// routeReport 汇总第一层路由聚合结果。
type routeReport struct {
	SourceCount        int            `json:"source_count"`
	RoutedCount        int            `json:"routed_count"`
	UnchangedCount     int            `json:"unchanged_count"`
	BatchCount         int            `json:"batch_count"`
	MinBatchTokens     int            `json:"min_batch_tokens"`
	MaxBatchTokens     int            `json:"max_batch_tokens"`
	AverageBatchTokens int            `json:"average_batch_tokens"`
	ReasonCounts       map[string]int `json:"reason_counts"`
	UniqueTraceIDs     int            `json:"unique_trace_ids"`
}

// routedItem 表示一条需要 Pro 复核的本地路由项。
type routedItem struct {
	Source  sourceRecord
	Reasons []string
}

// layer2BatchItem 是第二层批次中唯一允许的模型语义字段。
type layer2BatchItem struct {
	Index  int    `json:"i"`
	Prompt string `json:"p"`
}

// layer2BatchPayload 是第二层模型输入批次。
type layer2BatchPayload struct {
	Items []layer2BatchItem `json:"items"`
}

// layer1RoutingRecord 记录本地路由原因，不包含 prompt。
type layer1RoutingRecord struct {
	TraceID string   `json:"trace_id"`
	Reasons []string `json:"reasons"`
}

// routeLayer2 读取第一层终态并生成第二层盲化批次和本地映射。
func routeLayer2(cfg routeConfig) (routeReport, error) {
	if cfg.SourcePath == "" || cfg.Layer1DecisionsPath == "" || cfg.OutputDir == "" {
		return routeReport{}, fmt.Errorf("route source, layer1 decisions and output directory are required")
	}
	if cfg.BatchSize < 1 || cfg.BatchMaxInputTokens < 1 {
		return routeReport{}, fmt.Errorf("route batch bounds are invalid")
	}
	sources, err := loadSourceRecords(cfg.SourcePath)
	if err != nil {
		return routeReport{}, err
	}
	layer1, err := loadLayerDecisions(cfg.Layer1DecisionsPath)
	if err != nil {
		return routeReport{}, err
	}
	if len(layer1) != len(sources) {
		return routeReport{}, fmt.Errorf("layer1 decisions %d, source rows %d", len(layer1), len(sources))
	}
	report := routeReport{
		SourceCount:  len(sources),
		ReasonCounts: make(map[string]int, len(layer2ReasonOrder)),
	}
	routed := make([]routedItem, 0)
	routingRecords := make([]layer1RoutingRecord, 0)
	for _, source := range sources {
		decision, ok := layer1[source.TraceID]
		if !ok {
			return routeReport{}, fmt.Errorf("layer1 is missing trace_id %q", source.TraceID)
		}
		reasons, err := routeReasons(source, decision)
		if err != nil {
			return routeReport{}, fmt.Errorf("route trace_id %q: %w", source.TraceID, err)
		}
		if len(reasons) == 0 {
			report.UnchangedCount++
			continue
		}
		report.RoutedCount++
		for _, reason := range reasons {
			report.ReasonCounts[reason]++
		}
		routingRecords = append(routingRecords, layer1RoutingRecord{TraceID: source.TraceID, Reasons: reasons})
		routed = append(routed, routedItem{Source: source, Reasons: reasons})
	}
	report.UniqueTraceIDs = report.RoutedCount
	plans, err := packLayer2Batches(routed, cfg.BatchSize, cfg.BatchMaxInputTokens)
	if err != nil {
		return routeReport{}, err
	}
	report.BatchCount = len(plans)
	report.MinBatchTokens, report.MaxBatchTokens, report.AverageBatchTokens = layer2TokenStats(plans)
	absoluteOutput, err := ensureNewOutputDirectory(cfg.OutputDir)
	if err != nil {
		return routeReport{}, err
	}
	parent := filepath.Dir(absoluteOutput)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return routeReport{}, fmt.Errorf("create route parent: %w", err)
	}
	stage, err := os.MkdirTemp(parent, "."+filepath.Base(absoluteOutput)+".stage-*")
	if err != nil {
		return routeReport{}, fmt.Errorf("create route stage: %w", err)
	}
	defer func() { _ = os.RemoveAll(stage) }()
	layer2Rows := make([]pilotInputRecord, 0, len(routed))
	for _, item := range routed {
		layer2Rows = append(layer2Rows, pilotInputRecord{TraceID: item.Source.TraceID, Prompt: item.Source.Prompt})
	}
	if err := writeJSONLines(filepath.Join(stage, "layer2.input.jsonl"), layer2Rows); err != nil {
		return routeReport{}, err
	}
	if err := writeJSONLines(filepath.Join(stage, "layer1-routing.jsonl"), routingRecords); err != nil {
		return routeReport{}, err
	}
	if err := writeLayer2Batches(filepath.Join(stage, "layer2.batches"), plans); err != nil {
		return routeReport{}, err
	}
	if err := writeJSONFile(filepath.Join(stage, "route-report.json"), report); err != nil {
		return routeReport{}, err
	}
	if err := os.Rename(stage, absoluteOutput); err != nil {
		return routeReport{}, fmt.Errorf("publish route output: %w", err)
	}
	return report, nil
}

// routeReasons 使用稳定顺序返回一条记录的全部路由原因。
func routeReasons(source sourceRecord, decision layerDecision) ([]string, error) {
	reasons := make([]string, 0, len(layer2ReasonOrder))
	if decision.State == "failed" {
		reasons = append(reasons, "provider_failure")
		return reasons, nil
	}
	if decision.Label == nil {
		return nil, fmt.Errorf("succeeded layer1 decision has no label")
	}
	switch *decision.Label {
	case 0:
		reasons = append(reasons, "uncertain")
	case 1:
		if source.Label != "safe" {
			reasons = append(reasons, "label_change")
		}
	case 2:
		if source.Label != "unsafe" {
			reasons = append(reasons, "label_change")
		}
	default:
		return nil, fmt.Errorf("layer1 label is invalid")
	}
	if decision.Risk != "" {
		reasons = append(reasons, "legacy_overlap")
	}
	return orderRouteReasons(reasons), nil
}

// orderRouteReasons 按冻结顺序排列路由原因。
func orderRouteReasons(reasons []string) []string {
	present := make(map[string]bool, len(reasons))
	for _, reason := range reasons {
		present[reason] = true
	}
	ordered := make([]string, 0, len(reasons))
	for _, reason := range layer2ReasonOrder {
		if present[reason] {
			ordered = append(ordered, reason)
		}
	}
	return ordered
}

// layer2Plan 表示一个已打包的第二层批次。
type layer2Plan struct {
	Name   string
	Items  []routedItem
	Tokens int
}

// packLayer2Batches 按连续顺序贪心打包第二层批次。
func packLayer2Batches(items []routedItem, batchSize int, maxTokens int) ([]layer2Plan, error) {
	plans := make([]layer2Plan, 0)
	current := make([]routedItem, 0, batchSize)
	currentTokens := 0
	for _, item := range items {
		candidate := make([]routedItem, 0, len(current)+1)
		candidate = append(candidate, current...)
		candidate = append(candidate, item)
		tokens, _, err := estimateLayer2Batch(candidate)
		if err != nil {
			return nil, err
		}
		if tokens <= maxTokens {
			current = candidate
			currentTokens = tokens
		} else {
			if len(current) == 0 {
				return nil, fmt.Errorf("single layer2 item exceeds token limit")
			}
			plans = append(plans, layer2Plan{
				Name:   fmt.Sprintf("batch-%03d.json", len(plans)+1),
				Items:  current,
				Tokens: currentTokens,
			})
			current = []routedItem{item}
			singleTokens, _, err := estimateLayer2Batch(current)
			if err != nil {
				return nil, err
			}
			if singleTokens > maxTokens {
				return nil, fmt.Errorf("single layer2 item exceeds token limit")
			}
			currentTokens = singleTokens
		}
		if len(current) == batchSize {
			plans = append(plans, layer2Plan{
				Name:   fmt.Sprintf("batch-%03d.json", len(plans)+1),
				Items:  current,
				Tokens: currentTokens,
			})
			current = make([]routedItem, 0, batchSize)
			currentTokens = 0
		}
	}
	if len(current) != 0 {
		plans = append(plans, layer2Plan{
			Name:   fmt.Sprintf("batch-%03d.json", len(plans)+1),
			Items:  current,
			Tokens: currentTokens,
		})
	}
	return plans, nil
}

// estimateLayer2Batch 使用保守估算器计算盲化批次输入 Token。
func estimateLayer2Batch(items []routedItem) (int, []byte, error) {
	payload := layer2BatchPayload{Items: make([]layer2BatchItem, 0, len(items))}
	for index, item := range items {
		payload.Items = append(payload.Items, layer2BatchItem{Index: index, Prompt: item.Source.Prompt})
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, fmt.Errorf("encode layer2 batch: %w", err)
	}
	tokens := tokenizer.Estimate([]dto.Message{{Role: "user", Content: string(encoded)}}, 0)
	return tokens, encoded, nil
}

// writeLayer2Batches 写入盲化批次文件和本地映射。
func writeLayer2Batches(directory string, plans []layer2Plan) error {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create layer2 batch directory: %w", err)
	}
	mappings := make([]batchMap, 0, len(plans))
	for _, plan := range plans {
		payload := layer2BatchPayload{Items: make([]layer2BatchItem, 0, len(plan.Items))}
		mapping := batchMap{Batch: plan.Name, Items: make([]batchMapItem, 0, len(plan.Items))}
		for index, item := range plan.Items {
			payload.Items = append(payload.Items, layer2BatchItem{Index: index, Prompt: item.Source.Prompt})
			mapping.Items = append(mapping.Items, batchMapItem{Index: index, TraceID: item.Source.TraceID})
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode layer2 batch %s: %w", plan.Name, err)
		}
		if err := atomicWriteFile(filepath.Join(directory, plan.Name), encoded); err != nil {
			return err
		}
		mappings = append(mappings, mapping)
	}
	return writeJSONLines(filepath.Join(filepath.Dir(directory), "layer2.batch-map.jsonl"), mappings)
}

// layer2TokenStats 计算第二层批次 Token 统计。
func layer2TokenStats(plans []layer2Plan) (int, int, int) {
	if len(plans) == 0 {
		return 0, 0, 0
	}
	minimum := plans[0].Tokens
	maximum := plans[0].Tokens
	total := 0
	for _, plan := range plans {
		if plan.Tokens < minimum {
			minimum = plan.Tokens
		}
		if plan.Tokens > maximum {
			maximum = plan.Tokens
		}
		total += plan.Tokens
	}
	return minimum, maximum, total / len(plans)
}
