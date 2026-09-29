package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"sendllm/internal/dao"
	"sendllm/internal/lib/configs"
)

// RunPolicyOptimizer 执行 policy-optimizer 子命令。
func RunPolicyOptimizer(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if ctx.Err() != nil {
		writePolicyOptimizerError(stderr, "canceled")
		return 130
	}
	if len(args) != 0 && args[0] == "status" {
		return runPolicyOptimizerStatus(ctx, args[1:], stdout, stderr)
	}
	parsed, err := parsePolicyOptimizerArgs(args)
	if err != nil {
		writePolicyOptimizerError(stderr, "arguments")
		return 1
	}
	if ctx.Err() != nil {
		writePolicyOptimizerError(stderr, "canceled")
		return 130
	}
	if parsed.command == "validate" {
		if _, err := configs.LoadPolicyOptimizer(parsed.config); err != nil {
			writePolicyOptimizerError(stderr, "configuration")
			return 1
		}
		if _, err := fmt.Fprintln(stdout, "validation=PASS"); err != nil {
			writePolicyOptimizerError(stderr, "output")
			return 1
		}
		return 0
	}
	if parsed.command == "approve" {
		return runPolicyOptimizerApprove(ctx, parsed, stdout, stderr)
	}
	if parsed.command == "release" {
		return runPolicyOptimizerRelease(ctx, parsed, stdout, stderr)
	}
	if parsed.command == "compile" {
		return runPolicyOptimizerCompile(ctx, parsed, stdout, stderr)
	}
	if parsed.command == "regression" {
		return runPolicyOptimizerRegression(ctx, parsed, stdout, stderr)
	}
	if parsed.command == "inspect" {
		return runPolicyOptimizerInspect(ctx, parsed, stdout, stderr)
	}
	if parsed.command == "mapping-approve" {
		return runPolicyOptimizerMappingApprove(ctx, parsed, stdout, stderr)
	}
	if parsed.command == "gold-approve" {
		return runPolicyOptimizerGoldApprove(ctx, parsed, stdout, stderr)
	}
	if parsed.command == "gold-promote" {
		return runPolicyOptimizerGoldPromote(ctx, parsed, stdout, stderr)
	}
	if parsed.command == "analyze" {
		return runPolicyOptimizerAnalyze(ctx, parsed, stdout, stderr)
	}
	writePolicyOptimizerError(stderr, "unsupported")
	return 1
}

// runPolicyOptimizerStatus 执行只读 status 解析；状态 workflow 由后续 runner 接管。
func runPolicyOptimizerStatus(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("policy-optimizer status", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	iterationDir := flags.String("iteration-dir", "", "iteration directory")
	watch := flags.Bool("watch", false, "watch status")
	if err := flags.Parse(args); err != nil || strings.TrimSpace(*iterationDir) == "" || flags.NArg() != 0 {
		writePolicyOptimizerError(stderr, "arguments")
		return 1
	}
	if ctx.Err() != nil {
		writePolicyOptimizerError(stderr, "canceled")
		return 130
	}
	store, err := dao.OpenPolicyOptimizer(ctx, filepath.Join(*iterationDir, "state.db"))
	if err != nil {
		writePolicyOptimizerError(stderr, "storage")
		return 1
	}
	defer func() { _ = store.Close() }()
	writeStatus := func() error {
		status, err := store.ReadFirstStatus(ctx)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(stdout, "iteration=%s mode=%s status=%s stage=%s recovery=%d\n",
			status.IterationID, status.Mode, status.Status, status.Stage, status.Recovery)
		return err
	}
	if err := writeStatus(); err != nil {
		writePolicyOptimizerError(stderr, "status")
		return 1
	}
	if !*watch {
		return 0
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return 130
		case <-ticker.C:
			if err := writeStatus(); err != nil {
				writePolicyOptimizerError(stderr, "status")
				return 1
			}
		}
	}
}

