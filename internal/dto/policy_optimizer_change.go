package dto

// PolicyOptimizerChangeOperation 表示 deterministic policy patch 操作。
type PolicyOptimizerChangeOperation struct {
	Operation      string   `json:"operation"`
	Pointer        string   `json:"pointer"`
	Value          any      `json:"value,omitempty"`
	Target         string   `json:"target,omitempty"`
	TargetPath     string   `json:"target_path,omitempty"`
	SourceIDs      []string `json:"source_ids,omitempty"`
	Authority      string   `json:"authority,omitempty"`
	OldValueSHA256 string   `json:"old_value_sha256,omitempty"`
}

// PolicyOptimizerProposal 表示 Rule Author 产生的候选提案。
type PolicyOptimizerProposal struct {
	ID                  string                           `json:"proposal_id"`
	Problem             string                           `json:"problem"`
	AffectedCount       int                              `json:"affected_count"`
	SourceDistribution  map[string]int                   `json:"source_distribution"`
	CurrentRuleHashes   map[string]string                `json:"current_rule_hashes"`
	Operations          []PolicyOptimizerChangeOperation `json:"operations"`
	AddressedPatternIDs []string                         `json:"addressed_pattern_ids"`
	RegressionRisks     []string                         `json:"regression_risks"`
	NewRegressionCases  []string                         `json:"new_regression_cases"`
	NonGoals            []string                         `json:"non_goals"`
}

// PolicyOptimizerCritique 表示独立 Critic 的审查结论。
type PolicyOptimizerCritique struct {
	ID                 string   `json:"critique_id"`
	ProposalID         string   `json:"proposal_id"`
	Verdict            string   `json:"verdict"`
	Issues             []string `json:"issues"`
	Counterexamples    []string `json:"counterexamples"`
	RequiredChanges    []string `json:"required_changes"`
	CheckedContractIDs []string `json:"checked_contract_ids"`
}

// PolicyOptimizerRejectedChange 表示 Resolver 拒绝的提案或请求。
type PolicyOptimizerRejectedChange struct {
	ID        string `json:"id"`
	Reason    string `json:"reason"`
	Authority string `json:"authority"`
}

// PolicyOptimizerBlockedConflict 表示必须停止编译的高权威冲突。
type PolicyOptimizerBlockedConflict struct {
	IDs    []string `json:"ids"`
	Reason string   `json:"reason"`
}

// PolicyOptimizerChangeSet 表示确定性 Change Resolver 输出。
type PolicyOptimizerChangeSet struct {
	BaseVersion                 string                           `json:"base_version"`
	BaseSHA256                  string                           `json:"base_sha256"`
	CandidateVersion            string                           `json:"candidate_version"`
	AcceptedChanges             []PolicyOptimizerChangeOperation `json:"accepted_changes"`
	RejectedChanges             []PolicyOptimizerRejectedChange  `json:"rejected_changes"`
	BlockedConflicts            []PolicyOptimizerBlockedConflict `json:"blocked_conflicts"`
	AffectedArtifacts           []string                         `json:"affected_artifacts"`
	RequiredRegressionContracts []string                         `json:"required_regression_contracts"`
}
