package service_test

import (
	"encoding/json"
	"strings"
	"testing"

	"sendllm/internal/dao"
	"sendllm/internal/dto"
	"sendllm/internal/service"
)

// TestSafetyReviewExpertCreatesOneStagePerCandidate 验证每个候选只创建一个固定 key 的 Expert 阶段。
func TestSafetyReviewExpertCreatesOneStagePerCandidate(t *testing.T) {
	policy := loadSafetyReviewPolicyForRouting(t)
	route := dto.SafetyReviewRoute{
		AttackMethodCandidates: []dto.SafetyReviewCandidate{
			{Category: "jailbreak", Reason: "召回"},
			{Category: "prompt_injection", Reason: "召回"},
		},
		AttackDomainCandidates: []dto.SafetyReviewCandidate{
			{Category: "gender_discrimination", Reason: "召回"},
		},
	}

	plan, err := service.BuildSafetyReviewRoutingPlan(policy, "prompt", route)
	if err != nil {
		t.Fatalf("BuildSafetyReviewRoutingPlan() error = %v", err)
	}
	want := []string{
		"expert:attack_domain:gender_discrimination",
		"expert:attack_method:jailbreak",
		"expert:attack_method:prompt_injection",
	}
	if len(plan.DownstreamStages) != len(want) {
		t.Fatalf("stage count = %d, want %d", len(plan.DownstreamStages), len(want))
	}
	for index, stage := range plan.DownstreamStages {
		if stage.StageKey != want[index] || stage.Role != "expert" {
			t.Fatalf("stage %d = %+v, want key %q", index, stage, want[index])
		}
		if stage.Axis == "" || stage.Category == "" {
			t.Fatalf("stage %d lacks axis/category: %+v", index, stage)
		}
	}
}

// TestSafetyReviewExpertRequestLoadsOneBlindRuleCard 验证 Expert 请求只包含一个冻结规则卡
// 且没有先验输出。
func TestSafetyReviewExpertRequestLoadsOneBlindRuleCard(t *testing.T) {
	policy := loadSafetyReviewPolicyForRouting(t)
	work := dao.SafetyReviewStageWork{
		TaskID:   "task-1",
		TraceID:  "trace-1",
		StageKey: "expert:attack_domain:ethnic_discrimination",
		Role:     "expert",
		Axis:     "attack_domain",
		Category: "ethnic_discrimination",
		Scene:    "prompt",
		Prompt:   "expert-prompt",
		RawJSON: []byte(`{"label":"unsafe","explanation":"原始解释","annotation":{"old":true},` +
			`"router":"LEAKED_ROUTER_OUTPUT","judge_a":"LEAKED_JUDGE_OUTPUT"}`),
	}

	request, err := service.BuildSafetyReviewExpertRequest(work, policy)
	if err != nil {
		t.Fatalf("BuildSafetyReviewExpertRequest() error = %v", err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(request.Messages[1].Content), &payload); err != nil {
		t.Fatalf("expert user payload is not JSON: %v", err)
	}
	wantKeys := []string{"trace_id", "scene", "prompt", "policy", "schema", "rule_card"}
	if len(payload) != len(wantKeys) {
		t.Fatalf("payload keys = %d, want %d", len(payload), len(wantKeys))
	}
	for _, key := range wantKeys {
		if _, ok := payload[key]; !ok {
			t.Fatalf("expert payload lacks %s", key)
		}
	}
	var card map[string]json.RawMessage
	if err := json.Unmarshal(payload["rule_card"], &card); err != nil {
		t.Fatalf("rule card is not JSON: %v", err)
	}
	if string(card["id"]) != `"ethnic_discrimination"` || string(card["axis"]) != `"attack_domain"` {
		t.Fatalf("rule card identity = %s/%s", card["id"], card["axis"])
	}
	combined := request.Messages[0].Content + request.Messages[1].Content
	for _, marker := range []string{"LEAKED_ROUTER_OUTPUT", "LEAKED_JUDGE_OUTPUT", "原始解释"} {
		if strings.Contains(combined, marker) {
			t.Fatalf("expert request contains forbidden prior output %q", marker)
		}
	}
}

// TestSafetyReviewExpertRequestRejectsWrongCardAssignment 验证 Expert 阶段与冻结卡片
// 的轴和类别必须一致。
func TestSafetyReviewExpertRequestRejectsWrongCardAssignment(t *testing.T) {
	policy := loadSafetyReviewPolicyForRouting(t)
	work := dao.SafetyReviewStageWork{
		TaskID:   "task-1",
		TraceID:  "trace-1",
		StageKey: "expert:attack_method:ethnic_discrimination",
		Role:     "expert",
		Axis:     "attack_method",
		Category: "ethnic_discrimination",
		Scene:    "prompt",
		Prompt:   "expert-prompt",
	}

	_, err := service.BuildSafetyReviewExpertRequest(work, policy)
	if err == nil {
		t.Fatal("BuildSafetyReviewExpertRequest() returned nil error for wrong card assignment")
	}
}