// policyOptimizerArgs 表示严格解析后的命令参数。
type policyOptimizerArgs struct {
	command        string
	config         string
	packageID      string
	source         string
	mapping        string
	approver       string
	policy         string
	candidate      string
	candidateGold  string
	approvedGold   string
	corrections    string
	note           string
	changeRequests []string
	preview        bool
}

// policyOptimizerStringList 支持重复的 --change-request 参数。
type policyOptimizerStringList []string

// String 返回 flag 当前值。
func (values *policyOptimizerStringList) String() string {
	return strings.Join(*values, ",")
}

// Set 追加一个重复参数值。
func (values *policyOptimizerStringList) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("value is empty")
	}
	*values = append(*values, value)
	return nil
}

// parsePolicyOptimizerArgs 解析冻结合同中的命令和 flag 组合。
func parsePolicyOptimizerArgs(args []string) (policyOptimizerArgs, error) {
	if len(args) == 0 {
		return policyOptimizerArgs{}, errors.New("policy optimizer command is required")
	}
	parsed := policyOptimizerArgs{command: args[0]}
	flags := flag.NewFlagSet("policy-optimizer "+parsed.command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&parsed.config, "config", "", "configuration")
	flags.StringVar(&parsed.packageID, "package", "", "audit package")
	flags.StringVar(&parsed.source, "source", "", "source id")
	flags.StringVar(&parsed.mapping, "mapping", "", "mapping path")
	flags.StringVar(&parsed.approver, "approver", "", "approver id")
	flags.StringVar(&parsed.policy, "policy", "", "base policy version")
	flags.StringVar(&parsed.candidate, "candidate", "", "candidate version")
	flags.StringVar(&parsed.candidateGold, "candidate-gold", "", "candidate gold path")
	flags.StringVar(&parsed.approvedGold, "approved-gold", "", "approved gold path")
	flags.StringVar(&parsed.corrections, "corrections", "", "gold corrections path")
	flags.StringVar(&parsed.note, "note", "", "approval note")
	flags.Var((*policyOptimizerStringList)(&parsed.changeRequests), "change-request", "change request path")
	flags.BoolVar(&parsed.preview, "preview", false, "compile preview")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
		return policyOptimizerArgs{}, errors.New("policy optimizer flags are invalid")
	}
	if parsed.preview && parsed.command != "compile" {
		return policyOptimizerArgs{}, errors.New("preview is only valid for compile")
	}
	if err := validatePolicyOptimizerRequiredFlags(parsed); err != nil {
		return policyOptimizerArgs{}, err
	}
	return parsed, nil
}

// validatePolicyOptimizerRequiredFlags 校验每个命令的必需参数。
func validatePolicyOptimizerRequiredFlags(parsed policyOptimizerArgs) error {
	require := func(values ...string) error {
		for _, value := range values {
			if strings.TrimSpace(value) == "" {
				return errors.New("policy optimizer required flag is empty")
			}
		}
		return nil
	}
	switch parsed.command {
	case "validate":
		return require(parsed.config)
	case "inspect":
		return require(parsed.config, parsed.packageID)
	case "mapping-approve":
		return require(parsed.config, parsed.packageID, parsed.source, parsed.mapping, parsed.approver)
	case "analyze":
		return require(parsed.config, parsed.packageID)
	case "compile":
		if len(parsed.changeRequests) == 0 {
			return errors.New("policy optimizer compile requires a change request")
		}
		return require(parsed.config, parsed.policy)
	case "regression":
		return require(parsed.config, parsed.candidate)
	case "gold-approve":
		return require(parsed.config, parsed.candidateGold, parsed.approver)
	case "gold-promote":
		return require(parsed.config, parsed.approvedGold, parsed.approver)
	case "approve":
		return require(parsed.config, parsed.candidate, parsed.approver)
	case "release":
		return require(parsed.config, parsed.candidate)
	default:
		return fmt.Errorf("unsupported policy optimizer command %q", parsed.command)
	}
}

// writePolicyOptimizerError 输出不含 payload 的短错误。
func writePolicyOptimizerError(stderr io.Writer, category string) {
	_, _ = fmt.Fprintf(stderr, "policy-optimizer error: %s\n", category)
}
