package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPrepareBuildsQueuesBatchesAndBlindedPayload(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "advertisement_dataset_final.json")
	worksheetPath := filepath.Join(directory, "human-review-worksheet.csv")
	outputDirectory := filepath.Join(directory, "prepared")

	sourceRows := []map[string]any{
		advertisementRow("reviewed", "THUCNews", "unsafe", "已审核内容", "other_spam", 0.9),
		advertisementRow(
			"reviewed-chinese-safe",
			"ChineseSafe",
			"unsafe",
			"已审核中文安全内容",
			"inducement_advertisement",
			0.9,
		),
		advertisementRow("p0-thuc", "THUCNews", "unsafe", "新闻上下文一", "other_external_site", 0.7),
		advertisementRow("p0-wiki", "Wikipedia", "unsafe", "百科上下文二", "other_spam", 0.8),
		advertisementRow("p1-inducement", "ChineseSafe", "unsafe", "短文本", "inducement_advertisement", 0.7),
		advertisementRow("p1-spam", "ChineseSafe", "unsafe", strings.Repeat("长", 120), "other_spam", 0.85),
		advertisementRow("p1-external", "ChineseSafe", "unsafe", strings.Repeat("更", 500), "other_external_site", 0.95),
		advertisementRow("safe-source", "THUCNews", "safe", "安全样本", "", 0.9),
		advertisementRow("other-source", "FGRC-SCD", "unsafe", "其他来源", "other_spam", 0.9),
	}
	writeJSONArray(t, sourcePath, sourceRows)
	writeWorksheet(t, worksheetPath, []map[string]string{{
		"trace_id":               "reviewed",
		"human_label":            "unsafe",
		"human_legacy_overlap":   "no",
		"original_label":         "unsafe",
		"attack_scenario":        "other_spam",
		"model_label":            "unsafe",
		"model_legacy_risk":      "",
		"model_scenario_suspect": "0",
		"issues":                 "",
	}, {
		"trace_id":               "reviewed-chinese-safe",
		"human_label":            "unsafe",
		"human_legacy_overlap":   "no",
		"original_label":         "unsafe",
		"attack_scenario":        "inducement_advertisement",
		"model_label":            "unsafe",
		"model_legacy_risk":      "",
		"model_scenario_suspect": "0",
		"issues":                 "",
	}})

	report, err := prepare(prepareConfig{
		InputPath:      sourcePath,
		WorksheetPath:  worksheetPath,
		OutputDir:      outputDirectory,
		MaxBatchTokens: 80000,
	})
	if err != nil {
		t.Fatalf("prepare() error = %v", err)
	}
	if report.P0Count != 2 || report.P1Count != 3 || report.UniqueTraceIDs != 5 {
		t.Fatalf("prepare report counts = %+v", report)
	}
	if report.BatchCount == 0 || report.MaxBatchTokens > 80000 {
		t.Fatalf("prepare batch metrics = %+v", report)
	}

	p0 := readJSONL(t, filepath.Join(outputDirectory, "p0.queue.jsonl"))
	p1 := readJSONL(t, filepath.Join(outputDirectory, "p1.queue.jsonl"))
	if len(p0) != 2 || p0[0]["trace_id"] != "p0-thuc" || p0[1]["trace_id"] != "p0-wiki" {
		t.Fatalf("p0 queue = %#v", p0)
	}
	if len(p1) != 3 {
		t.Fatalf("p1 queue = %#v", p1)
	}
	for _, row := range append(p0, p1...) {
		if row["trace_id"] == "reviewed" || row["trace_id"] == "reviewed-chinese-safe" ||
			row["trace_id"] == "safe-source" || row["trace_id"] == "other-source" {
			t.Fatalf("excluded row leaked into queue: %#v", row)
		}
	}

	batches, err := filepath.Glob(filepath.Join(outputDirectory, "review-batches", "batch-*.json"))
	if err != nil {
		t.Fatalf("glob batches: %v", err)
	}
	if len(batches) != report.BatchCount {
		t.Fatalf("batch files = %d, report = %d", len(batches), report.BatchCount)
	}
	seen := make(map[string]bool)
	for _, batchPath := range batches {
		contents := readFile(t, batchPath)
		var batch struct {
			Items []map[string]json.RawMessage `json:"items"`
		}
		if err := json.Unmarshal(contents, &batch); err != nil {
			t.Fatalf("decode batch %s: %v", batchPath, err)
		}
		for _, item := range batch.Items {
			if len(item) != 2 {
				t.Fatalf("batch item keys = %v", item)
			}
			if _, ok := item["i"]; !ok {
				t.Fatalf("batch item missing i: %v", item)
			}
			prompt, ok := item["p"]
			if !ok {
				t.Fatalf("batch item missing p: %v", item)
			}
			if bytes.Contains(prompt, []byte("reviewed")) {
				t.Fatalf("batch item leaked excluded ID into prompt payload")
			}
		}
	}

	mappingRows := readJSONL(t, filepath.Join(outputDirectory, "review-batch-map.jsonl"))
	for _, row := range mappingRows {
		batchFile, ok := row["batch"].(string)
		if !ok || !strings.HasPrefix(batchFile, "batch-") {
			t.Fatalf("mapping batch = %#v", row)
		}
		items, ok := row["items"].([]any)
		if !ok {
			t.Fatalf("mapping items = %#v", row)
		}
		for _, rawItem := range items {
			item, ok := rawItem.(map[string]any)
			if !ok {
				t.Fatalf("mapping item = %#v", rawItem)
			}
			traceID, ok := item["trace_id"].(string)
			if !ok || traceID == "" || seen[traceID] {
				t.Fatalf("mapping trace_id = %#v", item)
			}
			seen[traceID] = true
		}
	}
	if len(seen) != report.UniqueTraceIDs {
		t.Fatalf("mapped IDs = %d, want %d", len(seen), report.UniqueTraceIDs)
	}
	for _, traceID := range []string{"p0-thuc", "p0-wiki", "p1-inducement", "p1-spam", "p1-external"} {
		if !seen[traceID] {
			t.Fatalf("mapping missing %s", traceID)
		}
	}

	if contents := readFile(t, filepath.Join(outputDirectory, "decisions.jsonl")); len(contents) != 0 {
		t.Fatalf("decisions.jsonl length = %d, want 0", len(contents))
	}
	if contents := readFile(t, filepath.Join(outputDirectory, "second-pass", "decisions.jsonl")); len(contents) != 0 {
		t.Fatalf("second-pass decisions length = %d, want 0", len(contents))
	}
	schema := readFile(t, filepath.Join(outputDirectory, "review-result.schema.json"))
	for _, want := range []string{
		"news_context",
		"actual_ad",
		"quoted_ad",
		"insufficient",
		`"additionalProperties": false`,
	} {
		if !bytes.Contains(schema, []byte(want)) {
			t.Fatalf("schema missing %q: %s", want, schema)
		}
	}
	reportBytes := readFile(t, filepath.Join(outputDirectory, "prepare-report.json"))
	if bytes.Contains(reportBytes, []byte("新闻上下文一")) {
		t.Fatal("prepare report leaked prompt text")
	}
}

