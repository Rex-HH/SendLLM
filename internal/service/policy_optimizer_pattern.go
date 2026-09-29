package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"sendllm/internal/dto"
)

// PolicyOptimizerGlobalMergeConfig 指定本地模式、记录索引和候选合并器。
type PolicyOptimizerGlobalMergeConfig struct {
	LocalPatterns []dto.PolicyOptimizerLocalPattern
	Records       map[string]dto.PolicyOptimizerAuditRecordRef
	Merge         func([]dto.PolicyOptimizerLocalPattern) ([]dto.PolicyOptimizerGlobalPattern, error)
}

// PolicyOptimizerMergeStats 汇总 Global Merge 输出。
type PolicyOptimizerMergeStats struct {
	LocalPatterns  int
	GlobalPatterns int
}

// RunGlobalMerge 调用候选合并器并确定性重算引用和计数。
func RunGlobalMerge(cfg PolicyOptimizerGlobalMergeConfig) ([]dto.PolicyOptimizerGlobalPattern, PolicyOptimizerMergeStats, error) {
	stats := PolicyOptimizerMergeStats{LocalPatterns: len(cfg.LocalPatterns)}
	if len(cfg.LocalPatterns) == 0 {
		return []dto.PolicyOptimizerGlobalPattern{}, stats, nil
	}
	for _, pattern := range cfg.LocalPatterns {
		if pattern.ID == "" || pattern.BatchID == "" || pattern.Confidence != "candidate" {
			return nil, stats, fmt.Errorf("policy optimizer local pattern is invalid")
		}
		if len(pattern.CoveredRecordIDs) < 2 && !singletonPolicyOptimizerPattern(pattern) {
			return nil, stats, fmt.Errorf("policy optimizer local pattern %q lacks coverage", pattern.ID)
		}
		for _, recordID := range append(append([]string{}, pattern.CoveredRecordIDs...), pattern.RepresentativeIDs...) {
			if _, ok := cfg.Records[recordID]; !ok {
				return nil, stats, fmt.Errorf("policy optimizer local pattern references unknown record %q", recordID)
			}
		}
	}
	patterns := make([]dto.PolicyOptimizerGlobalPattern, 0)
	var err error
	if cfg.Merge != nil {
		patterns, err = cfg.Merge(cfg.LocalPatterns)
		if err != nil {
			return nil, stats, err
		}
	} else {
		patterns = deterministicPolicyOptimizerMerge(cfg.LocalPatterns)
	}
	for index := range patterns {
		pattern := &patterns[index]
		if pattern.ID == "" {
			pattern.ID = fmt.Sprintf("GP:ITER:%06d", index+1)
		}
		coverage, batches, sources, models, err := recomputePolicyOptimizerCoverage(*pattern, cfg.LocalPatterns, cfg.Records)
		if err != nil {
			return nil, stats, err
		}
		pattern.CoverageCount = coverage
		pattern.BatchCount = batches
		pattern.SourceDistribution = sources
		pattern.ModelDistribution = models
	}
	stats.GlobalPatterns = len(patterns)
	return patterns, stats, nil
}

