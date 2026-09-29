package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"sendllm/internal/dto"
)

// PolicyOptimizerBatchingConfig 表示确定性分层比例和批次边界。
type PolicyOptimizerBatchingConfig struct {
	HomogeneousPercent int
	ConflictPercent    int
	RandomPercent      int
	TargetSize         int
	MinSize            int
	MaxSize            int
}

// StratifyAuditRecords 使用迭代种子把记录分配到 70/20/10 三个非重叠流。
func StratifyAuditRecords(
	records []dto.PolicyOptimizerAuditRecordRef,
	cfg PolicyOptimizerBatchingConfig,
	iterationID string,
) ([]dto.PolicyOptimizerBatch, error) {
	if cfg.HomogeneousPercent+cfg.ConflictPercent+cfg.RandomPercent != 100 ||
		cfg.MinSize < 1 || cfg.TargetSize < cfg.MinSize || cfg.MaxSize < cfg.TargetSize {
		return nil, fmt.Errorf("policy optimizer batching config is invalid")
	}
	if iterationID == "" {
		return nil, fmt.Errorf("policy optimizer iteration id is required")
	}
	sorted := append([]dto.PolicyOptimizerAuditRecordRef(nil), records...)
	sort.SliceStable(sorted, func(i, j int) bool {
		left := policyOptimizerStratumKey(sorted[i], iterationID)
		right := policyOptimizerStratumKey(sorted[j], iterationID)
		if left != right {
			return left < right
		}
		return sorted[i].RecordID < sorted[j].RecordID
	})
	total := len(sorted)
	homogeneousCount := total * cfg.HomogeneousPercent / 100
	conflictCount := total * cfg.ConflictPercent / 100
	if homogeneousCount+conflictCount > total {
		return nil, fmt.Errorf("policy optimizer batching counts overflow")
	}
	streams := []struct {
		mixType string
		code    string
		records []dto.PolicyOptimizerAuditRecordRef
	}{
		{mixType: "homogeneous", code: "H", records: append([]dto.PolicyOptimizerAuditRecordRef(nil), sorted[:homogeneousCount]...)},
		{
			mixType: "conflict", code: "C",
			records: append(
				[]dto.PolicyOptimizerAuditRecordRef(nil),
				sorted[homogeneousCount:homogeneousCount+conflictCount]...,
			),
		},
		{mixType: "random", code: "R", records: append([]dto.PolicyOptimizerAuditRecordRef(nil), sorted[homogeneousCount+conflictCount:]...)},
	}
	result := make([]dto.PolicyOptimizerBatch, 0)
	for _, stream := range streams {
		batches, err := packPolicyOptimizerStream(stream.mixType, stream.code, stream.records, cfg)
		if err != nil {
			return nil, err
		}
		result = append(result, batches...)
	}
	for index := range result {
		result[index].Order = index
	}
	return result, nil
}

// policyOptimizerStratumKey 生成稳定分层键。
func policyOptimizerStratumKey(record dto.PolicyOptimizerAuditRecordRef, iterationID string) string {
	raw, _ := json.Marshal([]string{iterationID, record.SourceID, record.Scene, record.ComparisonType, record.Category})
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

// packPolicyOptimizerStream 按目标/最小/最大边界打包一个流。
func packPolicyOptimizerStream(
	mixType, code string,
	records []dto.PolicyOptimizerAuditRecordRef,
	cfg PolicyOptimizerBatchingConfig,
) ([]dto.PolicyOptimizerBatch, error) {
	if len(records) == 0 {
		return []dto.PolicyOptimizerBatch{}, nil
	}
	chunks := make([][]dto.PolicyOptimizerAuditRecordRef, 0)
	for start := 0; start < len(records); start += cfg.TargetSize {
		end := start + cfg.TargetSize
		if end > len(records) {
			end = len(records)
		}
		chunks = append(chunks, append([]dto.PolicyOptimizerAuditRecordRef(nil), records[start:end]...))
	}
	if len(chunks) > 1 && len(chunks[len(chunks)-1]) < cfg.MinSize {
		last := chunks[len(chunks)-1]
		previous := chunks[len(chunks)-2]
		if len(previous)+len(last) <= cfg.MaxSize {
			chunks[len(chunks)-2] = append(previous, last...)
			chunks = chunks[:len(chunks)-1]
		} else {
			needed := cfg.MinSize - len(last)
			move := append([]dto.PolicyOptimizerAuditRecordRef(nil), previous[len(previous)-needed:]...)
			chunks[len(chunks)-2] = previous[:len(previous)-needed]
			chunks[len(chunks)-1] = append(move, last...)
		}
	}
	result := make([]dto.PolicyOptimizerBatch, 0, len(chunks))
	for index, chunk := range chunks {
		ids := make([]string, 0, len(chunk))
		for _, record := range chunk {
			ids = append(ids, record.RecordID)
		}
		raw, err := json.Marshal(ids)
		if err != nil {
			return nil, fmt.Errorf("encode policy optimizer batch IDs: %w", err)
		}
		digest := sha256.Sum256(raw)
		result = append(result, dto.PolicyOptimizerBatch{
			ID: fmt.Sprintf("B:%s:%06d", code, index+1), Order: index,
			MixType: mixType, Stratum: code, RecordIDs: ids, Count: len(ids),
			SHA256: hex.EncodeToString(digest[:]),
		})
	}
	return result, nil
}

// PolicyOptimizerMiningConfig 指定 local mining 的批次和 executor。
type PolicyOptimizerMiningConfig struct {
	Batches  []dto.PolicyOptimizerBatch
	Executor func(dto.PolicyOptimizerBatch) error
}

// PolicyOptimizerMiningStats 汇总 Local Mining 进度。
type PolicyOptimizerMiningStats struct {
	Batches  int
	Patterns int
}

// RunLocalMining 调用注入的 executor 并汇总本地挖掘结果。
func RunLocalMining(cfg PolicyOptimizerMiningConfig) (PolicyOptimizerMiningStats, error) {
	if cfg.Executor == nil {
		return PolicyOptimizerMiningStats{}, fmt.Errorf("policy optimizer local mining executor is nil")
	}
	stats := PolicyOptimizerMiningStats{}
	for _, batch := range cfg.Batches {
		if len(batch.RecordIDs) == 0 || batch.Count != len(batch.RecordIDs) {
			return stats, fmt.Errorf("policy optimizer batch %q is invalid", batch.ID)
		}
		if err := cfg.Executor(batch); err != nil {
			return stats, fmt.Errorf("mine policy optimizer batch %q: %w", batch.ID, err)
		}
		stats.Batches++
	}
	return stats, nil
}
