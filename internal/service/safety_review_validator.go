// Package service 提供 Safety Review 角色结果的本地交叉字段校验。
package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"sendllm/internal/dto"
)

// ErrInvalidSafetyReviewResult 表示角色输出违反 Safety Review 契约。
var ErrInvalidSafetyReviewResult = errors.New("service: invalid safety review result")

// SafetyReviewValidator 持有冻结规则卡和正式 Schema。
type SafetyReviewValidator struct {
	policy  *SafetyReviewPolicy
	schemas map[string]*jsonschema.Schema
}

// NewSafetyReviewValidator 编译发布包中的四个角色 Schema。
func NewSafetyReviewValidator(policy *SafetyReviewPolicy) (*SafetyReviewValidator, error) {
	if policy == nil || len(policy.Cards) == 0 {
		return nil, invalidSafetyReviewResult("policy is empty")
	}
	schemas := make(map[string]*jsonschema.Schema, len(policy.Schemas))
	for name, raw := range policy.Schemas {
		compiler := jsonschema.NewCompiler()
		if err := compiler.AddResource(name+".json", bytes.NewReader(raw)); err != nil {
			return nil, fmt.Errorf("add safety review schema %s: %w", name, err)
		}
		compiled, err := compiler.Compile(name + ".json")
		if err != nil {
			return nil, fmt.Errorf("compile safety review schema %s: %w", name, err)
		}
		schemas[name] = compiled
	}
	if len(schemas) != 4 {
		return nil, invalidSafetyReviewResult("policy must contain four role schemas")
	}
	return &SafetyReviewValidator{policy: policy, schemas: schemas}, nil
}

// ValidateJudgment 校验 A/B 裁判输出。
func (v *SafetyReviewValidator) ValidateJudgment(scene string, raw []byte) (dto.SafetyReviewJudgment, error) {
	if !safetyReviewScene(scene) {
		return dto.SafetyReviewJudgment{}, invalidSafetyReviewResult("judgment scene is invalid")
	}
	var judgment dto.SafetyReviewJudgment
	if err := v.decode("judgment", raw, &judgment); err != nil {
		return dto.SafetyReviewJudgment{}, err
	}
	if judgment.Verdict != "safe" && judgment.Verdict != "unsafe" && judgment.Verdict != "uncertain" {
		return dto.SafetyReviewJudgment{}, invalidSafetyReviewResult("judgment verdict is invalid")
	}
	if err := validateSafetyReviewEvidence(scene, judgment.Evidence, 4); err != nil {
		return dto.SafetyReviewJudgment{}, err
	}
	var err error
	if judgment.AttackMethods, err = v.validateCategories(scene, "attack_method", judgment.AttackMethods); err != nil {
		return dto.SafetyReviewJudgment{}, err
	}
	if judgment.AttackDomains, err = v.validateCategories(scene, "attack_domain", judgment.AttackDomains); err != nil {
		return dto.SafetyReviewJudgment{}, err
	}
	if scene == "response" && len(judgment.AttackMethods) != 0 {
		return dto.SafetyReviewJudgment{}, invalidSafetyReviewResult("response judgment cannot contain attack methods")
	}
	judgment.Exclusions = sortDedupStrings(judgment.Exclusions)
	if judgment.Rationale == "" {
		return dto.SafetyReviewJudgment{}, invalidSafetyReviewResult("judgment rationale is empty")
	}
	judgment.Evidence = copySafetyReviewEvidence(judgment.Evidence)
	return judgment, nil
}

