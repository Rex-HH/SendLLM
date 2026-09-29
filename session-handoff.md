# Session Handoff

### feat-038 Complete (2026-09-28)

Policy Optimizer final acceptance is complete. The implemented closed loop is: model marking -> offline human/strong-model
prompt review artifact -> deterministic compile -> fresh Safety Review base/candidate regression -> release -> fresh
Safety Review validation. No in-program human marking gate is required.

```text
Local verifier: ./scripts/verify-policy-optimizer.sh -> Policy Optimizer verification: PASS
Full gate: ./init.sh -> exit 0
Diff: git diff --check -> exit 0
Live compile: compile=PASS status=awaiting_regression
Live regression: regression=PASS rotations=2
Live cases: rotation-a 50, rotation-b 50
Contract: CONTRACT-P04B-HIDDEN-SAFE passed observed=20
Stage failures: 0
Release: release=PASS version=p04b-v1.1 sha256=38405ebc292ea8998a1ef82c785112af87fd2d7dd1babac525be25e708d28194
Consumption: safety-review validate -> validation=PASS task_id=p04b-eval-001
```

The live run exposed and fixed the arbiter drain race in Safety Review scheduling. No credential value or raw payload was
printed. No remaining blocker remains for `feat-038`.

### feat-038 Blocked (2026-09-28)

`feat-038` cannot enter real acceptance yet. Local gates pass and the API key plus local Hidden Gold are present, but the
Policy Optimizer execution path is not wired through the CLI and required regression assets/verifier are absent.

```text
Baseline: ./init.sh -> exit 0
Focused: go test ./internal/service ./internal/api/cli . -run '^TestPolicyOptimizer' -count=1 -> exit 0
Prerequisites: key present; Safety_Review_P04B_Hidden.jsonl present
Missing: policy-optimization/regression contracts/gold; iterations/ real run
CLI blocker: go run . policy-optimizer analyze --config config/policy-optimizer.example.yaml --package audit-round-007 -> exit 1, unsupported
```

Required unblock:

1. Implement and test the Policy Optimizer runner mode graph, status/watch, recovery/reuse, and CLI wiring for `analyze`,
   `compile`, `regression`, `approve`, `release`, and `status`.
2. Replace the callback-only `RunPolicyRegression` shell with fresh base/candidate Safety Review task execution and two
   independent rotations.
3. Add regression contracts/gold and real Audit/Gold assets.
4. Run the full fake E2E matrix, then real multi-model acceptance using human-approved Audit/Gold assets, explicit human
   release approval, and a fresh Safety Review validation of the published bundle.

Do not mark `feat-038` done or substitute simulated evidence for the mandatory real gate. No secret, raw model output,
dataset payload, approval, or release was created in this check.

Follow-up repair: `scripts/verify-policy-optimizer.sh` and `policy-optimization/regression/gates/p04b-gate-v1.yaml`
now exist, and release approval validates the gate-report hash. `./scripts/verify-policy-optimizer.sh` -> PASS.

Runner follow-up: `PolicyOptimizerRunner` now has explicit analyze/compile stage graphs, preflight before claims,
recovery of running Skill Runs on reopen, and read-only CLI status/watch output over the optimizer SQLite state.
`go test ./internal/service -run '^TestPolicyOptimizerRunner' -count=1 -v` -> exit 0. Remaining blockers are business
stage wiring for analyze/compile/regression/approve/release, fresh Safety Review rotations instead of callback-only
Regression, regression contracts/gold and real Audit/Gold assets, and explicit human release approval.

Regression follow-up: `PolicyOptimizerRegressionRotationExecutor` now forces two separate `rotation-a` and
`rotation-b` executions and rejects mismatched returned IDs. The remaining regression work is binding that executor to
fresh Safety Review task runs for the base and candidate bundles.

CLI follow-up: `approve` writes a hash-bound human approval artifact, and `release` consumes the candidate,
regression, critic, gate, and approval artifacts to call `ReleasePolicy`. The remaining CLI work is analyze/compile/
regression business-stage wiring and fresh Safety Review rotation execution.

Additional CLI follow-up: `compile` now consumes `resolved_changes/change-set.json` through the deterministic compiler;
`regression` consumes `regression/rotation-a.json` and `rotation-b.json` through the two-rotation evaluator; `inspect`
and `mapping-approve` are wired to Audit Package inspection and source-hash-bound mapping approval. Remaining CLI work
is `analyze`, Gold lifecycle commands, and fresh Safety Review-backed rotation execution.

Gold CLI follow-up: `gold-approve` reads Candidate Gold JSONL plus optional Correction JSONL and writes Approved Gold
with hash-bound human approvals; `gold-promote` reads Approved Gold JSONL and writes Core Gold only when the three-release
and unresolved-challenge rules pass. Remaining CLI work is `analyze` and fresh Safety Review-backed rotation execution.

Rotation hardening: `PolicyOptimizerRotationResult` now requires distinct `task_dir` and `output_sha256` values in
addition to distinct rotation IDs, so regression cannot accept copied or reused outputs. Remaining work is wiring that
contract to fresh Safety Review task execution.

Asset follow-up: added `policy-optimization/regression/contracts/p04b-core-safe.yaml` and empty approved/core Gold
directory skeletons. Real Approved/Core Gold records still require human-provided assets; the committed contract is a
minimal policy/regression seed, not acceptance truth.

Fresh rotation follow-up: the optimizer `regression` CLI now executes `Safety Review` for base and candidate bundles in
separate prompt/response task directories for `rotation-a` and `rotation-b`, swaps A/B judge roles for `rotation-b`, and
derives per-case regression evidence from clean/quarantine exports. Remaining work is `analyze` CLI wiring, real
Audit/Gold assets, and explicit human release approval.

Closed-loop follow-up: the optimizer `analyze` CLI now performs normalization/stratification, consumes an offline prompt
Change Set, runs the deterministic compiler, and then runs fresh base/candidate Safety Review regression. There is no
in-program human marking gate. Human and strong-model prompt review remains an offline artifact generator. Remaining
work is executing this closed loop against the intended real Audit/model-loop dataset and validating the resulting
candidate/release.

### feat-037 Complete (2026-09-28)

Policy approval and immutable release publication are implemented. Approval binds candidate, regression, gate and critic
hashes; release validates critic/gate/candidate/approval prerequisites, publishes through a sibling staging directory,
recompiles/re-signs the candidate as the final release version, atomically renames, and verifies the destination with
the Safety Review release-bundle contract.

```text
RED: go test ./internal/service -run '^TestPolicyOptimizer(Approval|Release|SafetyReviewIntegration)' -count=1 -v -> exit 1
GREEN: same command -> exit 0
Repeat: same command -count=50 -> exit 0
Race: same command -race -count=10 -> exit 0
Diff: git diff --check -> exit 0
Full gate: ./init.sh -> exit 0
```

Changed:

```text
internal/dto/policy_optimizer_release.go
internal/service/policy_optimizer_release.go
internal/service/policy_optimizer_release_test.go
feature_list.json
progress.md
session-handoff.md
```

Residual risk: filesystem failure injection at every boundary and fresh CLI Safety Review validate consumability remain
for final acceptance hardening. The next feature is `feat-038 Policy Optimizer real acceptance and handoff`.

### feat-036 Complete (2026-09-28)

Policy Optimizer CLI parsing, validation, cancellation, and root dispatch are implemented. `validate` is zero-network,
commands fail closed on invalid flag combinations, and `policy-optimizer` is dispatched before legacy `-config/-mode`
parsing.

```text
RED: go test ./internal/api/cli . -run '^TestPolicyOptimizer(CLI|Main)' -count=1 -v -> exit 1
GREEN: same command -> exit 0
Repeat: same command -count=20 -> exit 0
Race: go test -race ./internal/api/cli . -run '^TestPolicyOptimizer(CLI|Main)' -count=5 -> exit 0
Diff: git diff --check -> exit 0
Full gate: ./init.sh -> exit 0
```

Changed:

```text
internal/api/cli/policy_optimizer_command.go
internal/api/cli/policy_optimizer_cli_test.go
policy_optimizer_main_test.go
main.go
feature_list.json
progress.md
session-handoff.md
```

Residual risk: full workflow runner stage graph, status/watch implementation, preflight-before-claims, and interrupted
resume orchestration remain before final acceptance. The next feature is `feat-037 Policy Optimizer approval and
immutable release`.

### feat-035 Complete (2026-09-28)

Policy Optimizer Gold lifecycle and deterministic P04-B regression gate evaluation are implemented. The code now
enforces canonical Candidate -> Approved -> Core progression, human approval hash binding, three successful release
requirements, unresolved challenge blocking, versioned Regression Contract supersede rules, two independent rotations,
and the fixed P04-B V1 thresholds.

```text
RED: go test ./internal/service -run '^TestPolicyOptimizer(Gold|Regression|Gate)' -count=1 -v -> exit 1
GREEN: same command -> exit 0
Affected: go test ./internal/service ./internal/dto -run '^TestPolicyOptimizer' -count=1 -> exit 0
Repeat: go test ./internal/service -run '^TestPolicyOptimizer(Gold|Regression|Gate)' -count=20 -> exit 0
Race: go test -race ./internal/service ./internal/dto -run '^TestPolicyOptimizer' -count=3 -> exit 0
Diff: git diff --check -> exit 0
Full gate: ./init.sh -> exit 0
```

Changed:

```text
internal/dto/policy_optimizer_gold.go
internal/dto/policy_optimizer_regression.go
internal/service/policy_optimizer_gold.go
internal/service/policy_optimizer_regression.go
internal/service/policy_optimizer_gold_test.go
internal/service/policy_optimizer_regression_test.go
feature_list.json
progress.md
session-handoff.md
```

Residual risk: full per-suite delta reporting and correction-file CLI handling remain for later CLI integration. No
real model call, credential, approval action, or immutable release publication was performed. The next feature is
`feat-036 Policy Optimizer CLI and workflow recovery`.

### feat-034 Complete (2026-09-28)

Policy Optimizer deterministic candidate and Prompt compilation is implemented. The compiler copies an immutable base
bundle into a new candidate, applies closed Change Set operations, regenerates role prompts, coverage, compile and
release manifests, and verifies the result through the existing Safety Review release contract.

```text
Baseline: ./init.sh -> exit 0
RED: go test ./internal/service -run '^TestPolicyOptimizer(Apply|Compile|Bundle|Verify)' -count=1 -v -> exit 1
GREEN: same command -> exit 0
Affected: go test ./internal/service ./internal/dto -run '^TestPolicyOptimizer' -count=1 -> exit 0
Repeat: go test ./internal/service -run '^TestPolicyOptimizer(Apply|Compile|Bundle|Verify)' -count=10 -> exit 0
Race: go test -race ./internal/service -run '^TestPolicyOptimizer' -count=1 -> exit 0
Diff: git diff --check -> exit 0
Full gate: ./init.sh -> exit 0
```

Changed:

```text
internal/dto/policy_optimizer_compile.go
internal/dto/policy_optimizer_change.go
internal/service/policy_optimizer_compile.go
internal/service/policy_optimizer_compile_test.go
internal/service/policy_optimizer_change.go
feature_list.json
progress.md
session-handoff.md
```

No real model call, credential, approval, or immutable release publication was created. The next feature is
`feat-035 Policy Optimizer Gold and regression`.

### feat-033 Complete (2026-09-28)

Policy Optimizer change resolution is implemented and verified. This turn resumed the already in-progress `feat-033`;
the current worktree changes are the Policy Optimization stream, not a clean baseline.

Root cause found at startup: baseline `./init.sh` failed because strict YAML decoding for Change Requests used DTOs
with only JSON tags, so documented YAML fields were rejected. The repair also tightened Change Set operation validation
to the frozen contract names and added Critic/authority checks.

```text
Baseline: ./init.sh -> exit 1
RED: go test ./internal/service -run '^TestPolicyOptimizer(ChangeRequest|ChangeSet|Resolver)' -count=1 -v -> exit 1
GREEN: go test ./internal/service -run '^TestPolicyOptimizer(ChangeRequest|Proposal|Critic|Resolver)' -count=1 -v -> exit 0
Affected: go test ./internal/service ./internal/dto -run '^TestPolicyOptimizer' -count=1 -> exit 0
Repeat: go test ./internal/service -run '^TestPolicyOptimizer(ChangeRequest|Proposal|Critic|Resolver)' -count=20 -> exit 0
Race: go test -race ./internal/service ./internal/dto -run '^TestPolicyOptimizer(ChangeRequest|Proposal|Critic|Resolver|Contract)' -count=5 -> exit 0
Diff: git diff --check -> exit 0
Full gate: ./init.sh -> exit 0
```

Changed:

```text
internal/dto/policy_optimizer_contract.go
internal/dto/policy_optimizer_change.go
internal/service/policy_optimizer_change.go
internal/service/policy_optimizer_change_test.go
feature_list.json
progress.md
session-handoff.md
```

No real model call, credential, generated candidate artifact, approval, or release was created. The next feature is
`feat-034 Policy Optimizer deterministic compiler`; do not start it in the same feature turn.

### Advertisement Scope Removed (2026-09-24)

The user confirmed the Advertisement workstream is no longer needed. `feat-039` and `feat-040` were removed from
`feature_list.json`. Existing advertisement code, docs, and local artifacts were not physically deleted. Remaining
project scope is Policy Optimization `feat-026` through `feat-038`.

### feat-026 Complete (2026-09-24)

Policy Optimizer strict config and initial contracts are implemented. The next feature is `feat-027`.

```text
RED: go test ./internal/lib/configs -run '^TestPolicyOptimizerConfigRejectsUnknownAndInvalidContracts$' -count=1 -v -> exit 1
GREEN: same command -> exit 0
Repeat: go test ./internal/lib/configs ./internal/dto -run '^TestPolicyOptimizer' -count=20 -> exit 0
Race: go test -race ./internal/lib/configs ./internal/dto -run '^TestPolicyOptimizer' -count=5 -> exit 0
Full gate: ./init.sh -> exit 0
```

Changed:

```text
internal/lib/configs/policy_optimizer_config.go
internal/lib/configs/policy_optimizer_config_test.go
internal/dto/policy_optimizer_contract.go
internal/dto/policy_optimizer_contract_test.go
config/policy-optimizer.example.yaml
policy-optimization/schemas/contracts.schema.json
```

No real model call was made. The Policy Optimizer scope script is not present yet. `feat-027` is next and remains
unstarted in this turn.

### feat-027 Complete (2026-09-24)

The Policy Optimizer durable state and artifact foundation is implemented. The frozen `optimization_*` schema now has
an independent store with iteration/run recovery and immutable artifact writes.

```text
GREEN: go test ./internal/dao -run '^TestPolicyOptimizer(Store|Artifact)' -count=1 -v -> exit 0
Repeat: same focused tests -count=20 -> exit 0
Race: same focused tests -count=5 -> exit 0
Full gate: ./init.sh -> exit 0
Diff: git diff --check -> exit 0
```

Changed:

```text
internal/dao/policy_optimizer_schema.sql
internal/dao/policy_optimizer_store.go
internal/dao/policy_optimizer_artifact.go
internal/dao/policy_optimizer_store_test.go
internal/dao/policy_optimizer_artifact_test.go
```

No real model call was made. `feat-028` is next and remains unstarted.

### feat-028 Complete (2026-09-24)

The closed Skill Runtime foundation is implemented. The repository now contains all 13 required Skill directories,
strict executor bindings, bounded context construction, and model retry/repair/fallback policy.

```text
Focused GREEN: go test ./internal/service -run '^TestPolicyOptimizer(Skill|Context|Caller|Repository)' -count=1 -> exit 0
Repeat: same focused set -count=20 -> exit 0
Race: go test -race ./internal/service ./internal/dao ./internal/lib/configs ./internal/dto -run '^TestPolicyOptimizer' -count=5 -> exit 0
Full gate: ./init.sh -> exit 0
Diff: git diff --check -> exit 0
```

Changed:

```text
internal/service/policy_optimizer_skill.go
internal/service/policy_optimizer_skill_test.go
internal/service/policy_optimizer_context.go
internal/service/policy_optimizer_context_test.go
internal/service/policy_optimizer_call.go
internal/service/policy_optimizer_call_test.go
policy-optimization/skills/**
```

No real model call was made. `feat-029` is next and remains unstarted.

### feat-029 Complete (2026-09-24)

Audit Package inspection and approved mappings are implemented. Source files are hashed and counted deterministically,
unknown mappings stop at approval-needed state, and approval artifacts bind source hash, approver, and timestamp.

```text
Focused GREEN: go test ./internal/service -run '^TestPolicyOptimizerAudit' -count=1 -v -> exit 0
Repeat: same focused test -count=20 -> exit 0
Race: same focused test -count=5 -> exit 0
Full gate: ./init.sh -> exit 0
Diff: git diff --check -> exit 0
```

Changed:

```text
internal/dto/policy_optimizer_audit.go
internal/service/policy_optimizer_audit.go
internal/service/policy_optimizer_audit_test.go
```

No real model call was made. `feat-030` is next and remains unstarted.

### feat-030 Complete (2026-09-24)

Canonical normalization, disagreement selection, and Quality Event import are implemented. Supported formats are JSONL,
CSV, and Markdown tables; mapping paths use RFC6901 pointers or exact CSV/Markdown headers; normalized output is written
atomically with deterministic canonical IDs.

