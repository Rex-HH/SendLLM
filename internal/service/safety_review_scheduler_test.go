package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"sendllm/internal/dao"
	"sendllm/internal/dto"
	"sendllm/internal/lib/limiter"
	"sendllm/internal/service"
)

// TestSafetyReviewTransitionGateCoversShortcutMatrix 验证初始三阶段终态的 shortcut 规则。
func TestSafetyReviewTransitionGateCoversShortcutMatrix(t *testing.T) {
	safeJudge := &dto.SafetyReviewJudgment{Verdict: "safe", EvidenceComplete: true}
	unsafeJudge := &dto.SafetyReviewJudgment{Verdict: "unsafe", EvidenceComplete: true}
	completeRouter := &dto.SafetyReviewRoute{CoverageComplete: true}
	incompleteRouter := &dto.SafetyReviewRoute{CoverageComplete: false}
	candidateRouter := &dto.SafetyReviewRoute{
		CoverageComplete:       true,
		AttackDomainCandidates: []dto.SafetyReviewCandidate{{Category: "ethnic_discrimination"}},
	}

	tests := []struct {
		name string
		a    *dto.SafetyReviewJudgment
		b    *dto.SafetyReviewJudgment
		r    *dto.SafetyReviewRoute
		dg   bool
		want string
	}{
		{name: "safe shortcut", a: safeJudge, b: safeJudge, r: completeRouter, want: "resolved_safe"},
		{name: "a only safe", a: safeJudge, b: unsafeJudge, r: completeRouter, want: "pending_arbiter"},
		{name: "b only safe", a: unsafeJudge, b: safeJudge, r: completeRouter, want: "pending_arbiter"},
		{name: "router incomplete", a: safeJudge, b: safeJudge, r: incompleteRouter, want: "pending_arbiter"},
		{name: "router candidate", a: safeJudge, b: safeJudge, r: candidateRouter, want: "awaiting_experts"},
		{name: "degraded independence", a: safeJudge, b: safeJudge, r: completeRouter, dg: true, want: "pending_arbiter"},
		{name: "missing judge", a: safeJudge, b: nil, r: completeRouter, want: "pending_arbiter"},
		{name: "missing router", a: safeJudge, b: safeJudge, r: nil, want: "pending_arbiter"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := service.SafetyReviewTransitionGate(test.a, test.b, test.r, test.dg)
			if got != test.want {
				t.Fatalf("gate = %q, want %q", got, test.want)
			}
		})
	}
}

// TestSafetyReviewTransitionAnyCandidateCreatesExperts 验证任一候选都会创建对应 Expert
// 并进入等待状态。
func TestSafetyReviewTransitionAnyCandidateCreatesExperts(t *testing.T) {
	store := newSafetyReviewFakeStore("trace-1")
	caller := newSafetyReviewRoutingCaller(t, oneCandidateRouterJSON(), "")
	runner := newSafetyReviewTestRunner(t, store, caller, map[dto.SafetyReviewRole]int{
		dto.SafetyReviewJudgeA: 1,
		dto.SafetyReviewJudgeB: 1,
		dto.SafetyReviewRouter: 1,
	})
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	item := store.items["trace-1"]
	if item.state != "awaiting_experts" {
		t.Fatalf("item state = %q, want awaiting_experts", item.state)
	}
	if _, ok := item.stages["expert:attack_domain:ethnic_discrimination"]; !ok {
		t.Fatal("router candidate did not create its Expert stage")
	}
}

// TestSafetyReviewTransitionUncertainRoutingStillRunsExperts 验证 Router 覆盖不确定时
// 候选仍先进入 Expert。
func TestSafetyReviewTransitionUncertainRoutingStillRunsExperts(t *testing.T) {
	store := newSafetyReviewFakeStore("trace-1")
	routerJSON := []byte(`{"features":[],"attack_method_candidates":[],` +
		`"attack_domain_candidates":[{"category":"ethnic_discrimination",` +
		`"evidence_ids":[],"reason":"召回"}],"coverage_complete":false}`)
	caller := newSafetyReviewRoutingCaller(t, routerJSON, "")
	policy := loadSafetyReviewPolicyForRouting(t)
	caller.script = func(_ context.Context, req service.SafetyReviewCallRequest) (
		service.SafetyReviewCallResult, error,
	) {
		if req.Role == dto.SafetyReviewExpert {
			return service.SafetyReviewCallResult{
				Content: safetyReviewEstablishedExpertContent(t, policy, req.StageKey),
			}, nil
		}
		if req.Role == dto.SafetyReviewRouter {
			return service.SafetyReviewCallResult{Content: append([]byte(nil), routerJSON...)}, nil
		}
		return service.SafetyReviewCallResult{Content: safetyReviewRoleContent(req.Role)}, nil
	}
	runner := newSafetyReviewTestRunner(t, store, caller, map[dto.SafetyReviewRole]int{
		dto.SafetyReviewJudgeA: 1,
		dto.SafetyReviewJudgeB: 1,
		dto.SafetyReviewRouter: 1,
		dto.SafetyReviewExpert: 1,
	})
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	item := store.items["trace-1"]
	if item.state != "pending_arbiter" || item.stages["arbiter"] == nil {
		t.Fatalf("item/stage = %q/%v, want pending_arbiter/arbiter", item.state, item.stages["arbiter"])
	}
	if item.stages["expert:attack_domain:ethnic_discrimination"] == nil {
		t.Fatal("uncertain Router did not create its Expert stage")
	}
}

// TestSafetyReviewTransitionIncompleteCoverageCreatesArbiter 验证 coverage incomplete 不能直接 Safe。
func TestSafetyReviewTransitionIncompleteCoverageCreatesArbiter(t *testing.T) {
	store := newSafetyReviewFakeStore("trace-1")
	routerJSON := []byte(`{"features":[],"attack_method_candidates":[],` +
		`"attack_domain_candidates":[],"coverage_complete":false}`)
	caller := newSafetyReviewRoutingCaller(t, routerJSON, "")
	runner := newSafetyReviewTestRunner(t, store, caller, map[dto.SafetyReviewRole]int{
		dto.SafetyReviewJudgeA: 1,
		dto.SafetyReviewJudgeB: 1,
		dto.SafetyReviewRouter: 1,
	})
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	item := store.items["trace-1"]
	if item.state != "pending_arbiter" {
		t.Fatalf("item state = %q, want pending_arbiter", item.state)
	}
	if _, ok := item.stages["arbiter"]; !ok {
		t.Fatal("incomplete Router coverage did not create an arbiter stage")
	}
	if len(caller.calls) != 3 {
		t.Fatalf("call count = %d, want 3", len(caller.calls))
	}
}

// TestSafetyReviewTransitionPolicyGapCannotSafeShortcut 验证未知类别必须进入 Arbiter/Quarantine 路径。
func TestSafetyReviewTransitionPolicyGapCannotSafeShortcut(t *testing.T) {
	store := newSafetyReviewFakeStore("trace-1")
	routerJSON := []byte(`{"features":[],"attack_method_candidates":[],` +
		`"attack_domain_candidates":[{"category":"not_in_bundle","evidence_ids":[],` +
		`"reason":"未知类别"}],"coverage_complete":true}`)
	caller := newSafetyReviewRoutingCaller(t, routerJSON, "")
	runner := newSafetyReviewTestRunner(t, store, caller, map[dto.SafetyReviewRole]int{
		dto.SafetyReviewJudgeA: 1,
		dto.SafetyReviewJudgeB: 1,
		dto.SafetyReviewRouter: 1,
	})
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	item := store.items["trace-1"]
	if item.state != "pending_arbiter" {
		t.Fatalf("item state = %q, want pending_arbiter", item.state)
	}
	if _, ok := item.stages["arbiter"]; !ok {
		t.Fatal("policy coverage gap did not create an arbiter stage")
	}
}