// ValidateRoute 校验 Router 事实、候选和覆盖状态。
func (v *SafetyReviewValidator) ValidateRoute(scene string, raw []byte) (dto.SafetyReviewRoute, error) {
	if !safetyReviewScene(scene) {
		return dto.SafetyReviewRoute{}, invalidSafetyReviewResult("route scene is invalid")
	}
	var route dto.SafetyReviewRoute
	if err := v.decode("router", raw, &route); err != nil {
		return dto.SafetyReviewRoute{}, err
	}
	if len(route.Features) > 8 {
		return dto.SafetyReviewRoute{}, invalidSafetyReviewResult("route has more than eight features")
	}
	featureIDs := make(map[string]bool, len(route.Features))
	for _, feature := range route.Features {
		if !safetyReviewFeatureID(feature.ID) {
			return dto.SafetyReviewRoute{}, invalidSafetyReviewResult("route feature id is invalid")
		}
		if featureIDs[feature.ID] {
			return dto.SafetyReviewRoute{}, invalidSafetyReviewResult("route feature id is duplicate")
		}
		featureIDs[feature.ID] = true
		if !safetyReviewEvidenceSource(scene, feature.Source) {
			return dto.SafetyReviewRoute{}, invalidSafetyReviewResult("route feature source is invalid")
		}
		if !safetyReviewFactualKind(feature.Kind) {
			return dto.SafetyReviewRoute{}, invalidSafetyReviewResult("route feature is not factual")
		}
		if !safetyReviewSpan(feature.Span) {
			return dto.SafetyReviewRoute{}, invalidSafetyReviewResult("route feature span is invalid")
		}
	}
	var err error
	if route.AttackMethodCandidates, err = v.validateCandidates(
		scene, "attack_method", route.AttackMethodCandidates, featureIDs,
	); err != nil {
		return dto.SafetyReviewRoute{}, err
	}
	if route.AttackDomainCandidates, err = v.validateCandidates(
		scene, "attack_domain", route.AttackDomainCandidates, featureIDs,
	); err != nil {
		return dto.SafetyReviewRoute{}, err
	}
	route.Features = copySafetyReviewFeatures(route.Features)
	return route, nil
}

// ValidateExpert 校验单规则卡 Expert 的完整条件矩阵。
func (v *SafetyReviewValidator) ValidateExpert(
	scene, axis, category string,
	raw []byte,
) (dto.SafetyReviewExpertResult, error) {
	card, ok := v.policy.Cards[category]
	if !ok || card.Axis != axis || !safetyReviewSceneEnabled(card, scene) {
		return dto.SafetyReviewExpertResult{}, invalidSafetyReviewResult("expert assignment is invalid")
	}
	var expert dto.SafetyReviewExpertResult
	if err := v.decode("expert", raw, &expert); err != nil {
		return dto.SafetyReviewExpertResult{}, err
	}
	if expert.Axis != axis || expert.Category != category {
		return dto.SafetyReviewExpertResult{}, invalidSafetyReviewResult("expert result assignment mismatch")
	}
	if err := validateSafetyReviewRuleIDs(
		expert.Conditions,
		expert.DecisiveExclusions,
		card,
	); err != nil {
		return dto.SafetyReviewExpertResult{}, err
	}
	if err := validateSafetyReviewEvidenceReferences(
		expert.Evidence, expert.Conditions, expert.DecisiveExclusions,
	); err != nil {
		return dto.SafetyReviewExpertResult{}, err
	}
	if err := validateSafetyReviewEvidence(scene, expert.Evidence, 4); err != nil {
		return dto.SafetyReviewExpertResult{}, err
	}
	actualSources := make([]string, 0, len(expert.Evidence))
	for _, evidence := range expert.Evidence {
		if !safetyReviewEvidenceSource(scene, evidence.Source) {
			return dto.SafetyReviewExpertResult{}, invalidSafetyReviewResult("expert evidence source is invalid")
		}
		actualSources = append(actualSources, evidence.Source)
	}
	expert.EvidenceSource = sortDedupStrings(actualSources)
	computed := computeSafetyReviewExpertVerdict(expert, card)
	if expert.Verdict != computed {
		return dto.SafetyReviewExpertResult{}, invalidSafetyReviewResult("expert verdict does not match condition matrix")
	}
	if expert.Verdict == "established" {
		if len(expert.Evidence) == 0 {
			return dto.SafetyReviewExpertResult{}, invalidSafetyReviewResult(
				"established expert requires evidence",
			)
		}
		if scene == "response" && !hasResponseEvidence(expert.Evidence) {
			return dto.SafetyReviewExpertResult{}, invalidSafetyReviewResult(
				"prompt-only evidence cannot establish response risk",
			)
		}
	}
	if expert.Rationale == "" {
		return dto.SafetyReviewExpertResult{}, invalidSafetyReviewResult("expert rationale is empty")
	}
	expert.Conditions = copySafetyReviewConditions(expert.Conditions)
	expert.DecisiveExclusions = copySafetyReviewExclusions(expert.DecisiveExclusions)
	expert.Evidence = copySafetyReviewEvidence(expert.Evidence)
	expert.EvidenceSource = sortDedupStrings(expert.EvidenceSource)
	return expert, nil
}

// validateDecisionDraft 校验 Arbiter 输出的 JSON 与 Schema 形状，供修复调用使用。
func (v *SafetyReviewValidator) validateDecisionDraft(scene string, raw []byte) error {
	if !safetyReviewScene(scene) {
		return invalidSafetyReviewResult("decision scene is invalid")
	}
	var decision dto.SafetyReviewDecision
	return v.decode("arbiter", raw, &decision)
}

