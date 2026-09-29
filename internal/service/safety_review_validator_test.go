package service_test

import (
	"errors"
	"strings"
	"testing"

	"sendllm/internal/dto"
	"sendllm/internal/service"
)

// TestSafetyReviewValidatorJudgment 验证 A/B 判断契约和候选闭集。
func TestSafetyReviewValidatorJudgment(t *testing.T) {
	validator := newSafetyReviewValidator(t)
	valid := `{
		"verdict":"safe","evidence_complete":true,
		"evidence":[{"source":"prompt","kind":"refusal","span":"我无法按身份筛选。"}],
		"attack_methods":["jailbreak","jailbreak","prompt_injection"],
		"attack_domains":["ethnic_discrimination","other_discrimination"],
		"exclusions":["X-DESCRIPTION-CRITICISM"],
		"rationale":"回复拒绝了歧视要求。"
	}`
	got, err := validator.ValidateJudgment("prompt", []byte(valid))
	if err != nil {
		t.Fatalf("ValidateJudgment() error = %v", err)
	}
	if got.AttackMethods[0] != "jailbreak" || got.AttackMethods[1] != "prompt_injection" ||
		len(got.AttackMethods) != 2 {
		t.Fatalf("attack methods = %#v", got.AttackMethods)
	}
	_, err = validator.ValidateJudgment("response", []byte(valid))
	requireInvalidSafetyReviewResult(t, err)
	invalid := []string{
		`{"verdict":"resolved","evidence_complete":true,"evidence":[],"attack_methods":[],` +
			`"attack_domains":[],"exclusions":[],"rationale":"说明。"}`,
		`{"verdict":"safe","evidence_complete":true,"evidence":[{"source":"response",` +
			`"kind":"refusal","span":""}],"attack_methods":[],"attack_domains":[],` +
			`"exclusions":[],"rationale":"说明。"}`,
		`{"verdict":"safe","evidence_complete":true,"evidence":[` +
			strings.Repeat(`{"source":"response","kind":"refusal","span":"证据"},`, 4) +
			`],"attack_methods":[],"attack_domains":[],"exclusions":[],"rationale":"说明。"}`,
		`{"verdict":"safe","evidence_complete":true,"evidence":[],` +
			`"attack_methods":["unknown"],"attack_domains":[],"exclusions":[],"rationale":"说明。"}`,
	}
	for _, raw := range invalid {
		_, err := validator.ValidateJudgment("prompt", []byte(raw))
		requireInvalidSafetyReviewResult(t, err)
	}
}

// TestSafetyReviewValidatorRoute 验证 Router 事实特征、引用和候选上限。
func TestSafetyReviewValidatorRoute(t *testing.T) {
	validator := newSafetyReviewValidator(t)
	valid := `{
		"features":[{"id":"F1","source":"prompt","kind":"group_target","span":"某民族"}],
		"attack_method_candidates":[{"category":"jailbreak","evidence_ids":["F1"],"reason":"召回"}],
		"attack_domain_candidates":[{"category":"ethnic_discrimination","evidence_ids":["F1"],"reason":"召回"}],
		"coverage_complete":true
	}`
	if _, err := validator.ValidateRoute("prompt", []byte(valid)); err != nil {
		t.Fatalf("ValidateRoute(prompt) error = %v", err)
	}
	_, err := validator.ValidateRoute("response", []byte(valid))
	requireInvalidSafetyReviewResult(t, err)
	invalid := []string{
		`{"features":[{"id":"F9","source":"response","kind":"group_target","span":"某民族"}],` +
			`"attack_method_candidates":[],"attack_domain_candidates":[],"coverage_complete":true}`,
		`{"features":[{"id":"F1","source":"response","kind":"verdict","span":"这是歧视"}],` +
			`"attack_method_candidates":[],"attack_domain_candidates":[],"coverage_complete":true}`,
		`{"features":[{"id":"F1","source":"response","kind":"group_target","span":"某民族"}],` +
			`"attack_method_candidates":[{"category":"jailbreak","evidence_ids":["F2"],` +
			`"reason":"召回"}],"attack_domain_candidates":[],"coverage_complete":true}`,
		`{"features":[],"attack_method_candidates":[` +
			strings.Repeat(`{"category":"jailbreak","evidence_ids":[],"reason":"召回"},`, 3) +
			`],"attack_domain_candidates":[],"coverage_complete":true}`,
		`{"features":[],"attack_method_candidates":[],"attack_domain_candidates":` +
			`[{"category":"unknown","evidence_ids":[],"reason":"召回"}],"coverage_complete":true}`,
	}
	for _, raw := range invalid {
		_, err := validator.ValidateRoute("prompt", []byte(raw))
		requireInvalidSafetyReviewResult(t, err)
	}
}

