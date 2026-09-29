package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"sendllm/internal/dto"
	"sendllm/internal/lib/configs"
	"sendllm/internal/service"
)

// runPolicyOptimizerApprove 创建人类审批工件并把候选标记为 release_ready。
func runPolicyOptimizerApprove(
	ctx context.Context,
	parsed policyOptimizerArgs,
	stdout, stderr io.Writer,
) int {
	if ctx.Err() != nil {
		writePolicyOptimizerError(stderr, "canceled")
		return 130
	}
	cfg, err := configs.LoadPolicyOptimizer(parsed.config)
	if err != nil {
		writePolicyOptimizerError(stderr, "configuration")
		return 1
	}
	candidate, err := loadPolicyOptimizerCandidate(cfg, parsed.candidate)
	if err != nil {
		writePolicyOptimizerError(stderr, "candidate")
		return 1
	}
	regression, err := loadPolicyOptimizerJSON[dto.PolicyOptimizerRegressionReport](
		filepath.Join(cfg.Iteration.Dir, "regression", "regression-report.json"),
	)
	if err != nil {
		writePolicyOptimizerError(stderr, "regression")
		return 1
	}
	critic, err := loadPolicyOptimizerJSON[dto.PolicyOptimizerCritique](filepath.Join(cfg.Iteration.Dir, "critic", "critic-report.json"))
	if err != nil {
		writePolicyOptimizerError(stderr, "critic")
		return 1
	}
	approval, err := service.CreatePolicyApproval(candidate, regression, critic, parsed.approver, time.Now().UTC())
	if err != nil {
		writePolicyOptimizerError(stderr, "approval")
		return 1
	}
	approval.Note = parsed.note
	path := filepath.Join(cfg.Iteration.Dir, "approvals", "policy_release-"+parsed.candidate+".json")
	if err := writePolicyOptimizerJSON(path, approval); err != nil {
		writePolicyOptimizerError(stderr, "approval")
		return 1
	}
	if _, err := fmt.Fprintln(stdout, "approval=PASS"); err != nil {
		writePolicyOptimizerError(stderr, "output")
		return 1
	}
	return 0
}

// runPolicyOptimizerRelease 使用已有审批和报告原子发布候选。
func runPolicyOptimizerRelease(
	ctx context.Context,
	parsed policyOptimizerArgs,
	stdout, stderr io.Writer,
) int {
	if ctx.Err() != nil {
		writePolicyOptimizerError(stderr, "canceled")
		return 130
	}
	cfg, err := configs.LoadPolicyOptimizer(parsed.config)
	if err != nil {
		writePolicyOptimizerError(stderr, "configuration")
		return 1
	}
	candidate, err := loadPolicyOptimizerCandidate(cfg, parsed.candidate)
	if err != nil {
		writePolicyOptimizerError(stderr, "candidate")
		return 1
	}
	regression, err := loadPolicyOptimizerJSON[dto.PolicyOptimizerRegressionReport](
		filepath.Join(cfg.Iteration.Dir, "regression", "regression-report.json"),
	)
	if err != nil {
		writePolicyOptimizerError(stderr, "regression")
		return 1
	}
	critic, err := loadPolicyOptimizerJSON[dto.PolicyOptimizerCritique](filepath.Join(cfg.Iteration.Dir, "critic", "critic-report.json"))
	if err != nil {
		writePolicyOptimizerError(stderr, "critic")
		return 1
	}
	approval, err := loadPolicyOptimizerJSON[dto.PolicyOptimizerApproval](
		filepath.Join(cfg.Iteration.Dir, "approvals", "policy_release-"+parsed.candidate+".json"),
	)
	if err != nil {
		writePolicyOptimizerError(stderr, "approval")
		return 1
	}
	gate, err := loadPolicyOptimizerGate(cfg.Regression.GatePolicy)
	if err != nil {
		writePolicyOptimizerError(stderr, "gate")
		return 1
	}
	release, err := service.ReleasePolicy(ctx, service.PolicyOptimizerReleaseConfig{
		Candidate: candidate, CandidateDir: filepath.Join(cfg.Iteration.Dir, "candidate_policy"),
		ReleasesDir: cfg.Policy.ReleasesDir, TargetVersion: cfg.Policy.TargetVersion,
		Approval: approval, Regression: regression, Critic: critic, Gate: gate,
	})
	if err != nil {
		writePolicyOptimizerError(stderr, "release")
		return 1
	}
	if _, err := fmt.Fprintf(stdout, "release=PASS version=%s sha256=%s\n", release.Version, release.SHA256); err != nil {
		writePolicyOptimizerError(stderr, "output")
		return 1
	}
	return 0
}

