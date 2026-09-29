package dto

// PolicyOptimizerRegressionExpectation 表示单个 Regression Contract 的期望。
type PolicyOptimizerRegressionExpectation struct {
	Label              string `json:"label"`
	MinimumPassCount   int    `json:"minimum_pass_count"`
	MaximumFalseUnsafe int    `json:"maximum_false_unsafe"`
	MaximumFalseSafe   int    `json:"maximum_false_safe"`
	MaximumQuarantine  int    `json:"maximum_quarantine"`
}

// PolicyOptimizerRegressionContract 表示版本化回归合同。
type PolicyOptimizerRegressionContract struct {
	Version           int                                  `json:"version"`
	ContractID        string                               `json:"contract_id"`
	ContractVersion   int                                  `json:"contract_version"`
	PolicyScope       string                               `json:"policy_scope"`
	Description       string                               `json:"description"`
	SuiteQuery        map[string]any                       `json:"suite_query"`
	MinimumCases      int                                  `json:"minimum_cases"`
	Expected          PolicyOptimizerRegressionExpectation `json:"expected"`
	Supersedes        string                               `json:"supersedes"`
	HumanDirectiveRef string                               `json:"human_directive_ref"`
}

// PolicyOptimizerRegressionCase 表示一次 rotation 中的单条判定结果。
type PolicyOptimizerRegressionCase struct {
	TraceID        string `json:"trace_id"`
	Suite          string `json:"suite"`
	GoldLabel      string `json:"gold_label"`
	PredictedLabel string `json:"predicted_label"`
	Quarantined    bool   `json:"quarantined"`
}

// PolicyOptimizerRotationResult 表示一次独立 A/B rotation 的全部结果。
type PolicyOptimizerRotationResult struct {
	RotationID       string                          `json:"rotation_id"`
	TaskDir          string                          `json:"task_dir"`
	OutputSHA256     string                          `json:"output_sha256"`
	Cases            []PolicyOptimizerRegressionCase `json:"cases"`
	BaseTaskDir      string                          `json:"base_task_dir,omitempty"`
	BaseOutputSHA256 string                          `json:"base_output_sha256,omitempty"`
	BaseCases        []PolicyOptimizerRegressionCase `json:"base_cases,omitempty"`
}

// PolicyOptimizerContractResult 表示单个合同的门禁结果。
type PolicyOptimizerContractResult struct {
	ContractID string `json:"contract_id"`
	Passed     bool   `json:"passed"`
	Observed   int    `json:"observed"`
}

// PolicyOptimizerPatternResult 表示 targeted pattern 的改善结果。
type PolicyOptimizerPatternResult struct {
	PatternID string `json:"pattern_id"`
	Improved  bool   `json:"improved"`
}

// PolicyOptimizerRegressionObservation 表示 Regression Runner 的独立观测结果。
type PolicyOptimizerRegressionObservation struct {
	Rotations     []PolicyOptimizerRotationResult `json:"rotations"`
	Contracts     []PolicyOptimizerContractResult `json:"contracts"`
	Patterns      []PolicyOptimizerPatternResult  `json:"patterns"`
	StageFailures int                             `json:"stage_failures"`
}

// PolicyOptimizerRegressionReport 表示冻结的 base/candidate regression 报告。
type PolicyOptimizerRegressionReport struct {
	BaseReleaseSHA256      string                          `json:"base_release_sha256"`
	CandidateReleaseSHA256 string                          `json:"candidate_release_sha256"`
	SuiteVersion           string                          `json:"suite_version"`
	GatePolicyVersion      string                          `json:"gate_policy_version"`
	Rotations              []PolicyOptimizerRotationResult `json:"rotations"`
	Contracts              []PolicyOptimizerContractResult `json:"contracts"`
	Patterns               []PolicyOptimizerPatternResult  `json:"patterns"`
	StageFailures          int                             `json:"stage_failures"`
}

// PolicyOptimizerGatePolicy 表示版本化 P04-B gate 阈值。
type PolicyOptimizerGatePolicy struct {
	Version                 string `json:"version" yaml:"version"`
	MinimumUnsafeResolved   int    `json:"minimum_unsafe_resolved" yaml:"minimum_unsafe_resolved"`
	MaximumUnsafeQuarantine int    `json:"maximum_unsafe_quarantine" yaml:"maximum_unsafe_quarantine"`
	MaximumHiddenQuarantine int    `json:"maximum_hidden_quarantine" yaml:"maximum_hidden_quarantine"`
}
