// Package service 提供 Safety Review 初始阶段与 Expert 的持久化调度。
package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"sendllm/internal/dao"
	"sendllm/internal/dto"
)

// SafetyReviewRunnerStore 是调度器需要的持久化边界。
type SafetyReviewRunnerStore interface {
	RecoverRunning(ctx context.Context, taskID string) (int64, error)
	ClaimStage(ctx context.Context, claim dao.SafetyReviewClaim) (dao.SafetyReviewStageWork, bool, error)
	CompleteStage(ctx context.Context, completion dao.SafetyReviewStageCompletion) error
	ReadInitialStageResults(
		ctx context.Context,
		taskID string,
		traceID string,
	) ([]dao.SafetyReviewInitialStageResult, error)
	ReadRoleStageResults(
		ctx context.Context,
		taskID string,
		traceID string,
		role string,
	) ([]dao.SafetyReviewInitialStageResult, error)
	ReadSummary(ctx context.Context, taskID string) (dao.SafetyReviewSummary, error)
}

// SafetyReviewRunnerQuota 是调度器需要的配额边界。
type SafetyReviewRunnerQuota interface {
	Acquire(ctx context.Context, role string, group string, estimatedTokens int) (func(), error)
}

// SafetyReviewStageCaller 是调度器使用的模型调用边界。
type SafetyReviewStageCaller interface {
	Call(ctx context.Context, req SafetyReviewCallRequest) (SafetyReviewCallResult, error)
}

// SafetyReviewRunnerConfig 指定并行调度的依赖与生命周期。
type SafetyReviewRunnerConfig struct {
	TaskID          string
	Store           SafetyReviewRunnerStore
	Caller          SafetyReviewStageCaller
	Quota           SafetyReviewRunnerQuota
	QuotaGroup      string
	QuotaGroups     map[dto.SafetyReviewRole]string
	Workers         map[dto.SafetyReviewRole]int
	Policy          *SafetyReviewPolicy
	ModelProfile    string
	ModelFamily     string
	APIKeyEnv       string
	ShutdownTimeout time.Duration
	Now             func() time.Time
	BuildRequest    func(dao.SafetyReviewStageWork) (SafetyReviewCallRequest, error)
}

// SafetyReviewRunStats 汇总一次调度的处理结果。
type SafetyReviewRunStats struct {
	Processed       int64
	ResolvedSafe    int64
	ResolvedUnsafe  int64
	AwaitingExperts int64
	PendingArbiter  int64
	Quarantined     int64
	TerminalFailed  int64
}

// SafetyReviewRunner 并行执行初始三阶段和按候选创建的 Expert 阶段。
type SafetyReviewRunner struct {
	cfg       SafetyReviewRunnerConfig
	validator *SafetyReviewValidator
}

// safetyReviewSchedulerState 串行化同条目的终态计算并汇总统计。
type safetyReviewSchedulerState struct {
	finalMu           sync.Mutex
	mu                sync.Mutex
	stats             SafetyReviewRunStats
	workAvailable     chan struct{}
	startExpertsOnce  sync.Once
	startExperts      func()
	startArbitersOnce sync.Once
	startArbiters     func()
}

// safetyReviewStageTransition 表示一次阶段完成后的持久化下游状态。
type safetyReviewStageTransition struct {
	itemState  string
	downstream []dao.SafetyReviewStageSpec
	decision   *dao.SafetyReviewDecisionRecord
}