// runPolicyOptimizerCompile 使用 Resolver Change Set 生成 candidate policy 和 prompts。
func runPolicyOptimizerCompile(
	ctx context.Context,
	parsed policyOptimizerArgs,
	stdout, stderr io.Writer,
) int {
	if ctx.Err() != nil {
		writePolicyOptimizerError(stderr, "canceled")
		return 130
	}
	cfg, err := configs.LoadPolicyOptimizer(parsed.config)
	if err != nil {
		writePolicyOptimizerError(stderr, "configuration")
		return 1
	}
	if parsed.policy != cfg.Policy.BaseVersion {
		writePolicyOptimizerError(stderr, "configuration")
		return 1
	}
	set, err := loadPolicyOptimizerJSON[dto.PolicyOptimizerChangeSet](filepath.Join(
		cfg.Iteration.Dir, "resolved_changes", "change-set.json",
	))
	if err != nil {
		writePolicyOptimizerError(stderr, "change_set")
		return 1
	}
	baseDir := filepath.Join(cfg.Policy.ReleasesDir, cfg.Policy.BaseVersion)
	candidateDir := filepath.Join(cfg.Iteration.Dir, "candidate_policy")
	if _, err := service.ApplyPolicyChangeSet(baseDir, candidateDir, set); err != nil {
		writePolicyOptimizerError(stderr, "compiler")
		return 1
	}
	if _, err := service.CompilePolicyPrompts(candidateDir, service.PolicyOptimizerCompileConfig{
		CompilerVersion: "1",
	}); err != nil {
		writePolicyOptimizerError(stderr, "compiler")
		return 1
	}
	status := "awaiting_regression"
	if parsed.preview {
		status = "preview_ready"
	}
	if _, err := fmt.Fprintf(stdout, "compile=PASS status=%s\n", status); err != nil {
		writePolicyOptimizerError(stderr, "output")
		return 1
	}
	return 0
}

// runPolicyOptimizerRegression 读取两轮独立 artifacts 并执行 P04-B gates。
func runPolicyOptimizerRegression(
	ctx context.Context,
	parsed policyOptimizerArgs,
	stdout, stderr io.Writer,
) int {
	if ctx.Err() != nil {
		writePolicyOptimizerError(stderr, "canceled")
		return 130
	}
	cfg, err := configs.LoadPolicyOptimizer(parsed.config)
	if err != nil {
		writePolicyOptimizerError(stderr, "configuration")
		return 1
	}
	candidate, err := loadPolicyOptimizerCandidate(cfg, parsed.candidate)
	if err != nil {
		writePolicyOptimizerError(stderr, "candidate")
		return 1
	}
	baseManifest, err := service.VerifyPolicyBundle(filepath.Join(cfg.Policy.ReleasesDir, cfg.Policy.BaseVersion))
	if err != nil {
		writePolicyOptimizerError(stderr, "base")
		return 1
	}
	executor := policyOptimizerFreshRotationExecutor{
		Config: cfg, BaseDir: filepath.Join(cfg.Policy.ReleasesDir, cfg.Policy.BaseVersion),
		CandidateDir: filepath.Join(cfg.Iteration.Dir, "candidate_policy"), Stderr: stderr,
	}
	report, err := service.RunPolicyRegression(ctx, service.PolicyOptimizerRegressionConfig{
		BaseReleaseSHA256: baseManifest.AggregateHash, CandidateReleaseSHA256: candidate.SHA256,
		SuiteVersion: "suite-v1", GatePolicyVersion: "p04b-gate-v1", RotationExecutor: executor,
	})
	if err != nil {
		writePolicyOptimizerError(stderr, "regression")
		return 1
	}
	report.Contracts = evaluatePolicyOptimizerHiddenSafeContract(report)
	gate, err := loadPolicyOptimizerGate(cfg.Regression.GatePolicy)
	if err != nil {
		writePolicyOptimizerError(stderr, "gate")
		return 1
	}
	if err := service.EvaluateP04BGates(report, gate); err != nil {
		_ = writePolicyOptimizerJSON(filepath.Join(cfg.Iteration.Dir, "regression", "regression-report.json"), report)
		writePolicyOptimizerError(stderr, "regression_failed")
		return 1
	}
	if err := writePolicyOptimizerJSON(filepath.Join(cfg.Iteration.Dir, "regression", "regression-report.json"), report); err != nil {
		writePolicyOptimizerError(stderr, "regression")
		return 1
	}
	if err := writePolicyOptimizerJSON(filepath.Join(cfg.Iteration.Dir, "regression", "gate-report.json"), gate); err != nil {
		writePolicyOptimizerError(stderr, "regression")
		return 1
	}
	if _, err := fmt.Fprintf(stdout, "regression=PASS rotations=%d\n", len(report.Rotations)); err != nil {
		writePolicyOptimizerError(stderr, "output")
		return 1
	}
	return 3
}

