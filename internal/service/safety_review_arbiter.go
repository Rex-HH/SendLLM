// Package service 提供 Safety Review Arbiter 的请求装配和最终决策投影。
package service

import (
	"encoding/json"
	"fmt"
	"time"

	"sendllm/internal/dao"
	"sendllm/internal/dto"
)

// SafetyReviewArbiterPriorOutputs 表示 Arbiter 可见的结构化先验结果。
type SafetyReviewArbiterPriorOutputs struct {
	JudgeA               dto.SafetyReviewJudgment       `json:"judge_a"`
	JudgeB               dto.SafetyReviewJudgment       `json:"judge_b"`
	Router               dto.SafetyReviewRoute          `json:"router"`
	Experts              []dto.SafetyReviewExpertResult `json:"experts"`
	TerminalFailures     []string                       `json:"terminal_failures"`
	FallbackUsed         bool                           `json:"fallback_used"`
	IndependenceDegraded bool                           `json:"independence_degraded"`
}

// safetyReviewArbiterPolicySummary 表示 Arbiter 可见的策略摘要。
type safetyReviewArbiterPolicySummary struct {
	Common            SafetyReviewCommonPolicy `json:"common"`
	Decisions         string                   `json:"decisions"`
	Categories        map[string][]string      `json:"categories"`
	PrimaryPriorities map[string]int           `json:"primary_priorities"`
}

// BuildSafetyReviewArbiterPriorOutputs 序列化固定字段的 Arbiter 先验输出。
func BuildSafetyReviewArbiterPriorOutputs(
	prior SafetyReviewArbiterPriorOutputs,
) ([]byte, error) {
	prior.Experts = copySafetyReviewExperts(prior.Experts)
	if prior.TerminalFailures == nil {
		prior.TerminalFailures = []string{}
	}
	if len(prior.TerminalFailures) != 0 {
		prior.TerminalFailures = sortDedupStrings(prior.TerminalFailures)
	}
	raw, err := json.Marshal(prior)
	if err != nil {
		return nil, fmt.Errorf("encode arbiter prior outputs: %w", err)
	}
	return raw, nil
}

// BuildSafetyReviewArbiterRequest 构建包含原始条目和结构化先验输出的 Arbiter 请求。
func BuildSafetyReviewArbiterRequest(
	work dao.SafetyReviewStageWork,
	prior SafetyReviewArbiterPriorOutputs,
	policy *SafetyReviewPolicy,
) (SafetyReviewCallRequest, error) {
	if work.Role != string(dto.SafetyReviewArbiter) || work.StageKey != "arbiter" {
		return SafetyReviewCallRequest{}, fmt.Errorf("invalid safety review arbiter stage %q", work.StageKey)
	}
	if policy == nil || !safetyReviewScene(work.Scene) {
		return SafetyReviewCallRequest{}, fmt.Errorf("safety review arbiter policy or scene is invalid")
	}
	priorJSON, err := BuildSafetyReviewArbiterPriorOutputs(prior)
	if err != nil {
		return SafetyReviewCallRequest{}, err
	}
	summary := safetyReviewArbiterPolicySummary{
		Common: policy.Common, Decisions: policy.Decisions,
		Categories:        safetyReviewCategoryCatalog(policy),
		PrimaryPriorities: safetyReviewPrimaryPriorities(policy),
	}
	policyJSON, err := json.Marshal(summary)
	if err != nil {
		return SafetyReviewCallRequest{}, fmt.Errorf("encode arbiter policy summary: %w", err)
	}
	messages, err := BuildSafetyReviewMessages(dto.SafetyReviewArbiter, dto.SafetyReviewRoleInput{
		TraceID: work.TraceID, Scene: work.Scene, Prompt: work.Prompt, Response: work.Response,
		Policy: string(policyJSON), Schema: policy.Schemas["arbiter"], PriorOutputs: priorJSON,
		SystemPrompt: policy.Prompts["arbiter"],
	})
	if err != nil {
		return SafetyReviewCallRequest{}, err
	}
	return newSafetyReviewCallRequest(work, messages, policy.Schemas["arbiter"]), nil
}

// ProjectSafetyReviewDecision 校验并投影 Arbiter 的最终决策。
func ProjectSafetyReviewDecision(
	policy *SafetyReviewPolicy,
	scene string,
	prior SafetyReviewArbiterPriorOutputs,
	raw []byte,
	decidedAt time.Time,
) (dao.SafetyReviewDecisionRecord, error) {
	if policy == nil || !safetyReviewScene(scene) {
		return dao.SafetyReviewDecisionRecord{}, fmt.Errorf("safety review arbiter policy or scene is invalid")
	}
	validator, err := NewSafetyReviewValidator(policy)
	if err != nil {
		return dao.SafetyReviewDecisionRecord{}, fmt.Errorf("build safety review validator: %w", err)
	}
	decision, err := validator.ValidateDecision(scene, prior.Experts, raw)
	if err != nil {
		return dao.SafetyReviewDecisionRecord{}, err
	}
	if decision.Label == "safe" && !canResolveArbiterSafe(policy, scene, prior) {
		return unresolvedSafetyReviewDecision(policy, scene, prior, decidedAt), nil
	}
	finalState := "quarantined"
	if decision.Verdict == "resolved" {
		switch decision.Label {
		case "safe":
			finalState = "resolved_safe"
		case "unsafe":
			finalState = "resolved_unsafe"
		default:
			return dao.SafetyReviewDecisionRecord{}, fmt.Errorf("invalid resolved arbiter label %q", decision.Label)
		}
	}
	return safetyReviewDecisionRecord(decision, finalState, decidedAt), nil
}

