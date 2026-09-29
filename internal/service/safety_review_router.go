package service

import (
	"encoding/json"
	"fmt"
	"sort"

	"sendllm/internal/dao"
	"sendllm/internal/dto"
)

// SafetyReviewRoutingPlan 表示 Router 结果归一化后的持久化下游计划。
type SafetyReviewRoutingPlan struct {
	Route             dto.SafetyReviewRoute
	DownstreamStages  []dao.SafetyReviewStageSpec
	ItemState         string
	PolicyCoverageGap bool
}

// safetyReviewRouterPolicySummary 表示 Router 可见的闭集策略摘要。
type safetyReviewRouterPolicySummary struct {
	Common     SafetyReviewCommonPolicy `json:"common"`
	Categories map[string][]string      `json:"categories"`
}

// parseSafetyReviewRoute 解析 Router 的结构化结果。
func parseSafetyReviewRoute(raw []byte) (*dto.SafetyReviewRoute, error) {
	result := &dto.SafetyReviewRoute{}
	if err := json.Unmarshal(raw, result); err != nil {
		return nil, fmt.Errorf("decode safety review route: %w", err)
	}
	return result, nil
}

// BuildSafetyReviewRoutingPlan 归一化 Router 候选并生成下游阶段计划。
func BuildSafetyReviewRoutingPlan(
	policy *SafetyReviewPolicy,
	scene string,
	route dto.SafetyReviewRoute,
) (SafetyReviewRoutingPlan, error) {
	if policy == nil {
		return SafetyReviewRoutingPlan{}, fmt.Errorf("safety review routing policy is empty")
	}
	if !safetyReviewScene(scene) {
		return SafetyReviewRoutingPlan{}, fmt.Errorf("invalid safety review routing scene %q", scene)
	}
	if scene == "response" && len(route.AttackMethodCandidates) != 0 {
		return SafetyReviewRoutingPlan{}, fmt.Errorf("response route cannot contain attack method candidates")
	}
	features, featureIDs, err := normalizeSafetyReviewFeatures(scene, route.Features)
	if err != nil {
		return SafetyReviewRoutingPlan{}, err
	}
	methods, methodGap, err := normalizeSafetyReviewCandidates(
		policy, scene, "attack_method", route.AttackMethodCandidates, featureIDs,
	)
	if err != nil {
		return SafetyReviewRoutingPlan{}, err
	}
	domains, domainGap, err := normalizeSafetyReviewCandidates(
		policy, scene, "attack_domain", route.AttackDomainCandidates, featureIDs,
	)
	if err != nil {
		return SafetyReviewRoutingPlan{}, err
	}
	domains = suppressOtherDiscrimination(domains)

	plan := SafetyReviewRoutingPlan{
		Route: dto.SafetyReviewRoute{
			Features:               features,
			AttackMethodCandidates: methods,
			AttackDomainCandidates: domains,
			CoverageComplete:       route.CoverageComplete,
		},
		PolicyCoverageGap: methodGap || domainGap,
		ItemState:         "pending_arbiter",
		DownstreamStages:  []dao.SafetyReviewStageSpec{arbiterSafetyReviewStage()},
	}
	if len(methods)+len(domains) != 0 {
		plan.ItemState = "awaiting_experts"
		plan.DownstreamStages = expertSafetyReviewStages(methods, domains)
	}
	return plan, nil
}

// BuildSafetyReviewRouterRequest 构建只包含策略摘要的盲区 Router 请求。
func BuildSafetyReviewRouterRequest(
	work dao.SafetyReviewStageWork,
	policy *SafetyReviewPolicy,
) (SafetyReviewCallRequest, error) {
	if work.Role != string(dto.SafetyReviewRouter) || work.StageKey != "router" {
		return SafetyReviewCallRequest{}, fmt.Errorf("invalid safety review router stage %q", work.StageKey)
	}
	if policy == nil || !safetyReviewScene(work.Scene) {
		return SafetyReviewCallRequest{}, fmt.Errorf("safety review router policy or scene is invalid")
	}
	summary := safetyReviewRouterPolicySummary{
		Common:     policy.Common,
		Categories: safetyReviewCategoryCatalog(policy),
	}
	policyJSON, err := json.Marshal(summary)
	if err != nil {
		return SafetyReviewCallRequest{}, fmt.Errorf("encode router policy summary: %w", err)
	}
	messages, err := BuildSafetyReviewMessages(dto.SafetyReviewRouter, dto.SafetyReviewRoleInput{
		TraceID:      work.TraceID,
		Scene:        work.Scene,
		Prompt:       work.Prompt,
		Response:     work.Response,
		Policy:       string(policyJSON),
		Schema:       policy.Schemas["router"],
		SystemPrompt: policy.Prompts["router"],
	})
	if err != nil {
		return SafetyReviewCallRequest{}, err
	}
	return newSafetyReviewCallRequest(work, messages, policy.Schemas["router"]), nil
}