func TestRunPrepareReportsOnlyAggregates(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.json")
	worksheetPath := filepath.Join(directory, "worksheet.csv")
	outputDirectory := filepath.Join(directory, "prepared")
	writeJSONArray(t, sourcePath, []map[string]any{
		advertisementRow("run-p0", "THUCNews", "unsafe", "不得打印的提示词", "other_spam", 0.8),
	})
	writeWorksheet(t, worksheetPath, nil)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{
		"prepare",
		"-input", sourcePath,
		"-worksheet", worksheetPath,
		"-output-dir", outputDirectory,
		"-max-batch-tokens", "80000",
	}, &stdout, &stderr); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "p0=1") || !strings.Contains(stdout.String(), "batches=1") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if strings.Contains(stdout.String(), "不得打印的提示词") ||
		strings.Contains(stderr.String(), "不得打印的提示词") {
		t.Fatal("run() printed prompt text")
	}
}

func TestSelectP1SampleIsDeterministicAndStratified(t *testing.T) {
	candidates := make([]sourceRow, 0, 64)
	for index := range 64 {
		scenario := []string{"inducement_advertisement", "other_spam", "other_external_site"}[index%3]
		length := []int{20, 100, 220, 500}[index%4]
		quality := []float64{0.70, 0.85, 0.95}[index%3]
		candidates = append(candidates, sourceRow{
			TraceID:        "candidate-" + string(rune('A'+index%26)) + string(rune('a'+index/26)),
			Source:         "ChineseSafe",
			Label:          "unsafe",
			Prompt:         strings.Repeat("x", length),
			AttackScenario: scenario,
			QualityScore:   quality,
			HasQuality:     true,
		})
	}
	first := selectP1Sample(candidates, nil, 40)
	second := selectP1Sample(candidates, nil, 40)
	if len(first) != 40 {
		t.Fatalf("sample length = %d", len(first))
	}
	if !reflect.DeepEqual(traceIDs(first), traceIDs(second)) {
		t.Fatalf("sample is not deterministic: %v vs %v", traceIDs(first), traceIDs(second))
	}
	scenarios := make(map[string]bool)
	lengthBands := make(map[int]bool)
	qualityBands := make(map[int]bool)
	for _, row := range first {
		scenarios[row.AttackScenario] = true
		lengthBands[lengthBand(len(row.Prompt))] = true
		qualityBands[qualityBand(row.QualityScore)] = true
	}
	if len(scenarios) < 3 || len(lengthBands) < 4 || len(qualityBands) < 3 {
		t.Fatalf("stratification coverage = scenarios %v length %v quality %v", scenarios, lengthBands, qualityBands)
	}
}

