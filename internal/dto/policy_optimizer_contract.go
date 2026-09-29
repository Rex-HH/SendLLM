// Package dto 提供 Policy Optimizer 的外部输入与状态契约。
package dto

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// PolicyOptimizerMode 表示优化器运行模式。
type PolicyOptimizerMode string

const (
	PolicyOptimizerModeAnalyze PolicyOptimizerMode = "analyze"
	PolicyOptimizerModeCompile PolicyOptimizerMode = "compile"
)

// PolicyOptimizerScene 表示审计记录场景。
type PolicyOptimizerScene string

const (
	PolicyOptimizerScenePrompt   PolicyOptimizerScene = "prompt"
	PolicyOptimizerSceneResponse PolicyOptimizerScene = "response"
)

// PolicyOptimizerStage 表示优化器工作流阶段。
type PolicyOptimizerStage string

const (
	PolicyOptimizerStagePreflight       PolicyOptimizerStage = "preflight"
	PolicyOptimizerStageInspectSources  PolicyOptimizerStage = "inspect_sources"
	PolicyOptimizerStageNormalize       PolicyOptimizerStage = "normalize_sources"
	PolicyOptimizerStageDisagreements   PolicyOptimizerStage = "select_disagreements"
	PolicyOptimizerStageStratify        PolicyOptimizerStage = "stratify"
	PolicyOptimizerStageLocalMining     PolicyOptimizerStage = "local_mining"
	PolicyOptimizerStageGlobalMerge     PolicyOptimizerStage = "global_merge"
	PolicyOptimizerStageAttachCases     PolicyOptimizerStage = "attach_cases"
	PolicyOptimizerStageAdjudication    PolicyOptimizerStage = "case_adjudication"
	PolicyOptimizerStageDiagnosis       PolicyOptimizerStage = "policy_diagnosis"
	PolicyOptimizerStageRuleAuthoring   PolicyOptimizerStage = "rule_authoring"
	PolicyOptimizerStageCritic          PolicyOptimizerStage = "independent_critic"
	PolicyOptimizerStageChangeRequests  PolicyOptimizerStage = "load_change_requests"
	PolicyOptimizerStageResolveChanges  PolicyOptimizerStage = "change_resolution"
	PolicyOptimizerStageCandidatePolicy PolicyOptimizerStage = "candidate_policy"
	PolicyOptimizerStagePromptCompile   PolicyOptimizerStage = "prompt_compile"
	PolicyOptimizerStageRegression      PolicyOptimizerStage = "regression"
	PolicyOptimizerStageApproval        PolicyOptimizerStage = "release_approval"
	PolicyOptimizerStageRelease         PolicyOptimizerStage = "release"
)

// PolicyOptimizerIterationStatus 表示迭代终态或等待状态。
type PolicyOptimizerIterationStatus string

const (
	PolicyOptimizerIterationCreated                 PolicyOptimizerIterationStatus = "created"
	PolicyOptimizerIterationAwaitingMappingApproval PolicyOptimizerIterationStatus = "awaiting_mapping_approval"
	PolicyOptimizerIterationRunning                 PolicyOptimizerIterationStatus = "running"
	PolicyOptimizerIterationNoChange                PolicyOptimizerIterationStatus = "no_change"
	PolicyOptimizerIterationPreviewReady            PolicyOptimizerIterationStatus = "preview_ready"
	PolicyOptimizerIterationAwaitingRegression      PolicyOptimizerIterationStatus = "awaiting_regression"
	PolicyOptimizerIterationRegressionFailed        PolicyOptimizerIterationStatus = "regression_failed"
	PolicyOptimizerIterationAwaitingReleaseApproval PolicyOptimizerIterationStatus = "awaiting_release_approval"
	PolicyOptimizerIterationReleaseReady            PolicyOptimizerIterationStatus = "release_ready"
	PolicyOptimizerIterationReleased                PolicyOptimizerIterationStatus = "released"
	PolicyOptimizerIterationInterrupted             PolicyOptimizerIterationStatus = "interrupted"
	PolicyOptimizerIterationFailed                  PolicyOptimizerIterationStatus = "failed"
)

// PolicyOptimizerStageStatus 表示单个 Skill Run 的状态。
type PolicyOptimizerStageStatus string

const (
	PolicyOptimizerStagePending      PolicyOptimizerStageStatus = "pending"
	PolicyOptimizerStageRunning      PolicyOptimizerStageStatus = "running"
	PolicyOptimizerStageRetryWait    PolicyOptimizerStageStatus = "retry_wait"
	PolicyOptimizerStageSucceeded    PolicyOptimizerStageStatus = "succeeded"
	PolicyOptimizerStageTerminalFail PolicyOptimizerStageStatus = "terminal_failed"
)

// PolicyOptimizerAuthority 表示 Change Request 或审计判断的权威等级。
type PolicyOptimizerAuthority string