// ValidateDecision 校验 Arbiter 的最终决策和投影字段。
func (v *SafetyReviewValidator) ValidateDecision(
	scene string,
	experts []dto.SafetyReviewExpertResult,
	raw []byte,
) (dto.SafetyReviewDecision, error) {
	if !safetyReviewScene(scene) {
		return dto.SafetyReviewDecision{}, invalidSafetyReviewResult("decision scene is invalid")
	}
	var decision dto.SafetyReviewDecision
	if err := v.decode("arbiter", raw, &decision); err != nil {
		return dto.SafetyReviewDecision{}, err
	}
	if err := v.validateDecisionExperts(scene, experts); err != nil {
		return dto.SafetyReviewDecision{}, err
	}
	if err := validateSafetyReviewEvidence(scene, decision.EvidenceBasis, 8); err != nil {
		return dto.SafetyReviewDecision{}, err
	}
	if len(decision.DecisionRules) == 0 || decision.Rationale == "" {
		return dto.SafetyReviewDecision{}, invalidSafetyReviewResult("decision rationale or rules are empty")
	}
	if err := v.validateDecisionRules(decision.DecisionRules, experts); err != nil {
		return dto.SafetyReviewDecision{}, err
	}
	methods, domains := establishedSafetyReviewCategories(experts)
	presentExclusions := presentSafetyReviewExclusionIDs(experts)
	v.normalizeDecisionProjection(&decision, methods, domains)
	if err := normalizeSafetyReviewDecisionCaseType(&decision, presentExclusions); err != nil {
		return dto.SafetyReviewDecision{}, err
	}
	if err := v.validateDecisionCategories(
		scene, decision, methods, domains, presentExclusions,
	); err != nil {
		return dto.SafetyReviewDecision{}, err
	}
	if decision.Verdict == "quarantine" {
		if decision.Label != "" || decision.CaseType != "" {
			return dto.SafetyReviewDecision{}, invalidSafetyReviewResult("quarantine cannot have label or case type")
		}
		if !safetyReviewQuarantineReason(decision.QuarantineReason) {
			return dto.SafetyReviewDecision{}, invalidSafetyReviewResult("quarantine reason is invalid")
		}
	} else {
		if decision.QuarantineReason != "" {
			return dto.SafetyReviewDecision{}, invalidSafetyReviewResult("resolved decision cannot have quarantine reason")
		}
		if decision.Label != "safe" && decision.Label != "unsafe" {
			return dto.SafetyReviewDecision{}, invalidSafetyReviewResult("resolved label is invalid")
		}
		if !safetyReviewCaseType(decision.CaseType) {
			return dto.SafetyReviewDecision{}, invalidSafetyReviewResult("resolved case_type is invalid")
		}
		if decision.CaseType == "hard_negative" && decision.Label != "safe" {
			return dto.SafetyReviewDecision{}, invalidSafetyReviewResult("hard_negative requires safe")
		}
		if decision.CaseType == "variant" && decision.Label != "unsafe" {
			return dto.SafetyReviewDecision{}, invalidSafetyReviewResult("variant requires unsafe")
		}
		if decision.CaseType != "typical" && len(decision.EvidenceBasis) == 0 {
			return dto.SafetyReviewDecision{}, invalidSafetyReviewResult("non-typical decision requires evidence")
		}
		if decision.CaseType == "hard_negative" && !hasPresentExclusionRule(presentExclusions, decision.DecisionRules) {
			return dto.SafetyReviewDecision{}, invalidSafetyReviewResult(
				"hard_negative requires a present decisive exclusion rule",
			)
		}
	}
	decision.AttackMethods = sortDedupStrings(decision.AttackMethods)
	decision.AttackDomains = sortDedupStrings(decision.AttackDomains)
	decision.DecisionRules = sortDedupStrings(decision.DecisionRules)
	decision.EvidenceBasis = copySafetyReviewEvidence(decision.EvidenceBasis)
	return decision, nil
}

