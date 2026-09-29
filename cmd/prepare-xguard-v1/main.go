// Command prepare-xguard-v1 将 XGuard v1 数据映射为标准标签复核入口。
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxJSONLLineSize = 64 * 1024 * 1024

type xguardRecord struct {
	SampleID           string          `json:"sample_id"`
	Split              string          `json:"split"`
	Messages           []xguardMessage `json:"messages"`
	IsRisk             bool            `json:"is_risk"`
	Labels             []xguardLabel   `json:"labels"`
	BoundarySampleType string          `json:"boundary_sample_type"`
	Severity           string          `json:"severity"`
	SourceLanguage     string          `json:"source_language"`
}

type xguardMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type xguardLabel struct {
	CategoryName string `json:"category_name"`
}

type labelReviewRow struct {
	TraceID      string                  `json:"trace_id"`
	Source       string                  `json:"source"`
	Split        string                  `json:"split"`
	Language     string                  `json:"language"`
	Scene        string                  `json:"scene"`
	Label        string                  `json:"label"`
	Prompt       string                  `json:"prompt"`
	Response     string                  `json:"response"`
	Explanation  string                  `json:"explanation"`
	ExtendedInfo labelReviewExtendedInfo `json:"extended_info"`
	Annotation   labelReviewAnnotation   `json:"annotation"`
}

type labelReviewExtendedInfo struct {
	AttackMethod   string `json:"attack_method"`
	AttackDomain   string `json:"attack_domain"`
	RiskLevel      string `json:"risk_level"`
	CaseType       string `json:"case_type"`
	IsAttack       bool   `json:"is_attack"`
	AttackScenario string `json:"attack_scenario"`
	Other          string `json:"other"`
}

type labelReviewAnnotation struct {
	Method       string   `json:"method"`
	QualityScore *float64 `json:"quality_score"`
}

type outputSet struct {
	prompt   *jsonlOutput
	response *jsonlOutput
	combined *jsonlOutput
}

type jsonlOutput struct {
	path string
	temp string
	file *os.File
	buf  *bufio.Writer
}

type convertCounts struct {
	prompt   int
	response int
	combined int
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "prepare-xguard-v1: %v\n", err)
		os.Exit(1)
	}
}

// run 解析命令参数并执行 XGuard 到标准标签复核入口的转换。
func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("prepare-xguard-v1", flag.ContinueOnError)
	flags.SetOutput(stderr)
	inputPath := flags.String("input", "", "XGuard JSONL 输入文件")
	promptPath := flags.String("prompt-output", "", "prompt 场景输出 JSONL")
	responsePath := flags.String("response-output", "", "response 场景输出 JSONL")
	combinedPath := flags.String("combined-output", "", "可选的合并输出 JSONL")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	if *inputPath == "" || *promptPath == "" || *responsePath == "" {
		return fmt.Errorf("input, prompt-output and response-output are required")
	}

	counts, err := convertFile(*inputPath, *promptPath, *responsePath, *combinedPath)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(
		stdout,
		"prompt=%d response=%d combined=%d\n",
		counts.prompt,
		counts.response,
		counts.combined,
	)
	return err
}

// convertFile 流式转换输入文件，并在全部成功后发布输出文件。
func convertFile(inputPath, promptPath, responsePath, combinedPath string) (convertCounts, error) {
	input, err := os.Open(inputPath)
	if err != nil {
		return convertCounts{}, fmt.Errorf("open input %q: %w", inputPath, err)
	}
	defer func() { _ = input.Close() }()

	outputs, err := openOutputs(promptPath, responsePath, combinedPath)
	if err != nil {
		return convertCounts{}, err
	}
	defer outputs.cleanup()

	counts, err := convertReader(input, outputs)
	if err != nil {
		return convertCounts{}, err
	}
	if err := outputs.publish(); err != nil {
		return convertCounts{}, err
	}
	return counts, nil
}

// convertReader 逐行解析 XGuard JSONL 并写入一个或两个标准标签复核行。
func convertReader(reader io.Reader, outputs outputSet) (convertCounts, error) {
	var counts convertCounts
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), maxJSONLLineSize+1)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		rows, err := convertLine([]byte(line))
		if err != nil {
			return convertCounts{}, fmt.Errorf("convert line %d: %w", lineNumber, err)
		}
		for _, row := range rows {
			if err := outputs.write(row); err != nil {
				return convertCounts{}, fmt.Errorf("write line %d: %w", lineNumber, err)
			}
			if row.Scene == "prompt" {
				counts.prompt++
			}
			if row.Scene == "response" {
				counts.response++
			}
			if outputs.combined != nil {
				counts.combined++
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return convertCounts{}, fmt.Errorf("read input: %w", err)
	}
	return counts, nil
}

