package dao

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestSafetyReviewClaimStageFiltersRoleAndUpdatesItem 验证角色过滤、模型字段和首次领取状态。
func TestSafetyReviewClaimStageFiltersRoleAndUpdatesItem(t *testing.T) {
	store, path := openSafetyReviewStore(t)
	ctx := context.Background()
	defer closeSafetyReviewStore(t, store)
	task := newSafetyReviewTask("response")
	if err := store.EnsureTask(ctx, task); err != nil {
		t.Fatalf("EnsureTask() error = %v", err)
	}
	if _, err := store.ImportJSONL(ctx, task.ID, strings.NewReader(safetyReviewInputLine("one")+"\n")); err != nil {
		t.Fatalf("ImportJSONL() error = %v", err)
	}

	work, ok, err := store.ClaimStage(ctx, SafetyReviewClaim{
		TaskID:        task.ID,
		Role:          "judge_b",
		Now:           safetyReviewTime(0),
		ModelProfile:  "model-b",
		ModelFamily:   "family-b",
		FallbackIndex: 0,
	})
	if err != nil || !ok {
		t.Fatalf("ClaimStage(judge_b) = (%+v, %v, %v), want one stage", work, ok, err)
	}
	if work.StageKey != "judge:b" || work.Role != "judge_b" || work.TraceID != "one" {
		t.Fatalf("work = %+v, want judge:b for one", work)
	}
	db := openSafetyReviewRawDB(t, path)
	state := querySafetyReviewText(t, db, `SELECT state FROM review_items WHERE trace_id = 'one'`)
	if state != "awaiting_initial" {
		t.Fatalf("item state = %q, want awaiting_initial", state)
	}
	profile := querySafetyReviewText(t, db, `SELECT model_profile FROM review_stages WHERE stage_key = 'judge:b'`)
	family := querySafetyReviewText(t, db, `SELECT model_family FROM review_stages WHERE stage_key = 'judge:b'`)
	if profile != "model-b" || family != "family-b" {
		t.Fatalf("model fields = %q/%q, want model-b/family-b", profile, family)
	}
}

// TestSafetyReviewCompleteStagePersistsSuccessAndDownstream 验证成功完成会持久化结果、
// 尝试和下游阶段。
func TestSafetyReviewCompleteStagePersistsSuccessAndDownstream(t *testing.T) {
	store, path := openSafetyReviewStore(t)
	ctx := context.Background()
	defer closeSafetyReviewStore(t, store)
	task := newSafetyReviewTask("response")
	if err := store.EnsureTask(ctx, task); err != nil {
		t.Fatalf("EnsureTask() error = %v", err)
	}
	if _, err := store.ImportJSONL(ctx, task.ID, strings.NewReader(safetyReviewInputLine("one")+"\n")); err != nil {
		t.Fatalf("ImportJSONL() error = %v", err)
	}
	work := claimSafetyReviewStage(t, store, ctx, task.ID, "judge_a")
	completion := SafetyReviewStageCompletion{
		TaskID:   task.ID,
		TraceID:  work.TraceID,
		StageKey: work.StageKey,
		Outcome:  SafetyReviewStageSucceeded,
		ResultJSON: []byte(`{"verdict":"safe"}
`),
		Attempt: safetyReviewAttempt("classification"),
		DownstreamStages: []SafetyReviewStageSpec{
			{
				StageKey: "expert:attack_domain:ethnic_discrimination", Role: "expert",
				Axis: "attack_domain", Category: "ethnic_discrimination",
			},
		},
		ItemState: "awaiting_experts",
	}
	if err := store.CompleteStage(ctx, completion); err != nil {
		t.Fatalf("CompleteStage(success) error = %v", err)
	}

	db := openSafetyReviewRawDB(t, path)
	defer closeSafetyReviewRawDB(t, db)
	state := querySafetyReviewText(t, db, `SELECT state FROM review_stages WHERE stage_key = 'judge:a'`)
	if state != "succeeded" {
		t.Fatalf("stage state = %q, want succeeded", state)
	}
	attempts := querySafetyReviewInt(t, db, `SELECT count(*) FROM review_attempts WHERE stage_key = 'judge:a'`)
	if attempts != 1 {
		t.Fatalf("attempt count = %d, want 1", attempts)
	}
	downstream := querySafetyReviewInt(t, db, `SELECT count(*) FROM review_stages
		WHERE stage_key = 'expert:attack_domain:ethnic_discrimination'`)
	if downstream != 1 {
		t.Fatalf("downstream count = %d, want 1", downstream)
	}
	itemState := querySafetyReviewText(t, db, `SELECT state FROM review_items WHERE trace_id = 'one'`)
	if itemState != "awaiting_experts" {
		t.Fatalf("item state = %q, want awaiting_experts", itemState)
	}
}

