package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sendllm/internal/service"
)

// safetyReviewEvalSchema 是 Safety Review 正式结果行的 JSON Schema。
const safetyReviewEvalSchema = `{
  "$schema":"https://json-schema.org/draft/2020-12/schema",
  "type":"object",
  "required":["trace_id","annotation"],
  "properties":{
    "trace_id":{"type":"string","minLength":1},
    "annotation":{
      "type":"object",
      "required":["method","label","is_attack","attack_methods","attack_domains","case_type"],
      "properties":{
        "method":{"const":"auto"},
        "label":{"enum":["safe","unsafe"]},
        "is_attack":{"type":"boolean"},
        "attack_method":{"type":"string"},
        "attack_domain":{"type":"string"},
        "risk_type":{"type":"string"},
        "attack_methods":{"type":"array","items":{"type":"string"}},
        "attack_domains":{"type":"array","items":{"type":"string"}},
        "case_type":{"enum":["typical","borderline","variant","hard_negative"]}
      },
      "additionalProperties":false
    }
  },
  "additionalProperties":true
}`

// safetyReviewEvalGold 表示隐藏 Gold 的最小评估字段。
type safetyReviewEvalGold struct {
	TraceID   string `json:"trace_id"`
	Scene     string `json:"scene"`
	GoldLabel string `json:"gold_label"`
	CaseType  string `json:"case_type"`
	RiskType  string `json:"risk_type"`
	Source    string `json:"source"`
}

// safetyReviewEvalRun 保存一个可变异的 scene run。
type safetyReviewEvalRun struct {
	dir        string
	clean      []map[string]any
	quarantine []map[string]any
	audit      []map[string]any
}

// safetyReviewEvalFixture 保存两轮、两场景的评估现场。
type safetyReviewEvalFixture struct {
	temp         string
	goldPath     string
	gold         []safetyReviewEvalGold
	promptGold   []safetyReviewEvalGold
	responseGold []safetyReviewEvalGold
	runs         map[string]map[string]*safetyReviewEvalRun
}

// TestSafetyReviewEvalAcceptsPassingRotations 验证两轮独立 rotation 全部通过。
func TestSafetyReviewEvalAcceptsPassingRotations(t *testing.T) {
	fixture := newSafetyReviewEvalFixture(t)
	summary, err := service.EvaluateSafetyReviewOutputs(context.Background(), service.SafetyReviewEvaluationConfig{
		GoldPath: fixture.goldPath,
		Schema:   json.RawMessage(safetyReviewEvalSchema),
		Runs:     fixture.evalRuns(),
	})
	if err != nil {
		t.Fatalf("EvaluateSafetyReviewOutputs() error = %v; summary=%+v", err, summary)
	}
	if !summary.Passed || len(summary.Rotations) != 2 {
		t.Fatalf("summary = %+v, want two passing rotations", summary)
	}
	for _, rotation := range summary.Rotations {
		if rotation.OutputCount != 50 || rotation.SchemaValid != 40 ||
			rotation.TotalQuarantined != 10 || rotation.UnsafeGoldResolvedUnsafe != 20 ||
			rotation.UnsafeGoldFalseSafe != 0 || rotation.SafeGoldFalseUnsafe != 0 ||
			rotation.BoundaryAcceptable != 10 || rotation.UnsafeWithoutExpert != 0 ||
			rotation.ResponseAttackMethod != 0 || rotation.UnaccountedStageFailure != 0 {
			t.Fatalf("rotation metrics = %+v", rotation)
		}
	}
}

// TestSafetyReviewEvalRejectsSafeGoldUnsafe 验证 Safe Gold 被判 Unsafe 必须失败。
func TestSafetyReviewEvalRejectsSafeGoldUnsafe(t *testing.T) {
	fixture := newSafetyReviewEvalFixture(t)
	mutateSafetyReviewEvalClean(fixture, "rotation-a", "prompt", "prompt-safe-01", func(row map[string]any) {
		annotation := row["annotation"].(map[string]any)
		annotation["label"] = "unsafe"
		annotation["attack_domains"] = []string{"ethnic_discrimination"}
	})
	assertSafetyReviewEvalRejected(t, fixture, "safe gold false unsafe")
}

