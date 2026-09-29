# Policy Optimization Agent Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a resumable, provider-neutral `sendllm policy-optimizer` workflow that turns approved audit evidence and Change Requests into regression-tested, human-approved, immutable Safety Review policy releases.

**Architecture:** A separate CLI composes strict configuration, dedicated `optimization_*` SQLite state, immutable artifact storage, a closed Skill Runtime, bounded OpenAI-compatible model roles, deterministic normalization/stratification/compilation/gates, and atomic release publication. The model may interpret, mine, diagnose, author, criticize, and propose resolution; deterministic code owns trust, counts, patches, compilation, approval verification, and release.

**Tech Stack:** Go 1.24, SQLite, YAML/JSON/JSON Schema, SHA-256, and the repository's existing dependencies. No new dependency is authorized.

## Global Constraints

- Do not begin `feat-026` until `feat-025` is `done` with both live Safety Review rotations passing.
- Read `AGENTS.md`, both approved designs, this plan, `docs/policy-optimization-agent-harness.md`, `feature_list.json`, `progress.md`, and `session-handoff.md` before each feature.
- Read `docs/superpowers/specs/2026-09-07-policy-optimization-implementation-contract.md` completely; it freezes exact interfaces and overrides any less-specific example in this plan.
- Work on exactly one dependency-complete feature. Record valid RED before production implementation and fresh GREEN evidence afterward.
- New Go basenames use `policy_optimizer_`. Do not append optimization logic to Safety Review or legacy Go files.
- The only approved existing production-file edit is the minimal `main.go` dispatch in `feat-036`.
- Keep one local process. Do not add an HTTP service, queue, Redis, cron, plugin loader, native provider Skill, hot reload, or multi-instance coordination.
- Reuse `service.Completer`, completion DTOs, `facade.NewOpenAI`, and compatible stable utilities through adapters. Do not modify old interfaces for convenience.
- Models never normalize authoritative data, compute trusted counts, edit files, compile prompts, approve Gold, evaluate gates, approve releases, or publish versions.
- Automated tests use fake Completers, `httptest`, fake clock/jitter, `t.TempDir()`, and synthetic canaries. Only `feat-038` uses real models and human-approved local assets.
- Never log or commit Prompt/Response, evidence text, raw model output, source datasets, normalized Audit Records, state DBs, candidates, `.env`, or credentials.
- Every feature ends with focused GREEN, affected packages, specified repeats/race, `./init.sh`, scope/security scans, `git diff --check`, and state-file updates.

## Approved File Map

```text
internal/api/cli/policy_optimizer_command.go
internal/api/cli/policy_optimizer_command_test.go
internal/dto/policy_optimizer_contract.go
internal/dto/policy_optimizer_contract_test.go
internal/lib/configs/policy_optimizer_config.go
internal/lib/configs/policy_optimizer_config_test.go
internal/lib/limiter/policy_optimizer_quota.go
internal/lib/limiter/policy_optimizer_quota_test.go
internal/dao/policy_optimizer_schema.sql
internal/dao/policy_optimizer_store.go
internal/dao/policy_optimizer_store_test.go
internal/dao/policy_optimizer_artifact.go
internal/dao/policy_optimizer_artifact_test.go
internal/dao/policy_optimizer_queries.go
internal/dao/policy_optimizer_queries_test.go
internal/service/policy_optimizer_skill.go
internal/service/policy_optimizer_skill_test.go
internal/service/policy_optimizer_context.go
internal/service/policy_optimizer_context_test.go
internal/service/policy_optimizer_call.go
internal/service/policy_optimizer_call_test.go
internal/service/policy_optimizer_audit.go
internal/service/policy_optimizer_audit_test.go
internal/service/policy_optimizer_normalize.go
internal/service/policy_optimizer_normalize_test.go
internal/service/policy_optimizer_stratify.go
internal/service/policy_optimizer_stratify_test.go
internal/service/policy_optimizer_mining.go
internal/service/policy_optimizer_mining_test.go
internal/service/policy_optimizer_pattern.go
internal/service/policy_optimizer_pattern_test.go
internal/service/policy_optimizer_change.go
internal/service/policy_optimizer_change_test.go
internal/service/policy_optimizer_compile.go
internal/service/policy_optimizer_compile_test.go
internal/service/policy_optimizer_gold.go
internal/service/policy_optimizer_gold_test.go
internal/service/policy_optimizer_regression.go
internal/service/policy_optimizer_regression_test.go
internal/service/policy_optimizer_release.go
internal/service/policy_optimizer_release_test.go
internal/service/policy_optimizer_status.go
internal/service/policy_optimizer_status_test.go
internal/service/policy_optimizer_runner.go
internal/service/policy_optimizer_runner_test.go
policy-optimization/skills/manifest.yaml
policy-optimization/skills/<13 fixed skill IDs>/{SKILL.md,input.schema.json,output.schema.json}
policy-optimization/schemas/*.json
policy-optimization/regression/gates/p04b-gate-v1.yaml
policy-optimization/regression/contracts/*.yaml
config/policy-optimizer.example.yaml
scripts/verify-policy-optimizer.sh
scripts/verify-policy-optimizer-scope.sh
policy_optimizer_main_test.go
```