// AttachPatternCases 为 Global Pattern 附加代表性、随机和 boundary case。
func AttachPatternCases(
	pattern dto.PolicyOptimizerGlobalPattern,
	records map[string]dto.PolicyOptimizerAuditRecordRef,
	seed string,
) (dto.PolicyOptimizerGlobalPattern, error) {
	covered := make([]string, 0, len(records))
	for recordID := range records {
		covered = append(covered, recordID)
	}
	if len(covered) == 0 {
		return pattern, fmt.Errorf("policy optimizer pattern has no records")
	}
	sort.Slice(covered, func(i, j int) bool {
		return policyOptimizerCaseRank(seed, covered[i]) < policyOptimizerCaseRank(seed, covered[j])
	})
	boundary := make([]string, 0)
	for _, recordID := range covered {
		record := records[recordID]
		if record.ComparisonType == "case_type_mismatch" || record.ComparisonType == "no_comparison" {
			boundary = append(boundary, recordID)
		}
		if len(boundary) == 10 {
			break
		}
	}
	pattern.BoundaryIDs = boundary
	boundarySet := map[string]bool{}
	for _, id := range boundary {
		boundarySet[id] = true
	}
	remaining := make([]string, 0, len(covered))
	for _, id := range covered {
		if !boundarySet[id] {
			remaining = append(remaining, id)
		}
	}
	representativeLimit := 3
	if len(remaining) < representativeLimit {
		representativeLimit = len(remaining)
	}
	pattern.RepresentativeIDs = append([]string(nil), remaining[:representativeLimit]...)
	remaining = remaining[representativeLimit:]
	randomLimit := 2
	if len(remaining) < randomLimit {
		randomLimit = len(remaining)
	}
	pattern.RandomIDs = append([]string(nil), remaining[:randomLimit]...)
	return pattern, nil
}

// PolicyOptimizerAdjudicationConfig 指定 case adjudication 输入和执行器。
type PolicyOptimizerAdjudicationConfig struct {
	Records  map[string]dto.PolicyOptimizerAuditRecordRef
	Executor func(dto.PolicyOptimizerAuditRecordRef) (dto.PolicyOptimizerCaseAdjudication, error)
}

// PolicyOptimizerAdjudicationStats 汇总 adjudication 输出。
type PolicyOptimizerAdjudicationStats struct {
	Cases int
}

// RunCaseAdjudication 执行候选裁决并拒绝模型授予 Gold 权威。
func RunCaseAdjudication(cfg PolicyOptimizerAdjudicationConfig) (PolicyOptimizerAdjudicationStats, error) {
	if cfg.Executor == nil {
		return PolicyOptimizerAdjudicationStats{}, fmt.Errorf("policy optimizer adjudication executor is nil")
	}
	stats := PolicyOptimizerAdjudicationStats{}
	ids := make([]string, 0, len(cfg.Records))
	for id := range cfg.Records {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		result, err := cfg.Executor(cfg.Records[id])
		if err != nil {
			return stats, fmt.Errorf("adjudicate %q: %w", id, err)
		}
		if result.RecordID != id || result.CandidateGoldStatus == "approved" || result.CandidateGoldStatus == "core" {
			return stats, fmt.Errorf("adjudication %q violates candidate boundary", id)
		}
		stats.Cases++
	}
	return stats, nil
}

// PolicyOptimizerDiagnosisConfig 指定诊断输入和执行器。
type PolicyOptimizerDiagnosisConfig struct {
	Patterns []dto.PolicyOptimizerGlobalPattern
	Executor func([]dto.PolicyOptimizerGlobalPattern) (dto.PolicyOptimizerDiagnosis, error)
}

// PolicyOptimizerDiagnosisStats 汇总诊断输出。
type PolicyOptimizerDiagnosisStats struct {
	Batches   int
	Diagnoses int
}

var policyOptimizerDiagnosisCauses = map[string]bool{
	"source_label_error": true, "rule_missing": true, "rule_too_broad": true,
	"rule_too_narrow": true, "taxonomy_conflict": true, "prompt_problem": true,
	"router_problem": true, "expert_problem": true, "evidence_ownership_problem": true,
	"workflow_problem": true, "incomplete_context": true, "sft_model_bias": true,
}

