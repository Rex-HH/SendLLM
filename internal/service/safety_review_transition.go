package service

import "sendllm/internal/dto"

// SafetyReviewTransitionGate 根据三路初始结果计算条目下游状态。
func SafetyReviewTransitionGate(
	judgeA *dto.SafetyReviewJudgment,
	judgeB *dto.SafetyReviewJudgment,
	router *dto.SafetyReviewRoute,
	independenceDegraded bool,
) string {
	if judgeA == nil || judgeB == nil || router == nil {
		return "pending_arbiter"
	}
	if judgeA.Verdict == "safe" && judgeB.Verdict == "safe" &&
		judgeA.EvidenceComplete && judgeB.EvidenceComplete &&
		router.CoverageComplete && !independenceDegraded &&
		len(router.AttackMethodCandidates) == 0 && len(router.AttackDomainCandidates) == 0 {
		return "resolved_safe"
	}
	if len(router.AttackMethodCandidates) > 0 || len(router.AttackDomainCandidates) > 0 {
		return "awaiting_experts"
	}
	return "pending_arbiter"
}
