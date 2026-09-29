package dto

import "time"

// PolicyOptimizerAuditManifest 表示 Audit Package 的严格 manifest。
type PolicyOptimizerAuditManifest struct {
	Version              int                             `yaml:"version"`
	PackageID            string                          `yaml:"package_id"`
	Objective            []string                        `yaml:"objective"`
	CurrentPolicyVersion string                          `yaml:"current_policy_version"`
	AnalysisPermissions  []string                        `yaml:"analysis_permissions"`
	Sources              []PolicyOptimizerSourceManifest `yaml:"sources"`
	ChangeRequests       []string                        `yaml:"change_requests"`
}

// PolicyOptimizerSourceManifest 表示 Audit Package 中的源声明。
type PolicyOptimizerSourceManifest struct {
	SourceID        string            `yaml:"source_id"`
	Type            string            `yaml:"type"`
	Path            string            `yaml:"path"`
	Format          string            `yaml:"format"`
	SelectionReason string            `yaml:"selection_reason"`
	ExpectedRecords int               `yaml:"expected_records"`
	Mapping         string            `yaml:"mapping"`
	Trust           map[string]string `yaml:"trust"`
}

// PolicyOptimizerMappingJudgment 表示映射到 Canonical Judgment 的字段路径。
type PolicyOptimizerMappingJudgment struct {
	ActorType string `yaml:"actor_type"`
	ActorID   string `yaml:"actor_id"`
	Authority string `yaml:"authority"`
	Label     string `yaml:"label"`
	RiskTypes string `yaml:"risk_types"`
}

// PolicyOptimizerMappingComparison 表示映射到比较关系的判断索引。
type PolicyOptimizerMappingComparison struct {
	Left  int    `yaml:"left"`
	Right int    `yaml:"right"`
	Type  string `yaml:"type"`
}

// PolicyOptimizerMapping 表示源记录到 Canonical Audit Record 的已批准映射。
type PolicyOptimizerMapping struct {
	Version        int                              `yaml:"version"`
	SourceID       string                           `yaml:"source_id"`
	SourceSHA256   string                           `yaml:"source_sha256"`
	Format         string                           `yaml:"format"`
	Status         string                           `yaml:"status"`
	ApprovedBy     string                           `yaml:"approved_by"`
	ApprovedAt     time.Time                        `yaml:"approved_at"`
	RecordSelector string                           `yaml:"record_selector"`
	Fields         map[string]string                `yaml:"fields"`
	Judgments      []PolicyOptimizerMappingJudgment `yaml:"judgments"`
	Comparison     PolicyOptimizerMappingComparison `yaml:"comparison"`
}

// PolicyOptimizerSourceInspection 表示单个源的只读结构检查结果。
type PolicyOptimizerSourceInspection struct {
	SourceID         string   `json:"source_id"`
	SourceType       string   `json:"source_type"`
	Format           string   `json:"format"`
	SHA256           string   `json:"sha256"`
	ExpectedCount    int      `json:"expected_count"`
	ActualCount      int      `json:"actual_count"`
	MappingStatus    string   `json:"mapping_status"`
	SuitableAnalyses []string `json:"suitable_analyses"`
	UnsuitableUses   []string `json:"unsuitable_uses"`
	Ambiguities      []string `json:"ambiguities"`
}

// PolicyOptimizerInspection 表示 Audit Package 的结构检查结果。
type PolicyOptimizerInspection struct {
	PackageID            string                            `json:"package_id"`
	CurrentPolicyVersion string                            `json:"current_policy_version"`
	Sources              []PolicyOptimizerSourceInspection `json:"sources"`
	NeedsApproval        bool                              `json:"needs_approval"`
}

// PolicyOptimizerQualityEvent 表示 Safety Review 导出的无 payload 质量事件。
type PolicyOptimizerQualityEvent struct {
	TraceID          string `json:"trace_id"`
	Scene            string `json:"scene"`
	PolicyVersion    string `json:"policy_version"`
	PolicyHash       string `json:"policy_hash"`
	TerminalState    string `json:"terminal_state"`
	PrimaryCategory  string `json:"primary_category"`
	DisagreementType string `json:"disagreement_type"`
	QuarantineReason string `json:"quarantine_reason"`
	ErrorPatternID   string `json:"error_pattern_id"`
}