// normalizeSafetyReviewFeatures 校验并排序 Router 的可观察事实。
func normalizeSafetyReviewFeatures(
	scene string,
	features []dto.SafetyReviewFeature,
) ([]dto.SafetyReviewFeature, map[string]bool, error) {
	result := make([]dto.SafetyReviewFeature, 0, len(features))
	seen := make(map[string]bool, len(features))
	for _, feature := range features {
		if !safetyReviewFeatureID(feature.ID) || seen[feature.ID] {
			return nil, nil, fmt.Errorf("invalid or duplicate route feature %q", feature.ID)
		}
		if !safetyReviewEvidenceSource(scene, feature.Source) ||
			!safetyReviewFactualKind(feature.Kind) || !safetyReviewSpan(feature.Span) {
			return nil, nil, fmt.Errorf("invalid route feature %q", feature.ID)
		}
		seen[feature.ID] = true
		result = append(result, feature)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, seen, nil
}

// normalizeSafetyReviewCandidates 校验候选闭集、去重排序并返回策略覆盖缺口。
func normalizeSafetyReviewCandidates(
	policy *SafetyReviewPolicy,
	scene string,
	axis string,
	candidates []dto.SafetyReviewCandidate,
	featureIDs map[string]bool,
) ([]dto.SafetyReviewCandidate, bool, error) {
	result := make([]dto.SafetyReviewCandidate, 0, len(candidates))
	seen := make(map[string]bool, len(candidates))
	policyGap := false
	for _, candidate := range candidates {
		card := policy.Cards[candidate.Category]
		if card == nil || card.Axis != axis || !safetyReviewSceneEnabled(card, scene) {
			policyGap = true
			continue
		}
		if seen[candidate.Category] {
			continue
		}
		if candidate.Reason == "" {
			return nil, false, fmt.Errorf("route candidate %s lacks reason", candidate.Category)
		}
		for _, id := range candidate.EvidenceIDs {
			if !featureIDs[id] {
				return nil, false, fmt.Errorf(
					"route candidate %s has invalid feature reference %s", candidate.Category, id,
				)
			}
		}
		seen[candidate.Category] = true
		result = append(result, dto.SafetyReviewCandidate{
			Category:    candidate.Category,
			EvidenceIDs: append([]string(nil), candidate.EvidenceIDs...),
			Reason:      candidate.Reason,
		})
	}
	if len(result) > 3 {
		return nil, false, fmt.Errorf("route has more than three %s candidates", axis)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Category < result[j].Category })
	return result, policyGap, nil
}

// suppressOtherDiscrimination 在存在具体歧视类别时移除 other_discrimination。
func suppressOtherDiscrimination(candidates []dto.SafetyReviewCandidate) []dto.SafetyReviewCandidate {
	hasSpecific := false
	for _, candidate := range candidates {
		if candidate.Category != "other_discrimination" {
			hasSpecific = true
		}
	}
	if !hasSpecific {
		return candidates
	}
	result := make([]dto.SafetyReviewCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Category != "other_discrimination" {
			result = append(result, candidate)
		}
	}
	return result
}

// expertSafetyReviewStages 为每个候选生成一个 Expert 阶段。
func expertSafetyReviewStages(
	methods []dto.SafetyReviewCandidate,
	domains []dto.SafetyReviewCandidate,
) []dao.SafetyReviewStageSpec {
	stages := make([]dao.SafetyReviewStageSpec, 0, len(methods)+len(domains))
	for _, candidate := range methods {
		stages = append(stages, dao.SafetyReviewStageSpec{
			StageKey: "expert:attack_method:" + candidate.Category,
			Role:     "expert", Axis: "attack_method", Category: candidate.Category,
		})
	}
	for _, candidate := range domains {
		stages = append(stages, dao.SafetyReviewStageSpec{
			StageKey: "expert:attack_domain:" + candidate.Category,
			Role:     "expert", Axis: "attack_domain", Category: candidate.Category,
		})
	}
	sort.Slice(stages, func(i, j int) bool { return stages[i].StageKey < stages[j].StageKey })
	return stages
}

// arbiterSafetyReviewStage 返回固定 Arbiter 下游阶段。
func arbiterSafetyReviewStage() dao.SafetyReviewStageSpec {
	return dao.SafetyReviewStageSpec{StageKey: "arbiter", Role: "arbiter"}
}

// safetyReviewCategoryCatalog 返回不含规则卡内容的类别目录。
func safetyReviewCategoryCatalog(policy *SafetyReviewPolicy) map[string][]string {
	catalog := make(map[string][]string, 2)
	for _, axis := range []string{"attack_method", "attack_domain"} {
		catalog[axis] = make([]string, 0)
	}
	for category, card := range policy.Cards {
		catalog[card.Axis] = append(catalog[card.Axis], category)
	}
	for axis := range catalog {
		sort.Strings(catalog[axis])
	}
	return catalog
}
