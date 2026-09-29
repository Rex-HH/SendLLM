package service_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"sendllm/internal/service"
)

// TestSafetyReviewPolicyLoadsImmutableBundle 验证发布包结构和不可变清单。
func TestSafetyReviewPolicyLoadsImmutableBundle(t *testing.T) {
	policy, err := service.LoadSafetyReviewPolicy("../../policy/releases/p04b-v1.0")
	if err != nil {
		t.Fatalf("LoadSafetyReviewPolicy() error = %v", err)
	}
	if policy.ReleaseVersion != "p04b-v1.0" || policy.SourcePolicyVersion != "1" || policy.CompilerVersion != "1" {
		t.Fatalf("release identity = %#v", policy)
	}
	if len(policy.Cards) != 16 {
		t.Fatalf("card count = %d, want 16", len(policy.Cards))
	}
	wantDomains := []string{
		"ethnic_discrimination", "religious_discrimination", "nationality_discrimination",
		"regional_discrimination", "gender_discrimination", "age_discrimination",
		"occupation_discrimination", "health_discrimination", "other_discrimination",
		"ethnic_hatred",
	}
	wantMethods := []string{
		"prompt_injection", "jailbreak", "encoding_obfuscation",
		"cross_language_attack", "cross_modal_attack", "multi_turn_jailbreak",
	}
	for _, id := range append(wantDomains, wantMethods...) {
		if policy.Cards[id] == nil {
			t.Errorf("missing card %s", id)
		}
	}
	if _, ok := policy.Cards["financial_domain_attack"]; ok {
		t.Error("financial_domain_attack must not be a card")
	}
	occupation := policy.Cards["occupation_discrimination"]
	employmentIdentities := []string{
		"正式员工", "合同制", "劳务派遣", "外包", "临时工", "稳定兼职身份",
	}
	if !containsAll(occupation.TargetAttributes, employmentIdentities...) {
		t.Errorf("occupation attributes = %#v", occupation.TargetAttributes)
	}
	other := policy.Cards["other_discrimination"]
	if containsAny(other.TargetAttributes, employmentIdentities...) {
		t.Errorf("other_discrimination leaked employment identities: %#v", other.TargetAttributes)
	}
	if !strings.Contains(policy.Common.RuleIDs, "DISCRIMINATION-R01") ||
		!strings.Contains(policy.Decisions, "P04B-DECISION-001") {
		t.Error("policy does not preserve approved human rule and decision IDs")
	}
	if len(policy.Prompts) != 6 || len(policy.Schemas) != 4 {
		t.Fatalf("prompt/schema counts = %d/%d, want 6/4", len(policy.Prompts), len(policy.Schemas))
	}
	humanReviewed := 0
	synthetic := 0
	for _, example := range policy.Examples {
		if example.Source == "human_reviewed" {
			humanReviewed++
			continue
		}
		if example.Source == "synthetic" {
			synthetic++
		}
	}
	if humanReviewed == 0 || synthetic > humanReviewed {
		t.Fatalf("example provenance = human %d, synthetic %d", humanReviewed, synthetic)
	}
	if policy.AggregateHash == "" {
		t.Fatal("aggregate hash is empty")
	}
	expertCoverage := make(map[string]bool)
	for _, id := range policy.Coverage["expert"] {
		expertCoverage[id] = true
	}
	arbiterCoverage := make(map[string]bool)
	for _, id := range policy.Coverage["arbiter"] {
		arbiterCoverage[id] = true
	}
	for id, card := range policy.Cards {
		if !arbiterCoverage[id] {
			t.Errorf("arbiter coverage lacks category %s", id)
		}
		for _, condition := range card.RequiredConditions {
			if !expertCoverage[condition.ID] || !arbiterCoverage[condition.ID] {
				t.Errorf("role coverage lacks condition %s", condition.ID)
			}
		}
		for _, exclusion := range card.DecisiveExclusions {
			if !expertCoverage[exclusion.ID] || !arbiterCoverage[exclusion.ID] {
				t.Errorf("role coverage lacks exclusion %s", exclusion.ID)
			}
		}
		if card.ConfusableWith != nil && policy.Cards[card.ConfusableWith[0]] == nil {
			t.Errorf("card %s has an invalid reference", id)
		}
		if card.ExamplesFile != "../../examples/p04b-development.jsonl" {
			t.Errorf("card %s references missing examples", id)
		}
	}
	if !arbiterCoverage["DISCRIMINATION-R01"] || !arbiterCoverage["P04B-DECISION-001"] {
		t.Error("role coverage lacks approved decision IDs")
	}

	second, err := service.LoadSafetyReviewPolicy("../../policy/releases/p04b-v1.0")
	if err != nil {
		t.Fatalf("second LoadSafetyReviewPolicy() error = %v", err)
	}
	if second.AggregateHash != policy.AggregateHash {
		t.Fatalf("aggregate hash changed: %q != %q", second.AggregateHash, policy.AggregateHash)
	}
}