// TestSafetyReviewCompleteStageRejectsInvalidDownstreamKey 验证下游阶段 key 必须符合固定语法。
func TestSafetyReviewCompleteStageRejectsInvalidDownstreamKey(t *testing.T) {
	store, path := openSafetyReviewStore(t)
	ctx := context.Background()
	defer closeSafetyReviewStore(t, store)
	task := newSafetyReviewTask("response")
	if err := store.EnsureTask(ctx, task); err != nil {
		t.Fatalf("EnsureTask() error = %v", err)
	}
	if _, err := store.ImportJSONL(ctx, task.ID, strings.NewReader(safetyReviewInputLine("one")+"\n")); err != nil {
		t.Fatalf("ImportJSONL() error = %v", err)
	}
	work := claimSafetyReviewStage(t, store, ctx, task.ID, "router")
	err := store.CompleteStage(ctx, SafetyReviewStageCompletion{
		TaskID:     task.ID,
		TraceID:    work.TraceID,
		StageKey:   work.StageKey,
		Outcome:    SafetyReviewStageSucceeded,
		ResultJSON: []byte(`{"coverage_complete":true}`),
		Attempt:    safetyReviewAttempt("classification"),
		DownstreamStages: []SafetyReviewStageSpec{
			{StageKey: "expert:bad", Role: "expert", Axis: "attack_domain", Category: "ethnic_discrimination"},
		},
		ItemState: "awaiting_experts",
	})
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("CompleteStage(invalid downstream) error = %v, want %v", err, ErrInvalidTransition)
	}
	db := openSafetyReviewRawDB(t, path)
	defer closeSafetyReviewRawDB(t, db)
	state := querySafetyReviewText(t, db, `SELECT state FROM review_stages WHERE stage_key = 'router'`)
	attempts := querySafetyReviewInt(t, db, `SELECT count(*) FROM review_attempts WHERE stage_key = 'router'`)
	if state != "running" || attempts != 0 {
		t.Fatalf("rollback state/attempts = %q/%d, want running/0", state, attempts)
	}
}

// TestSafetyReviewCompleteStageAllowsInitialSafeShortcut 验证三路初始阶段可直接写入
// Safe shortcut 决策。
func TestSafetyReviewCompleteStageAllowsInitialSafeShortcut(t *testing.T) {
	store, path := openSafetyReviewStore(t)
	ctx := context.Background()
	defer closeSafetyReviewStore(t, store)
	task := newSafetyReviewTask("response")
	if err := store.EnsureTask(ctx, task); err != nil {
		t.Fatalf("EnsureTask() error = %v", err)
	}
	if _, err := store.ImportJSONL(ctx, task.ID, strings.NewReader(safetyReviewInputLine("one")+"\n")); err != nil {
		t.Fatalf("ImportJSONL() error = %v", err)
	}
	work := claimSafetyReviewStage(t, store, ctx, task.ID, "router")
	completion := SafetyReviewStageCompletion{
		TaskID:     task.ID,
		TraceID:    work.TraceID,
		StageKey:   work.StageKey,
		Outcome:    SafetyReviewStageSucceeded,
		ResultJSON: []byte(`{"coverage_complete":true}`),
		Attempt:    safetyReviewAttempt("classification"),
		ItemState:  "resolved_safe",
		Decision:   safetyReviewDecision(),
	}
	if err := store.CompleteStage(ctx, completion); err != nil {
		t.Fatalf("CompleteStage(shortcut decision) error = %v", err)
	}

	db := openSafetyReviewRawDB(t, path)
	defer closeSafetyReviewRawDB(t, db)
	itemState := querySafetyReviewText(t, db, `SELECT state FROM review_items WHERE trace_id = 'one'`)
	decisions := querySafetyReviewInt(t, db, `SELECT count(*) FROM review_decisions WHERE trace_id = 'one'`)
	if itemState != "resolved_safe" || decisions != 1 {
		t.Fatalf("shortcut state/decisions = %q/%d, want resolved_safe/1", itemState, decisions)
	}
}

