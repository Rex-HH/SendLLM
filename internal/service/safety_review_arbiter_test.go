package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"sendllm/internal/dao"
	"sendllm/internal/dto"
	"sendllm/internal/service"
)

// TestSafetyReviewDecisionPrimarySelection 验证 primary 只能来自 established Expert
// 并按冻结优先级与类别 ID 排序。
func TestSafetyReviewDecisionPrimarySelection(t *testing.T) {
	policy := loadSafetyReviewPolicyForRouting(t)
	validator, err := service.NewSafetyReviewValidator(policy)
	if err != nil {
		t.Fatalf("NewSafetyReviewValidator() error = %v", err)
	}
	tests := []struct {
		name    string
		scene   string
		experts []dto.SafetyReviewExpertResult
		want    string
	}{
		{
			name:  "specific over other",
			scene: "prompt",
			experts: []dto.SafetyReviewExpertResult{
				establishedArbiterExpert("attack_domain", "other_discrimination", "prompt"),
				establishedArbiterExpert("attack_domain", "ethnic_discrimination", "prompt"),
			},
			want: "ethnic_discrimination",
		},
		{
			name:  "ethnic hatred threshold",
			scene: "prompt",
			experts: []dto.SafetyReviewExpertResult{
				establishedArbiterExpert("attack_domain", "ethnic_discrimination", "prompt"),
				establishedArbiterExpert("attack_domain", "ethnic_hatred", "prompt"),
			},
			want: "ethnic_hatred",
		},
		{
			name:  "fixed method priority",
			scene: "prompt",
			experts: []dto.SafetyReviewExpertResult{
				establishedArbiterExpert("attack_method", "jailbreak", "prompt"),
				establishedArbiterExpert("attack_method", "prompt_injection", "prompt"),
			},
			want: "prompt_injection",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := arbiterDecisionForExperts(test.scene, test.experts)
			primary := "other_discrimination"
			if test.want == "ethnic_hatred" {
				primary = "ethnic_discrimination"
			}
			if test.want == "prompt_injection" {
				primary = "jailbreak"
			}
			decision.PrimaryAttackDomain = primary
			decision.PrimaryRiskType = primary
			if test.want == "prompt_injection" {
				decision.PrimaryAttackMethod = primary
				decision.PrimaryAttackDomain = ""
			}
			raw, marshalErr := marshalArbiterDecision(decision)
			if marshalErr != nil {
				t.Fatalf("marshal decision: %v", marshalErr)
			}
			got, validateErr := validator.ValidateDecision(test.scene, test.experts, raw)
			if validateErr != nil {
				t.Fatalf("ValidateDecision() error = %v", validateErr)
			}
			if got.PrimaryAttackMethod != test.want && got.PrimaryAttackDomain != test.want {
				t.Fatalf("normalized primary = %+v, want %q", got, test.want)
			}
		})
	}
}

// TestSafetyReviewDecisionPrimaryTieUsesLexicalOrder 验证相同优先级时按类别 ID 字典序选择。
func TestSafetyReviewDecisionPrimaryTieUsesLexicalOrder(t *testing.T) {
	policy, err := service.LoadSafetyReviewPolicy("../../policy/releases/p04b-v1.0")
	if err != nil {
		t.Fatalf("LoadSafetyReviewPolicy() error = %v", err)
	}
	policy.Cards["ethnic_discrimination"].PrimaryPriority = 100
	policy.Cards["religious_discrimination"].PrimaryPriority = 100
	validator, err := service.NewSafetyReviewValidator(policy)
	if err != nil {
		t.Fatalf("NewSafetyReviewValidator() error = %v", err)
	}
	experts := []dto.SafetyReviewExpertResult{
		establishedArbiterExpert("attack_domain", "religious_discrimination", "prompt"),
		establishedArbiterExpert("attack_domain", "ethnic_discrimination", "prompt"),
	}
	decision := arbiterDecisionForExperts("prompt", experts)
	decision.PrimaryAttackDomain = "religious_discrimination"
	decision.PrimaryRiskType = "religious_discrimination"
	raw, err := marshalArbiterDecision(decision)
	if err != nil {
		t.Fatalf("marshal decision: %v", err)
	}
	got, err := validator.ValidateDecision("prompt", experts, raw)
	if err != nil {
		t.Fatalf("ValidateDecision(tie) error = %v", err)
	}
	if got.PrimaryAttackDomain != "ethnic_discrimination" {
		t.Fatalf("normalized tie primary = %q, want ethnic_discrimination", got.PrimaryAttackDomain)
	}
}

