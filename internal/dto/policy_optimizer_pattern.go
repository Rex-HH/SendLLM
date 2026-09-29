package dto

// PolicyOptimizerLocalPattern 表示单个批次发现的问题模式候选。
type PolicyOptimizerLocalPattern struct {
	ID                string   `json:"pattern_id"`
	BatchID           string   `json:"batch_id"`
	MergeKey          string   `json:"merge_key"`
	Title             string   `json:"title"`
	Hypothesis        string   `json:"hypothesis"`
	CoveredRecordIDs  []string `json:"covered_record_ids"`
	RepresentativeIDs []string `json:"representative_ids"`
	CounterexampleIDs []string `json:"counterexample_ids"`
	Confidence        string   `json:"confidence"`
	Limitations       []string `json:"limitations"`
}

// PolicyOptimizerGlobalPattern 表示确定性合并和复核后的全局模式。
type PolicyOptimizerGlobalPattern struct {
	ID                 string         `json:"pattern_id"`
	MergedLocalIDs     []string       `json:"merged_local_ids"`
	Description        string         `json:"description"`
	CoverageCount      int            `json:"coverage_count"`
	BatchCount         int            `json:"batch_count"`
	SourceDistribution map[string]int `json:"source_distribution"`
	ModelDistribution  map[string]int `json:"model_distribution"`
	RepresentativeIDs  []string       `json:"representative_ids"`
	RandomIDs          []string       `json:"random_ids"`
	BoundaryIDs        []string       `json:"boundary_ids"`
	MergeRationale     string         `json:"merge_rationale"`
}

// PolicyOptimizerCaseAdjudication 表示单 case 的候选裁决。
type PolicyOptimizerCaseAdjudication struct {
	CaseID              string   `json:"case_id"`
	RecordID            string   `json:"record_id"`
	PolicyVersion       string   `json:"policy_version"`
	Rules               []string `json:"rules"`
	EvidenceOwnership   string   `json:"evidence_ownership"`
	Ambiguity           string   `json:"ambiguity"`
	CurrentResult       string   `json:"current_result"`
	CandidateGoldStatus string   `json:"candidate_gold_status"`
	Rationale           string   `json:"rationale"`
}

// PolicyOptimizerDiagnosis 表示对一组 Global Pattern 的候选诊断。
type PolicyOptimizerDiagnosis struct {
	ID                      string   `json:"diagnosis_id"`
	GlobalPatternIDs        []string `json:"global_pattern_ids"`
	Causes                  []string `json:"causes"`
	AffectedRules           []string `json:"affected_rules"`
	AffectedPrompts         []string `json:"affected_prompts"`
	EvidenceRefs            []string `json:"evidence_refs"`
	EstimatedImpact         string   `json:"estimated_impact"`
	PolicyChangeWarranted   bool     `json:"policy_change_warranted"`
	AlternativeExplanations []string `json:"alternative_explanations"`
}