Do not create a new package layer. The angle-bracket entry means the exact 13 IDs from Design section 6.1, not arbitrary plugin directories.

## Task 1: Strict config, contracts, and scope lock (`feat-026`)

**Interfaces:**

```go
func LoadPolicyOptimizer(path string) (*PolicyOptimizerConfig, error)
func (c *PolicyOptimizerConfig) Validate() error
func (c *PolicyOptimizerConfig) SemanticFingerprint(files map[string][]byte) (string, error)
func ValidateAuditRecord(record dto.PolicyOptimizerAuditRecord) error
func ValidateChangeRequest(request dto.PolicyOptimizerChangeRequest) error
```

- [ ] Capture baseline HEAD/status, `./init.sh`, and protected existing-Go hashes. The scope script permits only `policy_optimizer_*.go` plus the later `main.go` exception.
- [ ] Write table-driven RED tests for every required config section, unknown keys, config-relative containment, exact `base_version`/`target_version` grammar and candidate derivation, exact role/profile sets, shared API env acceptance, critic-family separation, 70/20/10 sum, batch bounds 30/50/100, retry values 3/1/1, context limits, release paths, and semantic/runtime fingerprint separation.
- [ ] Run `go test ./internal/lib/configs ./internal/dto -run '^TestPolicyOptimizer(Config|Contract)' -count=1 -v`; RED must be missing optimizer types/loader, not a fixture error.
- [ ] Define closed named enums and structs for source types, authority, iteration/stage states, scenes, comparisons, artifacts, patterns, diagnoses, proposals, critiques, Change Sets, approvals, regressions, and releases. Reject unknown JSON/YAML fields at every external boundary.
- [ ] Implement the smallest strict config/contract validation. Copy retained slices/maps; resolve paths relative to config/package; omit secrets and runtime-only values from the semantic fingerprint.
- [ ] Create the example config and all formal JSON Schemas. A schema-enumeration parity test must compare each closed Go enum with its Schema enum.
- [ ] Run focused twice, affected packages, race, `./init.sh`, scope/security, and `git diff --check`; persist exact exits.

## Task 2: Dedicated SQLite and immutable Artifact Store (`feat-027`)

**Interfaces:**

