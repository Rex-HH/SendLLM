package service

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"sendllm/internal/dto"
)

// LoadChangeRequests 严格加载一个或多个 Change Request YAML。
func LoadChangeRequests(paths []string) ([]dto.PolicyOptimizerChangeRequest, error) {
	if len(paths) == 0 {
		return []dto.PolicyOptimizerChangeRequest{}, nil
	}
	result := make([]dto.PolicyOptimizerChangeRequest, 0, len(paths))
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read change request %q: %w", path, err)
		}
		request := dto.PolicyOptimizerChangeRequest{}
		decoder := yaml.NewDecoder(bytes.NewReader(raw))
		decoder.KnownFields(true)
		if err := decoder.Decode(&request); err != nil {
			return nil, fmt.Errorf("parse change request %q: %w", path, err)
		}
		if err := dto.ValidateChangeRequest(request); err != nil {
			return nil, err
		}
		result = append(result, request)
	}
	return result, nil
}

// PolicyOptimizerAuthorConfig 指定 Rule Author 输入和模型执行边界。
type PolicyOptimizerAuthorConfig struct {
	Patterns []dto.PolicyOptimizerGlobalPattern
	Requests []dto.PolicyOptimizerChangeRequest
	Executor func([]dto.PolicyOptimizerGlobalPattern, []dto.PolicyOptimizerChangeRequest) ([]dto.PolicyOptimizerProposal, error)
}

// RunRuleAuthor 调用候选作者并验证 Proposal 结构。
func RunRuleAuthor(ctx context.Context, cfg PolicyOptimizerAuthorConfig) ([]dto.PolicyOptimizerProposal, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(cfg.Patterns) == 0 && len(cfg.Requests) == 0 {
		return []dto.PolicyOptimizerProposal{}, nil
	}
	if cfg.Executor == nil {
		return nil, fmt.Errorf("policy optimizer rule author executor is nil")
	}
	proposals, err := cfg.Executor(cfg.Patterns, cfg.Requests)
	if err != nil {
		return nil, err
	}
	for _, proposal := range proposals {
		if err := ValidatePolicyOptimizerProposal(proposal); err != nil {
			return nil, err
		}
	}
	return proposals, nil
}

// ValidatePolicyOptimizerProposal 校验 Proposal 的必填字段和 patch 操作。
func ValidatePolicyOptimizerProposal(proposal dto.PolicyOptimizerProposal) error {
	if strings.TrimSpace(proposal.ID) == "" || strings.TrimSpace(proposal.Problem) == "" ||
		proposal.AffectedCount < 0 || len(proposal.Operations) == 0 {
		return fmt.Errorf("policy optimizer proposal is incomplete")
	}
	for _, operation := range proposal.Operations {
		if err := ValidatePolicyOptimizerOperation(operation); err != nil {
			return fmt.Errorf("proposal %q: %w", proposal.ID, err)
		}
	}
	return nil
}

// ValidatePolicyOptimizerOperation 校验 closed patch 操作。
func ValidatePolicyOptimizerOperation(operation dto.PolicyOptimizerChangeOperation) error {
	switch operation.Operation {
	case "add_yaml_field", "replace_yaml_field", "remove_yaml_field":
		if !strings.HasPrefix(operation.Pointer, "/") {
			return fmt.Errorf("change pointer is invalid")
		}
		return nil
	case "add_versioned_card", "add_decision", "add_regression_contract":
		if strings.TrimSpace(operation.Target) == "" {
			return fmt.Errorf("change target is required")
		}
		return nil
	default:
		return fmt.Errorf("change operation %q is invalid", operation.Operation)
	}
}

// PolicyOptimizerCriticConfig 指定 Critic 输入和模型执行边界。
type PolicyOptimizerCriticConfig struct {
	Proposals      []dto.PolicyOptimizerProposal
	AuthorFamily   string
	CriticFamily   string
	ResolverFamily string
	Executor       func([]dto.PolicyOptimizerProposal) ([]dto.PolicyOptimizerCritique, error)
}

// RunPolicyCritic 调用独立 Critic 并校验 verdict 闭集。
func RunPolicyCritic(ctx context.Context, cfg PolicyOptimizerCriticConfig) ([]dto.PolicyOptimizerCritique, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(cfg.Proposals) == 0 {
		return []dto.PolicyOptimizerCritique{}, nil
	}
	if cfg.CriticFamily != "" && (cfg.CriticFamily == cfg.AuthorFamily || cfg.CriticFamily == cfg.ResolverFamily) {
		return nil, fmt.Errorf("policy optimizer critic family must differ from author and resolver")
	}
	if cfg.Executor == nil {
		return nil, fmt.Errorf("policy optimizer critic executor is nil")
	}
	critiques, err := cfg.Executor(cfg.Proposals)
	if err != nil {
		return nil, err
	}
	for _, critique := range critiques {
		switch critique.Verdict {
		case "accept", "revise", "block":
		default:
			return nil, fmt.Errorf("policy optimizer critique %q verdict is invalid", critique.ID)
		}
	}
	return critiques, nil
}