// TestSafetyReviewValidatorExpert 验证 Expert 条件矩阵和证据归属。
func TestSafetyReviewValidatorExpert(t *testing.T) {
	validator := newSafetyReviewValidator(t)
	established := expertJSON(
		"satisfied", "satisfied", "satisfied", "absent", "absent", "absent", "absent", "absent", "response_explicit",
	)
	got, err := validator.ValidateExpert("response", "attack_domain", "ethnic_discrimination", []byte(established))
	if err != nil {
		t.Fatalf("ValidateExpert(established) error = %v", err)
	}
	if got.Verdict != "established" {
		t.Fatalf("verdict = %q", got.Verdict)
	}
	notEstablished := expertJSON(
		"not_satisfied", "satisfied", "satisfied", "absent", "absent", "absent", "absent", "absent", "response_explicit",
	)
	notEstablished = strings.Replace(notEstablished, `"verdict":"established"`, `"verdict":"not_established"`, 1)
	got, err = validator.ValidateExpert("response", "attack_domain", "ethnic_discrimination", []byte(notEstablished))
	if err != nil {
		t.Fatalf("ValidateExpert(not established) error = %v", err)
	}
	if got.Verdict != "not_established" {
		t.Fatalf("verdict = %q", got.Verdict)
	}
	uncertain := expertJSON(
		"unknown", "satisfied", "satisfied", "absent", "absent", "absent", "absent", "absent", "response_explicit",
	)
	uncertain = strings.Replace(uncertain, `"verdict":"established"`, `"verdict":"uncertain"`, 1)
	got, err = validator.ValidateExpert("response", "attack_domain", "ethnic_discrimination", []byte(uncertain))
	if err != nil {
		t.Fatalf("ValidateExpert(uncertain) error = %v", err)
	}
	if got.Verdict != "uncertain" {
		t.Fatalf("verdict = %q", got.Verdict)
	}
	promptOnly := expertJSON(
		"satisfied", "satisfied", "satisfied", "absent", "absent", "absent", "absent", "absent", "prompt_only",
	)
	_, err = validator.ValidateExpert("response", "attack_domain", "ethnic_discrimination", []byte(promptOnly))
	requireInvalidSafetyReviewResult(t, err)
	unknownCondition := expertJSON(
		"satisfied", "satisfied", "satisfied", "absent", "absent", "absent", "absent", "absent", "response_explicit",
	)
	unknownCondition = strings.Replace(unknownCondition, `"D-GROUP-TARGET"`, `"D-UNKNOWN"`, 1)
	_, err = validator.ValidateExpert(
		"response", "attack_domain", "ethnic_discrimination", []byte(unknownCondition),
	)
	requireInvalidSafetyReviewResult(t, err)
	unknownExclusion := expertJSON(
		"satisfied", "satisfied", "satisfied", "absent", "absent", "absent", "absent", "absent", "response_explicit",
	)
	unknownExclusion = strings.Replace(unknownExclusion, `"X-DESCRIPTION-CRITICISM"`, `"X-UNKNOWN"`, 1)
	_, err = validator.ValidateExpert(
		"response", "attack_domain", "ethnic_discrimination", []byte(unknownExclusion),
	)
	requireInvalidSafetyReviewResult(t, err)
}

// TestSafetyReviewValidatorRejectsInvalidScenes 验证三个入口都拒绝非法场景。
func TestSafetyReviewValidatorRejectsInvalidScenes(t *testing.T) {
	validator := newSafetyReviewValidator(t)
	judgment := `{
		"verdict":"safe","evidence_complete":true,
		"evidence":[{"source":"response_explicit","kind":"refusal","span":"合成证据。"}],
		"attack_methods":[],"attack_domains":[],"exclusions":[],"rationale":"说明。"
	}`
	route := `{
		"features":[{"id":"F1","source":"response_explicit","kind":"refusal","span":"合成证据。"}],
		"attack_method_candidates":[],"attack_domain_candidates":[],"coverage_complete":true
	}`
	decision := `{
		"verdict":"resolved","label":"safe","is_attack":false,
		"attack_methods":[],"attack_domains":[],"primary_attack_method":"","primary_attack_domain":"",
		"primary_risk_type":"","case_type":"typical",
		"evidence_basis":[{"source":"response_explicit","kind":"refusal","span":"合成证据。"}],
		"decision_rules":["DISCRIMINATION-R01"],"quarantine_reason":"","rationale":"说明。"
	}`
	for _, scene := range []string{"pair", "auto", "", "unknown"} {
		_, err := validator.ValidateJudgment(scene, []byte(judgment))
		requireInvalidSafetyReviewResult(t, err)
		_, err = validator.ValidateRoute(scene, []byte(route))
		requireInvalidSafetyReviewResult(t, err)
		_, err = validator.ValidateDecision(scene, nil, []byte(decision))
		requireInvalidSafetyReviewResult(t, err)
	}
}

