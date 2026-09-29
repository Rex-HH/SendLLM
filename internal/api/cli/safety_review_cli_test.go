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
	"sendllm/internal/dao"
)

// TestSafetyReviewCLIValidateZeroNetwork 验证 validate 不发起任何网络请求。
func TestSafetyReviewCLIValidateZeroNetwork(t *testing.T) {
	env := "SAFETY_REVIEW_TEST_API_KEY"
	t.Setenv(env, "test-key-value")
	server := newSafetyReviewCLIServer(t, false)
	path := writeSafetyReviewCLIConfig(t, server.URL, env, safetyReviewCLIInput("one"))
	var stdout, stderr strings.Builder

	code := cli.RunSafetyReview(
		context.Background(), []string{"validate", "--config", path}, &stdout, &stderr,
	)
	if code != 0 {
		t.Fatalf("RunSafetyReview(validate) = %d, want 0; stderr=%s", code, stderr.String())
	}
	if server.requestCount() != 0 {
		t.Fatalf("network request count = %d, want 0", server.requestCount())
	}
	if !strings.Contains(stdout.String(), "validation=PASS") {
		t.Fatalf("stdout = %q, want validation pass summary", stdout.String())
	}
}

// TestSafetyReviewCLIRunOrdersPreflightBeforeClaims 验证 run 先完成全部 preflight 再执行分类。
func TestSafetyReviewCLIRunOrdersPreflightBeforeClaims(t *testing.T) {
	env := "SAFETY_REVIEW_TEST_API_KEY"
	t.Setenv(env, "test-key-value")
	server := newSafetyReviewCLIServer(t, false)
	path := writeSafetyReviewCLIConfig(t, server.URL, env, safetyReviewCLIInput("one"))
	var stdout, stderr strings.Builder

	code := cli.RunSafetyReview(
		context.Background(), []string{"run", "--config", path}, &stdout, &stderr,
	)
	if code != 0 {
		t.Fatalf("RunSafetyReview(run) = %d, want 0; stderr=%s", code, stderr.String())
	}
	entries := server.entries()
	preflight := 0
	for index, entry := range entries {
		if entry.preflight {
			preflight++
			continue
		}
		if preflight != 10 {
			t.Fatalf("classification at request %d before all 10 preflights", index+1)
		}
	}
	if preflight != 10 {
		t.Fatalf("preflight count = %d, want 10", preflight)
	}
	preflightRoles := map[string]int{}
	for _, entry := range entries {
		if entry.preflight {
			preflightRoles[entry.role]++
		}
	}
	for _, role := range []string{"judge_a", "judge_b", "router", "expert", "arbiter"} {
		if preflightRoles[role] != 2 {
			t.Fatalf("preflight role %s count = %d, want 2; all roles=%v", role, preflightRoles[role], preflightRoles)
		}
	}
	if classification := server.classificationCount(); classification != 3 {
		t.Fatalf("classification count = %d, want 3", classification)
	}
	if !strings.Contains(stdout.String(), "status=completed") {
		t.Fatalf("stdout = %q, want completed summary", stdout.String())
	}
	for _, status := range []string{"status=preflight", "status=running", "status=exporting"} {
		if !strings.Contains(stdout.String(), status) {
			t.Fatalf("stdout = %q, want %s", stdout.String(), status)
		}
	}
	assertSafetyReviewCLIExportArtifacts(t, configTaskDir(path), "one")
}

// TestSafetyReviewCLIRunResumesWithoutDuplicateClassification 验证完成后的重跑不重复分类。
func TestSafetyReviewCLIRunResumesWithoutDuplicateClassification(t *testing.T) {
	env := "SAFETY_REVIEW_TEST_API_KEY"
	t.Setenv(env, "test-key-value")
	server := newSafetyReviewCLIServer(t, false)
	path := writeSafetyReviewCLIConfig(t, server.URL, env, safetyReviewCLIInput("one"))

	for index := 0; index < 2; index++ {
		var stdout, stderr strings.Builder
		code := cli.RunSafetyReview(
			context.Background(), []string{"run", "--config", path}, &stdout, &stderr,
		)
		if code != 0 {
			t.Fatalf("run %d exit = %d, want 0; stderr=%s", index+1, code, stderr.String())
		}
	}
	if got := server.classificationCount(); got != 3 {
		t.Fatalf("classification count after resume = %d, want 3", got)
	}
}