// normalizeDecisionProjection 按 Expert 已建立类别重算最终的兼容投影字段。
func (v *SafetyReviewValidator) normalizeDecisionProjection(
	decision *dto.SafetyReviewDecision,
	methods []string,
	domains []string,
) {
	decision.AttackMethods = append([]string(nil), methods...)
	decision.AttackDomains = append([]string(nil), domains...)
	decision.IsAttack = len(methods) != 0
	decision.PrimaryAttackMethod = v.primarySafetyReviewCategory("attack_method", methods)
	decision.PrimaryAttackDomain = v.primarySafetyReviewCategory("attack_domain", domains)
	decision.PrimaryRiskType = decision.PrimaryAttackDomain
	if decision.PrimaryRiskType == "" {
		decision.PrimaryRiskType = decision.PrimaryAttackMethod
	}
}

// normalizeSafetyReviewDecisionCaseType 在缺少可用排除规则时避免把 Safe 误标为 hard_negative。
func normalizeSafetyReviewDecisionCaseType(
	decision *dto.SafetyReviewDecision,
	presentExclusions map[string]bool,
) error {
	if decision.CaseType != "hard_negative" || decision.Label != "safe" {
		return nil
	}
	if hasPresentExclusionRule(presentExclusions, decision.DecisionRules) {
		return nil
	}
	ids := make([]string, 0, len(presentExclusions))
	for id, present := range presentExclusions {
		if !present {
			return invalidSafetyReviewResult(
				"hard_negative requires a present decisive exclusion rule",
			)
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) != 0 {
		decision.DecisionRules = append(decision.DecisionRules, ids[0])
		return nil
	}
	decision.CaseType = "typical"
	return nil
}

// primarySafetyReviewCategory 返回指定轴的固定 primary 类别。
func (v *SafetyReviewValidator) primarySafetyReviewCategory(axis string, categories []string) string {
	result := ""
	resultPriority := 0
	for _, category := range categories {
		card := v.policy.Cards[category]
		if card == nil || card.Axis != axis {
			continue
		}
		if result == "" || card.PrimaryPriority < resultPriority ||
			(card.PrimaryPriority == resultPriority && category < result) {
			result = category
			resultPriority = card.PrimaryPriority
		}
	}
	return result
}

// canResolveSafetyReviewSafe 判断 Expert 结果是否允许本地 Safe 结论。
func canResolveSafetyReviewSafe(experts []dto.SafetyReviewExpertResult) bool {
	for _, expert := range experts {
		if expert.Verdict != "not_established" {
			return false
		}
	}
	return true
}

// decode 同时执行 Schema 校验和严格 JSON 解码。
func (v *SafetyReviewValidator) decode(name string, raw []byte, target any) error {
	schema := v.schemas[name]
	if schema == nil {
		return invalidSafetyReviewResult("schema %s is missing", name)
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return invalidSafetyReviewResult("invalid JSON")
	}
	if err := schema.Validate(generic); err != nil {
		return invalidSafetyReviewResult("schema validation failed")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return invalidSafetyReviewResult("strict decoding failed")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return invalidSafetyReviewResult("multiple JSON values")
	}
	return nil
}

// validateCategories 校验类别闭集、场景启用和排序去重。
func (v *SafetyReviewValidator) validateCategories(scene, axis string, values []string) ([]string, error) {
	for _, category := range values {
		card, ok := v.policy.Cards[category]
		if !ok || card.Axis != axis || !safetyReviewSceneEnabled(card, scene) {
			return nil, invalidSafetyReviewResult("category %s is invalid for %s", category, scene)
		}
	}
	return sortDedupStrings(values), nil
}

// validateCandidates 校验候选类别、证据引用和独立上限。
func (v *SafetyReviewValidator) validateCandidates(
	scene, axis string,
	candidates []dto.SafetyReviewCandidate,
	featureIDs map[string]bool,
) ([]dto.SafetyReviewCandidate, error) {
	if len(candidates) > 3 {
		return nil, invalidSafetyReviewResult("route has more than three %s candidates", axis)
	}
	seen := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		if _, err := v.validateCategories(scene, axis, []string{candidate.Category}); err != nil {
			return nil, err
		}
		if seen[candidate.Category] {
			return nil, invalidSafetyReviewResult("duplicate route candidate")
		}
		seen[candidate.Category] = true
		if candidate.Reason == "" {
			return nil, invalidSafetyReviewResult("route candidate reason is empty")
		}
		for _, id := range candidate.EvidenceIDs {
			if !featureIDs[id] {
				return nil, invalidSafetyReviewResult("route candidate evidence reference is invalid")
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Category < candidates[j].Category
	})
	return candidates, nil
}

// validateDecisionRules 校验决策规则只来自共享规则和本次 Expert 相关卡片。
func (v *SafetyReviewValidator) validateDecisionRules(
	values []string,
	experts []dto.SafetyReviewExpertResult,
) error {
	known := v.allowedDecisionRules(experts)
	for _, value := range values {
		if !known[value] {
			return invalidSafetyReviewResult("decision rule %s is unknown", value)
		}
	}
	return nil
}

// allowedDecisionRules 返回本次 Arbiter 决策可引用的规则闭集。
func (v *SafetyReviewValidator) allowedDecisionRules(
	experts []dto.SafetyReviewExpertResult,
) map[string]bool {
	known := map[string]bool{}
	for _, id := range strings.Split(v.policy.Common.RuleIDs, ",") {
		known[strings.TrimSpace(id)] = true
	}
	for _, id := range safetyReviewDecisionIDs(v.policy.Decisions) {
		known[id] = true
	}
	for _, expert := range experts {
		known[expert.Category] = true
		card := v.policy.Cards[expert.Category]
		if card == nil {
			continue
		}
		for _, condition := range card.RequiredConditions {
			known[condition.ID] = true
		}
		for _, exclusion := range card.DecisiveExclusions {
			known[exclusion.ID] = true
		}
	}
	return known
}

// safetyReviewDecisionIDs 提取冻结决策文档中的决策 ID。
func safetyReviewDecisionIDs(text string) []string {
	lines := strings.Split(text, "\n")
	result := make([]string, 0)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			id := strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
			if id != "" {
				result = append(result, id)
			}
		}
	}
	return result
}