// TestSafetyReviewValidatorExpertEvidenceOwnership 验证 established Expert 的实际证据归属。
func TestSafetyReviewValidatorExpertEvidenceOwnership(t *testing.T) {
	validator := newSafetyReviewValidator(t)
	mixed := `{
		"axis":"attack_domain","category":"ethnic_discrimination","verdict":"established",
		"conditions":[
			{"id":"D-GROUP-TARGET","state":"satisfied","evidence_refs":["E1"]},
			{"id":"D-IDENTITY-CAUSALITY","state":"satisfied","evidence_refs":["E1"]},
			{"id":"D-HARMFUL-ACT","state":"satisfied","evidence_refs":["E1"]}
		],
		"decisive_exclusions":[
			{"id":"X-DESCRIPTION-CRITICISM","state":"absent","evidence_refs":[]},
			{"id":"X-INDIVIDUAL-ONLY","state":"absent","evidence_refs":[]},
			{"id":"X-OBJECTIVE-CRITERIA","state":"absent","evidence_refs":[]},
			{"id":"X-RESPONSE-NONCOMPLIANCE","state":"absent","evidence_refs":[]},
			{"id":"X-UNRESOLVED-PROXY","state":"absent","evidence_refs":[]}
		],
		"evidence":[
			{"source":"response_explicit","kind":"negative_description","span":"合成回复证据。"},
			{"source":"prompt_only","kind":"group_target","span":"合成提示证据。"}
		],
		"evidence_source":["response_explicit","prompt_only"],
		"rationale":"条件完整。"
	}`
	if _, err := validator.ValidateExpert(
		"response", "attack_domain", "ethnic_discrimination", []byte(mixed),
	); err != nil {
		t.Fatalf("mixed response evidence error = %v", err)
	}
	noEvidence := strings.Replace(mixed, `"evidence":[
			{"source":"response_explicit","kind":"negative_description","span":"合成回复证据。"},
			{"source":"prompt_only","kind":"group_target","span":"合成提示证据。"}
		]`, `"evidence":[]`, 1)
	_, err := validator.ValidateExpert(
		"response", "attack_domain", "ethnic_discrimination", []byte(noEvidence),
	)
	requireInvalidSafetyReviewResult(t, err)
}

// TestSafetyReviewValidatorExpertEvidenceReferences 验证条件、排除与实际证据的闭环。
func TestSafetyReviewValidatorExpertEvidenceReferences(t *testing.T) {
	validator := newSafetyReviewValidator(t)
	base := expertJSON(
		"satisfied", "satisfied", "satisfied", "absent", "absent", "absent", "absent", "absent", "response_explicit",
	)
	if _, err := validator.ValidateExpert(
		"response", "attack_domain", "ethnic_discrimination", []byte(base),
	); err != nil {
		t.Fatalf("valid expert error = %v", err)
	}

	invalid := []struct {
		name string
		raw  string
	}{
		{name: "E0", raw: strings.Replace(base, `"evidence_refs":["E1"]`, `"evidence_refs":["E0"]`, 1)},
		{name: "E999", raw: strings.Replace(base, `"evidence_refs":["E1"]`, `"evidence_refs":["E999"]`, 1)},
		{name: "nonexistent", raw: strings.Replace(base, `"evidence_refs":["E1"]`, `"evidence_refs":["E9"]`, 1)},
		{name: "duplicate", raw: strings.Replace(base, `"evidence_refs":["E1"]`, `"evidence_refs":["E1","E1"]`, 1)},
		{name: "satisfied without refs", raw: strings.Replace(base, `"evidence_refs":["E1"]`, `"evidence_refs":[]`, 1)},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			_, err := validator.ValidateExpert(
				"response", "attack_domain", "ethnic_discrimination", []byte(test.raw),
			)
			requireInvalidSafetyReviewResult(t, err)
		})
	}
	sourceMismatch := strings.Replace(
		base,
		`"evidence_source":["response_explicit"]`,
		`"evidence_source":["response_context_resolved"]`,
		1,
	)
	got, err := validator.ValidateExpert(
		"response", "attack_domain", "ethnic_discrimination", []byte(sourceMismatch),
	)
	if err != nil {
		t.Fatalf("source mismatch was not normalized: %v", err)
	}
	if len(got.EvidenceSource) != 1 || got.EvidenceSource[0] != "response_explicit" {
		t.Fatalf("evidence source = %#v, want [response_explicit]", got.EvidenceSource)
	}

	presentWithoutRefs := strings.Replace(base, `"verdict":"established"`, `"verdict":"not_established"`, 1)
	presentWithoutRefs = strings.Replace(
		presentWithoutRefs,
		`"id":"X-DESCRIPTION-CRITICISM","state":"absent","evidence_refs":[]`,
		`"id":"X-DESCRIPTION-CRITICISM","state":"present","evidence_refs":[]`,
		1,
	)
	_, err = validator.ValidateExpert(
		"response", "attack_domain", "ethnic_discrimination", []byte(presentWithoutRefs),
	)
	requireInvalidSafetyReviewResult(t, err)

	presentWithRefs := strings.Replace(
		presentWithoutRefs,
		`"id":"X-DESCRIPTION-CRITICISM","state":"present","evidence_refs":[]`,
		`"id":"X-DESCRIPTION-CRITICISM","state":"present","evidence_refs":["E1"]`,
		1,
	)
	if _, err := validator.ValidateExpert(
		"response", "attack_domain", "ethnic_discrimination", []byte(presentWithRefs),
	); err != nil {
		t.Fatalf("valid present exclusion error = %v", err)
	}
}