// TestSafetyReviewPolicySchemasConstrainFactualKinds 验证 Schema 与本地事实类型闭集一致。
func TestSafetyReviewPolicySchemasConstrainFactualKinds(t *testing.T) {
	policy, err := service.LoadSafetyReviewPolicy("../../policy/releases/p04b-v1.0")
	if err != nil {
		t.Fatalf("LoadSafetyReviewPolicy() error = %v", err)
	}
	want := []string{
		"group_target", "identity_attribute", "negative_description", "group_generalization",
		"resource_employment_restriction", "refusal", "quotation_reporting_context",
		"operational_attack_wording",
	}
	for _, schemaName := range []string{"judgment", "router", "expert", "arbiter"} {
		var schema map[string]any
		if err := json.Unmarshal(policy.Schemas[schemaName], &schema); err != nil {
			t.Fatalf("decode %s schema: %v", schemaName, err)
		}
		for _, enum := range collectFieldEnums(schema, "kind") {
			if !equalStrings(enum, want) {
				t.Fatalf("%s kind enum = %#v, want %#v", schemaName, enum, want)
			}
		}
		if len(collectFieldEnums(schema, "kind")) == 0 {
			t.Fatalf("%s schema lacks kind enum", schemaName)
		}
	}
}

// TestSafetyReviewPolicySchemasConstrainEvidenceSources 验证 Schema 明确约束证据来源闭集。
func TestSafetyReviewPolicySchemasConstrainEvidenceSources(t *testing.T) {
	policy, err := service.LoadSafetyReviewPolicy("../../policy/releases/p04b-v1.0")
	if err != nil {
		t.Fatalf("LoadSafetyReviewPolicy() error = %v", err)
	}
	want := []string{"prompt", "response_explicit", "response_context_resolved", "prompt_only"}
	for _, schemaName := range []string{"judgment", "router", "expert", "arbiter"} {
		var schema map[string]any
		if err := json.Unmarshal(policy.Schemas[schemaName], &schema); err != nil {
			t.Fatalf("decode %s schema: %v", schemaName, err)
		}
		for _, enum := range collectFieldEnums(schema, "source") {
			if !equalStrings(enum, want) {
				t.Fatalf("%s source enum = %#v, want %#v", schemaName, enum, want)
			}
		}
		if len(collectFieldEnums(schema, "source")) == 0 {
			t.Fatalf("%s schema lacks source enum", schemaName)
		}
	}
}

// TestSafetyReviewPolicySchemasConstrainCategories 验证 Schema 明确约束类别闭集。
func TestSafetyReviewPolicySchemasConstrainCategories(t *testing.T) {
	policy, err := service.LoadSafetyReviewPolicy("../../policy/releases/p04b-v1.0")
	if err != nil {
		t.Fatalf("LoadSafetyReviewPolicy() error = %v", err)
	}
	for _, schemaName := range []string{"judgment", "router", "expert", "arbiter"} {
		var schema map[string]any
		if err := json.Unmarshal(policy.Schemas[schemaName], &schema); err != nil {
			t.Fatalf("decode %s schema: %v", schemaName, err)
		}
		if free := collectFreeCategoryStrings(schema); len(free) != 0 {
			t.Fatalf("%s schema leaves category fields unconstrained: %#v", schemaName, free)
		}
	}
}

