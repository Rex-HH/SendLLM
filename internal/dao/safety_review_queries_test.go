package dao

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestSafetyReviewReadSummaryCountsStates 验证摘要只包含任务、条目、阶段与决策计数。
func TestSafetyReviewReadSummaryCountsStates(t *testing.T) {
	store, _ := openSafetyReviewStore(t)
	ctx := context.Background()
	defer closeSafetyReviewStore(t, store)
	task := newSafetyReviewTask("response")
	if err := store.EnsureTask(ctx, task); err != nil {
		t.Fatalf("EnsureTask() error = %v", err)
	}
	input := strings.Join([]string{safetyReviewInputLine("one"), safetyReviewInputLine("two")}, "\n") + "\n"
	if _, err := store.ImportJSONL(ctx, task.ID, strings.NewReader(input)); err != nil {
		t.Fatalf("ImportJSONL() error = %v", err)
	}
	summary, err := store.ReadSummary(ctx, task.ID)
	if err != nil {
		t.Fatalf("ReadSummary(initial) error = %v", err)
	}
	if summary.TaskID != task.ID || summary.Scene != task.Scene || summary.TaskStatus != "created" {
		t.Fatalf("summary identity = %+v", summary)
	}
	if summary.Items["pending_initial"] != 2 || summary.Stages["pending"] != 6 || summary.Decisions != 0 {
		t.Fatalf("initial summary = %+v", summary)
	}

	work := claimSafetyReviewStage(t, store, ctx, task.ID, "judge_a")
	if err := store.CompleteStage(ctx, SafetyReviewStageCompletion{
		TaskID:     task.ID,
		TraceID:    work.TraceID,
		StageKey:   work.StageKey,
		Outcome:    SafetyReviewStageSucceeded,
		ResultJSON: []byte(`{"verdict":"safe"}`),
		Attempt:    safetyReviewAttempt("classification"),
		ItemState:  "awaiting_initial",
	}); err != nil {
		t.Fatalf("CompleteStage() error = %v", err)
	}
	summary, err = store.ReadSummary(ctx, task.ID)
	if err != nil {
		t.Fatalf("ReadSummary(after completion) error = %v", err)
	}
	if summary.Items["awaiting_initial"] != 1 || summary.Items["pending_initial"] != 1 ||
		summary.Stages["succeeded"] != 1 || summary.Stages["running"] != 0 {
		t.Fatalf("updated summary = %+v", summary)
	}

	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("Marshal(summary) error = %v", err)
	}
	text := string(encoded)
	for _, forbidden := range []string{"prompt", "raw_json", "raw_response"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("summary contains forbidden field %q", forbidden)
		}
	}
}

// TestSafetyReviewReadSummaryMissingTask 验证缺失任务返回明确错误。
func TestSafetyReviewReadSummaryMissingTask(t *testing.T) {
	store, _ := openSafetyReviewStore(t)
	ctx := context.Background()
	defer closeSafetyReviewStore(t, store)
	if _, err := store.ReadSummary(ctx, "missing-task"); err == nil {
		t.Fatal("ReadSummary(missing task) returned nil error")
	}
}