```go
func OpenPolicyOptimizer(ctx context.Context, path string) (*PolicyOptimizerStore, error)
func (s *PolicyOptimizerStore) EnsureIteration(ctx context.Context, iteration dto.PolicyOptimizerIteration) error
func (s *PolicyOptimizerStore) ClaimSkillRun(ctx context.Context, key dto.PolicyOptimizerRunKey) (dto.PolicyOptimizerSkillRun, bool, error)
func (s *PolicyOptimizerStore) CompleteSkillRun(ctx context.Context, result dto.PolicyOptimizerSkillResult) error
func (s *PolicyOptimizerStore) RecoverRunning(ctx context.Context, iterationID string) (int64, error)
func (s *PolicyOptimizerStore) ReadStatus(ctx context.Context, iterationID string) (dto.PolicyOptimizerStatus, error)
func (a *PolicyOptimizerArtifactStore) Put(ctx context.Context, artifact dto.PolicyOptimizerArtifactWrite) (dto.PolicyOptimizerArtifact, error)
```

- [ ] RED tests require exactly the 11 `optimization_*` tables listed in Design section 16, foreign keys, unique iteration/source/record/run/version keys, closed-state checks, attempt counters, parent-hash JSON, and no writes to legacy/Safety Review tables.
- [ ] Run `go test ./internal/dao -run '^TestPolicyOptimizer(Store|Artifact)' -count=1 -v`; RED must show the optimizer store/schema is absent.
- [ ] Embed only `policy_optimizer_schema.sql`; configure WAL, foreign keys, busy timeout, and one writer. Store restricted payload only by artifact reference, never duplicated in public status columns.
- [ ] Implement write-temp, file `Sync`, close, atomic rename, then DB index transaction. Reject path escape, hash mismatch, overwrite, missing parent, duplicate active artifact, and metadata/body mismatch.
- [ ] Implement mode-specific legal iteration transitions and `pending/running/retry_wait/succeeded/terminal_failed` skill runs. Reopen converts only `running` to `pending`; hash-matched success is reused, while changed semantic input is rejected.
- [ ] Inject failures before write, after partial write, after sync, before rename, and before DB commit. Reopen after each transition and prove no dangling active reference or overwritten artifact.
- [ ] Run focused `-count=50`, DAO tests, race `-count=10`, full gates, and record evidence.

## Task 3: Closed Skill Runtime, Context Builder, and model call policy (`feat-028`)

**Interfaces:**

```go
type PolicyOptimizerSkillExecutor interface {
	Execute(context.Context, dto.PolicyOptimizerSkillInput) (dto.PolicyOptimizerSkillOutput, error)
}
func LoadPolicyOptimizerSkills(root, manifest string) (*PolicyOptimizerSkillRegistry, error)
func BuildPolicyOptimizerContext(input PolicyOptimizerContextInput) (dto.PolicyOptimizerContextPackage, error)
func NewPolicyOptimizerCaller(cfg PolicyOptimizerCallerConfig) (*PolicyOptimizerCaller, error)
func (c *PolicyOptimizerCaller) Call(ctx context.Context, req PolicyOptimizerCallRequest) (PolicyOptimizerCallResult, error)
```

- [ ] RED tests verify all 13 fixed Skill IDs, exact executor kind, required headings/files/Schemas, duplicate/unknown/missing IDs, optional examples, unknown-file reporting, and prohibition of filesystem executables/native provider Skills.
- [ ] Context RED tests prove role-specific least context, exact artifact hashes, permissions, no chat memory, no silent truncation, deterministic batch split, indivisible overflow failure, and payload exclusion from status/errors.
- [ ] Caller RED tests cover 3 transient attempts/profile, one format repair, one refusal reprompt, immediate content-rejection fallback, auth/model circuit, persisted attempts, cancellation, per-skill and shared-profile quota, and critic/author family enforcement.
- [ ] Run `go test ./internal/service ./internal/lib/limiter -run '^TestPolicyOptimizer(Skill|Context|Call|Quota)' -count=1 -v`; expected RED is missing registry/builder/caller.
- [ ] Implement deterministic executor dispatch by manifest enum and explicit constructors. Model-backed executors use `service.Completer`; deterministic executors never call HTTP and still validate input/output and persist artifacts.
- [ ] Use system = generic safety boundary + Skill instructions, developer = task/policy/permissions/Schema, user = current batch/case. Request no chain-of-thought and persist only validated structured output as restricted artifacts.
- [ ] Blocking fakes prove exact concurrency ceilings, downstream priority, permit release, bounded cancellation, and no request receives another run's context. Repeat 100 normal, 100 `GOMAXPROCS=1`, race 20.