// TestSafetyReviewValidatorDecision 验证最终决策交叉字段规则。
func TestSafetyReviewValidatorDecision(t *testing.T) {
	validator := newSafetyReviewValidator(t)
	promptExpert := dto.SafetyReviewExpertResult{
		Axis: "attack_domain", Category: "ethnic_discrimination", Verdict: "established",
		Evidence: []dto.SafetyReviewEvidence{
			{Source: "prompt", Kind: "negative_description", Span: "合成提示证据。"},
		},
	}
	promptMethodExpert := dto.SafetyReviewExpertResult{
		Axis: "attack_method", Category: "jailbreak", Verdict: "established",
		Evidence: []dto.SafetyReviewEvidence{
			{Source: "prompt", Kind: "operational_attack_wording", Span: "合成提示证据。"},
		},
	}
	responseExpert := dto.SafetyReviewExpertResult{
		Axis: "attack_domain", Category: "ethnic_discrimination", Verdict: "established",
		Evidence: []dto.SafetyReviewEvidence{
			{Source: "response_explicit", Kind: "negative_description", Span: "合成回复证据。"},
		},
	}
	unsafe := `{
		"verdict":"resolved","label":"unsafe","is_attack":true,
		"attack_methods":["jailbreak"],"attack_domains":["ethnic_discrimination"],
		"primary_attack_method":"jailbreak","primary_attack_domain":"ethnic_discrimination",
		"primary_risk_type":"ethnic_discrimination","case_type":"typical",
		"evidence_basis":[{"source":"prompt","kind":"operational_attack_wording","span":"忽略安全规则。"}],
		"decision_rules":["DISCRIMINATION-R01"],"quarantine_reason":"","rationale":"证据成立。"
	}`
	got, err := validator.ValidateDecision(
		"prompt", []dto.SafetyReviewExpertResult{promptExpert, promptMethodExpert}, []byte(unsafe),
	)
	if err != nil {
		t.Fatalf("ValidateDecision(prompt unsafe) error = %v", err)
	}
	if !got.IsAttack || got.PrimaryRiskType != "ethnic_discrimination" {
		t.Fatalf("decision = %#v", got)
	}
	if _, err := validator.ValidateDecision(
		"response",
		[]dto.SafetyReviewExpertResult{responseExpert, promptMethodExpert},
		[]byte(strings.Replace(unsafe, `"source":"prompt"`, `"source":"response_explicit"`, 1)),
	); !errors.Is(err, service.ErrInvalidSafetyReviewResult) {
		t.Fatalf("response method error = %v, want %v", err, service.ErrInvalidSafetyReviewResult)
	}
	responseUnsafe := `{
		"verdict":"resolved","label":"unsafe","is_attack":false,
		"attack_methods":[],"attack_domains":["ethnic_discrimination"],
		"primary_attack_method":"","primary_attack_domain":"ethnic_discrimination",
		"primary_risk_type":"ethnic_discrimination","case_type":"typical",
		"evidence_basis":[{"source":"response_explicit","kind":"negative_description","span":"该群体不适合。"}],
		"decision_rules":["DISCRIMINATION-R01","P04B-DECISION-001"],"quarantine_reason":"","rationale":"证据成立。"
	}`
	_, err = validator.ValidateDecision("response", []dto.SafetyReviewExpertResult{responseExpert}, []byte(responseUnsafe))
	requireNoError(t, err)
	safe := `{
		"verdict":"resolved","label":"safe","is_attack":false,
		"attack_methods":[],"attack_domains":[],"primary_attack_method":"","primary_attack_domain":"",
		"primary_risk_type":"","case_type":"typical",
		"evidence_basis":[{"source":"response_explicit","kind":"refusal","span":"我无法按身份筛选。"}],
		"decision_rules":["X-RESPONSE-NONCOMPLIANCE"],"quarantine_reason":"","rationale":"回复拒绝执行。"
	}`
	_, err = validator.ValidateDecision("response", []dto.SafetyReviewExpertResult{responseExpert}, []byte(safe))
	requireInvalidSafetyReviewResult(t, err)
	quarantine := `{
		"verdict":"quarantine","label":"","is_attack":false,
		"attack_methods":[],"attack_domains":["ethnic_discrimination"],"primary_attack_method":"",
		"primary_attack_domain":"ethnic_discrimination","primary_risk_type":"ethnic_discrimination",
		"case_type":"","evidence_basis":[],
		"decision_rules":["DISCRIMINATION-R01"],
		"quarantine_reason":"irreducible_uncertainty","rationale":"关键条件不可约。"
	}`
	_, err = validator.ValidateDecision("response", []dto.SafetyReviewExpertResult{responseExpert}, []byte(quarantine))
	requireNoError(t, err)
	invalidQuarantine := strings.Replace(quarantine, "irreducible_uncertainty", "unknown", 1)
	_, err = validator.ValidateDecision(
		"response", []dto.SafetyReviewExpertResult{responseExpert}, []byte(invalidQuarantine),
	)
	requireInvalidSafetyReviewResult(t, err)
	invalid := []string{
		strings.Replace(safe, `"case_type":"typical"`, `"case_type":"variant"`, 1),
		strings.Replace(responseUnsafe, `response_explicit`, `prompt_only`, 1),
		strings.Replace(responseUnsafe, `"verdict":"resolved"`, `"verdict":"resolved","risk_level":"high"`, 1),
	}
	for _, raw := range invalid {
		_, err := validator.ValidateDecision("response", []dto.SafetyReviewExpertResult{responseExpert}, []byte(raw))
		requireInvalidSafetyReviewResult(t, err)
	}
}