// TestSafetyReviewPolicyArbiterSchemaConstrainsAttackFlag 验证 Arbiter Schema 对 is_attack 投影做硬约束。
func TestSafetyReviewPolicyArbiterSchemaConstrainsAttackFlag(t *testing.T) {
	policy, err := service.LoadSafetyReviewPolicy("../../policy/releases/p04b-v1.0")
	if err != nil {
		t.Fatalf("LoadSafetyReviewPolicy() error = %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(policy.Schemas["arbiter"], &schema); err != nil {
		t.Fatalf("decode arbiter schema: %v", err)
	}
	if !arbiterSchemaHasAttackFlagGuard(schema) {
		t.Fatal("arbiter schema lacks is_attack/attack_methods guard")
	}
}

// TestSafetyReviewPolicyRouterSchemaConstrainsFeatureIDs 验证 Router Schema 固定 F1..F8 引用格式。
func TestSafetyReviewPolicyRouterSchemaConstrainsFeatureIDs(t *testing.T) {
	policy, err := service.LoadSafetyReviewPolicy("../../policy/releases/p04b-v1.0")
	if err != nil {
		t.Fatalf("LoadSafetyReviewPolicy() error = %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(policy.Schemas["router"], &schema); err != nil {
		t.Fatalf("decode router schema: %v", err)
	}
	if !schemaHasPattern(schema, "id", "^F[1-8]$") {
		t.Fatal("router schema lacks feature id pattern")
	}
	if !schemaHasPattern(schema, "evidence_ids", "^F[1-8]$") {
		t.Fatal("router schema lacks evidence_ids pattern")
	}
}

// TestSafetyReviewPolicyRejectsBrokenBundles 验证路径、哈希、大小和未列资产。
func TestSafetyReviewPolicyRejectsBrokenBundles(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, dir string)
	}{
		{name: "tampered file", mutate: func(t *testing.T, dir string) {
			path := filepath.Join(dir, "policy", "rules", "attack_domain", "ethnic_discrimination.yaml")
			appendFile(t, path, "\n# tamper\n")
		}},
		{name: "missing file", mutate: func(t *testing.T, dir string) {
			path := filepath.Join(dir, "prompts", "router.txt")
			if err := os.Remove(path); err != nil {
				t.Fatalf("Remove() error = %v", err)
			}
		}},
		{name: "escaping manifest path", mutate: func(t *testing.T, dir string) {
			replaceFileText(
				t,
				filepath.Join(dir, "release.yaml"),
				"path: prompts/router.txt",
				"path: ../../router.txt",
			)
		}},
		{name: "absolute manifest path", mutate: func(t *testing.T, dir string) {
			replaceFileText(
				t,
				filepath.Join(dir, "release.yaml"),
				"path: prompts/router.txt",
				"path: /tmp/router.txt",
			)
		}},
		{name: "duplicate manifest path", mutate: func(t *testing.T, dir string) {
			replaceFileText(
				t,
				filepath.Join(dir, "release.yaml"),
				"  - {path: prompts/router.txt",
				"  - {path: prompts/router.txt, size: 511, "+
					"sha256: 6ad2190611061756e3d6571674457b29d835f77ab0b46cffc70dbf4af1ddd36c}\n"+
					"  - {path: prompts/router.txt",
			)
		}},
		{name: "unlisted required asset", mutate: func(t *testing.T, dir string) {
			writeFile(
				t,
				filepath.Join(dir, "policy", "rules", "attack_domain", "extra.yaml"),
				"id: extra\n",
			)
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := copyBundle(t)
			test.mutate(t, dir)
			_, err := service.LoadSafetyReviewPolicy(dir)
			if !errors.Is(err, service.ErrInvalidSafetyReviewPolicy) {
				t.Fatalf("LoadSafetyReviewPolicy() error = %v, want %v", err, service.ErrInvalidSafetyReviewPolicy)
			}
		})
	}
}

// TestSafetyReviewPolicyRejectsSymlinkedBundleComponents 验证根、清单和父路径组件都不是符号链接。
func TestSafetyReviewPolicyRejectsSymlinkedBundleComponents(t *testing.T) {
	t.Run("release manifest symlink", func(t *testing.T) {
		dir := copyBundle(t)
		targetDir := t.TempDir()
		target := filepath.Join(targetDir, "release.yaml")
		contents, err := os.ReadFile(filepath.Join(dir, "release.yaml"))
		if err != nil {
			t.Fatalf("ReadFile() error = %v", err)
		}
		writeFile(t, target, string(contents))
		path := filepath.Join(dir, "release.yaml")
		if err := os.Remove(path); err != nil {
			t.Fatalf("Remove() error = %v", err)
		}
		if err := os.Symlink(target, path); err != nil {
			t.Fatalf("Symlink() error = %v", err)
		}
		_, err = service.LoadSafetyReviewPolicy(dir)
		requireInvalidSafetyReviewPolicy(t, err)
	})

	t.Run("bundle root symlink", func(t *testing.T) {
		realBundle := copyBundle(t)
		link := filepath.Join(t.TempDir(), "bundle")
		if err := os.Symlink(realBundle, link); err != nil {
			t.Fatalf("Symlink() error = %v", err)
		}
		_, err := service.LoadSafetyReviewPolicy(link)
		requireInvalidSafetyReviewPolicy(t, err)
	})

	t.Run("parent directory symlink", func(t *testing.T) {
		dir := copyBundle(t)
		realPolicy := filepath.Join(dir, "policy_real")
		if err := os.Rename(filepath.Join(dir, "policy"), realPolicy); err != nil {
			t.Fatalf("Rename() error = %v", err)
		}
		if err := os.Symlink(realPolicy, filepath.Join(dir, "policy")); err != nil {
			t.Fatalf("Symlink() error = %v", err)
		}
		_, err := service.LoadSafetyReviewPolicy(dir)
		requireInvalidSafetyReviewPolicy(t, err)
	})
}