## Task 4: Audit Package inspection and approved mappings (`feat-029`)

**Interfaces:**

```go
func InspectAuditPackage(ctx context.Context, cfg AuditInspectConfig) (dto.PolicyOptimizerInspection, error)
func LoadAuditPackage(path string) (*PolicyOptimizerAuditPackage, error)
func ApproveAuditMapping(candidatePath, outputPath, approver string, approvedAt time.Time) error
func ValidateApprovedMapping(mapping dto.PolicyOptimizerMapping, source dto.PolicyOptimizerSource) error
```

- [ ] RED fixtures cover strict manifest, JSONL RFC 6901 pointers, exact case-sensitive CSV/Markdown columns, one-table Markdown, fixed `each_record`, all source types, one/many sources, expected count/hash, package path containment, malformed records, declared trust, comparison semantics, analysis permissions, and custom input. Reject defaults, transformations, expressions, scripts, recursive selectors, and arbitrary Markdown prose.
- [ ] Unknown/custom layouts must emit a candidate interpretation with suitable/unsuitable analyses and ambiguities, then stop at `awaiting_mapping_approval`; they may not normalize.
- [ ] Mapping approval requires `status=approved`, approver, timestamp, exact source SHA-256, explicit field paths, actor authority, scene mapping, and comparison mapping. Changed source bytes invalidate it.
- [ ] Run `go test ./internal/service -run '^TestPolicyOptimizerAudit' -count=1 -v`; expected RED is missing package loader/inspection.
- [ ] Implement deterministic structural inspection first, then model interpreter only where needed. The model cannot set approval fields; `ApproveAuditMapping` is an explicit human CLI operation added later.
- [ ] Canary fixtures prove errors/status/inspection summaries expose field names/counts/hashes but no source payload values.
- [ ] Run focused, malformed-fixture fuzz smoke, race, and all gates.

## Task 5: Canonical normalization and disagreement selection (`feat-030`)

**Interfaces:**

```go
func NormalizeAuditSource(ctx context.Context, cfg PolicyOptimizerNormalizeConfig) (PolicyOptimizerNormalizeStats, error)
func MineDisagreements(records []dto.PolicyOptimizerAuditRecord, policy PolicyOptimizerSelectionPolicy) ([]string, error)
func ImportQualityEvents(r io.Reader) ([]dto.PolicyOptimizerQualityEvent, error)
```

- [ ] RED tables map every supported source format to the exact Canonical Audit Record, preserving unknown source fields under `metadata.source_fields` and every judgment's actor/authority/source reference.
- [ ] Reject missing provenance, unstable/duplicate IDs, impossible comparison indices, candidate judgment used as Gold, payload absent for a payload-required permission, mapping/source hash mismatch, and nondeterministic ordering.
- [ ] Quality-event tests accept only the sanitized Safety Review contract and require a separately declared payload source before payload analysis. Selection uses metadata/disagreement only.
- [ ] Run `go test ./internal/service -run '^TestPolicyOptimizer(Normalize|Disagreement|QualityEvent)' -count=1 -v`; expected RED is missing deterministic normalizer.
- [ ] Implement streaming CSV/JSONL/Markdown adapters behind a closed format switch. Hash canonical records with `json.Decoder.UseNumber`; persist payload in restricted artifacts and indexed metadata in SQLite.
- [ ] Prove identical input produces byte-identical normalized artifacts and order; one source-byte mutation invalidates the approved mapping and produces zero normalized records for that source.
- [ ] Run focused `-count=20`, 5-second parser fuzz, race, and all gates.

## Task 6: Deterministic stratification and Local Error Mining (`feat-031`)

**Interfaces:**