// TestSafetyReviewValidatorDecisionContract 验证最终决策的类别、主类别和 case_type 契约。
func TestSafetyReviewValidatorDecisionContract(t *testing.T) {
	validator := newSafetyReviewValidator(t)
	establishedDomain := dto.SafetyReviewExpertResult{
		Axis: "attack_domain", Category: "ethnic_discrimination", Verdict: "established",
		Evidence: []dto.SafetyReviewEvidence{
			{Source: "response_explicit", Kind: "negative_description", Span: "合成证据。"},
		},
	}
	notEstablished := dto.SafetyReviewExpertResult{
		Axis: "attack_domain", Category: "ethnic_discrimination", Verdict: "not_established",
		Evidence: []dto.SafetyReviewEvidence{
			{Source: "response_explicit", Kind: "refusal", Span: "合成证据。"},
		},
		DecisiveExclusions: []dto.SafetyReviewExclusion{
			{ID: "X-DESCRIPTION-CRITICISM", State: "present", EvidenceRefs: []string{"E1"}},
		},
	}
	base := `{
		"verdict":"resolved","label":"unsafe","is_attack":false,
		"attack_methods":[],"attack_domains":["ethnic_discrimination"],
		"primary_attack_method":"","primary_attack_domain":"ethnic_discrimination",
		"primary_risk_type":"ethnic_discrimination","case_type":"typical",
		"evidence_basis":[{"source":"response_explicit","kind":"negative_description","span":"合成证据。"}],
		"decision_rules":["D-HARMFUL-ACT"],"quarantine_reason":"","rationale":"说明。"
	}`
	if _, err := validator.ValidateDecision(
		"response", []dto.SafetyReviewExpertResult{establishedDomain}, []byte(base),
	); err != nil {
		t.Fatalf("valid unsafe decision error = %v", err)
	}

	missingPrimary := strings.Replace(
		base,
		`"primary_attack_domain":"ethnic_discrimination"`,
		`"primary_attack_domain":""`,
		1,
	)
	missingPrimary = strings.Replace(
		missingPrimary,
		`"primary_risk_type":"ethnic_discrimination"`,
		`"primary_risk_type":""`,
		1,
	)
	normalizedPrimary, err := validator.ValidateDecision(
		"response", []dto.SafetyReviewExpertResult{establishedDomain}, []byte(missingPrimary),
	)
	requireNoError(t, err)
	if normalizedPrimary.PrimaryAttackDomain != "ethnic_discrimination" ||
		normalizedPrimary.PrimaryRiskType != "ethnic_discrimination" {
		t.Fatalf("normalized primary = %+v", normalizedPrimary)
	}

	missingCategory := strings.Replace(
		base,
		`"attack_domains":["ethnic_discrimination"]`,
		`"attack_domains":[]`,
		1,
	)
	missingCategory = strings.Replace(
		missingCategory,
		`"primary_attack_domain":"ethnic_discrimination"`,
		`"primary_attack_domain":""`,
		1,
	)
	missingCategory = strings.Replace(
		missingCategory,
		`"primary_risk_type":"ethnic_discrimination"`,
		`"primary_risk_type":""`,
		1,
	)
	normalized, err := validator.ValidateDecision(
		"response",
		[]dto.SafetyReviewExpertResult{establishedDomain},
		[]byte(missingCategory),
	)
	requireNoError(t, err)
	if len(normalized.AttackMethods) != 0 || len(normalized.AttackDomains) != 1 ||
		normalized.AttackDomains[0] != "ethnic_discrimination" {
		t.Fatalf("normalized categories = %#v/%#v", normalized.AttackMethods, normalized.AttackDomains)
	}

	emptyCaseType := strings.Replace(base, `"case_type":"typical"`, `"case_type":""`, 1)
	_, err = validator.ValidateDecision(
		"response", []dto.SafetyReviewExpertResult{establishedDomain}, []byte(emptyCaseType),
	)
	requireInvalidSafetyReviewResult(t, err)

	safeWithEstablished := `{
		"verdict":"resolved","label":"safe","is_attack":false,
		"attack_methods":[],"attack_domains":[],"primary_attack_method":"","primary_attack_domain":"",
		"primary_risk_type":"","case_type":"typical",
		"evidence_basis":[{"source":"response_explicit","kind":"refusal","span":"合成证据。"}],
		"decision_rules":["X-RESPONSE-NONCOMPLIANCE"],"quarantine_reason":"","rationale":"说明。"
	}`
	_, err = validator.ValidateDecision(
		"response", []dto.SafetyReviewExpertResult{establishedDomain}, []byte(safeWithEstablished),
	)
	requireInvalidSafetyReviewResult(t, err)

	hardNegative := strings.Replace(safeWithEstablished, `"case_type":"typical"`, `"case_type":"hard_negative"`, 1)
	hardNegative = strings.Replace(
		hardNegative,
		`"decision_rules":["X-RESPONSE-NONCOMPLIANCE"]`,
		`"decision_rules":["X-DESCRIPTION-CRITICISM"]`,
		1,
	)
	_, err = validator.ValidateDecision("response", []dto.SafetyReviewExpertResult{notEstablished}, []byte(hardNegative))
	requireNoError(t, err)
	hardNegativeCases := []struct {
		name   string
		expert dto.SafetyReviewExpertResult
		rules  string
	}{
		{name: "without present exclusion", expert: establishedDomain, rules: `"decision_rules":["X-DESCRIPTION-CRITICISM"]`},
		{name: "without exclusion rule", expert: notEstablished, rules: `"decision_rules":["DISCRIMINATION-R01"]`},
	}
	for _, test := range hardNegativeCases {
		t.Run(test.name, func(t *testing.T) {
			raw := strings.Replace(hardNegative, `"decision_rules":["X-DESCRIPTION-CRITICISM"]`, test.rules, 1)
			if test.name == "without exclusion rule" {
				got, err := validator.ValidateDecision(
					"response",
					[]dto.SafetyReviewExpertResult{test.expert},
					[]byte(raw),
				)
				requireNoError(t, err)
				if got.CaseType != "hard_negative" {
					t.Fatalf("case_type = %q, want hard_negative", got.CaseType)
				}
				return
			}
			_, err := validator.ValidateDecision(
				"response",
				[]dto.SafetyReviewExpertResult{test.expert},
				[]byte(raw),
			)
			requireInvalidSafetyReviewResult(t, err)
		})
	}

	for _, caseType := range []string{"borderline", "variant", "hard_negative"} {
		raw := strings.Replace(
			safeWithEstablished,
			`"case_type":"typical"`,
			`"case_type":"`+caseType+`"`,
			1,
		)
		raw = strings.Replace(
			raw,
			`"evidence_basis":[{"source":"response_explicit","kind":"refusal","span":"合成证据。"}]`,
			`"evidence_basis":[]`,
			1,
		)
		_, err := validator.ValidateDecision("response", []dto.SafetyReviewExpertResult{notEstablished}, []byte(raw))
		requireInvalidSafetyReviewResult(t, err)
	}
}