// TestSafetyReviewPolicyValidatesClosedCardSet 验证 16 张规则卡的闭集和固定契约。
func TestSafetyReviewPolicyValidatesClosedCardSet(t *testing.T) {
	policy, err := service.LoadSafetyReviewPolicy("../../policy/releases/p04b-v1.0")
	if err != nil {
		t.Fatalf("LoadSafetyReviewPolicy() error = %v", err)
	}
	type cardSpec struct {
		axis       string
		priority   int
		scenes     []string
		conditions []string
		exclusions []string
	}
	specificConditions := []string{"D-GROUP-TARGET", "D-IDENTITY-CAUSALITY", "D-HARMFUL-ACT"}
	specificExclusions := []string{
		"X-DESCRIPTION-CRITICISM", "X-INDIVIDUAL-ONLY", "X-OBJECTIVE-CRITERIA",
		"X-RESPONSE-NONCOMPLIANCE", "X-UNRESOLVED-PROXY",
	}
	methodConditions := []string{
		"M-OPERATIONAL-ACTION", "M-MODEL-OR-CONTROL-TARGET", "M-EVASION-FUNCTION",
	}
	domainSpec := func(priority int, conditions []string) cardSpec {
		return cardSpec{
			axis: "attack_domain", priority: priority, scenes: []string{"prompt", "response"},
			conditions: conditions, exclusions: specificExclusions,
		}
	}
	methodSpec := func(priority int, condition string) cardSpec {
		return cardSpec{
			axis: "attack_method", priority: priority, scenes: []string{"prompt"},
			conditions: append(methodConditions, condition),
			exclusions: []string{"MX-DISCUSSION-OR-QUOTATION"},
		}
	}
	want := map[string]cardSpec{
		"ethnic_discrimination":      domainSpec(100, specificConditions),
		"religious_discrimination":   domainSpec(101, specificConditions),
		"nationality_discrimination": domainSpec(102, specificConditions),
		"regional_discrimination":    domainSpec(103, specificConditions),
		"gender_discrimination":      domainSpec(104, specificConditions),
		"age_discrimination":         domainSpec(105, specificConditions),
		"occupation_discrimination":  domainSpec(106, specificConditions),
		"health_discrimination":      domainSpec(107, specificConditions),
		"other_discrimination": domainSpec(900, []string{
			"D-GROUP-TARGET", "D-STABLE-IDENTITY", "D-IDENTITY-CAUSALITY",
			"D-SERIOUS-MATERIAL-HARM", "D-HARMFUL-ACT",
		}),
		"ethnic_hatred": {
			axis: "attack_domain", priority: 10, scenes: []string{"prompt", "response"},
			conditions: []string{"H-ETHNIC-TARGET", "H-ACTIVE-INCITEMENT", "H-SEVERE-OUTCOME"},
			exclusions: []string{"X-DESCRIPTION-CRITICISM", "X-INDIVIDUAL-ONLY"},
		},
		"prompt_injection":      methodSpec(100, "M-INSTRUCTION-OVERRIDE"),
		"jailbreak":             methodSpec(101, "M-SAFETY-BYPASS"),
		"encoding_obfuscation":  methodSpec(102, "M-OBFUSCATION-EVASION"),
		"cross_language_attack": methodSpec(103, "M-LANGUAGE-EVASION"),
		"cross_modal_attack":    methodSpec(104, "M-MODAL-EVASION"),
		"multi_turn_jailbreak":  methodSpec(105, "M-STAGED-INDUCEMENT"),
	}
	for id, spec := range want {
		card := policy.Cards[id]
		if card == nil {
			t.Fatalf("missing card %s", id)
		}
		if card.Axis != spec.axis || card.PrimaryPriority != spec.priority ||
			!equalStrings(card.EnabledScenes, spec.scenes) ||
			!equalStrings(conditionIDs(card.RequiredConditions), spec.conditions) ||
			!equalStrings(exclusionIDs(card.DecisiveExclusions), spec.exclusions) {
			t.Fatalf("card %s = %#v, want %#v", id, card, spec)
		}
	}
	if len(policy.Cards) != len(want) {
		t.Fatalf("card count = %d, want %d", len(policy.Cards), len(want))
	}
}

// TestSafetyReviewPolicyRejectsSemanticPathMutation 验证规则卡路径必须匹配 axis 和 ID。
func TestSafetyReviewPolicyRejectsSemanticPathMutation(t *testing.T) {
	dir := copyBundle(t)
	original := filepath.Join(dir, "policy", "rules", "attack_domain", "ethnic_discrimination.yaml")
	renamed := filepath.Join(dir, "policy", "rules", "attack_domain", "wrong_name.yaml")
	if err := os.Rename(original, renamed); err != nil {
		t.Fatalf("Rename() error = %v", err)
	}
	refreshReleaseManifest(t, dir)
	_, err := service.LoadSafetyReviewPolicy(dir)
	if !errors.Is(err, service.ErrInvalidSafetyReviewPolicy) {
		t.Fatalf("LoadSafetyReviewPolicy() error = %v, want %v", err, service.ErrInvalidSafetyReviewPolicy)
	}
}