```go
func StratifyAuditRecords(records []dto.PolicyOptimizerAuditRecordRef, cfg PolicyOptimizerBatchingConfig, iterationID string) ([]dto.PolicyOptimizerBatch, error)
func RunLocalMining(ctx context.Context, cfg PolicyOptimizerLocalMiningConfig) (PolicyOptimizerMiningStats, error)
```

- [ ] RED tests assign 10,000 records exactly once with deterministic SHA-256 seeding, exact 70/20/10 integer counts, all grouping dimensions, stable order, configurable percentages summing 100, and no label-total isolation.
- [ ] Test target 50/min 30/max 100, remainders 1/29/30/99/101, and datasets 0/1/29. Only a whole dataset below 30 may produce a batch below 30.
- [ ] Local output validation requires cited existing record IDs, at least two records or `singleton_candidate`, representative/counterexample subsets, allowed causes, candidate confidence, and limitations. It rejects counts invented by a model.
- [ ] Run `go test ./internal/service -run '^TestPolicyOptimizer(Stratify|LocalMining)' -count=1 -v`; expected RED is missing stratifier/miner.
- [ ] Implement deterministic batching, bounded workers, durable claims, resume reuse by input/context hash, and terminal artifacts for exhausted batches. Unrelated batches continue; dependent merge remains blocked if any required batch terminally fails.
- [ ] Interrupt after a known subset succeeds, reopen, and prove succeeded calls are not repeated and every remaining record is processed once. Repeat 100 and race 20.

## Task 7: Global merge, case adjudication, and diagnosis (`feat-032`)

**Interfaces:**

```go
func RunGlobalMerge(ctx context.Context, cfg PolicyOptimizerGlobalMergeConfig) (PolicyOptimizerMergeStats, error)
func AttachPatternCases(pattern dto.PolicyOptimizerGlobalPattern, records PolicyOptimizerRecordReader, seed string) (dto.PolicyOptimizerGlobalPattern, error)
func RunCaseAdjudication(ctx context.Context, cfg PolicyOptimizerAdjudicationConfig) (PolicyOptimizerAdjudicationStats, error)
func RunPolicyDiagnosis(ctx context.Context, cfg PolicyOptimizerDiagnosisConfig) (PolicyOptimizerDiagnosisStats, error)
```

- [ ] RED tests prove Global Merge receives Local Pattern artifacts, never the full raw corpus; code independently recomputes coverage/batch/source/model distributions and rejects invented IDs/counts.
- [ ] Every Global Pattern must attach 3-10 representatives, 2-5 deterministic random cases, and all known boundary/counterexamples up to 10. Tests cover populations smaller than each bound without duplicate references.
- [ ] Adjudication validates policy/rules, scene evidence ownership, full condition/exclusion matrix, ambiguity, current-policy result, and candidate Gold status; it cannot approve Gold.
- [ ] Diagnosis batches 5-20 patterns and permits only the 12 causes in Design section 9. It must cite artifacts and preserve alternative explanations.
- [ ] Run `go test ./internal/service -run '^TestPolicyOptimizer(Global|Adjudication|Diagnosis)' -count=1 -v`; expected RED is missing stage runners.
- [ ] Implement model proposals plus deterministic reference/count verification. Load raw payload only for attached cases, not merge summaries or status.
- [ ] Mutation checks must fail when a model adds a record, changes a count, omits a counterexample, uses Prompt-only Response evidence, or marks Candidate Gold approved.

## Task 8: Change Requests, Proposal, Critic, and Resolver (`feat-033`)

**Interfaces:**

```go
func LoadChangeRequests(paths []string) ([]dto.PolicyOptimizerChangeRequest, error)
func RunRuleAuthor(ctx context.Context, cfg PolicyOptimizerAuthorConfig) ([]dto.PolicyOptimizerProposal, error)
func RunPolicyCritic(ctx context.Context, cfg PolicyOptimizerCriticConfig) ([]dto.PolicyOptimizerCritique, error)
func ResolvePolicyChanges(ctx context.Context, cfg PolicyOptimizerResolveConfig) (dto.PolicyOptimizerChangeSet, error)
func ValidateChangeSet(input PolicyOptimizerChangeSetValidation) error
```