// TestSafetyReviewEvalRejectsUnsafeGoldSafe 验证 Unsafe Gold 被判 Safe 必须失败。
func TestSafetyReviewEvalRejectsUnsafeGoldSafe(t *testing.T) {
	fixture := newSafetyReviewEvalFixture(t)
	mutateSafetyReviewEvalClean(fixture, "rotation-a", "prompt", "prompt-unsafe-01", func(row map[string]any) {
		annotation := row["annotation"].(map[string]any)
		annotation["label"] = "safe"
		annotation["attack_domains"] = []string{}
		annotation["attack_domain"] = ""
		annotation["risk_type"] = ""
	})
	assertSafetyReviewEvalRejected(t, fixture, "unsafe gold false safe")
}

// TestSafetyReviewEvalRejectsLowUnsafeRecall 验证 Unsafe Gold 召回不足必须失败。
func TestSafetyReviewEvalRejectsLowUnsafeRecall(t *testing.T) {
	fixture := newSafetyReviewEvalFixture(t)
	moveSafetyReviewEvalCleanToQuarantine(fixture, "rotation-a", "prompt", "prompt-unsafe-01")
	moveSafetyReviewEvalCleanToQuarantine(fixture, "rotation-a", "prompt", "prompt-unsafe-02")
	moveSafetyReviewEvalCleanToQuarantine(fixture, "rotation-a", "response", "response-unsafe-01")
	assertSafetyReviewEvalRejected(t, fixture, "unsafe recall")
}

// TestSafetyReviewEvalRejectsUnsafeQuarantineOverflow 验证 Unsafe Gold 隔离超限必须失败。
func TestSafetyReviewEvalRejectsUnsafeQuarantineOverflow(t *testing.T) {
	fixture := newSafetyReviewEvalFixture(t)
	moveSafetyReviewEvalCleanToQuarantine(fixture, "rotation-a", "prompt", "prompt-unsafe-01")
	moveSafetyReviewEvalCleanToQuarantine(fixture, "rotation-a", "prompt", "prompt-unsafe-02")
	moveSafetyReviewEvalCleanToQuarantine(fixture, "rotation-a", "prompt", "prompt-unsafe-03")
	assertSafetyReviewEvalRejected(t, fixture, "unsafe quarantine")
}

// TestSafetyReviewEvalRejectsLowBoundaryAcceptance 验证边界可接受数量不足必须失败。
func TestSafetyReviewEvalRejectsLowBoundaryAcceptance(t *testing.T) {
	fixture := newSafetyReviewEvalFixture(t)
	for _, traceID := range []string{"boundary-01", "boundary-02", "boundary-03"} {
		run := fixture.runs["rotation-a"]["response"]
		row := safetyReviewEvalQuarantineRow(traceID, "irreducible_uncertainty")
		row["annotation"].(map[string]any)["label"] = "unsafe"
		run.clean = append(run.clean, row)
		run.quarantine = removeSafetyReviewEvalRow(run.quarantine, traceID)
		mutateSafetyReviewEvalAudit(run.audit, traceID, func(auditRow map[string]any) {
			auditRow["final_state"] = "resolved_unsafe"
			auditRow["label"] = "unsafe"
		})
	}
	assertSafetyReviewEvalRejected(t, fixture, "boundary acceptance")
}

// TestSafetyReviewEvalRejectsTotalQuarantineOverflow 验证总隔离数超阈值必须失败。
func TestSafetyReviewEvalRejectsTotalQuarantineOverflow(t *testing.T) {
	fixture := newSafetyReviewEvalFixture(t)
	moveSafetyReviewEvalCleanToQuarantine(fixture, "rotation-a", "prompt", "prompt-safe-01")
	moveSafetyReviewEvalCleanToQuarantine(fixture, "rotation-a", "prompt", "prompt-safe-02")
	moveSafetyReviewEvalCleanToQuarantine(fixture, "rotation-a", "prompt", "prompt-safe-03")
	assertSafetyReviewEvalRejected(t, fixture, "total quarantine")
}

// TestSafetyReviewEvalRejectsDuplicateTraceID 验证重复 trace_id 必须失败。
func TestSafetyReviewEvalRejectsDuplicateTraceID(t *testing.T) {
	fixture := newSafetyReviewEvalFixture(t)
	run := fixture.runs["rotation-a"]["prompt"]
	duplicate := copySafetyReviewEvalRow(run.clean[0])
	run.clean = append(run.clean, duplicate)
	assertSafetyReviewEvalRejected(t, fixture, "duplicate trace_id")
}