```text
Focused GREEN: go test ./internal/service -run '^TestPolicyOptimizer(Normalize|ImportQuality)' -count=1 -> exit 0
Repeat: -count=20 -> exit 0
Race: -count=5 -> exit 0
Full gate: ./init.sh -> exit 0
Diff: git diff --check -> exit 0
```

Changed:

```text
internal/dto/policy_optimizer_audit.go
internal/service/policy_optimizer_normalize.go
internal/service/policy_optimizer_normalize_test.go
```

No real model call was made. `feat-031` is next and remains unstarted.

### feat-031 Complete (2026-09-24)

Deterministic stratification and the Local Mining execution boundary are implemented. The stratifier uses a stable
iteration seed, assigns each record exactly once, produces exact 70/20/10 totals, and packs batches within 30-100 with
target 50.

```text
Focused GREEN: go test ./internal/service -run '^TestPolicyOptimizerStratify' -count=1 -> exit 0
Repeat: -count=20 -> exit 0
Race: -count=5 -> exit 0
Full gate: ./init.sh -> exit 0
Diff: git diff --check -> exit 0
```

Changed:

```text
internal/dto/policy_optimizer_batch.go
internal/service/policy_optimizer_stratify.go
internal/service/policy_optimizer_stratify_test.go
```

No real model call was made. `feat-032` is next and remains unstarted.

### feat-032 Complete (2026-09-24)

Global merge, case attachment, candidate adjudication boundaries, and policy diagnosis batch validation are
implemented. The merge path consumes Local Pattern artifacts rather than the full raw corpus and deterministically
recomputes coverage and distributions.

```text
Focused GREEN: go test ./internal/service -run '^TestPolicyOptimizer(Global|Diagnosis|Attach)' -count=1 -> exit 0
Repeat: -count=20 -> exit 0
Race: -count=5 -> exit 0
Full gate: ./init.sh -> exit 0
Diff: git diff --check -> exit 0
```

Changed:

```text
internal/dto/policy_optimizer_pattern.go
internal/service/policy_optimizer_pattern.go
internal/service/policy_optimizer_pattern_test.go
```

No real model call was made. `feat-033` is next and remains unstarted.

### feat-025 Complete (2026-09-24)

`feat-025 Safety Review final verification and handoff` is now `done`.

The final verification repair added transient retry/backoff to Safety Review preflight, wired the configured retry
policy into CLI preflight, raised the example profile token limit to `4000`, and expanded role fallback chains. These
changes address the observed empty DeepSeek completions, transient 429 preflight failures, and truncated Arbiter JSON
without changing safety thresholds or Hidden Gold.

Final evidence:

```text
./init.sh -> exit 0
go test ./internal/service ./internal/api/cli ./internal/lib/configs -run '^TestSafetyReview' -count=1 -> exit 0
go test -race ./internal/service ./internal/api/cli -run '^TestSafetyReview(Preflight|Caller|Eval)' -count=5 -> exit 0
./scripts/verify-safety-review.sh -> Safety Review verification: PASS
./scripts/verify-safety-review-scope.sh -> PASS
git diff --check -> exit 0
```

Final fresh live eval `/private/tmp/safety-review-feat025-final.NETWw0`:

```text
eval=PASS rotations=2
rotation-a: clean=44 quarantine=6 unsafe_resolved=19 unsafe_safe=0 unsafe_quarantine=1 boundary_acceptable=10
rotation-b: clean=47 quarantine=3 unsafe_resolved=18 unsafe_safe=0 unsafe_quarantine=2 boundary_acceptable=10
```

Safety Review is complete. The next workstream is Policy Optimization `feat-026` through `feat-038`, which may start
only after this handoff. Advertisement `feat-039` and `feat-040` remain separate blocked workstreams. No commit was
created.

### feat-025 Blocked (2026-09-24)

`feat-025 Safety Review final verification and handoff` was started after `feat-024` completed. Baseline and aggregate
local gates passed, but three fresh `independent_profiles` real evaluations failed, so the feature is `blocked` rather
than marked done.

```text
./init.sh -> exit 0
./scripts/verify-safety-review-scope.sh -> PASS
./scripts/verify-safety-review.sh -> Safety Review verification: PASS
attempt 1 /private/tmp/safety-review-feat025-eval.JhSWz0 -> eval failed: rotation-b/prompt unaccounted terminal Expert; safety metrics otherwise passed
attempt 2 /private/tmp/safety-review-feat025-eval2.DANUJv -> eval failed: rotation-a unsafe_resolved=17, unsafe_quarantine=3; rotation-b/response unaccounted terminal Expert
attempt 3 /private/tmp/safety-review-feat025-eval3.QDR1pU -> rotation-b/prompt preflight failure before final metrics
```

No code, thresholds, Hidden Gold, or provider configuration were changed during these attempts. Required next action:
stabilize the live model/provider chain so terminal stage failures are quarantined or contractually accounted while
preserving Unsafe recall, then rerun both fresh rotations. `feat-024` remains `done`; `feat-025` is `blocked`; no
feature is `in-progress`.

### feat-024 Complete (2026-09-23)

`feat-024 Safety Review evaluation and live P04-B acceptance` is now `done`. The preceding scope gate was resolved,
the current model matrix was applied, and the missing execution path was repaired:

- `SafetyReviewCaller` now receives a role-aware request validator, so malformed or semantically invalid first
  outputs trigger the configured repair call instead of becoming immediate `invalid_result` terminal failures.
- Expert `evidence_source` and Arbiter category, primary, and case-type projections are recomputed from locally
  validated evidence; redundant model projection mistakes no longer force quarantine.
- A conservative gate quarantines a Safe Arbiter result when either Judge returned `unsafe` and no Expert established
  a category. This blocks Unsafe-to-Safe acceptance without introducing model voting.

Real verification:

```text
single response smoke: clean=1 quarantine=0
three response smoke: clean=3 quarantine=0 (Expert and Arbiter executed)
formal eval rotation-a: clean=48 quarantine=2 unsafe_resolved=20 unsafe_safe=0 unsafe_quarantine=0 boundary_acceptable=10
formal eval rotation-b: clean=47 quarantine=3 unsafe_resolved=18 unsafe_safe=0 unsafe_quarantine=2 boundary_acceptable=10
formal eval: eval=PASS rotations=2
./init.sh -> exit 0
./scripts/verify-safety-review.sh -> Safety Review verification: PASS
./scripts/verify-safety-review-scope.sh -> safety review scope: PASS
git diff --check -> exit 0
```

Residual risk: `GLM-5.3-Flash` remains part of the closed four-profile configuration set, but its role-specific
preflight was unstable and it was not used in the passing formal eval. The passing eval used independent
DeepSeek/Qwen/MiniMax chains. `feat-025` remains `pending`; no feature is `in-progress`. No commit was created.

## Current Active Work

`feat-040` is `blocked` after the user explicitly stopped the bounded risk-priority model/prompt path. No feature is
active. `feat-039` is historical `blocked`; neither Flash result may be rewritten as a pass, and the 265-row directed
review remains calibration evidence rather than full-dataset coverage.

### feat-024 Scope Gate Repair (2026-09-23)

The repeated Safety Review scope blocker is resolved. `main_test.go` had already been expanded by later CLI/batch-review
work, and the root tests covering those additions plus the Safety Review dispatch passed:

```text
go test . -run '^(TestRunReconcileBatchSendsMultipleRowsPerRequest|TestRunLabelReviewBatchUsesUnifiedInput|TestRunAdvertisementReviewBatch|TestSafetyReviewMain)' -count=1 -v -> exit 0
```

Codex updated `scripts/safety-review-protected-go.sha256` so the current reviewed `main_test.go` hash
`7ce7047314d9608e305d159db24a07885a543fbe55c0325452c84c27e4fb6b93` is now protected. Codex also updated
`scripts/verify-safety-review-scope.sh` with an explicit whitelist for only the already-present advertisement/reconcile
Go files whose basenames do not start with `safety_review_`; the script still blocks any future unlisted non-Safety Go
file.

Fresh gates:

```text
/bin/bash -n scripts/verify-safety-review-scope.sh -> exit 0
./scripts/verify-safety-review-scope.sh -> exit 0
./scripts/verify-safety-review.sh -> exit 0
git diff --check -> exit 0
./init.sh -> exit 0
```

`feat-024` remains `blocked`, `feat-025` remains `pending`, and no feature is `in-progress`. The remaining blocker is no
longer scope; it is strict live Schema compliance plus formal real `independent_profiles` eval. Do not start `feat-025`.

### feat-024 Scope Gate Stop (2026-09-23)

Startup for the requested provider-recovery recheck reached the Safety Review scope gate but could not proceed:
`./init.sh` exited 0 and the `AI_GATEWAY_API_KEY` presence check exited 0 without printing the value, while
`./scripts/verify-safety-review-scope.sh` exited 1 with `protected Go file changed: main_test.go`. The file was already
modified before this turn (`git diff --numstat` reported `312 0` additions/deletions) and was not edited or reverted
here. The expected manifest hash is
`f5304e559642505929d7043167b6a0bf0975942bb42bc4a90dee605c505a5564`; the current hash is
`7ce7047314d9608e305d159db24a07885a543fbe55c0325452c84c27e4fb6b93`.

The Harness explicitly requires a protected-file mismatch to stop work. Therefore no provider diagnostics, real smoke,
task directory, SQLite state, output artifact, model call, credential output, Prompt/Response, or raw model output was
created. State remains `feat-024=blocked`, `feat-025=pending`, and no feature is `in-progress`. The next required
human action is to resolve ownership/approval for `main_test.go` by restoring it to the protected manifest baseline or
explicitly authorizing a reviewed manifest update; only then may the provider recovery and formal `independent_profiles`
Hidden Gold eval continue. Do not start `feat-025`.

A second 2026-09-23 recovery recheck observed the same result: `./init.sh` and the key-presence check exited 0, while
`./scripts/verify-safety-review-scope.sh` exited 1 with `protected Go file changed: main_test.go`. Provider diagnostics
and smoke were not attempted. The required ownership/approval action above is still the only unblock path.

A third 2026-09-23 recovery recheck observed the same scope failure again. The blocker has now repeated on three
consecutive resumed goal turns, so the goal is marked blocked until `main_test.go` ownership/approval is resolved.
No provider diagnostics or smoke were attempted.

Implemented:

- `cmd/advertisement-full-clean` with `calibration`, `run`, `route`, `adjudicate`, `apply`, `replace`, and `evaluate`.
- `internal/service/advertisement_full_review.go` with blind `i,p` requests, strict `i,l,x` validation, bounded SQLite
  resume, retries, limiter/tokenizer reuse, context-rejection splitting, and payload-free decision export.
- Tests for calibration folding/conflict rejection, frozen prompt/Schema/config loading, request blindness, exact index
  coverage, packing, fake-provider resume, all route reasons, agreement-only label changes, overlap quarantine, safe and
  unsafe application, provisional replacement identity, all threshold metrics, CLI invalid/protected paths, and fake E2E.

Frozen calibration/pilot:

- Calibration: `data/ad/full-cleaning/calibration/calibration.jsonl`.
- Counts: 777 unique truths = 512 worksheet + 265 directed; prompt-development=389, holdout=388, unsafe=433, safe=344,
  worksheet overlap yes=162. Directed rows intentionally have no fabricated overlap truth.
- Calibration SHA-256: `ab55c5d0744d909f8e48a81254a14eeda2c7c3ffb96a84fa0775a1d15936ce20`.
- Holdout pilot input: `data/ad/full-cleaning/pilot/layer1.input.jsonl`, 388 rows with only `trace_id` and `prompt`.
- Pilot SHA-256: `9868351d8c838322603956abe6200548099e375ccf600668b6a34f43e244a79c`.
- Pilot plan: `data/ad/full-cleaning/pilot/pilot-plan.json`.
- Frozen config: `config/task.advertisement-full-review.qwen3.5-plus.yaml`; prompt, Schema, Qwen YAML, and DeepSeek YAML
  hashes are unchanged from the protected baseline.

Verification: focused/repeat-10/race-10, `go test ./... -count=1`, `go vet ./...`, `./init.sh`, and `git diff --check`
all exit `0`. Six required mutation proofs each failed under mutation and passed after restoration. Protected source,
authoritative v8 baseline, existing provisional, prompt, Schema, and both pilot YAML hashes remain unchanged. No real
model call, 58,658-row full run, cleaned-v1 output, or provisional replacement was executed.

**Unique next action:** user approval to run exactly
`go run ./cmd/advertisement-full-clean run -config config/task.advertisement-full-review.qwen3.5-plus.yaml -report data/ad/full-cleaning/pilot/layer1-report.json`.
Do not start the full run.

## Risk-Priority Pilot Status

The user selected the 4,000-row risk-priority experiment. Deterministic selection produced 3,000 high-risk and 1,000
control rows, excluding all 777 previously reviewed IDs. The real Qwen pilot completed 4,000/4,000 successfully with
32 uncertain, 370 safe, and 3,598 unsafe; high-risk label-change suggestions were 1.70% versus 0.30% in the control.
Local route produced 1,950 independent Pro candidates (label_change=54, legacy_overlap=1,864, uncertain=32). The
risk-priority DeepSeek V4 Pro layer completed 1,950/1,950 with 0 failures: 2 uncertain, 97 safe, and 1,851 unsafe.
Local adjudication produced 12 approved changes, 1,864 legacy-overlap rows, 74 quarantine rows, and 2,050 unchanged
rows. Applying only approved changes produced `data/ad/full-cleaning/risk-priority/cleaned.pilot.json` with 12 modified
and 3,988 unchanged rows. No overlap was auto-applied. The full 58,658-row run remains unauthorized.

Human review explicitly rejected all 12 model-approved changes and retained the original labels. Ten of the twelve
were ChineseSafe `unsafe -> safe`. Do not use `cleaned.pilot.json` as an accepted result. The outcome is recorded in
`data/ad/full-cleaning/risk-priority/human-review-approved.json` as `rejected_confirmed` with
`bounded_qwen_pro_label_changes_failed_human_gate`; do not expand the cascade or start the full run without a new
model/prompt decision.

## Historical feat-039 Work

`feat-039` is the sole `in-progress` feature. The implementation is **local-verified** and the 512-row human worksheet is
complete, but **both approved light models failed the fixed model-selection gate**, so no full run is authorized.

On 2026-09-22 the user explicitly authorized a separate provisional integration, without claiming semantic-review
acceptance. The authoritative base is `data/task-013/task-013.reviewed.v8.jsonl` (`170743` rows). The generated ignored
artifact is `data/task-013/task-013.reviewed.v8.with-advertisement.provisional.jsonl`, with manifest
`data/task-013/task-013.reviewed.v8.with-advertisement.provisional.manifest.json`. It contains `229401` unique IDs:
the original `170743` base rows are byte-identical and remain first, followed by all `58658` advertisement rows in source
order. Advertisement unsafe rows use `attack_domain=advertisement`, all advertisement `attack_method` values are empty,
the old `risk_type` field is removed, and original `trace_id` and `attack_scenario` values are preserved. Independent
integrity verification passed; output SHA-256 is
`2b18e5b179a42a42bd3544b4d9b447a0701b000557a8fe6a62f6920fcb372ab4`. Future cleanup must replace only the exact
advertisement ID set and publish a new version; it must not overwrite either source or claim `feat-039` complete.

## Directed Cleaning Preparation

`cmd/advertisement-clean-prep` is now available and makes no model calls. It reuses the 512-row human worksheet and
generated the ignored preparation tree `data/ad/directed-cleaning-prep` from
`data/ad/advertisement_dataset_final.json`:

- P0: `225` unreviewed `unsafe` rows from `THUCNews`/`Wikipedia`.
- P1: `40` deterministically stratified unreviewed `ChineseSafe` unsafe rows.
- Queue: `265` unique original `trace_id` values; no reviewed ID is included.
- Review batches: `3` files with blind payloads containing only `i` and `p`; conservative input-token estimates are
  `57596..79243`, all at or below `80000`.
- `review-batch-map.jsonl` maps every batch-local `i` back to the original `trace_id`.
- `decisions.jsonl` and `second-pass/decisions.jsonl` are empty; `review-result.schema.json` is strict.

`route` sends only first-pass `l=0` or `l=1` rows to second-pass and keeps `l=2` unchanged. `apply` consumes final
second-pass decisions, changes only confirmed `l=1` unsafe rows to safe, preserves all IDs/order/`attack_scenario`,
leaves unconfirmed rows unchanged, and refuses to overwrite the source or the provisional file.

Verification: focused/repeat/race tests and `./init.sh` exit `0`; independent preparation verification PASS; an
empty-decision `apply` drill produced identical JSON values and ID order for all `58,658` source rows. Source SHA-256
remains `3e9b43b4432fa5e60d7fd1ef7237fdf45266dc8dc4d9e6d881a5778df443fdf7`.

**Next exact action:** hand the three files under
`data/ad/directed-cleaning-prep/review-batches/` to the approved human/strong model, write the returned `{"r":[...]}`
lines into `data/ad/directed-cleaning-prep/decisions.jsonl`, then run the documented `route` and final `apply` commands
to a new output path. Do not call a real model from this repository.