- [ ] RED tests cover all source/authority enums and authority order: human/security > approved Gold > automated/third-party > observation. Higher authority cannot be silently overridden.
- [ ] Equal high-authority conflict, Schema violation, internal contradiction, impossible target, missing evidence reference, and required-condition/exclusion removal without Human Directive must create exact blocked conflicts.
- [ ] Proposal validation requires affected deterministic counts/distributions, current rule excerpts by hash, normalized patch operations, addressed patterns, regression risks/cases/contracts, and non-goals.
- [ ] Critic must use a different family from author/resolver and return `accept/revise/block`; `accept` is not approval. A `block` prevents compilable status.
- [ ] Resolver may emit only closed patch operations and exact JSON Pointers against the base. Deterministic validation reapplies authority, verifies references, and rejects nonempty `blocked_conflicts`.
- [ ] Run `go test ./internal/service -run '^TestPolicyOptimizer(ChangeRequest|Proposal|Critic|Resolver)' -count=1 -v`; expected RED is missing authority/resolution behavior.
- [ ] Test modes A/B/C: mined proposals only, proposals plus requests, and requests only. Direct compile makes zero mining/adjudication/diagnosis/author calls but still runs a different-family Critic over the requested change bundle before Resolver.
- [ ] Run focused `-count=20`, critic-family mutation, race, and all gates.

## Task 9: Deterministic candidate Policy and Prompt Compiler (`feat-034`)

**Interfaces:**

```go
func ApplyPolicyChangeSet(baseDir, candidateDir string, set dto.PolicyOptimizerChangeSet) (dto.PolicyOptimizerCandidate, error)
func CompilePolicyPrompts(candidateDir string, cfg PolicyOptimizerCompileConfig) (dto.PolicyOptimizerCompileManifest, error)
func VerifyPolicyBundle(bundleDir string) (dto.PolicyOptimizerBundleManifest, error)
```

- [ ] RED tests cover add/replace/remove exact YAML field, add versioned card/decision/contract, invalid pointer, unknown operation, base-hash mismatch, path escape, duplicate ID, and any mutation of base/released bytes.
- [ ] Compiler tests render Judge A/B, Router, Expert, Arbiter, and refusal prompts from policy assets, role templates, Schemas, coverage maps, and approved examples only.
- [ ] Compilation fails for omitted enabled rule/condition/exclusion/decision ID, unrecognized policy sentence, unknown source asset, size-budget overflow, Schema mismatch, unstable ordering, or differing bytes across two identical runs.
- [ ] Run `go test ./internal/service -run '^TestPolicyOptimizer(Apply|Compile|Bundle)' -count=1 -v`; expected RED is missing deterministic compiler.
- [ ] Implement exact patch application over a copied base into a new candidate directory. Compiler performs transformation/organization/deduplication only; model wording remains a proposal until source policy accepts it.
- [ ] Emit a manifest containing compiler/release/source versions, all input/output hashes/sizes, role coverage, and aggregate hash. Verify it through the same released-bundle contract used by Safety Review.
- [ ] Mutation proof: alter one base byte, one compiled prompt, one coverage ID, and one Schema; each must fail while the untouched candidate remains byte-identical across runs.

## Task 10: Gold lifecycle and Regression Evaluator (`feat-035`)

**Interfaces:**