// RunPolicyDiagnosis 按 5-20 个 Global Pattern 分批诊断并校验原因闭集。
func RunPolicyDiagnosis(cfg PolicyOptimizerDiagnosisConfig) (PolicyOptimizerDiagnosisStats, error) {
	if cfg.Executor == nil {
		return PolicyOptimizerDiagnosisStats{}, fmt.Errorf("policy optimizer diagnosis executor is nil")
	}
	stats := PolicyOptimizerDiagnosisStats{}
	for start := 0; start < len(cfg.Patterns); start += 20 {
		end := start + 20
		if end > len(cfg.Patterns) {
			end = len(cfg.Patterns)
		}
		batch := cfg.Patterns[start:end]
		if len(batch) < 5 && len(cfg.Patterns) >= 5 {
			return stats, fmt.Errorf("policy optimizer diagnosis batch is below five patterns")
		}
		diagnosis, err := cfg.Executor(batch)
		if err != nil {
			return stats, fmt.Errorf("diagnose policy optimizer pattern batch: %w", err)
		}
		for _, cause := range diagnosis.Causes {
			if !policyOptimizerDiagnosisCauses[cause] {
				return stats, fmt.Errorf("policy optimizer diagnosis cause %q is invalid", cause)
			}
		}
		stats.Batches++
		stats.Diagnoses++
	}
	return stats, nil
}

// deterministicPolicyOptimizerMerge 按 merge_key 对本地模式做稳定合并。
func deterministicPolicyOptimizerMerge(local []dto.PolicyOptimizerLocalPattern) []dto.PolicyOptimizerGlobalPattern {
	grouped := map[string][]dto.PolicyOptimizerLocalPattern{}
	keys := make([]string, 0)
	for _, pattern := range local {
		key := pattern.MergeKey
		if key == "" {
			key = pattern.Title
		}
		if _, ok := grouped[key]; !ok {
			keys = append(keys, key)
		}
		grouped[key] = append(grouped[key], pattern)
	}
	sort.Strings(keys)
	result := make([]dto.PolicyOptimizerGlobalPattern, 0, len(keys))
	for index, key := range keys {
		values := grouped[key]
		mergedIDs := make([]string, 0, len(values))
		descriptions := make([]string, 0, len(values))
		for _, value := range values {
			mergedIDs = append(mergedIDs, value.ID)
			descriptions = append(descriptions, value.Hypothesis)
		}
		result = append(result, dto.PolicyOptimizerGlobalPattern{
			ID: fmt.Sprintf("GP:ITER:%06d", index+1), MergedLocalIDs: mergedIDs,
			Description: strings.Join(descriptions, " | "), MergeRationale: "deterministic merge_key",
		})
	}
	return result
}

// recomputePolicyOptimizerCoverage 独立重算覆盖、批次和分布计数。
func recomputePolicyOptimizerCoverage(
	global dto.PolicyOptimizerGlobalPattern,
	local []dto.PolicyOptimizerLocalPattern,
	records map[string]dto.PolicyOptimizerAuditRecordRef,
) (int, int, map[string]int, map[string]int, error) {
	localByID := map[string]dto.PolicyOptimizerLocalPattern{}
	for _, pattern := range local {
		localByID[pattern.ID] = pattern
	}
	covered := map[string]bool{}
	batches := map[string]bool{}
	sources := map[string]int{}
	models := map[string]int{}
	for _, id := range global.MergedLocalIDs {
		pattern, ok := localByID[id]
		if !ok {
			return 0, 0, nil, nil, fmt.Errorf("global pattern references unknown local pattern %q", id)
		}
		batches[pattern.BatchID] = true
		for _, recordID := range pattern.CoveredRecordIDs {
			if _, ok := records[recordID]; !ok {
				return 0, 0, nil, nil, fmt.Errorf("global pattern references unknown record %q", recordID)
			}
			covered[recordID] = true
			sources[records[recordID].SourceID]++
			models[records[recordID].ComparisonType]++
		}
	}
	return len(covered), len(batches), sources, models, nil
}

// singletonPolicyOptimizerPattern 判断模式是否显式标记 singleton。
func singletonPolicyOptimizerPattern(pattern dto.PolicyOptimizerLocalPattern) bool {
	for _, limitation := range pattern.Limitations {
		if limitation == "singleton_candidate" {
			return true
		}
	}
	return false
}

// policyOptimizerCaseRank 返回 case 选择的稳定排序键。
func policyOptimizerCaseRank(seed, recordID string) string {
	raw, _ := json.Marshal([]string{seed, recordID})
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