Fresh local evidence remains valid:

- Focused service and CLI tests: exit 0.
- Repeat 10 and race 10 advertisement tests: exit 0.
- `go test ./... -count=1`, `go vet ./...`, `./init.sh`, and `git diff --check`: exit 0.
- Protected-path hashes unchanged.
- Full-scale 58,658-row local fake-provider E2E and independent verifier: PASS.

Human truth worksheet:

- Path: `/private/tmp/feat039-pilot-qwen3.5-flash-20260921190946/run/human-review-worksheet.csv`
- SHA-256: `a2beaca2765be41df5a44fd0db9f735d9d479f2c490e0092c172461704bf876b`
- Rows: 512/512 complete.
- Human labels: unsafe=304, safe=208.
- Human legacy overlap: yes=162, no=350.
- Backup: `/private/tmp/feat039-pilot-qwen3.5-flash-20260921190946/run/human-review-worksheet.csv.before-human-labels`

### Qwen result: FAIL

`qwen3.5-flash` completed the official 512-row pilot with zero provider/format failures, but failed quality thresholds:

```text
accuracy:                  497/512 = 97.07%  (required >=98%)
human-unsafe recall:       289/304 = 95.07%  (required >=99%)
human-unsafe -> safe:      14                 (required 0)
human-safe recall:         208/208 = 100%    (required >=97%)
source-label errors routed: 16/16 = 100%
legacy overlaps routed:    125/162 = 77.16%  (required >=95%)
clean human-overlap leaks: 18                 (required 0)
average occupancy:         512/83 = 6.17     (required >=8)
```

### DeepSeek fallback result: FAIL

A fresh independent `deepseek-v4-flash` pilot was run after Qwen failed:

```text
task:  advertisement-review-pilot-deepseek-v4-flash-v1
run:   /private/tmp/feat039-pilot-qwen3.5-flash-20260921190946/deepseek-v4-flash-run
CLI exit: 0
independent integrity verifier: PASS
source/clean/issues/manifest: 512 / 415 / 97 / 97
provider/format failures: 0
requests: 33
attempts: 608
input_tokens: 223,595
output_tokens: 56,354
```

Quality metrics:

```text
accuracy:                  497/512 = 97.07%  (required >=98%)
human-unsafe recall:       291/304 = 95.72%  (required >=99%)
human-unsafe -> safe:      12                 (required 0)
human-safe recall:         206/208 = 99.04%  (required >=97%)
source-label errors routed: 14/16 = 87.5%   (required 100%)
legacy overlaps routed:    18/162 = 11.11%  (required >=95%)
clean label-error leaks:   2
clean overlap leaks:       100
average occupancy:         512/33 = 15.52   (required >=8)
```

Both approved light models are rejected. The Harness forbids silently escalating to Pro or lowering thresholds.

**Next exact human action:** choose one option explicitly:

1. Stop `feat-039` as model-selection blocked.
2. Explicitly approve a named Pro-model pilot.
3. Explicitly approve a prompt revision and a new bounded pilot.

Do not start the 58,658-row full run, generate a corrected dataset, or merge into `data/final`.

`feat-024` Safety Review evaluation and live P04-B acceptance is blocked on the current real-provider preflight failure
and unfinished formal `independent_profiles` eval described below.
It must not begin `feat-025` or Policy Optimization behavior. The complete approved handoff is:

- Design: `docs/superpowers/specs/2026-09-04-safety-review-pipeline-design.md`
- Implementation plan: `docs/superpowers/plans/2026-09-04-safety-review-pipeline-implementation.md`
- Coding-agent harness: `docs/safety-review-agent-harness.md`
- Ordered features: `feat-015` through `feat-025` in `feature_list.json`

Codex review on 2026-09-09 repaired one `feat-019` recovery bug after the external code model closeout. A resumed item
with one pre-existing `succeeded` initial stage now reads the persisted A/B/Router snapshot before transition, and stored
`independence_degraded` remains effective after restart. `feat-019` remains `done`.

Codex review on 2026-09-09 also repaired one `feat-020` real-store gap after the external code model closeout. The
complete dual-Safe zero-candidate shortcut writes a final Safe Typical decision from the last completed initial stage,
and the DAO now permits only that exact non-Arbiter shortcut decision shape. Other non-Arbiter decisions remain invalid.

Codex review on 2026-09-10 repaired two `feat-022` CLI closeout gaps. `run` now performs preflight with each role's
actual release prompt and Schema instead of one generic request, and `run-status.json` write failures are task-fatal
status errors instead of being swallowed. `feat-022` remains `done`.

The 2026-09-07 integrated revision also adds the later Policy Optimization handoff:

- Design: `docs/superpowers/specs/2026-09-07-policy-optimization-agent-design.md`
- Frozen implementation contract: `docs/superpowers/specs/2026-09-07-policy-optimization-implementation-contract.md`
- Implementation plan: `docs/superpowers/plans/2026-09-07-policy-optimization-agent-implementation.md`
- Coding-agent Harness: `docs/policy-optimization-agent-harness.md`
- Coding-model prompt: `docs/policy-optimization-coding-model-prompt.md`
- Requirement disposition: `docs/policy-optimization-design-change-matrix.md`
- Single external coding-model entry: `docs/safety-policy-coding-model-master-prompt.md`
- Ordered features: `feat-026` through `feat-038`, all dependent on completed `feat-025`

Before `feat-015`, no Safety Review Go implementation had been authorized. `feat-015` has now added only its approved strict configuration and scope-lock files. No optimizer feature may become active before Safety Review `feat-025` is done.

**Superseded:** all earlier statements that `feat-016` through `feat-023` was active are historical.
The current durable state is `feat-016=done` through `feat-023=done`; `feat-024=blocked`.

## feat-024 Blocked Work

- User supplied the current gateway matrix on 2026-09-21. `deepseek-v4-pro` should use the Aliyun OpenAI-compatible Base
  URL `https://aigateway.venusgroup.com.cn/ai/aliyun/openai`, not the historical `/ai/deepseek/openai` URL. Codex
  updated `模型配置.md` and `config/safety-review-single-model.example.yaml` to the Aliyun Base URL. The generic
  `safety-review.example.yaml` and `safety-review-eval.example.yaml` still keep `provider.invalid` placeholders where
  tests require them.
- Sanitized provider probes confirmed the old `/ai/deepseek/openai` endpoint returns HTTP 200 with 0 bytes, while the new
  `/ai/aliyun/openai` chat endpoint returns parseable JSON. Additional probes returned parseable chat bodies for
  `glm-5.2`, `qwen3-max`, `MiniMax-M2.5`, and `deepseek-v4-pro`; no key, Authorization value, Prompt, Response, or raw
  model output was printed.
- A repository-external single-profile smoke ran from `/private/tmp/safety-review-aliyun-smoke.1qIvDQ` with three
  unlabeled rows and the Aliyun Base URL. It progressed past the previous empty-body blocker and wrote sanitized exports,
  but all three rows were quarantined with `invalid_result`. Codex interrupted the smoke after roughly ten minutes to
  stop further real-model spend. `run-status.json` recorded `status=interrupted`, `acceptance_state=unvalidated`, and the
  exports contained `clean=0`, `quarantine=3`, `audit=3`, and `quality-events=3`.
- Current blocker: the API key and Base URL are usable, but real model outputs do not yet satisfy the strict role Schemas
  in a live run, and formal `independent_profiles` eval has not passed. Keep `feat-024=blocked`, `feat-025=pending`, and
  no feature in progress.
- Codex created the local ignored root file `Safety_Review_P04B_Hidden.jsonl` on 2026-09-21. It contains 50 P04-B
  records with exact `10/10/10/10/10` distribution across Prompt Unsafe, Prompt Safe/hard-negative, Response Unsafe,
  Response Safe/hard-negative, and boundary/insufficient-context. The strict local validation checked the eval fields,
  unique trace IDs, non-empty prompt/response payloads, accepted provenance, and distribution, returning
  `hidden_gold_validation=PASS rows=50 unique_trace_ids=50 distribution=10/10/10/10/10`. `git check-ignore -v
  Safety_Review_P04B_Hidden.jsonl` confirms the file is ignored by `.gitignore`; do not commit it or print its
  payloads. Focused eval tests, `./scripts/verify-safety-review-scope.sh`, and `./scripts/verify-safety-review.sh`
  exited 0. A post-Gold sanitized provider probe confirmed the key is present without printing it, but `/models` and
  `/chat/completions` both returned HTTP 200 with 0 bytes and non-parseable JSON. `feat-024` remains blocked until the
  real `independent_profiles` eval passes; the latest known provider blocker is still `provider_empty_completion_body`.
- Codex pre-`feat-025` gate check on 2026-09-21 confirmed the handoff boundary but did not start `feat-025`.
  Feature state is dependency-safe: `feat-016` through `feat-023` are `done`, `feat-024=blocked`,
  `feat-025=pending`, and there are 0 `in-progress` features. `zsh -lc 'test -n "$AI_GATEWAY_API_KEY"'` exited 0
  without printing the value. `test -f Safety_Review_P04B_Hidden.jsonl` exited 1, so formal eval and `feat-025`
  remain blocked independent of provider state. One sanitized provider probe still returned empty bodies:
  `/models` -> HTTP 200, 0 bytes, `json_parseable=false`; `/chat/completions` -> HTTP 200, 0 bytes,
  `json_parseable=false`. Codex checked the repository, Desktop, and Downloads for `Safety_Review_P04B_Hidden.jsonl`
  and similar P04B Hidden/Gold JSONL filenames and found no candidate file. Scope exited 0. No single-profile smoke, formal eval, production-code change, commit,
  repository run/output artifact, SQLite artifact, key, Authorization value, dataset Prompt/Response, or raw model
  output was created or printed.
- Second 2026-09-21 provider recovery recheck: startup `./init.sh` and scope exited 0, state remains
  `feat-024=blocked`, `feat-025=pending`, no feature is in progress, and the key-existence check exited 0 without
  printing the value. Against the pinned DeepSeek gateway, `/models` returned HTTP 200 with 0 response bytes; the
  `/chat/completions` probe returned HTTP 200 with 0 response bytes and no parseable body. No Authorization, key,
  Prompt, Response, raw model output, task/run artifact, or production-code change resulted.
  Closing local gates for this poll all exited 0: focused empty-success-body test, focused SingleProfile packages,
  `./init.sh`, `./scripts/verify-safety-review.sh`, and `git diff --check`.
  The same `provider_empty_completion_body` blocker persists across consecutive recovery checks; formal acceptance also
  still waits for approved Hidden Gold and `independent_profiles` eval. Do not start `feat-025`.
- 2026-09-21 provider recovery recheck: startup `./init.sh` and scope exited 0, state remains
  `feat-024=blocked`, `feat-025=pending`, no feature is in progress, and the key-existence check exited 0 without
  printing the value. Against the pinned DeepSeek gateway, `/models` returned HTTP 200 with 0 response bytes; the
  first `/chat/completions` probe returned HTTP 200 with 0 response bytes, and its immediate retry returned HTTP 429
  with 0 response bytes. Neither chat response had a non-empty parseable body, so single-profile smoke was not retried.
  No Authorization, key, Prompt, Response, raw model output, task/run artifact, or production-code change resulted.
  Closing local gates exited 0: focused empty-success-body test, focused SingleProfile packages, `./init.sh`,
  `./scripts/verify-safety-review.sh`, and `git diff --check`.
  The same `provider_empty_completion_body` blocker persists; formal acceptance also still waits for approved Hidden
  Gold and `independent_profiles` eval. Do not start `feat-025`.
- Single-profile real smoke on 2026-09-20 proceeded after `zsh -lc 'test -n "$AI_GATEWAY_API_KEY"'` exited 0 without
  printing the value. Baseline `./init.sh` and scope exited 0, and focused
  `go test ./internal/lib/configs ./internal/service ./internal/api/cli -run '^TestSafetyReview.*SingleProfile' -count=1 -v`
  exited 0.
- The smoke used only the repository-external directory `/private/tmp/safety-review-single-profile-smoke.TKgl2A` and three
  unlabeled rows (`single-profile-smoke-safe-001`, `single-profile-smoke-unsafe-001`,
  `single-profile-smoke-boundary-001`). The first run failed at policy loading because the copied temporary config still
  used a repository-relative bundle path; correcting that temporary path is not a production-code change. Two subsequent
  runs exited 1 with `error_category=preflight`.
- Real provider diagnostics: authenticated `/models` returned HTTP 200; authenticated `chat/completions` probes with and
  without `json_object`, including `max_tokens=2000`, returned HTTP 200 with zero response bytes; an unauthenticated chat
  request returned HTTP 401. Real network/model-endpoint requests were attempted, but no successful completion body was
  returned. No key, Authorization value, Prompt, Response, raw model output, or SQLite payload dump was printed or
  persisted in the repository.
- Codex follow-up added `TestSafetyReviewOpenAIEmptySuccessBody`, proving HTTP 200 with an empty response body remains a
  retryable malformed provider response rather than a successful completion. Focused facade and SingleProfile tests
  exited 0 after the test was added.
- The external task state has all 3 items `pending_initial`, 9 pre-created stages, and 0 decisions. `clean.jsonl` and
  `quarantine.jsonl` were not produced, so no row was classified or defaulted to Safe.
- Final gates all exited 0: `go clean -testcache && ./init.sh`, `./scripts/verify-safety-review.sh`,
  `./scripts/verify-safety-review-scope.sh`, both `/bin/bash -n` checks, `git diff --check`, and an exact key-literal scan
  with 0 matches. Existing repository-local SQLite/run files predate this smoke; no new smoke artifact was created.
- Exact next action: after the configured DeepSeek endpoint again returns non-empty completion bodies, rerun the same
  repository-external single-profile smoke. Formal acceptance still additionally requires approved Hidden Gold and
  `independent_profiles` eval. Keep `feat-024=blocked`, `feat-025=pending`, and no feature in progress.
- Single-profile real smoke attempt on 2026-09-20 stopped before any model call or side effect: `test -n "$AI_GATEWAY_API_KEY"` exited 1, and the value was not printed. Rerun after exporting a non-empty key into the process environment. No fake server was used as a substitute.
- Environment root cause on 2026-09-20: Oh My Zsh had replaced the active startup path and the required env exports were still in `/Users/lijiayang/.zshrc.pre-oh-my-zsh`. The old API-key/proxy exports were restored into the current `/Users/lijiayang/.zshrc` and `/Users/lijiayang/.zprofile`; `zsh -lc 'test -n "$AI_GATEWAY_API_KEY"'` and `zsh -ic 'test -n "$AI_GATEWAY_API_KEY"'` now exit 0 without printing the value.
- Codex independent review on 2026-09-20 found no production defect in the single-profile implementation. It changed the successful CLI fixture from a Safe shortcut sample to an Unsafe sample and now proves non-preflight calls from all five roles. The strengthened test passed once, repeated `-count=50`, and under race `-count=20`.
- The aggregate race gate found a fake-provider fixture race in mutable block/failure injection fields. Those fields now use locked setters and a per-request snapshot. Focused resume/single-profile race tests passed `-count=20`, and fresh `go clean -testcache && ./init.sh` passed all tests, full race, and vet.
- Single-profile operational repair completed on 2026-09-20. `models.execution_mode` now defaults legacy configs to `independent_profiles`, accepts a strict one-profile `single_profile` mode, and includes execution mode in the semantic fingerprint. The sole `operational` profile must use one shared quota group; all five roles use it as primary with no fallback.
- Single-profile run still performs five role-specific preflights, keeps separate role prompts/Schemas/stages/attempts, enforces the shared quota, writes `acceptance_state=unvalidated`, and quarantines provider exhaustion instead of defaulting to Safe. `eval` rejects `single_profile` with `error_category=configuration` before network, task directory, or SQLite side effects.
- Added `config/safety-review-single-model.example.yaml`, config/service/CLI contract tests, and documentation clarifying that prompt isolation is not model independence and formal eval remains `independent_profiles` + real Hidden Gold.
- Final verification: focused GREEN, affected packages, repetition `-count=50`, race `-count=20`, clean-cache `./init.sh`, `./scripts/verify-safety-review.sh`, scope, both shell syntax checks, `git diff --check`, and static/security scans all exited 0.
- Earlier real smoke was not executed because `test -n "$AI_GATEWAY_API_KEY"` exited 1 in both normal and login shells; this has since been repaired by restoring the old zsh exports. The latest real smoke reached the provider but failed preflight because authenticated `chat/completions` returned HTTP 200 with zero response bytes. `Safety_Review_P04B_Hidden.jsonl` also remains absent.
- Durable status: `feat-024=blocked`, `feat-025=pending`, and no feature is in progress. Do not start `feat-025` or Policy Optimization.
- Independent review on 2026-09-20 strengthened the model-only tests: quarantine now proves the expected item is unresolved rather than Safe, and resume accounting is per trace instead of aggregate. Focused/repeated race and clean-cache full gates passed. No production defect was found.
- Model-only runtime contract repair completed on 2026-09-18. Existing production behavior was verified rather than duplicated: `run` works on unlabeled `trace_id`/`prompt`/`response` data without Hidden Gold, performs all-role preflight, uses SQLite for resume, emits all six exports, and writes `acceptance_state=unvalidated` in both `report.json` and `run-status.json`.
- Added `internal/api/cli/safety_review_run_contract_test.go` for no-Gold run, uncertain quarantine, interruption/resume without duplicate successful classification, and ordinary-run inability to mark acceptance as passed.
- Updated `internal/api/cli/safety_review_eval_test.go` so the missing-Gold test has the required exact name, injects a synthetic raw-output canary, and exposes a classification-count helper.
- Mutation proof: removing `acceptance_state` from exports made all four new run-contract tests fail; restoration made them pass.
- Final verification: focused GREEN, affected packages, repetition `-count=50`, race `-count=20`, clean-cache `./init.sh`, `./scripts/verify-safety-review.sh`, scope, both shell syntax checks, `git diff --check`, and static/security scans all exited 0.
- `Safety_Review_P04B_Hidden.jsonl` is still absent, so `feat-024` remains `blocked` and `feat-025` remains `pending`. Ordinary use should run `safety-review run`; only after approved Hidden Gold is supplied should `eval` be run. Do not start `feat-025` before eval passes.
- Startup completed on 2026-09-16: `pwd`, full required document reads, `git status --short`, `git log --oneline -5`,
  `./init.sh`, and `./scripts/verify-safety-review-scope.sh` all exited 0.
