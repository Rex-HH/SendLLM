package service_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sendllm/internal/dto"
	"sendllm/internal/service"
)

// TestPolicyOptimizerChangeRequestAndResolver 验证 authority 和 Critic block 处理。
func TestPolicyOptimizerChangeRequestAndResolver(t *testing.T) {
	root := t.TempDir()
	requestPath := filepath.Join(root, "request.yaml")
	writePolicyOptimizerTestFile(t, requestPath, `version: 1
change_id: CR-1
source: {type: human, actor_id: user-1}
authority: human_directive
targets: [expert_prompt]
requested_changes: ["tighten ownership"]
rationale: "synthetic"
evidence_refs: []
created_at: 2026-09-24T00:00:00Z
`)
	requests, err := service.LoadChangeRequests([]string{requestPath})
	if err != nil {
		t.Fatalf("LoadChangeRequests() error = %v", err)
	}
	if len(requests) != 1 {
		t.Fatalf("requests = %+v", requests)
	}
	proposal := dto.PolicyOptimizerProposal{
		ID: "PR:1", Problem: "p", AffectedCount: 1, Operations: []dto.PolicyOptimizerChangeOperation{{
			Operation: "replace_yaml_field", Pointer: "/rules/ownership",
		}},
	}
	critiques, err := service.RunPolicyCritic(context.Background(), service.PolicyOptimizerCriticConfig{
		Proposals: []dto.PolicyOptimizerProposal{proposal},
		Executor: func([]dto.PolicyOptimizerProposal) ([]dto.PolicyOptimizerCritique, error) {
			return []dto.PolicyOptimizerCritique{{ID: "CT:1", ProposalID: "PR:1", Verdict: "block"}}, nil
		},
	})
	if err != nil {
		t.Fatalf("RunPolicyCritic() error = %v", err)
	}
	set, err := service.ResolvePolicyChanges(service.PolicyOptimizerResolveConfig{
		BaseVersion: "p04b-v1.0", TargetVersion: "p04b-v1.1",
		Requests: requests, Proposals: []dto.PolicyOptimizerProposal{proposal}, Critiques: critiques,
	})
	if err != nil {
		t.Fatalf("ResolvePolicyChanges() error = %v", err)
	}
	if len(set.AcceptedChanges) != 0 || len(set.RejectedChanges) != 1 {
		t.Fatalf("change set = %+v", set)
	}
}

