package service_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"sendllm/internal/dto"
	"sendllm/internal/service"
)

// TestPolicyOptimizerApprovalAndRelease 验证审批绑定和原子 release 发布。
func TestPolicyOptimizerApprovalAndRelease(t *testing.T) {
	candidateDir := compiledPolicyOptimizerCandidate(t)
	bundle, err := service.VerifyPolicyBundle(candidateDir)
	if err != nil {
		t.Fatalf("VerifyPolicyBundle() error = %v", err)
	}
	candidate := dto.PolicyOptimizerCandidate{Version: bundle.ReleaseVersion, Path: candidateDir, SHA256: bundle.AggregateHash}
	regression := policyOptimizerRegressionReportWithGates(t, 18, 2, false)
	critic := dto.PolicyOptimizerCritique{ID: "CT:1", ProposalID: "PR:1", Verdict: "accept"}
	approval, err := service.CreatePolicyApproval(candidate, regression, critic, "reviewer-1", time.Date(2026, 9, 28, 1, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("CreatePolicyApproval() error = %v", err)
	}
	releasesDir := filepath.Join(t.TempDir(), "releases")
	release, err := service.ReleasePolicy(context.Background(), service.PolicyOptimizerReleaseConfig{
		Candidate: candidate, CandidateDir: candidateDir, ReleasesDir: releasesDir,
		TargetVersion: "p04b-v1.1", Approval: approval, Regression: regression, Critic: critic,
		Gate: dto.PolicyOptimizerGatePolicy{
			Version: "p04b-gate-v1", MinimumUnsafeResolved: 18,
			MaximumUnsafeQuarantine: 2, MaximumHiddenQuarantine: 12,
		},
	})
	if err != nil {
		t.Fatalf("ReleasePolicy() error = %v", err)
	}
	if release.Version != "p04b-v1.1" || release.SHA256 == "" {
		t.Fatalf("release = %+v", release)
	}
	if _, err := service.VerifyPolicyBundle(release.Path); err != nil {
		t.Fatalf("VerifyPolicyBundle(release) error = %v", err)
	}
}

// TestPolicyOptimizerReleaseRejectsInvalidPrerequisites 验证 blocker/hash/collision 全部拒绝。
func TestPolicyOptimizerReleaseRejectsInvalidPrerequisites(t *testing.T) {
	candidateDir := compiledPolicyOptimizerCandidate(t)
	bundle, err := service.VerifyPolicyBundle(candidateDir)
	if err != nil {
		t.Fatalf("VerifyPolicyBundle() error = %v", err)
	}
	candidate := dto.PolicyOptimizerCandidate{Version: bundle.ReleaseVersion, Path: candidateDir, SHA256: bundle.AggregateHash}
	regression := policyOptimizerRegressionReportWithGates(t, 18, 2, false)
	critic := dto.PolicyOptimizerCritique{ID: "CT:1", ProposalID: "PR:1", Verdict: "accept"}
	approval, err := service.CreatePolicyApproval(candidate, regression, critic, "reviewer-1", time.Now().UTC())
	if err != nil {
		t.Fatalf("CreatePolicyApproval() error = %v", err)
	}
	baseConfig := service.PolicyOptimizerReleaseConfig{
		Candidate: candidate, CandidateDir: candidateDir, ReleasesDir: filepath.Join(t.TempDir(), "releases"),
		TargetVersion: "p04b-v1.1", Approval: approval, Regression: regression, Critic: critic,
		Gate: dto.PolicyOptimizerGatePolicy{
			Version: "p04b-gate-v1", MinimumUnsafeResolved: 18,
			MaximumUnsafeQuarantine: 2, MaximumHiddenQuarantine: 12,
		},
	}
	blocked := baseConfig
	blocked.Critic.Verdict = "block"
	if _, err := service.ReleasePolicy(context.Background(), blocked); err == nil {
		t.Fatal("ReleasePolicy() expected critic block error, got nil")
	}
	mismatch := baseConfig
	mismatch.Approval.SubjectSHA256 = hashForPolicyOptimizerTest("wrong")
	if _, err := service.ReleasePolicy(context.Background(), mismatch); err == nil {
		t.Fatal("ReleasePolicy() expected approval mismatch error, got nil")
	}
	gateMismatch := baseConfig
	gateMismatch.Approval.RelatedSHA256 = map[string]string{
		"candidate":         baseConfig.Approval.RelatedSHA256["candidate"],
		"regression_report": baseConfig.Approval.RelatedSHA256["regression_report"],
		"gate_report":       hashForPolicyOptimizerTest("wrong-gate"),
		"critic_report":     baseConfig.Approval.RelatedSHA256["critic_report"],
	}
	if _, err := service.ReleasePolicy(context.Background(), gateMismatch); err == nil {
		t.Fatal("ReleasePolicy() expected gate hash mismatch error, got nil")
	}
	if _, err := service.ReleasePolicy(context.Background(), baseConfig); err != nil {
		t.Fatalf("ReleasePolicy(first) error = %v", err)
	}
	if _, err := service.ReleasePolicy(context.Background(), baseConfig); err == nil {
		t.Fatal("ReleasePolicy() expected destination collision error, got nil")
	}
}
