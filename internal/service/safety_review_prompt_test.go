package service_test

import (
	"encoding/json"
	"strings"
	"testing"

	"sendllm/internal/dto"
	"sendllm/internal/service"
)

// TestSafetyReviewPromptBuildsBlindRoleMessages 验证各角色输入边界和盲区。
func TestSafetyReviewPromptBuildsBlindRoleMessages(t *testing.T) {
	policy, err := service.LoadSafetyReviewPolicy("../../policy/releases/p04b-v1.0")
	if err != nil {
		t.Fatalf("LoadSafetyReviewPolicy() error = %v", err)
	}
	card := policy.Cards["ethnic_discrimination"]
	cardJSON, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("marshal card error = %v", err)
	}
	input := dto.SafetyReviewRoleInput{
		TraceID:  "trace-1",
		Scene:    "response",
		Prompt:   "discriminatory prompt",
		Response: "safe response",
		Policy:   "common policy",
		Schema:   []byte(`{"type":"object"}`),
		RuleCard: cardJSON,
		PriorOutputs: []byte(`{
			"judge_a":{"verdict":"safe"},
			"judge_b":{"verdict":"safe"},
			"router":{"coverage_complete":true},
			"experts":[],
			"terminal_failures":[],
			"fallback_used":false,
			"independence_degraded":false
		}`),
	}

	roles := map[dto.SafetyReviewRole][]string{
		dto.SafetyReviewJudgeA: {"trace_id", "scene", "prompt", "response", "policy", "schema"},
		dto.SafetyReviewJudgeB: {"trace_id", "scene", "prompt", "response", "policy", "schema"},
		dto.SafetyReviewRouter: {"trace_id", "scene", "prompt", "response", "policy", "schema"},
		dto.SafetyReviewExpert: {"trace_id", "scene", "prompt", "response", "policy", "schema", "rule_card"},
		dto.SafetyReviewArbiter: {
			"trace_id", "scene", "prompt", "response", "policy", "schema", "prior_outputs",
		},
	}
	for role, wantFields := range roles {
		input.SystemPrompt = policy.Prompts[string(role)]
		messages, err := service.BuildSafetyReviewMessages(role, input)
		if err != nil {
			t.Fatalf("BuildSafetyReviewMessages(%s) error = %v", role, err)
		}
		if len(messages) != 2 || messages[0].Role != "system" || messages[1].Role != "user" {
			t.Fatalf("%s message shape = %#v", role, messages)
		}
		if messages[0].Content != policy.Prompts[string(role)] {
			t.Fatalf("%s system prompt does not match release artifact", role)
		}
		var payload map[string]json.RawMessage
		if err := json.Unmarshal([]byte(messages[1].Content), &payload); err != nil {
			t.Fatalf("%s user content is not JSON: %v", role, err)
		}
		for _, field := range wantFields {
			if _, ok := payload[field]; !ok {
				t.Errorf("%s user payload lacks %s", role, field)
			}
		}
		for _, forbidden := range []string{"original_label", "annotation", "judge_a", "judge_b", "router_output"} {
			if _, ok := payload[forbidden]; ok {
				t.Errorf("%s user payload leaks forbidden field %s", role, forbidden)
			}
		}
	}

	promptInput := input
	promptInput.Scene = "prompt"
	promptInput.SystemPrompt = policy.Prompts[string(dto.SafetyReviewJudgeA)]
	messages, err := service.BuildSafetyReviewMessages(dto.SafetyReviewJudgeA, promptInput)
	if err != nil {
		t.Fatalf("BuildSafetyReviewMessages(prompt) error = %v", err)
	}
	var promptPayload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(messages[1].Content), &promptPayload); err != nil {
		t.Fatalf("prompt user content is not JSON: %v", err)
	}
	if _, ok := promptPayload["response"]; ok {
		t.Fatal("prompt scene must not send response content")
	}
}

