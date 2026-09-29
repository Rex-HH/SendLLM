# Safety Review Pipeline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the approved precision-first, four-model-family, resumable `sendllm safety-review` pipeline for the P04-B pilot without changing legacy command behavior.

**Architecture:** A new CLI boundary composes independent OpenAI-compatible clients, frozen YAML policy, a dedicated SQLite store, five role pools, strict role validators, and atomic exporters. A, B, and Router run in parallel for every item; the Safe shortcut requires both complete Safe signals plus a complete zero-candidate Router result. One Expert call validates each category, and Arbiter can select only categories established by Experts. Reuse only `service.Completer`, completion DTOs, `facade.NewOpenAI`, and compatible utilities.

**Tech Stack:** Go 1.24 and the repository's existing dependencies. No new dependency is authorized.

**Revision:** 2026-09-07. Router is mandatory for every item, P04-B V1 omits `risk_level`, case types are rule-derived, and live Unsafe Gold cannot resolve Safe.

## Global Constraints

- Read `AGENTS.md`, the approved design, this plan, `feature_list.json`, `progress.md`, and `session-handoff.md` before each feature.
- Work on exactly one unblocked feature. Record RED before implementation and fresh GREEN evidence afterward.
- New Go files use the `safety_review_*.go` prefix. Every function has a concise Chinese comment.
- Only a minimal `main.go` dispatch is pre-approved. Every other existing `.go` file is protected and needs user approval before editing.
- Do not weaken/delete/skip tests; do not add Pair, batch model calls, services, queues, plugins, hot reload, or dependencies.
- Automated tests use fake `Completer`, fake clock/jitter, `httptest`, and temporary SQLite. They do not use paid models.
- Required real tests cannot pass by skipping. Missing data, key, model, or metric means blocked.
- Never log or commit credentials, datasets, Prompt/Response, evidence spans, raw model output, SQLite state, run directories, or exports.
- Every feature closes with focused tests, repeated concurrency tests where applicable, race, `./init.sh`, scope/security scans, and `git diff --check`.

## Approved File Map

```text
internal/api/cli/safety_review_command.go
internal/api/cli/safety_review_command_test.go
internal/dto/safety_review_contract.go
internal/dto/safety_review_contract_test.go
internal/lib/configs/safety_review_config.go
internal/lib/configs/safety_review_config_test.go
internal/lib/limiter/safety_review_quota.go
internal/lib/limiter/safety_review_quota_test.go
internal/dao/safety_review_schema.sql
internal/dao/safety_review_store.go
internal/dao/safety_review_store_test.go
internal/dao/safety_review_import.go
internal/dao/safety_review_import_test.go
internal/dao/safety_review_stages.go
internal/dao/safety_review_stages_test.go
internal/dao/safety_review_queries.go
internal/dao/safety_review_queries_test.go
internal/service/safety_review_policy.go
internal/service/safety_review_policy_test.go
internal/service/safety_review_prompt.go
internal/service/safety_review_prompt_test.go
internal/service/safety_review_validator.go
internal/service/safety_review_validator_test.go
internal/service/safety_review_models.go
internal/service/safety_review_models_test.go
internal/service/safety_review_preflight.go
internal/service/safety_review_preflight_test.go
internal/service/safety_review_call.go
internal/service/safety_review_call_test.go
internal/service/safety_review_transition.go
internal/service/safety_review_transition_test.go
internal/service/safety_review_scheduler.go
internal/service/safety_review_scheduler_test.go
internal/service/safety_review_judges.go
internal/service/safety_review_judges_test.go
internal/service/safety_review_router.go
internal/service/safety_review_router_test.go
internal/service/safety_review_expert.go
internal/service/safety_review_expert_test.go
internal/service/safety_review_arbiter.go
internal/service/safety_review_arbiter_test.go
internal/service/safety_review_export.go
internal/service/safety_review_export_test.go
internal/service/safety_review_status.go
internal/service/safety_review_status_test.go
internal/service/safety_review_eval.go
internal/service/safety_review_eval_test.go
internal/service/safety_review_runner.go
internal/service/safety_review_runner_test.go
policy/releases/p04b-v1.0/release.yaml
policy/releases/p04b-v1.0/policy/common.yaml
policy/releases/p04b-v1.0/policy/rules/attack_method/*.yaml
policy/releases/p04b-v1.0/policy/rules/attack_domain/*.yaml
policy/releases/p04b-v1.0/policy/examples/p04b-development.jsonl
policy/releases/p04b-v1.0/policy/decisions/P04-B.md
policy/releases/p04b-v1.0/prompts/*.txt
policy/releases/p04b-v1.0/schemas/*.json
config/safety-review.example.yaml
config/safety-review-eval.example.yaml
scripts/verify-safety-review.sh
scripts/verify-safety-review-scope.sh
safety_review_main_test.go
```