// TestSafetyReviewCLIPreflightFailureLeavesZeroExecutions 验证 preflight 失败不产生阶段执行或决策。
func TestSafetyReviewCLIPreflightFailureLeavesZeroExecutions(t *testing.T) {
	env := "SAFETY_REVIEW_TEST_API_KEY"
	t.Setenv(env, "test-key-value")
	server := newSafetyReviewCLIServer(t, true)
	path := writeSafetyReviewCLIConfig(t, server.URL, env, safetyReviewCLIInput("one"))
	var stdout, stderr strings.Builder

	code := cli.RunSafetyReview(
		context.Background(), []string{"run", "--config", path}, &stdout, &stderr,
	)
	if code != 1 {
		t.Fatalf("RunSafetyReview(preflight failure) = %d, want 1", code)
	}
	if server.classificationCount() != 0 {
		t.Fatalf("classification count = %d, want 0", server.classificationCount())
	}
	summary := readSafetyReviewCLISummary(t, path)
	if summary.Stages["pending"] != 3 || summary.Stages["running"] != 0 ||
		summary.Stages["succeeded"] != 0 || summary.Decisions != 0 {
		t.Fatalf("summary after preflight failure = %+v", summary)
	}
}

// TestSafetyReviewCLICancellationDrainsAndWritesStatus 验证取消后停止新领取、保留完成结果
// 并写状态。
func TestSafetyReviewCLICancellationDrainsAndWritesStatus(t *testing.T) {
	env := "SAFETY_REVIEW_TEST_API_KEY"
	t.Setenv(env, "test-key-value")
	server := newSafetyReviewCLIServer(t, false)
	server.blockClassification = make(chan struct{})
	path := writeSafetyReviewCLIConfig(
		t, server.URL, env, safetyReviewCLIInput("one")+safetyReviewCLIInput("two"),
	)
	ctx, cancel := context.WithCancel(context.Background())
	code := make(chan int, 1)
	var stdout, stderr strings.Builder

	go func() {
		code <- cli.RunSafetyReview(ctx, []string{"run", "--config", path}, &stdout, &stderr)
	}()
	<-server.classificationEntered
	cancel()
	close(server.blockClassification)

	select {
	case got := <-code:
		if got != 130 {
			t.Fatalf("cancelled exit = %d, want 130", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled run did not finish")
	}
	if got := server.classificationCount(); got != 3 {
		t.Fatalf("classification count after cancellation = %d, want 3", got)
	}
	summary := readSafetyReviewCLISummary(t, path)
	if summary.Items["resolved_safe"] != 1 || summary.Items["pending_initial"] != 1 ||
		summary.Stages["running"] != 0 {
		t.Fatalf("summary after cancellation = %+v", summary)
	}
	status := readSafetyReviewCLIJSON(t, filepath.Join(configTaskDir(path), "run-status.json"))
	if status["status"] != "interrupted" {
		t.Fatalf("run status = %#v, want interrupted", status)
	}
	tempStatus, err := filepath.Glob(filepath.Join(configTaskDir(path), ".run-status-*"))
	if err != nil || len(tempStatus) != 0 {
		t.Fatalf("temporary run-status files = %v, err = %v", tempStatus, err)
	}
}

// TestSafetyReviewCLIPayloadRedaction 验证输出不包含输入、凭据或模型输出载荷。
func TestSafetyReviewCLIPayloadRedaction(t *testing.T) {
	env := "SAFETY_REVIEW_TEST_API_KEY"
	keyValue := "CLI_SECRET_KEY_CANARY"
	t.Setenv(env, keyValue)
	server := newSafetyReviewCLIServer(t, false)
	server.modelOutputCanary = "CLI_RAW_MODEL_OUTPUT_CANARY"
	path := writeSafetyReviewCLIConfig(
		t, server.URL, env, safetyReviewCLIInputPayload(),
	)
	var stdout, stderr strings.Builder

	code := cli.RunSafetyReview(
		context.Background(), []string{"run", "--config", path}, &stdout, &stderr,
	)
	if code != 0 {
		t.Fatalf("RunSafetyReview(redaction) = %d, want 0; stderr=%s", code, stderr.String())
	}
	statusPath := filepath.Join(configTaskDir(path), "run-status.json")
	statusBytes, err := os.ReadFile(statusPath)
	if err != nil {
		t.Fatalf("read run status: %v", err)
	}
	combined := stdout.String() + stderr.String() + string(statusBytes)
	for _, name := range []string{"audit.jsonl", "quality-events.jsonl", "report.json"} {
		raw, readErr := os.ReadFile(filepath.Join(configTaskDir(path), name))
		if readErr != nil {
			t.Fatalf("read %s: %v", name, readErr)
		}
		combined += string(raw)
	}
	for _, canary := range []string{"CLI_PROMPT_CANARY", "CLI_RESPONSE_CANARY", keyValue, server.modelOutputCanary} {
		if strings.Contains(combined, canary) {
			t.Fatalf("output contains canary %q", canary)
		}
	}
}

// assertSafetyReviewCLIExportArtifacts 验证 run 生成完整原子导出。
func assertSafetyReviewCLIExportArtifacts(t *testing.T, taskDir, traceID string) {
	t.Helper()
	clean := readSafetyReviewCLIJSONL(t, filepath.Join(taskDir, "clean.jsonl"))
	if len(clean) != 1 || clean[0]["trace_id"] != traceID {
		t.Fatalf("clean export = %#v", clean)
	}
	annotation, _ := clean[0]["annotation"].(map[string]any)
	if annotation["method"] != "auto" || annotation["label"] != "safe" {
		t.Fatalf("clean annotation = %#v", annotation)
	}
	if len(readSafetyReviewCLIJSONL(t, filepath.Join(taskDir, "quarantine.jsonl"))) != 0 {
		t.Fatal("quarantine export is not empty")
	}
	if len(readSafetyReviewCLIJSONL(t, filepath.Join(taskDir, "audit.jsonl"))) != 1 ||
		len(readSafetyReviewCLIJSONL(t, filepath.Join(taskDir, "quality-events.jsonl"))) != 1 {
		t.Fatal("audit or quality-events export has wrong row count")
	}
	if _, err := os.Stat(filepath.Join(taskDir, "report.json")); err != nil {
		t.Fatalf("report export missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(taskDir, "run-status.json")); err != nil {
		t.Fatalf("run-status export missing: %v", err)
	}
	temps, err := filepath.Glob(filepath.Join(taskDir, ".safety-review-*"))
	if err != nil || len(temps) != 0 {
		t.Fatalf("temporary exports remain: %v, err = %v", temps, err)
	}
}

// readSafetyReviewCLIJSONL 读取 CLI 导出的 JSONL 行。
func readSafetyReviewCLIJSONL(t *testing.T, path string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var result []map[string]any
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		value := map[string]any{}
		if err := json.Unmarshal([]byte(line), &value); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		result = append(result, value)
	}
	return result
}

// TestSafetyReviewCLIReturnsOneForFailures 验证 usage 和配置、输入、环境失败返回 1。
func TestSafetyReviewCLIReturnsOneForFailures(t *testing.T) {
	env := "SAFETY_REVIEW_TEST_API_KEY"
	t.Setenv(env, "test-key-value")
	server := newSafetyReviewCLIServer(t, false)
	valid := writeSafetyReviewCLIConfig(t, server.URL, env, safetyReviewCLIInput("one"))

	tests := []struct {
		name string
		args []string
	}{
		{name: "no command", args: nil},
		{name: "unsupported command", args: []string{"status", "--task-dir", "/tmp"}},
		{name: "missing config", args: []string{"validate", "--config", "/missing/config.yaml"}},
		{name: "missing policy", args: []string{
			"validate", "--config", mutateSafetyReviewCLIConfig(t, valid, "missing-policy"),
		}},
		{name: "invalid input", args: []string{
			"validate", "--config", writeSafetyReviewCLIConfig(t, server.URL, env, "not-json\n"),
		}},
		{name: "missing input", args: []string{
			"validate", "--config", valid,
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr strings.Builder
			if test.name == "missing input" {
				_ = os.Remove(inputPathForSafetyReviewConfig(valid))
			}
			if code := cli.RunSafetyReview(context.Background(), test.args, &stdout, &stderr); code != 1 {
				t.Fatalf("RunSafetyReview(%s) = %d, want 1", test.name, code)
			}
		})
	}
}

// mutateSafetyReviewCLIConfig 生成策略路径缺失的配置副本。
func mutateSafetyReviewCLIConfig(t *testing.T, source, mutation string) string {
	t.Helper()
	raw, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read source config: %v", err)
	}
	if mutation == "missing-policy" {
		raw = []byte(strings.Replace(
			string(raw), "policy/releases/p04b-v1.0", "policy/releases/missing", 1,
		))
	}
	target := filepath.Join(t.TempDir(), "safety-review.yaml")
	if err := os.WriteFile(target, raw, 0o600); err != nil {
		t.Fatalf("write mutated config: %v", err)
	}
	return target
}

// TestSafetyReviewCLISharedAPIEnvAllowed 验证多个 Profile 可复用同一个非空 API env。
func TestSafetyReviewCLISharedAPIEnvAllowed(t *testing.T) {
	env := "SAFETY_REVIEW_SHARED_API_KEY"
	t.Setenv(env, "shared-key-value")
	server := newSafetyReviewCLIServer(t, false)
	path := writeSafetyReviewCLIConfig(t, server.URL, env, safetyReviewCLIInput("one"))
	var stdout, stderr strings.Builder

	code := cli.RunSafetyReview(
		context.Background(), []string{"validate", "--config", path}, &stdout, &stderr,
	)
	if code != 0 {
		t.Fatalf("RunSafetyReview(shared env) = %d, want 0; stderr=%s", code, stderr.String())
	}
	if strings.Contains(stdout.String()+stderr.String(), "shared-key-value") {
		t.Fatal("API key value was printed")
	}
}

// TestSafetyReviewCLIMissingAPIEnvFails 验证 API env 缺失时返回 1 且不打印值。
func TestSafetyReviewCLIMissingAPIEnvFails(t *testing.T) {
	env := "SAFETY_REVIEW_MISSING_API_KEY"
	server := newSafetyReviewCLIServer(t, false)
	path := writeSafetyReviewCLIConfig(t, server.URL, env, safetyReviewCLIInput("one"))
	var stdout, stderr strings.Builder

	code := cli.RunSafetyReview(
		context.Background(), []string{"validate", "--config", path}, &stdout, &stderr,
	)
	if code != 1 {
		t.Fatalf("RunSafetyReview(missing env) = %d, want 1", code)
	}
	combined := stdout.String() + stderr.String()
	if strings.Contains(combined, "test-key") || strings.Contains(combined, "AI_GATEWAY") {
		t.Fatalf("output leaked API key material: %s", combined)
	}
}

// TestSafetyReviewCLIRejectsTaskPathFile 验证 task_dir 指向普通文件时返回路径错误。
func TestSafetyReviewCLIRejectsTaskPathFile(t *testing.T) {
	env := "SAFETY_REVIEW_TEST_API_KEY"
	t.Setenv(env, "test-key-value")
	server := newSafetyReviewCLIServer(t, false)
	path := writeSafetyReviewCLIConfig(t, server.URL, env, safetyReviewCLIInput("one"))
	taskDir := configTaskDir(path)
	if err := os.MkdirAll(filepath.Dir(taskDir), 0o700); err != nil {
		t.Fatalf("create task parent: %v", err)
	}
	if err := os.WriteFile(taskDir, []byte("not-a-directory"), 0o600); err != nil {
		t.Fatalf("create task file: %v", err)
	}
	var stdout, stderr strings.Builder

	code := cli.RunSafetyReview(
		context.Background(), []string{"validate", "--config", path}, &stdout, &stderr,
	)
	if code != 1 {
		t.Fatalf("RunSafetyReview(task path file) = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "error_category=path") {
		t.Fatalf("stderr = %q, want path error", stderr.String())
	}
}

// TestSafetyReviewCLIStatusAndWatch 验证 status 命令支持一次性输出和只读 watch。
func TestSafetyReviewCLIStatusAndWatch(t *testing.T) {
	directory := t.TempDir()
	store, err := dao.OpenSafetyReview(context.Background(), filepath.Join(directory, "state.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.EnsureTask(context.Background(), dao.SafetyReviewTask{
		ID: "status-test", SemanticFingerprint: "fingerprint", Scene: "response",
		SnapshotDir: filepath.Join(directory, "snapshots"),
	}); err != nil {
		t.Fatalf("ensure task: %v", err)
	}
	if _, err := store.ImportJSONL(context.Background(), "status-test", strings.NewReader(
		`{"trace_id":"one","prompt":"PROMPT_CANARY","response":"RESPONSE_CANARY"}`+"\n",
	)); err != nil {
		t.Fatalf("import item: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	var stdout, stderr strings.Builder
	code := cli.RunSafetyReview(
		context.Background(), []string{"status", "--task-dir", directory}, &stdout, &stderr,
	)
	if code != 0 {
		t.Fatalf("RunSafetyReview(status) = %d, want 0; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "task_id=status-test") ||
		!strings.Contains(stdout.String(), "items_total=1") {
		t.Fatalf("status stdout = %q", stdout.String())
	}
	if strings.Contains(stdout.String()+stderr.String(), "PROMPT_CANARY") ||
		strings.Contains(stdout.String()+stderr.String(), "RESPONSE_CANARY") {
		t.Fatal("status output leaked source payload")
	}

	stdout.Reset()
	stderr.Reset()
	ctx, cancel := context.WithCancel(context.Background())
	cancelWriter := &safetyReviewCLICancelWriter{ctx: ctx, cancel: cancel, writer: &stdout}
	code = cli.RunSafetyReview(ctx, []string{"status", "--task-dir", directory, "--watch"}, cancelWriter, &stderr)
	if code != 0 {
		t.Fatalf("RunSafetyReview(watch) = %d, want 0; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "task_id=status-test") {
		t.Fatalf("watch stdout = %q", stdout.String())
	}
}

// safetyReviewCLICancelWriter 在第一次输出后取消 watch。
type safetyReviewCLICancelWriter struct {
	ctx    context.Context
	cancel context.CancelFunc
	writer *strings.Builder
}

// Write 记录输出并取消上下文。
func (w *safetyReviewCLICancelWriter) Write(p []byte) (int, error) {
	w.cancel()
	return w.writer.Write(p)
}

// TestSafetyReviewCLIRunStatusWriteFailureIsFatal 验证 run-status 原子写失败不能被静默吞掉。
func TestSafetyReviewCLIRunStatusWriteFailureIsFatal(t *testing.T) {
	env := "SAFETY_REVIEW_TEST_API_KEY"
	t.Setenv(env, "test-key-value")
	server := newSafetyReviewCLIServer(t, false)
	path := writeSafetyReviewCLIConfig(t, server.URL, env, safetyReviewCLIInput("one"))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	raw = []byte(strings.Replace(string(raw), "run_status: run-status.json", "run_status: blocked/run-status.json", 1))
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("rewrite config: %v", err)
	}
	if err := os.MkdirAll(configTaskDir(path), 0o700); err != nil {
		t.Fatalf("create task dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configTaskDir(path), "blocked"), []byte("not-a-directory"), 0o600); err != nil {
		t.Fatalf("create blocking status parent: %v", err)
	}
	var stdout, stderr strings.Builder

	code := cli.RunSafetyReview(context.Background(), []string{"run", "--config", path}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("RunSafetyReview(status failure) = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "error_category=export") {
		t.Fatalf("stderr = %q, want export error", stderr.String())
	}
}

// safetyReviewCLIServer 提供本地 OpenAI 兼容假服务。
type safetyReviewCLIServer struct {
	*httptest.Server
	mu                    sync.Mutex
	log                   []safetyReviewCLIRequest
	modelOutputCanary     string
	blockClassification   chan struct{}
	classificationEntered chan struct{}
	preflightFailure      bool
}

// safetyReviewCLIRequest 记录安全摘要，不保存模型载荷。
type safetyReviewCLIRequest struct {
	role         string
	preflight    bool
	classRequest bool
}

// newSafetyReviewCLIServer 构造可编程的本地模型服务。
func newSafetyReviewCLIServer(t *testing.T, preflightFailure bool) *safetyReviewCLIServer {
	t.Helper()
	server := &safetyReviewCLIServer{
		classificationEntered: make(chan struct{}, 1),
		preflightFailure:      preflightFailure,
	}
	server.Server = httptest.NewServer(http.HandlerFunc(server.handle))
	t.Cleanup(server.Close)
	return server
}

// handle 按角色返回固定合法 JSON，并记录不含载荷的调用摘要。
func (s *safetyReviewCLIServer) handle(w http.ResponseWriter, r *http.Request) {
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
	preflight := len(request.Messages) == 2 && strings.Contains(request.Messages[1].Content, `"preflight"`)
	if len(request.Messages) != 0 {
		role = safetyReviewCLIRole(request.Messages[0].Content)
	}
	s.record(role, preflight)
	if preflight {
		if s.preflightFailure {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"message":"preflight unavailable"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{}"},"finish_reason":"stop"}]}`))
		return
	}
	if s.blockClassification != nil {
		s.notifyClassification()
		<-s.blockClassification
	}
	_, _ = w.Write([]byte(fmt.Sprintf(
		`{"choices":[{"message":{"content":%q},"finish_reason":"stop"}]}`,
		s.modelOutput(role),
	)))
}

// record 记录一次请求的安全摘要。
func (s *safetyReviewCLIServer) record(role string, preflight bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log = append(s.log, safetyReviewCLIRequest{role: role, preflight: preflight})
}

// notifyClassification 通知测试分类请求已进入。
func (s *safetyReviewCLIServer) notifyClassification() {
	select {
	case s.classificationEntered <- struct{}{}:
	default:
	}
}

// modelOutput 返回指定角色的固定合法输出。
func (s *safetyReviewCLIServer) modelOutput(role string) string {
	switch role {
	case "judge_a", "judge_b":
		rationale := "safe"
		if s.modelOutputCanary != "" {
			rationale = s.modelOutputCanary
		}
		return `{"verdict":"safe","evidence_complete":true,"evidence":[],` +
			`"attack_methods":[],"attack_domains":[],"exclusions":[],"rationale":"` + rationale + `"}`
	case "router":
		return `{"features":[],"attack_method_candidates":[],` +
			`"attack_domain_candidates":[],"coverage_complete":true}`
	default:
		return `{}`
	}
}

// safetyReviewCLIRole 从系统提示词识别角色。
func safetyReviewCLIRole(system string) string {
	switch {
	case strings.Contains(system, "Arbiter"):
		return "arbiter"
	case strings.Contains(system, "Expert"):
		return "expert"
	case strings.Contains(system, "Router"):
		return "router"
	case strings.Contains(system, "Judge A"):
		return "judge_a"
	case strings.Contains(system, "Judge B"):
		return "judge_b"
	default:
		return "unknown"
	}
}

// entries 返回请求摘要副本。
func (s *safetyReviewCLIServer) entries() []safetyReviewCLIRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]safetyReviewCLIRequest(nil), s.log...)
}

// requestCount 返回总请求数。
func (s *safetyReviewCLIServer) requestCount() int {
	return len(s.entries())
}

// classificationCount 返回分类请求数。
func (s *safetyReviewCLIServer) classificationCount() int {
	count := 0
	for _, entry := range s.entries() {
		if !entry.preflight {
			count++
		}
	}
	return count
}

// writeSafetyReviewCLIConfig 写入指向本地假服务和真实冻结 Policy 的配置。
func writeSafetyReviewCLIConfig(t *testing.T, baseURL, env, inputContents string) string {
	t.Helper()
	root := safetyReviewCLIRepoRoot(t)
	temp := t.TempDir()
	taskDir := filepath.Join(temp, "runs", "cli-test")
	input := filepath.Join(temp, "input.jsonl")
	if err := os.WriteFile(input, []byte(inputContents), 0o600); err != nil {
		t.Fatalf("write input: %v", err)
	}
	config := fmt.Sprintf(`version: 1
task:
  id: cli-test
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
`, input, taskDir, filepath.Join(root, "policy", "releases", "p04b-v1.0"),
		baseURL, env, baseURL, env, baseURL, env, baseURL, env)
	path := filepath.Join(temp, "safety-review.yaml")
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// safetyReviewCLIInput 返回一条固定 JSONL 输入。
func safetyReviewCLIInput(traceID string) string {
	return `{"trace_id":"` + traceID + `","prompt":"synthetic prompt","response":"synthetic response"}` + "\n"
}

// safetyReviewCLIInputPayload 返回含唯一脱敏测试标记的 JSONL 输入。
func safetyReviewCLIInputPayload() string {
	return `{"trace_id":"one","prompt":"CLI_PROMPT_CANARY","response":"CLI_RESPONSE_CANARY"}` + "\n"
}

// safetyReviewCLIRepoRoot 返回仓库根目录。
func safetyReviewCLIRepoRoot(t *testing.T) string {
	t.Helper()
	current, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			t.Fatal("repository root not found")
		}
		current = parent
	}
}

// configTaskDir 返回配置对应任务目录。
func configTaskDir(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "runs", "cli-test")
}

// inputPathForSafetyReviewConfig 返回配置输入路径。
func inputPathForSafetyReviewConfig(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "input.jsonl")
}

// readSafetyReviewCLISummary 从 SQLite 读取安全摘要。
func readSafetyReviewCLISummary(t *testing.T, configPath string) dao.SafetyReviewSummary {
	t.Helper()
	store, err := dao.OpenSafetyReview(
		context.Background(), filepath.Join(configTaskDir(configPath), "state.db"),
	)
	if err != nil {
		t.Fatalf("open safety review store: %v", err)
	}
	defer func() { _ = store.Close() }()
	summary, err := store.ReadSummary(context.Background(), "cli-test")
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	return summary
}

// readSafetyReviewCLIJSON 读取 JSON 文件为通用对象。
func readSafetyReviewCLIJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read JSON: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	return result
}
