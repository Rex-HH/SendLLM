// Package dto 提供 Safety Review 各角色的结构化输出契约。
package dto

import "encoding/json"

// SafetyReviewRole 表示 Safety Review 的五个模型角色。
type SafetyReviewRole string

const (
	// SafetyReviewJudgeA 表示风险发现裁判。
	SafetyReviewJudgeA SafetyReviewRole = "judge_a"
	// SafetyReviewJudgeB 表示误报与排除裁判。
	SafetyReviewJudgeB SafetyReviewRole = "judge_b"
	// SafetyReviewRouter 表示事实抽取与召回路由。
	SafetyReviewRouter SafetyReviewRole = "router"
	// SafetyReviewExpert 表示单规则卡专家。
	SafetyReviewExpert SafetyReviewRole = "expert"
	// SafetyReviewArbiter 表示非投票仲裁者。
	SafetyReviewArbiter SafetyReviewRole = "arbiter"
)

// SafetyReviewRoleInput 包含构建角色消息所需的盲区安全输入。
type SafetyReviewRoleInput struct {
	TraceID      string
	Scene        string
	Prompt       string
	Response     string
	Policy       string
	Schema       json.RawMessage
	RuleCard     json.RawMessage
	PriorOutputs json.RawMessage
	SystemPrompt string
}

// SafetyReviewEvidence 表示可审计的短证据片段。
type SafetyReviewEvidence struct {
	Source string `json:"source"`
	Kind   string `json:"kind"`
	Span   string `json:"span"`
}

// SafetyReviewJudgment 表示 A/B 裁判的结构化信号。
type SafetyReviewJudgment struct {
	Verdict          string                 `json:"verdict"`
	EvidenceComplete bool                   `json:"evidence_complete"`
	Evidence         []SafetyReviewEvidence `json:"evidence"`
	AttackMethods    []string               `json:"attack_methods"`
	AttackDomains    []string               `json:"attack_domains"`
	Exclusions       []string               `json:"exclusions"`
	Rationale        string                 `json:"rationale"`
}

// SafetyReviewFeature 表示 Router 抽取的可观察事实。
type SafetyReviewFeature struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Kind   string `json:"kind"`
	Span   string `json:"span"`
}

// SafetyReviewCandidate 表示一个召回候选及其证据引用。
type SafetyReviewCandidate struct {
	Category    string   `json:"category"`
	EvidenceIDs []string `json:"evidence_ids"`
	Reason      string   `json:"reason"`
}

// MarshalJSON 保证候选证据引用的空集合输出为 []。
func (c SafetyReviewCandidate) MarshalJSON() ([]byte, error) {
	if c.EvidenceIDs == nil {
		c.EvidenceIDs = []string{}
	}
	type candidate SafetyReviewCandidate
	return json.Marshal(candidate(c))
}

// SafetyReviewRoute 表示 Router 的特征、候选和覆盖状态。
type SafetyReviewRoute struct {
	Features               []SafetyReviewFeature   `json:"features"`
	AttackMethodCandidates []SafetyReviewCandidate `json:"attack_method_candidates"`
	AttackDomainCandidates []SafetyReviewCandidate `json:"attack_domain_candidates"`
	CoverageComplete       bool                    `json:"coverage_complete"`
}

// SafetyReviewCondition 表示规则卡必要条件的判定状态。
type SafetyReviewCondition struct {
	ID           string   `json:"id"`
	State        string   `json:"state"`
	EvidenceRefs []string `json:"evidence_refs"`
}

// MarshalJSON 保证条件证据引用的空集合输出为 []。
func (c SafetyReviewCondition) MarshalJSON() ([]byte, error) {
	if c.EvidenceRefs == nil {
		c.EvidenceRefs = []string{}
	}
	type condition SafetyReviewCondition
	return json.Marshal(condition(c))
}

// SafetyReviewExclusion 表示决定性排除条件的判定状态。
type SafetyReviewExclusion struct {
	ID           string   `json:"id"`
	State        string   `json:"state"`
	EvidenceRefs []string `json:"evidence_refs"`
}

