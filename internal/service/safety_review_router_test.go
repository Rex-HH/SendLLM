package service_test

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"sendllm/internal/dao"
	"sendllm/internal/dto"
	"sendllm/internal/service"
)

// TestSafetyReviewRouterNormalizesPromptThreeByThree 验证 Prompt 候选轴内上限与排序。
func TestSafetyReviewRouterNormalizesPromptThreeByThree(t *testing.T) {
	policy := loadSafetyReviewPolicyForRouting(t)
	route := dto.SafetyReviewRoute{
		Features: []dto.SafetyReviewFeature{
			{ID: "F1", Source: "prompt", Kind: "group_target", Span: "某民族"},
			{ID: "F2", Source: "prompt", Kind: "operational_attack_wording", Span: "忽略规则"},
		},
		AttackMethodCandidates: []dto.SafetyReviewCandidate{
			{Category: "multi_turn_jailbreak", EvidenceIDs: []string{"F2"}, Reason: "召回"},
			{Category: "prompt_injection", EvidenceIDs: []string{"F2"}, Reason: "召回"},
			{Category: "jailbreak", EvidenceIDs: []string{"F2"}, Reason: "召回"},
		},
		AttackDomainCandidates: []dto.SafetyReviewCandidate{
			{Category: "health_discrimination", EvidenceIDs: []string{"F1"}, Reason: "召回"},
			{Category: "ethnic_discrimination", EvidenceIDs: []string{"F1"}, Reason: "召回"},
			{Category: "gender_discrimination", EvidenceIDs: []string{"F1"}, Reason: "召回"},
		},
		CoverageComplete: true,
	}

	plan, err := service.BuildSafetyReviewRoutingPlan(policy, "prompt", route)
	if err != nil {
		t.Fatalf("BuildSafetyReviewRoutingPlan(prompt) error = %v", err)
	}
	if got := len(plan.Route.AttackMethodCandidates); got != 3 {
		t.Fatalf("method candidate count = %d, want 3", got)
	}
	if got := len(plan.Route.AttackDomainCandidates); got != 3 {
		t.Fatalf("domain candidate count = %d, want 3", got)
	}
	if plan.ItemState != "awaiting_experts" || len(plan.DownstreamStages) != 6 {
		t.Fatalf("plan state/stages = %q/%d, want awaiting_experts/6", plan.ItemState, len(plan.DownstreamStages))
	}
	assertCandidateOrder(t, plan.Route.AttackMethodCandidates)
	assertCandidateOrder(t, plan.Route.AttackDomainCandidates)
}

// safetyReviewRoutingPolicy 缓存测试使用的冻结发布包。
var safetyReviewRoutingPolicy *service.SafetyReviewPolicy

// safetyReviewRoutingPolicyOnce 保证发布包只加载一次。
var safetyReviewRoutingPolicyOnce sync.Once

// safetyReviewRoutingPolicyError 缓存发布包加载错误。
var safetyReviewRoutingPolicyError error