// TestSafetyReviewEvalRejectsMissingTraceID 验证缺失 trace_id 必须失败。
func TestSafetyReviewEvalRejectsMissingTraceID(t *testing.T) {
	fixture := newSafetyReviewEvalFixture(t)
	run := fixture.runs["rotation-a"]["prompt"]
	run.clean = removeSafetyReviewEvalRow(run.clean, "prompt-safe-01")
	assertSafetyReviewEvalRejected(t, fixture, "missing trace_id")
}

// TestSafetyReviewEvalRejectsInvalidSchema 验证正式结果 Schema 校验失败必须拒绝。
func TestSafetyReviewEvalRejectsInvalidSchema(t *testing.T) {
	fixture := newSafetyReviewEvalFixture(t)
	mutateSafetyReviewEvalClean(fixture, "rotation-a", "prompt", "prompt-safe-01", func(row map[string]any) {
		delete(row, "annotation")
	})
	assertSafetyReviewEvalRejected(t, fixture, "schema")
}

// TestSafetyReviewEvalRejectsResponseAttackMethod 验证 Response 场景不能出现 attack_method。
func TestSafetyReviewEvalRejectsResponseAttackMethod(t *testing.T) {
	fixture := newSafetyReviewEvalFixture(t)
	mutateSafetyReviewEvalClean(fixture, "rotation-a", "response", "response-unsafe-01", func(row map[string]any) {
		annotation := row["annotation"].(map[string]any)
		annotation["attack_methods"] = []string{"jailbreak"}
		annotation["attack_method"] = "jailbreak"
	})
	assertSafetyReviewEvalRejected(t, fixture, "response attack method")
}

// TestSafetyReviewEvalRejectsUnsafeWithoutExpert 验证 Unsafe 结果必须有 Expert 建立的类别。
func TestSafetyReviewEvalRejectsUnsafeWithoutExpert(t *testing.T) {
	fixture := newSafetyReviewEvalFixture(t)
	mutateSafetyReviewEvalClean(fixture, "rotation-a", "prompt", "prompt-unsafe-01", func(row map[string]any) {
		annotation := row["annotation"].(map[string]any)
		annotation["attack_methods"] = []string{}
		annotation["attack_domains"] = []string{}
		annotation["attack_method"] = ""
		annotation["attack_domain"] = ""
		annotation["risk_type"] = ""
	})
	assertSafetyReviewEvalRejected(t, fixture, "unsafe without expert")
}

// TestSafetyReviewEvalRejectsUnaccountedStageFailure 验证未入账的终态阶段失败必须拒绝。
func TestSafetyReviewEvalRejectsUnaccountedStageFailure(t *testing.T) {
	fixture := newSafetyReviewEvalFixture(t)
	run := fixture.runs["rotation-a"]["prompt"]
	mutateSafetyReviewEvalAudit(run.audit, "prompt-safe-01", func(row map[string]any) {
		row["stages"] = []map[string]any{{
			"stage_key": "expert:attack_domain:ethnic_discrimination",
			"role":      "expert",
			"axis":      "attack_domain",
			"category":  "ethnic_discrimination",
			"state":     "terminal_failed",
		}}
	})
	assertSafetyReviewEvalRejected(t, fixture, "unaccounted stage")
}

// TestSafetyReviewEvalRequiresTwoRotations 验证只评估一轮不能通过。
func TestSafetyReviewEvalRequiresTwoRotations(t *testing.T) {
	fixture := newSafetyReviewEvalFixture(t)
	runs := fixture.evalRuns()
	_, err := service.EvaluateSafetyReviewOutputs(context.Background(), service.SafetyReviewEvaluationConfig{
		GoldPath: fixture.goldPath,
		Schema:   json.RawMessage(safetyReviewEvalSchema),
		Runs:     runs[:2],
	})
	if err == nil || !strings.Contains(err.Error(), "two rotations") {
		t.Fatalf("EvaluateSafetyReviewOutputs(one rotation) error = %v, want two rotations error", err)
	}
}

// TestSafetyReviewEvalLoadsGoldOnlyAfterTerminalDecisions 验证 Gold 只在终态后加载。
func TestSafetyReviewEvalLoadsGoldOnlyAfterTerminalDecisions(t *testing.T) {
	fixture := newSafetyReviewEvalFixture(t)
	if err := os.WriteFile(fixture.goldPath, []byte("not-json\n"), 0o600); err != nil {
		t.Fatalf("rewrite gold: %v", err)
	}
	writeSafetyReviewEvalRunStatus(t, fixture.runs["rotation-a"]["prompt"].dir, "running")
	_, err := service.EvaluateSafetyReviewOutputs(context.Background(), service.SafetyReviewEvaluationConfig{
		GoldPath: fixture.goldPath,
		Schema:   json.RawMessage(safetyReviewEvalSchema),
		Runs:     fixture.evalRuns(),
	})
	if err == nil || !strings.Contains(err.Error(), "terminal decisions") {
		t.Fatalf("EvaluateSafetyReviewOutputs(non-terminal) error = %v, want terminal decision error", err)
	}
}

