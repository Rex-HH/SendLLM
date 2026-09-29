package dto

import "time"

// PolicyOptimizerCandidateGoldInput 表示由 Case Adjudication 生成的候选 Gold 输入。
type PolicyOptimizerCandidateGoldInput struct {
	SampleRef              string
	SourceSHA256           string
	PolicyVersion          string
	Label                  string
	RiskTypes              []string
	CaseType               string
	RuleIDs                []string
	DecisionIDs            []string
	ModelCandidateArtifact string
}

// PolicyOptimizerGoldRecord 表示不可变的 Candidate、Approved 或 Core Gold 记录。
type PolicyOptimizerGoldRecord struct {
	GoldID                 string    `json:"gold_id"`
	SampleRef              string    `json:"sample_ref"`
	SourceSHA256           string    `json:"source_sha256"`
	PolicyVersion          string    `json:"policy_version"`
	Status                 string    `json:"status"`
	Label                  string    `json:"label"`
	RiskTypes              []string  `json:"risk_types"`
	CaseType               string    `json:"case_type"`
	RuleIDs                []string  `json:"rule_ids"`
	DecisionIDs            []string  `json:"decision_ids"`
	ModelCandidateArtifact string    `json:"model_candidate_artifact"`
	ApprovalArtifact       string    `json:"approval_artifact"`
	SuccessfulReleaseIDs   []string  `json:"successful_release_ids"`
	ChallengeIDs           []string  `json:"challenge_ids"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

// PolicyOptimizerGoldHistory 表示 Core 提升需要重算的 release 与 challenge 历史。
type PolicyOptimizerGoldHistory struct {
	SuccessfulReleaseIDs   []string `json:"successful_release_ids"`
	UnresolvedChallengeIDs []string `json:"unresolved_challenge_ids"`
}

// PolicyOptimizerGoldCorrection 表示人工对 Candidate Gold 的修正。
type PolicyOptimizerGoldCorrection struct {
	GoldID      string   `json:"gold_id"`
	Label       string   `json:"label"`
	RiskTypes   []string `json:"risk_types"`
	CaseType    string   `json:"case_type"`
	RuleIDs     []string `json:"rule_ids"`
	DecisionIDs []string `json:"decision_ids"`
	Rationale   string   `json:"rationale"`
}

// PolicyOptimizerGoldApproval 表示绑定 Gold 主体哈希的人工审批。
type PolicyOptimizerGoldApproval struct {
	Version       int               `json:"version"`
	ApprovalID    string            `json:"approval_id"`
	SubjectType   string            `json:"subject_type"`
	SubjectID     string            `json:"subject_id"`
	SubjectSHA256 string            `json:"subject_sha256"`
	RelatedSHA256 map[string]string `json:"related_sha256"`
	ApproverID    string            `json:"approver_id"`
	Decision      string            `json:"decision"`
	Note          string            `json:"note"`
	CreatedAt     time.Time         `json:"created_at"`
}