// convertLine 将一条 XGuard 记录按消息角色拆为标准标签复核行。
func convertLine(raw []byte) ([]labelReviewRow, error) {
	var record xguardRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return nil, fmt.Errorf("decode JSON: %w", err)
	}
	if record.SampleID == "" || len(record.Messages) == 0 {
		return nil, fmt.Errorf("required XGuard fields are missing")
	}
	prompt, response, err := firstUserAssistant(record.Messages)
	if err != nil {
		return nil, err
	}
	category := xguardCategory(record)
	domain, err := xguardAttackDomain(category, record.IsRisk)
	if err != nil {
		return nil, err
	}
	riskLevel, err := xguardRiskLevel(record)
	if err != nil {
		return nil, err
	}
	base := labelReviewBase(record, category, domain, riskLevel)
	rows := make([]labelReviewRow, 0, 2)
	if prompt != "" {
		row := base
		row.TraceID = record.SampleID + "__prompt"
		row.Scene = "prompt"
		row.Prompt = prompt
		row.Response = ""
		rows = append(rows, row)
	}
	if response != "" {
		row := base
		row.TraceID = record.SampleID + "__response"
		row.Scene = "response"
		row.Prompt = prompt
		row.Response = response
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("record has no user or assistant content")
	}
	return rows, nil
}

// firstUserAssistant 抽取首个 user 和 assistant 内容。
func firstUserAssistant(messages []xguardMessage) (string, string, error) {
	var prompt string
	var response string
	for _, message := range messages {
		if message.Content == "" {
			return "", "", fmt.Errorf("message content is empty")
		}
		switch message.Role {
		case "user":
			if prompt == "" {
				prompt = message.Content
			}
		case "assistant":
			if response == "" {
				response = message.Content
			}
		}
	}
	return prompt, response, nil
}

// labelReviewBase 构造 prompt 与 response 行共享的标准标签字段。
func labelReviewBase(record xguardRecord, category string, domain string, riskLevel string) labelReviewRow {
	label := "safe"
	if record.IsRisk {
		label = "unsafe"
	}
	return labelReviewRow{
		Source:      "v1",
		Split:       valueOrDefault(record.Split, "train"),
		Language:    valueOrDefault(record.SourceLanguage, "zh"),
		Label:       label,
		Explanation: xguardExplanation(category, record.IsRisk),
		ExtendedInfo: labelReviewExtendedInfo{
			AttackMethod: "",
			AttackDomain: domain,
			RiskLevel:    riskLevel,
			CaseType:     xguardCaseType(record.BoundarySampleType),
			IsAttack:     record.IsRisk,
		},
		Annotation: labelReviewAnnotation{
			Method:       "source_mapping",
			QualityScore: nil,
		},
	}
}

// xguardCategory 返回首个非空 XGuard 类别。
func xguardCategory(record xguardRecord) string {
	for _, label := range record.Labels {
		if strings.TrimSpace(label.CategoryName) != "" {
			return strings.TrimSpace(label.CategoryName)
		}
	}
	return "安全"
}

// xguardAttackDomain 将 XGuard 类别映射到 38 类中的内容领域风险。
func xguardAttackDomain(category string, isRisk bool) (string, error) {
	if !isRisk {
		return "", nil
	}
	if category == "安全" {
		return "", fmt.Errorf("risk record cannot use safe category")
	}
	domains := map[string]string{
		"财产侵权":       "other_legal_rights_infringement",
		"个人隐私":       "privacy_right_infringement",
		"社会伦理":       "other_illegal_or_prohibited_content",
		"色情违禁品":      "violence_or_explicit_content",
		"辱骂诅咒":       "harm_to_physical_or_mental_health",
		"毒品犯罪":       "other_illegal_or_prohibited_content",
		"身体健康":       "harm_to_physical_or_mental_health",
		"黑客攻击":       "other_illegal_or_prohibited_content",
		"经济犯罪":       "other_business_illegal_activity",
		"危险武器":       "violence_or_explicit_content",
		"心理健康":       "harm_to_physical_or_mental_health",
		"社会破坏":       "other_illegal_or_prohibited_content",
		"科学伦理":       "other_illegal_or_prohibited_content",
		"医学":         "harm_to_physical_or_mental_health",
		"法律":         "other_illegal_or_prohibited_content",
		"商业秘密":       "trade_secret_leakage",
		"金融":         "other_business_illegal_activity",
		"极端主义意识形态趋势": "terrorism_or_extremism",
		"动态策略临时类别 a": "other_illegal_or_prohibited_content",
		"动态策略临时类别 b": "other_illegal_or_prohibited_content",
		"动态策略临时类别 c": "other_illegal_or_prohibited_content",
		"动态策略临时类别 h": "other_illegal_or_prohibited_content",
		"诽谤":         "reputation_right_infringement",
		"恶意代码":       "other_illegal_or_prohibited_content",
		"暴力恐怖活动":     "terrorism_or_extremism",
		"未成年人虐待与剥削":  "violence_or_explicit_content",
		"访问控制":       "other_illegal_or_prohibited_content",
		"腐蚀未成年人":     "violence_or_explicit_content",
		"威胁和恐吓":      "harm_to_physical_or_mental_health",
		"网络欺凌":       "harm_to_physical_or_mental_health",
		"物理安全":       "harm_to_physical_or_mental_health",
		"未成年人违法行为":   "other_illegal_or_prohibited_content",
	}
	domain, ok := domains[category]
	if !ok {
		return "", fmt.Errorf("unknown XGuard category %q", category)
	}
	return domain, nil
}