// unresolvedSafetyReviewDecision 把不能成立的 Safe 固定投影为隔离决策。
func unresolvedSafetyReviewDecision(
	policy *SafetyReviewPolicy,
	scene string,
	prior SafetyReviewArbiterPriorOutputs,
	decidedAt time.Time,
) dao.SafetyReviewDecisionRecord {
	decision := dto.SafetyReviewDecision{
		Verdict: "quarantine", AttackMethods: []string{}, AttackDomains: []string{},
		EvidenceBasis: []dto.SafetyReviewEvidence{}, DecisionRules: []string{"P04B-DECISION-001"},
		QuarantineReason: unresolvedSafetyReviewQuarantineReason(policy, scene, prior),
		Rationale:        "Arbiter 的 Safe 结论缺少可验证前提，已隔离。",
	}
	return safetyReviewDecisionRecord(decision, "quarantined", decidedAt)
}

// unresolvedSafetyReviewQuarantineReason 选择不能成立 Safe 的稳定隔离原因。
func unresolvedSafetyReviewQuarantineReason(
	policy *SafetyReviewPolicy,
	scene string,
	prior SafetyReviewArbiterPriorOutputs,
) string {
	if prior.IndependenceDegraded {
		return "independence_degraded_unresolved"
	}
	if len(prior.TerminalFailures) != 0 {
		return "model_stage_exhausted"
	}
	plan, err := BuildSafetyReviewRoutingPlan(policy, scene, prior.Router)
	if err != nil || plan.PolicyCoverageGap || !prior.Router.CoverageComplete {
		return "policy_coverage_gap"
	}
	return "irreducible_uncertainty"
}

// modelExhaustedSafetyReviewDecision 构造 Arbiter 耗尽后的固定隔离决策。
func modelExhaustedSafetyReviewDecision(decidedAt time.Time) dao.SafetyReviewDecisionRecord {
	decision := dto.SafetyReviewDecision{
		Verdict: "quarantine", AttackMethods: []string{}, AttackDomains: []string{},
		EvidenceBasis: []dto.SafetyReviewEvidence{}, DecisionRules: []string{"P04B-DECISION-001"},
		QuarantineReason: "model_stage_exhausted", Rationale: "Arbiter 模型链已耗尽。",
	}
	return safetyReviewDecisionRecord(decision, "quarantined", decidedAt)
}

// canResolveArbiterSafe 判断当前先验结果是否允许 Safe 结论。
func canResolveArbiterSafe(
	policy *SafetyReviewPolicy,
	scene string,
	prior SafetyReviewArbiterPriorOutputs,
) bool {
	if prior.IndependenceDegraded || len(prior.TerminalFailures) != 0 {
		return false
	}
	if prior.JudgeA.Verdict == "unsafe" || prior.JudgeB.Verdict == "unsafe" {
		return false
	}
	for _, expert := range prior.Experts {
		if expert.Verdict != "not_established" {
			return false
		}
	}
	plan, err := BuildSafetyReviewRoutingPlan(policy, scene, prior.Router)
	if err != nil || plan.PolicyCoverageGap || !prior.Router.CoverageComplete {
		return false
	}
	return true
}

// safetyReviewDecisionRecord 将 DTO 决策转换为 DAO 持久化记录。
func safetyReviewDecisionRecord(
	decision dto.SafetyReviewDecision,
	finalState string,
	decidedAt time.Time,
) dao.SafetyReviewDecisionRecord {
	methods, marshalErr := json.Marshal(decision.AttackMethods)
	if marshalErr != nil {
		methods = []byte("[]")
	}
	domains, marshalErr := json.Marshal(decision.AttackDomains)
	if marshalErr != nil {
		domains = []byte("[]")
	}
	evidence, marshalErr := json.Marshal(decision.EvidenceBasis)
	if marshalErr != nil {
		evidence = []byte("[]")
	}
	rules, marshalErr := json.Marshal(decision.DecisionRules)
	if marshalErr != nil {
		rules = []byte("[]")
	}
	return dao.SafetyReviewDecisionRecord{
		FinalState: finalState, Label: decision.Label, IsAttack: decision.IsAttack,
		AttackMethods: methods, AttackDomains: domains,
		PrimaryAttackMethod: decision.PrimaryAttackMethod,
		PrimaryAttackDomain: decision.PrimaryAttackDomain,
		PrimaryRiskType:     decision.PrimaryRiskType, CaseType: decision.CaseType,
		Evidence: evidence, DecisionRules: rules, Rationale: decision.Rationale,
		QuarantineReason: decision.QuarantineReason, DecidedAt: decidedAt,
	}
}

// copySafetyReviewExperts 返回 Expert 结果的独立副本。
func copySafetyReviewExperts(values []dto.SafetyReviewExpertResult) []dto.SafetyReviewExpertResult {
	result := make([]dto.SafetyReviewExpertResult, len(values))
	copy(result, values)
	return result
}

// safetyReviewPrimaryPriorities 返回类别到冻结 primary_priority 的映射。
func safetyReviewPrimaryPriorities(policy *SafetyReviewPolicy) map[string]int {
	priorities := make(map[string]int, len(policy.Cards))
	for category, card := range policy.Cards {
		priorities[category] = card.PrimaryPriority
	}
	return priorities
}