// evaluatePolicyOptimizerHiddenSafeContract 重算 hidden-safe 合同结果。
func evaluatePolicyOptimizerHiddenSafeContract(
	report dto.PolicyOptimizerRegressionReport,
) []dto.PolicyOptimizerContractResult {
	safeCases := 0
	falseUnsafe := 0
	for _, rotation := range report.Rotations {
		for _, item := range rotation.Cases {
			if item.GoldLabel != "safe" || (item.Suite != "hidden" && item.Suite != "hard_negative") {
				continue
			}
			safeCases++
			if !item.Quarantined && item.PredictedLabel == "unsafe" {
				falseUnsafe++
			}
		}
	}
	return []dto.PolicyOptimizerContractResult{{
		ContractID: "CONTRACT-P04B-HIDDEN-SAFE", Passed: safeCases >= 1 && falseUnsafe == 0,
		Observed: safeCases,
	}}
}

// loadPolicyOptimizerCandidate 验证候选 bundle 并返回稳定身份。
func loadPolicyOptimizerCandidate(cfg *configs.PolicyOptimizerConfig, version string) (dto.PolicyOptimizerCandidate, error) {
	dir := filepath.Join(cfg.Iteration.Dir, "candidate_policy")
	manifest, err := service.VerifyPolicyBundle(dir)
	if err != nil {
		return dto.PolicyOptimizerCandidate{}, err
	}
	if manifest.ReleaseVersion != version {
		return dto.PolicyOptimizerCandidate{}, fmt.Errorf("candidate version mismatch")
	}
	return dto.PolicyOptimizerCandidate{Version: version, Path: dir, SHA256: manifest.AggregateHash}, nil
}

// runPolicyOptimizerInspect 执行 Audit Package 结构检查。
func runPolicyOptimizerInspect(
	ctx context.Context,
	parsed policyOptimizerArgs,
	stdout, stderr io.Writer,
) int {
	if _, err := configs.LoadPolicyOptimizer(parsed.config); err != nil {
		writePolicyOptimizerError(stderr, "configuration")
		return 1
	}
	pkg, err := service.LoadAuditPackage(parsed.packageID)
	if err != nil {
		writePolicyOptimizerError(stderr, "package")
		return 1
	}
	inspection, err := service.InspectAuditPackage(ctx, service.AuditInspectConfig{Package: pkg})
	if err != nil {
		writePolicyOptimizerError(stderr, "inspect")
		return 1
	}
	if inspection.NeedsApproval {
		_, _ = fmt.Fprintln(stdout, "inspect=awaiting_mapping_approval")
		return 3
	}
	_, _ = fmt.Fprintln(stdout, "inspect=PASS")
	return 0
}