func TestPackBatchesRejectsOversizedSingleItem(t *testing.T) {
	_, err := packBatches([]queueItem{{
		TraceID: "oversized",
		Prompt:  strings.Repeat("x", 80000*4+1),
	}}, 80000)
	if err == nil {
		t.Fatal("packBatches() accepted an oversized single item")
	}
}

func TestRouteSecondPassKeepsOnlySafeAndUncertain(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.json")
	worksheetPath := filepath.Join(directory, "worksheet.csv")
	prepareDirectory := filepath.Join(directory, "prepared")
	writeJSONArray(t, sourcePath, []map[string]any{
		advertisementRow("route-0", "ChineseSafe", "unsafe", "内容零", "inducement_advertisement", 0.8),
		advertisementRow("route-1", "ChineseSafe", "unsafe", "内容一", "other_spam", 0.8),
		advertisementRow("route-2", "ChineseSafe", "unsafe", "内容二", "other_external_site", 0.8),
	})
	writeWorksheet(t, worksheetPath, nil)
	if _, err := prepare(prepareConfig{
		InputPath:      sourcePath,
		WorksheetPath:  worksheetPath,
		OutputDir:      prepareDirectory,
		MaxBatchTokens: 80000,
	}); err != nil {
		t.Fatalf("prepare() error = %v", err)
	}
	mappingRows := readJSONL(t, filepath.Join(prepareDirectory, "review-batch-map.jsonl"))
	if len(mappingRows) != 1 {
		t.Fatalf("mapping rows = %d", len(mappingRows))
	}
	items := mappingRows[0]["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("mapping item count = %d", len(items))
	}
	decisionsPath := filepath.Join(prepareDirectory, "decisions.jsonl")
	writeFile(
		t,
		decisionsPath,
		`{"r":[{"i":0,"l":0,"t":"insufficient"},`+
			`{"i":1,"l":1,"t":"news_context"},`+
			`{"i":2,"l":2,"t":"actual_ad"}]}`+"\n",
	)
	routeDirectory := filepath.Join(directory, "second-pass")
	report, err := routeSecondPass(routeConfig{
		QueuePath:      filepath.Join(prepareDirectory, "queue.jsonl"),
		BatchDir:       filepath.Join(prepareDirectory, "review-batches"),
		MappingPath:    filepath.Join(prepareDirectory, "review-batch-map.jsonl"),
		DecisionsPath:  decisionsPath,
		OutputDir:      routeDirectory,
		MaxBatchTokens: 80000,
	})
	if err != nil {
		t.Fatalf("routeSecondPass() error = %v", err)
	}
	if report.SecondPassItems != 2 || report.BatchCount != 1 {
		t.Fatalf("route report = %+v", report)
	}
	secondBatches, err := filepath.Glob(filepath.Join(routeDirectory, "review-batches", "batch-*.json"))
	if err != nil {
		t.Fatalf("glob second-pass batches: %v", err)
	}
	if len(secondBatches) != 1 {
		t.Fatalf("second-pass batches = %d", len(secondBatches))
	}
	contents := readFile(t, secondBatches[0])
	if bytes.Contains(contents, []byte("trace_id")) || bytes.Contains(contents, []byte("route-2")) {
		t.Fatalf("second-pass batch leaked non-blind data: %s", contents)
	}
	if !bytes.Contains(contents, []byte(`"i":0`)) || !bytes.Contains(contents, []byte(`"p"`)) {
		t.Fatalf("second-pass batch missing compact fields: %s", contents)
	}
	if contents := readFile(t, filepath.Join(routeDirectory, "decisions.jsonl")); len(contents) != 0 {
		t.Fatalf("second-pass decisions length = %d", len(contents))
	}
}