// TestSafetyReviewClaimStageCarriesSceneAndReadsRoleResults 验证领取结果携带场景并可读取角色阶段。
func TestSafetyReviewClaimStageCarriesSceneAndReadsRoleResults(t *testing.T) {
	store, _ := openSafetyReviewStore(t)
	ctx := context.Background()
	defer closeSafetyReviewStore(t, store)
	task := newSafetyReviewTask("response")
	if err := store.EnsureTask(ctx, task); err != nil {
		t.Fatalf("EnsureTask() error = %v", err)
	}
	if _, err := store.ImportJSONL(ctx, task.ID, strings.NewReader(safetyReviewInputLine("one")+"\n")); err != nil {
		t.Fatalf("ImportJSONL() error = %v", err)
	}

	router := claimSafetyReviewStage(t, store, ctx, task.ID, "router")
	if router.Scene != "response" {
		t.Fatalf("router scene = %q, want response", router.Scene)
	}
	expertSpec := SafetyReviewStageSpec{
		StageKey: "expert:attack_domain:ethnic_discrimination", Role: "expert",
		Axis: "attack_domain", Category: "ethnic_discrimination",
	}
	if err := store.CompleteStage(ctx, SafetyReviewStageCompletion{
		TaskID: task.ID, TraceID: router.TraceID, StageKey: router.StageKey,
		Outcome: SafetyReviewStageSucceeded, ResultJSON: []byte(`{"coverage_complete":true}`),
		Attempt: safetyReviewAttempt("classification"), ItemState: "awaiting_experts",
		DownstreamStages: []SafetyReviewStageSpec{expertSpec},
	}); err != nil {
		t.Fatalf("CompleteStage(router) error = %v", err)
	}

	expert := claimSafetyReviewStage(t, store, ctx, task.ID, "expert")
	if expert.Scene != "response" || expert.Axis != expertSpec.Axis ||
		expert.Category != expertSpec.Category || expert.StageKey != expertSpec.StageKey {
		t.Fatalf("expert work = %+v, want assignment %+v", expert, expertSpec)
	}
	running, err := store.ReadRoleStageResults(ctx, task.ID, "one", "expert")
	if err != nil {
		t.Fatalf("ReadRoleStageResults(running) error = %v", err)
	}
	if len(running) != 1 || running[0].State != "running" ||
		running[0].Axis != expertSpec.Axis || running[0].Category != expertSpec.Category {
		t.Fatalf("running role results = %+v", running)
	}
	if err := store.CompleteStage(ctx, SafetyReviewStageCompletion{
		TaskID: task.ID, TraceID: expert.TraceID, StageKey: expert.StageKey,
		Outcome: SafetyReviewStageSucceeded, ResultJSON: []byte(`{"verdict":"uncertain"}`),
		Attempt: safetyReviewAttempt("classification"), ItemState: "pending_arbiter",
		DownstreamStages: []SafetyReviewStageSpec{{StageKey: "arbiter", Role: "arbiter"}},
	}); err != nil {
		t.Fatalf("CompleteStage(expert) error = %v", err)
	}
	finished, err := store.ReadRoleStageResults(ctx, task.ID, "one", "expert")
	if err != nil {
		t.Fatalf("ReadRoleStageResults(finished) error = %v", err)
	}
	if len(finished) != 1 || finished[0].State != "succeeded" {
		t.Fatalf("finished role results = %+v", finished)
	}
}