// TestSafetyReviewDecisionLabelMatrixAndNoMajority 验证标签矩阵和缺少 Expert 时不能 Unsafe。
func TestSafetyReviewDecisionLabelMatrixAndNoMajority(t *testing.T) {
	validator := newArbiterDecisionValidator(t)
	tests := []struct {
		name     string
		scene    string
		experts  []dto.SafetyReviewExpertResult
		decision dto.SafetyReviewDecision
		wantErr  bool
	}{
		{
			name:  "prompt method and domain unsafe",
			scene: "prompt",
			experts: []dto.SafetyReviewExpertResult{
				establishedArbiterExpert("attack_method", "prompt_injection", "prompt"),
				establishedArbiterExpert("attack_domain", "ethnic_discrimination", "prompt"),
			},
		},
		{
			name:  "prompt domain only unsafe",
			scene: "prompt",
			experts: []dto.SafetyReviewExpertResult{
				establishedArbiterExpert("attack_domain", "ethnic_discrimination", "prompt"),
			},
		},
		{
			name:  "response domain only unsafe",
			scene: "response",
			experts: []dto.SafetyReviewExpertResult{
				establishedArbiterExpert("attack_domain", "ethnic_discrimination", "response_explicit"),
			},
		},
		{
			name:     "no established category cannot unsafe",
			scene:    "prompt",
			experts:  []dto.SafetyReviewExpertResult{uncertainArbiterExpert("attack_domain", "ethnic_discrimination")},
			decision: safeArbiterDecision(),
			wantErr:  false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := test.decision
			if decision.Verdict == "" {
				decision = arbiterDecisionForExperts(test.scene, test.experts)
			}
			if test.name == "no established category cannot unsafe" {
				decision.Label = "unsafe"
				test.wantErr = true
			}
			raw, marshalErr := marshalArbiterDecision(decision)
			if marshalErr != nil {
				t.Fatalf("marshal decision: %v", marshalErr)
			}
			_, validateErr := validator.ValidateDecision(test.scene, test.experts, raw)
			if test.wantErr && validateErr == nil {
				t.Fatal("ValidateDecision() accepted invalid label matrix")
			}
			if !test.wantErr && validateErr != nil {
				t.Fatalf("ValidateDecision() error = %v", validateErr)
			}
		})
	}
}

// TestSafetyReviewDecisionUncertaintyIsDeferredToProjection 验证本地校验保留 Safe 原文，
// 由最终投影层根据 uncertain Expert 强制隔离。
func TestSafetyReviewDecisionUncertaintyIsDeferredToProjection(t *testing.T) {
	validator := newArbiterDecisionValidator(t)
	experts := []dto.SafetyReviewExpertResult{uncertainArbiterExpert("attack_domain", "ethnic_discrimination")}
	decision := safeArbiterDecision()
	raw, err := marshalArbiterDecision(decision)
	if err != nil {
		t.Fatalf("marshal decision: %v", err)
	}
	if _, err := validator.ValidateDecision("prompt", experts, raw); err != nil {
		t.Fatalf("ValidateDecision() error = %v", err)
	}
}

// TestSafetyReviewDecisionDefendsConflictsAndUpgrades 验证类别冲突和 uncertain 不能被升级。
func TestSafetyReviewDecisionDefendsConflictsAndUpgrades(t *testing.T) {
	validator := newArbiterDecisionValidator(t)
	experts := []dto.SafetyReviewExpertResult{
		establishedArbiterExpert("attack_domain", "ethnic_discrimination", "prompt"),
		establishedArbiterExpert("attack_domain", "religious_discrimination", "prompt"),
	}
	decision := arbiterDecisionForExperts("prompt", experts)
	decision.AttackDomains = []string{"ethnic_discrimination"}
	raw, err := marshalArbiterDecision(decision)
	if err != nil {
		t.Fatalf("marshal decision: %v", err)
	}
	got, err := validator.ValidateDecision("prompt", experts, raw)
	if err != nil {
		t.Fatalf("ValidateDecision(conflict) error = %v", err)
	}
	if len(got.AttackDomains) != 2 {
		t.Fatalf("normalized attack domains = %#v, want two established categories", got.AttackDomains)
	}

	uncertain := []dto.SafetyReviewExpertResult{uncertainArbiterExpert("attack_domain", "ethnic_discrimination")}
	upgraded := arbiterDecisionForExperts("prompt", uncertain)
	upgraded.AttackDomains = []string{"ethnic_discrimination"}
	upgraded.PrimaryAttackDomain = "ethnic_discrimination"
	upgraded.PrimaryRiskType = "ethnic_discrimination"
	raw, err = marshalArbiterDecision(upgraded)
	if err != nil {
		t.Fatalf("marshal upgraded decision: %v", err)
	}
	_, err = validator.ValidateDecision("prompt", uncertain, raw)
	requireInvalidSafetyReviewResult(t, err)

	responseExperts := []dto.SafetyReviewExpertResult{
		establishedArbiterExpert("attack_domain", "ethnic_discrimination", "response_explicit"),
	}
	responseDecision := arbiterDecisionForExperts("response", responseExperts)
	responseDecision.AttackMethods = []string{"prompt_injection"}
	responseDecision.PrimaryAttackMethod = "prompt_injection"
	responseDecision.IsAttack = true
	raw, err = marshalArbiterDecision(responseDecision)
	if err != nil {
		t.Fatalf("marshal response decision: %v", err)
	}
	normalizedResponse, err := validator.ValidateDecision("response", responseExperts, raw)
	if err != nil {
		t.Fatalf("ValidateDecision(response method injection) error = %v", err)
	}
	if len(normalizedResponse.AttackMethods) != 0 || normalizedResponse.IsAttack {
		t.Fatalf("normalized response methods = %#v/%v", normalizedResponse.AttackMethods, normalizedResponse.IsAttack)
	}
}