Do not add another top-level directory under `internal/`.

## Task 1: Strict config and scope lock (`feat-015`)

**Produces:**

```go
func LoadSafetyReview(path string) (*SafetyReviewConfig, error)
func (c *SafetyReviewConfig) Validate() error
func (c *SafetyReviewConfig) SemanticFingerprint(snapshotFiles map[string][]byte) (string, error)
```

- [ ] Record `git rev-parse HEAD`, `git status --short`, and baseline `./init.sh`.
- [ ] Create a SHA-256 manifest for tracked existing `.go` files except `main.go`. `verify-safety-review-scope.sh` rejects protected-file changes and new Go basenames without `safety_review_`.
- [ ] RED tests cover every Design section 7 field, unknown YAML, closed role/profile sets, fallback family difference, references, scenes, positive limits, output containment, non-empty API env names, shared API env acceptance across profiles, relative paths, and semantic/runtime fingerprint separation.
- [ ] Run `go test ./internal/lib/configs -run '^TestSafetyReview' -count=1 -v`; expected RED is undefined loader/types.
- [ ] Implement `yaml.Decoder.KnownFields(true)`, copied maps/slices, config-relative paths, environment-only secrets, canonical hashes, and exactly the approved runtime exclusions.
- [ ] Mutate one semantic and one runtime value in tests; only the semantic mutation changes the hash.
- [ ] Run focused twice, race, `./init.sh`, scope, and diff gates; store exact exits.

## Task 2: Policy, prompts, contracts, validators (`feat-016`)

**Produces:**

```go
func LoadSafetyReviewPolicy(bundleDir string) (*SafetyReviewPolicy, error)
func BuildSafetyReviewMessages(role SafetyReviewRole, input SafetyReviewRoleInput) ([]dto.Message, error)
func NewSafetyReviewValidator(policy *SafetyReviewPolicy) (*SafetyReviewValidator, error)
func (v *SafetyReviewValidator) ValidateJudgment(scene string, raw []byte) (dto.SafetyReviewJudgment, error)
func (v *SafetyReviewValidator) ValidateRoute(scene string, raw []byte) (dto.SafetyReviewRoute, error)
func (v *SafetyReviewValidator) ValidateExpert(scene, axis, category string, raw []byte) (dto.SafetyReviewExpertResult, error)
func (v *SafetyReviewValidator) ValidateDecision(scene string, experts []dto.SafetyReviewExpertResult, raw []byte) (dto.SafetyReviewDecision, error)
```

- [ ] RED tests cover strict release-manifest loading and hashes, immutable bundle path containment, IDs/references, scenes, candidate caps, factual/non-verdict Router features, feature references, exact Expert condition/exclusion IDs, Expert truth tables, primary membership, legacy projection, Response evidence ownership, formal `case_type` rules, V1 `risk_level` rejection, Safe clearing, and quarantine reasons.
- [ ] Run `go test ./internal/dto ./internal/service -run '^TestSafetyReview(Policy|Prompt|Validator|Contract)' -count=1 -v`; expected RED is missing contracts.
- [ ] Implement named types and exact Design section 5 contracts. Empty slices encode as `[]`.
- [ ] Create the trusted immutable `p04b-v1.0` bootstrap release with cards for eight specific discrimination categories, `other_discrimination`, `ethnic_hatred`, and six methods. Exclude `financial_domain_attack`; put all stable employment identities only in `occupation_discrimination`; encode `DISCRIMINATION-R01`, `P04B-DECISION-001`, formal Case Type Policy, and approved P04-B boundaries.
- [ ] Build development examples around the human-confirmed P04-B cases with `source=human_reviewed`; add `source=synthetic` cases only for uncovered branches. Checked compiled prompts state blindness, Schema, evidence limits, and no chain-of-thought. Router prohibits verdict-like features; Expert receives one card; Arbiter prohibits voting and new categories.
- [ ] Generate `release.yaml` with release/source/compiler versions, every policy/prompt/Schema path, byte size, SHA-256, and aggregate bundle hash. Tests prove every enabled condition/exclusion/decision ID appears in the required compiled-role coverage map and two manifest-verification passes produce the same aggregate hash.
- [ ] Mutation proof: accepting a Response method, Expert established with an unknown required condition, and Expert established with an unknown decisive exclusion must each fail a targeted test; restore changes.
- [ ] Run focused, race, full, policy, scope, and diff gates.