// xguardRiskLevel 校验并返回最终风险等级。
func xguardRiskLevel(record xguardRecord) (string, error) {
	if !record.IsRisk {
		return "", nil
	}
	switch record.Severity {
	case "low", "medium", "high":
		return record.Severity, nil
	default:
		return "", fmt.Errorf("risk record has invalid severity %q", record.Severity)
	}
}

// xguardCaseType 将 XGuard 样例边界类型映射为最终样例类型。
func xguardCaseType(value string) string {
	switch value {
	case "边界正例", "边界负例":
		return "borderline"
	default:
		return "typical"
	}
}

// xguardExplanation 生成送模型复核用的原始标签说明。
func xguardExplanation(category string, isRisk bool) string {
	if !isRisk {
		return "原始XGuard标签判定为安全。"
	}
	return "原始XGuard标签判定为" + category + "风险。"
}

// openOutputs 创建所有输出临时文件。
func openOutputs(promptPath, responsePath, combinedPath string) (outputSet, error) {
	prompt, err := openJSONLOutput(promptPath)
	if err != nil {
		return outputSet{}, err
	}
	response, err := openJSONLOutput(responsePath)
	if err != nil {
		prompt.cleanup()
		return outputSet{}, err
	}
	outputs := outputSet{prompt: prompt, response: response}
	if combinedPath == "" {
		return outputs, nil
	}
	combined, err := openJSONLOutput(combinedPath)
	if err != nil {
		outputs.cleanup()
		return outputSet{}, err
	}
	outputs.combined = combined
	return outputs, nil
}

// openJSONLOutput 在目标目录创建待发布的临时 JSONL 文件。
func openJSONLOutput(path string) (*jsonlOutput, error) {
	if path == "" {
		return nil, fmt.Errorf("output path is empty")
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".prepare-xguard-v1-*.jsonl")
	if err != nil {
		return nil, fmt.Errorf("create temporary output for %q: %w", path, err)
	}
	return &jsonlOutput{
		path: path,
		temp: file.Name(),
		file: file,
		buf:  bufio.NewWriter(file),
	}, nil
}

// write 将一条记录写入对应场景输出和可选合并输出。
func (o outputSet) write(row labelReviewRow) error {
	switch row.Scene {
	case "prompt":
		if err := o.prompt.write(row); err != nil {
			return err
		}
	case "response":
		if err := o.response.write(row); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported scene %q", row.Scene)
	}
	if o.combined == nil {
		return nil
	}
	return o.combined.write(row)
}

// publish 刷盘并用临时文件替换正式输出。
func (o outputSet) publish() error {
	for _, output := range []*jsonlOutput{o.prompt, o.response, o.combined} {
		if output == nil {
			continue
		}
		if err := output.publish(); err != nil {
			return err
		}
	}
	return nil
}

// cleanup 删除尚未发布的临时文件。
func (o outputSet) cleanup() {
	for _, output := range []*jsonlOutput{o.prompt, o.response, o.combined} {
		if output != nil {
			output.cleanup()
		}
	}
}

// write 编码并追加一条 JSONL。
func (o *jsonlOutput) write(row labelReviewRow) error {
	encoded, err := json.Marshal(row)
	if err != nil {
		return fmt.Errorf("encode output row: %w", err)
	}
	if _, err := o.buf.Write(encoded); err != nil {
		return fmt.Errorf("write output row: %w", err)
	}
	if err := o.buf.WriteByte('\n'); err != nil {
		return fmt.Errorf("write output newline: %w", err)
	}
	return nil
}

// publish 完成单个输出文件的刷盘、关闭和替换。
func (o *jsonlOutput) publish() error {
	if err := o.buf.Flush(); err != nil {
		_ = o.file.Close()
		return fmt.Errorf("flush %q: %w", o.path, err)
	}
	if err := o.file.Sync(); err != nil {
		_ = o.file.Close()
		return fmt.Errorf("sync %q: %w", o.path, err)
	}
	if err := o.file.Close(); err != nil {
		return fmt.Errorf("close %q: %w", o.path, err)
	}
	if err := os.Rename(o.temp, o.path); err != nil {
		return fmt.Errorf("replace %q: %w", o.path, err)
	}
	o.temp = ""
	return nil
}

// cleanup 清理单个未发布临时文件。
func (o *jsonlOutput) cleanup() {
	if o.temp != "" {
		_ = o.file.Close()
		_ = os.Remove(o.temp)
	}
}

// valueOrDefault 返回非空值或默认值。
func valueOrDefault(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