const (
	PolicyOptimizerAuthorityHumanDirective     PolicyOptimizerAuthority = "human_directive"
	PolicyOptimizerAuthoritySecurityDecision   PolicyOptimizerAuthority = "security_team_decision"
	PolicyOptimizerAuthorityApprovedGold       PolicyOptimizerAuthority = "approved_gold_evidence"
	PolicyOptimizerAuthorityAutomatedProposal  PolicyOptimizerAuthority = "automated_proposal"
	PolicyOptimizerAuthorityThirdPartyProposal PolicyOptimizerAuthority = "third_party_proposal"
	PolicyOptimizerAuthorityObservation        PolicyOptimizerAuthority = "observation"
)

// PolicyOptimizerComparisonType 表示源记录的比较关系。
type PolicyOptimizerComparisonType string

const (
	PolicyOptimizerComparisonLabelMismatch       PolicyOptimizerComparisonType = "label_mismatch"
	PolicyOptimizerComparisonRiskMismatch        PolicyOptimizerComparisonType = "risk_mismatch"
	PolicyOptimizerComparisonCaseTypeMismatch    PolicyOptimizerComparisonType = "case_type_mismatch"
	PolicyOptimizerComparisonPolicyVersionChange PolicyOptimizerComparisonType = "policy_version_change"
	PolicyOptimizerComparisonModelDisagreement   PolicyOptimizerComparisonType = "model_disagreement"
	PolicyOptimizerComparisonHumanSelected       PolicyOptimizerComparisonType = "human_selected"
	PolicyOptimizerComparisonNoComparison        PolicyOptimizerComparisonType = "no_comparison"
)

// PolicyOptimizerJudgment 表示一条审计记录中的单个判断来源。
type PolicyOptimizerJudgment struct {
	ActorType string                   `json:"actor_type"`
	ActorID   string                   `json:"actor_id"`
	Label     string                   `json:"label"`
	RiskTypes []string                 `json:"risk_types"`
	Authority PolicyOptimizerAuthority `json:"authority"`
	SourceRef string                   `json:"source_ref"`
}

// PolicyOptimizerComparison 表示审计记录中两个判断的比较关系。
type PolicyOptimizerComparison struct {
	Left  int                           `json:"left"`
	Right int                           `json:"right"`
	Type  PolicyOptimizerComparisonType `json:"type"`
}

// PolicyOptimizerAuditRecord 表示进入优化器的规范化审计记录。
type PolicyOptimizerAuditRecord struct {
	RecordID        string                    `json:"record_id"`
	SampleID        string                    `json:"sample_id"`
	SourceID        string                    `json:"source_id"`
	SourceType      string                    `json:"source_type"`
	SourceTask      string                    `json:"source_task"`
	SelectionReason string                    `json:"selection_reason"`
	Scene           PolicyOptimizerScene      `json:"scene"`
	Prompt          string                    `json:"prompt"`
	Response        string                    `json:"response"`
	Judgments       []PolicyOptimizerJudgment `json:"judgments"`
	PolicyVersion   string                    `json:"policy_version"`
	Comparison      PolicyOptimizerComparison `json:"comparison"`
	Metadata        map[string]any            `json:"metadata"`
}

// PolicyOptimizerChangeSource 表示 Change Request 的来源。
type PolicyOptimizerChangeSource struct {
	Type    string `json:"type" yaml:"type"`
	ActorID string `json:"actor_id" yaml:"actor_id"`
}

// PolicyOptimizerChangeRequest 表示人工或其他已登记来源提出的策略变更请求。
type PolicyOptimizerChangeRequest struct {
	Version          int                         `json:"version" yaml:"version"`
	ChangeID         string                      `json:"change_id" yaml:"change_id"`
	Source           PolicyOptimizerChangeSource `json:"source" yaml:"source"`
	Authority        PolicyOptimizerAuthority    `json:"authority" yaml:"authority"`
	Targets          []string                    `json:"targets" yaml:"targets"`
	RequestedChanges []string                    `json:"requested_changes" yaml:"requested_changes"`
	Rationale        string                      `json:"rationale" yaml:"rationale"`
	EvidenceRefs     []string                    `json:"evidence_refs" yaml:"evidence_refs"`
	CreatedAt        time.Time                   `json:"created_at" yaml:"created_at"`
}

var policyOptimizerIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