## Task 3: Dedicated SQLite state and import (`feat-017`)

**Produces:**

```go
func OpenSafetyReview(ctx context.Context, path string) (*SafetyReviewStore, error)
func (s *SafetyReviewStore) EnsureTask(ctx context.Context, task SafetyReviewTask) error
func (s *SafetyReviewStore) ImportJSONL(ctx context.Context, taskID string, r io.Reader) (SafetyReviewImportStats, error)
func (s *SafetyReviewStore) RecoverRunning(ctx context.Context, taskID string) (int64, error)
func (s *SafetyReviewStore) ClaimStage(ctx context.Context, claim SafetyReviewClaim) (SafetyReviewStageWork, bool, error)
func (s *SafetyReviewStore) CompleteStage(ctx context.Context, completion SafetyReviewStageCompletion) error
func (s *SafetyReviewStore) ReadSummary(ctx context.Context, taskID string) (SafetyReviewSummary, error)
```

- [ ] RED tests cover six tables, constraints, foreign keys, exact duplicate/conflicting ID, input order, unknown fields, scenes, rollback, task mismatch, atomic completion, idempotent stage creation, recovery, retry persistence, and decision idempotency.
- [ ] Embed only `safety_review_schema.sql`; configure WAL, foreign keys, busy timeout, and one writer. Do not alter legacy Store/schema. Hash canonical JSON with `UseNumber`.
- [ ] Import creates `judge:a`, `judge:b`, and `router` transactionally. Completion writes attempt/result, checks expected state, creates downstream stages, and advances item state in one transaction.
- [ ] Reopen DB between every transition in one test. Run `go test ./internal/dao -run '^TestSafetyReview' -count=100` and race with `-count=10`.
- [ ] Run all feature gates and record evidence.

## Task 4: Model registry, preflight, call policy, quota (`feat-018`)

**Produces:**

```go
type SafetyReviewModelRegistry interface { Chain(role dto.SafetyReviewRole) []SafetyReviewModel }
type SafetyReviewModel struct { Profile, Family string; Completer Completer }
func RunSafetyReviewPreflight(ctx context.Context, cfg SafetyReviewPreflightConfig) error
func NewSafetyReviewCaller(cfg SafetyReviewCallerConfig) (*SafetyReviewCaller, error)
func (c *SafetyReviewCaller) Call(ctx context.Context, req SafetyReviewCallRequest) (SafetyReviewCallResult, error)
func NewSafetyReviewQuota(cfg SafetyReviewQuotaConfig) (*SafetyReviewQuota, error)
```

- [ ] RED scripted-completer tests cover success, network, timeout, 408, 429/Retry-After, 5xx, malformed, semantic invalid, repair, refusal, repeated refusal, provider rejection, auth, bad model/mode, fallback, exhaustion, and independence degradation.
- [ ] Inject consumer-owned Clock/Sleeper/Jitter. Production uses real time/randomness; tests never sleep on wall clock.
- [ ] Persist each normal/repair/refusal/fallback attempt. Enforce 3 transient/profile and one repair/refusal. Rejection advances immediately; circuit errors skip profile for the run.
- [ ] Apply role and quota-group limits. Expert outranks Router; equal priority is FIFO. Test cancellation, cooldown, permit leaks, exact maxima, RPM/TPM, and finite-backlog progress.
- [ ] Preflight every role/profile combination. One failure prevents claiming. Probe content is fixed harmless data; raw probe output is not stored/logged.
- [ ] Run focused `-count=50`, race `-count=10`, and all gates.

## Task 5: Parallel A/B/Router scheduler foundation (`feat-019`)

**Produces:**

```go
func NewSafetyReviewRunner(cfg SafetyReviewRunnerConfig) (*SafetyReviewRunner, error)
func (r *SafetyReviewRunner) Run(ctx context.Context) (SafetyReviewRunStats, error)
```