// runPolicyOptimizerMappingApprove 写入 source-hash-bound approved mapping。
func runPolicyOptimizerMappingApprove(
	ctx context.Context,
	parsed policyOptimizerArgs,
	stdout, stderr io.Writer,
) int {
	if ctx.Err() != nil {
		writePolicyOptimizerError(stderr, "canceled")
		return 130
	}
	if _, err := configs.LoadPolicyOptimizer(parsed.config); err != nil {
		writePolicyOptimizerError(stderr, "configuration")
		return 1
	}
	pkg, err := service.LoadAuditPackage(parsed.packageID)
	if err != nil {
		writePolicyOptimizerError(stderr, "package")
		return 1
	}
	var sourcePath string
	for _, source := range pkg.Manifest.Sources {
		if source.SourceID == parsed.source {
			sourcePath = source.Mapping
			break
		}
	}
	if sourcePath == "" {
		writePolicyOptimizerError(stderr, "source")
		return 1
	}
	outputPath := filepath.Join(pkg.Root, filepath.FromSlash(sourcePath))
	if err := service.ApproveAuditMapping(parsed.mapping, outputPath, parsed.approver, time.Now().UTC()); err != nil {
		writePolicyOptimizerError(stderr, "mapping")
		return 1
	}
	_, _ = fmt.Fprintln(stdout, "mapping_approve=PASS")
	return 0
}

// runPolicyOptimizerGoldApprove 将 Candidate Gold JSONL 按 Corrections 批准为 Approved Gold。
func runPolicyOptimizerGoldApprove(
	ctx context.Context,
	parsed policyOptimizerArgs,
	stdout, stderr io.Writer,
) int {
	if ctx.Err() != nil {
		writePolicyOptimizerError(stderr, "canceled")
		return 130
	}
	cfg, err := configs.LoadPolicyOptimizer(parsed.config)
	if err != nil {
		writePolicyOptimizerError(stderr, "configuration")
		return 1
	}
	candidates, err := loadPolicyOptimizerJSONL[dto.PolicyOptimizerGoldRecord](parsed.candidateGold)
	if err != nil {
		writePolicyOptimizerError(stderr, "gold")
		return 1
	}
	corrections := []dto.PolicyOptimizerGoldCorrection{}
	if parsed.corrections != "" {
		corrections, err = loadPolicyOptimizerJSONL[dto.PolicyOptimizerGoldCorrection](parsed.corrections)
		if err != nil {
			writePolicyOptimizerError(stderr, "corrections")
			return 1
		}
	}
	correctionByID := map[string]dto.PolicyOptimizerGoldCorrection{}
	for _, correction := range corrections {
		if _, exists := correctionByID[correction.GoldID]; exists {
			writePolicyOptimizerError(stderr, "corrections")
			return 1
		}
		correctionByID[correction.GoldID] = correction
	}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if seen[candidate.GoldID] {
			writePolicyOptimizerError(stderr, "gold")
			return 1
		}
		seen[candidate.GoldID] = true
		if correction, ok := correctionByID[candidate.GoldID]; ok {
			candidate.Label = correction.Label
			candidate.RiskTypes = append([]string(nil), correction.RiskTypes...)
			candidate.CaseType = correction.CaseType
			candidate.RuleIDs = append([]string(nil), correction.RuleIDs...)
			candidate.DecisionIDs = append([]string(nil), correction.DecisionIDs...)
		}
		approval, err := policyOptimizerGoldCLIApproval(candidate.GoldID, "gold", candidate, parsed.approver)
		if err != nil {
			writePolicyOptimizerError(stderr, "approval")
			return 1
		}
		approved, err := service.ApproveGold(candidate, approval)
		if err != nil {
			writePolicyOptimizerError(stderr, "gold")
			return 1
		}
		path := filepath.Join(cfg.Iteration.Dir, "gold", "approved", approved.GoldID+".json")
		if err := writePolicyOptimizerJSON(path, approved); err != nil {
			writePolicyOptimizerError(stderr, "gold")
			return 1
		}
	}
	_, _ = fmt.Fprintln(stdout, "gold_approve=PASS")
	return 0
}