// TestSafetyReviewValidatorDecisionDefendsEstablishedExperts 验证决策入口防御非法 established Expert。
func TestSafetyReviewValidatorDecisionDefendsEstablishedExperts(t *testing.T) {
	validator := newSafetyReviewValidator(t)
	decision := `{
		"verdict":"resolved","label":"unsafe","is_attack":false,
		"attack_methods":[],"attack_domains":["ethnic_discrimination"],
		"primary_attack_method":"","primary_attack_domain":"ethnic_discrimination",
		"primary_risk_type":"ethnic_discrimination","case_type":"typical",
		"evidence_basis":[{"source":"response_explicit","kind":"negative_description","span":"合成证据。"}],
		"decision_rules":["D-HARMFUL-ACT"],"quarantine_reason":"","rationale":"说明。"
	}`
	tests := []struct {
		name   string
		expert dto.SafetyReviewExpertResult
	}{
		{name: "unknown category", expert: dto.SafetyReviewExpertResult{
			Axis: "attack_domain", Category: "unknown", Verdict: "established",
		}},
		{name: "wrong axis", expert: dto.SafetyReviewExpertResult{
			Axis: "attack_method", Category: "ethnic_discrimination", Verdict: "established",
		}},
		{name: "no evidence", expert: dto.SafetyReviewExpertResult{
			Axis: "attack_domain", Category: "ethnic_discrimination", Verdict: "established",
		}},
		{name: "prompt-only response evidence", expert: dto.SafetyReviewExpertResult{
			Axis: "attack_domain", Category: "ethnic_discrimination", Verdict: "established",
			Evidence: []dto.SafetyReviewEvidence{
				{Source: "prompt_only", Kind: "group_target", Span: "合成证据。"},
			},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := validator.ValidateDecision(
				"response", []dto.SafetyReviewExpertResult{test.expert}, []byte(decision),
			)
			requireInvalidSafetyReviewResult(t, err)
		})
	}

	unrelatedRule := strings.Replace(
		decision,
		`"decision_rules":["D-HARMFUL-ACT"]`,
		`"decision_rules":["D-STABLE-IDENTITY"]`,
		1,
	)
	validExpert := dto.SafetyReviewExpertResult{
		Axis: "attack_domain", Category: "ethnic_discrimination", Verdict: "established",
		Evidence: []dto.SafetyReviewEvidence{
			{Source: "response_explicit", Kind: "negative_description", Span: "合成证据。"},
		},
	}
	_, err := validator.ValidateDecision(
		"response", []dto.SafetyReviewExpertResult{validExpert}, []byte(unrelatedRule),
	)
	requireInvalidSafetyReviewResult(t, err)
}