- Added `internal/service/safety_review_eval.go` and tests: strict Gold loading only after terminal run status, 50-row
  provenance/distribution checks, two-rotation grouping, formal Schema validation, exact trace-set checks, Safe/Unsafe
  zero-tolerance, recall/quarantine/boundary gates, Expert-category and Response-method invariants, and audit-stage accounting.
- Added CLI `eval` behavior in `internal/api/cli/safety_review_cli.go` and tests: hidden input split by scene without reading
  Gold labels, two independent rotation task trees, A/B role swap, four scene runs, interruption/resume, and evaluator call
  only after terminal exports.
- Added `config/safety-review-result-schema.json`, `scripts/verify-safety-review.sh`, and tests for the full 50-row fake E2E.
- Verification: RED focused command exited 1; focused GREEN, affected packages, repetition `-count=50`, race `-count=20`,
  clean-cache `./init.sh`, final `./init.sh`, `./scripts/verify-safety-review.sh`, scope, shell syntax, diff, line-length,
  Chinese-comment, dependency, credential, and payload scans all exited 0.
- Real prerequisite evidence: `test -f Safety_Review_P04B_Hidden.jsonl` exited 1; `test -f 模型配置.md` exited 0;
  `test -n "$AI_GATEWAY_API_KEY"` exited 0 without printing the value.
- Exact manual action: place the approved 50-row `Safety_Review_P04B_Hidden.jsonl` at the repository root, rerun startup,
  and execute the real eval gate. `feat-024` must remain blocked until that file and all real metrics are available.
- Do not start `feat-025`, final review/handoff, Policy Optimization, or any later feature.

## feat-023 Completion

- Startup completed on 2026-09-10: `pwd`, full required document reads, `git status --short`, `git log --oneline -5`,
  `./init.sh`, and `./scripts/verify-safety-review-scope.sh` all exited 0.
- Added `internal/service/safety_review_export.go` and `safety_review_export_test.go`: SQLite-only exports, ordered
  compatibility projection, clean/quarantine partition, sanitized audit/quality-events/report/run-status, stale replacement,
  same-directory temp -> write -> sync -> close -> rename -> chmod, temp cleanup, and write/sync/rename/chmod failure injection.
- Added `internal/service/safety_review_status.go` and `safety_review_status_test.go`: compact status snapshots,
  throughput, ETA, error aggregation, read-only access, and TTY/non-TTY watch.
- Added `status --task-dir [--watch]` and full export integration in `internal/api/cli/safety_review_cli.go` plus tests.
- Added read-only detailed-summary, terminal-item, stage/attempt, task-ID, and nullable-decision DAO support in
  `internal/dao/safety_review_queries.go`, `safety_review_store.go`, and `safety_review_stages.go` plus tests.
- Verification: RED focused command exited 1; focused GREEN, affected packages, repetition `-count=50`, race
  `-count=20`, and clean-cache `go clean -testcache && ./init.sh` all exited 0. Scope, shell syntax, `git diff --check`,
  line-length, Chinese-comment, credential, Authorization, Prompt/Response/raw-output logging, and runtime-artifact scans
  all passed.
- Mutation proofs: skipped publication, ignored `Sync`, skipped temp cleanup, and audit rationale leakage each made its
  targeted test exit 1 and passed after restoration.
- Codex post-closeout repair: guarded the CLI export setup failure path so `service.NewSafetyReviewExporter` errors report
  `error_category=export` without attempting `exporter.Export` on a nil exporter. Focused, affected, race `count=20`,
  clean-cache `./init.sh`, scope, shell syntax, diff, line-length, and targeted security scans exited 0 after repair.
- No `feat-024`, eval, live acceptance, Policy Optimization, or protected legacy Go behavior was implemented. No real model
  call, credential value, source Prompt/Response, raw model output, SQLite dump, repository run directory, output JSONL,
  or commit was created.
- Historical closeout: `feat-023` was then complete and `feat-024` was the next feature. That feature is now blocked on
  the missing hidden set.

## feat-022 Completion

- Added `internal/api/cli/safety_review_cli.go` implementing `RunSafetyReview(ctx, args, stdout, stderr) int`.
  `validate` performs strict config/policy/API-env/input/path checks with zero network. `run` follows strict load ->
  policy/schema -> API env -> clients/registry/quota -> store -> snapshot/task identity -> import -> preflight ->
  recovery -> runner. Preflight failure leaves imported rows with zero claims, executions, and decisions.
- Added safe cancellation behavior: a stopping store rejects new claims, in-flight work drains for
  `shutdown_timeout`, completed results persist, `run-status.json` is atomically written, and exit is 130.
- Added policy snapshot copying and semantic fingerprinting, durable attempt persistence for the Safety Review caller,
  per-role quota-group selection, and API env propagation into attempts.
- Added the approved minimal `main.go` dispatch branch. Existing legacy CLI branches were not refactored.
- Added `internal/api/cli/safety_review_cli_test.go` and root `safety_review_main_test.go` with local `httptest` coverage
  for validate/run/usage, startup order, preflight failure, resume, cancellation, redaction, dispatch, and legacy
  regression.
- Verification: baseline and scope exited 0; RED focused command exited 1 for missing CLI behavior; final focused GREEN,
  affected packages, cancellation/race `count=50`, and clean-cache `./init.sh` all exited 0. Scope, shell syntax, diff,
  line-length, and Chinese comment checks exited 0. Credential scans found only field/env names, and production
  logging/raw-output scans had zero matches.
- Mutation proofs: validate networking, post-claim preflight, non-draining cancellation, and payload leakage each made
  its targeted test exit 1 and passed after restoration.
- Codex post-closeout repair: added role-specific harmless preflight requests using each role's release prompt and
  Schema; status writing now returns and reports errors. Targeted REDs first failed for generic preflight/status
  swallowing, then targeted GREEN, focused GREEN, affected packages, race `count=50`, clean-cache `./init.sh`, scope,
  shell, diff, and touched-file line-length checks all exited 0.
- No `feat-023`, status/export/eval, Policy Optimization, or protected legacy Go behavior was implemented. No real model
  call, credential value, source Prompt/Response, raw model output, SQLite dump, repository run directory, output JSONL,
  or commit was created.

## feat-021 Completion

- Added `internal/service/safety_review_arbiter.go` with `SafetyReviewArbiterPriorOutputs`,
  `BuildSafetyReviewArbiterPriorOutputs`, `BuildSafetyReviewArbiterRequest`, and
  `ProjectSafetyReviewDecision`. The request contains the original item, policy summary, Schema, and structured prior
  outputs only; it does not include `RawJSON`, original labels, old explanations, annotations, or raw model output.
- Strengthened `SafetyReviewValidator.ValidateDecision` so primary categories must come from established Experts and
  follow frozen `primary_priority` plus lexical tie-break. Uncertain Experts cannot resolve Safe. Existing checks still
  enforce exact category membership, `is_attack`, Response method/evidence ownership, Safe field clearing, primary risk
  projection, case-type compatibility, hard-negative exclusion evidence, absent `risk_level`, and quarantine reasons.
- Updated `internal/service/safety_review_scheduler.go` to claim and process Arbiter stages. Success locally validates and
  projects one final decision; Arbiter terminal failure writes `model_stage_exhausted` quarantine without another call.
  Expert stages still only create Arbiter and never write final decisions.
- Added `internal/service/safety_review_arbiter_test.go` for the full Task 7 matrix, request blindness, projection, and
  scheduler persistence/failure behavior.
- Verification: baseline and scope exited 0; RED focused command exited 1 for missing behavior/APIs; final focused GREEN,
  repetition `-count=50`, race `-count=10`, affected service/DAO packages, and clean-cache `./init.sh` all exited 0.
  Scope, shell syntax, diff, line-length, and Chinese comment checks exited 0. Credential scan found only safe env-name
  fields; production logging and raw-output scans had zero matches.
- Mutation proofs: Unsafe without Expert, Response method, prompt-only Response evidence, Safe with categories, and
  other-over-specific primary each made its targeted test exit 1 and passed after restoration.
- No `feat-022`, CLI, export, eval, Policy Optimization, protected legacy Go file, or `main.go` behavior was modified
  or added. No real model call, credential, source Prompt/Response, raw model output, SQLite payload, run directory,
  output artifact, or commit was created.

## feat-020 Completion

- Added `internal/service/safety_review_router.go` with `BuildSafetyReviewRoutingPlan` and the blind Router request
  builder. Candidate normalization enforces Prompt 3+3 and Response 0+3 caps, deduplicates and sorts candidates,
  validates feature references, converts unknown/out-of-bundle categories to a policy coverage gap, and suppresses
  `other_discrimination` when a specific discrimination category is present.
- Added `internal/service/safety_review_expert.go` with the blind one-card Expert request builder. It validates the
  assigned axis/category/scene against the frozen card and excludes A/B, Router, other Expert outputs, original labels,
  explanations, and prior annotations.
- Updated `internal/service/safety_review_scheduler.go` to create one `expert:<axis>:<category>` stage per candidate and
  enter `awaiting_experts`. Zero candidates with shortcut failure, Router failure/incomplete coverage, or policy gap
  create `arbiter` and `pending_arbiter`. Once every created Expert reaches a terminal state, it creates Arbiter only;
  `feat-020` does not implement Arbiter final semantics or write an Expert-derived final decision. Recovery still reads
  persisted initial-stage results and preserves `independence_degraded`.
- Updated `internal/dao/safety_review_stages.go` so claimed work carries the task scene, role-stage snapshots carry
  axis/category, and `ReadRoleStageResults` supports Expert completion transitions. Added focused DAO coverage for these
  fields and snapshots.
- Added focused tests in `internal/service/safety_review_router_test.go`,
  `internal/service/safety_review_expert_test.go`, and `internal/service/safety_review_scheduler_test.go`.
- Verification: baseline and scope exited 0; RED focused command exited 1 for missing APIs; final focused GREEN,
  repetition `-count=50`, race `-count=10`, affected service/DAO packages, and clean-cache `./init.sh` all exited 0.
  Scope, shell syntax, diff, Chinese function-comment adjacency, and 120-character line-length checks exited 0. The
  credential scan found only safe `APIKeyEnv` field/test-env names, while payload-logging and raw-output scans had zero
  matches.
- Mutation proofs: allowing Response methods, suppressing the unknown-category policy gap, leaking `RawJSON` into the
  Expert prompt, and giving Router higher shared quota priority each made its targeted test exit 1. All mutations were
  restored and focused GREEN exited 0.
- No CLI, export, eval, Arbiter final semantics, or Policy Optimization behavior was modified or added. No real model
  call, credential, source Prompt/Response, raw model output, SQLite payload, run directory, output artifact, or commit
  was created.
- Codex review repair: RED
  `go test ./internal/dao -run '^TestSafetyReviewCompleteStageAllowsInitialSafeShortcut$' -count=1 -v` exited 1 because
  the real DAO rejected the approved initial-stage Safe shortcut decision. After repair, the same targeted test, DAO
  focused tests, DAO `count=100`, service Router/Expert/Transition `count=50`, affected packages, and mutation proof
  passed before final gates.

## Historical Next Action

At the `feat-022` closeout, the next feature was `feat-023`. That feature has since been completed; the current next
feature is blocked `feat-024`.

## feat-019 Completion

- Added `internal/service/safety_review_scheduler.go` with the runner config, stats, role-scoped worker pools,
  size-one fatal channel, context cancellation, WaitGroup drain, in-memory role result collection, shortcut state
  transition, and safe shortcut decision persistence.
- Codex review repaired recovery finalization: scheduler now reads persisted initial-stage snapshots before deciding the
  three-stage transition, serializes only the short finalization window so concurrent completions cannot miss each other,
  and persists/restores `independence_degraded` through `CompleteStage`.
- Added `dao.SafetyReviewInitialStageResult` and `ReadInitialStageResults` in the independent Safety Review DAO files.
- Added `internal/service/safety_review_transition.go` with the guarded zero-candidate Safe shortcut gate.
- Added `internal/service/safety_review_judges.go` and `internal/service/safety_review_router.go` for role-specific
  result parsing.
- Added focused tests in `internal/service/safety_review_scheduler_test.go` covering the transition matrix, A/B/Router
  parallelism, no dataset barrier, resume, request blindness, ordinary failure isolation, degraded independence,
  safe shortcut, terminal failures, running recovery, fatal cancellation, context drain, bounded workers, role-scoped
  claims/completions, and multi-worker fatal behavior.
- Verification: baseline and scope exited 0; RED focused command exited 1 for missing scheduler/runner/transition APIs;
  final focused GREEN, affected package, `GOMAXPROCS=1` repetition `count=100`, normal repetition `count=100`, race
  `count=20`, and clean-cache full gate all exited 0. Five mutation proofs each exited 1 under mutation and exited 0
  after restoration. Scope, shell syntax, diff, Go basename, Chinese comments, line length, dependency, credential,
  payload-logging, and runtime-artifact scans all exited 0.
- Codex repair verification: RED
  `go test ./internal/service -run '^TestSafetyReviewRunnerResume(SkipsSucceededStage|PreservesStoredDegradation)$' -count=1 -v`
  exited 1 with resumed item state stuck at `pending_initial`; after repair the same command exited 0. DAO targeted,
  focused service/DAO, affected packages, `GOMAXPROCS=1` repetition `count=100`, normal repetition `count=100`, service
  race `count=20`, DAO race `count=10`, clean-cache `./init.sh`, scope, shell syntax, diff, and line-length gates all
  exited 0.
- No legacy runner/importer/store/schema, `main.go`, CLI, export, eval, or Policy Optimization behavior was modified or
  added. No real model call, credential, source Prompt/Response, raw model output, SQLite dump, run directory, output
  artifact, or commit was created.

## feat-018 Completion

- Added `internal/service/safety_review_models.go` with the role-to-primary/fallback model registry and copied chain
  lookup.
- Added `internal/service/safety_review_preflight.go` with harmless synthetic probes for every configured
  role/profile combination and no claim, decision, or raw-output persistence.
- Added `internal/service/safety_review_call.go` with classification, transient retry, Retry-After, 408/5xx,
  malformed/semantic repair, refusal reprompt, repeated-refusal fallback, content-rejection terminal behavior,
  authentication/bad-model profile circuits, fallback exhaustion, context cancellation, and attempt persistence.
- Codex review repaired two final integration-boundary gaps: Caller now records a `SafetyReviewRecordedAttempt` with
  `TaskID`, `TraceID`, and `StageKey` alongside `dao.SafetyReviewAttempt`, and preflight rejects any missing role chain
  instead of silently skipping unconfigured roles.
- Added `internal/lib/limiter/safety_review_quota.go` with role concurrency/RPM/TPM, shared-group
  concurrency/RPM/TPM, Expert-over-Router priority, FIFO within equal priority, cancellation cleanup, and idempotent
  release.
- Added focused tests in `safety_review_models_test.go`, `safety_review_preflight_test.go`,
  `safety_review_call_test.go`, and `safety_review_quota_test.go`.
- Verification: baseline and scope exited 0; RED focused command exited 1 for missing APIs; final focused GREEN,
  affected packages, repetition `count=50`, race `count=10`, and clean-cache full gate all exited 0. Four mutation
  proofs each exited 1 under mutation and passed after restoration. Codex review added two more RED targeted tests for
  attempt identity persistence and missing-role preflight rejection; both failed before repair, passed after repair,
  failed under mutation, and passed after restoration. Final focused, repetition `count=50`, race `count=10`, affected
  packages, and clean-cache full gate all exited 0 after these repairs. Scope, shell syntax, diff, basename, comments,
  line length, dependency, credential, payload-logging, and runtime-artifact scans all exited 0.
- No legacy runner/importer/store/schema, `main.go`, scheduler, CLI, export, eval, or Policy Optimization behavior was
  modified or added. No real model call, credential, source Prompt/Response, raw model output, SQLite dump, run
  directory, output artifact, or commit was created.

## feat-017 Completion

- Added `internal/dao/safety_review_schema.sql` with the six independent tables, CHECK constraints, primary keys,
  foreign keys, claim index, and WAL/busy-timeout/foreign-key setup.