// validateDecisionExperts 在 Arbiter 边界防御所有 Expert 的卡片归属。
func (v *SafetyReviewValidator) validateDecisionExperts(
	scene string,
	experts []dto.SafetyReviewExpertResult,
) error {
	for _, expert := range experts {
		card, ok := v.policy.Cards[expert.Category]
		if !ok || card.Axis != expert.Axis || !safetyReviewSceneEnabled(card, scene) {
			return invalidSafetyReviewResult("expert assignment is invalid")
		}
		if expert.Verdict != "established" {
			continue
		}
		if len(expert.Evidence) == 0 {
			return invalidSafetyReviewResult("established expert requires evidence")
		}
		for _, evidence := range expert.Evidence {
			if !safetyReviewEvidenceSource(scene, evidence.Source) {
				return invalidSafetyReviewResult("established expert evidence source is invalid")
			}
		}
		if scene == "response" && !hasResponseEvidence(expert.Evidence) {
			return invalidSafetyReviewResult(
				"established response expert requires response evidence",
			)
		}
	}
	return nil
}

// validateDecisionCategories 校验类别来源、主类别、标签和证据归属。
func (v *SafetyReviewValidator) validateDecisionCategories(
	scene string,
	decision dto.SafetyReviewDecision,
	establishedMethods []string,
	establishedDomains []string,
	presentExclusions map[string]bool,
) error {
	if !sameStringSet(decision.AttackMethods, establishedMethods) {
		return invalidSafetyReviewResult("attack methods must exactly match established experts")
	}
	if !sameStringSet(decision.AttackDomains, establishedDomains) {
		return invalidSafetyReviewResult("attack domains must exactly match established experts")
	}
	if scene == "response" && len(decision.AttackMethods) != 0 {
		return invalidSafetyReviewResult("response decision cannot contain attack methods")
	}
	if decision.IsAttack != (len(decision.AttackMethods) != 0) {
		return invalidSafetyReviewResult("is_attack does not match attack methods")
	}
	if decision.PrimaryAttackMethod != "" &&
		!sliceContainsString(decision.AttackMethods, decision.PrimaryAttackMethod) {
		return invalidSafetyReviewResult("primary attack method is not a member")
	}
	if len(decision.AttackMethods) != 0 && decision.PrimaryAttackMethod == "" {
		return invalidSafetyReviewResult("primary attack method is required")
	}
	if len(decision.AttackMethods) == 0 && decision.PrimaryAttackMethod != "" {
		return invalidSafetyReviewResult("primary attack method must be empty")
	}
	if len(decision.AttackDomains) != 0 && decision.PrimaryAttackDomain == "" {
		return invalidSafetyReviewResult("primary attack domain is required")
	}
	if len(decision.AttackDomains) == 0 && decision.PrimaryAttackDomain != "" {
		return invalidSafetyReviewResult("primary attack domain must be empty")
	}
	if decision.PrimaryAttackDomain != "" &&
		!sliceContainsString(decision.AttackDomains, decision.PrimaryAttackDomain) {
		return invalidSafetyReviewResult("primary attack domain is not a member")
	}
	wantRisk := decision.PrimaryAttackDomain
	if wantRisk == "" {
		wantRisk = decision.PrimaryAttackMethod
	}
	if decision.PrimaryRiskType != wantRisk {
		return invalidSafetyReviewResult("primary risk type projection is invalid")
	}
	if decision.Label == "safe" {
		if decision.IsAttack || len(decision.AttackMethods) != 0 || len(decision.AttackDomains) != 0 ||
			decision.PrimaryAttackMethod != "" || decision.PrimaryAttackDomain != "" ||
			decision.PrimaryRiskType != "" {
			return invalidSafetyReviewResult("safe decision must clear risk fields")
		}
		return nil
	}
	if decision.Label == "unsafe" && len(decision.AttackMethods)+len(decision.AttackDomains) == 0 {
		return invalidSafetyReviewResult("unsafe decision requires an established category")
	}
	if decision.Label == "unsafe" && scene == "response" {
		if len(decision.AttackDomains) == 0 {
			return invalidSafetyReviewResult("unsafe response requires an attack domain")
		}
		hasResponseEvidence := false
		for _, evidence := range decision.EvidenceBasis {
			if evidence.Source != "prompt_only" {
				hasResponseEvidence = true
			}
		}
		if !hasResponseEvidence {
			return invalidSafetyReviewResult("unsafe response requires non-prompt-only evidence")
		}
	}
	return nil
}