// runPolicyOptimizerGoldPromote 将 Approved Gold 提升为 Core Gold。
func runPolicyOptimizerGoldPromote(
	ctx context.Context,
	parsed policyOptimizerArgs,
	stdout, stderr io.Writer,
) int {
	if ctx.Err() != nil {
		writePolicyOptimizerError(stderr, "canceled")
		return 130
	}
	cfg, err := configs.LoadPolicyOptimizer(parsed.config)
	if err != nil {
		writePolicyOptimizerError(stderr, "configuration")
		return 1
	}
	approvedRecords, err := loadPolicyOptimizerJSONL[dto.PolicyOptimizerGoldRecord](parsed.approvedGold)
	if err != nil {
		writePolicyOptimizerError(stderr, "gold")
		return 1
	}
	for _, approved := range approvedRecords {
		history := dto.PolicyOptimizerGoldHistory{
			SuccessfulReleaseIDs:   approved.SuccessfulReleaseIDs,
			UnresolvedChallengeIDs: approved.ChallengeIDs,
		}
		approval, err := policyOptimizerGoldCLIApproval(approved.GoldID, "core_gold", approved, parsed.approver)
		if err != nil {
			writePolicyOptimizerError(stderr, "approval")
			return 1
		}
		core, err := service.PromoteCoreGold(approved, history, approval)
		if err != nil {
			writePolicyOptimizerError(stderr, "gold")
			return 1
		}
		path := filepath.Join(cfg.Iteration.Dir, "gold", "core", core.GoldID+".json")
		if err := writePolicyOptimizerJSON(path, core); err != nil {
			writePolicyOptimizerError(stderr, "gold")
			return 1
		}
	}
	_, _ = fmt.Fprintln(stdout, "gold_promote=PASS")
	return 0
}

// runPolicyOptimizerAnalyze 执行自动闭环：规范化、分层，并消费离线 prompt change artifact。
func runPolicyOptimizerAnalyze(
	ctx context.Context,
	parsed policyOptimizerArgs,
	stdout, stderr io.Writer,
) int {
	if ctx.Err() != nil {
		writePolicyOptimizerError(stderr, "canceled")
		return 130
	}
	cfg, err := configs.LoadPolicyOptimizer(parsed.config)
	if err != nil {
		writePolicyOptimizerError(stderr, "configuration")
		return 1
	}
	pkg, err := service.LoadAuditPackage(parsed.packageID)
	if err != nil {
		writePolicyOptimizerError(stderr, "package")
		return 1
	}
	inspection, err := service.InspectAuditPackage(ctx, service.AuditInspectConfig{Package: pkg})
	if err != nil {
		writePolicyOptimizerError(stderr, "inspect")
		return 1
	}
	if inspection.NeedsApproval {
		_, _ = fmt.Fprintln(stdout, "analyze=awaiting_mapping_approval")
		return 3
	}
	records := make([]dto.PolicyOptimizerAuditRecord, 0)
	for _, source := range pkg.Manifest.Sources {
		mapping, err := loadPolicyOptimizerMapping(filepath.Join(pkg.Root, filepath.FromSlash(source.Mapping)))
		if err != nil {
			writePolicyOptimizerError(stderr, "mapping")
			return 1
		}
		outputPath := filepath.Join(cfg.Iteration.Dir, "normalized", source.SourceID+".jsonl")
		if _, err := service.NormalizeAuditSource(ctx, service.PolicyOptimizerNormalizeConfig{
			Package: pkg, Source: source, Mapping: mapping, OutputPath: outputPath,
		}); err != nil {
			writePolicyOptimizerError(stderr, "normalize")
			return 1
		}
		loaded, err := loadPolicyOptimizerJSONL[dto.PolicyOptimizerAuditRecord](outputPath)
		if err != nil {
			writePolicyOptimizerError(stderr, "normalize")
			return 1
		}
		records = append(records, loaded...)
	}
	refs := make([]dto.PolicyOptimizerAuditRecordRef, 0, len(records))
	for _, record := range records {
		refs = append(refs, dto.PolicyOptimizerAuditRecordRef{
			RecordID: record.RecordID, SourceID: record.SourceID, Scene: string(record.Scene),
			ComparisonType: string(record.Comparison.Type),
		})
	}
	batches, err := service.StratifyAuditRecords(refs, service.PolicyOptimizerBatchingConfig{
		HomogeneousPercent: cfg.Batching.HomogeneousPercent,
		ConflictPercent:    cfg.Batching.ConflictPercent, RandomPercent: cfg.Batching.RandomPercent,
		TargetSize: cfg.Batching.TargetSize, MinSize: cfg.Batching.MinSize, MaxSize: cfg.Batching.MaxSize,
	}, cfg.Iteration.ID)
	if err != nil {
		writePolicyOptimizerError(stderr, "stratify")
		return 1
	}
	for _, batch := range batches {
		if err := writePolicyOptimizerJSON(filepath.Join(cfg.Iteration.Dir, "batches", batch.ID+".json"), batch); err != nil {
			writePolicyOptimizerError(stderr, "stratify")
			return 1
		}
	}
	changeSetPath := filepath.Join(cfg.Iteration.Dir, "resolved_changes", "change-set.json")
	if _, err := os.Stat(changeSetPath); errors.Is(err, os.ErrNotExist) {
		_, _ = fmt.Fprintln(stdout, "analyze=no_change")
		return 0
	}
	compileArgs := parsed
	compileArgs.command = "compile"
	compileArgs.policy = cfg.Policy.BaseVersion
	if code := runPolicyOptimizerCompile(ctx, compileArgs, stdout, stderr); code != 0 {
		return code
	}
	regressionArgs := parsed
	regressionArgs.command = "regression"
	regressionArgs.candidate = cfg.Policy.TargetVersion + "-candidate.1"
	if code := runPolicyOptimizerRegression(ctx, regressionArgs, stdout, stderr); code != 0 {
		return code
	}
	return 3
}