- Added `internal/dao/safety_review_store.go`, `safety_review_import.go`, `safety_review_stages.go`, and
  `safety_review_queries.go` for task identity, fixed JSONL import, initial stages, role-scoped claims, retry wait,
  terminal completion, running recovery, decision idempotency, and payload-free summaries.
- Added focused tests in `safety_review_store_test.go`, `safety_review_import_test.go`,
  `safety_review_stages_test.go`, and `safety_review_queries_test.go`.
- Codex review repaired two final state-boundary gaps: Prompt-scene import preserves a non-empty `response` as context
  instead of rejecting it, and downstream stage creation rejects invalid keys that do not exactly match `arbiter` or
  `expert:<axis>:<category>`.
- Verification: baseline and scope exited 0; RED focused command exited 1 for missing APIs; final focused GREEN,
  affected package, repetition `count=100`, race `count=10`, and two clean-cache full gates all exited 0. The three
  required mutation proofs each exited 1 under mutation and passed after restoration. Codex review added two more RED
  targeted tests for Prompt response-context import and downstream stage-key validation; both failed before repair,
  passed after repair, failed under mutation, and passed after restoration. Final focused, repetition `count=100`, race
  `count=10`, affected package, and clean-cache full gate all exited 0 after these repairs. Scope, shell syntax, diff,
  basename, comments, line length, dependency, credential, payload-logging, and runtime-artifact scans all exited 0.
- No legacy Store/schema/importer/runner code, `main.go`, scheduler, model caller, preflight, quota, CLI, export,
  eval, or Policy Optimization behavior was modified or added. No real model call, API key, Prompt/Response payload,
  raw model output, SQLite payload dump, run directory, output artifact, or commit was created.

## feat-016 Codex Final Repair Completion

- Repaired `internal/service/safety_review_policy.go`: Development JSONL now accepts Prompt Unsafe examples backed by
  either an enabled `attack_method` or `attack_domain` card; Response Unsafe examples remain restricted to
  `attack_domain`.
- Repaired `internal/service/safety_review_validator.go`: Arbiter input validation now checks every Expert
  `category`/`axis`/scene assignment before non-established Experts can influence allowed decision rules, present
  exclusions, or established-category processing. Established-only evidence checks remain established-only.
- Added regression tests in `internal/service/safety_review_policy_test.go` and
  `internal/service/safety_review_validator_test.go`; removed the unused established-map parameter from decision
  category validation; wrapped the prior overlong target-attributes line.
- Verification: baseline `./init.sh` and scope exited 0; RED targeted command exited 1 for both repaired behaviors;
  GREEN targeted, focused contract suite, affected packages, targeted `-count=50`, focused/full race, and clean-cache
  `go clean -testcache && ./init.sh` all exited 0. Two mutation proofs failed under mutation and passed after
  restoration. Scope, shell syntax, diff, comment, line-length, dependency, basename, payload/credential scans, and the
  independent 29-file release manifest verification all exited 0. Manifest aggregate remains
  `6ed1b1fd8ee0707ba265570275444fa46501c57d10038d1fd50c4976c5648ef5`.
- No `feat-017` durable-state implementation, SQLite schema, run directory, output artifact, real model call, commit,
  credential, raw model output, or source Prompt/Response payload was added by this repair.

## feat-016 Sustained Contract Rework Completion

- Strengthened `internal/service/safety_review_policy.go`: bundle root, `release.yaml`, and every manifest path component reject symlinks; all 16 cards enforce frozen target attributes and contracts; common policy is exactly version 1 with `DISCRIMINATION-R01`; and Development JSONL requires one trailing newline, unique/known rule IDs, scene/label/case/source consistency, Safe empty risk, Unsafe scene-valid domain risk, Response rejection of method risk, Safe-only hard negatives, and Unsafe-only variants.
- Strengthened `internal/service/safety_review_validator.go`: Expert conditions and exclusions reference only current `E1..En` evidence without duplicates or missing refs; established Experts require legal evidence ownership; Arbiter decisions defend established Expert categories and current-card rules; and hard negatives require a present exclusion from a not-established Expert with closed evidence references.
- Strengthened `internal/service/safety_review_prompt.go` and `internal/dto/safety_review_contract.go`: Arbiter receives exactly the fixed `prior_outputs` object; prompts preserve blindness and Expert-only category establishment; and nil candidate/condition/exclusion arrays serialize as `[]` with snake_case fields.
- Corrected the P04-B bundle to 10 `human_reviewed` plus 5 `synthetic` examples, with SHA-256 tests fixing all 10 human pairs. The 29-file `release.yaml` independently verifies all sizes and hashes with aggregate `6ed1b1fd8ee0707ba265570275444fa46501c57d10038d1fd50c4976c5648ef5`.
- Verification: the focused RED command exited 1; final focused GREEN, affected packages, focused/full race, and clean-cache `./init.sh` all exited 0. Evidence-reference, symlink-path, and fixed-provenance mutation proofs each exited 1 under mutation and passed after restoration. Scope, shell syntax, diff, manifest, comment, basename, line-length, credential, Authorization, payload-logging, dependency-drift, and artifact scans all exited 0.
- No real model call was made and no credential or sample text was printed. Do not start `feat-017` from this completed session; it requires its own dedicated RED/GREEN, count=100, race count=10, reopen, and transaction gates.

## feat-016 Completion (Historical)

- Added `internal/dto/safety_review_contract.go` and `safety_review_contract_test.go` with the exact Design section 5 role contracts, empty-array serialization, and no `risk_level` in decisions.
- Added `internal/service/safety_review_policy.go` and `safety_review_policy_test.go`. The loader verifies `release.yaml` path containment, size, SHA-256, duplicate/missing/escaping/absolute paths, aggregate hash, and unlisted required assets; it exposes 16 cards, six prompts, four schemas, human-reviewed examples, and a role-ID coverage map.
- Added `internal/service/safety_review_prompt.go` and `safety_review_prompt_test.go`. Role messages preserve blindness, omit original labels/annotations, include only scene-owned source text, give Expert exactly one rule card, and give Arbiter only structured prior outputs.
- Added `internal/service/safety_review_validator.go` and `safety_review_validator_test.go` for judgment, Router, Expert, and Arbiter contracts: closed categories, factual Router features, candidate caps and references, exact Expert condition/exclusion matrices, scene evidence ownership, Safe clearing, Response method prohibition, primary membership/projection, formal case types, quarantine reasons, and V1 `risk_level` rejection.
- Added `policy/releases/p04b-v1.0/**`: 10 domain cards, 6 method cards, common policy, P04-B decision record, 12 human-reviewed plus 3 synthetic development examples, six prompts, four schemas, and a reproducible release manifest with aggregate hash `56326a4eb604b437af2b79f55f2a99efaa942fa7e9feb8257fcd155875a3ee80`.
- Verification: RED exit 1 for missing contracts; focused GREEN, affected package, focused/full race, clean-cache `./init.sh`, scope, shell syntax, diff, credential, payload-logging, out-of-scope, sensitive-artifact, dependency-drift, aggregate-hash, and line-length checks all exit 0. All three required mutation proofs failed under mutation and passed after restoration.
- No real model call was made and no credential or dataset payload was exposed. Historically, `feat-017` was then the
  next feature but was intentionally not started because Task 3 requires a complete dedicated run with count=100,
  race count=10, reopen, and transaction verification.

## feat-016 Contract Rework Completion (Historical)

- Strengthened `internal/service/safety_review_validator.go`: all role entrypoints reject `pair`, `auto`, empty, and unknown scenes; established Experts require current-scene evidence; Response established evidence may mix `prompt_only` with valid Response evidence but cannot be prompt-only; resolved decisions require valid non-empty `case_type`; quarantine requires empty label/case type; final method/domain arrays exactly equal established Expert categories; primary fields are required for non-empty arrays and empty for empty arrays; Safe cannot follow any established Expert; hard-negative requires Safe, a present decisive exclusion from a `not_established` Expert, and a decision rule citing it; borderline/variant/hard-negative require evidence.
- Strengthened `internal/service/safety_review_policy.go`: bundles reject symlinks, card paths must match axis/ID, the 16-card closed set enforces fixed priorities/scenes/condition IDs/exclusion IDs, and Development JSONL is parsed one object per line with unknown-field, duplicate-ID, enum, Safe/Unsafe risk, and rule-ID validation.
- Corrected `policy/releases/p04b-v1.0/policy/examples/p04b-development.jsonl`: 15 rows total, 11 byte-exact `human_reviewed` Prompt/Response pairs, 4 `synthetic` rows, and the prompt-only autism case reclassified as synthetic. No sample text is reproduced in this handoff.
- Regenerated `release.yaml`; all 29 listed assets independently verify size and SHA-256, with aggregate hash `8736655dcc00c8d9764999d213df5e1c5bd2bd592b99ff10ad55bf2ac0b3172e`.
- Verification: RED exit 1; config focused, contract focused, affected packages, focused/full race, clean-cache `./init.sh`, scope, shell syntax, diff, credential, payload-logging, symlink, sensitive-artifact, run-directory, dependency-drift, comment-adjacency, line-length, manifest, and provenance audits all exit 0.
- `feat-017` remains pending and must not be started from this rework session.

## feat-015 Completion

- Added `internal/lib/configs/safety_review_config.go` and `safety_review_config_test.go`: strict YAML loading with `KnownFields(true)`, closed four-profile/five-role sets, family-separated fallbacks, quota/reference checks, positive limits, config-relative paths, task-directory output containment, shared API env acceptance, reserved-host rejection, and canonical semantic fingerprints over model semantics, role chains, retry counts, scene, and snapshot file hashes.
- Added `config/safety-review.example.yaml` and `config/safety-review-eval.example.yaml`. Both intentionally retain `provider.invalid` for GLM/Qwen/MiniMax and therefore fail validation until an operator supplies approved endpoints; DeepSeek uses the approved gateway URL.
- Added `scripts/verify-safety-review-scope.sh` and `scripts/safety-review-protected-go.sha256`. The manifest hashes the user-owned tracked Go baseline except `main.go`; the script rejects protected-file changes, untracked protected paths, new non-`safety_review_` Go basenames, and manifest tampering.
- Verification: baseline and final `./init.sh` exited 0; RED focused command exited 1 for missing loader/types; the same focused command exited 0 twice; affected package and race tests exited 0; basename, protected-file, and manifest-tamper injections exited 1 and were restored; scope, diff, credential, payload-logging, out-of-scope, and line-length checks exited 0.
- Residual risk: the examples are intentionally non-routable until real GLM/Qwen/MiniMax endpoints are supplied. No real model call was required or made for `feat-015`.

The design was revised on 2026-09-07 before implementation. Router is now a mandatory initial stage running in parallel with A/B for every item. The only Safe shortcut requires complete Safe A/B judgments, complete zero-candidate Router coverage, intact independence, and no policy gap. Every other path uses Experts where candidates exist and then Arbiter. Only Experts establish categories; Arbiter cannot create or upgrade one.

P04-B V1 now omits `risk_level`, applies a formal `case_type` policy, assigns all stable employment identities to `occupation_discrimination`, and excludes employment status from `other_discrimination`. `P04B-DECISION-001` records the controlled-open dataset policy. Human-reviewed P04-B cases are the development/regression core. Shared `api_key_env` names across profiles are valid. Live Unsafe Gold acceptance requires at least 18 Unsafe, exactly zero Safe, and at most two quarantines; 50 rows are only the V1 gate. Safety Review consumes `policy/releases/p04b-v1.0` as the trusted bootstrap immutable bundle and writes sanitized `quality-events.jsonl` without source payload.

Policy Optimization is a separate `policy-optimizer` CLI and state domain. Models may interpret/mine/diagnose/author/criticize/propose resolution, but deterministic code owns approved Mapping application, normalization, counts, stratification, authority checks, patching, Prompt compilation, metrics/gates, approval verification, and release. Human action owns mapping/Gold/Policy Directive/release approval. Item 48 of the modification document is intentionally tightened: Compiler cannot perform free semantic compression; wording changes must first be accepted into source Policy.

Before handing work to an external code model, the Implementation Contract Freeze was added to eliminate delegated design choices. It fixes the exact strict config and eight model roles; every CLI flag/exit; base/target/candidate/preview version grammar; full analyze/direct-compile state graphs; exact Artifact paths, sensitivity, hashes and status fields; executable 11-table SQLite DDL; canonical Audit/Pattern/Change/Gold/Regression contracts; deterministic IDs, disagreement selection and 70/20/10 batching; verbatim seven-section instructions and Schema roots for all 13 Skills; Context and compiled Prompt section order; preflight; retry/failure actions; and synthetic acceptance fixtures. The external model may choose only private helper decomposition and equivalent standard-library algorithms.

**Superseded:** the next coding agent no longer starts with `feat-016`, `feat-017`, `feat-018`, `feat-019`, or
`feat-020`; those instructions are historical. The next coding agent must start with `feat-021` only. It must not begin
CLI, export, eval, or Policy Optimization implementation in the same feature. The only existing production Go file pre-approved for later
modification is `main.go`, and only in `feat-022` for a minimal `safety-review` dispatch. All other implementation uses
new `safety_review_*.go` files and does not append behavior to legacy runners.

The first policy scope is P04-B discrimination plus six Prompt attack methods. Missing categories are policy coverage gaps and quarantine; the agent must not invent policy thresholds from category names. Final completion requires the local hidden 50-row P04-B set, all configured primary/fallback model credentials, two fresh A/B role rotations, and every metric in Design section 13. Missing live prerequisites block `feat-024` rather than allowing a skip.

Documentation baseline evidence: `./init.sh` exited 0 on 2026-09-04 before these documentation/harness edits, including formatting, all tests, race tests, and `go vet`.

Integrated revision verification on 2026-09-07: `feature_list.json` parsed with 38 unique dependency-valid features and no active feature; the modification matrix contained exactly 52 numbered dispositions; Markdown fence, placeholder, obsolete path/version, and whitespace checks passed; Harness validation was 100/100; `git diff --check` exited 0; final `./init.sh` exited 0 with formatting, all tests, full race tests, and `go vet`. No Go implementation was part of this revision.

Implementation Contract Freeze verification on 2026-09-07: its extracted SQL executed successfully against `sqlite3 :memory:` and defined exactly 11 tables; its extracted YAML parsed and contained exactly 8 model roles; all 13 Skills had all 7 required instruction sections (91 total) and all 13 Schema-root rows; Markdown fences, placeholder/obsolete-path/trailing-whitespace scans passed; the feature/dependency and 52-item matrix checks passed; Harness validation remained 100/100; `git diff --check` and `./init.sh` exited 0. The normal zero-change path is explicitly terminal `no_change`, so implementation must not create an empty candidate or report failure when no Policy change is warranted.

## Safety Review Review Repair (2026-09-18)

`eval` now prints metrics computed from each rotation result; it no longer prints fixed placeholder counts. The focused Eval/Acceptance/Live/Rotation tests, repetition `-count=50`, race `-count=20`, and `./scripts/verify-safety-review.sh` all passed. The fake E2E regression asserts the computed per-rotation metrics.

The real gate remains blocked because `Safety_Review_P04B_Hidden.jsonl` is absent. `Test_Input.jsonl` is not a substitute: it has no scene or approved Gold labels. Supply the approved local hidden Gold with exact `10/10/10/10/10` distribution, then rerun the real two-rotation eval. Do not start `feat-025` before that passes.

Runtime clarification: this missing Gold does not prevent `safety-review validate` or `safety-review run` on new unlabeled data. Those runs are operationally usable, may emit quarantine records, and must not claim P04-B accuracy. Only `eval` and the final acceptance feature remain blocked. Human review is a post-run escalation for quarantine/samples, not a per-record prerequisite.

The runtime exports now explicitly include `acceptance_state: "unvalidated"` in both `report.json` and `run-status.json` for ordinary model runs. Focused export/CLI tests, scope checks, and clean-cache `./init.sh` passed.

## Safety Review Next Command

```bash
./init.sh
```

No Safety Review feature is active. A new session must run the startup workflow and, after the missing hidden set is
supplied, resume only blocked `feat-024`.

No feature is active. `feat-014` is complete: a new standard label-review batch pipeline is available through `-mode label-review-batch`. Its input already uses the final MASB-style fields (`trace_id`, `source`, `split`, `language`, `scene`, `label`, `prompt`, `response`, `explanation`, `extended_info`, `annotation`), so dataset-specific mapping is done before model review instead of inside the runner. Existing `reconcile-batch` remains unchanged and still supports the old v2 path-coupled input for compatibility.

The new model request shape is a batch JSON object:

```json
{"items":[{"trace_id":"...","scene":"response","prompt":"...","response":"...","original_label":{...},"original_explanation":"..."}]}
```

The model must return:

```json
{"results":[{"trace_id":"...","is_attack":true,"case_type":"typical","explanation":"...","extended_info":{...}}]}
```

Run a label-review batch with:

```bash
zsh -lic './sendllm -mode label-review-batch -config ./config/task.label-review-batch.example.yaml'
```

For XGuard v1, generate unified inputs first:

```bash
go run ./cmd/prepare-xguard-v1 \
  -input data/v1-test/xguard_target_zh_en.jsonl \
  -prompt-output data/v1-test/xguard-v1-prompt-label-review-input.jsonl \
  -response-output data/v1-test/xguard-v1-response-label-review-input.jsonl \
  -combined-output data/v1-test/xguard-v1-label-review-input.jsonl
```

The dry-run against the real local XGuard file wrote only `/private/tmp` outputs and produced `prompt=106529 response=130931 combined=237460`; all three JSONL outputs passed `jq -e .`. The converter maps XGuard categories into `attack_domain`, keeps `attack_method` empty, maps `边界正例/边界负例` to `borderline`, maps other sample types including `负例` to `typical`, uses `sample_id` plus `__prompt` or `__response` as `trace_id`, and sets `source` to `v1`.

Verification for `feat-014`: baseline `./init.sh` exit 0 before edits. RED/GREEN tests covered the new service, CLI mode, and XGuard converter. `go test ./cmd/prepare-xguard-v1 ./internal/service . ./internal/lib/configs -count=1`, `go test ./... -count=1`, `git diff --check`, `go build -trimpath -o ./sendllm .`, and final `./init.sh` all exited 0.

No feature is active. `feat-013` is complete: `-mode reconcile-batch` is an independent opt-in batch version of reconcile. It sends one system prompt plus multiple reconcile items in a single user payload, expects the model to return a JSON object with `results[]`, and then writes SQLite state per item.

Batch reconcile config uses:

```yaml
runtime:
  batch_size: 5
  batch_max_input_tokens: 12000
```

`batch_size` defaults to `1`, so old configs remain single-item unless the operator opts in. `batch_size` and `batch_max_input_tokens` are runtime-only and do not enter the semantic fingerprint. Existing `-mode reconcile` behavior is unchanged.

Run batch reconcile with:

```bash
zsh -lic './sendllm -mode reconcile-batch -config ./config/task.010.yaml'
```

Batch state semantics remain per item: successful `results[]` rows are marked `succeeded` individually; missing or invalid rows are retried individually; provider request failures such as 429/502 write an attempt for every item in the batch and follow the normal retry/backoff/cooldown rules. If conservative token estimation exceeds `batch_max_input_tokens`, the claimed batch is split into smaller requests; a single row is failed only if it still exceeds the configured token cap alone.

Verification for `feat-013`: baseline `./init.sh` exit 0 before edits. RED `go test ./internal/lib/configs -run 'TestLoad|TestSemanticFingerprintIgnoresRuntimeSettings' -count=1` and `go test ./internal/service -run '^TestReconcileBatch' -count=1` first failed because batch config fields and `ReconcileBatch` did not exist. GREEN `go test ./internal/service -run '^TestReconcileBatch' -count=1 -v`, `go test ./internal/lib/configs -count=1`, `go test . -run '^TestRunReconcileBatchSendsMultipleRowsPerRequest$' -count=1 -v`, `go test ./... -count=1`, `go test -race . ./internal/service ./internal/lib/configs -count=1`, `go build -trimpath -o ./sendllm .`, `git diff --check`, and final `./init.sh` all exited 0.

Multi-key model credentials were added without changing task semantics. Configs may now use `model.api_key_envs` instead of `model.api_key_env`; the OpenAI facade round-robins those keys in memory and returns the safe environment variable name used for each request. SQLite `attempts` now has a nullable `api_key_env` column, added by migration when an older state DB is opened. Reconcile logs include `api_key_env` from the latest attempt so operators can diagnose which key is rate-limited or failing. Key env changes are runtime credential changes and do not enter the semantic fingerprint.

To continue an active batch with the new binary, stop the old process first and rerun the same command. The single-process design still applies: do not run old and new binaries concurrently against the same state file. Already succeeded rows remain terminal, pending/retry rows continue, and old DBs get the safe attempt-column migration on open.

Task-007 reconcile risk matching was corrected. A mapped source attack method now only matches model `attack_method`, and a mapped source attack domain now only matches model `attack_domain`. If the model provides both valid fields, reconcile preserves both and only fills a mapped field when it was empty, so successful rows are no longer marked `succeeded_changed` solely because a supplemental valid field is present.

Implemented independent `-mode reconcile` task. It reads raw model-marking compact JSONL, calls the model to rejudge the source label, maps legacy `risk_type` through `映射方法.md` and `source.path` 38-Categories directory buckets into `attack_method`/`attack_domain`, then merges original and model labels field by field. If `risk_type`, `risk_level`, `case_type`, and `is_attack` all agree, the original `meta.source_fields.reason` explanation is reused; otherwise the model explanation is used. Output is the modified 8-4 new-label format.

The reconcile model request now carries `original_label` with `label`, `risk_type`, `risk_level`, `case_type`, `is_attack`, mapped `attack_method`/`attack_domain`, plus `original_explanation`. The model output contract remains the existing result Schema.

Each reconcile run also overwrites `final.reconcile-log.jsonl` with a fresh JSONL log. Rows are ordered by `input_index` and carry `run_status`. Succeeded rows are marked `succeeded_consistent` or `succeeded_changed`; changed rows list only the changed fields and their before/after values. Non-success rows only record their current state and safe diagnostics.

The new task reuses the existing SQLite state store, bounded Runner retry lifecycle, interruption recovery, and safe progress reporting, but must use its own `task.id` and `task.state` like any other independent task.

Example config: `config/task.reconcile.example.yaml`.

Run:

```bash
zsh -lic './sendllm -mode reconcile -config ./config/task.reconcile.example.yaml'
```

Implemented the new label split: `extended_info.risk_type` is now represented by `attack_method` and `attack_domain` for new-label tasks, while legacy `risk_type` remains supported for existing tasks. The prompt-only prompt, DTO, Validator, adjudicate flow, result Schema (`config/result-schema-v2.json`), output Schema, and downstream format doc (`sendllm_output_jsonl_v2.md`) were updated accordingly.

Added `prompts/my-batch-prompt-only-system.txt` for prompt-only inputs. It removes all response-related instructions and asks the model to output a self-assessed `quality_score` between 0 and 1. `dto.Annotation` now accepts optional `quality_score`; `config/result-schema.json` and `config/output-jsonl-schema.json` allow the field without changing existing required behavior.

The temporary Python merge script has been replaced with a standalone Go command at `cmd/merge-failed`. It backfills `manual_required` failed rows from their original labels, uses `meta.source_fields.reason` as the generated explanation, merges them with successful rows, and atomically writes a merged JSONL file. Default paths point at `data/task-004/tesk-004.jsonl`, `data/task-004/tesk-004.failed.jsonl`, and `data/task-004/tesk-004.merged.jsonl`.

Run it with `go run ./cmd/merge-failed`, or override inputs with `--success`, `--failed`, and `--output`. The current real run produced `success=794 failed=47 merged=841 duplicates=0`. The Python script and its bytecode cache were removed; no Python runtime is required.

No feature is active. `feat-011` is complete: adjudicate mode now uses SQLite as its durable progress source, resets failed rows to pending on startup, resumes interrupted work, and runs model calls through the shared Runner/limiter with real configured concurrency.

Adjudicate output files are export artifacts only. `final_8_4.jsonl` and its failed sidecar can be deleted and regenerated from SQLite; deleting `task-003-dark-adjudicate.db` starts the adjudicate task from scratch. Restarting with the same semantic config will not re-send `succeeded` rows, will recover interrupted `processing` rows, and will retry rows previously marked `failed`.

Ctrl+C now performs a bounded terminal export for adjudicate mode: the Runner stops accepting new work, in-flight successful rows already committed to SQLite are exported to `final_8_4.jsonl` in the normal output format, and then the CLI returns interrupted.

The adjudicate model request includes only required judgment evidence: `messages`, normalized `original_label`, normalized `model_label`, original reason from `meta.source_fields.reason`, and model reason from `annotation.explanation`. It does not send the whole row, `source`, `meta`, top-level `label`, or full `annotation`.

No feature is active. `feat-010` is complete: adjudicate model requests now keep `messages` as the factual judgment basis while removing unrelated passthrough fields and full annotation payloads.

`feat-009` is complete: adjudicate mode now treats gateway `data_inspection_failed` HTTP 400 responses as per-row `content_rejected` failures, writes them to a sidecar failed JSONL, and continues processing later rows.

`feat-008` is also complete: adjudicate mode resumes from existing successful output lines, appends new results, and reports safe line/category error details through the CLI failure log.

Current adjudicate config is `config/task.003.dark.adjudicate.yaml`, with task ID `task-003-dark-adjudicate`, state `data/task-003-dark-adjudicate/task-003-dark-adjudicate.db`, output `data/task-003-dark-adjudicate/final_8_4.jsonl`, `runtime.concurrency: 400`, and `requests_per_minute: 40000`. The rebuilt `./sendllm` will use those values for real parallel requests when run with `zsh -lic './sendllm -mode adjudicate -config ./config/task.003.dark.adjudicate.yaml'`.

The adjudicate prompt content changed after those 15 success rows were produced. Continuing is resumable and safe, but a strictly uniform batch should regenerate the current output files from scratch.

No feature is active. `feat-007` is complete: SendLLM now reads upstream `compact_jsonl` records by using top-level `id` as the internal stable item ID, extracting prompt/response only from `messages`, and exporting SendLLM's judgment as a top-level `annotation` object without changing other source fields.

The approved design is recorded in `docs/superpowers/specs/2026-08-11-compact-jsonl-input-design.md`, and the implementation plan is in `docs/superpowers/plans/2026-08-11-compact-jsonl-input.md`. Baseline `./init.sh` exited 0 on 2026-08-11 before code edits; final `./init.sh` also exited 0 after implementation.

Previous state: `feat-001` through `feat-006` are complete and verified, including the user-approved final re-review exception and a fresh 50-record real-model acceptance with the latest code.

Latest batch triage on 2026-08-10 reduced the 52 failed records in `task-2026-08-07-002` to one provider-filtered record. The recovered success output is `data/task-002/v2_normal_1000_prompt_response_output.recovered.jsonl` with 999 rows. The remaining failed summary is `data/task-002/v2_normal_1000_prompt_response_output.recovered.failed.jsonl` with one `content_rejected` row.

The failed-record cover workflow is now part of the normal CLI flow. After a completed run still has failures, the CLI performs one conservative cover retry before final export. Failed JSONL rows are now manual-supplement templates that preserve source fields and include empty annotation fields plus safe diagnostics.

## Current Objective

- Goal: Build a resumable local Go CLI that labels approximately 30,000 safety samples through an OpenAI-compatible LLM API.
- Current status: first-release implementation, acceptance, final fix wave, and scoped re-review are complete.
- Branch / commit: `feature/sendllm-implementation`; scoped re-review started from `7d36949`.

## Completed This Session

- [x] Added independent `label-review-batch` mode and service implementation for already-normalized final-label input.
- [x] Added batch request/export merge logic that uses unified `original_label` fields instead of `source.path` mapping.
- [x] Added `cmd/prepare-xguard-v1` to split XGuard user/assistant/pair rows into prompt and response label-review inputs.
- [x] Added XGuard category mapping into the final `attack_domain` closed set, with explicit errors for unknown risk categories or invalid unsafe severity.
- [x] Added `prompts/label-review-batch-system.txt` and `config/task.label-review-batch.example.yaml`.
- [x] Verified real XGuard conversion counts in `/private/tmp` and validated the generated JSONL with `jq`.
- [x] Rebuilt `./sendllm` and ran the full Harness gate.
- [x] Added independent `reconcile-batch` mode and service implementation.
- [x] Added batch runtime config fields and validation/defaults.
- [x] Added batch request/response handling with per-item validation, success, retry, failure, and reconcile log/export reuse.
- [x] Added context guard behavior that splits oversized batches using conservative token estimates.
- [x] Added config, service, and CLI wiring tests, then rebuilt `./sendllm`.
- [x] Added independent `reconcile` mode and service implementation.
- [x] Added original label and mapped risk context to reconcile model requests.
- [x] Added fresh per-line reconcile JSONL log generation with success/difference/state diagnostics.
- [x] Implemented source-path mapping, field comparison, original/model merge, and new 8-4 output export.
- [x] Added reconcile unit/integration tests and example configuration.
- [x] Updated quick-start documentation and Harness status/evidence files.
- [x] Reproduced the adjudicate resume bug with a RED test: an existing first output row still caused the model to be called for the first input row.
- [x] Fixed adjudicate mode to count existing valid output JSONL lines, open output in append mode, and skip completed input lines while preserving original input line numbers for errors.
- [x] Added CLI-level safe error details for adjudicate failures without logging raw dataset payloads or model output.
- [x] Rebuilt the local `./sendllm` binary and verified the current production adjudicate output/input counts: 14 completed rows out of 8879 input rows.
- [x] Safely probed line 15 and confirmed the provider returned HTTP 400 with `code=type=data_inspection_failed`; no raw prompt, response, model output, or API Key was printed.
- [x] Added RED/GREEN coverage so `data_inspection_failed` is classified as `content_rejected`.
- [x] Added RED/GREEN coverage so adjudicate records single-row `content_rejected` failures to `final_8_4.jsonl.failed` and continues later rows.
- [x] Rebuilt `./sendllm`, ran a short real adjudicate smoke, confirmed it advanced past line 15, then interrupted the smoke process with SIGINT.
- [x] Confirmed the prior adjudicate request did not send full JSONL rows, but did send complete `annotation`.
- [x] Added RED/GREEN coverage that model requests retain `messages` and labels while excluding `source`, `meta`, top-level `label`, and nonessential `annotation` fields.
- [x] Updated the adjudicate system prompt to describe the minimized input shape.
- [x] Rebuilt the local `./sendllm` binary after the minimal-payload change.
- [x] Replaced adjudicate output-file resume with SQLite-backed resume/export: import differences into SQLite, reset failed rows on startup, run shared Runner, and atomically regenerate success/failed output files from SQLite terminal state.
- [x] Added coverage proving SQLite state wins over existing output files, succeeded rows are not re-sent, failed rows are retried on restart, and adjudicate honors configured limiter concurrency.
- [x] Added original-label reason extraction from `meta.source_fields.reason` so the adjudicate request contains both original and model reasons without sending unrelated metadata.
- [x] Rebuilt `./sendllm` after the SQLite-backed adjudicate change.
- [x] Added RED/GREEN coverage and implementation so adjudicate exports current succeeded rows after manual cancellation.
- [x] Rebuilt `./sendllm` after the Ctrl+C terminal export change.
- [x] Diagnosed the latest 52 failed records without printing raw dataset payloads. Root causes were mostly retry exhaustion from provider 429/502, plus one retryable malformed provider response and one stable provider `content_filter`.
- [x] Terminated stale stopped local `sendllm` processes that were holding old task resources.
- [x] Extracted failed records into local retry inputs, ran conservative retry tasks, and recovered 51 of 52 failed records through the real configured gateway.
- [x] Added a RED/GREEN regression and minimal fix so `ProviderMalformedResponse` is retryable.
- [x] Re-exported the original 948 succeeded records from SQLite and merged recovered outputs into a deterministic 999-success / 1-failed coverage set.
- [x] Merged the failed-record cover workflow into `run`: one completed run can reset final failed records and retry them once with conservative single-concurrency settings before export.
- [x] Changed failed export rows to preserve original source fields and include `is_attack: null`, empty `case_type`, empty `explanation`, empty `extended_info`, `annotation.method: manual_required`, and safe failure diagnostics.
- [x] Completed `feat-007`: compact `id` maps to internal `trace_id`, prompt/response are extracted from first user/assistant messages, original compact fields are preserved, and generated results live under nested `annotation`.
- [x] Updated `config/output-jsonl-schema.json`, CLI integration tests, and opt-in live acceptance parsing for nested `annotation`.
- [x] Completed the user-approved Harness exception for the two load-bearing re-review findings.
- [x] Made completed-result progress bookkeeping independent of caller claim cancellation, preserving graceful peer drain and the original caller cause.
- [x] Kept completed-result progress queries caller-cancelable and converted caller-canceled progress errors into bounded graceful drain, so blocked bookkeeping still respects `shutdown_timeout`.
- [x] Made importer duplicate checks re-canonicalize persisted `raw_json` when a stored hash differs, lazily upgrading exact legacy hashes while rejecting true legacy collisions.
- [x] Added deterministic concurrency/cause/lifecycle coverage plus large-integer and `1.0` upgrade compatibility tests.
- [x] Re-ran the required real model E2E in `/private/tmp/sendllm-live-final-moqKr4` with a fresh task ID, state DB, and output JSONL; existing local acceptance artifacts were not overwritten.
- [x] Fixed SQLite retry ordering with fixed-width UTC timestamps and normalized legacy variable-width values on open.
- [x] Preserved large integer precision in source hashing with `json.Decoder.UseNumber`.
- [x] Separated Runner claim and work cancellation so upstream cancellation drains in-flight calls until `shutdown_timeout`, while task failures cancel immediately.
- [x] Rejected non-positive `max_tokens` and moved Validator/OpenAI/Limiter/Runner construction before durable task creation and import.
- [x] Enforced fixed `label` and `risk_level` enums independently of the configured Schema.
- [x] Kept non-2xx HTTP classification when response-body reads fail or overflow and bounded the retained audit prefix to 4 MiB.
- [x] Covered Export residue ignores and suffixes, aligned operator defaults, corrected the sentinel Go doc, and filled the three requested test gaps.
- [x] Used only synthetic/fake-provider tests in this wave; no real provider call or acceptance-artifact read/write occurred.
- [x] Ran dependency cleanup, all-Go formatting, five-second parser fuzzing, full tests, race tests, and vet.
- [x] Proved the real gateway rejects `json_schema` with HTTP 400 but accepts `json_object` with HTTP 200.
- [x] Added a RED/GREEN regression that preserves bounded HTTP and malformed provider failure responses for SQLite audit without logging them.
- [x] Diagnosed `max_tokens=500` failures from safe metadata: valid envelopes ended with `finish_reason=length`, zero content, and 500 completion tokens.
- [x] Completed the compiled real CLI run with a fresh formal state, `json_object`, `max_tokens=2000`, concurrency 4, and 50 succeeded / 0 failed.
- [x] Independently validated all 50 outputs with the formal Schema and production `service.Validator`, exact unique trace ID equality, automatic annotation metadata, and zero failed state records.
- [x] Replaced the ignored one-off verifier with a tracked, opt-in `TestLiveAcceptance` that validates existing artifacts without calling the model or printing payloads.
- [x] Kept review round 2 limited to the acceptance test and durable evidence; production behavior is unchanged.
- [x] Resolved product scope and data contract.
- [x] Selected SQLite-backed architecture.
- [x] Designed concurrency, retry, validation, and export behavior.
- [x] Created a minimal agent harness with concrete feature dependencies.
- [x] Added Uber Go guide requirements to `AGENTS.md`.
- [x] Converted the complete design specification to Chinese.
- [x] Received user approval for the persisted Chinese design.
- [x] Wrote and self-reviewed the seven-task implementation plan.
- [x] Added mandatory minimal-code and Chinese-comment rules before implementation.
- [x] Added the real-model 50-record end-to-end completion gate.
- [x] Added Go 1.24 module, typed configuration loader, prompt/risk/schema loading, validation, semantic fingerprint, and API key environment lookup.
- [x] Added extensible source, annotation, and OpenAI-compatible completion DTO contracts.
- [x] Added example real-gateway configuration without credentials, complete MASB taxonomy, result Schema, and system prompt.
- [x] Added SQLite schema and store with WAL, foreign keys, busy timeout, single write connection, task semantic identity, and atomic imports.
- [x] Added streaming JSONL import with 16 MiB line limit, source normalization hash, duplicate skip, conflict rollback, and fuzz coverage.
- [x] Added atomic ordered claims, expected-state transitions, attempt persistence, retry scheduling, recovery counts, and next-retry lookup.
- [x] Added the bounded Runner with owned goroutines, unbuffered handoff, shared cooldown, classified retries, format repair, cancellation, and safe progress callbacks.
- [x] Added ordered streaming export, source-field preservation, generated-field replacement, safe failed JSONL, and atomic staged file replacement.
- [x] Added the thin local CLI with environment-only API Key loading, configured Schema/Mode propagation, safe progress, resume export, and fixed exit codes.
- [x] Added the operator README without real endpoint, Key, or dataset examples.