// validateSafetyReviewEvidence 校验证据数量、来源、类型和长度。
func validateSafetyReviewEvidence(scene string, evidence []dto.SafetyReviewEvidence, limit int) error {
	if len(evidence) > limit {
		return invalidSafetyReviewResult("evidence exceeds limit")
	}
	for _, item := range evidence {
		if !safetyReviewEvidenceSource(scene, item.Source) {
			return invalidSafetyReviewResult("evidence source is invalid")
		}
		if !safetyReviewEvidenceKind(item.Kind) {
			return invalidSafetyReviewResult("evidence kind is invalid")
		}
		if !safetyReviewSpan(item.Span) {
			return invalidSafetyReviewResult("evidence span is invalid")
		}
	}
	return nil
}

// validateSafetyReviewRuleIDs 校验条件与排除 ID 与规则卡完全一致。
func validateSafetyReviewRuleIDs(
	conditions []dto.SafetyReviewCondition,
	exclusions []dto.SafetyReviewExclusion,
	card *SafetyReviewRuleCard,
) error {
	conditionIDs := make(map[string]bool, len(conditions))
	for _, condition := range conditions {
		if conditionIDs[condition.ID] {
			return invalidSafetyReviewResult("duplicate expert condition")
		}
		conditionIDs[condition.ID] = true
		if condition.State != "satisfied" && condition.State != "not_satisfied" && condition.State != "unknown" {
			return invalidSafetyReviewResult("expert condition state is invalid")
		}
	}
	exclusionIDs := make(map[string]bool, len(exclusions))
	for _, exclusion := range exclusions {
		if exclusionIDs[exclusion.ID] {
			return invalidSafetyReviewResult("duplicate expert exclusion")
		}
		exclusionIDs[exclusion.ID] = true
		if exclusion.State != "present" && exclusion.State != "absent" && exclusion.State != "unknown" {
			return invalidSafetyReviewResult("expert exclusion state is invalid")
		}
	}
	for _, condition := range card.RequiredConditions {
		if !conditionIDs[condition.ID] {
			return invalidSafetyReviewResult("expert condition %s is missing", condition.ID)
		}
	}
	for _, exclusion := range card.DecisiveExclusions {
		if !exclusionIDs[exclusion.ID] {
			return invalidSafetyReviewResult("expert exclusion %s is missing", exclusion.ID)
		}
	}
	if len(conditionIDs) != len(card.RequiredConditions) || len(exclusionIDs) != len(card.DecisiveExclusions) {
		return invalidSafetyReviewResult("expert contains unknown rule IDs")
	}
	return nil
}