// newSafetyReviewEvalFixture 构造 50 条 Gold 和两轮两场景的可通过现场。
func newSafetyReviewEvalFixture(t *testing.T) *safetyReviewEvalFixture {
	t.Helper()
	temp := t.TempDir()
	gold := safetyReviewEvalGoldRecords()
	promptGold := make([]safetyReviewEvalGold, 0, 20)
	responseGold := make([]safetyReviewEvalGold, 0, 30)
	for _, record := range gold {
		if record.Scene == "prompt" {
			promptGold = append(promptGold, record)
			continue
		}
		responseGold = append(responseGold, record)
	}
	goldPath := filepath.Join(temp, "hidden.jsonl")
	writeSafetyReviewEvalGold(t, goldPath, gold)

	runs := map[string]map[string]*safetyReviewEvalRun{
		"rotation-a": {}, "rotation-b": {},
	}
	for _, rotation := range []string{"rotation-a", "rotation-b"} {
		for _, scene := range []string{"prompt", "response"} {
			sceneGold := promptGold
			if scene == "response" {
				sceneGold = responseGold
			}
			clean := safetyReviewEvalCleanRows(sceneGold)
			quarantine := safetyReviewEvalQuarantineRows(sceneGold)
			audit := safetyReviewEvalAuditRows(sceneGold)
			dir := filepath.Join(temp, rotation, scene)
			writeSafetyReviewEvalRun(t, dir, clean, quarantine, audit)
			runs[rotation][scene] = &safetyReviewEvalRun{
				dir: dir, clean: copySafetyReviewEvalRows(clean),
				quarantine: copySafetyReviewEvalRows(quarantine), audit: copySafetyReviewEvalRows(audit),
			}
		}
	}
	return &safetyReviewEvalFixture{
		temp: temp, goldPath: goldPath, gold: gold,
		promptGold: promptGold, responseGold: responseGold, runs: runs,
	}
}

// evalRuns 返回评估 API 需要的四次 scene run。
func (f *safetyReviewEvalFixture) evalRuns() []service.SafetyReviewEvalRun {
	result := make([]service.SafetyReviewEvalRun, 0, 4)
	for _, rotation := range []string{"rotation-a", "rotation-b"} {
		for _, scene := range []string{"prompt", "response"} {
			result = append(result, service.SafetyReviewEvalRun{
				Rotation: rotation, Scene: scene,
				TaskDir: f.runs[rotation][scene].dir, TaskID: rotation + "-" + scene,
			})
		}
	}
	return result
}

// safetyReviewEvalGoldRecords 生成 10/10/10/10/10 的隐藏 Gold。
func safetyReviewEvalGoldRecords() []safetyReviewEvalGold {
	records := make([]safetyReviewEvalGold, 0, 50)
	for index := 1; index <= 10; index++ {
		records = append(records, safetyReviewEvalGold{
			TraceID: "prompt-unsafe-" + twoDigits(index), Scene: "prompt",
			GoldLabel: "unsafe", CaseType: "typical", RiskType: "ethnic_discrimination",
			Source: "approved_gold",
		})
	}
	for index := 1; index <= 10; index++ {
		records = append(records, safetyReviewEvalGold{
			TraceID: "prompt-safe-" + twoDigits(index), Scene: "prompt",
			GoldLabel: "safe", CaseType: "typical", Source: "approved_gold",
		})
	}
	for index := 1; index <= 10; index++ {
		records = append(records, safetyReviewEvalGold{
			TraceID: "response-unsafe-" + twoDigits(index), Scene: "response",
			GoldLabel: "unsafe", CaseType: "typical", RiskType: "ethnic_discrimination",
			Source: "approved_gold",
		})
	}
	for index := 1; index <= 10; index++ {
		records = append(records, safetyReviewEvalGold{
			TraceID: "response-safe-" + twoDigits(index), Scene: "response",
			GoldLabel: "safe", CaseType: "hard_negative", Source: "approved_gold",
		})
	}
	for index := 1; index <= 10; index++ {
		records = append(records, safetyReviewEvalGold{
			TraceID: "boundary-" + twoDigits(index), Scene: "response",
			GoldLabel: "safe", CaseType: "borderline", Source: "approved_gold",
		})
	}
	return records
}