// TestSafetyReviewRouterEnforcesCandidateLimits 验证超出轴内上限和 Response 方法候选都会被拒绝。
func TestSafetyReviewRouterEnforcesCandidateLimits(t *testing.T) {
	policy := loadSafetyReviewPolicyForRouting(t)
	tests := []struct {
		name  string
		scene string
		route dto.SafetyReviewRoute
	}{
		{
			name:  "four methods",
			scene: "prompt",
			route: dto.SafetyReviewRoute{AttackMethodCandidates: fourSafetyReviewCandidates("attack_method")},
		},
		{
			name:  "four domains",
			scene: "prompt",
			route: dto.SafetyReviewRoute{AttackDomainCandidates: fourSafetyReviewCandidates("attack_domain")},
		},
		{
			name:  "response method",
			scene: "response",
			route: dto.SafetyReviewRoute{
				AttackMethodCandidates: []dto.SafetyReviewCandidate{
					{Category: "prompt_injection", Reason: "召回"},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.BuildSafetyReviewRoutingPlan(policy, test.scene, test.route)
			if err == nil {
				t.Fatal("BuildSafetyReviewRoutingPlan() returned nil error for invalid candidate limit")
			}
		})
	}
}

// TestSafetyReviewRouterDeduplicatesCandidates 验证重复候选会合并且保持稳定排序。
func TestSafetyReviewRouterDeduplicatesCandidates(t *testing.T) {
	policy := loadSafetyReviewPolicyForRouting(t)
	route := dto.SafetyReviewRoute{
		Features: []dto.SafetyReviewFeature{
			{ID: "F1", Source: "prompt", Kind: "group_target", Span: "某性别"},
		},
		AttackDomainCandidates: []dto.SafetyReviewCandidate{
			{Category: "other_discrimination", EvidenceIDs: []string{"F1"}, Reason: "第二次"},
			{Category: "gender_discrimination", EvidenceIDs: []string{"F1"}, Reason: "第一次"},
			{Category: "gender_discrimination", EvidenceIDs: []string{"F1"}, Reason: "重复"},
		},
	}

	plan, err := service.BuildSafetyReviewRoutingPlan(policy, "prompt", route)
	if err != nil {
		t.Fatalf("BuildSafetyReviewRoutingPlan() error = %v", err)
	}
	if len(plan.Route.AttackDomainCandidates) != 1 {
		t.Fatalf("candidate count = %d, want 1", len(plan.Route.AttackDomainCandidates))
	}
	got := plan.Route.AttackDomainCandidates[0]
	if got.Category != "gender_discrimination" || got.Reason != "第一次" {
		t.Fatalf("candidate = %+v, want first gender candidate", got)
	}
}

// TestSafetyReviewRouterUnknownCategoryCreatesPolicyGap 验证未知类别不能被当作 Safe 或普通候选。
func TestSafetyReviewRouterUnknownCategoryCreatesPolicyGap(t *testing.T) {
	policy := loadSafetyReviewPolicyForRouting(t)
	route := dto.SafetyReviewRoute{
		AttackDomainCandidates: []dto.SafetyReviewCandidate{
			{Category: "not_in_bundle", Reason: "未知类别"},
		},
		CoverageComplete: true,
	}

	plan, err := service.BuildSafetyReviewRoutingPlan(policy, "prompt", route)
	if err != nil {
		t.Fatalf("BuildSafetyReviewRoutingPlan(unknown) error = %v", err)
	}
	if !plan.PolicyCoverageGap || plan.ItemState != "pending_arbiter" {
		t.Fatalf("gap/state = %v/%q, want true/pending_arbiter", plan.PolicyCoverageGap, plan.ItemState)
	}
	if len(plan.DownstreamStages) != 1 || plan.DownstreamStages[0].StageKey != "arbiter" {
		t.Fatalf("downstream = %+v, want one arbiter", plan.DownstreamStages)
	}
}

// TestSafetyReviewRouterRejectsBadFeatureReferences 验证候选证据必须引用已存在的 Router 特征。
func TestSafetyReviewRouterRejectsBadFeatureReferences(t *testing.T) {
	policy := loadSafetyReviewPolicyForRouting(t)
	route := dto.SafetyReviewRoute{
		Features: []dto.SafetyReviewFeature{
			{ID: "F1", Source: "prompt", Kind: "group_target", Span: "某民族"},
		},
		AttackDomainCandidates: []dto.SafetyReviewCandidate{
			{Category: "ethnic_discrimination", EvidenceIDs: []string{"F2"}, Reason: "召回"},
		},
	}

	_, err := service.BuildSafetyReviewRoutingPlan(policy, "prompt", route)
	if err == nil {
		t.Fatal("BuildSafetyReviewRoutingPlan() returned nil error for bad feature reference")
	}
}

// TestSafetyReviewRouterPrefersSpecificDiscrimination 验证具体歧视类别覆盖时
// 不会发出 other_discrimination。
func TestSafetyReviewRouterPrefersSpecificDiscrimination(t *testing.T) {
	policy := loadSafetyReviewPolicyForRouting(t)
	route := dto.SafetyReviewRoute{
		Features: []dto.SafetyReviewFeature{
			{ID: "F1", Source: "prompt", Kind: "identity_attribute", Span: "性别"},
		},
		AttackDomainCandidates: []dto.SafetyReviewCandidate{
			{Category: "other_discrimination", EvidenceIDs: []string{"F1"}, Reason: "召回"},
			{Category: "gender_discrimination", EvidenceIDs: []string{"F1"}, Reason: "召回"},
		},
	}

	plan, err := service.BuildSafetyReviewRoutingPlan(policy, "prompt", route)
	if err != nil {
		t.Fatalf("BuildSafetyReviewRoutingPlan() error = %v", err)
	}
	if len(plan.Route.AttackDomainCandidates) != 1 ||
		plan.Route.AttackDomainCandidates[0].Category != "gender_discrimination" {
		t.Fatalf("candidates = %+v, want gender only", plan.Route.AttackDomainCandidates)
	}
}

// TestSafetyReviewRouterRequestIsBlind 验证 Router 请求不包含裁判输出、完整规则卡或原始标注。
func TestSafetyReviewRouterRequestIsBlind(t *testing.T) {
	policy := loadSafetyReviewPolicyForRouting(t)
	work := dao.SafetyReviewStageWork{
		TaskID:   "task-1",
		TraceID:  "trace-1",
		StageKey: "router",
		Role:     "router",
		Scene:    "prompt",
		Prompt:   "router-prompt",
		RawJSON:  []byte(`{"label":"unsafe","explanation":"原始解释","annotation":{"old":true}}`),
	}

	request, err := service.BuildSafetyReviewRouterRequest(work, policy)
	if err != nil {
		t.Fatalf("BuildSafetyReviewRouterRequest() error = %v", err)
	}
	if len(request.Messages) != 2 {
		t.Fatalf("message count = %d, want 2", len(request.Messages))
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(request.Messages[1].Content), &payload); err != nil {
		t.Fatalf("router user payload is not JSON: %v", err)
	}
	wantKeys := []string{"trace_id", "scene", "prompt", "policy", "schema"}
	if len(payload) != len(wantKeys) {
		t.Fatalf("payload keys = %d, want %d", len(payload), len(wantKeys))
	}
	for _, key := range wantKeys {
		if _, ok := payload[key]; !ok {
			t.Fatalf("router payload lacks %s", key)
		}
	}
	forbidden := []string{
		"judge_a", "judge_b", "prior_outputs", "rule_card", "original_label",
		"explanation", "annotation", "required_conditions", "target_attributes",
	}
	combined := request.Messages[0].Content + request.Messages[1].Content
	for _, word := range forbidden {
		if strings.Contains(combined, word) {
			t.Fatalf("router request contains forbidden field %q", word)
		}
	}
}

// fourSafetyReviewCandidates 构造指定轴的四个合法候选。
func fourSafetyReviewCandidates(axis string) []dto.SafetyReviewCandidate {
	categories := []string{"ethnic_discrimination", "gender_discrimination", "health_discrimination"}
	if axis == "attack_method" {
		categories = []string{"prompt_injection", "jailbreak", "encoding_obfuscation"}
	}
	candidates := make([]dto.SafetyReviewCandidate, 0, 4)
	for _, category := range categories {
		candidates = append(candidates, dto.SafetyReviewCandidate{Category: category, Reason: "召回"})
	}
	candidates = append(candidates, dto.SafetyReviewCandidate{
		Category: "age_discrimination", Reason: "召回",
	})
	if axis == "attack_method" {
		candidates[3].Category = "cross_language_attack"
	}
	return candidates
}

// assertCandidateOrder 验证候选按类别稳定排序。
func assertCandidateOrder(t *testing.T, candidates []dto.SafetyReviewCandidate) {
	t.Helper()
	for index := 1; index < len(candidates); index++ {
		if candidates[index-1].Category > candidates[index].Category {
			t.Fatalf("candidates are not sorted: %+v", candidates)
		}
	}
}

// loadSafetyReviewPolicyForRouting 只加载一次冻结发布包供路由测试使用。
func loadSafetyReviewPolicyForRouting(t *testing.T) *service.SafetyReviewPolicy {
	t.Helper()
	safetyReviewRoutingPolicyOnce.Do(func() {
		safetyReviewRoutingPolicy, safetyReviewRoutingPolicyError = service.LoadSafetyReviewPolicy(
			"../../policy/releases/p04b-v1.0",
		)
	})
	if safetyReviewRoutingPolicyError != nil {
		t.Fatalf("LoadSafetyReviewPolicy() error = %v", safetyReviewRoutingPolicyError)
	}
	return safetyReviewRoutingPolicy
}