// PolicyOptimizerResolveConfig 指定 Change Resolver 的输入。
type PolicyOptimizerResolveConfig struct {
	BaseVersion   string
	BaseSHA256    string
	TargetVersion string
	Requests      []dto.PolicyOptimizerChangeRequest
	Proposals     []dto.PolicyOptimizerProposal
	Critiques     []dto.PolicyOptimizerCritique
}

// ResolvePolicyChanges 应用 authority 顺序并生成确定性 Change Set。
func ResolvePolicyChanges(cfg PolicyOptimizerResolveConfig) (dto.PolicyOptimizerChangeSet, error) {
	if cfg.BaseVersion == "" || cfg.TargetVersion == "" {
		return dto.PolicyOptimizerChangeSet{}, fmt.Errorf("policy optimizer change set versions are required")
	}
	set := dto.PolicyOptimizerChangeSet{
		BaseVersion: cfg.BaseVersion, BaseSHA256: cfg.BaseSHA256,
		CandidateVersion:  cfg.TargetVersion + "-candidate.1",
		AcceptedChanges:   []dto.PolicyOptimizerChangeOperation{},
		RejectedChanges:   []dto.PolicyOptimizerRejectedChange{},
		BlockedConflicts:  []dto.PolicyOptimizerBlockedConflict{},
		AffectedArtifacts: []string{}, RequiredRegressionContracts: []string{},
	}
	criticByProposal := map[string]dto.PolicyOptimizerCritique{}
	for _, critique := range cfg.Critiques {
		criticByProposal[critique.ProposalID] = critique
	}
	for _, proposal := range cfg.Proposals {
		critique, ok := criticByProposal[proposal.ID]
		if ok && critique.Verdict == "block" {
			set.RejectedChanges = append(set.RejectedChanges, dto.PolicyOptimizerRejectedChange{
				ID: proposal.ID, Reason: "critic_blocked", Authority: "automated_proposal",
			})
			continue
		}
		set.AcceptedChanges = append(set.AcceptedChanges, proposal.Operations...)
		set.RequiredRegressionContracts = append(set.RequiredRegressionContracts, proposal.NewRegressionCases...)
	}
	conflicts := detectPolicyOptimizerAuthorityConflicts(cfg.Requests)
	if len(conflicts) != 0 {
		set.BlockedConflicts = append(set.BlockedConflicts, conflicts...)
	}
	set.RejectedChanges = dedupePolicyOptimizerRejected(set.RejectedChanges)
	set.RequiredRegressionContracts = sortDedupStrings(set.RequiredRegressionContracts)
	return set, nil
}

// ValidateChangeSet 校验 Change Set 不包含冲突或非法 patch。
func ValidateChangeSet(set dto.PolicyOptimizerChangeSet) error {
	if set.BaseVersion == "" || set.CandidateVersion == "" {
		return fmt.Errorf("policy optimizer change set versions are incomplete")
	}
	if set.BaseSHA256 != "" && !policyOptimizerSHA256(set.BaseSHA256) {
		return fmt.Errorf("policy optimizer change set base hash is invalid")
	}
	if len(set.BlockedConflicts) != 0 {
		return fmt.Errorf("policy optimizer change set has blocked conflicts")
	}
	for _, operation := range set.AcceptedChanges {
		if err := ValidatePolicyOptimizerOperation(operation); err != nil {
			return err
		}
	}
	return nil
}

// policyOptimizerSHA256 判断字符串是否为小写 SHA-256。
func policyOptimizerSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			if char < 'a' || char > 'f' {
				return false
			}
		}
	}
	return true
}

// detectPolicyOptimizerAuthorityConflicts 检测同等级高权威请求冲突。
func detectPolicyOptimizerAuthorityConflicts(
	requests []dto.PolicyOptimizerChangeRequest,
) []dto.PolicyOptimizerBlockedConflict {
	high := map[string][]string{}
	for _, request := range requests {
		if request.Authority != dto.PolicyOptimizerAuthorityHumanDirective &&
			request.Authority != dto.PolicyOptimizerAuthoritySecurityDecision {
			continue
		}
		for _, target := range request.Targets {
			high[target] = append(high[target], request.ChangeID)
		}
	}
	conflicts := make([]dto.PolicyOptimizerBlockedConflict, 0)
	targets := make([]string, 0, len(high))
	for target := range high {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	for _, target := range targets {
		ids := sortDedupStrings(high[target])
		if len(ids) > 1 {
			conflicts = append(conflicts, dto.PolicyOptimizerBlockedConflict{
				IDs: ids, Reason: "equal_high_authority_conflict:" + target,
			})
		}
	}
	return conflicts
}

// dedupePolicyOptimizerRejected 去重 rejected changes。
func dedupePolicyOptimizerRejected(values []dto.PolicyOptimizerRejectedChange) []dto.PolicyOptimizerRejectedChange {
	seen := map[string]bool{}
	result := make([]dto.PolicyOptimizerRejectedChange, 0, len(values))
	for _, value := range values {
		key := value.ID + "\x00" + value.Reason
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, value)
	}
	return result
}
