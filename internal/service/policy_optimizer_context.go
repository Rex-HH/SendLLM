package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// PolicyOptimizerArtifactRef 表示上下文中的不可变 Artifact 引用。
type PolicyOptimizerArtifactRef struct {
	ID     string `json:"artifact_id"`
	Path   string `json:"relative_path"`
	SHA256 string `json:"sha256"`
}

// PolicyOptimizerContextInput 表示构建一次模型上下文的输入。
type PolicyOptimizerContextInput struct {
	SkillID           string                       `json:"skill_id"`
	SkillVersion      int                          `json:"skill_version"`
	IterationID       string                       `json:"iteration_id"`
	Objective         string                       `json:"objective"`
	Permissions       []string                     `json:"analysis_permissions"`
	PolicyVersion     string                       `json:"policy_version"`
	RelevantRuleCards []json.RawMessage            `json:"relevant_rule_cards"`
	ArtifactRefs      []PolicyOptimizerArtifactRef `json:"artifact_refs"`
	BatchID           string                       `json:"batch_id"`
	Records           []json.RawMessage            `json:"records"`
	OutputSchema      json.RawMessage              `json:"output_schema"`
	MaxInputTokens    int                          `json:"-"`
	MaxArtifacts      int                          `json:"-"`
}

// PolicyOptimizerContextPackage 表示已受限的模型上下文包。
type PolicyOptimizerContextPackage struct {
	SkillID      string          `json:"skill_id"`
	SkillVersion int             `json:"skill_version"`
	IterationID  string          `json:"iteration_id"`
	Task         map[string]any  `json:"task"`
	Policy       map[string]any  `json:"policy"`
	History      map[string]any  `json:"history"`
	Input        map[string]any  `json:"input"`
	OutputSchema json.RawMessage `json:"output_schema"`
	ContextHash  string          `json:"context_hash"`
}

// BuildPolicyOptimizerContext 构建不含聊天记忆的确定性上下文包。
func BuildPolicyOptimizerContext(input PolicyOptimizerContextInput) (PolicyOptimizerContextPackage, error) {
	if strings.TrimSpace(input.SkillID) == "" || input.SkillVersion < 1 ||
		strings.TrimSpace(input.IterationID) == "" || strings.TrimSpace(input.Objective) == "" ||
		strings.TrimSpace(input.PolicyVersion) == "" || len(input.OutputSchema) == 0 {
		return PolicyOptimizerContextPackage{}, fmt.Errorf("policy optimizer context identity is incomplete")
	}
	if input.MaxInputTokens <= 0 || input.MaxArtifacts <= 0 {
		return PolicyOptimizerContextPackage{}, fmt.Errorf("policy optimizer context limits are invalid")
	}
	if len(input.ArtifactRefs) > input.MaxArtifacts {
		return PolicyOptimizerContextPackage{}, fmt.Errorf("policy optimizer context exceeds artifact limit")
	}
	for _, ref := range input.ArtifactRefs {
		if strings.TrimSpace(ref.ID) == "" || strings.TrimSpace(ref.Path) == "" ||
			!policyOptimizerContextHash(ref.SHA256) {
			return PolicyOptimizerContextPackage{}, fmt.Errorf("policy optimizer artifact reference is invalid")
		}
	}
	context := PolicyOptimizerContextPackage{
		SkillID: input.SkillID, SkillVersion: input.SkillVersion, IterationID: input.IterationID,
		Task: map[string]any{
			"objective":            input.Objective,
			"analysis_permissions": append([]string(nil), input.Permissions...),
		},
		Policy: map[string]any{
			"version":             input.PolicyVersion,
			"relevant_rule_cards": clonePolicyOptimizerRawMessages(input.RelevantRuleCards),
		},
		History: map[string]any{"artifact_refs": append([]PolicyOptimizerArtifactRef(nil), input.ArtifactRefs...)},
		Input: map[string]any{
			"batch_id": input.BatchID,
			"records":  clonePolicyOptimizerRawMessages(input.Records),
		},
		OutputSchema: append(json.RawMessage(nil), input.OutputSchema...),
	}
	encoded, err := json.Marshal(context)
	if err != nil {
		return PolicyOptimizerContextPackage{}, fmt.Errorf("encode policy optimizer context: %w", err)
	}
	if estimated := (len(encoded) + 3) / 4; estimated > input.MaxInputTokens {
		return PolicyOptimizerContextPackage{}, fmt.Errorf("policy optimizer context exceeds token limit")
	}
	digest := sha256.Sum256(encoded)
	context.ContextHash = hex.EncodeToString(digest[:])
	return context, nil
}

// clonePolicyOptimizerRawMessages 返回 JSON 原始消息副本。
func clonePolicyOptimizerRawMessages(values []json.RawMessage) []json.RawMessage {
	result := make([]json.RawMessage, len(values))
	for index, value := range values {
		result[index] = append(json.RawMessage(nil), value...)
	}
	return result
}

// policyOptimizerContextHash 判断是否为小写 SHA-256。
func policyOptimizerContextHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if !('0' <= char && char <= '9') && !('a' <= char && char <= 'f') {
			return false
		}
	}
	return true
}