// TestSafetyReviewPromptArbiterPriorOutputsContract 验证 Arbiter 先验输出对象和固定字段。
func TestSafetyReviewPromptArbiterPriorOutputsContract(t *testing.T) {
	policy, err := service.LoadSafetyReviewPolicy("../../policy/releases/p04b-v1.0")
	if err != nil {
		t.Fatalf("LoadSafetyReviewPolicy() error = %v", err)
	}
	base := dto.SafetyReviewRoleInput{
		TraceID:      "trace-1",
		Scene:        "response",
		Prompt:       "synthetic prompt",
		Response:     "synthetic response",
		Policy:       "common policy",
		Schema:       []byte(`{"type":"object"}`),
		SystemPrompt: policy.Prompts["arbiter"],
		PriorOutputs: []byte(`{
			"judge_a":{"verdict":"safe"},
			"judge_b":{"verdict":"safe"},
			"router":{"coverage_complete":true},
			"experts":[],
			"terminal_failures":[],
			"fallback_used":false,
			"independence_degraded":false
		}`),
	}
	messages, err := service.BuildSafetyReviewMessages(dto.SafetyReviewArbiter, base)
	if err != nil {
		t.Fatalf("BuildSafetyReviewMessages() error = %v", err)
	}
	var payload struct {
		PriorOutputs struct {
			JudgeA               json.RawMessage   `json:"judge_a"`
			JudgeB               json.RawMessage   `json:"judge_b"`
			Router               json.RawMessage   `json:"router"`
			Experts              []json.RawMessage `json:"experts"`
			TerminalFailures     []json.RawMessage `json:"terminal_failures"`
			FallbackUsed         bool              `json:"fallback_used"`
			IndependenceDegraded bool              `json:"independence_degraded"`
		} `json:"prior_outputs"`
	}
	if err := json.Unmarshal([]byte(messages[1].Content), &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if payload.PriorOutputs.JudgeA == nil || payload.PriorOutputs.JudgeB == nil ||
		payload.PriorOutputs.Router == nil || payload.PriorOutputs.Experts == nil ||
		payload.PriorOutputs.TerminalFailures == nil {
		t.Fatal("prior_outputs lacks one or more fixed fields")
	}

	missing := strings.Replace(
		`{"judge_a":{"verdict":"safe"},"experts":[],`+
			`"terminal_failures":[],"fallback_used":false,"independence_degraded":false}`,
		`"judge_a":{"verdict":"safe"},`,
		"",
		1,
	)
	base.PriorOutputs = []byte(missing)
	if _, err := service.BuildSafetyReviewMessages(dto.SafetyReviewArbiter, base); err == nil {
		t.Fatal("BuildSafetyReviewMessages() accepted prior_outputs missing judge_a")
	}
}

// TestSafetyReviewPromptArtifactsDeclareConstraints 验证编译提示词声明盲区和输出限制。
func TestSafetyReviewPromptArtifactsDeclareConstraints(t *testing.T) {
	policy, err := service.LoadSafetyReviewPolicy("../../policy/releases/p04b-v1.0")
	if err != nil {
		t.Fatalf("LoadSafetyReviewPolicy() error = %v", err)
	}
	for role, prompt := range policy.Prompts {
		lower := strings.ToLower(prompt)
		for _, want := range []string{"blind", "schema", "evidence", "chain-of-thought"} {
			if !strings.Contains(lower, want) {
				t.Errorf("%s prompt lacks %q", role, want)
			}
		}
		if !strings.Contains(prompt, "For Response scene, use prompt_only for prompt context") {
			t.Errorf("%s prompt lacks response-scene prompt_only instruction", role)
		}
	}
	router := strings.ToLower(policy.Prompts["router"])
	if !strings.Contains(router, "observable facts") || !strings.Contains(router, "recall candidates") {
		t.Error("router prompt does not restrict output to facts and recall candidates")
	}
	expertPrompt := policy.Prompts["expert"]
	expert := strings.ToLower(expertPrompt)
	if !strings.Contains(expert, "exactly one rule card") {
		t.Error("expert prompt does not restrict input to one rule card")
	}
	if !strings.Contains(expertPrompt, "E1") || !strings.Contains(expert, "position") {
		t.Error("expert prompt does not define positional evidence references")
	}
	if !strings.Contains(expertPrompt, "Copy every required condition and every decisive exclusion") {
		t.Error("expert prompt does not require full rule-card matrix")
	}
	arbiterPrompt := policy.Prompts["arbiter"]
	arbiter := strings.ToLower(arbiterPrompt)
	if !strings.Contains(arbiter, "no voting") || !strings.Contains(arbiter, "established") {
		t.Error("arbiter prompt does not prohibit voting and new categories")
	}
	if strings.Contains(arbiterPrompt, "or other role outputs") {
		t.Error("arbiter prompt contradicts prior-output visibility")
	}
	for _, want := range []string{"raw provider responses", "Judge A", "Judge B", "Router", "signals", "Only Expert"} {
		if !strings.Contains(arbiterPrompt, want) {
			t.Errorf("arbiter prompt lacks %q", want)
		}
	}
	for _, want := range []string{
		"is_attack=true only when attack_methods is non-empty",
		"hard_negative only when a decisive exclusion is present",
		"attack_domains must exactly match established Expert categories",
	} {
		if !strings.Contains(arbiterPrompt, want) {
			t.Errorf("arbiter prompt lacks live-field constraint %q", want)
		}
	}
}