func TestApplyOnlyConfirmedSafeAndPreservesIDsAndUnconfirmedRows(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.json")
	outputPath := filepath.Join(directory, "applied.json")
	rows := []map[string]any{
		advertisementRow("apply-safe", "THUCNews", "unsafe", "待改安全", "other_spam", 0.8),
		advertisementRow("apply-unsafe", "THUCNews", "unsafe", "保持不安全", "other_spam", 0.8),
		advertisementRow("apply-uncertain", "THUCNews", "unsafe", "保持不确定", "other_spam", 0.8),
		advertisementRow("apply-uncovered", "FGRC-SCD", "unsafe", "未覆盖", "other_spam", 0.8),
	}
	writeJSONArray(t, sourcePath, rows)
	batchDirectory := filepath.Join(directory, "review-batches")
	if err := os.MkdirAll(batchDirectory, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	writeFile(
		t,
		filepath.Join(batchDirectory, "batch-001.json"),
		`{"items":[{"i":0,"p":"待改安全"},`+
			`{"i":1,"p":"保持不安全"},`+
			`{"i":2,"p":"保持不确定"}]}`,
	)
	writeFile(t, filepath.Join(directory, "review-batch-map.jsonl"),
		`{"batch":"batch-001.json","items":[`+
			`{"i":0,"trace_id":"apply-safe"},`+
			`{"i":1,"trace_id":"apply-unsafe"},`+
			`{"i":2,"trace_id":"apply-uncertain"}]}`+"\n")
	writeFile(
		t,
		filepath.Join(directory, "decisions.jsonl"),
		`{"r":[{"i":0,"l":1,"t":"news_context"},`+
			`{"i":1,"l":2,"t":"actual_ad"},`+
			`{"i":2,"l":0,"t":"insufficient"}]}`+"\n",
	)
	before := readFile(t, sourcePath)
	report, err := applyDecisions(applyConfig{
		InputPath:     sourcePath,
		OutputPath:    outputPath,
		Format:        "json",
		BatchDir:      batchDirectory,
		MappingPath:   filepath.Join(directory, "review-batch-map.jsonl"),
		DecisionsPath: filepath.Join(directory, "decisions.jsonl"),
		Protected:     []string{sourcePath},
	})
	if err != nil {
		t.Fatalf("applyDecisions() error = %v", err)
	}
	if report.Modified != 1 || report.Unchanged != 3 {
		t.Fatalf("apply report = %+v", report)
	}
	if !bytes.Equal(before, readFile(t, sourcePath)) {
		t.Fatal("apply mutated source file")
	}
	output := readJSONArray(t, outputPath)
	if len(output) != len(rows) {
		t.Fatalf("output rows = %d", len(output))
	}
	for index := range rows {
		if output[index]["trace_id"] != rows[index]["trace_id"] {
			t.Fatalf("trace_id changed at %d: %v", index, output[index]["trace_id"])
		}
	}
	safeRow := output[0]
	if safeRow["label"] != "safe" {
		t.Fatalf("safe row label = %v", safeRow["label"])
	}
	safeExtended := safeRow["extended_info"].(map[string]any)
	if safeExtended["is_attack"] != false || safeExtended["risk_type"] != "" || safeExtended["risk_level"] != "" {
		t.Fatalf("safe row extended_info = %#v", safeExtended)
	}
	if safeExtended["attack_scenario"] != "other_spam" {
		t.Fatalf("attack_scenario changed: %#v", safeExtended)
	}
	for index := 1; index < len(rows); index++ {
		if !reflect.DeepEqual(output[index], rows[index]) {
			t.Fatalf("unconfirmed row changed at %d: %#v", index, output[index])
		}
	}
}

func TestApplyRejectsDuplicateAndOutOfRangeDecisionIndices(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.json")
	writeJSONArray(t, sourcePath, []map[string]any{
		advertisementRow("trace-0", "THUCNews", "unsafe", "内容", "other_spam", 0.8),
		advertisementRow("trace-1", "THUCNews", "unsafe", "内容", "other_spam", 0.8),
	})
	batchDirectory := filepath.Join(directory, "review-batches")
	if err := os.MkdirAll(batchDirectory, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	writeFile(t, filepath.Join(batchDirectory, "batch-001.json"), `{"items":[{"i":0,"p":"a"},{"i":1,"p":"b"}]}`)
	writeFile(t, filepath.Join(directory, "review-batch-map.jsonl"),
		`{"batch":"batch-001.json","items":[{"i":0,"trace_id":"trace-0"},{"i":1,"trace_id":"trace-1"}]}`+"\n")
	tests := []struct {
		name      string
		decisions string
	}{
		{name: "duplicate index", decisions: `{"r":[{"i":0,"l":1,"t":"news_context"},{"i":0,"l":2,"t":"actual_ad"}]}`},
		{name: "out of range index", decisions: `{"r":[{"i":0,"l":1,"t":"news_context"},{"i":2,"l":2,"t":"actual_ad"}]}`},
		{name: "invalid enum", decisions: `{"r":[{"i":0,"l":1,"t":"unknown"},{"i":1,"l":2,"t":"actual_ad"}]}`},
		{name: "extra field", decisions: `{"r":[{"i":0,"l":1,"t":"news_context","x":1},{"i":1,"l":2,"t":"actual_ad"}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decisionsPath := filepath.Join(directory, strings.ReplaceAll(tt.name, " ", "-")+".jsonl")
			writeFile(t, decisionsPath, tt.decisions+"\n")
			_, err := applyDecisions(applyConfig{
				InputPath:     sourcePath,
				OutputPath:    filepath.Join(directory, strings.ReplaceAll(tt.name, " ", "-")+".out.json"),
				Format:        "json",
				BatchDir:      batchDirectory,
				MappingPath:   filepath.Join(directory, "review-batch-map.jsonl"),
				DecisionsPath: decisionsPath,
			})
			if err == nil {
				t.Fatal("applyDecisions() error = nil")
			}
		})
	}
}

func TestApplyRefusesDefaultProvisionalOutput(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.json")
	writeJSONArray(t, sourcePath, []map[string]any{
		advertisementRow("trace-0", "THUCNews", "unsafe", "内容", "other_spam", 0.8),
	})
	batchDirectory := filepath.Join(directory, "review-batches")
	if err := os.MkdirAll(batchDirectory, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	writeFile(t, filepath.Join(batchDirectory, "batch-001.json"), `{"items":[{"i":0,"p":"内容"}]}`)
	writeFile(t, filepath.Join(directory, "review-batch-map.jsonl"),
		`{"batch":"batch-001.json","items":[{"i":0,"trace_id":"trace-0"}]}`+"\n")
	writeFile(t, filepath.Join(directory, "decisions.jsonl"), `{"r":[{"i":0,"l":1,"t":"news_context"}]}`+"\n")
	_, err := applyDecisions(applyConfig{
		InputPath:     sourcePath,
		OutputPath:    defaultProvisionalPath,
		Format:        "json",
		BatchDir:      batchDirectory,
		MappingPath:   filepath.Join(directory, "review-batch-map.jsonl"),
		DecisionsPath: filepath.Join(directory, "decisions.jsonl"),
	})
	if err == nil {
		t.Fatal("applyDecisions() overwrote protected provisional path")
	}
}

func TestApplyJSONLPreservesIDsAndWritesNewVersion(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.jsonl")
	outputPath := filepath.Join(directory, "applied.jsonl")
	rows := []map[string]any{
		advertisementRow("jsonl-0", "THUCNews", "unsafe", "内容零", "other_spam", 0.8),
		advertisementRow("jsonl-1", "THUCNews", "unsafe", "内容一", "other_spam", 0.8),
	}
	var sourceBuilder strings.Builder
	for _, row := range rows {
		encoded, err := json.Marshal(row)
		if err != nil {
			t.Fatalf("marshal JSONL row: %v", err)
		}
		sourceBuilder.Write(encoded)
		sourceBuilder.WriteByte('\n')
	}
	writeFile(t, sourcePath, sourceBuilder.String())
	batchDirectory := filepath.Join(directory, "review-batches")
	if err := os.MkdirAll(batchDirectory, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	writeFile(t, filepath.Join(batchDirectory, "batch-001.json"), `{"items":[{"i":0,"p":"a"},{"i":1,"p":"b"}]}`)
	writeFile(t, filepath.Join(directory, "review-batch-map.jsonl"),
		`{"batch":"batch-001.json","items":[{"i":0,"trace_id":"jsonl-0"},{"i":1,"trace_id":"jsonl-1"}]}`+"\n")
	writeFile(t, filepath.Join(directory, "decisions.jsonl"),
		`{"r":[{"i":0,"l":1,"t":"news_context"},{"i":1,"l":2,"t":"actual_ad"}]}`+"\n")
	report, err := applyDecisions(applyConfig{
		InputPath:     sourcePath,
		OutputPath:    outputPath,
		Format:        "auto",
		BatchDir:      batchDirectory,
		MappingPath:   filepath.Join(directory, "review-batch-map.jsonl"),
		DecisionsPath: filepath.Join(directory, "decisions.jsonl"),
	})
	if err != nil {
		t.Fatalf("applyDecisions() error = %v", err)
	}
	if report.Modified != 1 || report.Unchanged != 1 {
		t.Fatalf("apply JSONL report = %+v", report)
	}
	output := readJSONL(t, outputPath)
	if len(output) != 2 || output[0]["trace_id"] != "jsonl-0" || output[1]["trace_id"] != "jsonl-1" {
		t.Fatalf("JSONL output IDs = %#v", output)
	}
	if output[0]["label"] != "safe" || output[1]["label"] != "unsafe" {
		t.Fatalf("JSONL labels = %v/%v", output[0]["label"], output[1]["label"])
	}
}

// advertisementRow 构造合成广告源记录。
func advertisementRow(traceID, source, label, prompt, scenario string, quality float64) map[string]any {
	return map[string]any{
		"trace_id":    traceID,
		"source":      source,
		"split":       "train",
		"language":    "zh",
		"scene":       "prompt",
		"label":       label,
		"prompt":      prompt,
		"response":    "",
		"explanation": "合成解释",
		"extended_info": map[string]any{
			"risk_type":       "spam_advertisement",
			"risk_level":      "medium",
			"attack_scenario": scenario,
			"case_type":       "typical",
			"is_attack":       label == "unsafe",
			"other":           "",
		},
		"annotation": map[string]any{
			"method":        "auto",
			"quality_score": quality,
		},
	}
}

// writeJSONArray 写入测试用 JSON 数组。
func writeJSONArray(t *testing.T, path string, rows []map[string]any) {
	t.Helper()
	encoded, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("marshal JSON array: %v", err)
	}
	writeFile(t, path, string(encoded))
}

// writeWorksheet 写入测试用人工复核 worksheet。
func writeWorksheet(t *testing.T, path string, rows []map[string]string) {
	t.Helper()
	header := []string{
		"trace_id",
		"source_json",
		"original_label",
		"attack_scenario",
		"model_label",
		"model_legacy_risk",
		"model_scenario_suspect",
		"issues",
		"human_label",
		"human_legacy_overlap",
		"human_notes",
	}
	var builder strings.Builder
	builder.WriteString(strings.Join(header, ","))
	builder.WriteByte('\n')
	for _, row := range rows {
		values := make([]string, 0, len(header))
		for _, name := range header {
			value := row[name]
			if strings.ContainsAny(value, ",\"\n") {
				value = `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
			}
			values = append(values, value)
		}
		builder.WriteString(strings.Join(values, ","))
		builder.WriteByte('\n')
	}
	writeFile(t, path, builder.String())
}

// writeFile 写入测试文件。
func writeFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

// readFile 读取测试文件。
func readFile(t *testing.T, path string) []byte {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	return contents
}

// readJSONL 读取测试 JSONL 文件。
func readJSONL(t *testing.T, path string) []map[string]any {
	t.Helper()
	contents := readFile(t, path)
	rows := make([]map[string]any, 0)
	for _, line := range strings.Split(strings.TrimSpace(string(contents)), "\n") {
		if line == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("decode JSONL %s: %v", path, err)
		}
		rows = append(rows, row)
	}
	return rows
}

// readJSONArray 读取测试 JSON 数组。
func readJSONArray(t *testing.T, path string) []map[string]any {
	t.Helper()
	var rows []map[string]any
	if err := json.Unmarshal(readFile(t, path), &rows); err != nil {
		t.Fatalf("decode JSON array %s: %v", path, err)
	}
	return rows
}

// traceIDs 提取测试记录的稳定 ID 列表。
func traceIDs(rows []sourceRow) []string {
	ids := make([]string, len(rows))
	for index, row := range rows {
		ids[index] = row.TraceID
	}
	return ids
}
