package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sendllm/internal/api/cli"
)

// TestSafetyReviewRunDoesNotRequireHiddenGold 验证普通 run 不依赖 Hidden Gold。
func TestSafetyReviewRunDoesNotRequireHiddenGold(t *testing.T) {
	env := "SAFETY_REVIEW_RUN_CONTRACT_API_KEY"
	t.Setenv(env, "test-key-value")
	root := safetyReviewCLIRepoRoot(t)
	temp := t.TempDir()
	inputPath := writeSafetyReviewRunInput(t, temp, "normal-01", "normal-02", "normal-03")
	taskDir := filepath.Join(temp, "runs", "model-only")
	server := newSafetyReviewEvalServer(t, filepath.Join(root, "policy", "releases", "p04b-v1.0"))
	configPath := writeSafetyReviewEvalCLIConfig(t, server.URL, env, inputPath, taskDir)

	var stdout, stderr strings.Builder
	code := cli.RunSafetyReview(
		context.Background(), []string{"run", "--config", configPath}, &stdout, &stderr,
	)
	if code != 0 {
		t.Fatalf("RunSafetyReview(run without hidden) = %d, want 0; stderr=%s", code, stderr.String())
	}
	if server.classificationCount() == 0 {
		t.Fatal("fake provider did not receive classification requests")
	}
	assertSafetyReviewRunArtifacts(t, taskDir, 3, 0, "unvalidated")
}

// TestSafetyReviewRunQuarantinesUncertainItemsWithoutGold 验证无 Gold 时不确定样本进入隔离。
func TestSafetyReviewRunQuarantinesUncertainItemsWithoutGold(t *testing.T) {
	env := "SAFETY_REVIEW_RUN_CONTRACT_API_KEY"
	t.Setenv(env, "test-key-value")
	root := safetyReviewCLIRepoRoot(t)
	temp := t.TempDir()
	inputPath := writeSafetyReviewRunInput(t, temp, "normal-01", "boundary-01", "normal-02")
	taskDir := filepath.Join(temp, "runs", "model-only-quarantine")
	server := newSafetyReviewEvalServer(t, filepath.Join(root, "policy", "releases", "p04b-v1.0"))
	server.modelOutputCanary = "RUN_RAW_MODEL_OUTPUT_CANARY"
	configPath := writeSafetyReviewEvalCLIConfig(t, server.URL, env, inputPath, taskDir)

	var stdout, stderr strings.Builder
	code := cli.RunSafetyReview(
		context.Background(), []string{"run", "--config", configPath}, &stdout, &stderr,
	)
	if code != 0 {
		t.Fatalf("RunSafetyReview(quarantine without hidden) = %d, want 0; stderr=%s", code, stderr.String())
	}
	assertSafetyReviewRunArtifacts(t, taskDir, 2, 1, "unvalidated")
	quarantine := readSafetyReviewCLIJSONL(t, filepath.Join(taskDir, "quarantine.jsonl"))
	if len(quarantine) != 1 || quarantine[0]["trace_id"] != "boundary-01" {
		t.Fatalf("quarantine rows = %#v, want boundary-01", quarantine)
	}
	annotation, _ := quarantine[0]["annotation"].(map[string]any)
	if annotation["label"] == "safe" || annotation["method"] != "manual_required" {
		t.Fatalf("quarantine annotation = %#v, want unresolved manual_required", annotation)
	}
	combined := readSafetyReviewRunContractArtifact(t, taskDir, "audit.jsonl") +
		readSafetyReviewRunContractArtifact(t, taskDir, "report.json")
	if strings.Contains(combined, server.modelOutputCanary) {
		t.Fatal("audit or report leaked raw model output")
	}
}

