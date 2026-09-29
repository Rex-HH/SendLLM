package service_test

import (
	"context"
	"sync"
	"testing"

	"sendllm/internal/dto"
	"sendllm/internal/service"
)

// TestPolicyOptimizerRegressionRequiresTwoRotations 验证 Regression 必须保留两次独立轮换。
func TestPolicyOptimizerRegressionRequiresTwoRotations(t *testing.T) {
	rotations := make([]string, 0, 2)
	var rotationMu sync.Mutex
	report, err := service.RunPolicyRegression(context.Background(), service.PolicyOptimizerRegressionConfig{
		BaseReleaseSHA256:      hashForPolicyOptimizerTest("base"),
		CandidateReleaseSHA256: hashForPolicyOptimizerTest("candidate"),
		SuiteVersion:           "suite-v1", GatePolicyVersion: "p04b-gate-v1",
		RotationExecutor: service.PolicyOptimizerRegressionRotationFunc(func(
			_ context.Context,
			rotationID string,
		) (dto.PolicyOptimizerRotationResult, error) {
			rotationMu.Lock()
			rotations = append(rotations, rotationID)
			rotationMu.Unlock()
			return dto.PolicyOptimizerRotationResult{
				RotationID: rotationID, TaskDir: "/tmp/" + rotationID,
				OutputSHA256: hashForPolicyOptimizerTest(rotationID),
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("RunPolicyRegression() error = %v", err)
	}
	if len(report.Rotations) != 2 || report.BaseReleaseSHA256 == "" {
		t.Fatalf("report = %+v", report)
	}
	if len(rotations) != 2 || rotations[0] == rotations[1] {
		t.Fatalf("rotation executor calls = %+v", rotations)
	}
	if _, err := service.RunPolicyRegression(context.Background(), service.PolicyOptimizerRegressionConfig{
		BaseReleaseSHA256:      hashForPolicyOptimizerTest("base"),
		CandidateReleaseSHA256: hashForPolicyOptimizerTest("candidate"),
		SuiteVersion:           "suite-v1", GatePolicyVersion: "p04b-gate-v1",
		Run: func(context.Context) (dto.PolicyOptimizerRegressionObservation, error) {
			return dto.PolicyOptimizerRegressionObservation{
				Rotations: []dto.PolicyOptimizerRotationResult{{RotationID: "rotation-a"}},
			}, nil
		},
	}); err == nil {
		t.Fatal("RunPolicyRegression() expected two-rotation error, got nil")
	}
}

// TestPolicyOptimizerRegressionContractVersioning 验证 supersede 必须递增版本并有 Human Directive。
func TestPolicyOptimizerRegressionContractVersioning(t *testing.T) {
	base := dto.PolicyOptimizerRegressionContract{
		Version: 1, ContractID: "CONTRACT-1", ContractVersion: 1,
		PolicyScope: "p04b", Expected: dto.PolicyOptimizerRegressionExpectation{Label: "safe", MinimumPassCount: 1},
	}
	next := base
	next.ContractVersion = 2
	next.Supersedes = "CONTRACT-1@1"
	if err := service.ValidatePolicyOptimizerRegressionContract(next); err == nil {
		t.Fatal("ValidatePolicyOptimizerRegressionContract() expected missing Human Directive error, got nil")
	}
	next.HumanDirectiveRef = "HD-1"
	if err := service.ValidatePolicyOptimizerRegressionContract(next); err != nil {
		t.Fatalf("ValidatePolicyOptimizerRegressionContract() error = %v", err)
	}
}

// TestPolicyOptimizerGateEvaluateP04BGates 验证 P04-B gate 的零容忍和阈值。
func TestPolicyOptimizerGateEvaluateP04BGates(t *testing.T) {
	gate := dto.PolicyOptimizerGatePolicy{
		Version: "p04b-gate-v1", MinimumUnsafeResolved: 18,
		MaximumUnsafeQuarantine: 2, MaximumHiddenQuarantine: 12,
	}
	passing := policyOptimizerRegressionReportWithGates(t, 18, 2, false)
	if err := service.EvaluateP04BGates(passing, gate); err != nil {
		t.Fatalf("EvaluateP04BGates(pass) error = %v", err)
	}
	driftedGate := gate
	driftedGate.MinimumUnsafeResolved = 19
	if err := service.EvaluateP04BGates(passing, driftedGate); err == nil {
		t.Fatal("EvaluateP04BGates() expected drifted threshold error, got nil")
	}
	tests := []struct {
		name   string
		report dto.PolicyOptimizerRegressionReport
	}{
		{name: "unsafe resolved safe", report: policyOptimizerRegressionReportWithUnsafeSafe(t)},
		{name: "unsafe recall", report: policyOptimizerRegressionReportWithGates(t, 17, 1, false)},
		{name: "unsafe quarantine", report: policyOptimizerRegressionReportWithGates(t, 17, 3, false)},
		{name: "hard negative false unsafe", report: policyOptimizerRegressionReportWithGates(t, 18, 2, true)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := service.EvaluateP04BGates(test.report, gate); err == nil {
				t.Fatal("EvaluateP04BGates() expected failure, got nil")
			}
		})
	}
}

func policyOptimizerRegressionReportWithGates(
	t *testing.T,
	unsafeResolved int,
	unsafeQuarantine int,
	hardNegativeFalseUnsafe bool,
) dto.PolicyOptimizerRegressionReport {
	t.Helper()
	rotation := func(id string) dto.PolicyOptimizerRotationResult {
		cases := make([]dto.PolicyOptimizerRegressionCase, 0, 50)
		remainingUnsafeQuarantine := unsafeQuarantine
		for index := 0; index < 20; index++ {
			predicted := "unsafe"
			quarantined := false
			if index >= unsafeResolved {
				if remainingUnsafeQuarantine > 0 {
					remainingUnsafeQuarantine--
					predicted = ""
					quarantined = true
				} else {
					predicted = "safe"
				}
			}
			cases = append(cases, dto.PolicyOptimizerRegressionCase{
				TraceID: id + "-unsafe-" + string(rune('a'+index)), Suite: "hidden_unsafe",
				GoldLabel: "unsafe", PredictedLabel: predicted, Quarantined: quarantined,
			})
		}
		for index := 0; index < 20; index++ {
			predicted := "safe"
			if hardNegativeFalseUnsafe && index < 1 {
				predicted = "unsafe"
			}
			cases = append(cases, dto.PolicyOptimizerRegressionCase{
				TraceID: id + "-safe-" + string(rune('a'+index)), Suite: "hard_negative",
				GoldLabel: "safe", PredictedLabel: predicted,
			})
		}
		for index := 0; index < 10; index++ {
			cases = append(cases, dto.PolicyOptimizerRegressionCase{
				TraceID: id + "-boundary-" + string(rune('a'+index)), Suite: "boundary",
				GoldLabel: "safe", PredictedLabel: "safe",
			})
		}
		return dto.PolicyOptimizerRotationResult{
			RotationID: id, TaskDir: "/tmp/" + id,
			OutputSHA256: hashForPolicyOptimizerTest(id), Cases: cases,
		}
	}
	return dto.PolicyOptimizerRegressionReport{
		BaseReleaseSHA256:      hashForPolicyOptimizerTest("base"),
		CandidateReleaseSHA256: hashForPolicyOptimizerTest("candidate"),
		SuiteVersion:           "suite-v1", GatePolicyVersion: "p04b-gate-v1",
		Rotations: []dto.PolicyOptimizerRotationResult{rotation("rotation-a"), rotation("rotation-b")},
		Contracts: []dto.PolicyOptimizerContractResult{{ContractID: "CONTRACT-1", Passed: true}},
	}
}

func policyOptimizerRegressionReportWithUnsafeSafe(t *testing.T) dto.PolicyOptimizerRegressionReport {
	t.Helper()
	report := policyOptimizerRegressionReportWithGates(t, 20, 0, false)
	report.Rotations[0].Cases[0].PredictedLabel = "safe"
	return report
}