// TestSafetyReviewPolicyRejectsTargetAttributeMutation 验证 target_attributes 属于冻结语义闭集。
func TestSafetyReviewPolicyRejectsTargetAttributeMutation(t *testing.T) {
	dir := copyBundle(t)
	path := filepath.Join(dir, "policy", "rules", "attack_domain", "ethnic_discrimination.yaml")
	replaceFileText(t, path, "target_attributes: [民族, 种族]", "target_attributes: [未知属性]")
	refreshReleaseManifest(t, dir)
	_, err := service.LoadSafetyReviewPolicy(dir)
	requireInvalidSafetyReviewPolicy(t, err)
}

// TestSafetyReviewPolicyRejectsEscapingSymlink 验证发布包内符号链接逃逸被拒绝。
func TestSafetyReviewPolicyRejectsEscapingSymlink(t *testing.T) {
	dir := copyBundle(t)
	targetDir := t.TempDir()
	target := filepath.Join(targetDir, "router.txt")
	writeFile(t, target, "external prompt\n")
	path := filepath.Join(dir, "prompts", "router.txt")
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	refreshReleaseManifest(t, dir)
	_, err := service.LoadSafetyReviewPolicy(dir)
	if !errors.Is(err, service.ErrInvalidSafetyReviewPolicy) {
		t.Fatalf("LoadSafetyReviewPolicy() error = %v, want %v", err, service.ErrInvalidSafetyReviewPolicy)
	}
}

// TestSafetyReviewPolicyAcceptsPromptUnsafeMethodExample 验证 Prompt 样例可使用 attack_method 风险。
func TestSafetyReviewPolicyAcceptsPromptUnsafeMethodExample(t *testing.T) {
	dir := copyBundle(t)
	example := `{"sample_id":"prompt-method","scene":"prompt","prompt":"合成提示",` +
		`"response":"","gold_label":"unsafe","case_type":"typical","risk_type":"jailbreak",` +
		`"rule_ids":["DISCRIMINATION-R01"],"source":"synthetic"}`
	writeFile(t, filepath.Join(dir, "policy", "examples", "p04b-development.jsonl"), example+"\n")
	refreshReleaseManifest(t, dir)
	policy, err := service.LoadSafetyReviewPolicy(dir)
	if err != nil {
		t.Fatalf("LoadSafetyReviewPolicy() error = %v", err)
	}
	if len(policy.Examples) != 1 || policy.Examples[0].RiskType != "jailbreak" {
		t.Fatalf("examples = %#v", policy.Examples)
	}
}

// TestSafetyReviewPolicyRejectsMalformedExamples 验证 Development JSONL 的逐行严格契约。
func TestSafetyReviewPolicyRejectsMalformedExamples(t *testing.T) {
	base := `{"sample_id":"test","scene":"response","prompt":"合成提示","response":"合成回复",` +
		`"gold_label":"safe","case_type":"typical","risk_type":"",` +
		`"rule_ids":["DISCRIMINATION-R01"],"source":"synthetic"}`
	tests := []struct {
		name string
		data string
	}{
		{name: "empty line", data: base + "\n\n"},
		{name: "same line multiple objects", data: base + " " + base + "\n"},
		{name: "multi line object", data: "{\n" + base[1:] + "\n"},
		{
			name: "unknown field",
			data: strings.Replace(
				base,
				`"source":"synthetic"`,
				`"source":"synthetic","unknown":true`,
				1,
			),
		},
		{name: "duplicate sample id", data: base + "\n" + base + "\n"},
		{name: "invalid scene", data: strings.Replace(base, `"scene":"response"`, `"scene":"pair"`, 1)},
		{name: "invalid source", data: strings.Replace(base, `"source":"synthetic"`, `"source":"unknown"`, 1)},
		{name: "invalid gold label", data: strings.Replace(base, `"gold_label":"safe"`, `"gold_label":"unknown"`, 1)},
		{name: "invalid case type", data: strings.Replace(base, `"case_type":"typical"`, `"case_type":"uncertain"`, 1)},
		{name: "safe nonempty risk", data: strings.Replace(base, `"risk_type":""`, `"risk_type":"ethnic_discrimination"`, 1)},
		{
			name: "unsafe empty risk",
			data: strings.Replace(
				strings.Replace(base, `"gold_label":"safe"`, `"gold_label":"unsafe"`, 1),
				`"risk_type":""`,
				`"risk_type":""`,
				1,
			),
		},
		{
			name: "unsafe unknown risk",
			data: strings.Replace(
				strings.Replace(base, `"gold_label":"safe"`, `"gold_label":"unsafe"`, 1),
				`"risk_type":""`,
				`"risk_type":"unknown"`,
				1,
			),
		},
		{name: "unknown rule id", data: strings.Replace(base, `"DISCRIMINATION-R01"`, `"UNKNOWN-RULE"`, 1)},
		{name: "empty rule ids", data: strings.Replace(base, `"rule_ids":["DISCRIMINATION-R01"]`, `"rule_ids":[]`, 1)},
		{
			name: "hard negative unsafe",
			data: strings.Replace(
				strings.Replace(base, `"gold_label":"safe"`, `"gold_label":"unsafe"`, 1),
				`"case_type":"typical"`,
				`"case_type":"hard_negative"`,
				1,
			),
		},
		{
			name: "variant safe",
			data: strings.Replace(base, `"case_type":"typical"`, `"case_type":"variant"`, 1),
		},
		{
			name: "response unsafe method risk",
			data: strings.Replace(
				strings.Replace(base, `"gold_label":"safe"`, `"gold_label":"unsafe"`, 1),
				`"risk_type":""`,
				`"risk_type":"jailbreak"`,
				1,
			),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := copyBundle(t)
			writeFile(t, filepath.Join(dir, "policy", "examples", "p04b-development.jsonl"), test.data+"\n")
			refreshReleaseManifest(t, dir)
			_, err := service.LoadSafetyReviewPolicy(dir)
			if !errors.Is(err, service.ErrInvalidSafetyReviewPolicy) {
				t.Fatalf("LoadSafetyReviewPolicy() error = %v, want %v", err, service.ErrInvalidSafetyReviewPolicy)
			}
		})
	}
}