// TestSafetyReviewDecisionResponseEvidenceOwnership 验证 Response Unsafe 不能只依赖 prompt_only。
func TestSafetyReviewDecisionResponseEvidenceOwnership(t *testing.T) {
	validator := newArbiterDecisionValidator(t)
	experts := []dto.SafetyReviewExpertResult{
		establishedArbiterExpert("attack_domain", "ethnic_discrimination", "response_explicit"),
	}
	decision := arbiterDecisionForExperts("response", experts)
	decision.EvidenceBasis = []dto.SafetyReviewEvidence{
		{Source: "prompt_only", Kind: "group_target", Span: "合成提示证据。"},
	}
	raw, err := marshalArbiterDecision(decision)
	if err != nil {
		t.Fatalf("marshal decision: %v", err)
	}
	_, err = validator.ValidateDecision("response", experts, raw)
	requireInvalidSafetyReviewResult(t, err)
}

// TestSafetyReviewDecisionResponseMethodProhibited 验证 Response 决策不能包含 attack method。
func TestSafetyReviewDecisionResponseMethodProhibited(t *testing.T) {
	policy, err := service.LoadSafetyReviewPolicy("../../policy/releases/p04b-v1.0")
	if err != nil {
		t.Fatalf("LoadSafetyReviewPolicy() error = %v", err)
	}
	card := policy.Cards["prompt_injection"]
	card.EnabledScenes = []string{"prompt", "response"}
	validator, err := service.NewSafetyReviewValidator(policy)
	if err != nil {
		t.Fatalf("NewSafetyReviewValidator() error = %v", err)
	}
	experts := []dto.SafetyReviewExpertResult{
		establishedArbiterExpert("attack_method", "prompt_injection", "response_explicit"),
		establishedArbiterExpert("attack_domain", "ethnic_discrimination", "response_explicit"),
	}
	decision := arbiterDecisionForExperts("response", experts)
	raw, err := marshalArbiterDecision(decision)
	if err != nil {
		t.Fatalf("marshal decision: %v", err)
	}
	_, err = validator.ValidateDecision("response", experts, raw)
	requireInvalidSafetyReviewResult(t, err)
}

// TestSafetyReviewDecisionSafeMustClearCategories 验证 Safe 决策必须清空全部风险字段。
func TestSafetyReviewDecisionSafeMustClearCategories(t *testing.T) {
	validator := newArbiterDecisionValidator(t)
	experts := []dto.SafetyReviewExpertResult{
		establishedArbiterExpert("attack_domain", "ethnic_discrimination", "response_explicit"),
	}
	decision := arbiterDecisionForExperts("response", experts)
	decision.Label = "safe"
	raw, err := marshalArbiterDecision(decision)
	if err != nil {
		t.Fatalf("marshal decision: %v", err)
	}
	_, err = validator.ValidateDecision("response", experts, raw)
	requireInvalidSafetyReviewResult(t, err)
}

// TestSafetyReviewDecisionFormalCaseTypes 验证 formal case_type 与 label 的兼容关系。
func TestSafetyReviewDecisionFormalCaseTypes(t *testing.T) {
	validator := newArbiterDecisionValidator(t)
	tests := []struct {
		name     string
		label    string
		caseType string
		wantErr  bool
	}{
		{name: "safe typical", label: "safe", caseType: "typical"},
		{name: "safe borderline", label: "safe", caseType: "borderline"},
		{name: "safe variant", label: "safe", caseType: "variant", wantErr: true},
		{name: "unsafe typical", label: "unsafe", caseType: "typical"},
		{name: "unsafe variant", label: "unsafe", caseType: "variant"},
		{name: "unsafe hard negative", label: "unsafe", caseType: "hard_negative", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			expert := establishedArbiterExpert("attack_domain", "ethnic_discrimination", "response_explicit")
			if test.label == "safe" {
				expert = notEstablishedArbiterExpert("attack_domain", "ethnic_discrimination")
			}
			experts := []dto.SafetyReviewExpertResult{expert}
			decision := arbiterDecisionForExperts("response", experts)
			decision.Label = test.label
			decision.CaseType = test.caseType
			if test.label == "safe" {
				clearArbiterRiskFields(&decision)
			}
			raw, err := marshalArbiterDecision(decision)
			if err != nil {
				t.Fatalf("marshal decision: %v", err)
			}
			_, validateErr := validator.ValidateDecision("response", experts, raw)
			if test.wantErr && validateErr == nil {
				t.Fatalf("ValidateDecision() accepted invalid case type %s", test.caseType)
			}
			if !test.wantErr && validateErr != nil {
				t.Fatalf("ValidateDecision(%s) error = %v", test.caseType, validateErr)
			}
		})
	}
}