// TestSafetyReviewCompleteStageRetryAndTerminal 验证重试等待与终态失败都会持久化尝试。
func TestSafetyReviewCompleteStageRetryAndTerminal(t *testing.T) {
	store, path := openSafetyReviewStore(t)
	ctx := context.Background()
	defer closeSafetyReviewStore(t, store)
	task := newSafetyReviewTask("response")
	if err := store.EnsureTask(ctx, task); err != nil {
		t.Fatalf("EnsureTask() error = %v", err)
	}
	input := safetyReviewInputLine("one") + "\n"
	if _, err := store.ImportJSONL(ctx, task.ID, strings.NewReader(input)); err != nil {
		t.Fatalf("ImportJSONL() error = %v", err)
	}

	router := claimSafetyReviewStage(t, store, ctx, task.ID, "router")
	retryAt := safetyReviewTime(10 * time.Minute)
	if err := store.CompleteStage(ctx, SafetyReviewStageCompletion{
		TaskID:        task.ID,
		TraceID:       router.TraceID,
		StageKey:      router.StageKey,
		Outcome:       SafetyReviewStageRetryWait,
		NextAttemptAt: retryAt,
		ErrorCategory: "server",
		ErrorSummary:  "safe retry summary",
		Attempt:       safetyReviewAttempt("classification"),
	}); err != nil {
		t.Fatalf("CompleteStage(retry) error = %v", err)
	}
	if _, ok, err := store.ClaimStage(ctx, SafetyReviewClaim{
		TaskID:       task.ID,
		Role:         "router",
		Now:          safetyReviewTime(5 * time.Minute),
		ModelProfile: "model-r",
		ModelFamily:  "family-r",
	}); err != nil || ok {
		t.Fatalf("ClaimStage(before due) = (%v, %v), want no work", ok, err)
	}
	if _, ok, err := store.ClaimStage(ctx, SafetyReviewClaim{
		TaskID:       task.ID,
		Role:         "router",
		Now:          retryAt,
		ModelProfile: "model-r",
		ModelFamily:  "family-r",
	}); err != nil || !ok {
		t.Fatalf("ClaimStage(after due) = (%v, %v), want work", ok, err)
	}

	judgeB := claimSafetyReviewStage(t, store, ctx, task.ID, "judge_b")
	if err := store.CompleteStage(ctx, SafetyReviewStageCompletion{
		TaskID:        task.ID,
		TraceID:       judgeB.TraceID,
		StageKey:      judgeB.StageKey,
		Outcome:       SafetyReviewStageTerminalFailed,
		ErrorCategory: "auth",
		ErrorSummary:  "safe terminal summary",
		Attempt:       safetyReviewAttempt("classification"),
	}); err != nil {
		t.Fatalf("CompleteStage(terminal) error = %v", err)
	}
	db := openSafetyReviewRawDB(t, path)
	defer closeSafetyReviewRawDB(t, db)
	terminal := querySafetyReviewText(t, db, `SELECT state FROM review_stages WHERE stage_key = 'judge:b'`)
	if terminal != "terminal_failed" {
		t.Fatalf("terminal state = %q, want terminal_failed", terminal)
	}
	attempts := querySafetyReviewInt(t, db, "SELECT count(*) FROM review_attempts")
	if attempts != 2 {
		t.Fatalf("attempt count = %d, want 2", attempts)
	}
}

// TestSafetyReviewRecoverRunningAcrossReopen 验证重开库只恢复 running 且保留成功与计数。
func TestSafetyReviewRecoverRunningAcrossReopen(t *testing.T) {
	store, path := openSafetyReviewStore(t)
	ctx := context.Background()
	task := newSafetyReviewTask("response")
	if err := store.EnsureTask(ctx, task); err != nil {
		t.Fatalf("EnsureTask() error = %v", err)
	}
	if _, err := store.ImportJSONL(ctx, task.ID, strings.NewReader(safetyReviewInputLine("one")+"\n")); err != nil {
		t.Fatalf("ImportJSONL() error = %v", err)
	}
	_ = claimSafetyReviewStage(t, store, ctx, task.ID, "judge_a")
	succeeded := claimSafetyReviewStage(t, store, ctx, task.ID, "judge_b")
	if err := store.CompleteStage(ctx, SafetyReviewStageCompletion{
		TaskID:     task.ID,
		TraceID:    succeeded.TraceID,
		StageKey:   succeeded.StageKey,
		Outcome:    SafetyReviewStageSucceeded,
		ResultJSON: []byte(`{"verdict":"safe"}`),
		Attempt:    safetyReviewAttempt("classification"),
		ItemState:  "awaiting_initial",
	}); err != nil {
		t.Fatalf("CompleteStage(success before reopen) error = %v", err)
	}
	closeSafetyReviewStore(t, store)

	reopened, err := OpenSafetyReview(ctx, path)
	if err != nil {
		t.Fatalf("OpenSafetyReview(reopen) error = %v", err)
	}
	defer closeSafetyReviewStore(t, reopened)
	count, err := reopened.RecoverRunning(ctx, task.ID)
	if err != nil || count != 1 {
		t.Fatalf("RecoverRunning() = (%d, %v), want (1, nil)", count, err)
	}
	db := openSafetyReviewRawDB(t, path)
	defer closeSafetyReviewRawDB(t, db)
	runningState := querySafetyReviewText(t, db, `SELECT state FROM review_stages WHERE stage_key = 'judge:a'`)
	succeededState := querySafetyReviewText(t, db, `SELECT state FROM review_stages WHERE stage_key = 'judge:b'`)
	if runningState != "pending" || succeededState != "succeeded" {
		t.Fatalf("states = %q/%q, want pending/succeeded", runningState, succeededState)
	}
	requestAttempts := querySafetyReviewInt(
		t,
		db,
		`SELECT request_attempts FROM review_stages WHERE stage_key = 'judge:b'`,
	)
	if requestAttempts != 1 {
		t.Fatalf("request_attempts = %d, want 1", requestAttempts)
	}
	if _, err := reopened.RecoverRunning(ctx, task.ID); err != nil {
		t.Fatalf("RecoverRunning(second) error = %v", err)
	}
}