// TestSafetyReviewPolicyExampleProvenance 验证人工复核样例只保留成对人工文本。
func TestSafetyReviewPolicyExampleProvenance(t *testing.T) {
	policy, err := service.LoadSafetyReviewPolicy("../../policy/releases/p04b-v1.0")
	if err != nil {
		t.Fatalf("LoadSafetyReviewPolicy() error = %v", err)
	}
	human := 0
	synthetic := 0
	for _, example := range policy.Examples {
		switch example.Source {
		case "human_reviewed":
			human++
			if example.Prompt == "" || example.Response == "" {
				t.Fatalf("human example %s lacks reviewed pair", example.SampleID)
			}
		case "synthetic":
			synthetic++
		default:
			t.Fatalf("example %s has invalid source", example.SampleID)
		}
	}
	if human != 10 || synthetic != 5 {
		t.Fatalf("provenance counts = human %d synthetic %d, want 10/5", human, synthetic)
	}
}

// TestSafetyReviewPolicyFixedHumanProvenanceHashes 固定人工样例内容摘要，防止任何字节漂移。
func TestSafetyReviewPolicyFixedHumanProvenanceHashes(t *testing.T) {
	want := map[string]string{
		"human-safe-001":       "e7326a8874161ea309291094707b08c7ea38f9d02a1b1dda596197c0c6ea63f4",
		"human-safe-002":       "f1735d21ffc69ace656e093a0cc9d36f9182acff2ad757f783342dcf9f7be648",
		"human-unsafe-001":     "3d65e947ba04234ef50622c6f88afa437df1e58e06a01808eca6568c5411ef76",
		"human-unsafe-002":     "94dbf64b4603b0f108a3540edc4c9f158daf572ef8fe0f0f8c71b8d77ae87671",
		"human-unsafe-003":     "000965a44879b50baf744d05fdecf95ce44a4fd6994fa1219b757ba5c8918141",
		"human-borderline-001": "2653299d546c7f8a6b7a6b468862a56b542c7df0050c14187a444f38f445d639",
		"human-borderline-002": "8876ac665381b98cf7cdfac20be2b60dfa29db5be10e63ab3d412eb5fd4a838b",
		"human-borderline-003": "e610d5dd6c2bae15bedff8a5ec938fa16d691b4272840c919b0abd6ae78c8496",
		"human-hard-003":       "d24b45d05255abb5cc112d7d48450b0afd46379f3b567bc645cea4302a915ccc",
		"human-hard-004":       "621f43f9d371e9181cce5ab5ac15730dc48511301a5ae0797dfb7eeb1dba2bed",
	}
	policy, err := service.LoadSafetyReviewPolicy("../../policy/releases/p04b-v1.0")
	if err != nil {
		t.Fatalf("LoadSafetyReviewPolicy() error = %v", err)
	}
	seen := make(map[string]bool, len(want))
	for _, example := range policy.Examples {
		if example.Source != "human_reviewed" {
			continue
		}
		digest := sha256.Sum256([]byte(example.Prompt + "\x00" + example.Response))
		actual := hex.EncodeToString(digest[:])
		if want[example.SampleID] != actual {
			t.Errorf("sample %s provenance hash = %s, want %s", example.SampleID, actual, want[example.SampleID])
		}
		seen[example.SampleID] = true
	}
	if len(seen) != len(want) {
		t.Fatalf("human sample count = %d, want %d", len(seen), len(want))
	}
}