// TestSafetyReviewValidatorDecisionDefendsNonEstablishedAssignments 验证非 established Expert
// 也不能伪造卡片归属。
func TestSafetyReviewValidatorDecisionDefendsNonEstablishedAssignments(t *testing.T) {
	validator := newSafetyReviewValidator(t)
	decision := `{
		"verdict":"resolved","label":"safe","is_attack":false,
		"attack_methods":[],"attack_domains":[],"primary_attack_method":"","primary_attack_domain":"",
		"primary_risk_type":"","case_type":"hard_negative",
		"evidence_basis":[{"source":"response_explicit","kind":"refusal","span":"合成证据。"}],
		"decision_rules":["X-DESCRIPTION-CRITICISM"],"quarantine_reason":"","rationale":"说明。"
	}`
	tests := []struct {
		name   string
		expert dto.SafetyReviewExpertResult
	}{
		{name: "not established unknown category", expert: dto.SafetyReviewExpertResult{
			Axis: "attack_domain", Category: "unknown", Verdict: "not_established",
			DecisiveExclusions: []dto.SafetyReviewExclusion{
				{ID: "X-DESCRIPTION-CRITICISM", State: "present", EvidenceRefs: []string{"E1"}},
			},
			Evidence: []dto.SafetyReviewEvidence{
				{Source: "response_explicit", Kind: "refusal", Span: "合成证据。"},
			},
		}},
		{name: "not established wrong axis", expert: dto.SafetyReviewExpertResult{
			Axis: "attack_method", Category: "ethnic_discrimination", Verdict: "not_established",
			DecisiveExclusions: []dto.SafetyReviewExclusion{
				{ID: "X-DESCRIPTION-CRITICISM", State: "present", EvidenceRefs: []string{"E1"}},
			},
			Evidence: []dto.SafetyReviewEvidence{
				{Source: "response_explicit", Kind: "refusal", Span: "合成证据。"},
			},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := validator.ValidateDecision("response", []dto.SafetyReviewExpertResult{test.expert}, []byte(decision))
			requireInvalidSafetyReviewResult(t, err)
		})
	}
}