```go
func CreateCandidateGold(input dto.PolicyOptimizerCandidateGoldInput) (dto.PolicyOptimizerGoldRecord, error)
func ApproveGold(candidate dto.PolicyOptimizerGoldRecord, approval dto.PolicyOptimizerGoldApproval) (dto.PolicyOptimizerGoldRecord, error)
func PromoteCoreGold(approved dto.PolicyOptimizerGoldRecord, history dto.PolicyOptimizerGoldHistory, approval dto.PolicyOptimizerGoldApproval) (dto.PolicyOptimizerGoldRecord, error)
func RunPolicyRegression(ctx context.Context, cfg PolicyOptimizerRegressionConfig) (dto.PolicyOptimizerRegressionReport, error)
func EvaluateP04BGates(report dto.PolicyOptimizerRegressionReport, gate dto.PolicyOptimizerGatePolicy) error
```

- [ ] RED tests enforce Candidate -> Approved -> Core, explicit reviewer/source/policy/rule/decision hashes, at least three successful releases, no unresolved challenge, explicit promotion approval, and no Candidate as truth.
- [ ] Regression suites preserve Core Gold, Approved Gold, Typical Safe/Unsafe, Hard Negative, Borderline, Previous Error, and deterministic Random Sample. Deduplicate a row within each gate population while retaining suite membership.
- [ ] Contract tests enforce immutable versioning, minimum cases, exact expectations, and Human Directive for supersession.
- [ ] Run base and candidate through fresh frozen Safety Review task dirs and both A/B rotations. Read outputs with an independent parser; never trust workflow-reported metrics.
- [ ] RED gate tests cover Core Safe false Unsafe >0, Hard Negative false Unsafe >0, old contract regression, Unsafe resolved Safe >0, Unsafe resolved <18/20, Unsafe quarantine >2/20, hidden false Unsafe >0, hidden quarantine >12/50, claimed pattern non-improvement, unaccounted stage failure, and one failed rotation.
- [ ] Run `go test ./internal/service -run '^TestPolicyOptimizer(Gold|Regression|Gate)' -count=1 -v`; expected RED is missing lifecycle/evaluator.
- [ ] Anti-cheating mutation changes expected resolved Unsafe 18 to 19 and must fail copied real-shaped evidence; restore 18 and pass. Threshold changes require a separate Human Directive and gate-policy version.

## Task 11: CLI modes, workflow graph, status, and recovery (`feat-036`)

**Interfaces:**

```go
func RunPolicyOptimizer(ctx context.Context, args []string, stdout, stderr io.Writer) int
func NewPolicyOptimizerRunner(cfg PolicyOptimizerRunnerConfig) (*PolicyOptimizerRunner, error)
func (r *PolicyOptimizerRunner) Run(ctx context.Context, mode dto.PolicyOptimizerMode) (dto.PolicyOptimizerRunStats, error)
```

- [ ] RED CLI tests cover `validate`, `inspect`, `mapping-approve`, `analyze`, `compile`, `compile --preview`, `regression`, `gold-approve`, `gold-promote`, `approve`, `release`, `status`, and `status --watch`; strict required/forbidden flag combinations; exits 0/1/3/130; and zero network in `validate/status`.
- [ ] Add only a minimal `main.go` branch for `args[0] == "policy-optimizer"`; do not refactor legacy or Safety Review dispatch.
- [ ] Workflow tests lock the exact mode graphs. Analyze A runs source through Regression; Analyze B additionally resolves requests; Compile C skips source/mining/adjudication/diagnosis/author but retains Critic and Resolver; zero effective changes terminate `no_change` without a candidate; preview cannot regress/approve/release; every release path requires Regression.
- [ ] Preflight occurs after iteration metadata/source snapshots may exist but before any model skill claim. Failure leaves zero model claims and zero candidate policy mutations.
- [ ] Status reports stage counts, rates, ETA, waiting approval, aggregate safe errors, hashes, and next operator action. TTY refreshes one panel; non-TTY emits one compact interval; no payload appears.
- [ ] SIGINT stops claims, drains to configured timeout, commits completed artifacts, resets recoverable running work, writes atomic summary, and exits 130. Resume reuses only hash-matched successes.
- [ ] Run `go test . ./internal/api/cli ./internal/service -run '^TestPolicyOptimizer(CLI|Runner|Status|Recovery)' -count=1 -v`, repeated 100, race 20, legacy tests, and all gates.