// NewSafetyReviewRunner 校验依赖并构造并行调度器。
func NewSafetyReviewRunner(cfg SafetyReviewRunnerConfig) (*SafetyReviewRunner, error) {
	if cfg.TaskID == "" || cfg.Store == nil || cfg.Caller == nil || cfg.Quota == nil {
		return nil, fmt.Errorf("safety review runner dependencies are incomplete")
	}
	for role, count := range cfg.Workers {
		if count < 0 {
			return nil, fmt.Errorf("safety review runner role %s has negative workers", role)
		}
		switch role {
		case dto.SafetyReviewJudgeA, dto.SafetyReviewJudgeB, dto.SafetyReviewRouter,
			dto.SafetyReviewExpert, dto.SafetyReviewArbiter:
		default:
			return nil, fmt.Errorf("safety review runner role %q is not supported", role)
		}
	}
	for _, role := range []dto.SafetyReviewRole{
		dto.SafetyReviewJudgeA, dto.SafetyReviewJudgeB, dto.SafetyReviewRouter,
	} {
		if cfg.Workers[role] < 1 {
			return nil, fmt.Errorf("safety review runner role %s requires at least one worker", role)
		}
	}
	if cfg.Policy == nil || cfg.ShutdownTimeout <= 0 || cfg.Now == nil || cfg.BuildRequest == nil {
		return nil, fmt.Errorf("safety review runner lifecycle configuration is invalid")
	}
	if cfg.ModelProfile == "" || cfg.ModelFamily == "" || cfg.APIKeyEnv == "" {
		return nil, fmt.Errorf("safety review runner model identity is incomplete")
	}
	validator, err := NewSafetyReviewValidator(cfg.Policy)
	if err != nil {
		return nil, fmt.Errorf("build safety review validator: %w", err)
	}
	return &SafetyReviewRunner{cfg: cfg, validator: validator}, nil
}

