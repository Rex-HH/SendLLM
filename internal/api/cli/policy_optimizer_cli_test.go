package cli_test

import (
	"context"
	"strings"
	"testing"

	"sendllm/internal/api/cli"
)

// TestPolicyOptimizerCLIValidateZeroNetwork 验证 validate 只读配置且不需要网络。
func TestPolicyOptimizerCLIValidateZeroNetwork(t *testing.T) {
	var stdout, stderr strings.Builder
	code := cli.RunPolicyOptimizer(context.Background(), []string{
		"validate", "--config", "../../../config/policy-optimizer.example.yaml",
	}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), "validation=PASS") {
		t.Fatalf("RunPolicyOptimizer(validate) = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

// TestPolicyOptimizerCLICommandFlagMatrix 验证冻结命令和必需 flag 的严格解析。
func TestPolicyOptimizerCLICommandFlagMatrix(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{name: "missing command", args: nil, want: 1},
		{name: "unknown command", args: []string{"unknown"}, want: 1},
		{name: "validate missing config", args: []string{"validate"}, want: 1},
		{name: "inspect missing package", args: []string{"inspect", "--config", "x"}, want: 1},
		{name: "mapping missing approver", args: []string{"mapping-approve", "--config", "x", "--package", "p", "--source", "s", "--mapping", "m"}, want: 1},
		{name: "compile missing change request", args: []string{"compile", "--config", "x", "--policy", "p04b-v1.0"}, want: 1},
		{name: "preview only compile", args: []string{"validate", "--config", "x", "--preview"}, want: 1},
		{name: "gold approve missing approver", args: []string{"gold-approve", "--config", "x", "--candidate-gold", "g"}, want: 1},
		{name: "status missing iteration dir", args: []string{"status"}, want: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr strings.Builder
			if code := cli.RunPolicyOptimizer(context.Background(), test.args, &stdout, &stderr); code != test.want {
				t.Fatalf("RunPolicyOptimizer(%v) = %d, want %d", test.args, code, test.want)
			}
		})
	}
}

// TestPolicyOptimizerCLICancellation 验证取消在配置动作前返回 130。
func TestPolicyOptimizerCLICancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stdout, stderr strings.Builder
	if code := cli.RunPolicyOptimizer(ctx, []string{"validate", "--config", "x"}, &stdout, &stderr); code != 130 {
		t.Fatalf("RunPolicyOptimizer(canceled) = %d, want 130", code)
	}
}