// MarshalJSON 保证排除证据引用的空集合输出为 []。
func (e SafetyReviewExclusion) MarshalJSON() ([]byte, error) {
	if e.EvidenceRefs == nil {
		e.EvidenceRefs = []string{}
	}
	type exclusion SafetyReviewExclusion
	return json.Marshal(exclusion(e))
}

// SafetyReviewExpertResult 表示单规则卡 Expert 的完整判定。
type SafetyReviewExpertResult struct {
	Axis               string                  `json:"axis"`
	Category           string                  `json:"category"`
	Verdict            string                  `json:"verdict"`
	Conditions         []SafetyReviewCondition `json:"conditions"`
	DecisiveExclusions []SafetyReviewExclusion `json:"decisive_exclusions"`
	Evidence           []SafetyReviewEvidence  `json:"evidence"`
	EvidenceSource     []string                `json:"evidence_source"`
	Rationale          string                  `json:"rationale"`
}

// SafetyReviewDecision 表示 Arbiter 的最终决策或隔离结果。
type SafetyReviewDecision struct {
	Verdict             string                 `json:"verdict"`
	Label               string                 `json:"label"`
	IsAttack            bool                   `json:"is_attack"`
	AttackMethods       []string               `json:"attack_methods"`
	AttackDomains       []string               `json:"attack_domains"`
	PrimaryAttackMethod string                 `json:"primary_attack_method"`
	PrimaryAttackDomain string                 `json:"primary_attack_domain"`
	PrimaryRiskType     string                 `json:"primary_risk_type"`
	CaseType            string                 `json:"case_type"`
	EvidenceBasis       []SafetyReviewEvidence `json:"evidence_basis"`
	DecisionRules       []string               `json:"decision_rules"`
	QuarantineReason    string                 `json:"quarantine_reason"`
	Rationale           string                 `json:"rationale"`
}

// MarshalJSON 保证空证据和候选集合输出为 []。
func (j SafetyReviewJudgment) MarshalJSON() ([]byte, error) {
	if j.Evidence == nil {
		j.Evidence = []SafetyReviewEvidence{}
	}
	if j.AttackMethods == nil {
		j.AttackMethods = []string{}
	}
	if j.AttackDomains == nil {
		j.AttackDomains = []string{}
	}
	if j.Exclusions == nil {
		j.Exclusions = []string{}
	}
	type judgment SafetyReviewJudgment
	return json.Marshal(judgment(j))
}

// MarshalJSON 保证空特征和候选集合输出为 []。
func (r SafetyReviewRoute) MarshalJSON() ([]byte, error) {
	if r.Features == nil {
		r.Features = []SafetyReviewFeature{}
	}
	if r.AttackMethodCandidates == nil {
		r.AttackMethodCandidates = []SafetyReviewCandidate{}
	}
	if r.AttackDomainCandidates == nil {
		r.AttackDomainCandidates = []SafetyReviewCandidate{}
	}
	type route SafetyReviewRoute
	return json.Marshal(route(r))
}

// MarshalJSON 保证空条件、排除和证据集合输出为 []。
func (e SafetyReviewExpertResult) MarshalJSON() ([]byte, error) {
	if e.Conditions == nil {
		e.Conditions = []SafetyReviewCondition{}
	}
	if e.DecisiveExclusions == nil {
		e.DecisiveExclusions = []SafetyReviewExclusion{}
	}
	if e.Evidence == nil {
		e.Evidence = []SafetyReviewEvidence{}
	}
	if e.EvidenceSource == nil {
		e.EvidenceSource = []string{}
	}
	type expert SafetyReviewExpertResult
	return json.Marshal(expert(e))
}

// MarshalJSON 保证空类别和证据集合输出为 []。
func (d SafetyReviewDecision) MarshalJSON() ([]byte, error) {
	if d.AttackMethods == nil {
		d.AttackMethods = []string{}
	}
	if d.AttackDomains == nil {
		d.AttackDomains = []string{}
	}
	if d.EvidenceBasis == nil {
		d.EvidenceBasis = []SafetyReviewEvidence{}
	}
	if d.DecisionRules == nil {
		d.DecisionRules = []string{}
	}
	type decision SafetyReviewDecision
	return json.Marshal(decision(d))
}