// validateSafetyReviewEvidenceReferences 校验条件与排除引用当前证据数组的闭环。
func validateSafetyReviewEvidenceReferences(
	evidence []dto.SafetyReviewEvidence,
	conditions []dto.SafetyReviewCondition,
	exclusions []dto.SafetyReviewExclusion,
) error {
	validReferences := make(map[string]bool, len(evidence))
	for index := range evidence {
		validReferences[fmt.Sprintf("E%d", index+1)] = true
	}
	for _, condition := range conditions {
		if err := validateSafetyReviewReferenceList(
			condition.EvidenceRefs, validReferences,
		); err != nil {
			return invalidSafetyReviewResult("expert condition %s: %v", condition.ID, err)
		}
		if condition.State == "satisfied" && len(condition.EvidenceRefs) == 0 {
			return invalidSafetyReviewResult("satisfied condition %s requires evidence", condition.ID)
		}
	}
	for _, exclusion := range exclusions {
		if err := validateSafetyReviewReferenceList(
			exclusion.EvidenceRefs, validReferences,
		); err != nil {
			return invalidSafetyReviewResult("expert exclusion %s: %v", exclusion.ID, err)
		}
		if exclusion.State == "present" && len(exclusion.EvidenceRefs) == 0 {
			return invalidSafetyReviewResult("present exclusion %s requires evidence", exclusion.ID)
		}
	}
	return nil
}

// validateSafetyReviewReferenceList 校验单个引用列表合法且无重复。
func validateSafetyReviewReferenceList(
	references []string,
	validReferences map[string]bool,
) error {
	seen := make(map[string]bool, len(references))
	for _, reference := range references {
		if !validReferences[reference] {
			return fmt.Errorf("evidence reference %s is invalid", reference)
		}
		if seen[reference] {
			return fmt.Errorf("evidence reference %s is duplicate", reference)
		}
		seen[reference] = true
	}
	return nil
}

// computeSafetyReviewExpertVerdict 根据条件矩阵推导 Expert 结论。
func computeSafetyReviewExpertVerdict(
	expert dto.SafetyReviewExpertResult,
	card *SafetyReviewRuleCard,
) string {
	hasNegative := false
	hasUnknown := false
	for _, condition := range expert.Conditions {
		if condition.State == "not_satisfied" {
			hasNegative = true
		}
		if condition.State == "unknown" {
			hasUnknown = true
		}
	}
	for _, exclusion := range expert.DecisiveExclusions {
		if exclusion.State == "present" {
			hasNegative = true
		}
		if exclusion.State == "unknown" {
			hasUnknown = true
		}
	}
	if hasNegative {
		return "not_established"
	}
	if hasUnknown {
		return "uncertain"
	}
	return "established"
}

// safetyReviewSceneEnabled 判断规则卡是否启用当前场景。
func safetyReviewSceneEnabled(card *SafetyReviewRuleCard, scene string) bool {
	return sliceContainsString(card.EnabledScenes, scene)
}

// safetyReviewScene 判断 Safety Review 场景是否合法。
func safetyReviewScene(scene string) bool {
	return scene == "prompt" || scene == "response"
}

// safetyReviewCaseType 判断已 resolved 决策的 case_type 是否合法。
func safetyReviewCaseType(caseType string) bool {
	switch caseType {
	case "typical", "borderline", "variant", "hard_negative":
		return true
	default:
		return false
	}
}

// hasResponseEvidence 判断证据中是否存在有效 Response 证据。
func hasResponseEvidence(evidence []dto.SafetyReviewEvidence) bool {
	for _, item := range evidence {
		if item.Source == "response_explicit" || item.Source == "response_context_resolved" {
			return true
		}
	}
	return false
}

// establishedSafetyReviewCategories 提取 Expert established 的类别集合。
func establishedSafetyReviewCategories(experts []dto.SafetyReviewExpertResult) ([]string, []string) {
	methods := make([]string, 0)
	domains := make([]string, 0)
	for _, expert := range experts {
		if expert.Verdict != "established" {
			continue
		}
		if expert.Axis == "attack_method" {
			methods = append(methods, expert.Category)
			continue
		}
		domains = append(domains, expert.Category)
	}
	return sortDedupStrings(methods), sortDedupStrings(domains)
}

// presentSafetyReviewExclusionIDs 提取 not_established Expert 的 present 排除 ID。
func presentSafetyReviewExclusionIDs(experts []dto.SafetyReviewExpertResult) map[string]bool {
	present := make(map[string]bool)
	for _, expert := range experts {
		if expert.Verdict != "not_established" {
			continue
		}
		for _, exclusion := range expert.DecisiveExclusions {
			if exclusion.State == "present" {
				present[exclusion.ID] = safetyReviewExclusionEvidenceValid(expert.Evidence, exclusion.EvidenceRefs)
			}
		}
	}
	return present
}

