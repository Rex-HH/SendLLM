package dto

import "time"

// PolicyOptimizerApproval 表示绑定候选、回归和 Critic 哈希的人工发布审批。
type PolicyOptimizerApproval struct {
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

// PolicyOptimizerRelease 表示已发布的不可变策略版本。
type PolicyOptimizerRelease struct {
	Version    string    `json:"release_version"`
	Candidate  string    `json:"candidate_version"`
	Path       string    `json:"release_path"`
	SHA256     string    `json:"release_sha256"`
	ApprovalID string    `json:"approval_id"`
	ReleasedAt time.Time `json:"released_at"`
}