- [ ] RED tests prove A/B/Router overlap for every item, no dataset barrier, no A/B-only shortcut, one/both-side Judge degradation, same-family shortcut prohibition, resume without duplicate success, bounded queues, and owned goroutines. Use a fake persisted Router result here; semantic Router behavior belongs to Task 6.
- [ ] Implement five owned pools with `sync.WaitGroup` and one size-one fatal channel. Ordinary item failures persist and do not cancel peers.
- [ ] Import exposes all three initial stages immediately. The last terminal initial-stage transaction invokes the shortcut gate; it must wait for both Judges and Router. Judge and Router requests never contain one another's output.
- [ ] Repeat focused tests 100 times with `GOMAXPROCS=1`, 100 times normally, and race 20 times. Assert goroutines return by a deadline.
- [ ] Run full gates and update state immediately.

## Task 6: Router and Experts (`feat-020`)

- [ ] RED matrix covers Prompt `3+3`, Response `0+3`, duplicates, unknown cards, bad feature refs, six parallel Experts, one call/candidate, blindness, policy gaps, all-excluded Safe eligibility, uncertain routing, mixed established categories, and all four shortcut cases from the approved design modification.
- [ ] Router sees common policy and manifest catalog, never full cards. It reports observable facts and candidates, never Unsafe, final risk type, or rule establishment. Sort/deduplicate. Prefer a specific identity card over `other_discrimination`; unknown/out-of-bundle becomes `policy_coverage_gap`, never Safe.
- [ ] Shortcut tests are exact: complete Safe A/B plus complete zero-candidate Router resolves Safe Typical without Expert/Arbiter; any candidate creates Expert; incomplete Router coverage proceeds to Arbiter and cannot resolve Safe without an established Expert category; degraded A/B independence disables shortcut.
- [ ] Expert loads one frozen card. Request excludes A/B and Router outputs. Validate assigned axis/category and full card condition matrix.
- [ ] A blocking fake proves separate Router/Expert concurrency, shared MiniMax ceiling, and Expert next-acquisition priority.
- [ ] Run focused/race/full gates and record evidence.

## Task 7: Arbiter and final projection (`feat-021`)

- [ ] RED table covers the label matrix, conflicts, primary ties, fixed priority, specific-over-other, ethnic hatred threshold, Response ownership, no-majority behavior, formal case types, shortcut-always-Typical, hard-negative Expert-exclusion evidence, absent `risk_level`, incomplete-Router Safe prohibition, all-Experts-not-established Unsafe prohibition, uncertain-upgrade prohibition, and all quarantine reasons.
- [ ] Arbiter receives original item and structured prior outputs, never raw responses. It cites rules. Local validation recomputes `is_attack`, memberships, `risk_type`, case-type/label compatibility, and Unsafe prerequisites. Arbiter may only use Expert-established categories.
- [ ] Code may sort/deduplicate and resolve declared ties by priority then lexical ID; it cannot add categories or upgrade uncertainty.
- [ ] Mutation proof must fail for Unsafe without Expert, Response method, Prompt-only Response evidence, Safe with categories, and other-over-specific selection.
- [ ] Run focused `-count=50`, race, and all gates.

## Task 8: CLI run/validate and recovery (`feat-022`)

**Produces:**

```go
func RunSafetyReview(ctx context.Context, args []string, stdout, stderr io.Writer) int
```

- [ ] RED tests cover commands, invalid usage, validate with zero network, shared API env values, preflight order, preflight failure before item claiming/semantic processing, resume, exits 0/1/130, legacy regression, and payload redaction.
- [ ] Add only this existing-code change: at the start of `run`, detect `args[0] == "safety-review"` and delegate `args[1:]`. Do not refactor old branches.
- [ ] Startup order: strict load, policy/Schema, API env, clients, store, identity/snapshot, import, all preflights, recovery, runner. A preflight failure may leave task metadata, snapshots, imported rows, and preflight records, but must leave zero claims, zero classification-stage executions, and zero decisions.
- [ ] Blocking-fake cancellation proves no new claims, bounded drain, committed results, running recovery, atomic run status, 130, and no leak; repeat under race 50 times.
- [ ] Run all legacy root tests and scope check; only protected `main.go` may differ.

## Task 9: Status and atomic export (`feat-023`)

