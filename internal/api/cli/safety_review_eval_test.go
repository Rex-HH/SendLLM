package cli_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"sendllm/internal/api/cli"
	"sendllm/internal/dto"
	"sendllm/internal/service"
)

// TestSafetyReviewEvalCommandFakeE2E 验证 50 行双场景双 rotation 的 fake E2E 和中断恢复。
func TestSafetyReviewEvalCommandFakeE2E(t *testing.T) {
	env := "SAFETY_REVIEW_EVAL_TEST_API_KEY"
	t.Setenv(env, "test-key-value")
	root := safetyReviewCLIRepoRoot(t)
	temp := t.TempDir()
	hiddenPath := filepath.Join(temp, "Safety_Review_P04B_Hidden.jsonl")
	writeSafetyReviewEvalHidden(t, hiddenPath)
	server := newSafetyReviewEvalServer(t, filepath.Join(root, "policy", "releases", "p04b-v1.0"))
	server.setBlockTraceID("prompt-safe-01")
	validator, err := service.NewSafetyReviewValidator(server.policy)
	if err != nil {
		t.Fatalf("NewSafetyReviewValidator() error = %v", err)
	}
	if _, err := validator.ValidateExpert(
		"response", "attack_domain", "ethnic_discrimination",
		safetyReviewEvalExpertJSON(server.policy, "established", "response"),
	); err != nil {
		t.Fatalf("ValidateExpert(response) error = %v", err)
	}
	configPath := writeSafetyReviewEvalCLIConfig(t, server.URL, env, hiddenPath, filepath.Join(temp, "runs", "eval-test"))

	ctx, cancel := context.WithCancel(context.Background())
	exitCode := make(chan int, 1)
	var interruptedStdout, interruptedStderr strings.Builder
	go func() {
		exitCode <- cli.RunSafetyReview(ctx, []string{"eval", "--config", configPath}, &interruptedStdout, &interruptedStderr)
	}()
	select {
	case <-server.classificationEntered:
		cancel()
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatalf("fake E2E classification did not start; stderr=%s", interruptedStderr.String())
	}
	select {
	case code := <-exitCode:
		if code != 130 {
			t.Fatalf("interrupted eval exit = %d, want 130", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("interrupted eval did not finish")
	}
	server.setBlockTraceID("")

	var stdout, stderr strings.Builder
	code := cli.RunSafetyReview(
		context.Background(), []string{"eval", "--config", configPath}, &stdout, &stderr,
	)
	if code != 0 {
		t.Fatalf("RunSafetyReview(eval) = %d, want 0; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "eval=PASS") ||
		!strings.Contains(stdout.String(), "rotations=2") ||
		!strings.Contains(stdout.String(), "rotation=rotation-a input=50 clean=40 quarantine=10") ||
		!strings.Contains(stdout.String(), "unsafe_resolved=20 unsafe_safe=0 unsafe_quarantine=0") ||
		!strings.Contains(stdout.String(), "boundary_acceptable=10") {
		t.Fatalf("eval stdout = %q", stdout.String())
	}
	assertSafetyReviewEvalRotationArtifacts(t, filepath.Join(temp, "runs", "eval-test", "rotation-a"))
	assertSafetyReviewEvalRotationArtifacts(t, filepath.Join(temp, "runs", "eval-test", "rotation-b"))
	assertSafetyReviewEvalRotationIsolation(t, filepath.Join(temp, "runs", "eval-test"))
	if models := server.judgeModels("judge_a"); len(models) != 2 ||
		!models["GLM-5.3-Flash"] || !models["qwen3-max"] {
		t.Fatalf("judge_a models = %v, want both primary models across rotations", models)
	}
	if models := server.judgeModels("judge_b"); len(models) != 2 ||
		!models["GLM-5.3-Flash"] || !models["qwen3-max"] {
		t.Fatalf("judge_b models = %v, want both primary models across rotations", models)
	}
}

// TestSafetyReviewEvalCommandMissingHiddenFileIsBlocked 验证 hidden 文件缺失时返回输入错误。
func TestSafetyReviewEvalStillBlocksWhenHiddenGoldMissing(t *testing.T) {
	env := "SAFETY_REVIEW_EVAL_TEST_API_KEY"
	t.Setenv(env, "test-key-value")
	root := safetyReviewCLIRepoRoot(t)
	server := newSafetyReviewEvalServer(t, filepath.Join(root, "policy", "releases", "p04b-v1.0"))
	configPath := writeSafetyReviewEvalCLIConfig(
		t, server.URL, env, "/missing/Safety_Review_P04B_Hidden.jsonl", t.TempDir(),
	)
	var stdout, stderr strings.Builder
	code := cli.RunSafetyReview(
		context.Background(), []string{"eval", "--config", configPath}, &stdout, &stderr,
	)
	if code != 1 {
		t.Fatalf("RunSafetyReview(missing hidden) = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "error_category=input") {
		t.Fatalf("stderr = %q, want input error", stderr.String())
	}
	if server.requestCount() != 0 {
		t.Fatalf("network request count = %d, want 0", server.requestCount())
	}
}

// safetyReviewEvalServer 提供按 trace 前缀返回分类结果的本地模型服务。
type safetyReviewEvalServer struct {
	*httptest.Server
	mu                    sync.Mutex
	log                   []safetyReviewEvalRequest
	modelOutputCanary     string
	classificationFailure bool
	preflightSystemsMu    sync.Mutex
	preflightSystems      map[string]string
	policy                *service.SafetyReviewPolicy
	blockTraceID          string
	classificationEntered chan struct{}
}

// safetyReviewEvalRequest 记录角色、模型和 trace 的安全摘要。
type safetyReviewEvalRequest struct {
	role      string
	model     string
	traceID   string
	preflight bool
}

// newSafetyReviewEvalServer 构造类别感知的 fake provider。
func newSafetyReviewEvalServer(t *testing.T, bundleDir string) *safetyReviewEvalServer {
	t.Helper()
	policy, err := service.LoadSafetyReviewPolicy(bundleDir)
	if err != nil {
		t.Fatalf("LoadSafetyReviewPolicy() error = %v", err)
	}
	server := &safetyReviewEvalServer{
		preflightSystems:      make(map[string]string),
		policy:                policy,
		classificationEntered: make(chan struct{}, 1),
	}
	server.Server = httptest.NewServer(http.HandlerFunc(server.handle))
	t.Cleanup(server.Close)
	return server
}

// handle 按角色和 trace 前缀返回合法结构化输出。
func (s *safetyReviewEvalServer) handle(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid request"}}`))
		return
	}
	role := "unknown"
	traceID := ""
	preflight := len(request.Messages) == 2 && strings.Contains(request.Messages[1].Content, `"preflight"`)
	if len(request.Messages) != 0 {
		role = safetyReviewCLIRole(request.Messages[0].Content)
		traceID = safetyReviewEvalTraceID(request.Messages[1].Content)
	}
	s.record(role, request.Model, traceID, preflight)
	if preflight {
		s.recordPreflightSystem(role, request.Messages[0].Content)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{}"},"finish_reason":"stop"}]}`))
		return
	}
	blockTraceID, classificationFailure := s.runtimeBehavior()
	if blockTraceID != "" && traceID == blockTraceID {
		s.notifyClassification()
		<-r.Context().Done()
		return
	}
	if classificationFailure {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"classification unavailable"}}`))
		return
	}
	content := s.modelOutput(role, traceID)
	_, _ = w.Write([]byte(fmt.Sprintf(
		`{"choices":[{"message":{"content":%q},"finish_reason":"stop"}]}`, content,
	)))
}

// setBlockTraceID 设置需要阻塞的样本，避免测试服务与控制协程发生数据竞争。
func (s *safetyReviewEvalServer) setBlockTraceID(traceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blockTraceID = traceID
}

// setClassificationFailure 设置分类失败注入状态。
func (s *safetyReviewEvalServer) setClassificationFailure(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.classificationFailure = enabled
}

// runtimeBehavior 返回一次请求使用的失败注入状态快照。
func (s *safetyReviewEvalServer) runtimeBehavior() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.blockTraceID, s.classificationFailure
}

// record 记录一次请求摘要。
func (s *safetyReviewEvalServer) record(role, model, traceID string, preflight bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log = append(s.log, safetyReviewEvalRequest{
		role: role, model: model, traceID: traceID, preflight: preflight,
	})
}

// notifyClassification 通知测试分类请求已进入。
func (s *safetyReviewEvalServer) notifyClassification() {
	select {
	case s.classificationEntered <- struct{}{}:
	default:
	}
}

// modelOutput 返回指定角色和 trace 的合法 JSON。
func (s *safetyReviewEvalServer) modelOutput(role, traceID string) string {
	unsafe := strings.Contains(traceID, "unsafe")
	boundary := strings.Contains(traceID, "boundary")
	scene := "response"
	if strings.HasPrefix(traceID, "prompt") {
		scene = "prompt"
	}
	evidenceSource := safetyReviewEvalEvidenceSource(scene)
	switch role {
	case "judge_a", "judge_b":
		if s.modelOutputCanary != "" {
			return `{"verdict":"safe","evidence_complete":true,"evidence":[],` +
				`"attack_methods":[],"attack_domains":[],"exclusions":[],` +
				`"rationale":"` + s.modelOutputCanary + `"}`
		}
		if unsafe {
			return safetyReviewEvalJSON(tJudgmentUnsafe)
		}
		if boundary {
			return safetyReviewEvalJSON(tJudgmentUncertain)
		}
		return safetyReviewEvalJSON(tJudgmentSafe)
	case "router":
		if unsafe || boundary {
			return safetyReviewEvalJSON(dto.SafetyReviewRoute{
				Features: []dto.SafetyReviewFeature{{
					ID: "F1", Source: evidenceSource, Kind: "group_target", Span: "synthetic group target",
				}},
				AttackDomainCandidates: []dto.SafetyReviewCandidate{{
					Category: "ethnic_discrimination", EvidenceIDs: []string{"F1"}, Reason: "recall",
				}},
				CoverageComplete: true,
			})
		}
		return safetyReviewEvalJSON(dto.SafetyReviewRoute{
			Features: []dto.SafetyReviewFeature{}, AttackMethodCandidates: []dto.SafetyReviewCandidate{},
			AttackDomainCandidates: []dto.SafetyReviewCandidate{}, CoverageComplete: true,
		})
	case "expert":
		if boundary {
			return string(safetyReviewEvalExpertJSON(s.policy, "uncertain", scene))
		}
		return string(safetyReviewEvalExpertJSON(s.policy, "established", scene))
	case "arbiter":
		if boundary {
			return safetyReviewEvalJSON(dto.SafetyReviewDecision{
				Verdict: "quarantine", AttackMethods: []string{}, AttackDomains: []string{},
				EvidenceBasis: []dto.SafetyReviewEvidence{}, DecisionRules: []string{"DISCRIMINATION-R01"},
				QuarantineReason: "irreducible_uncertainty", Rationale: "合成隔离决策。",
			})
		}
		if unsafe {
			return safetyReviewEvalJSON(dto.SafetyReviewDecision{
				Verdict: "resolved", Label: "unsafe", AttackMethods: []string{},
				AttackDomains:       []string{"ethnic_discrimination"},
				PrimaryAttackDomain: "ethnic_discrimination", PrimaryRiskType: "ethnic_discrimination",
				CaseType: "typical", EvidenceBasis: []dto.SafetyReviewEvidence{{
					Source: evidenceSource, Kind: "negative_description", Span: "合成证据。",
				}}, DecisionRules: []string{"DISCRIMINATION-R01"}, Rationale: "合成不安全决策。",
			})
		}
		return safetyReviewEvalJSON(dto.SafetyReviewDecision{
			Verdict: "resolved", Label: "safe", AttackMethods: []string{}, AttackDomains: []string{},
			CaseType: "typical", EvidenceBasis: []dto.SafetyReviewEvidence{{
				Source: evidenceSource, Kind: "refusal", Span: "合成安全证据。",
			}}, DecisionRules: []string{"DISCRIMINATION-R01"}, Rationale: "合成安全决策。",
		})
	default:
		return "{}"
	}
}

// judgeModels 返回指定角色使用过的模型集合。
func (s *safetyReviewEvalServer) judgeModels(role string) map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	models := map[string]bool{}
	for _, entry := range s.log {
		if entry.role == role && !entry.preflight {
			models[entry.model] = true
		}
	}
	return models
}

// recordPreflightSystem 记录角色 preflight 的系统提示词摘要。
func (s *safetyReviewEvalServer) recordPreflightSystem(role, system string) {
	s.preflightSystemsMu.Lock()
	defer s.preflightSystemsMu.Unlock()
	s.preflightSystems[role] = system
}

// preflightSystemsSnapshot 返回角色系统提示词快照。
func (s *safetyReviewEvalServer) preflightSystemsSnapshot() map[string]string {
	s.preflightSystemsMu.Lock()
	defer s.preflightSystemsMu.Unlock()
	return map[string]string{
		"judge_a": s.preflightSystems["judge_a"],
		"judge_b": s.preflightSystems["judge_b"],
		"router":  s.preflightSystems["router"],
		"expert":  s.preflightSystems["expert"],
		"arbiter": s.preflightSystems["arbiter"],
	}
}

// preflightEntries 返回 preflight 请求摘要副本。
func (s *safetyReviewEvalServer) preflightEntries() []safetyReviewEvalRequest {
	entries := s.entries()
	result := make([]safetyReviewEvalRequest, 0)
	for _, entry := range entries {
		if entry.preflight {
			result = append(result, entry)
		}
	}
	return result
}

// entries 返回请求摘要副本。
func (s *safetyReviewEvalServer) entries() []safetyReviewEvalRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]safetyReviewEvalRequest(nil), s.log...)
}

// classificationCount 返回分类请求数。
func (s *safetyReviewEvalServer) classificationCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, entry := range s.log {
		if !entry.preflight {
			count++
		}
	}
	return count
}

// classificationCountForTrace 返回指定样本的分类请求数。
func (s *safetyReviewEvalServer) classificationCountForTrace(traceID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, entry := range s.log {
		if !entry.preflight && entry.traceID == traceID {
			count++
		}
	}
	return count
}

// classificationRoleCounts 返回每个角色的分类请求数。
func (s *safetyReviewEvalServer) classificationRoleCounts() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	counts := make(map[string]int)
	for _, entry := range s.log {
		if !entry.preflight {
			counts[entry.role]++
		}
	}
	return counts
}

// requestCount 返回总请求数。
func (s *safetyReviewEvalServer) requestCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.log)
}

// safetyReviewEvalTraceID 从用户 JSON 中提取 trace_id。
func safetyReviewEvalTraceID(content string) string {
	var payload struct {
		TraceID string `json:"trace_id"`
	}
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return ""
	}
	return payload.TraceID
}

// safetyReviewEvalJSON 序列化对象为 JSON 字符串。
func safetyReviewEvalJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

// safetyReviewEvalExpertJSON 生成指定 verdict 的合法 Expert 输出。
func safetyReviewEvalExpertJSON(policy *service.SafetyReviewPolicy, verdict, scene string) []byte {
	card := policy.Cards["ethnic_discrimination"]
	conditions := make([]dto.SafetyReviewCondition, 0, len(card.RequiredConditions))
	for _, condition := range card.RequiredConditions {
		state := "unknown"
		refs := []string{}
		if verdict == "established" {
			state = "satisfied"
			refs = []string{"E1"}
		}
		conditions = append(conditions, dto.SafetyReviewCondition{
			ID: condition.ID, State: state, EvidenceRefs: refs,
		})
	}
	exclusions := make([]dto.SafetyReviewExclusion, 0, len(card.DecisiveExclusions))
	for _, exclusion := range card.DecisiveExclusions {
		state := "unknown"
		if verdict == "established" {
			state = "absent"
		}
		exclusions = append(exclusions, dto.SafetyReviewExclusion{
			ID: exclusion.ID, State: state,
		})
	}
	evidence := []dto.SafetyReviewEvidence{}
	evidenceSource := []string{}
	if verdict == "established" {
		evidence = append(evidence, dto.SafetyReviewEvidence{
			Source: safetyReviewEvalEvidenceSource(scene), Kind: "negative_description", Span: "合成证据。",
		})
		evidenceSource = []string{safetyReviewEvalEvidenceSource(scene)}
	}
	raw, err := json.Marshal(dto.SafetyReviewExpertResult{
		Axis: card.Axis, Category: card.ID, Verdict: verdict,
		Conditions: conditions, DecisiveExclusions: exclusions,
		Evidence: evidence, EvidenceSource: evidenceSource, Rationale: "合成专家结论。",
	})
	if err != nil {
		panic(err)
	}
	return raw
}

// safetyReviewEvalEvidenceSource 返回场景对应的合法证据来源。
func safetyReviewEvalEvidenceSource(scene string) string {
	if scene == "prompt" {
		return "prompt"
	}
	return "response_explicit"
}

// safetyReviewEvalJudgmentWire 表示测试用的固定裁判输出。
type safetyReviewEvalJudgmentWire struct {
	Verdict          string                     `json:"verdict"`
	EvidenceComplete bool                       `json:"evidence_complete"`
	Evidence         []dto.SafetyReviewEvidence `json:"evidence"`
	AttackMethods    []string                   `json:"attack_methods"`
	AttackDomains    []string                   `json:"attack_domains"`
	Exclusions       []string                   `json:"exclusions"`
	Rationale        string                     `json:"rationale"`
}

// tJudgmentSafe 是安全裁判输出。
var tJudgmentSafe = safetyReviewEvalJudgmentWire{
	Verdict: "safe", EvidenceComplete: true, Evidence: []dto.SafetyReviewEvidence{},
	AttackMethods: []string{}, AttackDomains: []string{}, Exclusions: []string{},
	Rationale: "safe",
}

// tJudgmentUnsafe 是不安全裁判输出。
var tJudgmentUnsafe = safetyReviewEvalJudgmentWire{
	Verdict: "unsafe", EvidenceComplete: true, Evidence: []dto.SafetyReviewEvidence{},
	AttackMethods: []string{}, AttackDomains: []string{}, Exclusions: []string{},
	Rationale: "unsafe",
}

// tJudgmentUncertain 是不确定裁判输出。
var tJudgmentUncertain = safetyReviewEvalJudgmentWire{
	Verdict: "uncertain", EvidenceComplete: false, Evidence: []dto.SafetyReviewEvidence{},
	AttackMethods: []string{}, AttackDomains: []string{}, Exclusions: []string{},
	Rationale: "uncertain",
}

// writeSafetyReviewEvalHidden 写入 50 条合成隐藏集。
func writeSafetyReviewEvalHidden(t *testing.T, path string) {
	t.Helper()
	var builder strings.Builder
	for index := 1; index <= 10; index++ {
		writeSafetyReviewEvalHiddenRow(&builder, "prompt-unsafe-"+fmt.Sprintf("%02d", index), "prompt", "unsafe", "typical")
		writeSafetyReviewEvalHiddenRow(&builder, "prompt-safe-"+fmt.Sprintf("%02d", index), "prompt", "safe", "typical")
		writeSafetyReviewEvalHiddenRow(
			&builder, "response-unsafe-"+fmt.Sprintf("%02d", index), "response", "unsafe", "typical",
		)
		writeSafetyReviewEvalHiddenRow(
			&builder, "response-safe-"+fmt.Sprintf("%02d", index), "response", "safe", "hard_negative",
		)
		writeSafetyReviewEvalHiddenRow(&builder, "boundary-"+fmt.Sprintf("%02d", index), "response", "safe", "borderline")
	}
	if err := os.WriteFile(path, []byte(builder.String()), 0o600); err != nil {
		t.Fatalf("write hidden: %v", err)
	}
}

// writeSafetyReviewEvalHiddenRow 写入一条合成隐藏集记录。
func writeSafetyReviewEvalHiddenRow(builder *strings.Builder, traceID, scene, label, caseType string) {
	row := map[string]any{
		"trace_id": traceID, "scene": scene, "prompt": "synthetic prompt", "response": "synthetic response",
		"gold_label": label, "case_type": caseType, "risk_type": "", "source": "approved_gold",
	}
	if label == "unsafe" {
		row["risk_type"] = "ethnic_discrimination"
	}
	if scene == "prompt" {
		row["response"] = ""
	}
	raw, err := json.Marshal(row)
	if err != nil {
		panic(err)
	}
	builder.Write(raw)
	builder.WriteByte('\n')
}

// writeSafetyReviewEvalCLIConfig 写入 eval 命令的基础严格配置。
func writeSafetyReviewEvalCLIConfig(t *testing.T, baseURL, env, inputPath, taskDir string) string {
	t.Helper()
	root := safetyReviewCLIRepoRoot(t)
	config := fmt.Sprintf(`version: 1
task:
  id: eval-test
  input: %s
  task_dir: %s
  scene: response
policy:
  bundle_dir: %s
models:
  profiles:
    glm_5_2:
      family: glm
      base_url: %s
      api_key_env: %s
      name: GLM-5.3-Flash
      structured_output: json_object
      max_tokens: 2000
      timeout: 2s
    qwen3_max:
      family: qwen
      base_url: %s
      api_key_env: %s
      name: qwen3-max
      structured_output: json_object
      max_tokens: 2000
      timeout: 2s
    minimax_m2_5:
      family: minimax
      base_url: %s
      api_key_env: %s
      name: MiniMax-M2.5
      structured_output: json_object
      max_tokens: 2000
      timeout: 2s
      quota_group: minimax_shared
    deepseek_v4_pro:
      family: deepseek
      base_url: %s
      api_key_env: %s
      name: deepseek-v4-pro
      structured_output: json_object
      max_tokens: 2000
      timeout: 2s
  roles:
    judge_a:
      primary: glm_5_2
      fallbacks: [deepseek_v4_pro]
      concurrency: 1
      requests_per_minute: 0
      tokens_per_minute: 0
    judge_b:
      primary: qwen3_max
      fallbacks: [minimax_m2_5]
      concurrency: 1
      requests_per_minute: 0
      tokens_per_minute: 0
    router:
      primary: minimax_m2_5
      fallbacks: [qwen3_max]
      concurrency: 1
      requests_per_minute: 0
      tokens_per_minute: 0
    expert:
      primary: minimax_m2_5
      fallbacks: [deepseek_v4_pro]
      concurrency: 1
      requests_per_minute: 0
      tokens_per_minute: 0
    arbiter:
      primary: deepseek_v4_pro
      fallbacks: [glm_5_2]
      concurrency: 1
      requests_per_minute: 0
      tokens_per_minute: 0
  quota_groups:
    minimax_shared:
      concurrency: 1
      requests_per_minute: 0
      tokens_per_minute: 0
runtime:
  shutdown_timeout: 200ms
  status_interval: 5s
retry:
  transient_attempts_per_model: 1
  format_repair_attempts: 1
  refusal_reprompt_attempts: 1
  initial_backoff: 1ms
  max_backoff: 2ms
output:
  clean: clean.jsonl
  audit: audit.jsonl
  quality_events: quality-events.jsonl
  quarantine: quarantine.jsonl
  report: report.json
  run_status: run-status.json
`, inputPath, taskDir, filepath.Join(root, "policy", "releases", "p04b-v1.0"),
		baseURL, env, baseURL, env, baseURL, env, baseURL, env)
	path := filepath.Join(t.TempDir(), "safety-review-eval.yaml")
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatalf("write eval config: %v", err)
	}
	return path
}

// assertSafetyReviewEvalRotationArtifacts 验证一个 rotation 的两个场景导出。
func assertSafetyReviewEvalRotationArtifacts(t *testing.T, rotationDir string) {
	t.Helper()
	clean := readSafetyReviewCLIJSONL(t, filepath.Join(rotationDir, "prompt", "clean.jsonl"))
	clean = append(clean, readSafetyReviewCLIJSONL(t, filepath.Join(rotationDir, "response", "clean.jsonl"))...)
	if len(clean) != 40 {
		t.Fatalf("rotation clean rows = %d, want 40", len(clean))
	}
	quarantine := readSafetyReviewCLIJSONL(t, filepath.Join(rotationDir, "prompt", "quarantine.jsonl"))
	quarantine = append(
		quarantine, readSafetyReviewCLIJSONL(t, filepath.Join(rotationDir, "response", "quarantine.jsonl"))...,
	)
	if len(quarantine) != 10 {
		t.Fatalf("rotation quarantine rows = %d, want 10", len(quarantine))
	}
	for _, scene := range []string{"prompt", "response"} {
		if _, err := os.Stat(filepath.Join(rotationDir, scene, "state.db")); err != nil {
			t.Fatalf("rotation %s state db missing: %v", scene, err)
		}
		if _, err := os.Stat(filepath.Join(rotationDir, scene, "report.json")); err != nil {
			t.Fatalf("rotation %s report missing: %v", scene, err)
		}
	}
}

// assertSafetyReviewEvalRotationIsolation 验证两轮 rotation 不复用状态或输出。
func assertSafetyReviewEvalRotationIsolation(t *testing.T, baseDir string) {
	t.Helper()
	aStat, err := os.Stat(filepath.Join(baseDir, "rotation-a", "prompt", "state.db"))
	if err != nil {
		t.Fatalf("rotation-a state stat: %v", err)
	}
	bStat, err := os.Stat(filepath.Join(baseDir, "rotation-b", "prompt", "state.db"))
	if err != nil {
		t.Fatalf("rotation-b state stat: %v", err)
	}
	if os.SameFile(aStat, bStat) {
		t.Fatal("rotation state databases are the same file")
	}
}
