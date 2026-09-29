package dto

// PolicyOptimizerRunStats 表示一次 Policy Optimizer workflow 运行结果。
type PolicyOptimizerRunStats struct {
	Mode      string   `json:"mode"`
	Status    string   `json:"status"`
	Stages    []string `json:"stages"`
	Recovered int64    `json:"recovered"`
}