// ValidateAuditRecord 校验 Canonical Audit Record 的必填字段和闭集关系。
func ValidateAuditRecord(record PolicyOptimizerAuditRecord) error {
	for name, value := range map[string]string{
		"record_id": record.RecordID, "sample_id": record.SampleID, "source_id": record.SourceID,
		"source_task": record.SourceTask, "selection_reason": record.SelectionReason,
		"policy_version": record.PolicyVersion,
	} {
		if !policyOptimizerIDPattern.MatchString(value) && name != "selection_reason" {
			return fmt.Errorf("policy optimizer audit record %s is invalid", name)
		}
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("policy optimizer audit record %s is required", name)
		}
	}
	if !policyOptimizerSourceType(record.SourceType) {
		return fmt.Errorf("policy optimizer source type %q is invalid", record.SourceType)
	}
	if record.Scene != PolicyOptimizerScenePrompt && record.Scene != PolicyOptimizerSceneResponse {
		return fmt.Errorf("policy optimizer scene %q is invalid", record.Scene)
	}
	if len(record.Judgments) == 0 {
		return fmt.Errorf("policy optimizer audit record requires judgments")
	}
	for index, judgment := range record.Judgments {
		if strings.TrimSpace(judgment.ActorID) == "" || strings.TrimSpace(judgment.ActorType) == "" {
			return fmt.Errorf("policy optimizer judgment %d actor is incomplete", index)
		}
		if judgment.Label != "safe" && judgment.Label != "unsafe" && judgment.Label != "uncertain" {
			return fmt.Errorf("policy optimizer judgment %d label is invalid", index)
		}
		if !policyOptimizerAuthority(judgment.Authority) {
			return fmt.Errorf("policy optimizer judgment %d authority is invalid", index)
		}
		if strings.TrimSpace(judgment.SourceRef) == "" {
			return fmt.Errorf("policy optimizer judgment %d source_ref is required", index)
		}
	}
	if !policyOptimizerComparison(record.Comparison.Type) {
		return fmt.Errorf("policy optimizer comparison type is invalid")
	}
	if record.Comparison.Left < 0 || record.Comparison.Right < 0 ||
		record.Comparison.Left >= len(record.Judgments) ||
		record.Comparison.Right >= len(record.Judgments) {
		return fmt.Errorf("policy optimizer comparison indices are invalid")
	}
	if record.Metadata == nil {
		return fmt.Errorf("policy optimizer audit record metadata must be an object")
	}
	return nil
}

// ValidateChangeRequest 校验 Change Request 的版本、来源、权限和必填内容。
func ValidateChangeRequest(request PolicyOptimizerChangeRequest) error {
	if request.Version != 1 {
		return fmt.Errorf("policy optimizer change request version must be 1")
	}
	if !policyOptimizerIDPattern.MatchString(request.ChangeID) {
		return fmt.Errorf("policy optimizer change request id is invalid")
	}
	if !policyOptimizerChangeSource(request.Source.Type) || strings.TrimSpace(request.Source.ActorID) == "" {
		return fmt.Errorf("policy optimizer change request source is invalid")
	}
	if !policyOptimizerAuthority(request.Authority) {
		return fmt.Errorf("policy optimizer change request authority is invalid")
	}
	if len(request.Targets) == 0 || len(request.RequestedChanges) == 0 {
		return fmt.Errorf("policy optimizer change request targets and changes are required")
	}
	if strings.TrimSpace(request.Rationale) == "" {
		return fmt.Errorf("policy optimizer change request rationale is required")
	}
	if request.CreatedAt.IsZero() {
		return fmt.Errorf("policy optimizer change request created_at is required")
	}
	return nil
}

// policyOptimizerSourceType 判断源类型是否属于冻结闭集。
func policyOptimizerSourceType(value string) bool {
	switch value {
	case "safety_review_disagreement", "sft_vs_judge", "sft_vs_gold",
		"policy_version_diff", "human_selected", "third_party_model",
		"external_benchmark", "historical_error", "random_sample", "custom":
		return true
	default:
		return false
	}
}

// policyOptimizerAuthority 判断权威等级是否属于冻结闭集。
func policyOptimizerAuthority(value PolicyOptimizerAuthority) bool {
	switch value {
	case PolicyOptimizerAuthorityHumanDirective, PolicyOptimizerAuthoritySecurityDecision,
		PolicyOptimizerAuthorityApprovedGold, PolicyOptimizerAuthorityAutomatedProposal,
		PolicyOptimizerAuthorityThirdPartyProposal, PolicyOptimizerAuthorityObservation:
		return true
	default:
		return false
	}
}

// policyOptimizerComparison 判断比较类型是否属于冻结闭集。
func policyOptimizerComparison(value PolicyOptimizerComparisonType) bool {
	switch value {
	case PolicyOptimizerComparisonLabelMismatch, PolicyOptimizerComparisonRiskMismatch,
		PolicyOptimizerComparisonCaseTypeMismatch, PolicyOptimizerComparisonPolicyVersionChange,
		PolicyOptimizerComparisonModelDisagreement, PolicyOptimizerComparisonHumanSelected,
		PolicyOptimizerComparisonNoComparison:
		return true
	default:
		return false
	}
}

// policyOptimizerChangeSource 判断 Change Request 来源是否属于冻结闭集。
func policyOptimizerChangeSource(value string) bool {
	switch value {
	case "human", "security_team", "automated_proposal", "third_party_model", "external_research":
		return true
	default:
		return false
	}
}