// TestSafetyReviewReadInitialStageResultsIncludesStoredDegradation 验证初始快照可读回结果与降级标记。
func TestSafetyReviewReadInitialStageResultsIncludesStoredDegradation(t *testing.T) {
	store, path := openSafetyReviewStore(t)
	ctx := context.Background()
	defer closeSafetyReviewStore(t, store)
	task := newSafetyReviewTask("response")
	if err := store.EnsureTask(ctx, task); err != nil {
		t.Fatalf("EnsureTask() error = %v", err)
	}
	if _, err := store.ImportJSONL(ctx, task.ID, strings.NewReader(safetyReviewInputLine("one")+"\n")); err != nil {
		t.Fatalf("ImportJSONL() error = %v", err)
	}
	work := claimSafetyReviewStage(t, store, ctx, task.ID, "judge_a")
	if err := store.CompleteStage(ctx, SafetyReviewStageCompletion{
		TaskID:               task.ID,
		TraceID:              work.TraceID,
		StageKey:             work.StageKey,
		Outcome:              SafetyReviewStageSucceeded,
		ResultJSON:           []byte(`{"verdict":"safe"}`),
		Attempt:              safetyReviewAttempt("classification"),
		ItemState:            "awaiting_initial",
		IndependenceDegraded: true,
	}); err != nil {
		t.Fatalf("CompleteStage() error = %v", err)
	}

	reopened, err := OpenSafetyReview(ctx, path)
	if err != nil {
		t.Fatalf("OpenSafetyReview(reopen) error = %v", err)
	}
	defer closeSafetyReviewStore(t, reopened)
	results, err := reopened.ReadInitialStageResults(ctx, task.ID, "one")
	if err != nil {
		t.Fatalf("ReadInitialStageResults() error = %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("initial stage count = %d, want 3", len(results))
	}
	for _, result := range results {
		if !result.IndependenceDegraded {
			t.Fatalf("stage %s degradation = false, want true", result.StageKey)
		}
		if result.StageKey == "judge:a" && string(result.ResultJSON) != `{"verdict":"safe"}` {
			t.Fatalf("judge:a result = %q, want persisted JSON", string(result.ResultJSON))
		}
	}
}

// TestSafetyReviewCompleteStageDecisionIdempotency 验证重复完成不会产生重复决策。
func TestSafetyReviewCompleteStageDecisionIdempotency(t *testing.T) {
	store, path := openSafetyReviewStore(t)
	ctx := context.Background()
	defer closeSafetyReviewStore(t, store)
	task := newSafetyReviewTask("response")
	if err := store.EnsureTask(ctx, task); err != nil {
		t.Fatalf("EnsureTask() error = %v", err)
	}
	if _, err := store.ImportJSONL(ctx, task.ID, strings.NewReader(safetyReviewInputLine("one")+"\n")); err != nil {
		t.Fatalf("ImportJSONL() error = %v", err)
	}
	initial := claimSafetyReviewStage(t, store, ctx, task.ID, "router")
	if err := store.CompleteStage(ctx, SafetyReviewStageCompletion{
		TaskID:     task.ID,
		TraceID:    initial.TraceID,
		StageKey:   initial.StageKey,
		Outcome:    SafetyReviewStageSucceeded,
		ResultJSON: []byte(`{"coverage_complete":true}`),
		Attempt:    safetyReviewAttempt("classification"),
		ItemState:  "pending_arbiter",
		DownstreamStages: []SafetyReviewStageSpec{
			{StageKey: "arbiter", Role: "arbiter"},
		},
	}); err != nil {
		t.Fatalf("CompleteStage(initial) error = %v", err)
	}
	arbiter := claimSafetyReviewStage(t, store, ctx, task.ID, "arbiter")
	completion := SafetyReviewStageCompletion{
		TaskID:     task.ID,
		TraceID:    arbiter.TraceID,
		StageKey:   arbiter.StageKey,
		Outcome:    SafetyReviewStageSucceeded,
		ResultJSON: []byte(`{"verdict":"resolved"}`),
		Attempt:    safetyReviewAttempt("classification"),
		ItemState:  "resolved_safe",
		Decision:   safetyReviewDecision(),
	}
	if err := store.CompleteStage(ctx, completion); err != nil {
		t.Fatalf("CompleteStage(decision) error = %v", err)
	}
	if err := store.CompleteStage(ctx, completion); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("CompleteStage(repeat) error = %v, want %v", err, ErrInvalidTransition)
	}
	db := openSafetyReviewRawDB(t, path)
	defer closeSafetyReviewRawDB(t, db)
	decisions := querySafetyReviewInt(t, db, "SELECT count(*) FROM review_decisions")
	if decisions != 1 {
		t.Fatalf("decision count = %d, want 1", decisions)
	}
}