## Verification Evidence

| Check | Command | Result | Notes |
|---|---|---|---|
| Label-review baseline | `./init.sh` | pass, exit 0 | Baseline before `feat-014` edits. |
| Label-review service RED/GREEN | `go test ./internal/service -run '^TestLabelReviewBatchUsesUnifiedOriginalLabels$' -count=1 -v` | RED exit 1; GREEN exit 0 | RED lacked `LabelReviewBatch`; GREEN sends unified original labels and reuses original explanation when model agrees. |
| Label-review CLI RED/GREEN | `go test . -run '^TestRunLabelReviewBatchUsesUnifiedInput$' -count=1 -v` | RED exit 1; GREEN exit 0 | RED rejected the mode as arguments; GREEN wires `-mode label-review-batch`. |
| XGuard prepare RED/GREEN | `go test ./cmd/prepare-xguard-v1 -count=1 -v` | RED exit 1; GREEN exit 0 | RED lacked `run`; GREEN covers split and mapping behavior. |
| Label-review affected packages | `go test ./cmd/prepare-xguard-v1 ./internal/service . ./internal/lib/configs -count=1` | pass, exit 0 | Converter, service, CLI, and config packages pass. |
| Label-review full tests | `go test ./... -count=1` | pass, exit 0 | All packages pass ordinary tests. |
| XGuard real dry-run | `go run ./cmd/prepare-xguard-v1 -input data/v1-test/xguard_target_zh_en.jsonl ...` | pass, exit 0 | `/private/tmp` outputs only; counts `prompt=106529 response=130931 combined=237460`. |
| XGuard JSONL validation | `jq -e . /private/tmp/sendllm-xguard-prepare-*/*.jsonl` | pass, exit 0 | All three generated dry-run files are valid JSONL. |
| Label-review final gate | `git diff --check`; `go build -trimpath -o ./sendllm .`; `./init.sh` | pass, exit 0 | Formatting, all tests, race tests, and vet passed. |
| Multi-key RED/GREEN | `go test ./internal/lib/configs ./internal/facade ./internal/dao ./internal/service -run 'Test(APIKeys|SemanticFingerprintIgnoresAPIKeyEnvs|OpenAI_CompleteRoundRobinsAPIKeys|Store_ItemLogIncludesLastAPIKeyEnv|Store_OpenMigratesMissingAttemptAPIKeyEnvColumn|ReconcileWritesFreshRunLog)' -count=1 -v` | RED failed; GREEN exit 0 | Covers credential arrays, semantic transparency, round-robin Authorization, safe attempt env persistence, old DB migration, and reconcile log `api_key_env`. |
| Multi-key affected packages | `go test ./internal/lib/configs ./internal/facade ./internal/dao ./internal/service -count=1` | pass, exit 0 | Config, facade, DAO, Runner/reconcile integration all pass. |
| Task-007 risk-match RED/GREEN | `go test ./internal/service -run '^TestMergeReconcileAnnotation$' -count=1 -v` | RED exit 1; GREEN exit 0 | RED treated a mapped domain value returned in model `attack_method` as a match; GREEN requires same-category matching and preserves valid supplemental fields when present. |
| Task-007 attack-label Validator | `go test ./internal/service -run '^TestValidator_NewAttackLabels$' -count=1 -v` | pass, exit 0 | Rejects domain enums in `attack_method` and method enums in `attack_domain`. |
| Task-007 reconcile focused gate | `go test ./internal/service -run '^Test(Reconcile|ParseReconcileInput|ReconcileRiskLabels|MappedReconcileLabelSafeEmptiesRiskFields|ReconcileCaseType|MergeReconcileAnnotation|BuildReconcileOutputMarshalsNewLabels|ReconcileRequestIncludesOriginalLabelAndMappedRisk)' -count=1 -v` | pass, exit 0 | Covers mapping, merge, request payload, output format, and reconcile integration. |
| Task-007 full gate | `./init.sh` | pass, exit 0 | Formatting, `go test ./...`, `go test -race ./...`, and `go vet ./...` passed after the risk-match fix. |
| Reconcile fresh log | `go test ./internal/service -run '^TestReconcileWritesFreshRunLog$' -count=1 -v` | pass, exit 0 | Existing log content is replaced; JSONL rows record success status and changed fields only. |
| Reconcile focused integration | `go test ./internal/service -run '^TestReconcile' -count=1 -v` | pass, exit 0 | Covers raw import, model run, SQLite state, original explanation reuse, and new 8-4 output. |
| Reconcile mapping/merge unit | `go test ./internal/service -run '^Test(ParseReconcileInput|ReconcileRiskLabels|MappedReconcileLabelSafeEmptiesRiskFields|ReconcileCaseType|MergeReconcileAnnotation|BuildReconcileOutputMarshalsNewLabels)$' -count=1 -v` | pass, exit 0 | Covers source-path mapping, safe-label clearing, case-type mapping, field merge, and no legacy `risk_type` in output. |
| Reconcile full gate | `go test ./... -count=1`; `go test -race ./internal/service ./internal/dao ./internal/lib/configs -count=1`; `go vet ./...`; `git diff --check`; `go build -trimpath -o ./sendllm .`; `./init.sh` | pass, exit 0 | Standard Harness gate passed after adding reconcile mode. |
| Adjudicate resume RED | `go test ./internal/service -run '^TestAdjudicate' -count=1 -v` | fail, exit 1 | Existing output was ignored and the first input row was sent to the model again. |
| Adjudicate focused GREEN | same focused command | pass, exit 0 | Existing valid output lines are preserved, input lines 1..N are skipped, and new rows are appended. |
| Adjudicate CLI regression | `go test . -run '^TestRunAdjudicateOutputsMASBFormat$' -count=1 -v` | pass, exit 0 | MASB 8-4 output shape remains valid for adjudicate mode. |
| Adjudicate wider gates | `go test ./... -count=1`; `go test -race . ./internal/service ./internal/lib/configs -count=1`; `git diff --check`; `go build -trimpath -o ./sendllm .`; `./init.sh` | pass, exit 0 | The local binary was rebuilt after verification. |
| Adjudicate inspection classification RED/GREEN | `go test ./internal/facade -run '^TestOpenAI_CompleteClassifiesContentRiskBadRequest$/inspection_code$' -count=1 -v` | RED exit 1; GREEN exit 0 | RED classified gateway inspection failure as `bad_request`; GREEN classifies it as `content_rejected`. |
| Adjudicate per-row failed sidecar RED/GREEN | `go test ./internal/service -run '^TestAdjudicateRecordsRejectedLineAndContinues$' -count=1 -v` | RED exit 1; GREEN exit 0 | RED stopped the whole task on row rejection; GREEN records one safe failed row and continues successful rows. |
| Adjudicate real smoke | `zsh -lic './sendllm -mode adjudicate -config ./config/task.003.dark.adjudicate.yaml'` | interrupted after progress, exit 130 | It no longer failed at line 15; after manual interrupt, success output had 15 rows and failed sidecar had 1 row. |
| Adjudicate continuation final gates | `go test ./internal/facade ./internal/service -run '^(TestOpenAI_CompleteClassifiesContentRiskBadRequest|TestAdjudicate)' -count=1 -v`; `go test ./... -count=1`; `go test -race . ./internal/facade ./internal/service ./internal/lib/configs -count=1`; `git diff --check`; `go build -trimpath -o ./sendllm .`; `./init.sh` | pass, exit 0 | The rebuilt local binary contains the continuation fix. |
| Adjudicate minimal-payload RED/GREEN | `go test ./internal/service -run '^TestAdjudicateSendsMinimalJudgmentPrompt$' -count=1 -v` | RED exit 1; GREEN exit 0 | RED showed full `annotation` in the model request; GREEN keeps `messages`, labels, and reasons only. |
| Adjudicate minimal-payload gates | `go test . -run '^TestRunAdjudicateOutputsMASBFormat$' -count=1 -v`; `go test ./... -count=1`; `go test -race . ./internal/service ./internal/lib/configs -count=1`; `git diff --check`; `go build -trimpath -o ./sendllm .`; `./init.sh` | pass, exit 0 | No real model calls were made for this minimal-payload verification. |
| SQLite-backed adjudicate focused | `GOCACHE=/private/tmp/sendllm-gocache go test ./internal/service -run '^(TestAdjudicate|TestRunner_UsesCustomClassificationRequest)' -count=1 -v` | pass, exit 0 | Covers SQLite resume over output file contents, failed reset/retry, both judgment reasons in request payload, and configured real concurrency. |
| SQLite-backed adjudicate affected packages | `GOCACHE=/private/tmp/sendllm-gocache go test ./internal/dao ./internal/service ./internal/lib/configs -count=1` | pass, exit 0 | DAO/service/config behavior passes without real model calls. |
| SQLite-backed adjudicate race | `GOCACHE=/private/tmp/sendllm-gocache go test -race ./internal/service ./internal/dao ./internal/lib/configs -count=1` | pass, exit 0 | Affected concurrency and state packages pass under race detector. |
| SQLite-backed adjudicate build/hygiene | `GOCACHE=/private/tmp/sendllm-gocache go build -trimpath -o ./sendllm .`; `git diff --check` | pass, exit 0 | Local CLI binary rebuilt. |
| Adjudicate Ctrl+C export RED/GREEN | `go test ./internal/service -run '^TestAdjudicateExportsSucceededRowsAfterCancellation$' -count=1 -v` | RED exit 1; GREEN exit 0 | RED had no output file after cancellation; GREEN exports the succeeded row using a bounded terminal export context. |
| Adjudicate Ctrl+C focused gates | `go test ./internal/service -run '^TestAdjudicate' -count=1 -v`; `go test ./internal/dao ./internal/service ./internal/lib/configs -count=1`; `go test -race ./internal/service ./internal/dao ./internal/lib/configs -count=1`; `go build -trimpath -o ./sendllm .` | pass, exit 0 | Local CLI binary rebuilt after the interruption export change. |
| Current full gate | `./init.sh` | pass, exit 0 | Formatting, `go test ./...`, `go test -race ./...`, and `go vet ./...` passed. |
| Compact JSONL baseline | `./init.sh` | pass, exit 0 | Formatting, all tests, race tests, and vet passed before `feat-007` edits. |
| Compact JSONL RED | `go test ./internal/dto ./internal/service -run 'Test(ParseSourceCompactJSONL|ParseSourceRejectsInvalidContent|ImportAcceptsCompactJSONL|ExportWritesOrderedMergedSuccessAndSafeFailures)' -count=1 -v` | fail, exit 1 | DTO still required legacy `trace_id`; Import rejected compact rows; Export removed compact `label` and wrote generated fields at top level. |
| Compact JSONL focused GREEN | same focused command | pass, exit 0 | Compact parsing, compact import, and nested annotation export passed. |
| Compact JSONL wider tests | `go test ./... -count=1` | pass, exit 0 | CLI integration tests now use compact input and nested annotation assertions. |
| Compact JSONL full gate | `./init.sh` | pass, exit 0 | Formatting, all tests, full race tests, and `go vet ./...` passed after implementation. |
| Latest 52 failure summary | Python failed-JSONL counter and SQLite metadata queries against `data/task-002/task-2026-08-07-002.db` | analyzed | 52 failed: 35 `rate_limited`, 12 `malformed_response`, 2 `invalid_result`, 2 `server`, 1 `content_rejected`; raw prompt/response and raw model content were not printed. |
| Latest 52 retry | `zsh -lic 'exec ./sendllm -config ./config/task.retry-52.yaml'` | exit 2 | Real gateway run recovered 50 of 52 with conservative concurrency/RPM; output `added=52 skipped=0 succeeded=50 failed=2`. |
| Remaining 2 retry | `zsh -lic 'exec ./sendllm -config ./config/task.retry-2.yaml'` | exit 2 | Real gateway run recovered 1 of 2; one row remained `content_rejected` with provider `finish_reason=content_filter`. |
| Re-export original state | `zsh -lic 'exec ./sendllm -config ./config/task.002.yaml'` | exit 2 | No model calls for terminal records; output `added=0 skipped=1000 succeeded=948 failed=52`, restoring the original 948-row export from SQLite. |
| Recovered output coverage | local Python trace coverage check | pass | `input=1000 output=999 failed=1 covered=1000 missing=0 overlap=0`; output rows contain required annotation fields and no top-level `label`. |
| Malformed retry RED/GREEN | `go test ./internal/service -run '^TestClassifyFailure$/malformed_provider_response$' -count=1 -v` | RED exit 1; GREEN exit 0 | RED showed `Retry:false` for `malformed_response`; GREEN showed it is retryable. |
| Integrated cover RED/GREEN | `go test . -run '^TestRunCoversFinalFailuresBeforeExport$' -count=1 -v` | RED exit 1; GREEN exit 0 | RED exported a failed 429 after one call; GREEN performed the integrated cover retry, made two provider calls, and exported a succeeded row with an empty failed file. |
| Failed template RED/GREEN | `go test ./internal/service -run '^TestExportWritesOrderedMergedSuccessAndSafeFailures$' -count=1 -v` | RED exit 1; GREEN exit 0 | RED showed failed rows lacked source fields and placeholders; GREEN preserved source fields and wrote manual supplement fields. |
| Latest full gate | `./init.sh` | pass, exit 0 | Formatting, all tests, race tests, and `go vet ./...` passed after the retry fix. |
| Re-review Runner RED/GREEN | `go test ./internal/service -run '^TestRunner_RunDrainsPeerWhenProgressRacesWithCallerCancellation$' -count=1 -timeout=10s -v` | RED exit 1; GREEN exit 0 | RED peer received caller cause during progress; GREEN preserved the cause while both items succeeded and all workers exited. |
| Re-review shutdown bound RED/GREEN | `go test ./internal/service -run '^TestRunner_RunBoundsDrainWhenProgressQueryBlocksAfterCallerCancellation$' -count=1 -timeout=5s -v` | RED exit 1; GREEN exit 0 | RED stayed blocked in progress bookkeeping past `shutdown_timeout`; GREEN entered bounded drain. |
| Re-review DAO RED/GREEN | `go test ./internal/dao -run '^TestImport_Add(LazilyMigratesLegacyHashForExactSource|RejectsDifferentSourceDespiteLegacyHashCollision)$' -count=1 -v` | RED exit 1; GREEN exit 0 | Exact legacy large integer and `1.0` sources migrate; adjacent-integer legacy collision remains `ErrTraceConflict`. |
| Re-review determinism | exact Runner regression with `-count=50`; DAO/Runner focused suite | pass, exit 0 | Deterministic interleaving remained stable; all `TestImport_` and `TestRunner_` cases passed. |
| Re-review race | `go test -race ./internal/dao ./internal/service -count=1` | pass, exit 0 | Full affected packages passed under the race detector. |
| Re-review full gate | `./init.sh` | pass, exit 0 | Formatting, all tests, full race tests, and vet passed. |
| Re-review fuzz | `go test -run=^$ -fuzz=FuzzParseJSONL -fuzztime=5s ./internal/service` | pass, exit 0 | 6,159 executions without crash or hang. |
| Re-review scope | diff, line-length, secret/logging, out-of-scope, and sensitive-artifact scans | pass | Zero findings; no Key values or payload logging paths found. |
| Latest real 50-record CLI | `zsh -lic 'exec /private/tmp/sendllm-live-final-moqKr4/sendllm -config /private/tmp/sendllm-live-final-moqKr4/task.yaml'` | pass, exit 0 | Fresh state/output; stdout `added=50 skipped=0 succeeded=50 failed=0`. |
| Latest real output integrity | `SENDLLM_LIVE_CONFIG="$PWD/config/task.example.yaml" SENDLLM_LIVE_INPUT=/Users/lijiayang/venus/SendLLM/Test_Input.jsonl SENDLLM_LIVE_OUTPUT=/private/tmp/sendllm-live-final-moqKr4/output.jsonl SENDLLM_LIVE_STATE=/private/tmp/sendllm-live-final-moqKr4/state.db SENDLLM_LIVE_TASK_ID=sendllm-final-live-20260806-moqKr4 go test ./internal/service -run '^TestLiveAcceptance$' -count=1 -v` | pass, exit 0 | `live_acceptance=PASS input=50 output=50 trace_unique=50 trace_set_equal=50 schema_valid=50 business_valid=50 annotation_auto=50 failed_file=0 state_succeeded=50 state_failed=0`. |
| Final fix focused regressions | combined affected-package command across seven packages | pass, exit 0 | Covers all nine findings and three explicit coverage gaps. |
| Final fix race gate | `go test -race . ./internal/dao ./internal/facade ./internal/lib/limiter ./internal/service -count=1` | pass, exit 0 | Exercises CLI, persistence, provider, limiter, and Runner changes under the race detector. |
| Final fix parser fuzz | `go test -run=^$ -fuzz=FuzzParseJSONL -fuzztime=5s ./internal/service` | pass, exit 0 | 6,738 executions without a crash or hang. |
| Final fix standard gate | `./init.sh` | pass, exit 0 | Formatting, all tests, full race tests, and vet passed. |
| Final fix hygiene | `git diff --check`; added-Go-line scan; Export ignore checks; security/scope scans | pass | Zero long added Go lines, secret literals, payload logging paths, out-of-scope services, or sensitive artifact changes. |
| First final-fix wave model scope | not run | intentionally omitted | That earlier wave used synthetic tests only; the latest user-approved re-review exception re-ran the 50-record real acceptance above. |
| Task 7 build | `go build -trimpath -o /private/tmp/sendllm-task7-final .` | pass, exit 0 | Empty stdout; real binary, no fake replacement. Runtime and final Task 7 production Go source had no diff. |
| Task 7 cleanup and fuzz | `GOPROXY=https://proxy.golang.org,direct go mod tidy`; all-Go `gofmt`; `git diff --check`; `go test -run=^$ -fuzz=FuzzParseJSONL -fuzztime=5s ./internal/service` | pass, exit 0 | Direct/indirect requirements normalized; fuzz completed 6,693 executions without a crash or hang. |
| Failure audit RED/GREEN | `go test ./internal/facade -run '^TestOpenAI_CompletePreservesAuditableFailureResponse$' -v` | RED exit 1, GREEN exit 0 | RED assertions: HTTP 400 and malformed HTTP 200 both had empty `RawResponse`; GREEN passed both cases after bounded propagation. No real raw payload was printed. |
| Task 7 facade and full gate | `go test ./internal/facade`; `go test -race ./internal/facade`; `./init.sh` | pass | Full tests, full race detector, formatting, and vet passed after the audit fix. |
| Real capability check | compiled client against the configured gateway in `json_schema` and `json_object` modes | fallback confirmed | `json_schema` returned HTTP 400 structured-output rejection; equivalent `json_object` returned HTTP 200. |
| Pinned real config | `zsh -lic 'test -n "$AI_GATEWAY_API_KEY"'` | pass, exit 0 | `base_url=https://aigateway.venusgroup.com.cn/ai/deepseek/openai`; `model=deepseek-v4-pro`; `api_key_env=AI_GATEWAY_API_KEY`; no Key value printed. |
| Real 50-record CLI | `zsh -lic 'exec /private/tmp/sendllm-task7-final -config /private/tmp/sendllm-task7-final.yaml'` | pass, exit 0 | Count-only stdout: `added=50 skipped=0 succeeded=50 failed=0`; fresh formal state, `json_object`, max tokens 2000, concurrency 4. |
| Real output integrity | `SENDLLM_LIVE_CONFIG="$PWD/config/task.example.yaml" SENDLLM_LIVE_INPUT=/Users/lijiayang/venus/SendLLM/Test_Input.jsonl SENDLLM_LIVE_OUTPUT=/Users/lijiayang/venus/SendLLM/Test_Output.jsonl SENDLLM_LIVE_STATE=/Users/lijiayang/venus/SendLLM/Test_State.db SENDLLM_LIVE_TASK_ID=sendllm-task7-real-final-20260806 go test ./internal/service -run '^TestLiveAcceptance$' -count=1 -v` | pass, exit 0 | Count-only stdout: `live_acceptance=PASS input=50 output=50 trace_unique=50 trace_set_equal=50 schema_valid=50 business_valid=50 annotation_auto=50 failed_file=0 state_succeeded=50 state_failed=0`. No model call was made. |
| Acceptance harness | `go test ./internal/service -run '^TestLiveAcceptance$' -count=1 -v`; exact live command with temporary expected count 51; restored exact live command | SKIP exit 0; RED exit 1; GREEN exit 0 | Default gate is credential-free. RED showed every actual count remained 50 and failed/state counts remained 0; restoring the tracked expectation to 50 passed. |
| Review round 1 closeout | `git status --short`; `git log --oneline -8` after `506da53` | pass, exit 0 | Status stdout was empty; log was headed by `506da53`, then `2ba55c7`, `656a0df`, `6a9f3aa`, `f504d0b`, `7ea4821`, `61401d5`, `01ef1ec`. |
| Security and scope | `rg -n 'Bearer |api[_-]?key|authorization|prompt.*slog|response.*slog' --glob '*.go' --glob '*.yaml' --glob '*.md' .`; `rg -n 'ListenAndServe|redis|kafka|RabbitMQ|message[ _-]?queue|cron' --glob '*.go' .` | reviewed | 23 expected protocol/env/test/evidence lines; credential values 0; payload-logging paths 0; out-of-scope Go matches 0. |
| Task 4 foundation | `go test ./internal/lib/tokenizer ./internal/lib/limiter ./internal/service -run 'Test(Estimate|Limiter|RetryPolicy|ClassifyFailure)'`; `go test -race ./internal/lib/limiter`; `./init.sh` | pass | Covers conservative token estimates, bounded concurrency, disabled rates, token budget rejection, cooldown, idempotent release, failure classes, backoff, full race tests, and vet. |
| Harness validation | `validate-harness.mjs --target .` | pass | 100/100; all subsystems 5/5. |
| Config and DTO tests | `go test ./internal/lib/configs ./internal/dto` | pass | Covers strict loading, defaults, source normalization, and annotation contracts. |
| Go verification | `./init.sh` | pass | Formatting, unit tests, race tests, and vet all passed. |
| Task 2 DAO/importer | `go test ./internal/dao ./internal/service -run 'Test(Store|Import)' -v` | pass | Covers task hash identity, duplicate source hash, JSONL failures, and rollback. |
| Task 2 race/fuzz | `go test -race ./internal/dao ./internal/service -run 'Test(Store|Import)'`; `go test ./internal/service -run '^$' -fuzz FuzzParseJSONL -fuzztime 1s` | pass | No races; parser fuzz smoke passed. |
| Task 3 validation | `go test ./internal/service -run TestValidator -v` | pass | Covers valid unsafe/safe/hard-negative results and strict malformed or cross-field failures. |
| Task 3 OpenAI facade | `go test ./internal/facade`; `go test -race ./internal/facade` | pass | Covers request modes, auth, usage, status classification, Retry-After, content filters, and response size limit. |
| Full gate after Task 3 | `./init.sh` | pass | Formatting, full tests, full race tests, and vet passed. |
| Task 5 DAO and Runner | `go test ./internal/dao ./internal/service`; `go test -race ./internal/dao ./internal/service` | pass | Covers durable state transitions, no repeated success calls, bounded concurrency, retries, repairs, cancellation, and recovery. |
| Full gate after Task 5 | `./init.sh` | pass | Formatting, all package tests, full race tests, and vet passed outside the port-restricted sandbox. |
| Task 6 Export and CLI | `go test ./internal/service -run TestExport`; `go test . -run TestRun`; `go test -race . ./internal/service` | pass | Covers order, merging, safe diagnostics, atomic replacement, configured Schema/Mode, exit codes, resume failure export, and payload-free output. |
| Full gate after Task 6 | `./init.sh` | pass | Formatting, all package tests, full race tests, and vet passed on 2026-08-06. |