// TestSafetyReviewPolicyCardJSONUsesSnakeCase 验证规则卡 JSON 与 YAML 字段名一致。
func TestSafetyReviewPolicyCardJSONUsesSnakeCase(t *testing.T) {
	policy, err := service.LoadSafetyReviewPolicy("../../policy/releases/p04b-v1.0")
	if err != nil {
		t.Fatalf("LoadSafetyReviewPolicy() error = %v", err)
	}
	cardJSON, err := json.Marshal(policy.Cards["ethnic_discrimination"])
	if err != nil {
		t.Fatalf("Marshal(card) error = %v", err)
	}
	textJSON, err := json.Marshal(policy.Cards["ethnic_discrimination"].RequiredConditions[0])
	if err != nil {
		t.Fatalf("Marshal(rule text) error = %v", err)
	}
	var cardFields map[string]json.RawMessage
	if err := json.Unmarshal(cardJSON, &cardFields); err != nil {
		t.Fatalf("Unmarshal(card) error = %v", err)
	}
	var textFields map[string]json.RawMessage
	if err := json.Unmarshal(textJSON, &textFields); err != nil {
		t.Fatalf("Unmarshal(rule text) error = %v", err)
	}
	for _, want := range []string{
		`"id"`, `"axis"`, `"enabled_scenes"`, `"primary_priority"`, `"target_attributes"`,
		`"required_conditions"`, `"decisive_exclusions"`, `"confusable_with"`,
		`"error_patterns"`, `"examples_file"`, `"text"`,
	} {
		if !strings.Contains(string(cardJSON), want) && !strings.Contains(string(textJSON), want) {
			t.Fatalf("rule-card JSON lacks snake_case field %s: %s %s", want, cardJSON, textJSON)
		}
	}
	for _, fields := range []map[string]json.RawMessage{cardFields, textFields} {
		for name := range fields {
			if strings.ToLower(name) != name || strings.ContainsAny(name, "-") {
				t.Fatalf("rule-card JSON contains non-snake_case field %q", name)
			}
		}
	}
}

// copyBundle 复制可信发布包到独立临时目录。
func copyBundle(t *testing.T) string {
	t.Helper()
	destination := t.TempDir()
	source := "../../policy/releases/p04b-v1.0"
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return err
		}
		return os.WriteFile(target, contents, 0o600)
	})
	if err != nil {
		t.Fatalf("copy bundle error = %v", err)
	}
	return destination
}

// requireInvalidSafetyReviewPolicy 断言错误属于发布包契约。
func requireInvalidSafetyReviewPolicy(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, service.ErrInvalidSafetyReviewPolicy) {
		t.Fatalf("error = %v, want %v", err, service.ErrInvalidSafetyReviewPolicy)
	}
}

// refreshReleaseManifest 重新计算发布包所有文件的 size、SHA-256 和 aggregate hash。
func refreshReleaseManifest(t *testing.T, dir string) {
	t.Helper()
	paths := make([]string, 0)
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Base(path) == "release.yaml" {
			return nil
		}
		relative, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		t.Fatalf("walk bundle error = %v", err)
	}
	sort.Strings(paths)
	hashInput := make([]byte, 0)
	var manifest strings.Builder
	manifest.WriteString("release_version: p04b-v1.0\nsource_policy_version: \"1\"\n")
	manifest.WriteString("compiler_version: \"1\"\naggregate_hash: PLACEHOLDER\nfiles:\n")
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path)))
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", path, err)
		}
		digest := sha256.Sum256(data)
		manifest.WriteString("  - {path: " + path + ", size: " +
			itoa(int64(len(data))) + ", sha256: " + hex.EncodeToString(digest[:]) + "}\n")
		hashInput = append(hashInput, path...)
		hashInput = append(hashInput, 0)
		hashInput = append(hashInput, data...)
	}
	aggregate := sha256.Sum256(hashInput)
	contents := strings.Replace(
		manifest.String(),
		"PLACEHOLDER",
		hex.EncodeToString(aggregate[:]),
		1,
	)
	writeFile(t, filepath.Join(dir, "release.yaml"), contents)
}

// conditionIDs 提取条件 ID 列表。
func conditionIDs(values []service.SafetyReviewRuleText) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.ID)
	}
	return result
}

// exclusionIDs 提取排除 ID 列表。
func exclusionIDs(values []service.SafetyReviewRuleText) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.ID)
	}
	return result
}