// claimSafetyReviewStage 领取指定角色的一个阶段。
func claimSafetyReviewStage(
	t *testing.T,
	store *SafetyReviewStore,
	ctx context.Context,
	taskID string,
	role string,
) SafetyReviewStageWork {
	t.Helper()
	work, ok, err := store.ClaimStage(ctx, SafetyReviewClaim{
		TaskID:       taskID,
		Role:         role,
		Now:          safetyReviewTime(0),
		ModelProfile: "model-" + role,
		ModelFamily:  "family-" + role,
	})
	if err != nil || !ok {
		t.Fatalf("ClaimStage(%s) = (%+v, %v, %v), want work", role, work, ok, err)
	}
	return work
}

// safetyReviewAttempt 构造一次安全摘要的模型尝试。
func safetyReviewAttempt(kind string) SafetyReviewAttempt {
	return SafetyReviewAttempt{
		AttemptKind:      kind,
		ModelProfile:     "model-test",
		ModelFamily:      "family-test",
		APIKeyEnv:        "MODEL_TEST_KEY",
		StartedAt:        safetyReviewTime(-time.Second),
		FinishedAt:       safetyReviewTime(0),
		HTTPStatus:       200,
		FinishReason:     "stop",
		ErrorCategory:    "",
		ErrorSummary:     "",
		Retryable:        false,
		PromptTokens:     10,
		CompletionTokens: 10,
		RawResponse:      []byte(`{"synthetic":true}`),
		ValidationError:  nil,
	}
}

// safetyReviewDecision 构造一条安全摘要决策记录。
func safetyReviewDecision() *SafetyReviewDecisionRecord {
	return &SafetyReviewDecisionRecord{
		FinalState:          "resolved_safe",
		Label:               "safe",
		IsAttack:            false,
		AttackMethods:       []byte(`[]`),
		AttackDomains:       []byte(`[]`),
		PrimaryAttackMethod: "",
		PrimaryAttackDomain: "",
		PrimaryRiskType:     "",
		CaseType:            "typical",
		Evidence:            []byte(`[]`),
		DecisionRules:       []byte(`["DISCRIMINATION-R01"]`),
		Rationale:           "安全摘要。",
		QuarantineReason:    "",
		DecidedAt:           safetyReviewTime(0),
	}
}

// safetyReviewTime 返回固定 UTC 测试时间。
func safetyReviewTime(offset time.Duration) time.Time {
	return time.Date(2026, time.September, 8, 12, 0, 0, 0, time.UTC).Add(offset)
}

// safetyReviewInputLine 构造固定格式的合成输入行。
func safetyReviewInputLine(traceID string) string {
	return `{"trace_id":"` + traceID + `","prompt":"synthetic context","response":"synthetic response"}`
}