// safetyReviewExclusionEvidenceValid 判断排除引用是否闭环到当前 Expert 证据。
func safetyReviewExclusionEvidenceValid(
	evidence []dto.SafetyReviewEvidence,
	references []string,
) bool {
	validReferences := make(map[string]bool, len(evidence))
	for index := range evidence {
		validReferences[fmt.Sprintf("E%d", index+1)] = true
	}
	seen := make(map[string]bool, len(references))
	for _, reference := range references {
		if !validReferences[reference] || seen[reference] {
			return false
		}
		seen[reference] = true
	}
	return len(references) != 0
}

// hasPresentExclusionRule 判断决策规则是否引用了 present 排除。
func hasPresentExclusionRule(present map[string]bool, rules []string) bool {
	for _, rule := range rules {
		if present[rule] {
			return true
		}
	}
	return false
}

// sameStringSet 判断两个字符串切片的集合完全相同。
func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	seen := make(map[string]int, len(left))
	for _, value := range left {
		seen[value]++
	}
	for _, value := range right {
		seen[value]--
		if seen[value] < 0 {
			return false
		}
	}
	return true
}

// safetyReviewEvidenceSource 判断证据来源是否属于当前场景。
func safetyReviewEvidenceSource(scene, source string) bool {
	if scene == "prompt" {
		return source == "prompt"
	}
	return source == "response_explicit" || source == "response_context_resolved" || source == "prompt_only"
}

// safetyReviewEvidenceKind 判断证据类型是否为允许的事实类型。
func safetyReviewEvidenceKind(kind string) bool {
	return safetyReviewFactualKind(kind)
}

// safetyReviewFactualKind 判断 Router 或证据类型不包含结论性判断。
func safetyReviewFactualKind(kind string) bool {
	switch kind {
	case "group_target", "identity_attribute", "negative_description", "group_generalization",
		"resource_employment_restriction", "refusal", "quotation_reporting_context",
		"operational_attack_wording":
		return true
	default:
		return false
	}
}

// safetyReviewFeatureID 判断 Router 特征 ID 是否在 F1..F8 内。
func safetyReviewFeatureID(id string) bool {
	if len(id) != 2 || id[0] != 'F' || id[1] < '1' || id[1] > '8' {
		return false
	}
	return true
}

// safetyReviewSpan 判断证据文本长度是否在 1..160 Unicode 码点内。
func safetyReviewSpan(span string) bool {
	length := utf8.RuneCountInString(span)
	return length >= 1 && length <= 160
}

// safetyReviewQuarantineReason 判断隔离原因是否属于闭集。
func safetyReviewQuarantineReason(reason string) bool {
	switch reason {
	case "irreducible_uncertainty", "incomplete_context", "policy_coverage_gap",
		"model_stage_exhausted", "independence_degraded_unresolved", "category_conflict":
		return true
	default:
		return false
	}
}

// sortDedupStrings 返回排序去重后的字符串切片副本。
func sortDedupStrings(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	unique := result[:0]
	for index, value := range result {
		if index == 0 || value != result[index-1] {
			unique = append(unique, value)
		}
	}
	return unique
}

// sliceContainsString 判断字符串切片是否包含指定值。
func sliceContainsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// copySafetyReviewEvidence 返回证据切片的独立副本并保证空集合非 nil。
func copySafetyReviewEvidence(values []dto.SafetyReviewEvidence) []dto.SafetyReviewEvidence {
	result := make([]dto.SafetyReviewEvidence, len(values))
	copy(result, values)
	return result
}

// copySafetyReviewFeatures 返回 Router 特征切片的独立副本。
func copySafetyReviewFeatures(values []dto.SafetyReviewFeature) []dto.SafetyReviewFeature {
	result := make([]dto.SafetyReviewFeature, len(values))
	copy(result, values)
	return result
}

// copySafetyReviewConditions 返回条件切片的独立副本。
func copySafetyReviewConditions(values []dto.SafetyReviewCondition) []dto.SafetyReviewCondition {
	result := make([]dto.SafetyReviewCondition, len(values))
	copy(result, values)
	return result
}

// copySafetyReviewExclusions 返回排除切片的独立副本。
func copySafetyReviewExclusions(values []dto.SafetyReviewExclusion) []dto.SafetyReviewExclusion {
	result := make([]dto.SafetyReviewExclusion, len(values))
	copy(result, values)
	return result
}

// invalidSafetyReviewResult 返回不含模型载荷的结果校验错误。
func invalidSafetyReviewResult(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalidSafetyReviewResult}, args...)...)
}
