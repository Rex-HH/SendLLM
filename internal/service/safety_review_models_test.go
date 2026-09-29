package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"sendllm/internal/dto"
	"sendllm/internal/service"
)

// TestSafetyReviewModelRegistryChainOrder 验证角色链顺序与 Profile/Family 保留。
func TestSafetyReviewModelRegistryChainOrder(t *testing.T) {
	first := service.SafetyReviewModel{Profile: "primary", Family: "family-a", Completer: safetyReviewSuccessCompleter()}
	second := service.SafetyReviewModel{Profile: "fallback", Family: "family-b", Completer: safetyReviewSuccessCompleter()}
	registry, err := service.NewSafetyReviewModelRegistry(map[dto.SafetyReviewRole][]service.SafetyReviewModel{
		dto.SafetyReviewJudgeA: {first, second},
	})
	if err != nil {
		t.Fatalf("NewSafetyReviewModelRegistry() error = %v", err)
	}

	chain := registry.Chain(dto.SafetyReviewJudgeA)
	if len(chain) != 2 {
		t.Fatalf("chain length = %d, want 2", len(chain))
	}
	if chain[0].Profile != "primary" || chain[0].Family != "family-a" ||
		chain[1].Profile != "fallback" || chain[1].Family != "family-b" {
		t.Fatalf("chain = %+v, want primary/fallback metadata preserved", chain)
	}
}

// TestSafetyReviewModelRegistryRejectsInvalidChains 验证缺失角色和空链被拒绝。
func TestSafetyReviewModelRegistryRejectsInvalidChains(t *testing.T) {
	if _, err := service.NewSafetyReviewModelRegistry(nil); err == nil {
		t.Fatal("NewSafetyReviewModelRegistry(nil) returned nil error")
	}
	empty := map[dto.SafetyReviewRole][]service.SafetyReviewModel{
		dto.SafetyReviewJudgeA: {},
	}
	if _, err := service.NewSafetyReviewModelRegistry(empty); err == nil {
		t.Fatal("NewSafetyReviewModelRegistry(empty chain) returned nil error")
	}
}

// safetyReviewOutcome 表示一次脚本化模型响应。
type safetyReviewOutcome struct {
	response dto.CompletionResponse
	err      error
}

// safetyReviewTestCompleter 记录请求并按脚本返回结果。
type safetyReviewTestCompleter struct {
	mu      sync.Mutex
	calls   []dto.CompletionRequest
	script  []safetyReviewOutcome
	blockOn context.Context
}

// Complete 记录请求并消费脚本中的下一个结果。
func (c *safetyReviewTestCompleter) Complete(
	ctx context.Context,
	req dto.CompletionRequest,
) (dto.CompletionResponse, error) {
	c.mu.Lock()
	c.calls = append(c.calls, req)
	index := len(c.calls) - 1
	c.mu.Unlock()

	if c.blockOn != nil {
		select {
		case <-ctx.Done():
			return dto.CompletionResponse{}, ctx.Err()
		case <-c.blockOn.Done():
		}
	}
	if index >= len(c.script) {
		return dto.CompletionResponse{}, errors.New("unexpected safety review model call")
	}
	outcome := c.script[index]
	return outcome.response, outcome.err
}

// safetyReviewSuccessCompleter 构造总是成功的测试模型。
func safetyReviewSuccessCompleter() *safetyReviewTestCompleter {
	return &safetyReviewTestCompleter{
		script: []safetyReviewOutcome{
			{response: dto.CompletionResponse{Content: []byte(`{"ok":true}`)}},
		},
	}
}