// TestSafetyReviewTransitionSafeShortcutDoesNotCallExperts 验证完整双 Safe 零候选 shortcut
// 不产生下游模型调用。
func TestSafetyReviewTransitionSafeShortcutDoesNotCallExperts(t *testing.T) {
	store := newSafetyReviewFakeStore("trace-1")
	caller := newSafetyReviewRoutingCaller(t, safetyReviewRoleContent(dto.SafetyReviewRouter), "")
	runner := newSafetyReviewTestRunner(t, store, caller, map[dto.SafetyReviewRole]int{
		dto.SafetyReviewJudgeA: 1,
		dto.SafetyReviewJudgeB: 1,
		dto.SafetyReviewRouter: 1,
	})
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := store.items["trace-1"].state; got != "resolved_safe" {
		t.Fatalf("item state = %q, want resolved_safe", got)
	}
	if len(caller.calls) != 3 {
		t.Fatalf("call count = %d, want 3", len(caller.calls))
	}
	for _, stage := range store.items["trace-1"].stages {
		if stage.role == dto.SafetyReviewExpert || stage.role == dto.SafetyReviewArbiter {
			t.Fatalf("shortcut created downstream stage %s", stage.role)
		}
	}
}

// TestSafetyReviewTransitionRunsSixExpertsInParallel 验证 3+3 候选会并行执行且每个候选只调用一次。
func TestSafetyReviewTransitionRunsSixExpertsInParallel(t *testing.T) {
	store := newSafetyReviewFakeStore("trace-1")
	routerJSON := []byte(`{"features":[],` +
		`"attack_method_candidates":[{"category":"prompt_injection","evidence_ids":[],"reason":"召回"},` +
		`{"category":"jailbreak","evidence_ids":[],"reason":"召回"},` +
		`{"category":"encoding_obfuscation","evidence_ids":[],"reason":"召回"}],` +
		`"attack_domain_candidates":[{"category":"ethnic_discrimination","evidence_ids":[],"reason":"召回"},` +
		`{"category":"religious_discrimination","evidence_ids":[],"reason":"召回"},` +
		`{"category":"nationality_discrimination","evidence_ids":[],"reason":"召回"}],` +
		`"coverage_complete":true}`)
	caller := newSafetyReviewRoutingCaller(t, routerJSON, "")
	entered := make(chan string, 6)
	allEntered := make(chan struct{})
	release := make(chan struct{})
	var mu sync.Mutex
	active := 0
	maxActive := 0
	caller.before = func(_ context.Context, req service.SafetyReviewCallRequest) {
		if req.Role != dto.SafetyReviewExpert {
			return
		}
		mu.Lock()
		active++
		if active > maxActive {
			maxActive = active
		}
		if active == 6 {
			close(allEntered)
		}
		mu.Unlock()
		entered <- req.StageKey
		<-release
		mu.Lock()
		active--
		mu.Unlock()
	}
	runner := newSafetyReviewTestRunner(t, store, caller, map[dto.SafetyReviewRole]int{
		dto.SafetyReviewJudgeA: 1,
		dto.SafetyReviewJudgeB: 1,
		dto.SafetyReviewRouter: 1,
		dto.SafetyReviewExpert: 6,
	})
	done := make(chan error, 1)
	go func() {
		_, runErr := runner.Run(context.Background())
		done <- runErr
	}()
	select {
	case <-allEntered:
	case err := <-done:
		t.Fatalf("Run() returned before six Experts entered barrier: %v", err)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("six Experts did not enter parallel barrier")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(caller.calls) != 9 {
		t.Fatalf("call count = %d, want 9", len(caller.calls))
	}
	seen := make(map[string]int)
	for _, call := range caller.calls {
		if call.Role == dto.SafetyReviewExpert {
			seen[call.StageKey]++
		}
	}
	if len(seen) != 6 {
		t.Fatalf("expert stage count = %d, want 6", len(seen))
	}
	for stageKey, count := range seen {
		if count != 1 {
			t.Fatalf("expert %s call count = %d, want 1", stageKey, count)
		}
	}
	mu.Lock()
	overlap := maxActive
	mu.Unlock()
	if overlap != 6 {
		t.Fatalf("max expert overlap = %d, want 6", overlap)
	}
	item := store.items["trace-1"]
	if item.state != "pending_arbiter" || item.stages["arbiter"] == nil {
		t.Fatalf("item/stage = %q/%v, want pending_arbiter/arbiter", item.state, item.stages["arbiter"])
	}
}

// TestSafetyReviewTransitionAllExcludedOnlyPreparesArbiter 验证全部排除只能为后续 Arbiter 提供条件。
func TestSafetyReviewTransitionAllExcludedOnlyPreparesArbiter(t *testing.T) {
	store := newSafetyReviewFakeStore("trace-1")
	caller := newSafetyReviewRoutingCaller(t, oneCandidateRouterJSON(), "")
	runner := newSafetyReviewTestRunner(t, store, caller, map[dto.SafetyReviewRole]int{
		dto.SafetyReviewJudgeA: 1,
		dto.SafetyReviewJudgeB: 1,
		dto.SafetyReviewRouter: 1,
		dto.SafetyReviewExpert: 1,
	})
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	item := store.items["trace-1"]
	if item.state != "pending_arbiter" {
		t.Fatalf("item state = %q, want pending_arbiter", item.state)
	}
	if item.stages["arbiter"] == nil {
		t.Fatal("all-excluded Expert did not create an arbiter stage")
	}
	for _, completion := range store.completed {
		if completion.Decision != nil {
			t.Fatal("feat-020 must not write a final decision after Expert exclusion")
		}
	}
}

// TestSafetyReviewTransitionMixedEstablishedCategoriesOnlyPrepareArbiter 验证混合 established
// 类别只形成 Arbiter 前置条件而不提前写最终决策。
func TestSafetyReviewTransitionMixedEstablishedCategoriesOnlyPrepareArbiter(t *testing.T) {
	store := newSafetyReviewFakeStore("trace-1")
	routerJSON := []byte(`{"features":[],` +
		`"attack_method_candidates":[{"category":"prompt_injection","evidence_ids":[],` +
		`"reason":"召回"}],"attack_domain_candidates":[{"category":"ethnic_discrimination",` +
		`"evidence_ids":[],"reason":"召回"}],"coverage_complete":true}`)
	caller := newSafetyReviewRoutingCaller(t, routerJSON, "")
	runner := newSafetyReviewTestRunner(t, store, caller, map[dto.SafetyReviewRole]int{
		dto.SafetyReviewJudgeA: 1,
		dto.SafetyReviewJudgeB: 1,
		dto.SafetyReviewRouter: 1,
		dto.SafetyReviewExpert: 2,
	})
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	item := store.items["trace-1"]
	if item.state != "pending_arbiter" || item.stages["arbiter"] == nil {
		t.Fatalf("item/stage = %q/%v, want pending_arbiter/arbiter", item.state, item.stages["arbiter"])
	}
	for _, stageKey := range []string{
		"expert:attack_method:prompt_injection",
		"expert:attack_domain:ethnic_discrimination",
	} {
		if item.stages[stageKey] == nil {
			t.Fatalf("mixed routing lacked Expert stage %s", stageKey)
		}
	}
	for _, completion := range store.completed {
		if completion.Decision != nil {
			t.Fatal("feat-020 wrote a final decision for mixed established categories")
		}
	}
}

// TestSafetyReviewTransitionExpertQuotaPriority 验证共享组中 Expert 获取配额优先于 Router。
func TestSafetyReviewTransitionExpertQuotaPriority(t *testing.T) {
	quota, err := limiter.NewSafetyReviewQuota(limiter.SafetyReviewQuotaConfig{
		Roles: map[string]limiter.SafetyReviewRoleQuota{
			"judge_a": {Concurrency: 1},
			"judge_b": {Concurrency: 1},
			"router":  {Concurrency: 1},
			"expert":  {Concurrency: 1},
		},
		Groups: map[string]limiter.SafetyReviewGroupQuota{
			"minimax": {Concurrency: 1},
		},
	})
	if err != nil {
		t.Fatalf("NewSafetyReviewQuota() error = %v", err)
	}
	holder, err := quota.Acquire(context.Background(), "judge_a", "minimax", 0)
	if err != nil {
		t.Fatalf("holder Acquire() error = %v", err)
	}
	store := newSafetyReviewFakeStore("trace-1")
	store.items["trace-1"].state = "awaiting_experts"
	store.items["trace-1"].stages = map[string]*fakeSafetyReviewStage{
		"router": {role: dto.SafetyReviewRouter, state: "pending"},
		"expert:attack_domain:ethnic_discrimination": {
			role: dto.SafetyReviewExpert, axis: "attack_domain",
			category: "ethnic_discrimination", state: "pending",
		},
	}
	caller := newSafetyReviewRoutingCaller(t, oneCandidateRouterJSON(), "")
	runner, err := service.NewSafetyReviewRunner(service.SafetyReviewRunnerConfig{
		TaskID:     "task-1",
		Store:      store,
		Caller:     caller,
		Quota:      quota,
		QuotaGroup: "minimax",
		Workers: map[dto.SafetyReviewRole]int{
			dto.SafetyReviewJudgeA: 1, dto.SafetyReviewJudgeB: 1,
			dto.SafetyReviewRouter: 1, dto.SafetyReviewExpert: 1,
		},
		Policy:          loadSafetyReviewPolicyForRouting(t),
		ModelProfile:    "profile",
		ModelFamily:     "family",
		APIKeyEnv:       "TEST_KEY",
		ShutdownTimeout: time.Second,
		Now:             time.Now,
		BuildRequest: func(work dao.SafetyReviewStageWork) (service.SafetyReviewCallRequest, error) {
			return service.SafetyReviewCallRequest{
				TaskID: work.TaskID, TraceID: work.TraceID, StageKey: work.StageKey,
				Role: dto.SafetyReviewRole(work.Role), Messages: []dto.Message{
					{Role: "user", Content: "fixed"},
				}, Schema: []byte(`{}`), Mode: "json_object",
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewSafetyReviewRunner() error = %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, runErr := runner.Run(context.Background())
		done <- runErr
	}()
	time.Sleep(20 * time.Millisecond)
	holder()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Run() did not finish after shared quota release")
	}
	first := ""
	if len(caller.calls) != 0 {
		first = string(caller.calls[0].Role)
	}
	if first != "expert" {
		t.Fatalf("first shared quota acquisition = %q, want expert", first)
	}
}

// TestSafetyReviewRunnerRunsInitialStagesInParallel 验证 A/B/Router 同一条目可同时执行。
func TestSafetyReviewRunnerRunsInitialStagesInParallel(t *testing.T) {
	store := newSafetyReviewFakeStore("trace-1")
	caller := newSafetyReviewFakeCaller(
		func(ctx context.Context, req service.SafetyReviewCallRequest) (service.SafetyReviewCallResult, error) {
			return service.SafetyReviewCallResult{Content: safetyReviewRoleContent(req.Role)}, nil
		},
	)
	entered := make(chan dto.SafetyReviewRole, 3)
	allEntered := make(chan struct{})
	release := make(chan struct{})
	var enteredCount int
	var barrierMu sync.Mutex
	caller.before = func(_ context.Context, req service.SafetyReviewCallRequest) {
		entered <- req.Role
		barrierMu.Lock()
		enteredCount++
		ready := enteredCount == 3
		barrierMu.Unlock()
		if ready {
			close(allEntered)
			close(release)
		}
		<-release
	}
	runner := newSafetyReviewTestRunner(t, store, caller, map[dto.SafetyReviewRole]int{
		dto.SafetyReviewJudgeA: 1,
		dto.SafetyReviewJudgeB: 1,
		dto.SafetyReviewRouter: 1,
	})

	done := make(chan error, 1)
	go func() {
		_, err := runner.Run(context.Background())
		done <- err
	}()
	select {
	case <-time.After(200 * time.Millisecond):
		t.Fatal("A/B/Router did not overlap within 200ms")
	case <-allEntered:
	case err := <-done:
		t.Fatalf("Run() returned before parallel barrier: %v", err)
	}
	seen := make(map[dto.SafetyReviewRole]bool, 3)
	for range 3 {
		role := <-entered
		seen[role] = true
	}
	for _, role := range []dto.SafetyReviewRole{dto.SafetyReviewJudgeA, dto.SafetyReviewJudgeB, dto.SafetyReviewRouter} {
		if !seen[role] {
			t.Fatalf("role %s did not enter parallel barrier", role)
		}
	}
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

// TestSafetyReviewRunnerHasNoDatasetBarrier 验证第二条记录不必等待第一条记录全部完成。
func TestSafetyReviewRunnerHasNoDatasetBarrier(t *testing.T) {
	store := newSafetyReviewFakeStore("trace-1", "trace-2")
	caller := newSafetyReviewFakeCaller(
		func(_ context.Context, req service.SafetyReviewCallRequest) (service.SafetyReviewCallResult, error) {
			return service.SafetyReviewCallResult{Content: safetyReviewRoleContent(req.Role)}, nil
		},
	)
	firstA := make(chan struct{})
	secondA := make(chan struct{})
	caller.before = func(_ context.Context, req service.SafetyReviewCallRequest) {
		if req.TraceID == "trace-1" && req.Role == dto.SafetyReviewJudgeA {
			close(firstA)
			<-secondA
		}
		if req.TraceID == "trace-2" && req.Role == dto.SafetyReviewJudgeA {
			close(secondA)
			<-firstA
		}
	}
	runner := newSafetyReviewTestRunner(t, store, caller, map[dto.SafetyReviewRole]int{
		dto.SafetyReviewJudgeA: 2,
		dto.SafetyReviewJudgeB: 1,
		dto.SafetyReviewRouter: 1,
	})
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	select {
	case <-firstA:
	default:
		t.Fatal("first item judge A did not run")
	}
	select {
	case <-secondA:
	default:
		t.Fatal("second item judge A did not run")
	}
}

// TestSafetyReviewRunnerResumeSkipsSucceededStage 验证恢复时不会重复调用已成功阶段。
func TestSafetyReviewRunnerResumeSkipsSucceededStage(t *testing.T) {
	store := newSafetyReviewFakeStore("trace-1")
	store.items["trace-1"].stages["judge:a"].state = "succeeded"
	store.items["trace-1"].stages["judge:a"].result = safetyReviewRoleContent(dto.SafetyReviewJudgeA)
	caller := newSafetyReviewFakeCaller(
		func(_ context.Context, req service.SafetyReviewCallRequest) (service.SafetyReviewCallResult, error) {
			return service.SafetyReviewCallResult{Content: safetyReviewRoleContent(req.Role)}, nil
		},
	)
	runner := newSafetyReviewTestRunner(t, store, caller, map[dto.SafetyReviewRole]int{
		dto.SafetyReviewJudgeA: 1,
		dto.SafetyReviewJudgeB: 1,
		dto.SafetyReviewRouter: 1,
	})
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	for _, call := range caller.calls {
		if call.TraceID == "trace-1" && call.Role == dto.SafetyReviewJudgeA {
			t.Fatal("succeeded judge:a stage was called again")
		}
	}
	if len(caller.calls) != 2 {
		t.Fatalf("call count = %d, want 2", len(caller.calls))
	}
	if got := store.items["trace-1"].state; got != "resolved_safe" {
		t.Fatalf("item state after resumed initial completion = %q, want resolved_safe", got)
	}
}

// TestSafetyReviewRunnerResumePreservesStoredDegradation 验证恢复后保留已持久化的独立性降级。
func TestSafetyReviewRunnerResumePreservesStoredDegradation(t *testing.T) {
	store := newSafetyReviewFakeStore("trace-1")
	store.items["trace-1"].independenceDegraded = true
	store.items["trace-1"].stages["judge:a"].state = "succeeded"
	store.items["trace-1"].stages["judge:a"].result = safetyReviewRoleContent(dto.SafetyReviewJudgeA)
	caller := newSafetyReviewFakeCaller(
		func(_ context.Context, req service.SafetyReviewCallRequest) (service.SafetyReviewCallResult, error) {
			return service.SafetyReviewCallResult{Content: safetyReviewRoleContent(req.Role)}, nil
		},
	)
	runner := newSafetyReviewTestRunner(t, store, caller, map[dto.SafetyReviewRole]int{
		dto.SafetyReviewJudgeA: 1,
		dto.SafetyReviewJudgeB: 1,
		dto.SafetyReviewRouter: 1,
	})
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := store.items["trace-1"].state; got != "pending_arbiter" {
		t.Fatalf("item state after degraded resume = %q, want pending_arbiter", got)
	}
}

// TestSafetyReviewRunnerPersistsValidationError 验证模型结构化输出失败时记录脱敏校验错误。
func TestSafetyReviewRunnerPersistsValidationError(t *testing.T) {
	store := newSafetyReviewFakeStore("trace-1")
	caller := newSafetyReviewFakeCaller(
		func(_ context.Context, req service.SafetyReviewCallRequest) (service.SafetyReviewCallResult, error) {
			if req.Role != dto.SafetyReviewJudgeA {
				return service.SafetyReviewCallResult{Content: safetyReviewRoleContent(req.Role)}, nil
			}
			return service.SafetyReviewCallResult{Content: []byte(`{"verdict":"safe"}`)}, nil
		},
	)
	runner := newSafetyReviewTestRunner(t, store, caller, map[dto.SafetyReviewRole]int{
		dto.SafetyReviewJudgeA: 1, dto.SafetyReviewJudgeB: 1, dto.SafetyReviewRouter: 1,
		dto.SafetyReviewExpert: 1, dto.SafetyReviewArbiter: 1,
	})
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	var completion *dao.SafetyReviewStageCompletion
	for i := range store.completed {
		if store.completed[i].StageKey == "judge:a" {
			completion = &store.completed[i]
		}
	}
	if completion == nil {
		t.Fatal("judge:a completion was not recorded")
	}
	got := string(completion.Attempt.ValidationError)
	if !strings.Contains(got, "schema") {
		t.Fatalf("validation error = %q, want schema detail", got)
	}
	if completion.ErrorSummary != "model result could not be parsed or validated" {
		t.Fatalf("stage summary = %q", completion.ErrorSummary)
	}
}

// TestSafetyReviewRunnerRequestsAreBlind 验证 Judge 与 Router 请求互不可见。
func TestSafetyReviewRunnerRequestsAreBlind(t *testing.T) {
	store := newSafetyReviewFakeStore("trace-1")
	caller := newSafetyReviewFakeCaller(
		func(_ context.Context, req service.SafetyReviewCallRequest) (service.SafetyReviewCallResult, error) {
			return service.SafetyReviewCallResult{Content: safetyReviewRoleContent(req.Role)}, nil
		},
	)
	runner := newSafetyReviewTestRunner(t, store, caller, map[dto.SafetyReviewRole]int{
		dto.SafetyReviewJudgeA: 1,
		dto.SafetyReviewJudgeB: 1,
		dto.SafetyReviewRouter: 1,
	})
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	for _, call := range caller.calls {
		content := call.Messages[0].Content
		want := "prompt-trace-1-" + string(call.Role) + " response-trace-1-" + string(call.Role)
		if content != want {
			t.Fatalf("request content = %q, want %q", content, want)
		}
		if len(call.Messages) != 1 {
			t.Fatalf("message count = %d, want 1", len(call.Messages))
		}
		switch call.Role {
		case dto.SafetyReviewJudgeA:
			if schedulerContainsAny(content, "router", "judge_b") {
				t.Fatalf("judge request contains another initial role: %s", content)
			}
		case dto.SafetyReviewJudgeB:
			if schedulerContainsAny(content, "router", "judge_a") {
				t.Fatalf("judge request contains another initial role: %s", content)
			}
		case dto.SafetyReviewRouter:
			if schedulerContainsAny(content, "judge_a", "judge_b") {
				t.Fatalf("router request contains judge output: %s", content)
			}
		}
	}
}

// TestSafetyReviewRunnerOrdinaryFailureDoesNotCancelPeers 验证单阶段失败不取消其他阶段。
func TestSafetyReviewRunnerOrdinaryFailureDoesNotCancelPeers(t *testing.T) {
	store := newSafetyReviewFakeStore("trace-1")
	caller := newSafetyReviewFakeCaller(
		func(_ context.Context, req service.SafetyReviewCallRequest) (service.SafetyReviewCallResult, error) {
			if req.Role == dto.SafetyReviewJudgeA {
				return service.SafetyReviewCallResult{}, errors.New("ordinary stage failure")
			}
			return service.SafetyReviewCallResult{Content: safetyReviewRoleContent(req.Role)}, nil
		},
	)
	runner := newSafetyReviewTestRunner(t, store, caller, map[dto.SafetyReviewRole]int{
		dto.SafetyReviewJudgeA: 1,
		dto.SafetyReviewJudgeB: 1,
		dto.SafetyReviewRouter: 1,
	})
	stats, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if stats.TerminalFailed != 1 {
		t.Fatalf("terminal failed = %d, want 1", stats.TerminalFailed)
	}
	if len(caller.calls) != 3 {
		t.Fatalf("peer calls = %d, want 3", len(caller.calls))
	}
}

// TestSafetyReviewRunnerDegradedFallbackDisallowsShortcut 验证 fallback 降级时不能走 Safe shortcut。
func TestSafetyReviewRunnerDegradedFallbackDisallowsShortcut(t *testing.T) {
	store := newSafetyReviewFakeStore("trace-1")
	caller := newSafetyReviewFakeCaller(
		func(_ context.Context, req service.SafetyReviewCallRequest) (service.SafetyReviewCallResult, error) {
			return service.SafetyReviewCallResult{
				Content:              safetyReviewRoleContent(req.Role),
				IndependenceDegraded: true,
			}, nil
		},
	)
	runner := newSafetyReviewTestRunner(t, store, caller, map[dto.SafetyReviewRole]int{
		dto.SafetyReviewJudgeA: 1,
		dto.SafetyReviewJudgeB: 1,
		dto.SafetyReviewRouter: 1,
	})
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := store.items["trace-1"].state; got != "pending_arbiter" {
		t.Fatalf("item state = %q, want pending_arbiter", got)
	}
}

// TestSafetyReviewRunnerSafeShortcut 验证完整三路安全信号可直接进入 resolved_safe。
func TestSafetyReviewRunnerSafeShortcut(t *testing.T) {
	store := newSafetyReviewFakeStore("trace-1")
	caller := newSafetyReviewFakeCaller(
		func(_ context.Context, req service.SafetyReviewCallRequest) (service.SafetyReviewCallResult, error) {
			return service.SafetyReviewCallResult{Content: safetyReviewRoleContent(req.Role)}, nil
		},
	)
	runner := newSafetyReviewTestRunner(t, store, caller, map[dto.SafetyReviewRole]int{
		dto.SafetyReviewJudgeA: 1,
		dto.SafetyReviewJudgeB: 1,
		dto.SafetyReviewRouter: 1,
	})
	stats, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := store.items["trace-1"].state; got != "resolved_safe" {
		t.Fatalf("item state = %q, want resolved_safe", got)
	}
	if stats.ResolvedSafe != 1 {
		t.Fatalf("resolved safe = %d, want 1", stats.ResolvedSafe)
	}
}

// TestSafetyReviewRunnerSafeShortcutWritesDecision 验证 shortcut 持久化 Typical Safe 决策。
func TestSafetyReviewRunnerSafeShortcutWritesDecision(t *testing.T) {
	store := newSafetyReviewFakeStore("trace-1")
	caller := newSafetyReviewFakeCaller(
		func(_ context.Context, req service.SafetyReviewCallRequest) (service.SafetyReviewCallResult, error) {
			return service.SafetyReviewCallResult{
				Content: safetyReviewRoleContent(req.Role),
				Profile: "profile-" + string(req.Role),
				Family:  "family-" + string(req.Role),
			}, nil
		},
	)
	runner := newSafetyReviewTestRunner(t, store, caller, map[dto.SafetyReviewRole]int{
		dto.SafetyReviewJudgeA: 1,
		dto.SafetyReviewJudgeB: 1,
		dto.SafetyReviewRouter: 1,
	})
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(store.completed) == 0 {
		t.Fatal("store did not record stage completions")
	}
	var decision *dao.SafetyReviewDecisionRecord
	for i := len(store.completed) - 1; i >= 0; i-- {
		if store.completed[i].Decision != nil {
			decision = store.completed[i].Decision
			break
		}
	}
	if decision == nil {
		t.Fatal("safe shortcut did not write a decision")
	}
	if decision.FinalState != "resolved_safe" || decision.Label != "safe" || decision.IsAttack {
		t.Fatalf("decision = %+v, want safe resolved shortcut", decision)
	}
	if decision.CaseType != "typical" || string(decision.AttackMethods) != "[]" ||
		string(decision.AttackDomains) != "[]" || decision.PrimaryRiskType != "" {
		t.Fatalf("decision = %+v, want typical empty-risk shortcut", decision)
	}
}

// TestSafetyReviewRunnerABOnlySafeDoesNotShortcut 验证缺少 Router 成功结果时不能 shortcut。
func TestSafetyReviewRunnerABOnlySafeDoesNotShortcut(t *testing.T) {
	store := newSafetyReviewFakeStore("trace-1")
	caller := newSafetyReviewFakeCaller(
		func(_ context.Context, req service.SafetyReviewCallRequest) (service.SafetyReviewCallResult, error) {
			if req.Role == dto.SafetyReviewRouter {
				return service.SafetyReviewCallResult{}, errors.New("router terminal failure")
			}
			return service.SafetyReviewCallResult{Content: safetyReviewRoleContent(req.Role)}, nil
		},
	)
	runner := newSafetyReviewTestRunner(t, store, caller, map[dto.SafetyReviewRole]int{
		dto.SafetyReviewJudgeA: 1,
		dto.SafetyReviewJudgeB: 1,
		dto.SafetyReviewRouter: 1,
	})
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := store.items["trace-1"].state; got != "pending_arbiter" {
		t.Fatalf("item state = %q, want pending_arbiter", got)
	}
	if len(caller.calls) != 3 {
		t.Fatalf("call count = %d, want 3", len(caller.calls))
	}
}

// TestSafetyReviewRunnerRecordsCallIdentity 验证调用记录包含角色、Profile、阶段与 trace。
func TestSafetyReviewRunnerRecordsCallIdentity(t *testing.T) {
	store := newSafetyReviewFakeStore("trace-1")
	caller := newSafetyReviewFakeCaller(
		func(_ context.Context, req service.SafetyReviewCallRequest) (service.SafetyReviewCallResult, error) {
			return service.SafetyReviewCallResult{
				Content: safetyReviewRoleContent(req.Role),
				Profile: "profile-" + string(req.Role),
				Family:  "family-" + string(req.Role),
			}, nil
		},
	)
	runner := newSafetyReviewTestRunner(t, store, caller, map[dto.SafetyReviewRole]int{
		dto.SafetyReviewJudgeA: 1,
		dto.SafetyReviewJudgeB: 1,
		dto.SafetyReviewRouter: 1,
	})
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(caller.calls) != 3 || len(caller.results) != 3 {
		t.Fatalf("calls/results = %d/%d, want 3/3", len(caller.calls), len(caller.results))
	}
	for index, call := range caller.calls {
		if call.TaskID == "" || call.TraceID == "" || call.StageKey == "" || call.Role == "" {
			t.Fatalf("call %d lacks identity fields: %+v", index, call)
		}
		if caller.results[index].Profile == "" || caller.results[index].Family == "" {
			t.Fatalf("result %d lacks model identity: %+v", index, caller.results[index])
		}
	}
}

// TestSafetyReviewRunnerOneJudgeTerminalFailedDoesNotShortcut 验证单个 Judge 终态失败禁止 shortcut。
func TestSafetyReviewRunnerOneJudgeTerminalFailedDoesNotShortcut(t *testing.T) {
	store := newSafetyReviewFakeStore("trace-1")
	caller := newSafetyReviewFakeCaller(
		func(_ context.Context, req service.SafetyReviewCallRequest) (service.SafetyReviewCallResult, error) {
			if req.Role == dto.SafetyReviewJudgeA {
				return service.SafetyReviewCallResult{}, errors.New("judge terminal failure")
			}
			return service.SafetyReviewCallResult{Content: safetyReviewRoleContent(req.Role)}, nil
		},
	)
	runner := newSafetyReviewTestRunner(t, store, caller, map[dto.SafetyReviewRole]int{
		dto.SafetyReviewJudgeA: 1,
		dto.SafetyReviewJudgeB: 1,
		dto.SafetyReviewRouter: 1,
	})
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := store.items["trace-1"].state; got != "pending_arbiter" {
		t.Fatalf("item state = %q, want pending_arbiter", got)
	}
}

// TestSafetyReviewRunnerEvidenceIncompleteDoesNotShortcut 验证证据不完整的双 Safe 禁止 shortcut。
func TestSafetyReviewRunnerEvidenceIncompleteDoesNotShortcut(t *testing.T) {
	store := newSafetyReviewFakeStore("trace-1")
	caller := newSafetyReviewFakeCaller(
		func(_ context.Context, req service.SafetyReviewCallRequest) (service.SafetyReviewCallResult, error) {
			content := safetyReviewRoleContent(req.Role)
			if req.Role == dto.SafetyReviewJudgeA || req.Role == dto.SafetyReviewJudgeB {
				content = []byte(
					`{"verdict":"safe","evidence_complete":false,` +
						`"evidence":[],"attack_methods":[],"attack_domains":[],` +
						`"exclusions":[],"rationale":"safe"}`,
				)
			}
			return service.SafetyReviewCallResult{Content: content}, nil
		},
	)
	runner := newSafetyReviewTestRunner(t, store, caller, map[dto.SafetyReviewRole]int{
		dto.SafetyReviewJudgeA: 1,
		dto.SafetyReviewJudgeB: 1,
		dto.SafetyReviewRouter: 1,
	})
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := store.items["trace-1"].state; got != "pending_arbiter" {
		t.Fatalf("item state = %q, want pending_arbiter", got)
	}
}

// TestSafetyReviewRunnerRouterCandidateDoesNotShortcut 验证候选存在时进入 Expert 骨架状态。
func TestSafetyReviewRunnerRouterCandidateDoesNotShortcut(t *testing.T) {
	store := newSafetyReviewFakeStore("trace-1")
	caller := newSafetyReviewFakeCaller(
		func(_ context.Context, req service.SafetyReviewCallRequest) (service.SafetyReviewCallResult, error) {
			content := safetyReviewRoleContent(req.Role)
			if req.Role == dto.SafetyReviewRouter {
				content = []byte(
					`{"features":[],"attack_method_candidates":[],` +
						`"attack_domain_candidates":[{"category":"ethnic_discrimination",` +
						`"evidence_ids":[],"reason":"candidate"}],"coverage_complete":true}`,
				)
			}
			return service.SafetyReviewCallResult{Content: content}, nil
		},
	)
	runner := newSafetyReviewTestRunner(t, store, caller, map[dto.SafetyReviewRole]int{
		dto.SafetyReviewJudgeA: 1,
		dto.SafetyReviewJudgeB: 1,
		dto.SafetyReviewRouter: 1,
	})
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := store.items["trace-1"].state; got != "awaiting_experts" {
		t.Fatalf("item state = %q, want awaiting_experts", got)
	}
}

// TestSafetyReviewRunnerRouterIncompleteDoesNotShortcut 验证 Router coverage incomplete 禁止 shortcut。
func TestSafetyReviewRunnerRouterIncompleteDoesNotShortcut(t *testing.T) {
	store := newSafetyReviewFakeStore("trace-1")
	caller := newSafetyReviewFakeCaller(
		func(_ context.Context, req service.SafetyReviewCallRequest) (service.SafetyReviewCallResult, error) {
			content := safetyReviewRoleContent(req.Role)
			if req.Role == dto.SafetyReviewRouter {
				content = []byte(
					`{"features":[],"attack_method_candidates":[],` +
						`"attack_domain_candidates":[],"coverage_complete":false}`,
				)
			}
			return service.SafetyReviewCallResult{Content: content}, nil
		},
	)
	runner := newSafetyReviewTestRunner(t, store, caller, map[dto.SafetyReviewRole]int{
		dto.SafetyReviewJudgeA: 1,
		dto.SafetyReviewJudgeB: 1,
		dto.SafetyReviewRouter: 1,
	})
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := store.items["trace-1"].state; got != "pending_arbiter" {
		t.Fatalf("item state = %q, want pending_arbiter", got)
	}
}

// TestSafetyReviewRunnerRecoversRunningStage 验证 running 阶段恢复后可重新领取。
func TestSafetyReviewRunnerRecoversRunningStage(t *testing.T) {
	store := newSafetyReviewFakeStore("trace-1")
	store.items["trace-1"].stages["judge:a"].state = "running"
	caller := newSafetyReviewFakeCaller(
		func(_ context.Context, req service.SafetyReviewCallRequest) (service.SafetyReviewCallResult, error) {
			return service.SafetyReviewCallResult{Content: safetyReviewRoleContent(req.Role)}, nil
		},
	)
	runner := newSafetyReviewTestRunner(t, store, caller, map[dto.SafetyReviewRole]int{
		dto.SafetyReviewJudgeA: 1,
		dto.SafetyReviewJudgeB: 1,
		dto.SafetyReviewRouter: 1,
	})
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := store.items["trace-1"].stages["judge:a"].state; got != "succeeded" {
		t.Fatalf("judge:a state = %q, want succeeded", got)
	}
	if len(caller.calls) != 3 {
		t.Fatalf("call count = %d, want 3", len(caller.calls))
	}
}

// TestSafetyReviewRunnerFatalCancelsWorkers 验证 run-level fatal 会取消全部 worker。
func TestSafetyReviewRunnerFatalCancelsWorkers(t *testing.T) {
	store := newSafetyReviewFakeStore("trace-1")
	store.claimErr = errors.New("fatal store failure")
	caller := newSafetyReviewFakeCaller(
		func(_ context.Context, req service.SafetyReviewCallRequest) (service.SafetyReviewCallResult, error) {
			return service.SafetyReviewCallResult{Content: safetyReviewRoleContent(req.Role)}, nil
		},
	)
	runner := newSafetyReviewTestRunner(t, store, caller, map[dto.SafetyReviewRole]int{
		dto.SafetyReviewJudgeA: 1,
		dto.SafetyReviewJudgeB: 1,
		dto.SafetyReviewRouter: 1,
	})
	if _, err := runner.Run(context.Background()); err == nil {
		t.Fatal("Run() returned nil error for fatal store failure")
	}
	if len(caller.calls) != 0 {
		t.Fatalf("calls after fatal = %d, want 0", len(caller.calls))
	}
}

// TestSafetyReviewRunnerContextCancelDrainsWorkers 验证取消后 worker 全部退出。
func TestSafetyReviewRunnerContextCancelDrainsWorkers(t *testing.T) {
	store := newSafetyReviewFakeStore("trace-1")
	entered := make(chan struct{}, 3)
	caller := newSafetyReviewFakeCaller(
		func(ctx context.Context, req service.SafetyReviewCallRequest) (service.SafetyReviewCallResult, error) {
			<-ctx.Done()
			return service.SafetyReviewCallResult{Content: safetyReviewRoleContent(req.Role)}, nil
		},
	)
	caller.before = func(_ context.Context, req service.SafetyReviewCallRequest) { entered <- struct{}{} }
	runner := newSafetyReviewTestRunner(t, store, caller, map[dto.SafetyReviewRole]int{
		dto.SafetyReviewJudgeA: 1,
		dto.SafetyReviewJudgeB: 1,
		dto.SafetyReviewRouter: 1,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := runner.Run(ctx)
		done <- err
	}()
	<-entered
	cancel()
	select {
	case <-time.After(200 * time.Millisecond):
		t.Fatal("workers did not exit after context cancellation")
	case <-done:
	}
}

// TestSafetyReviewRunnerBoundedWorkers 验证并发 worker 数不超过配置上限。
func TestSafetyReviewRunnerBoundedWorkers(t *testing.T) {
	store := newSafetyReviewFakeStore("trace-1", "trace-2", "trace-3")
	active := make(map[string]int)
	maxActive := make(map[string]int)
	var mu sync.Mutex
	caller := newSafetyReviewFakeCaller(
		func(_ context.Context, req service.SafetyReviewCallRequest) (service.SafetyReviewCallResult, error) {
			return service.SafetyReviewCallResult{Content: safetyReviewRoleContent(req.Role)}, nil
		},
	)
	caller.before = func(_ context.Context, req service.SafetyReviewCallRequest) {
		mu.Lock()
		active[string(req.Role)]++
		if active[string(req.Role)] > maxActive[string(req.Role)] {
			maxActive[string(req.Role)] = active[string(req.Role)]
		}
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		mu.Lock()
		active[string(req.Role)]--
		mu.Unlock()
	}
	runner := newSafetyReviewTestRunner(t, store, caller, map[dto.SafetyReviewRole]int{
		dto.SafetyReviewJudgeA: 1,
		dto.SafetyReviewJudgeB: 1,
		dto.SafetyReviewRouter: 1,
	})
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	for role, count := range active {
		if count != 0 {
			t.Fatalf("role %s active workers = %d, want 0", role, count)
		}
	}
	for role, count := range maxActive {
		if count > 1 {
			t.Fatalf("role %s max active workers = %d, want <= 1", role, count)
		}
	}
}

// TestSafetyReviewRunnerClaimsAndCompletionsStayRoleScoped 验证领取与完成不会跨角色串池。
func TestSafetyReviewRunnerClaimsAndCompletionsStayRoleScoped(t *testing.T) {
	store := newSafetyReviewFakeStore("trace-1", "trace-2", "trace-3")
	caller := newSafetyReviewFakeCaller(
		func(_ context.Context, req service.SafetyReviewCallRequest) (service.SafetyReviewCallResult, error) {
			return service.SafetyReviewCallResult{Content: safetyReviewRoleContent(req.Role)}, nil
		},
	)
	runner := newSafetyReviewTestRunner(t, store, caller, map[dto.SafetyReviewRole]int{
		dto.SafetyReviewJudgeA: 2,
		dto.SafetyReviewJudgeB: 2,
		dto.SafetyReviewRouter: 2,
	})
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	claimCounts := make(map[dto.SafetyReviewRole]int)
	for _, claim := range store.claims {
		claimCounts[dto.SafetyReviewRole(claim.Role)]++
	}
	for _, role := range []dto.SafetyReviewRole{dto.SafetyReviewJudgeA, dto.SafetyReviewJudgeB, dto.SafetyReviewRouter} {
		if claimCounts[role] != 3 {
			t.Fatalf("role %s claim count = %d, want 3", role, claimCounts[role])
		}
	}
	if len(store.completed) != 9 {
		t.Fatalf("completion count = %d, want 9", len(store.completed))
	}
	for _, completion := range store.completed {
		if _, ok := map[string]bool{"judge:a": true, "judge:b": true, "router": true}[completion.StageKey]; !ok {
			t.Fatalf("completion has unexpected stage key %q", completion.StageKey)
		}
	}
}

// TestSafetyReviewRunnerFatalWithMultipleWorkersReturnsSingleError 验证多 worker fatal 只返回一个错误。
func TestSafetyReviewRunnerFatalWithMultipleWorkersReturnsSingleError(t *testing.T) {
	store := newSafetyReviewFakeStore("trace-1")
	store.claimErr = errors.New("fatal store failure")
	caller := newSafetyReviewFakeCaller(
		func(_ context.Context, req service.SafetyReviewCallRequest) (service.SafetyReviewCallResult, error) {
			return service.SafetyReviewCallResult{Content: safetyReviewRoleContent(req.Role)}, nil
		},
	)
	runner := newSafetyReviewTestRunner(t, store, caller, map[dto.SafetyReviewRole]int{
		dto.SafetyReviewJudgeA: 2,
		dto.SafetyReviewJudgeB: 2,
		dto.SafetyReviewRouter: 2,
	})
	if _, err := runner.Run(context.Background()); err == nil {
		t.Fatal("Run() returned nil error for fatal store failure")
	}
	if len(caller.calls) != 0 {
		t.Fatalf("calls after fatal = %d, want 0", len(caller.calls))
	}
}

// fakeSafetyReviewStore 提供测试用的初始阶段状态存储。
type fakeSafetyReviewStore struct {
	mu        sync.Mutex
	items     map[string]*fakeSafetyReviewItem
	claimErr  error
	completed []dao.SafetyReviewStageCompletion
	claims    []dao.SafetyReviewClaim
}

// fakeSafetyReviewItem 表示测试条目的初始阶段状态。
type fakeSafetyReviewItem struct {
	state                string
	scene                string
	independenceDegraded bool
	stages               map[string]*fakeSafetyReviewStage
}

// fakeSafetyReviewStage 表示测试阶段状态。
type fakeSafetyReviewStage struct {
	role     dto.SafetyReviewRole
	axis     string
	category string
	state    string
	result   []byte
}

// RecoverRunning 将 running 阶段恢复为 pending。
func (s *fakeSafetyReviewStore) RecoverRunning(ctx context.Context, taskID string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := int64(0)
	for _, item := range s.items {
		for _, stage := range item.stages {
			if stage.state == "running" {
				stage.state = "pending"
				count++
			}
		}
	}
	return count, nil
}

// ClaimStage 领取指定角色的下一个 pending 阶段。
func (s *fakeSafetyReviewStore) ClaimStage(
	ctx context.Context,
	claim dao.SafetyReviewClaim,
) (dao.SafetyReviewStageWork, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.claimErr != nil {
		return dao.SafetyReviewStageWork{}, false, s.claimErr
	}
	for traceID, item := range s.items {
		for stageKey, stage := range item.stages {
			if string(stage.role) != claim.Role || stage.state != "pending" {
				continue
			}
			stage.state = "running"
			s.claims = append(s.claims, claim)
			return dao.SafetyReviewStageWork{
				TaskID:       claim.TaskID,
				TraceID:      traceID,
				StageKey:     stageKey,
				Role:         string(stage.role),
				Axis:         stage.axis,
				Category:     stage.category,
				Scene:        item.scene,
				Prompt:       "prompt-" + traceID + "-" + string(stage.role),
				Response:     "response-" + traceID + "-" + string(stage.role),
				ModelProfile: claim.ModelProfile,
				ModelFamily:  claim.ModelFamily,
			}, true, nil
		}
	}
	return dao.SafetyReviewStageWork{}, false, nil
}

// CompleteStage 记录阶段完成状态。
func (s *fakeSafetyReviewStore) CompleteStage(
	ctx context.Context,
	completion dao.SafetyReviewStageCompletion,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.completed = append(s.completed, completion)
	if item, ok := s.items[completion.TraceID]; ok {
		if stage, ok := item.stages[completion.StageKey]; ok {
			stage.state = string(completion.Outcome)
			stage.result = append([]byte(nil), completion.ResultJSON...)
		}
		for _, stage := range completion.DownstreamStages {
			if _, exists := item.stages[stage.StageKey]; exists {
				continue
			}
			item.stages[stage.StageKey] = &fakeSafetyReviewStage{
				role: dto.SafetyReviewRole(stage.Role), axis: stage.Axis,
				category: stage.Category, state: "pending",
			}
		}
		if completion.IndependenceDegraded {
			item.independenceDegraded = true
		}
		if completion.ItemState != "" {
			item.state = completion.ItemState
		}
	}
	return nil
}

// ReadRoleStageResults 返回指定角色的测试阶段快照。
func (s *fakeSafetyReviewStore) ReadRoleStageResults(
	ctx context.Context,
	taskID string,
	traceID string,
	role string,
) ([]dao.SafetyReviewInitialStageResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[traceID]
	if !ok {
		return nil, nil
	}
	results := make([]dao.SafetyReviewInitialStageResult, 0, len(item.stages))
	for stageKey, stage := range item.stages {
		if string(stage.role) != role {
			continue
		}
		results = append(results, dao.SafetyReviewInitialStageResult{
			StageKey: stageKey, Role: string(stage.role), State: stage.state,
			Axis: stage.axis, Category: stage.category,
			ResultJSON: append([]byte(nil), stage.result...),
		})
	}
	return results, nil
}

// ReadInitialStageResults 返回测试条目的初始阶段快照。
func (s *fakeSafetyReviewStore) ReadInitialStageResults(
	ctx context.Context,
	taskID string,
	traceID string,
) ([]dao.SafetyReviewInitialStageResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[traceID]
	if !ok {
		return nil, nil
	}
	results := make([]dao.SafetyReviewInitialStageResult, 0, len(item.stages))
	for stageKey, stage := range item.stages {
		if stageKey != "judge:a" && stageKey != "judge:b" && stageKey != "router" {
			continue
		}
		results = append(results, dao.SafetyReviewInitialStageResult{
			StageKey:             stageKey,
			Role:                 string(stage.role),
			State:                stage.state,
			ResultJSON:           append([]byte(nil), stage.result...),
			IndependenceDegraded: item.independenceDegraded,
		})
	}
	return results, nil
}

// ReadSummary 返回测试 store 的聚合计数。
func (s *fakeSafetyReviewStore) ReadSummary(ctx context.Context, taskID string) (dao.SafetyReviewSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	summary := dao.SafetyReviewSummary{
		TaskID:     taskID,
		TaskStatus: "running",
		Items:      make(map[string]int64),
		Stages:     make(map[string]int64),
	}
	for _, item := range s.items {
		summary.Items[item.state]++
		for _, stage := range item.stages {
			summary.Stages[stage.state]++
		}
	}
	return summary, nil
}

// fakeSafetyReviewCaller 记录请求并返回脚本结果。
type fakeSafetyReviewCaller struct {
	mu      sync.Mutex
	calls   []service.SafetyReviewCallRequest
	results []service.SafetyReviewCallResult
	script  func(context.Context, service.SafetyReviewCallRequest) (service.SafetyReviewCallResult, error)
	before  func(context.Context, service.SafetyReviewCallRequest)
}

// Call 记录请求并执行脚本结果。
func (c *fakeSafetyReviewCaller) Call(
	ctx context.Context,
	req service.SafetyReviewCallRequest,
) (service.SafetyReviewCallResult, error) {
	c.mu.Lock()
	c.calls = append(c.calls, req)
	c.mu.Unlock()
	if c.before != nil {
		c.before(ctx, req)
	}
	result, err := c.script(ctx, req)
	c.mu.Lock()
	c.results = append(c.results, result)
	c.mu.Unlock()
	return result, err
}

// newSafetyReviewFakeStore 构造带初始三阶段的测试存储。
func newSafetyReviewFakeStore(traceIDs ...string) *fakeSafetyReviewStore {
	store := &fakeSafetyReviewStore{items: make(map[string]*fakeSafetyReviewItem, len(traceIDs))}
	for _, traceID := range traceIDs {
		store.items[traceID] = &fakeSafetyReviewItem{
			state: "pending_initial",
			scene: "prompt",
			stages: map[string]*fakeSafetyReviewStage{
				"judge:a": {role: dto.SafetyReviewJudgeA, state: "pending"},
				"judge:b": {role: dto.SafetyReviewJudgeB, state: "pending"},
				"router":  {role: dto.SafetyReviewRouter, state: "pending"},
			},
		}
	}
	return store
}

// newSafetyReviewFakeCaller 构造脚本化调用器。
func newSafetyReviewFakeCaller(
	script func(context.Context, service.SafetyReviewCallRequest) (service.SafetyReviewCallResult, error),
) *fakeSafetyReviewCaller {
	return &fakeSafetyReviewCaller{script: script}
}

// newSafetyReviewTestRunner 构造测试用 Runner。
func newSafetyReviewTestRunner(
	t *testing.T,
	store *fakeSafetyReviewStore,
	caller *fakeSafetyReviewCaller,
	workers map[dto.SafetyReviewRole]int,
) *service.SafetyReviewRunner {
	t.Helper()
	runner, err := service.NewSafetyReviewRunner(service.SafetyReviewRunnerConfig{
		TaskID:          "task-1",
		Store:           store,
		Caller:          caller,
		Quota:           fakeSafetyReviewQuota{},
		Workers:         workers,
		Policy:          loadSafetyReviewPolicyForRouting(t),
		ModelProfile:    "test-profile",
		ModelFamily:     "test-family",
		APIKeyEnv:       "TEST_KEY",
		ShutdownTimeout: time.Second,
		Now:             time.Now,
		BuildRequest: func(work dao.SafetyReviewStageWork) (service.SafetyReviewCallRequest, error) {
			if work.Role == "expert" {
				return service.BuildSafetyReviewExpertRequest(work, loadSafetyReviewPolicyForRouting(t))
			}
			return service.SafetyReviewCallRequest{
				TaskID:   work.TaskID,
				TraceID:  work.TraceID,
				StageKey: work.StageKey,
				Role:     dto.SafetyReviewRole(work.Role),
				Messages: []dto.Message{{Role: "user", Content: work.Prompt + " " + work.Response}},
				Schema:   []byte(`{"type":"object"}`),
				Mode:     "json_object",
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewSafetyReviewRunner() error = %v", err)
	}
	return runner
}

// newSafetyReviewRoutingCaller 构造初始三阶段与 Expert 均可成功的调用器。
func newSafetyReviewRoutingCaller(t *testing.T, routerJSON []byte, _ string) *fakeSafetyReviewCaller {
	t.Helper()
	policy := loadSafetyReviewPolicyForRouting(t)
	return newSafetyReviewFakeCaller(
		func(_ context.Context, req service.SafetyReviewCallRequest) (service.SafetyReviewCallResult, error) {
			if req.Role == dto.SafetyReviewExpert {
				return service.SafetyReviewCallResult{
					Content: safetyReviewExpertContent(t, policy, req.StageKey),
				}, nil
			}
			if req.Role == dto.SafetyReviewRouter {
				return service.SafetyReviewCallResult{Content: append([]byte(nil), routerJSON...)}, nil
			}
			return service.SafetyReviewCallResult{Content: safetyReviewRoleContent(req.Role)}, nil
		},
	)
}

// oneCandidateRouterJSON 返回一个域候选 Router 结果。
func oneCandidateRouterJSON() []byte {
	return []byte(`{"features":[],"attack_method_candidates":[],` +
		`"attack_domain_candidates":[{"category":"ethnic_discrimination",` +
		`"evidence_ids":[],"reason":"召回"}],"coverage_complete":true}`)
}

// safetyReviewExpertContent 生成指定规则卡的合法 uncertain Expert 结果。
func safetyReviewExpertContent(
	t *testing.T,
	policy *service.SafetyReviewPolicy,
	stageKey string,
) []byte {
	t.Helper()
	parts := strings.Split(stageKey, ":")
	if len(parts) != 3 {
		t.Fatalf("invalid expert stage key %q", stageKey)
	}
	card, ok := policy.Cards[parts[2]]
	if !ok || card.Axis != parts[1] {
		t.Fatalf("expert stage key %q does not match policy", stageKey)
	}
	conditions := make([]dto.SafetyReviewCondition, 0, len(card.RequiredConditions))
	for _, condition := range card.RequiredConditions {
		conditions = append(conditions, dto.SafetyReviewCondition{
			ID: condition.ID, State: "unknown",
		})
	}
	exclusions := make([]dto.SafetyReviewExclusion, 0, len(card.DecisiveExclusions))
	for _, exclusion := range card.DecisiveExclusions {
		exclusions = append(exclusions, dto.SafetyReviewExclusion{
			ID: exclusion.ID, State: "unknown",
		})
	}
	content, err := json.Marshal(dto.SafetyReviewExpertResult{
		Axis: card.Axis, Category: card.ID, Verdict: "uncertain",
		Conditions: conditions, DecisiveExclusions: exclusions,
		Evidence: []dto.SafetyReviewEvidence{}, EvidenceSource: []string{},
		Rationale: "关键条件未知。",
	})
	if err != nil {
		t.Fatalf("marshal expert result: %v", err)
	}
	return content
}

// safetyReviewEstablishedExpertContent 生成指定规则卡的合法 established Expert 结果。
func safetyReviewEstablishedExpertContent(
	t *testing.T,
	policy *service.SafetyReviewPolicy,
	stageKey string,
) []byte {
	t.Helper()
	parts := strings.Split(stageKey, ":")
	if len(parts) != 3 {
		t.Fatalf("invalid expert stage key %q", stageKey)
	}
	card, ok := policy.Cards[parts[2]]
	if !ok || card.Axis != parts[1] {
		t.Fatalf("expert stage key %q does not match policy", stageKey)
	}
	conditions := make([]dto.SafetyReviewCondition, 0, len(card.RequiredConditions))
	for _, condition := range card.RequiredConditions {
		conditions = append(conditions, dto.SafetyReviewCondition{
			ID: condition.ID, State: "satisfied", EvidenceRefs: []string{"E1"},
		})
	}
	exclusions := make([]dto.SafetyReviewExclusion, 0, len(card.DecisiveExclusions))
	for _, exclusion := range card.DecisiveExclusions {
		exclusions = append(exclusions, dto.SafetyReviewExclusion{
			ID: exclusion.ID, State: "absent",
		})
	}
	content, err := json.Marshal(dto.SafetyReviewExpertResult{
		Axis: card.Axis, Category: card.ID, Verdict: "established",
		Conditions: conditions, DecisiveExclusions: exclusions,
		Evidence: []dto.SafetyReviewEvidence{{
			Source: "prompt", Kind: "operational_attack_wording", Span: "攻击证据",
		}},
		EvidenceSource: []string{"prompt"}, Rationale: "全部必要条件已满足。",
	})
	if err != nil {
		t.Fatalf("marshal established expert result: %v", err)
	}
	return content
}

// fakeSafetyReviewQuota 是无限制的测试配额。
type fakeSafetyReviewQuota struct{}

// Acquire 立即返回一个空释放函数。
func (fakeSafetyReviewQuota) Acquire(
	ctx context.Context,
	role string,
	group string,
	estimatedTokens int,
) (func(), error) {
	return func() {}, nil
}

// safetyReviewRoleContent 返回指定角色的成功 JSON。
func safetyReviewRoleContent(role dto.SafetyReviewRole) []byte {
	switch role {
	case dto.SafetyReviewJudgeA, dto.SafetyReviewJudgeB:
		return []byte(
			`{"verdict":"safe","evidence_complete":true,` +
				`"evidence":[],"attack_methods":[],"attack_domains":[],` +
				`"exclusions":[],"rationale":"safe"}`,
		)
	case dto.SafetyReviewRouter:
		return []byte(`{"features":[],"attack_method_candidates":[],"attack_domain_candidates":[],"coverage_complete":true}`)
	default:
		return []byte(`{}`)
	}
}

// schedulerContainsAny 判断文本是否包含任一关键词。
func schedulerContainsAny(text string, words ...string) bool {
	for _, word := range words {
		if strings.Contains(text, word) {
			return true
		}
	}
	return false
}