// TestSafetyReviewDecisionQuarantineReasonClosedSet 验证全部隔离原因和非法原因。
func TestSafetyReviewDecisionQuarantineReasonClosedSet(t *testing.T) {
	validator := newArbiterDecisionValidator(t)
	experts := []dto.SafetyReviewExpertResult{uncertainArbiterExpert("attack_domain", "ethnic_discrimination")}
	for _, reason := range []string{
		"irreducible_uncertainty", "incomplete_context", "policy_coverage_gap",
		"model_stage_exhausted", "independence_degraded_unresolved", "category_conflict",
	} {
		t.Run(reason, func(t *testing.T) {
			decision := quarantineArbiterDecision(reason)
			raw, err := marshalArbiterDecision(decision)
			if err != nil {
				t.Fatalf("marshal decision: %v", err)
			}
			if _, err := validator.ValidateDecision("prompt", experts, raw); err != nil {
				t.Fatalf("ValidateDecision(%s) error = %v", reason, err)
			}
		})
	}
	decision := quarantineArbiterDecision("unknown")
	raw, err := marshalArbiterDecision(decision)
	if err != nil {
		t.Fatalf("marshal decision: %v", err)
	}
	_, err = validator.ValidateDecision("prompt", experts, raw)
	requireInvalidSafetyReviewResult(t, err)
}

// TestSafetyReviewDecisionRejectsRiskLevel 验证 P04-B V1 决策不允许 risk_level。
func TestSafetyReviewDecisionRejectsRiskLevel(t *testing.T) {
	validator := newArbiterDecisionValidator(t)
	experts := []dto.SafetyReviewExpertResult{
		establishedArbiterExpert("attack_domain", "ethnic_discrimination", "response_explicit"),
	}
	raw := []byte(`{
		"verdict":"resolved","label":"unsafe","is_attack":false,
		"attack_methods":[],"attack_domains":["ethnic_discrimination"],
		"primary_attack_method":"","primary_attack_domain":"ethnic_discrimination",
		"primary_risk_type":"ethnic_discrimination","case_type":"typical",
		"evidence_basis":[{"source":"response_explicit","kind":"negative_description","span":"合成证据。"}],
		"decision_rules":["D-HARMFUL-ACT"],"quarantine_reason":"","rationale":"说明。",
		"risk_level":"high"
	}`)
	_, err := validator.ValidateDecision("response", experts, raw)
	requireInvalidSafetyReviewResult(t, err)
}