## Task 12: Human approval, atomic release, and Safety Review integration (`feat-037`)

**Interfaces:**

```go
func CreatePolicyApproval(candidate dto.PolicyOptimizerCandidate, regression dto.PolicyOptimizerRegressionReport, critic dto.PolicyOptimizerCritique, approver string, at time.Time) (dto.PolicyOptimizerApproval, error)
func ReleasePolicy(ctx context.Context, cfg PolicyOptimizerReleaseConfig) (dto.PolicyOptimizerRelease, error)
```

- [ ] RED tests bind approval to candidate/regression/critic hashes, approver and timestamp. A one-byte change invalidates approval. Models and `analyze/compile/regression` cannot invoke approval internally.
- [ ] Release rejects blocked Change Set, critic `block`, failed/missing gate, missing/mismatched approval, reused version, invalid manifest, changed candidate, escaping path, and target collision.
- [ ] Atomic publication writes a sibling staging directory, verifies all hashes, syncs files/directories, renames once, and refuses overwrite/delete/reuse. Failure injection at every boundary leaves no partial released version.
- [ ] Generate release manifest and changelog from deterministic artifacts. Base and candidate bytes remain unchanged.
- [ ] A fresh `sendllm safety-review validate` must consume the new release by versioned path/hash. Safety Review state DB is never opened for optimization writes.
- [ ] Run `go test ./internal/service ./internal/api/cli -run '^TestPolicyOptimizer(Approval|Release|SafetyReviewIntegration)' -count=1 -v`; expected RED is missing approval/release behavior.
- [ ] Run focused `-count=50`, race `-count=10`, `scripts/verify-policy-optimizer.sh`, scope/security, and all gates.

## Task 13: Real optimization acceptance and final handoff (`feat-038`)

- [ ] Run the full fake E2E over CSV, JSONL, Markdown, quality events plus separately declared payloads, all three modes, interruption/resume, critic block, approval invalidation, regression failure, and atomic release failure.
- [ ] Run the 10,000-row deterministic test and independently verify exact one-time assignment, 70/20/10 totals, and legal batch sizes.
- [ ] Read local model configuration without printing secrets. Real preflight must call every configured model/role, verify structured mode, and leave zero semantic claims on failure.
- [ ] Run one real P04-B optimization iteration using human-approved local Audit/Gold assets. Missing assets, key, model, or approver action means `feat-038=blocked`, never skipped or replaced by fake evidence.
- [ ] Run base/candidate regression in both rotations and independently verify every Design section 14 gate, zero terminal batch failure, exact artifact hashes, and no Candidate Gold used as truth.
- [ ] Perform explicit human `approve`, publish one new immutable release, and validate it with a fresh Safety Review process. Record only aggregate counts, IDs, paths, versions, and hashes.
- [ ] Run `scripts/verify-policy-optimizer.sh`, `scripts/verify-safety-review.sh`, `./init.sh`, repeated race, `git diff --check`, scope scan, secret/payload canary scan, sensitive-artifact scan, and manual review of every changed file.
- [ ] Update `feature_list.json`, `progress.md`, and `session-handoff.md` with exact commands/exits, aggregate real metrics, release hash/version, residual risks, and the next operator action. Mark complete only when every prior feature is done.

## Required Evidence Template

```text
Feature: feat-NNN
Baseline command and exit:
RED command, exit, and exact missing behavior:
Files changed:
Focused GREEN command and exit:
Repeat/concurrency command and exit:
Race command and exit:
Full ./init.sh command and exit:
Scope/security commands and exits:
Artifact/state invariants checked:
Real-model aggregate evidence (feat-038 only):
Human action evidence (when required):
Known residual risk:
Next unblocked feature:
```

Cached output, generic “tests pass,” skipped live checks, model-reported counts without independent recomputation, tests written after implementation, or approval performed by an automated workflow are insufficient evidence.