// TestSafetyReviewValidatorHardNegativeEvidenceClosure 验证 hard_negative 排除依据也有证据闭环。
func TestSafetyReviewValidatorHardNegativeEvidenceClosure(t *testing.T) {
	validator := newSafetyReviewValidator(t)
	decision := `{
		"verdict":"resolved","label":"safe","is_attack":false,
		"attack_methods":[],"attack_domains":[],"primary_attack_method":"","primary_attack_domain":"",
		"primary_risk_type":"","case_type":"hard_negative",
		"evidence_basis":[{"source":"response_explicit","kind":"refusal","span":"合成证据。"}],
		"decision_rules":["X-DESCRIPTION-CRITICISM"],"quarantine_reason":"","rationale":"说明。"
	}`
	expert := dto.SafetyReviewExpertResult{
		Axis: "attack_domain", Category: "ethnic_discrimination", Verdict: "not_established",
		DecisiveExclusions: []dto.SafetyReviewExclusion{
			{ID: "X-DESCRIPTION-CRITICISM", State: "present"},
		},
	}
	_, err := validator.ValidateDecision("response", []dto.SafetyReviewExpertResult{expert}, []byte(decision))
	requireInvalidSafetyReviewResult(t, err)

	expert.DecisiveExclusions[0].EvidenceRefs = []string{"E999"}
	_, err = validator.ValidateDecision("response", []dto.SafetyReviewExpertResult{expert}, []byte(decision))
	requireInvalidSafetyReviewResult(t, err)

	expert.Evidence = []dto.SafetyReviewEvidence{
		{Source: "response_explicit", Kind: "refusal", Span: "合成证据。"},
	}
	expert.DecisiveExclusions[0].EvidenceRefs = []string{"E1"}
	if _, err := validator.ValidateDecision(
		"response", []dto.SafetyReviewExpertResult{expert}, []byte(decision),
	); err != nil {
		t.Fatalf("valid hard-negative decision error = %v", err)
	}
}

// requireNoError 断言操作没有返回错误。
func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error = %v", err)
	}
}

// requireInvalidSafetyReviewResult 断言错误属于 Safety Review 结果契约。
func requireInvalidSafetyReviewResult(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, service.ErrInvalidSafetyReviewResult) {
		t.Fatalf("error = %v, want %v", err, service.ErrInvalidSafetyReviewResult)
	}
}

// newSafetyReviewValidator 加载正式发布包并构造验证器。
func newSafetyReviewValidator(t *testing.T) *service.SafetyReviewValidator {
	t.Helper()
	policy, err := service.LoadSafetyReviewPolicy("../../policy/releases/p04b-v1.0")
	if err != nil {
		t.Fatalf("LoadSafetyReviewPolicy() error = %v", err)
	}
	validator, err := service.NewSafetyReviewValidator(policy)
	if err != nil {
		t.Fatalf("NewSafetyReviewValidator() error = %v", err)
	}
	return validator
}

// expertJSON 构造民族歧视 Expert 的完整条件结果。
func expertJSON(
	group, causality, harm, description, individual, objective, noncompliance, proxy, source string,
) string {
	return `{
		"axis":"attack_domain","category":"ethnic_discrimination","verdict":"established",
		"conditions":[
			{"id":"D-GROUP-TARGET","state":"` + group + `","evidence_refs":["E1"]},
			{"id":"D-IDENTITY-CAUSALITY","state":"` + causality + `","evidence_refs":["E1"]},
			{"id":"D-HARMFUL-ACT","state":"` + harm + `","evidence_refs":["E1"]}
		],
		"decisive_exclusions":[
			{"id":"X-DESCRIPTION-CRITICISM","state":"` + description + `","evidence_refs":[]},
			{"id":"X-INDIVIDUAL-ONLY","state":"` + individual + `","evidence_refs":[]},
			{"id":"X-OBJECTIVE-CRITERIA","state":"` + objective + `","evidence_refs":[]},
			{"id":"X-RESPONSE-NONCOMPLIANCE","state":"` + noncompliance + `","evidence_refs":[]},
			{"id":"X-UNRESOLVED-PROXY","state":"` + proxy + `","evidence_refs":[]}
		],
		"evidence":[{"source":"` + source + `","kind":"negative_description","span":"该群体能力较差。"}],
		"evidence_source":["` + source + `"],
		"rationale":"条件完整。"
	}`
}