// safetyReviewEvalCleanRows 根据 Gold 生成 clean 输出。
func safetyReviewEvalCleanRows(gold []safetyReviewEvalGold) []map[string]any {
	rows := make([]map[string]any, 0, len(gold))
	for _, record := range gold {
		if record.CaseType == "borderline" {
			continue
		}
		annotation := map[string]any{
			"method": "auto", "label": record.GoldLabel, "is_attack": false,
			"attack_methods": []string{}, "attack_domains": []string{},
			"case_type": record.CaseType,
		}
		if record.GoldLabel == "unsafe" {
			annotation["attack_domains"] = []string{"ethnic_discrimination"}
			annotation["attack_domain"] = "ethnic_discrimination"
			annotation["risk_type"] = "ethnic_discrimination"
		}
		rows = append(rows, map[string]any{"trace_id": record.TraceID, "annotation": annotation})
	}
	return rows
}

// safetyReviewEvalQuarantineRows 根据 Gold 生成隔离输出。
func safetyReviewEvalQuarantineRows(gold []safetyReviewEvalGold) []map[string]any {
	rows := make([]map[string]any, 0)
	for _, record := range gold {
		if record.CaseType != "borderline" {
			continue
		}
		rows = append(rows, safetyReviewEvalQuarantineRow(record.TraceID, "irreducible_uncertainty"))
	}
	return rows
}

// safetyReviewEvalQuarantineRow 构造一条隔离输出。
func safetyReviewEvalQuarantineRow(traceID, reason string) map[string]any {
	return map[string]any{
		"trace_id": traceID,
		"annotation": map[string]any{
			"method": "manual_required", "label": "", "is_attack": false,
			"attack_methods": []string{}, "attack_domains": []string{},
			"case_type": "", "quarantine_reason": reason,
		},
	}
}

// safetyReviewEvalAuditRows 生成与输出对应的审计行。
func safetyReviewEvalAuditRows(gold []safetyReviewEvalGold) []map[string]any {
	rows := make([]map[string]any, 0, len(gold))
	for _, record := range gold {
		state := "resolved_" + record.GoldLabel
		label := record.GoldLabel
		if record.CaseType == "borderline" {
			state = "quarantined"
			label = ""
		}
		rows = append(rows, map[string]any{
			"trace_id": record.TraceID, "scene": record.Scene, "final_state": state,
			"label": label, "is_attack": false, "attack_methods": []string{},
			"attack_domains": []string{}, "primary_attack_method": "",
			"primary_attack_domain": "", "risk_type": "", "case_type": record.CaseType,
			"decision_rules": []string{"DISCRIMINATION-R01"}, "quarantine_reason": "",
			"stages": []map[string]any{}, "attempt_count": 3,
		})
	}
	return rows
}

// writeSafetyReviewEvalGold 写入隐藏 Gold 文件。
func writeSafetyReviewEvalGold(t *testing.T, path string, gold []safetyReviewEvalGold) {
	t.Helper()
	var builder strings.Builder
	for _, record := range gold {
		raw, err := json.Marshal(record)
		if err != nil {
			t.Fatalf("marshal gold: %v", err)
		}
		builder.Write(raw)
		builder.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(builder.String()), 0o600); err != nil {
		t.Fatalf("write gold: %v", err)
	}
}

// writeSafetyReviewEvalRun 写入一个完整 scene run 的正式导出。
func writeSafetyReviewEvalRun(
	t *testing.T,
	dir string,
	clean []map[string]any,
	quarantine []map[string]any,
	audit []map[string]any,
) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create run dir: %v", err)
	}
	writeSafetyReviewEvalJSONL(t, filepath.Join(dir, "clean.jsonl"), clean)
	writeSafetyReviewEvalJSONL(t, filepath.Join(dir, "quarantine.jsonl"), quarantine)
	writeSafetyReviewEvalJSONL(t, filepath.Join(dir, "audit.jsonl"), audit)
	writeSafetyReviewEvalRunStatus(t, dir, "completed")
}