// Run 恢复 running 阶段并处理初始阶段与已创建的 Expert 阶段。
func (r *SafetyReviewRunner) Run(ctx context.Context) (SafetyReviewRunStats, error) {
	if _, err := r.cfg.Store.RecoverRunning(ctx, r.cfg.TaskID); err != nil {
		return SafetyReviewRunStats{}, fmt.Errorf("recover safety review stages: %w", err)
	}
	summary, err := r.cfg.Store.ReadSummary(ctx, r.cfg.TaskID)
	if err != nil {
		return SafetyReviewRunStats{}, fmt.Errorf("read safety review summary: %w", err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	fatal := make(chan error, 1)
	state := newSafetyReviewSchedulerState()
	initialDone := make(chan struct{})
	expertDone := make(chan struct{})
	var initialWorkers sync.WaitGroup
	var expertWorkers sync.WaitGroup
	var arbiterWorkers sync.WaitGroup
	state.startExperts = func() {
		if r.cfg.Workers[dto.SafetyReviewExpert] < 1 {
			return
		}
		r.startRoleWorkers(
			runCtx, dto.SafetyReviewExpert, r.cfg.Workers[dto.SafetyReviewExpert],
			&expertWorkers, fatal, state, cancel, initialDone,
		)
	}
	state.startArbiters = func() {
		if r.cfg.Workers[dto.SafetyReviewArbiter] < 1 {
			return
		}
		r.startRoleWorkers(
			runCtx, dto.SafetyReviewArbiter, r.cfg.Workers[dto.SafetyReviewArbiter],
			&arbiterWorkers, fatal, state, cancel, expertDone,
		)
	}
	r.startInitialWorkers(runCtx, &initialWorkers, fatal, state, cancel)
	if summary.Items["awaiting_experts"] > 0 {
		state.requestExperts()
	}
	if summary.Items["pending_arbiter"] > 0 {
		state.requestArbiters()
	}

	initialWorkers.Wait()
	close(initialDone)
	expertWorkers.Wait()
	close(expertDone)
	arbiterWorkers.Wait()
	select {
	case err := <-fatal:
		return state.snapshotStats(), err
	default:
	}
	if err := ctx.Err(); err != nil {
		return state.snapshotStats(), err
	}
	return state.snapshotStats(), nil
}

// startInitialWorkers 启动 A、B 与 Router 的独立 worker 池。
func (r *SafetyReviewRunner) startInitialWorkers(
	ctx context.Context,
	workers *sync.WaitGroup,
	fatal chan error,
	state *safetyReviewSchedulerState,
	cancel context.CancelFunc,
) {
	for _, role := range []dto.SafetyReviewRole{
		dto.SafetyReviewJudgeA, dto.SafetyReviewJudgeB, dto.SafetyReviewRouter,
	} {
		r.startRoleWorkers(ctx, role, r.cfg.Workers[role], workers, fatal, state, cancel, nil)
	}
}

// startRoleWorkers 启动指定数量的角色 worker。
func (r *SafetyReviewRunner) startRoleWorkers(
	ctx context.Context,
	role dto.SafetyReviewRole,
	count int,
	workers *sync.WaitGroup,
	fatal chan error,
	state *safetyReviewSchedulerState,
	cancel context.CancelFunc,
	stop <-chan struct{},
) {
	for index := 0; index < count; index++ {
		workers.Add(1)
		go r.worker(ctx, role, workers, fatal, state, cancel, stop)
	}
}

// worker 持续领取并执行指定角色的阶段。
func (r *SafetyReviewRunner) worker(
	ctx context.Context,
	role dto.SafetyReviewRole,
	workers *sync.WaitGroup,
	fatal chan error,
	state *safetyReviewSchedulerState,
	cancel context.CancelFunc,
	stop <-chan struct{},
) {
	defer workers.Done()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		claim := dao.SafetyReviewClaim{
			TaskID:        r.cfg.TaskID,
			Role:          string(role),
			Now:           r.cfg.Now(),
			ModelProfile:  r.cfg.ModelProfile,
			ModelFamily:   r.cfg.ModelFamily,
			FallbackIndex: 0,
		}
		work, ok, err := r.cfg.Store.ClaimStage(ctx, claim)
		if err != nil {
			r.reportFatal(fatal, cancel, err)
			return
		}
		if !ok {
			if stop != nil {
				select {
				case <-ctx.Done():
					return
				case <-stop:
					// Expert 已全部结束，退出前将已创建的 Arbiter 阶段排空。
					for {
						work, ok, err = r.cfg.Store.ClaimStage(ctx, claim)
						if err != nil {
							r.reportFatal(fatal, cancel, err)
							return
						}
						if !ok {
							return
						}
						if err := r.processStage(ctx, work, state); err != nil {
							r.reportFatal(fatal, cancel, err)
							return
						}
					}
				case <-state.workAvailable:
					continue
				}
			}
			return
		}
		if err := r.processStage(ctx, work, state); err != nil {
			r.reportFatal(fatal, cancel, err)
			return
		}
	}
}

// processStage 构建请求、调用模型并持久化阶段结果。
func (r *SafetyReviewRunner) processStage(
	ctx context.Context,
	work dao.SafetyReviewStageWork,
	state *safetyReviewSchedulerState,
) error {
	request, requestErr := r.buildStageRequest(ctx, work)
	if requestErr != nil {
		return fmt.Errorf("build safety review request: %w", requestErr)
	}
	role := dto.SafetyReviewRole(work.Role)
	quotaGroup := r.cfg.QuotaGroup
	if group, ok := r.cfg.QuotaGroups[role]; ok {
		quotaGroup = group
	}
	release, err := r.cfg.Quota.Acquire(ctx, work.Role, quotaGroup, request.EstimatedTokens)
	if err != nil {
		return fmt.Errorf("acquire safety review quota: %w", err)
	}
	defer release()

	startedAt := r.cfg.Now()
	result, callErr := r.cfg.Caller.Call(ctx, request)
	finishedAt := r.cfg.Now()
	parsed, parseErr := r.parseStageResult(ctx, work, role, result.Content)
	outcome := dao.SafetyReviewStageSucceeded
	if callErr != nil || parseErr != nil {
		outcome = dao.SafetyReviewStageTerminalFailed
		parsed = nil
	}

	state.finalMu.Lock()
	transition, transitionErr := r.stageTransition(
		ctx, work, role, parsed, outcome, result.IndependenceDegraded, result.Content,
	)
	if transitionErr != nil {
		state.finalMu.Unlock()
		return transitionErr
	}

	profile := result.Profile
	if profile == "" {
		profile = work.ModelProfile
	}
	family := result.Family
	if family == "" {
		family = work.ModelFamily
	}
	apiKeyEnv := result.APIKeyEnv
	if apiKeyEnv == "" {
		apiKeyEnv = r.cfg.APIKeyEnv
	}
	completion := dao.SafetyReviewStageCompletion{
		TaskID:           r.cfg.TaskID,
		TraceID:          work.TraceID,
		StageKey:         work.StageKey,
		Outcome:          outcome,
		ResultJSON:       result.Content,
		ItemState:        transition.itemState,
		Decision:         transition.decision,
		DownstreamStages: transition.downstream,
		Attempt: dao.SafetyReviewAttempt{
			AttemptKind:      "classification",
			ModelProfile:     profile,
			ModelFamily:      family,
			APIKeyEnv:        apiKeyEnv,
			StartedAt:        startedAt,
			FinishedAt:       finishedAt,
			FinishReason:     family,
			CompletionTokens: result.Attempts,
		},
		IndependenceDegraded: result.IndependenceDegraded,
	}
	if callErr != nil {
		completion.ErrorCategory = "model_failure"
		completion.ErrorSummary = "model call failed"
		completion.Attempt.ErrorCategory = completion.ErrorCategory
		completion.Attempt.ErrorSummary = completion.ErrorSummary
	}
	if parseErr != nil {
		completion.ErrorCategory = "invalid_result"
		completion.ErrorSummary = "model result could not be parsed or validated"
		completion.Attempt.ErrorCategory = completion.ErrorCategory
		completion.Attempt.ErrorSummary = completion.ErrorSummary
		completion.Attempt.ValidationError = []byte(parseErr.Error())
	}
	completeErr := r.cfg.Store.CompleteStage(ctx, completion)
	startExperts := hasExpertSafetyReviewStage(transition.downstream)
	startArbiters := hasArbiterSafetyReviewStage(transition.downstream)
	state.finalMu.Unlock()
	if completeErr != nil {
		return fmt.Errorf("complete safety review stage %s: %w", work.StageKey, completeErr)
	}
	state.recordItemState(transition.itemState)
	state.recordStageOutcome(outcome)
	if startExperts {
		state.requestExperts()
		state.signalWork()
	}
	if startArbiters {
		state.requestArbiters()
		state.signalWork()
	}
	return nil
}

// buildStageRequest 按角色构建阶段模型请求。
func (r *SafetyReviewRunner) buildStageRequest(
	ctx context.Context,
	work dao.SafetyReviewStageWork,
) (SafetyReviewCallRequest, error) {
	if dto.SafetyReviewRole(work.Role) != dto.SafetyReviewArbiter {
		return r.cfg.BuildRequest(work)
	}
	prior, err := r.arbiterPriorOutputs(ctx, work)
	if err != nil {
		return SafetyReviewCallRequest{}, err
	}
	return BuildSafetyReviewArbiterRequest(work, prior, r.cfg.Policy)
}

// stageTransition 计算指定角色完成后的条目状态和下游阶段。
func (r *SafetyReviewRunner) stageTransition(
	ctx context.Context,
	work dao.SafetyReviewStageWork,
	role dto.SafetyReviewRole,
	current any,
	outcome dao.SafetyReviewStageOutcome,
	currentDegraded bool,
	raw []byte,
) (safetyReviewStageTransition, error) {
	if role == dto.SafetyReviewExpert {
		return r.expertTransition(ctx, work, outcome)
	}
	if role == dto.SafetyReviewArbiter {
		return r.arbiterTransition(ctx, work, outcome, raw)
	}
	return r.initialTransition(ctx, work, role, current, outcome, currentDegraded)
}

// initialTransition 用持久化快照计算初始三阶段的下游状态。
func (r *SafetyReviewRunner) initialTransition(
	ctx context.Context,
	work dao.SafetyReviewStageWork,
	role dto.SafetyReviewRole,
	current any,
	outcome dao.SafetyReviewStageOutcome,
	currentDegraded bool,
) (safetyReviewStageTransition, error) {
	results, err := r.cfg.Store.ReadInitialStageResults(ctx, work.TaskID, work.TraceID)
	if err != nil {
		return safetyReviewStageTransition{}, fmt.Errorf("read safety review initial stage results: %w", err)
	}

	judges := make(map[dto.SafetyReviewRole]*dto.SafetyReviewJudgment, 2)
	completed := make(map[dto.SafetyReviewRole]bool, 3)
	var router *dto.SafetyReviewRoute
	degraded := currentDegraded
	for _, result := range results {
		stageRole := dto.SafetyReviewRole(result.Role)
		if result.IndependenceDegraded {
			degraded = true
		}
		if result.StageKey == work.StageKey {
			continue
		}
		switch result.State {
		case string(dao.SafetyReviewStageSucceeded):
			parsed, err := r.parseStageResult(ctx, work, stageRole, result.ResultJSON)
			if err != nil {
				return safetyReviewStageTransition{}, fmt.Errorf(
					"parse persisted safety review stage %s: %w", result.StageKey, err,
				)
			}
			assignSafetyReviewInitialResult(judges, &router, stageRole, parsed)
			completed[stageRole] = true
		case string(dao.SafetyReviewStageTerminalFailed):
			completed[stageRole] = true
		}
	}
	if outcome == dao.SafetyReviewStageSucceeded {
		assignSafetyReviewInitialResult(judges, &router, role, current)
		completed[role] = true
	}
	if outcome == dao.SafetyReviewStageTerminalFailed {
		completed[role] = true
	}
	if len(completed) != 3 {
		return safetyReviewStageTransition{}, nil
	}

	gate := SafetyReviewTransitionGate(
		judges[dto.SafetyReviewJudgeA], judges[dto.SafetyReviewJudgeB], router, degraded,
	)
	plan, planErr := r.routingPlan(work.Scene, router)
	if planErr != nil {
		return safetyReviewStageTransition{}, planErr
	}
	if plan != nil && plan.PolicyCoverageGap && len(plan.DownstreamStages) == 1 &&
		plan.DownstreamStages[0].StageKey == "arbiter" {
		gate = "pending_arbiter"
	}
	if gate == "resolved_safe" {
		return safetyReviewStageTransition{
			itemState: "resolved_safe",
			decision: &dao.SafetyReviewDecisionRecord{
				FinalState:    "resolved_safe",
				Label:         "safe",
				AttackMethods: []byte("[]"),
				AttackDomains: []byte("[]"),
				CaseType:      "typical",
				Evidence:      []byte("[]"),
				DecisionRules: []byte(`["DISCRIMINATION-R01"]`),
				Rationale:     "complete dual-safe zero-candidate shortcut",
				DecidedAt:     r.cfg.Now(),
			},
		}, nil
	}
	if plan != nil && gate == "awaiting_experts" {
		return safetyReviewStageTransition{
			itemState:  plan.ItemState,
			downstream: plan.DownstreamStages,
		}, nil
	}
	return safetyReviewStageTransition{
		itemState:  "pending_arbiter",
		downstream: []dao.SafetyReviewStageSpec{arbiterSafetyReviewStage()},
	}, nil
}

// expertTransition 等待全部 Expert 终态后只创建 Arbiter 阶段。
func (r *SafetyReviewRunner) expertTransition(
	ctx context.Context,
	work dao.SafetyReviewStageWork,
	outcome dao.SafetyReviewStageOutcome,
) (safetyReviewStageTransition, error) {
	results, err := r.cfg.Store.ReadRoleStageResults(ctx, work.TaskID, work.TraceID, "expert")
	if err != nil {
		return safetyReviewStageTransition{}, fmt.Errorf("read safety review expert stages: %w", err)
	}
	terminal := 0
	for _, result := range results {
		if result.StageKey == work.StageKey {
			continue
		}
		if result.State == string(dao.SafetyReviewStageSucceeded) ||
			result.State == string(dao.SafetyReviewStageTerminalFailed) {
			terminal++
		}
	}
	if outcome == dao.SafetyReviewStageSucceeded || outcome == dao.SafetyReviewStageTerminalFailed {
		terminal++
	}
	if len(results) == 0 || terminal != len(results) {
		return safetyReviewStageTransition{}, nil
	}
	return safetyReviewStageTransition{
		itemState:  "pending_arbiter",
		downstream: []dao.SafetyReviewStageSpec{arbiterSafetyReviewStage()},
	}, nil
}

// arbiterTransition 将 Arbiter 成功结果投影为最终决策，失败时写固定隔离决策。
func (r *SafetyReviewRunner) arbiterTransition(
	ctx context.Context,
	work dao.SafetyReviewStageWork,
	outcome dao.SafetyReviewStageOutcome,
	raw []byte,
) (safetyReviewStageTransition, error) {
	if outcome != dao.SafetyReviewStageSucceeded {
		decision := modelExhaustedSafetyReviewDecision(r.cfg.Now())
		return safetyReviewStageTransition{
			itemState: "quarantined",
			decision:  &decision,
		}, nil
	}
	prior, err := r.arbiterPriorOutputs(ctx, work)
	if err != nil {
		return safetyReviewStageTransition{}, err
	}
	record, err := ProjectSafetyReviewDecision(
		r.cfg.Policy, work.Scene, prior, raw, r.cfg.Now(),
	)
	if err != nil {
		return safetyReviewStageTransition{}, err
	}
	return safetyReviewStageTransition{itemState: record.FinalState, decision: &record}, nil
}

// parseStageResult 解析并校验当前阶段的模型结果。
func (r *SafetyReviewRunner) parseStageResult(
	ctx context.Context,
	work dao.SafetyReviewStageWork,
	role dto.SafetyReviewRole,
	raw []byte,
) (any, error) {
	switch role {
	case dto.SafetyReviewJudgeA, dto.SafetyReviewJudgeB:
		judgment, err := r.validator.ValidateJudgment(work.Scene, raw)
		if err != nil {
			return nil, err
		}
		return &judgment, nil
	case dto.SafetyReviewRouter:
		route, err := parseSafetyReviewRoute(raw)
		if err != nil {
			return nil, err
		}
		if _, err := BuildSafetyReviewRoutingPlan(r.cfg.Policy, work.Scene, *route); err != nil {
			return nil, err
		}
		return route, nil
	case dto.SafetyReviewExpert:
		return r.validator.ValidateExpert(work.Scene, work.Axis, work.Category, raw)
	case dto.SafetyReviewArbiter:
		prior, err := r.arbiterPriorOutputs(ctx, work)
		if err != nil {
			return nil, err
		}
		record, err := ProjectSafetyReviewDecision(
			r.cfg.Policy, work.Scene, prior, raw, r.cfg.Now(),
		)
		if err != nil {
			return nil, err
		}
		return &record, nil
	default:
		return nil, fmt.Errorf("unknown safety review role %q", role)
	}
}

// arbiterPriorOutputs 读取并解析一条记录的全部先验阶段结果。
func (r *SafetyReviewRunner) arbiterPriorOutputs(
	ctx context.Context,
	work dao.SafetyReviewStageWork,
) (SafetyReviewArbiterPriorOutputs, error) {
	prior := SafetyReviewArbiterPriorOutputs{
		Experts: []dto.SafetyReviewExpertResult{}, TerminalFailures: []string{},
	}
	initial, err := r.cfg.Store.ReadInitialStageResults(ctx, work.TaskID, work.TraceID)
	if err != nil {
		return prior, fmt.Errorf("read safety review initial results: %w", err)
	}
	for _, result := range initial {
		if result.IndependenceDegraded {
			prior.IndependenceDegraded = true
		}
		if result.State == string(dao.SafetyReviewStageTerminalFailed) {
			prior.TerminalFailures = append(prior.TerminalFailures, result.StageKey)
			continue
		}
		if result.State != string(dao.SafetyReviewStageSucceeded) {
			continue
		}
		switch dto.SafetyReviewRole(result.Role) {
		case dto.SafetyReviewJudgeA:
			judgment, parseErr := r.validator.ValidateJudgment(work.Scene, result.ResultJSON)
			if parseErr != nil {
				return prior, fmt.Errorf("parse persisted judge A: %w", parseErr)
			}
			prior.JudgeA = judgment
		case dto.SafetyReviewJudgeB:
			judgment, parseErr := r.validator.ValidateJudgment(work.Scene, result.ResultJSON)
			if parseErr != nil {
				return prior, fmt.Errorf("parse persisted judge B: %w", parseErr)
			}
			prior.JudgeB = judgment
		case dto.SafetyReviewRouter:
			route, parseErr := parseSafetyReviewRoute(result.ResultJSON)
			if parseErr != nil {
				return prior, fmt.Errorf("parse persisted router: %w", parseErr)
			}
			if _, planErr := BuildSafetyReviewRoutingPlan(r.cfg.Policy, work.Scene, *route); planErr != nil {
				return prior, fmt.Errorf("normalize persisted router: %w", planErr)
			}
			prior.Router = *route
		}
	}
	experts, err := r.cfg.Store.ReadRoleStageResults(ctx, work.TaskID, work.TraceID, "expert")
	if err != nil {
		return prior, fmt.Errorf("read safety review expert results: %w", err)
	}
	for _, result := range experts {
		if result.State == string(dao.SafetyReviewStageTerminalFailed) {
			prior.TerminalFailures = append(prior.TerminalFailures, result.StageKey)
			continue
		}
		if result.State != string(dao.SafetyReviewStageSucceeded) {
			continue
		}
		expert, parseErr := r.validator.ValidateExpert(
			work.Scene, result.Axis, result.Category, result.ResultJSON,
		)
		if parseErr != nil {
			return prior, fmt.Errorf("parse persisted expert %s: %w", result.StageKey, parseErr)
		}
		prior.Experts = append(prior.Experts, expert)
	}
	prior.FallbackUsed = prior.IndependenceDegraded
	return prior, nil
}

// routingPlan 归一化 Router 结果并区分空结果。
func (r *SafetyReviewRunner) routingPlan(
	scene string,
	router *dto.SafetyReviewRoute,
) (*SafetyReviewRoutingPlan, error) {
	if router == nil {
		return nil, nil
	}
	plan, err := BuildSafetyReviewRoutingPlan(r.cfg.Policy, scene, *router)
	if err != nil {
		return nil, err
	}
	return &plan, nil
}

// assignSafetyReviewInitialResult 按角色保存已解析的初始阶段结果。
func assignSafetyReviewInitialResult(
	judges map[dto.SafetyReviewRole]*dto.SafetyReviewJudgment,
	router **dto.SafetyReviewRoute,
	role dto.SafetyReviewRole,
	result any,
) {
	switch typed := result.(type) {
	case *dto.SafetyReviewJudgment:
		judges[role] = typed
	case *dto.SafetyReviewRoute:
		*router = typed
	}
}

// reportFatal 向大小为一的 fatal channel 报告任务级错误。
func (r *SafetyReviewRunner) reportFatal(
	fatal chan error,
	cancel context.CancelFunc,
	err error,
) {
	if cancel != nil {
		cancel()
	}
	select {
	case fatal <- err:
	default:
	}
}

// newSafetyReviewSchedulerState 构造调度状态收集器。
func newSafetyReviewSchedulerState() *safetyReviewSchedulerState {
	return &safetyReviewSchedulerState{
		workAvailable: make(chan struct{}, 1),
	}
}

// signalWork 通知等待中的下游 worker 有新阶段可领取。
func (s *safetyReviewSchedulerState) signalWork() {
	select {
	case s.workAvailable <- struct{}{}:
	default:
	}
}

// requestExperts 按需启动 Expert worker 池。
func (s *safetyReviewSchedulerState) requestExperts() {
	if s.startExperts == nil {
		return
	}
	s.startExpertsOnce.Do(s.startExperts)
}

// requestArbiters 按需启动 Arbiter worker 池。
func (s *safetyReviewSchedulerState) requestArbiters() {
	if s.startArbiters == nil {
		return
	}
	s.startArbitersOnce.Do(s.startArbiters)
}

// recordItemState 记录条目的下游状态。
func (s *safetyReviewSchedulerState) recordItemState(itemState string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch itemState {
	case "resolved_safe":
		s.stats.ResolvedSafe++
	case "resolved_unsafe":
		s.stats.ResolvedUnsafe++
	case "quarantined":
		s.stats.Quarantined++
	case "awaiting_experts":
		s.stats.AwaitingExperts++
	case "pending_arbiter":
		s.stats.PendingArbiter++
	}
}

// recordStageOutcome 记录阶段处理计数。
func (s *safetyReviewSchedulerState) recordStageOutcome(outcome dao.SafetyReviewStageOutcome) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats.Processed++
	if outcome == dao.SafetyReviewStageTerminalFailed {
		s.stats.TerminalFailed++
	}
}

// snapshotStats 返回当前统计快照。
func (s *safetyReviewSchedulerState) snapshotStats() SafetyReviewRunStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stats
}

// hasExpertSafetyReviewStage 判断下游阶段中是否存在 Expert。
func hasExpertSafetyReviewStage(stages []dao.SafetyReviewStageSpec) bool {
	for _, stage := range stages {
		if stage.Role == "expert" {
			return true
		}
	}
	return false
}

// hasArbiterSafetyReviewStage 判断下游阶段中是否存在 Arbiter。
func hasArbiterSafetyReviewStage(stages []dao.SafetyReviewStageSpec) bool {
	for _, stage := range stages {
		if stage.Role == "arbiter" {
			return true
		}
	}
	return false
}
