package main

import (
	"context"
	"strings"
	"testing"
)

// TestPolicyOptimizerMainDispatch 验证 policy-optimizer 子命令在 legacy 解析前分发。
func TestPolicyOptimizerMainDispatch(t *testing.T) {
	var stdout, stderr strings.Builder
	code := run(context.Background(), []string{
		"policy-optimizer", "validate", "--config", "config/policy-optimizer.example.yaml",
	}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), "validation=PASS") {
		t.Fatalf("run(policy-optimizer validate) = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
