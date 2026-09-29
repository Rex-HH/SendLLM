package dto

// PolicyOptimizerAuditRecordRef 表示可确定性分层的记录引用。
type PolicyOptimizerAuditRecordRef struct {
	RecordID       string   `json:"record_id"`
	SourceID       string   `json:"source_id"`
	Scene          string   `json:"scene"`
	ComparisonType string   `json:"comparison_type"`
	Category       string   `json:"category"`
	RiskTypes      []string `json:"risk_types"`
}

// PolicyOptimizerBatch 表示一个冻结的挖掘批次。
type PolicyOptimizerBatch struct {
	ID        string   `json:"batch_id"`
	Order     int      `json:"batch_order"`
	MixType   string   `json:"mix_type"`
	Stratum   string   `json:"stratum_key"`
	RecordIDs []string `json:"record_ids"`
	Count     int      `json:"record_count"`
	SHA256    string   `json:"input_sha256"`
}