// loadPolicyOptimizerMapping 严格读取 approved mapping。
func loadPolicyOptimizerMapping(path string) (dto.PolicyOptimizerMapping, error) {
	var mapping dto.PolicyOptimizerMapping
	raw, err := os.ReadFile(path)
	if err != nil {
		return mapping, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&mapping); err != nil {
		return mapping, err
	}
	return mapping, nil
}

// loadPolicyOptimizerJSON 严格读取一个 JSON 工件。
func loadPolicyOptimizerJSON[T any](path string) (T, error) {
	var value T
	raw, err := os.ReadFile(path)
	if err != nil {
		return value, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, err
	}
	return value, nil
}

// loadPolicyOptimizerJSONL 严格读取一个 JSONL 工件列表。
func loadPolicyOptimizerJSONL[T any](path string) ([]T, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	values := make([]T, 0)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}
		var value T
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

// policyOptimizerGoldCLIApproval 构造绑定 Gold 主体哈希的人类审批。
func policyOptimizerGoldCLIApproval(
	subjectID string,
	subjectType string,
	subject any,
	approver string,
) (dto.PolicyOptimizerGoldApproval, error) {
	hash, err := service.PolicyOptimizerGoldSubjectHash(subject)
	if err != nil {
		return dto.PolicyOptimizerGoldApproval{}, err
	}
	approvalID := "AP:" + subjectType + ":" + subjectID
	return dto.PolicyOptimizerGoldApproval{
		Version: 1, ApprovalID: approvalID, SubjectType: subjectType, SubjectID: subjectID,
		SubjectSHA256: hash, RelatedSHA256: map[string]string{}, ApproverID: approver,
		Decision: "approved", CreatedAt: time.Now().UTC(),
	}, nil
}

// loadPolicyOptimizerGate 严格读取 gate policy。
func loadPolicyOptimizerGate(path string) (dto.PolicyOptimizerGatePolicy, error) {
	var gate dto.PolicyOptimizerGatePolicy
	raw, err := os.ReadFile(path)
	if err != nil {
		return gate, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&gate); err != nil {
		return gate, err
	}
	return gate, nil
}

// writePolicyOptimizerJSON 原子写入审批 JSON。
func writePolicyOptimizerJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".approval-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if _, err := temp.Write(raw); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