- [ ] RED tests cover order, unknown fields, compatibility projection, empty arrays, clean/quarantine partition, sanitized audit, sanitized `quality-events.jsonl`, reports, stale replacement, temp cleanup, rename errors, TTY/non-TTY, read-only watch, throughput, ETA, and error aggregation.
- [ ] Export only from SQLite. Sync/close same-directory temp before rename. Failure is task-fatal but decisions remain.
- [ ] `quality-events.jsonl` contains only `trace_id`, scene, policy version/hash, terminal state/category, stage verdict/category summaries, disagreement type, fallback/degradation flags, quarantine reason, and error-pattern IDs. It contains no source payload and is explicitly suitable as metadata input to an Audit Package.
- [ ] Canary test puts unique Prompt/Response/evidence/raw-output/key values into state. Logs/status/audit/quality-events/report/errors contain none; clean/quarantine contain only expected source payloads.
- [ ] Inject failures before write, after partial write, after sync, and before rename. A partial final file is forbidden.
- [ ] Run focused/race/full gates.

## Task 10: Eval and P04-B live acceptance (`feat-024`)

- [ ] RED metrics independently fail on one Safe Gold -> Unsafe, one Unsafe Gold -> Safe, only 17 Unsafe resolutions, more than two Unsafe quarantines, only 7 acceptable boundaries, 13 total quarantines, duplicate/missing ID, invalid Schema, Response method, Unsafe without Expert, one failed rotation, and one unaccounted stage.
- [ ] Gold labels load only after terminal decisions. Assert requests/stages exclude originals. Run two fresh task directories with A/B swapped and no output reuse.
- [ ] Development and hidden truth accept only `approved_gold` or `core_gold` provenance. Candidate Gold produced by a top-model pass is exported for human review but never enters acceptance metrics until an explicit approval artifact binds reviewer, policy version, source hash, rule/decision IDs, and any correction.
- [ ] `verify-safety-review.sh` uses `set -euo pipefail` and runs format, focused, all, repeated race, vet, policy, architecture/scope, secret/payload, and diff gates in order.
- [ ] Fake E2E runs 50 synthetic rows through all stages, exports, interruption/resume, and both rotations with exact calls and only `httptest` network.
- [ ] Real acceptance requires local `Safety_Review_P04B_Hidden.jsonl` with exact 10/10/10/10/10 composition and every configured API env. Run validate then eval; print metrics only. For 20 Unsafe Gold, require resolved Unsafe >=18, resolved Safe =0, and quarantine <=2.
- [ ] Report explicitly states that 50 rows are the P04-B V1 gate, not full statistical validation; later 100-300+ Development and Hidden sets are P2 and do not block V1.
- [ ] A separate Go acceptance process recomputes IDs, Schemas, metrics, and rotations without workflow metric helpers.
- [ ] Anti-cheating proof: expected recall 21 fails against copied real evidence; restored 18 passes. A duplicate injected into copied export fails. Real evidence is untouched.
- [ ] Keep the real acceptance claim blocked if any key/model/hidden set/metric is absent. Fake success is insufficient. Do not block ordinary `validate` or model-only `run` when Hidden Gold is absent; those runs report `unvalidated` and quarantine unresolved items.
- [ ] Support `single_profile` as an operational mode: one `operational` profile, five role-specific prompt/Schema/stage/attempt flows, no fallback, and one shared quota group. It remains `unvalidated` and `eval` rejects it before side effects; `independent_profiles` remains the only acceptance mode.

## Task 11: Final review and handoff (`feat-025`)

- [ ] Run `scripts/verify-safety-review.sh` in a clean process.
- [ ] Run both real rotations from fresh task directories.
- [ ] Inspect every diff and justify every existing-file change.
- [ ] Scan for credentials, datasets, DBs, runs, raw outputs, and payload logging.
- [ ] Stop long commands and prove both DBs support status and resume.
- [ ] Update feature/progress/handoff with exact commands, exits, metrics, risks, and next action.
- [ ] Mark complete only when every prior feature and real acceptance is complete.

## Required Evidence Template

```text
Feature:
Baseline command and exit:
RED command, exit, and expected failure:
Files changed:
Focused GREEN command and exit:
Repeated/concurrency command and exit:
Race command and exit:
Full ./init.sh command and exit:
Scope/security commands and exits:
Real-model aggregate evidence (only when required):
Known residual risk:
Next unblocked feature:
```

“Tests pass”, cached output without `-count=1`, skipped required live tests, or tests written after implementation are insufficient evidence.