// writeSafetyReviewEvalJSONL 写入 JSONL 文件。
func writeSafetyReviewEvalJSONL(t *testing.T, path string, rows []map[string]any) {
	t.Helper()
	var builder strings.Builder
	for _, row := range rows {
		raw, err := json.Marshal(row)
		if err != nil {
			t.Fatalf("marshal JSONL row: %v", err)
		}
		builder.Write(raw)
		builder.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(builder.String()), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// writeSafetyReviewEvalRunStatus 写入 run-status 文件。
func writeSafetyReviewEvalRunStatus(t *testing.T, dir, status string) {
	t.Helper()
	payload := map[string]any{
		"task_id": "eval", "status": status, "items": map[string]int{
			"resolved_safe": 30, "resolved_unsafe": 20, "quarantined": 10,
		}, "stages": map[string]int{}, "decisions": 50,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal run status: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run-status.json"), raw, 0o600); err != nil {
		t.Fatalf("write run status: %v", err)
	}
}

// mutateSafetyReviewEvalClean 修改指定 clean 行。
func mutateSafetyReviewEvalClean(
	fixture *safetyReviewEvalFixture,
	rotation, scene, traceID string,
	mutate func(map[string]any),
) {
	for _, row := range fixture.runs[rotation][scene].clean {
		if row["trace_id"] == traceID {
			mutate(row)
			return
		}
	}
}

// mutateSafetyReviewEvalAudit 修改指定审计行。
func mutateSafetyReviewEvalAudit(rows []map[string]any, traceID string, mutate func(map[string]any)) {
	for _, row := range rows {
		if row["trace_id"] == traceID {
			mutate(row)
			return
		}
	}
}

// moveSafetyReviewEvalCleanToQuarantine 将 clean 行移动到隔离输出。
func moveSafetyReviewEvalCleanToQuarantine(
	fixture *safetyReviewEvalFixture,
	rotation, scene, traceID string,
) {
	run := fixture.runs[rotation][scene]
	var moved map[string]any
	clean := make([]map[string]any, 0, len(run.clean))
	for _, row := range run.clean {
		if row["trace_id"] == traceID {
			moved = row
			continue
		}
		clean = append(clean, row)
	}
	if moved == nil {
		return
	}
	run.clean = clean
	run.quarantine = append(run.quarantine, safetyReviewEvalQuarantineRow(traceID, "irreducible_uncertainty"))
	mutateSafetyReviewEvalAudit(run.audit, traceID, func(row map[string]any) {
		row["final_state"] = "quarantined"
		row["label"] = ""
	})
}

// removeSafetyReviewEvalRow 删除指定 trace_id 的行。
func removeSafetyReviewEvalRow(rows []map[string]any, traceID string) []map[string]any {
	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if row["trace_id"] != traceID {
			result = append(result, row)
		}
	}
	return result
}

// copySafetyReviewEvalRows 深拷贝输出行，避免测试间共享引用。
func copySafetyReviewEvalRows(rows []map[string]any) []map[string]any {
	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		result = append(result, copySafetyReviewEvalRow(row))
	}
	return result
}

// copySafetyReviewEvalRow 深拷贝单行输出。
func copySafetyReviewEvalRow(row map[string]any) map[string]any {
	raw, err := json.Marshal(row)
	if err != nil {
		panic(err)
	}
	result := map[string]any{}
	if err := json.Unmarshal(raw, &result); err != nil {
		panic(err)
	}
	return result
}

// assertSafetyReviewEvalRejected 断言变异后的评估必须失败并落盘。
func assertSafetyReviewEvalRejected(t *testing.T, fixture *safetyReviewEvalFixture, phrase string) {
	t.Helper()
	for _, rotation := range []string{"rotation-a", "rotation-b"} {
		for _, scene := range []string{"prompt", "response"} {
			run := fixture.runs[rotation][scene]
			writeSafetyReviewEvalRun(t, run.dir, run.clean, run.quarantine, run.audit)
		}
	}
	_, err := service.EvaluateSafetyReviewOutputs(context.Background(), service.SafetyReviewEvaluationConfig{
		GoldPath: fixture.goldPath,
		Schema:   json.RawMessage(safetyReviewEvalSchema),
		Runs:     fixture.evalRuns(),
	})
	if err == nil {
		t.Fatalf("EvaluateSafetyReviewOutputs() accepted invalid %s output", phrase)
	}
	if !errors.Is(err, service.ErrSafetyReviewEvaluationFailed) {
		t.Fatalf("error = %v, want %v containing %s", err, service.ErrSafetyReviewEvaluationFailed, phrase)
	}
	if !strings.Contains(err.Error(), phrase) {
		t.Fatalf("error = %v, want phrase %q", err, phrase)
	}
}

// twoDigits 返回两位数字字符串。
func twoDigits(index int) string {
	return fmt.Sprintf("%02d", index)
}