// TestPolicyOptimizerResolverChangeSetValidation 验证冻结 patch 操作和 blocking 冲突。
func TestPolicyOptimizerResolverChangeSetValidation(t *testing.T) {
	tests := []struct {
		name    string
		set     dto.PolicyOptimizerChangeSet
		wantErr string
	}{
		{
			name: "frozen operation succeeds",
			set: dto.PolicyOptimizerChangeSet{
				BaseVersion: "p04b-v1.0", CandidateVersion: "p04b-v1.1-candidate.1",
				AcceptedChanges: []dto.PolicyOptimizerChangeOperation{{
					Operation: "replace_yaml_field", Pointer: "/policy/common/rules/0",
				}},
			},
		},
		{
			name: "legacy operation rejected",
			set: dto.PolicyOptimizerChangeSet{
				BaseVersion: "p04b-v1.0", CandidateVersion: "p04b-v1.1-candidate.1",
				AcceptedChanges: []dto.PolicyOptimizerChangeOperation{{
					Operation: "replace", Pointer: "/policy/common/rules/0",
				}},
			},
			wantErr: "invalid",
		},
		{
			name: "blocked conflict rejected",
			set: dto.PolicyOptimizerChangeSet{
				BaseVersion: "p04b-v1.0", CandidateVersion: "p04b-v1.1-candidate.1",
				BlockedConflicts: []dto.PolicyOptimizerBlockedConflict{{
					IDs: []string{"CR-1", "CR-2"}, Reason: "equal_high_authority_conflict:expert_prompt",
				}},
			},
			wantErr: "blocked",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := service.ValidateChangeSet(test.set)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateChangeSet() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("ValidateChangeSet() error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

// TestPolicyOptimizerProposalCriticAndResolver 覆盖 Rule Author、Critic 和 Resolver 的主链路。
func TestPolicyOptimizerProposalCriticAndResolver(t *testing.T) {
	proposals, err := service.RunRuleAuthor(context.Background(), service.PolicyOptimizerAuthorConfig{
		Patterns: []dto.PolicyOptimizerGlobalPattern{{ID: "GP:ITER-001:000001"}},
		Executor: func(
			[]dto.PolicyOptimizerGlobalPattern,
			[]dto.PolicyOptimizerChangeRequest,
		) ([]dto.PolicyOptimizerProposal, error) {
			return []dto.PolicyOptimizerProposal{{
				ID: "PR:1", Problem: "p", AffectedCount: 1,
				SourceDistribution:  map[string]int{"source-a": 1},
				CurrentRuleHashes:   map[string]string{"policy/common.yaml": strings.Repeat("a", 64)},
				Operations:          []dto.PolicyOptimizerChangeOperation{{Operation: "replace_yaml_field", Pointer: "/rules/0"}},
				AddressedPatternIDs: []string{"GP:ITER-001:000001"},
				RegressionRisks:     []string{"over_breadth"},
				NewRegressionCases:  []string{"CONTRACT-1"},
				NonGoals:            []string{"no taxonomy expansion"},
			}}, nil
		},
	})
	if err != nil {
		t.Fatalf("RunRuleAuthor() error = %v", err)
	}
	critiques, err := service.RunPolicyCritic(context.Background(), service.PolicyOptimizerCriticConfig{
		Proposals: proposals, AuthorFamily: "deepseek", CriticFamily: "qwen", ResolverFamily: "deepseek",
		Executor: func([]dto.PolicyOptimizerProposal) ([]dto.PolicyOptimizerCritique, error) {
			return []dto.PolicyOptimizerCritique{{
				ID: "CT:1", ProposalID: "PR:1", Verdict: "accept",
				CheckedContractIDs: []string{"CONTRACT-1"},
			}}, nil
		},
	})
	if err != nil {
		t.Fatalf("RunPolicyCritic() error = %v", err)
	}
	set, err := service.ResolvePolicyChanges(service.PolicyOptimizerResolveConfig{
		BaseVersion: "p04b-v1.0", TargetVersion: "p04b-v1.1", Proposals: proposals, Critiques: critiques,
	})
	if err != nil {
		t.Fatalf("ResolvePolicyChanges() error = %v", err)
	}
	if err := service.ValidateChangeSet(set); err != nil {
		t.Fatalf("ValidateChangeSet() error = %v", err)
	}
	if len(set.AcceptedChanges) != 1 || set.RequiredRegressionContracts[0] != "CONTRACT-1" {
		t.Fatalf("change set = %+v", set)
	}
}

// TestPolicyOptimizerCriticRejectsSharedFamily 验证 Critic 不能和 Author 或 Resolver 同族。
func TestPolicyOptimizerCriticRejectsSharedFamily(t *testing.T) {
	_, err := service.RunPolicyCritic(context.Background(), service.PolicyOptimizerCriticConfig{
		Proposals:    []dto.PolicyOptimizerProposal{{ID: "PR:1"}},
		AuthorFamily: "qwen", CriticFamily: "qwen", ResolverFamily: "deepseek",
		Executor: func([]dto.PolicyOptimizerProposal) ([]dto.PolicyOptimizerCritique, error) {
			t.Fatal("Executor should not be called when critic family conflicts")
			return nil, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "critic family") {
		t.Fatalf("RunPolicyCritic() error = %v, want critic family conflict", err)
	}
}

// TestPolicyOptimizerResolverBlocksEqualHighAuthority 验证同级高权威冲突不会被静默合并。
func TestPolicyOptimizerResolverBlocksEqualHighAuthority(t *testing.T) {
	requests := []dto.PolicyOptimizerChangeRequest{
		policyOptimizerChangeRequest("CR-1", dto.PolicyOptimizerAuthorityHumanDirective, "expert_prompt"),
		policyOptimizerChangeRequest("CR-2", dto.PolicyOptimizerAuthoritySecurityDecision, "expert_prompt"),
	}
	set, err := service.ResolvePolicyChanges(service.PolicyOptimizerResolveConfig{
		BaseVersion: "p04b-v1.0", TargetVersion: "p04b-v1.1", Requests: requests,
	})
	if err != nil {
		t.Fatalf("ResolvePolicyChanges() error = %v", err)
	}
	if len(set.BlockedConflicts) != 1 {
		t.Fatalf("BlockedConflicts = %+v, want one conflict", set.BlockedConflicts)
	}
	if err := service.ValidateChangeSet(set); err == nil {
		t.Fatal("ValidateChangeSet() expected blocked conflict error, got nil")
	}
}

func policyOptimizerChangeRequest(
	id string,
	authority dto.PolicyOptimizerAuthority,
	target string,
) dto.PolicyOptimizerChangeRequest {
	return dto.PolicyOptimizerChangeRequest{
		Version: 1, ChangeID: id,
		Source:    dto.PolicyOptimizerChangeSource{Type: "human", ActorID: "user-1"},
		Authority: authority, Targets: []string{target},
		RequestedChanges: []string{"tighten ownership"},
		Rationale:        "synthetic",
		CreatedAt:        mustParsePolicyOptimizerTestTime("2026-09-24T00:00:00Z"),
	}
}

func mustParsePolicyOptimizerTestTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return parsed
}