// TestSafetyReviewArbiterSchedulerWritesDecision 验证只有 Arbiter 成功阶段才写最终决策。
func TestSafetyReviewArbiterSchedulerWritesDecision(t *testing.T) {
	store := arbiterSchedulerStore(t, "irreducible_uncertainty")
	caller := newSafetyReviewFakeCaller(
		func(_ context.Context, req service.SafetyReviewCallRequest) (service.SafetyReviewCallResult, error) {
			return service.SafetyReviewCallResult{Content: quarantineDecisionJSON("irreducible_uncertainty")}, nil
		},
	)
	runner, err := service.NewSafetyReviewRunner(service.SafetyReviewRunnerConfig{
		TaskID: "task-1", Store: store, Caller: caller, Quota: fakeSafetyReviewQuota{},
		Workers: map[dto.SafetyReviewRole]int{
			dto.SafetyReviewJudgeA: 1, dto.SafetyReviewJudgeB: 1,
			dto.SafetyReviewRouter: 1, dto.SafetyReviewExpert: 1,
			dto.SafetyReviewArbiter: 1,
		},
		Policy: loadSafetyReviewPolicyForRouting(t), ModelProfile: "model", ModelFamily: "family",
		APIKeyEnv: "TEST_KEY", ShutdownTimeout: time.Second, Now: time.Now,
		BuildRequest: func(work dao.SafetyReviewStageWork) (service.SafetyReviewCallRequest, error) {
			return service.SafetyReviewCallRequest{
				TaskID: work.TaskID, TraceID: work.TraceID, StageKey: work.StageKey,
				Role: dto.SafetyReviewRole(work.Role), Messages: []dto.Message{{Role: "user", Content: "fixed"}},
				Schema: []byte(`{}`), Mode: "json_object",
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewSafetyReviewRunner() error = %v", err)
	}
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := store.items["trace-1"].state; got != "quarantined" {
		t.Fatalf("item state = %q, want quarantined", got)
	}
	if len(store.completed) == 0 || store.completed[len(store.completed)-1].Decision == nil {
		t.Fatal("Arbiter success did not persist a final decision")
	}
}

// TestSafetyReviewArbiterTerminalFailureQuarantines 验证 Arbiter 失败直接隔离且不再调用模型。
func TestSafetyReviewArbiterTerminalFailureQuarantines(t *testing.T) {
	store := arbiterSchedulerStore(t, "model_stage_exhausted")
	caller := newSafetyReviewFakeCaller(
		func(_ context.Context, req service.SafetyReviewCallRequest) (service.SafetyReviewCallResult, error) {
			if req.Role == dto.SafetyReviewArbiter {
				return service.SafetyReviewCallResult{}, errors.New("model exhausted")
			}
			return service.SafetyReviewCallResult{Content: quarantineDecisionJSON("irreducible_uncertainty")}, nil
		},
	)
	runner, err := service.NewSafetyReviewRunner(service.SafetyReviewRunnerConfig{
		TaskID: "task-1", Store: store, Caller: caller, Quota: fakeSafetyReviewQuota{},
		Workers: map[dto.SafetyReviewRole]int{
			dto.SafetyReviewJudgeA: 1, dto.SafetyReviewJudgeB: 1,
			dto.SafetyReviewRouter: 1, dto.SafetyReviewExpert: 1,
			dto.SafetyReviewArbiter: 1,
		},
		Policy: loadSafetyReviewPolicyForRouting(t), ModelProfile: "model", ModelFamily: "family",
		APIKeyEnv: "TEST_KEY", ShutdownTimeout: time.Second, Now: time.Now,
		BuildRequest: func(work dao.SafetyReviewStageWork) (service.SafetyReviewCallRequest, error) {
			return service.SafetyReviewCallRequest{
				TaskID: work.TaskID, TraceID: work.TraceID, StageKey: work.StageKey,
				Role: dto.SafetyReviewRole(work.Role), Messages: []dto.Message{{Role: "user", Content: "fixed"}},
				Schema: []byte(`{}`), Mode: "json_object",
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewSafetyReviewRunner() error = %v", err)
	}
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := store.items["trace-1"].state; got != "quarantined" {
		t.Fatalf("item state = %q, want quarantined", got)
	}
	decision := store.completed[len(store.completed)-1].Decision
	if decision == nil || decision.QuarantineReason != "model_stage_exhausted" {
		t.Fatalf("decision = %+v, want model_stage_exhausted quarantine", decision)
	}
	if len(caller.calls) != 1 {
		t.Fatalf("call count = %d, want one Arbiter call", len(caller.calls))
	}
}

// TestSafetyReviewArbiterPriorOutputsAreStructured 验证先验输出是固定字段的 JSON 对象。
func TestSafetyReviewArbiterPriorOutputsAreStructured(t *testing.T) {
	prior := service.SafetyReviewArbiterPriorOutputs{
		JudgeA: dto.SafetyReviewJudgment{Verdict: "safe", Rationale: "合成 A 结果。"},
		JudgeB: dto.SafetyReviewJudgment{Verdict: "safe", Rationale: "合成 B 结果。"},
		Router: dto.SafetyReviewRoute{CoverageComplete: true},
		Experts: []dto.SafetyReviewExpertResult{
			notEstablishedArbiterExpert("attack_domain", "ethnic_discrimination"),
		},
		TerminalFailures:     []string{},
		FallbackUsed:         false,
		IndependenceDegraded: false,
	}
	raw, err := service.BuildSafetyReviewArbiterPriorOutputs(prior)
	if err != nil {
		t.Fatalf("BuildSafetyReviewArbiterPriorOutputs() error = %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("prior outputs are not JSON: %v", err)
	}
	want := []string{
		"judge_a", "judge_b", "router", "experts", "terminal_failures",
		"fallback_used", "independence_degraded",
	}
	if len(fields) != len(want) {
		t.Fatalf("prior output field count = %d, want %d", len(fields), len(want))
	}
	for _, name := range want {
		if _, ok := fields[name]; !ok {
			t.Fatalf("prior outputs lack %s", name)
		}
	}
}

// TestSafetyReviewArbiterRequestIsBlind 验证 Arbiter 请求只包含原始条目和结构化先验输出。
func TestSafetyReviewArbiterRequestIsBlind(t *testing.T) {
	policy := loadSafetyReviewPolicyForRouting(t)
	work := dao.SafetyReviewStageWork{
		TaskID: "task-1", TraceID: "trace-1", StageKey: "arbiter", Role: "arbiter",
		Scene: "response", Prompt: "arbiter-prompt", Response: "arbiter-response",
		RawJSON: []byte(`{"label":"unsafe","explanation":"原始解释","annotation":{"old":true},` +
			`"raw_model_output":"LEAKED_RAW_OUTPUT"}`),
	}
	prior := arbiterPriorOutputs()
	request, err := service.BuildSafetyReviewArbiterRequest(work, prior, policy)
	if err != nil {
		t.Fatalf("BuildSafetyReviewArbiterRequest() error = %v", err)
	}
	if len(request.Messages) != 2 {
		t.Fatalf("message count = %d, want 2", len(request.Messages))
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(request.Messages[1].Content), &payload); err != nil {
		t.Fatalf("arbiter user payload is not JSON: %v", err)
	}
	want := []string{
		"trace_id", "scene", "prompt", "response", "policy", "schema", "prior_outputs",
	}
	if len(payload) != len(want) {
		t.Fatalf("payload field count = %d, want %d", len(payload), len(want))
	}
	for _, name := range want {
		if _, ok := payload[name]; !ok {
			t.Fatalf("arbiter payload lacks %s", name)
		}
	}
	for _, marker := range []string{"原始解释", "annotation", "LEAKED_RAW_OUTPUT"} {
		if strings.Contains(request.Messages[1].Content, marker) {
			t.Fatalf("arbiter request contains forbidden prior data %q", marker)
		}
	}
}

// TestSafetyReviewArbiterRequestRejectsInvalidStage 验证 Arbiter 阶段身份必须固定。
func TestSafetyReviewArbiterRequestRejectsInvalidStage(t *testing.T) {
	work := dao.SafetyReviewStageWork{
		TaskID: "task-1", TraceID: "trace-1", StageKey: "expert", Role: "arbiter", Scene: "prompt",
	}
	_, err := service.BuildSafetyReviewArbiterRequest(work, arbiterPriorOutputs(), loadSafetyReviewPolicyForRouting(t))
	if err == nil {
		t.Fatal("BuildSafetyReviewArbiterRequest() accepted invalid stage")
	}
}

// TestSafetyReviewArbiterProjectionWritesDecisionRecord 验证合法决策会投影为 DAO 决策记录。
func TestSafetyReviewArbiterProjectionWritesDecisionRecord(t *testing.T) {
	policy := loadSafetyReviewPolicyForRouting(t)
	prior := arbiterPriorOutputs()
	prior.Experts = []dto.SafetyReviewExpertResult{
		establishedArbiterExpert("attack_domain", "ethnic_discrimination", "response_explicit"),
	}
	decision := arbiterDecisionForExperts("response", prior.Experts)
	raw, err := marshalArbiterDecision(decision)
	if err != nil {
		t.Fatalf("marshal decision: %v", err)
	}
	decidedAt := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
	record, err := service.ProjectSafetyReviewDecision(policy, "response", prior, raw, decidedAt)
	if err != nil {
		t.Fatalf("ProjectSafetyReviewDecision() error = %v", err)
	}
	if record.FinalState != "resolved_unsafe" || record.Label != "unsafe" || record.IsAttack {
		t.Fatalf("record = %+v, want resolved unsafe non-attack", record)
	}
	if record.PrimaryAttackDomain != "ethnic_discrimination" ||
		record.PrimaryRiskType != "ethnic_discrimination" || record.PrimaryAttackMethod != "" {
		t.Fatalf("record primary fields = %+v", record)
	}
	if record.CaseType != "typical" || record.QuarantineReason != "" ||
		!record.DecidedAt.Equal(decidedAt) {
		t.Fatalf("record metadata = %+v", record)
	}
}

// TestSafetyReviewArbiterProjectionDefendsSafePrerequisites 验证未决、失败和 Router 覆盖不完整时
// 不能 Safe。
func TestSafetyReviewArbiterProjectionDefendsSafePrerequisites(t *testing.T) {
	policy := loadSafetyReviewPolicyForRouting(t)
	tests := []struct {
		name  string
		prior service.SafetyReviewArbiterPriorOutputs
	}{
		{
			name: "uncertain expert",
			prior: arbiterPriorWithExperts([]dto.SafetyReviewExpertResult{
				uncertainArbiterExpert("attack_domain", "ethnic_discrimination"),
			}),
		},
		{
			name: "terminal expert failure",
			prior: arbiterPriorWithExperts([]dto.SafetyReviewExpertResult{
				notEstablishedArbiterExpert("attack_domain", "ethnic_discrimination"),
			}),
		},
		{
			name:  "incomplete router coverage",
			prior: arbiterPriorWithRouter(false),
		},
	}
	for index := range tests {
		if tests[index].name == "terminal expert failure" {
			tests[index].prior.TerminalFailures = []string{"expert:attack_domain:ethnic_discrimination"}
		}
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := safeArbiterDecision()
			decision.EvidenceBasis[0].Source = "response_explicit"
			raw, err := marshalArbiterDecision(decision)
			if err != nil {
				t.Fatalf("marshal decision: %v", err)
			}
			record, err := service.ProjectSafetyReviewDecision(policy, "response", test.prior, raw, time.Now())
			if err != nil {
				t.Fatalf("ProjectSafetyReviewDecision() error = %v", err)
			}
			if record.FinalState != "quarantined" || record.Label != "" {
				t.Fatalf("record = %+v, want quarantined without label for %s", record, test.name)
			}
		})
	}
}

// TestSafetyReviewArbiterProjectionQuarantinesJudgeUnsafeConflict 验证 Judge 明确 Unsafe
// 而 Expert 未建立类别时不能解析成 Safe。
func TestSafetyReviewArbiterProjectionQuarantinesJudgeUnsafeConflict(t *testing.T) {
	policy := loadSafetyReviewPolicyForRouting(t)
	tests := []struct {
		name          string
		judgeAVerdict string
		judgeBVerdict string
	}{
		{name: "judge a unsafe", judgeAVerdict: "unsafe", judgeBVerdict: "safe"},
		{name: "judge b unsafe", judgeAVerdict: "safe", judgeBVerdict: "unsafe"},
		{name: "both judges unsafe", judgeAVerdict: "unsafe", judgeBVerdict: "unsafe"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			prior := arbiterPriorOutputs()
			prior.JudgeA.Verdict = test.judgeAVerdict
			prior.JudgeB.Verdict = test.judgeBVerdict
			decision := safeArbiterDecision()
			decision.EvidenceBasis[0].Source = "response_explicit"
			raw, err := marshalArbiterDecision(decision)
			if err != nil {
				t.Fatalf("marshal decision: %v", err)
			}
			record, err := service.ProjectSafetyReviewDecision(policy, "response", prior, raw, time.Now())
			if err != nil {
				t.Fatalf("ProjectSafetyReviewDecision() error = %v", err)
			}
			if record.FinalState != "quarantined" || record.Label != "" {
				t.Fatalf("record = %+v, want quarantined without label", record)
			}
		})
	}
}

// TestSafetyReviewArbiterProjectionAllowsClearNotEstablishedSafe 验证明确排除时可以 Safe。
func TestSafetyReviewArbiterProjectionAllowsClearNotEstablishedSafe(t *testing.T) {
	policy := loadSafetyReviewPolicyForRouting(t)
	prior := arbiterPriorWithExperts([]dto.SafetyReviewExpertResult{
		notEstablishedArbiterExpert("attack_domain", "ethnic_discrimination"),
	})
	decision := safeArbiterDecision()
	decision.EvidenceBasis[0].Source = "response_explicit"
	raw, err := marshalArbiterDecision(decision)
	if err != nil {
		t.Fatalf("marshal decision: %v", err)
	}
	if _, err := service.ProjectSafetyReviewDecision(policy, "response", prior, raw, time.Now()); err != nil {
		t.Fatalf("ProjectSafetyReviewDecision(clear safe) error = %v", err)
	}
}

// newArbiterDecisionValidator 构造 Arbiter 决策验证器。
func newArbiterDecisionValidator(t *testing.T) *service.SafetyReviewValidator {
	t.Helper()
	validator, err := service.NewSafetyReviewValidator(loadSafetyReviewPolicyForRouting(t))
	if err != nil {
		t.Fatalf("NewSafetyReviewValidator() error = %v", err)
	}
	return validator
}

// establishedArbiterExpert 构造已建立的合成 Expert 结果。
func establishedArbiterExpert(axis, category, source string) dto.SafetyReviewExpertResult {
	return dto.SafetyReviewExpertResult{
		Axis: axis, Category: category, Verdict: "established",
		Evidence: []dto.SafetyReviewEvidence{
			{Source: source, Kind: "negative_description", Span: "合成证据。"},
		},
	}
}

// uncertainArbiterExpert 构造未决的合成 Expert 结果。
func uncertainArbiterExpert(axis, category string) dto.SafetyReviewExpertResult {
	return dto.SafetyReviewExpertResult{
		Axis: axis, Category: category, Verdict: "uncertain",
		Evidence: []dto.SafetyReviewEvidence{},
	}
}

// notEstablishedArbiterExpert 构造因关键条件不满足而未建立的 Expert。
func notEstablishedArbiterExpert(axis, category string) dto.SafetyReviewExpertResult {
	return dto.SafetyReviewExpertResult{
		Axis: axis, Category: category, Verdict: "not_established",
		Evidence: []dto.SafetyReviewEvidence{
			{Source: "response_explicit", Kind: "refusal", Span: "合成安全证据。"},
		},
	}
}

// arbiterDecisionForExperts 根据 established Expert 构造 unsafe 决策。
func arbiterDecisionForExperts(scene string, experts []dto.SafetyReviewExpertResult) dto.SafetyReviewDecision {
	decision := dto.SafetyReviewDecision{
		Verdict: "resolved", Label: "unsafe", CaseType: "typical",
		DecisionRules: []string{"DISCRIMINATION-R01"}, Rationale: "合成决策。",
	}
	for _, expert := range experts {
		if expert.Verdict != "established" {
			continue
		}
		if expert.Axis == "attack_method" {
			decision.AttackMethods = append(decision.AttackMethods, expert.Category)
			continue
		}
		decision.AttackDomains = append(decision.AttackDomains, expert.Category)
	}
	decision.AttackMethods = sortArbiterStrings(decision.AttackMethods)
	decision.AttackDomains = sortArbiterStrings(decision.AttackDomains)
	if len(decision.AttackMethods) != 0 {
		decision.PrimaryAttackMethod = decision.AttackMethods[0]
		decision.IsAttack = true
	}
	if len(decision.AttackDomains) != 0 {
		decision.PrimaryAttackDomain = decision.AttackDomains[0]
	}
	decision.PrimaryRiskType = decision.PrimaryAttackDomain
	if decision.PrimaryRiskType == "" {
		decision.PrimaryRiskType = decision.PrimaryAttackMethod
	}
	source := "prompt"
	if scene == "response" {
		source = "response_explicit"
	}
	decision.EvidenceBasis = []dto.SafetyReviewEvidence{
		{Source: source, Kind: "negative_description", Span: "合成证据。"},
	}
	return decision
}

// safeArbiterDecision 构造安全决策。
func safeArbiterDecision() dto.SafetyReviewDecision {
	return dto.SafetyReviewDecision{
		Verdict: "resolved", Label: "safe", CaseType: "typical",
		AttackMethods: []string{}, AttackDomains: []string{},
		EvidenceBasis: []dto.SafetyReviewEvidence{
			{Source: "prompt", Kind: "refusal", Span: "合成安全证据。"},
		},
		DecisionRules: []string{"DISCRIMINATION-R01"}, Rationale: "合成安全决策。",
	}
}

// quarantineArbiterDecision 构造指定原因的隔离决策。
func quarantineArbiterDecision(reason string) dto.SafetyReviewDecision {
	return dto.SafetyReviewDecision{
		Verdict: "quarantine", IsAttack: false, AttackMethods: []string{}, AttackDomains: []string{},
		EvidenceBasis: []dto.SafetyReviewEvidence{},
		DecisionRules: []string{"DISCRIMINATION-R01"}, QuarantineReason: reason,
		Rationale: "合成隔离决策。",
	}
}

// arbiterDecisionWire 表示测试序列化时保留空 label 与 quarantine_reason 的决策契约。
type arbiterDecisionWire struct {
	Verdict             string                     `json:"verdict"`
	Label               string                     `json:"label"`
	IsAttack            bool                       `json:"is_attack"`
	AttackMethods       []string                   `json:"attack_methods"`
	AttackDomains       []string                   `json:"attack_domains"`
	PrimaryAttackMethod string                     `json:"primary_attack_method"`
	PrimaryAttackDomain string                     `json:"primary_attack_domain"`
	PrimaryRiskType     string                     `json:"primary_risk_type"`
	CaseType            string                     `json:"case_type"`
	EvidenceBasis       []dto.SafetyReviewEvidence `json:"evidence_basis"`
	DecisionRules       []string                   `json:"decision_rules"`
	QuarantineReason    string                     `json:"quarantine_reason"`
	Rationale           string                     `json:"rationale"`
}

// marshalArbiterDecision 序列化决策并保留 Schema 必需的空字符串字段。
func marshalArbiterDecision(decision dto.SafetyReviewDecision) ([]byte, error) {
	return json.Marshal(arbiterDecisionWire{
		Verdict: decision.Verdict, Label: decision.Label, IsAttack: decision.IsAttack,
		AttackMethods: decision.AttackMethods, AttackDomains: decision.AttackDomains,
		PrimaryAttackMethod: decision.PrimaryAttackMethod,
		PrimaryAttackDomain: decision.PrimaryAttackDomain,
		PrimaryRiskType:     decision.PrimaryRiskType, CaseType: decision.CaseType,
		EvidenceBasis: decision.EvidenceBasis, DecisionRules: decision.DecisionRules,
		QuarantineReason: decision.QuarantineReason, Rationale: decision.Rationale,
	})
}

// clearArbiterRiskFields 清空决策中的风险字段。
func clearArbiterRiskFields(decision *dto.SafetyReviewDecision) {
	decision.IsAttack = false
	decision.AttackMethods = []string{}
	decision.AttackDomains = []string{}
	decision.PrimaryAttackMethod = ""
	decision.PrimaryAttackDomain = ""
	decision.PrimaryRiskType = ""
}

// sortArbiterStrings 返回排序去重后的字符串切片。
func sortArbiterStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

// quarantineDecisionJSON 返回固定隔离决策 JSON。
func quarantineDecisionJSON(reason string) []byte {
	return []byte(`{
		"verdict":"quarantine","label":"","is_attack":false,
		"attack_methods":[],"attack_domains":[],"primary_attack_method":"",
		"primary_attack_domain":"","primary_risk_type":"","case_type":"",
		"evidence_basis":[],"decision_rules":["DISCRIMINATION-R01"],
		"quarantine_reason":"` + reason + `","rationale":"合成隔离决策。"
	}`)
}

// arbiterSchedulerStore 构造只待 Arbiter 处理的测试存储。
func arbiterSchedulerStore(t *testing.T, _ string) *fakeSafetyReviewStore {
	t.Helper()
	store := newSafetyReviewFakeStore("trace-1")
	item := store.items["trace-1"]
	item.state = "pending_arbiter"
	for _, stage := range item.stages {
		stage.state = "succeeded"
	}
	item.stages["judge:a"].result = safetyReviewRoleContent(dto.SafetyReviewJudgeA)
	item.stages["judge:b"].result = safetyReviewRoleContent(dto.SafetyReviewJudgeB)
	item.stages["router"].result = safetyReviewRoleContent(dto.SafetyReviewRouter)
	item.stages["expert:attack_domain:ethnic_discrimination"] = &fakeSafetyReviewStage{
		role: dto.SafetyReviewExpert, axis: "attack_domain", category: "ethnic_discrimination",
		state: "succeeded", result: safetyReviewEstablishedExpertContent(
			t, loadSafetyReviewPolicyForRouting(t), "expert:attack_domain:ethnic_discrimination",
		),
	}
	item.stages["arbiter"] = &fakeSafetyReviewStage{role: dto.SafetyReviewArbiter, state: "pending"}
	return store
}

// arbiterPriorOutputs 构造完整的合成 Arbiter 先验输出。
func arbiterPriorOutputs() service.SafetyReviewArbiterPriorOutputs {
	return service.SafetyReviewArbiterPriorOutputs{
		JudgeA: dto.SafetyReviewJudgment{Verdict: "safe", Rationale: "合成 A 结果。"},
		JudgeB: dto.SafetyReviewJudgment{Verdict: "safe", Rationale: "合成 B 结果。"},
		Router: dto.SafetyReviewRoute{CoverageComplete: true},
		Experts: []dto.SafetyReviewExpertResult{
			notEstablishedArbiterExpert("attack_domain", "ethnic_discrimination"),
		},
		TerminalFailures:     []string{},
		FallbackUsed:         false,
		IndependenceDegraded: false,
	}
}

// arbiterPriorWithExperts 返回指定 Expert 集合的先验输出。
func arbiterPriorWithExperts(experts []dto.SafetyReviewExpertResult) service.SafetyReviewArbiterPriorOutputs {
	prior := arbiterPriorOutputs()
	prior.Experts = append([]dto.SafetyReviewExpertResult(nil), experts...)
	return prior
}

// arbiterPriorWithRouter 返回指定 Router 覆盖状态的先验输出。
func arbiterPriorWithRouter(coverageComplete bool) service.SafetyReviewArbiterPriorOutputs {
	prior := arbiterPriorOutputs()
	prior.Router.CoverageComplete = coverageComplete
	return prior
}