// TestSafetyReviewRunResumeWithoutGold 验证无 Gold 运行可以中断后恢复且不重复成功分类。
func TestSafetyReviewRunResumeWithoutGold(t *testing.T) {
	env := "SAFETY_REVIEW_RUN_CONTRACT_API_KEY"
	t.Setenv(env, "test-key-value")
	root := safetyReviewCLIRepoRoot(t)
	temp := t.TempDir()
	inputPath := writeSafetyReviewRunInput(t, temp, "normal-01", "normal-02")
	taskDir := filepath.Join(temp, "runs", "model-only-resume")
	server := newSafetyReviewEvalServer(t, filepath.Join(root, "policy", "releases", "p04b-v1.0"))
	server.setBlockTraceID("normal-02")
	configPath := writeSafetyReviewEvalCLIConfig(t, server.URL, env, inputPath, taskDir)

	ctx, cancel := context.WithCancel(context.Background())
	exitCode := make(chan int, 1)
	var interruptedStdout, interruptedStderr strings.Builder
	go func() {
		exitCode <- cli.RunSafetyReview(ctx, []string{"run", "--config", configPath}, &interruptedStdout, &interruptedStderr)
	}()
	select {
	case <-server.classificationEntered:
		cancel()
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatalf("run classification did not start; stderr=%s", interruptedStderr.String())
	}
	select {
	case code := <-exitCode:
		if code != 130 {
			t.Fatalf("interrupted run exit = %d, want 130", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("interrupted run did not finish")
	}
	firstNormalCount := server.classificationCountForTrace("normal-01")
	firstBlockedCount := server.classificationCountForTrace("normal-02")
	server.setBlockTraceID("")

	var stdout, stderr strings.Builder
	code := cli.RunSafetyReview(
		context.Background(), []string{"run", "--config", configPath}, &stdout, &stderr,
	)
	if code != 0 {
		t.Fatalf("resumed run exit = %d, want 0; stderr=%s", code, stderr.String())
	}
	if got := server.classificationCountForTrace("normal-01"); got != firstNormalCount {
		t.Fatalf("completed trace classification count after resume = %d, want %d", got, firstNormalCount)
	}
	if got := server.classificationCountForTrace("normal-02"); got != firstBlockedCount+3 {
		t.Fatalf("blocked trace classification count after resume = %d, want %d", got, firstBlockedCount+3)
	}
	assertSafetyReviewRunArtifacts(t, taskDir, 2, 0, "unvalidated")
}

// TestSafetyReviewAcceptanceStateCannotBePassedByRun 验证普通 run 不能标记验收通过。
func TestSafetyReviewAcceptanceStateCannotBePassedByRun(t *testing.T) {
	env := "SAFETY_REVIEW_RUN_CONTRACT_API_KEY"
	t.Setenv(env, "test-key-value")
	root := safetyReviewCLIRepoRoot(t)
	temp := t.TempDir()
	inputPath := writeSafetyReviewRunInput(t, temp, "normal-01")
	taskDir := filepath.Join(temp, "runs", "model-only-state")
	server := newSafetyReviewEvalServer(t, filepath.Join(root, "policy", "releases", "p04b-v1.0"))
	configPath := writeSafetyReviewEvalCLIConfig(t, server.URL, env, inputPath, taskDir)

	var stdout, stderr strings.Builder
	code := cli.RunSafetyReview(
		context.Background(), []string{"run", "--config", configPath}, &stdout, &stderr,
	)
	if code != 0 {
		t.Fatalf("RunSafetyReview(state contract) = %d, want 0; stderr=%s", code, stderr.String())
	}
	report := readSafetyReviewCLIJSON(t, filepath.Join(taskDir, "report.json"))
	runStatus := readSafetyReviewCLIJSON(t, filepath.Join(taskDir, "run-status.json"))
	if report["acceptance_state"] != "unvalidated" || runStatus["acceptance_state"] != "unvalidated" {
		t.Fatalf(
			"acceptance states = report:%v run:%v, want unvalidated",
			report["acceptance_state"], runStatus["acceptance_state"],
		)
	}
	if report["acceptance_state"] == "passed" || runStatus["acceptance_state"] == "passed" {
		t.Fatal("ordinary run marked acceptance_state as passed")
	}
}

// writeSafetyReviewRunInput 写入普通未标注 JSONL 输入。
func writeSafetyReviewRunInput(t *testing.T, directory string, traceIDs ...string) string {
	t.Helper()
	var builder strings.Builder
	for _, traceID := range traceIDs {
		builder.WriteString(`{"trace_id":"` + traceID + `","prompt":"synthetic prompt",` +
			`"response":"synthetic response"}` + "\n")
	}
	path := filepath.Join(directory, "model-only-input.jsonl")
	if err := os.WriteFile(path, []byte(builder.String()), 0o600); err != nil {
		t.Fatalf("write model-only input: %v", err)
	}
	return path
}

// assertSafetyReviewRunArtifacts 验证普通 run 的导出计数和验收状态。
func assertSafetyReviewRunArtifacts(t *testing.T, taskDir string, clean, quarantine int, acceptanceState string) {
	t.Helper()
	cleanRows := readSafetyReviewCLIJSONL(t, filepath.Join(taskDir, "clean.jsonl"))
	quarantineRows := readSafetyReviewCLIJSONL(t, filepath.Join(taskDir, "quarantine.jsonl"))
	if len(cleanRows) != clean || len(quarantineRows) != quarantine {
		t.Fatalf("clean/quarantine counts = %d/%d, want %d/%d", len(cleanRows), len(quarantineRows), clean, quarantine)
	}
	report := readSafetyReviewCLIJSON(t, filepath.Join(taskDir, "report.json"))
	runStatus := readSafetyReviewCLIJSON(t, filepath.Join(taskDir, "run-status.json"))
	if report["acceptance_state"] != acceptanceState || runStatus["acceptance_state"] != acceptanceState {
		t.Fatalf(
			"acceptance states = report:%v run:%v, want %s",
			report["acceptance_state"], runStatus["acceptance_state"], acceptanceState,
		)
	}
}

// readSafetyReviewRunContractArtifact 读取导出文件内容用于安全断言。
func readSafetyReviewRunContractArtifact(t *testing.T, taskDir, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(taskDir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(raw)
}