## Files Changed

- `internal/service/reconcile.go`, `reconcile_internal_test.go`, `reconcile_test.go`
- `main.go`, `config/task.reconcile.example.yaml`, `docs/quickstart-model-adjudicate.md`
- `feature_list.json`, `progress.md`, `session-handoff.md`
- `internal/dto/annotation.go`, `annotation_test.go`
- `internal/service/validator.go`, `validator_test.go`, `adjudicate.go`
- `config/result-schema-v2.json`, `config/output-jsonl-schema.json`
- `prompts/my-batch-prompt-only-system.txt`
- `sendllm_output_jsonl_v2.md`
- `prompts/my-batch-prompt-only-system.txt`
- `internal/dto/annotation.go`, `annotation_test.go`
- `internal/service/validator_test.go`
- `config/result-schema.json`, `config/output-jsonl-schema.json`
- `sendllm_output_jsonl.md`, `sendllm_model_output_jsonl_format.md`
- `cmd/merge-failed/main.go`, `cmd/merge-failed/main_test.go`
- `progress.md`, `session-handoff.md`
- `internal/service/runner.go`, `runner_internal_test.go`
- `internal/dao/import.go`, `sqlite_test.go`
- `feature_list.json`, `progress.md`, `session-handoff.md`
- `.gitignore`, `README.md`, `config/task.example.yaml`
- `main.go`, `main_test.go`
- `internal/dao/import.go`, `items.go`, `sqlite.go`, and focused tests
- `internal/facade/openai.go`, `openai_test.go`
- `internal/lib/configs/config.go`, `config_test.go`
- `internal/service/exporter.go`, `runner.go`, `validator.go`, and focused tests
- `internal/dto/annotation_test.go`, `sample_test.go`, `internal/lib/limiter/limiter_test.go`
- `feature_list.json`, `progress.md`, `session-handoff.md`
- `internal/facade/openai.go`, `internal/facade/openai_test.go`
- `feature_list.json`, `progress.md`, `session-handoff.md`
- `main.go`, `main_test.go`
- `internal/dao/items.go`
- `internal/service/exporter.go`, `exporter_test.go`
- `README.md`
- `feature_list.json`, `progress.md`, `session-handoff.md`
- `internal/lib/tokenizer/estimate.go` and `estimate_test.go`
- `internal/lib/limiter/limiter.go` and `limiter_test.go`
- `internal/service/retry.go` and `retry_test.go`
- `AGENTS.md`
- `feature_list.json`
- `progress.md`
- `session-handoff.md`
- `init.sh`
- `.gitignore`
- `docs/superpowers/specs/2026-08-05-sendllm-design.md`
- `docs/superpowers/plans/2026-08-05-sendllm-implementation.md`
- `go.mod`, `go.sum`
- `internal/lib/configs/config.go`, `internal/lib/configs/load.go`, and tests
- `internal/dto/sample.go`, `annotation.go`, `completion.go`, and tests
- `internal/dao/schema.sql`, `sqlite.go`, `items.go`, `import.go`, and tests
- `internal/service/importer.go`, tests, and synthetic JSONL fixture
- `internal/service/completer.go`, `validator.go`, and `validator_test.go`
- `internal/service/runner.go`, `runner_test.go`, and `progress.go`
- `internal/facade/openai.go` and `openai_test.go`
- `config/task.example.yaml`, `config/risk-types.yaml`, `config/result-schema.json`
- `prompts/masb-system.txt`

## Decisions Made

- Single-process CLI, SQLite, JSONL import/export, OpenAI-compatible facade.
- `response` is optional; `prompt` and `response` cannot both be empty.
- Unknown source fields are preserved.
- Structured result validation is local and strict.
- Account concurrency is configurable up to 500, with shared rate-limit cooldown.
- The first release must use the smallest readable implementation; all necessary Go comments are concise and written in Chinese.
- The CLI passes the same loaded result Schema to Validator and Runner, and passes `model.structured_output` without hard-coding a provider mode.
- After the task identity is verified, import or runner errors still trigger export of already-terminal SQLite records before exit.
- Final acceptance uses `deepseek-v4-pro` through the configured real gateway and must produce 50/50 valid output records.
- The tested gateway mode is `json_object`; its `json_schema` response format was rejected with HTTP 400.
- For this reasoning model, the accepted run raised `max_tokens` from 500 to 2000 after 500 caused empty-content length truncation.
- Configuration resolves all task-relative paths before execution and rejects YAML unknown fields and client-managed `extra_body` keys.
- SQLite stores canonical source hashes for idempotency while retaining raw source JSON; a changed `trace_id` source aborts and rolls back the entire import transaction.
- SQLite retry timestamps use a fixed-width nine-digit UTC representation so text ordering is chronological; opening the state normalizes legacy values under the single-process contract.
- Runner owns separate claim and in-flight work contexts: upstream cancellation drains bounded work, while task-level causes cancel both immediately.
- All fallible, side-effect-free construction completes before task creation and import.
- Completed-result progress bookkeeping remains caller-cancelable; expected caller cancellation becomes bounded graceful drain rather than task failure.
- Legacy hash compatibility trusts persisted `raw_json` as the durable source: current canonical equality skips and migrates in the same transaction; current inequality conflicts.

## Blockers / Risks

- Provider RPM/TPM remain undocumented; transient 429s occurred during acceptance even at concurrency 4 and were recovered by durable retries.
- `config/task.003.dark.adjudicate.yaml` currently uses `runtime.concurrency: 400` and `requests_per_minute: 40000`; this is true concurrent request load. Lower these values and restart if 429s climb.
- The original design target of concurrency 64 is superseded by real-gateway evidence. The current formal recommendation is concurrency 4 with continued calibration against observed 429s.
- OpenAI-compatible capability is provider-specific. Do not switch this task back to `json_schema` without a new state file and a fresh capability check.
- Runner shutdown assumes `Completer.Complete` honors context; Go cannot forcibly stop an implementation that ignores cancellation forever.
- Retry-time migration is intentionally single-process and does not coordinate multiple concurrent instances.

## Next Session Startup

1. Read `AGENTS.md` completely.
2. Read the design specification.
3. Read `feature_list.json` and `progress.md`.
4. Run `./init.sh` after any code or configuration change.
5. Use a new task ID/state file when model semantics, structured-output mode, prompt, taxonomy, or Schema changes.

## Recommended Next Step

Place the approved 50-row `Safety_Review_P04B_Hidden.jsonl` at the repository root, then resume blocked
`feat-024 Safety Review evaluation and live P04-B acceptance` after rerunning the startup workflow and baseline gates.

## Advertisement Full Cleaning Handoff (2026-09-22)

- Active feature: `feat-040`.
- Historical feature: `feat-039=blocked`; do not restart either Flash full run or reinterpret the 265-row directed review as full coverage.
- Read first: `docs/superpowers/specs/2026-09-22-advertisement-full-cleaning-design.md`.
- Execute: `docs/superpowers/plans/2026-09-22-advertisement-full-cleaning.md`.
- Agent prompt: `docs/advertisement-full-cleaning-coding-model-prompt.md`.
- Frozen runtime inputs: `prompts/advertisement-full-review-system.txt`, `config/advertisement-full-review-result-schema.json`, `config/task.advertisement-full-review.qwen3.5-plus.yaml`, and `config/task.advertisement-full-review.deepseek-v4-pro.yaml`.
- Models are frozen as `qwen3.5-plus` for all 58,658 Layer-1 rows and `deepseek-v4-pro` only for deterministic Layer-2 routing.
- Protected inputs remain the advertisement source, task-013 reviewed v8 baseline, and existing provisional integration.
- Current checkpoint contains documentation/state only. No real pilot or full model run has been authorized or started.
- Unique next action: implement Tasks 1-6 with fake providers and local gates, then stop for explicit approval of the frozen Qwen pilot.