// equalStrings 判断两个字符串切片完全相等。
func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// itoa 将整数转换为字符串，避免测试引入额外依赖。
func itoa(value int64) string {
	if value == 0 {
		return "0"
	}
	var buffer bytes.Buffer
	for value > 0 {
		buffer.WriteByte(byte('0' + value%10))
		value /= 10
	}
	var result []byte
	contents := buffer.Bytes()
	for i := len(contents) - 1; i >= 0; i-- {
		result = append(result, contents[i])
	}
	return string(result)
}

// appendFile 向现有文件追加内容。
func appendFile(t *testing.T, path, contents string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("OpenFile(%q) error = %v", path, err)
	}
	defer file.Close()
	if _, err := file.WriteString(contents); err != nil {
		t.Fatalf("WriteString(%q) error = %v", path, err)
	}
}

// replaceFileText 替换文件中的第一处文本。
func replaceFileText(t *testing.T, path, old, new string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	value := strings.Replace(string(contents), old, new, 1)
	writeFile(t, path, value)
}

// writeFile 写入测试文件。
func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

// containsAll 判断切片是否包含全部指定值。
func containsAll(values []string, wants ...string) bool {
	for _, want := range wants {
		if !sliceContains(values, want) {
			return false
		}
	}
	return true
}

// containsAny 判断切片是否包含任一指定值。
func containsAny(values []string, wants ...string) bool {
	for _, want := range wants {
		if sliceContains(values, want) {
			return true
		}
	}
	return false
}

// collectFieldEnums 递归收集 Schema 中指定字段的枚举。
func collectFieldEnums(value any, field string) [][]string {
	switch typed := value.(type) {
	case map[string]any:
		var result [][]string
		for key, nested := range typed {
			if key == field {
				if nestedMap, ok := nested.(map[string]any); ok {
					if enum, ok := nestedMap["enum"].([]any); ok {
						result = append(result, anyStrings(enum))
					}
				}
				continue
			}
			result = append(result, collectFieldEnums(nested, field)...)
		}
		return result
	case []any:
		var result [][]string
		for _, nested := range typed {
			result = append(result, collectFieldEnums(nested, field)...)
		}
		return result
	default:
		return nil
	}
}

// anyStrings 将 JSON 数组转换为字符串切片。
func anyStrings(values []any) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

// collectFreeCategoryStrings 递归查找没有 enum 的类别字段。
func collectFreeCategoryStrings(value any) []string {
	switch typed := value.(type) {
	case map[string]any:
		var result []string
		for key, nested := range typed {
			if categorySchemaName(key) {
				if nestedMap, ok := nested.(map[string]any); ok && nestedMap["type"] == "string" {
					if _, ok := nestedMap["enum"]; !ok {
						result = append(result, key)
					}
				}
				continue
			}
			if key == "items" {
				continue
			}
			result = append(result, collectFreeCategoryStrings(nested)...)
		}
		return result
	case []any:
		var result []string
		for _, nested := range typed {
			result = append(result, collectFreeCategoryStrings(nested)...)
		}
		return result
	default:
		return nil
	}
}

// arbiterSchemaHasAttackFlagGuard 判断 Schema 是否约束 is_attack 与 attack_methods 的一致性。
func arbiterSchemaHasAttackFlagGuard(schema map[string]any) bool {
	allOf, _ := schema["allOf"].([]any)
	for _, item := range allOf {
		rule, ok := item.(map[string]any)
		if !ok {
			continue
		}
		thenRule, _ := rule["then"].(map[string]any)
		properties, _ := thenRule["properties"].(map[string]any)
		isAttack, _ := properties["is_attack"].(map[string]any)
		attackMethods, _ := properties["attack_methods"].(map[string]any)
		if isAttack["const"] == false || attackMethods["minItems"] == float64(1) {
			return true
		}
	}
	return false
}

// schemaHasPattern 判断指定字段或其数组元素是否声明固定 pattern。
func schemaHasPattern(value any, field string, pattern string) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			if key == field {
				if fieldSchemaHasPattern(nested, pattern) {
					return true
				}
				continue
			}
			if schemaHasPattern(nested, field, pattern) {
				return true
			}
		}
	case []any:
		for _, nested := range typed {
			if schemaHasPattern(nested, field, pattern) {
				return true
			}
		}
	}
	return false
}

// fieldSchemaHasPattern 判断单个字段 Schema 或数组元素 Schema 是否声明 pattern。
func fieldSchemaHasPattern(value any, pattern string) bool {
	typed, ok := value.(map[string]any)
	if !ok {
		return false
	}
	if typed["pattern"] == pattern {
		return true
	}
	items, ok := typed["items"].(map[string]any)
	return ok && items["pattern"] == pattern
}

// categorySchemaName 判断字段是否代表策略类别。
func categorySchemaName(name string) bool {
	switch name {
	case "category", "primary_attack_method", "primary_attack_domain", "primary_risk_type":
		return true
	default:
		return false
	}
}

// sliceContains 判断字符串切片是否包含指定值。
func sliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
