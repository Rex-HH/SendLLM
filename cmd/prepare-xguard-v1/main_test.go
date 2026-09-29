package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareXGuardV1SplitsAndMapsUnifiedRows(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "xguard.jsonl")
	promptPath := filepath.Join(directory, "prompt.jsonl")
	responsePath := filepath.Join(directory, "response.jsonl")
	combinedPath := filepath.Join(directory, "combined.jsonl")
	writeTestFile(t, inputPath,
		xguardLine("sample-user", true, "个人隐私", "边界正例", "medium", "user")+
			xguardLine("sample-assistant", true, "商业秘密", "正例", "high", "assistant")+
			xguardLine("sample-pair", false, "安全", "负例", "none", "pair"),
	)

	var stdout bytes.Buffer
	if err := run([]string{
		"-input", inputPath,
		"-prompt-output", promptPath,
		"-response-output", responsePath,
		"-combined-output", combinedPath,
	}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "prompt=2 response=2 combined=4") {
		t.Fatalf("stdout = %q, want output counts", stdout.String())
	}

	promptRows := readRows(t, promptPath)
	responseRows := readRows(t, responsePath)
	combinedRows := readRows(t, combinedPath)
	if len(promptRows) != 2 || len(responseRows) != 2 || len(combinedRows) != 4 {
		t.Fatalf(
			"counts = prompt %d response %d combined %d, want 2/2/4",
			len(promptRows),
			len(responseRows),
			len(combinedRows),
		)
	}
	if promptRows[0]["trace_id"] != "sample-user__prompt" || promptRows[0]["scene"] != "prompt" {
		t.Fatalf("prompt row = %#v, want prompt trace and scene", promptRows[0])
	}
	extended := promptRows[0]["extended_info"].(map[string]any)
	if extended["attack_domain"] != "privacy_right_infringement" ||
		extended["case_type"] != "borderline" ||
		extended["is_attack"] != true {
		t.Fatalf("prompt extended_info = %#v", extended)
	}
	responseExtended := responseRows[0]["extended_info"].(map[string]any)
	if responseRows[0]["trace_id"] != "sample-assistant__response" ||
		responseExtended["attack_domain"] != "trade_secret_leakage" {
		t.Fatalf("response row = %#v", responseRows[0])
	}
	safeExtended := combinedRows[3]["extended_info"].(map[string]any)
	if combinedRows[3]["label"] != "safe" ||
		safeExtended["is_attack"] != false ||
		safeExtended["risk_level"] != "" ||
		safeExtended["case_type"] != "typical" {
		t.Fatalf("safe pair response row = %#v", combinedRows[3])
	}
}

// xguardLine 生成 prepare 命令测试使用的 XGuard 输入行。
func xguardLine(id string, risk bool, category string, boundary string, severity string, shape string) string {
	messages := []map[string]string{}
	if shape == "user" || shape == "pair" {
		messages = append(messages, map[string]string{"role": "user", "content": "合成用户内容"})
	}
	if shape == "assistant" || shape == "pair" {
		messages = append(messages, map[string]string{"role": "assistant", "content": "合成助手内容"})
	}
	record := map[string]any{
		"sample_id":            id,
		"split":                "train",
		"messages":             messages,
		"is_risk":              risk,
		"labels":               []map[string]string{{"category_name": category}},
		"boundary_sample_type": boundary,
		"severity":             severity,
		"source_language":      "zh",
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		panic(err)
	}
	return string(encoded) + "\n"
}

// writeTestFile 写入测试夹具文件。
func writeTestFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

// readRows 读取 JSONL 测试输出。
func readRows(t *testing.T, path string) []map[string]any {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	var rows []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(contents)), "\n") {
		if line == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("invalid JSONL row: %v", err)
		}
		rows = append(rows, row)
	}
	return rows
}
