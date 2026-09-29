# Session Progress Log

## Current State

**Last Updated:** 2026-09-28
**Active Feature:** none; `feat-038` is complete

### feat-038 Complete (2026-09-28)

- [x] Completed the closed-loop workflow without in-program human marking: model marking -> offline prompt/strong-model review artifact -> automatic compile -> fresh Safety Review regression -> release validation.
- [x] Added real Safety Review-backed base/candidate execution for both rotations, parallelized within a rotation, with independent task dirs and output hashes.
- [x] Added CLI wiring for `analyze`, `compile`, `regression`, `inspect`, `mapping-approve`, `gold-approve`, `gold-promote`, `approve`, `release`, and `status`.
- [x] Fixed the Safety Review arbiter drain race exposed by the live regression run.
- [x] Real live smoke with configured models: candidate compile PASS; regression PASS rotations=2; each rotation 50 cases; contract passed observed=20; stage_failures=0.
- [x] Real release: `p04b-v1.1`, SHA-256 `38405ebc292ea8998a1ef82c785112af87fd2d7dd1babac525be25e708d28194`.
- [x] Fresh Safety Review consumption: `validation=PASS task_id=p04b-eval-001`.
- [x] Final gates: `./scripts/verify-policy-optimizer.sh` PASS; `./init.sh` exit 0; `git diff --check` exit 0.
- [ ] No remaining blocker.

### feat-038 Blocked (2026-09-28)

- [x] Baseline `./init.sh` -> exit 0; focused `go test ./internal/service ./internal/api/cli . -run '^TestPolicyOptimizer' -count=1` -> exit 0.
- [x] Sanitized prerequisites: `AI_GATEWAY_API_KEY` present and `Safety_Review_P04B_Hidden.jsonl` present; no secret value printed.
- [x] Added `scripts/verify-policy-optimizer.sh` and `policy-optimization/regression/gates/p04b-gate-v1.yaml`; verifier PASS.
- [x] Added minimal `PolicyOptimizerRunner` mode graphs, preflight-before-claims, recovery-on-reopen, and read-only CLI status/watch output.
- [x] Added `PolicyOptimizerRegressionRotationExecutor` so regression invokes `rotation-a` and `rotation-b` separately and validates both identities.
- [x] Wired CLI `approve` and `release` to the existing approval/release service layer using iteration artifacts.
- [x] Wired CLI `compile`, `regression`, `inspect`, and `mapping-approve` to the existing deterministic service layer.
- [x] Wired CLI `gold-approve` and `gold-promote` to Candidate/Correction JSONL and Core promotion artifacts.
- [x] Regression rotation results now require distinct task dirs and distinct output SHA-256 values, in addition to distinct rotation IDs.
- [x] Added the initial frozen P04-B Core Safe regression contract and approved/core Gold directory skeletons.
- [x] Wired `regression` CLI to fresh Safety Review rotations: base/candidate bundles each run prompt/response tasks per rotation, with distinct task dirs and output hashes.
- [x] Wired `analyze` CLI to the automatic loop: normalize/stratify, consume offline prompt Change Set, compile, then fresh Safety Review regression; no in-program human marking.
- [x] Added runner tests: `go test ./internal/service -run '^TestPolicyOptimizerRunner' -count=1 -v` -> exit 0.
- [x] Blocker evidence: contracts and gold directories absent; no `iterations/` real run.
- [x] CLI blocker: `go run . policy-optimizer analyze --config config/policy-optimizer.example.yaml --package audit-round-007` -> exit 1, `policy-optimizer error: unsupported`.
- [x] Independent audit: workflow runner/status/recovery not implemented or CLI-wired; frozen commands beyond `validate` return unsupported; `RunPolicyRegression` is still a callback shell without fresh Safety Review rotation execution; full fake E2E matrix is absent.
- [ ] Remaining external action: run the closed loop against the intended real Audit/model-loop dataset, then validate the resulting candidate/release with fresh Safety Review.
- [ ] No real-model acceptance, release publication, human approval, or final completion claim was made.

### feat-037 Complete (2026-09-28)

- [x] Added policy release approval binding candidate, regression, gate, and critic hashes.
- [x] Added `ReleasePolicy` checks for critic block, P04-B gate pass, candidate hash, approval hash match, target grammar, path containment, destination collision, and staging collision.
- [x] Added sibling staging publication with deterministic recompile/re-sign, atomic rename, and post-publication Safety Review bundle verification.
- [x] RED `go test ./internal/service -run '^TestPolicyOptimizer(Approval|Release|SafetyReviewIntegration)' -count=1 -v` -> exit 1 for missing APIs.
- [x] GREEN same command -> exit 0; repeat `-count=50` -> exit 0; race `-count=10` -> exit 0.
- [x] Full gate: `./init.sh` -> exit 0; `git diff --check` -> exit 0.
- [ ] Residual risk: filesystem failure injection at every boundary and fresh CLI Safety Review validate consumability remain for final acceptance hardening.
- [ ] Next exact action: implement `feat-038` final acceptance in a new turn.

### feat-036 Complete (2026-09-28)

- [x] Added strict Policy Optimizer command/flag parsing for all frozen commands and `compile --preview` / `status --watch` combinations.
- [x] Added zero-network `validate`, cancellation exit 130, concise safe errors, and early `policy-optimizer` dispatch in `main.go` before legacy parsing.
- [x] RED: `go test ./internal/api/cli . -run '^TestPolicyOptimizer(CLI|Main)' -count=1 -v` -> exit 1 for missing CLI/main dispatch.
- [x] GREEN: same command -> exit 0.
- [x] Repeat: `-count=20` -> exit 0; race `-count=5` -> exit 0.
- [x] Full gate: `./init.sh` -> exit 0; `git diff --check` -> exit 0.
- [ ] Residual risk: full workflow runner stage graph, status/watch implementation, preflight-before-claims, and interrupted resume orchestration remain before final acceptance.
- [ ] Next exact action: implement `feat-037` approval/release in a new turn.

### feat-035 Complete (2026-09-28)

- [x] Added deterministic Candidate -> Approved -> Core Gold lifecycle with subject-hash-bound approvals, three distinct successful release checks, and unresolved challenge blocking.
- [x] Added versioned Regression Contract validation, including supersede version increments and mandatory Human Directive references.
- [x] Added two-rotation Regression execution validation and a fixed P04-B V1 gate evaluator that independently checks unsafe recall, unsafe quarantine, false Unsafe, total quarantine, contracts, patterns, and stage failures.
- [x] RED: `go test ./internal/service -run '^TestPolicyOptimizer(Gold|Regression|Gate)' -count=1 -v` -> exit 1 for missing lifecycle/evaluator APIs.
- [x] GREEN: same command -> exit 0.
- [x] Affected: `go test ./internal/service ./internal/dto -run '^TestPolicyOptimizer' -count=1` -> exit 0.
- [x] Repeat: `go test ./internal/service -run '^TestPolicyOptimizer(Gold|Regression|Gate)' -count=20` -> exit 0.
- [x] Race: `go test -race ./internal/service ./internal/dto -run '^TestPolicyOptimizer' -count=3` -> exit 0.
- [x] Full gate: `./init.sh` -> exit 0; `git diff --check` -> exit 0.
- [x] No real model call, credential, approval action, or immutable release publication was performed.
- [ ] Residual risk: full per-suite delta reporting and correction-file CLI handling remain for later CLI integration.
- [ ] Next exact action: implement `feat-036` CLI/workflow recovery in a new turn; do not start it in this feature turn.

### feat-034 Complete (2026-09-28)

- [x] Resumed the next pending Policy Optimizer feature after `feat-033` without resetting or reverting the existing dirty worktree.
- [x] Baseline `./init.sh` -> exit 0.
- [x] RED: `go test ./internal/service -run '^TestPolicyOptimizer(Apply|Compile|Bundle|Verify)' -count=1 -v` -> exit 1 because candidate/compiler DTOs and APIs were absent.
- [x] Added deterministic candidate bundle copying, base version/hash checks, frozen patch operations, RFC6901 YAML-pointer handling, path escape and duplicate card-ID rejection.
- [x] Added deterministic Judge A/B, Router, Expert, Arbiter, and refusal prompt compilation, coverage-map.json, compile-manifest.json, release-manifest re-signing, and bundle verification through the Safety Review release contract.
- [x] GREEN: `go test ./internal/service -run '^TestPolicyOptimizer(Apply|Compile|Bundle|Verify)' -count=1 -v` -> exit 0.
- [x] Affected tests: `go test ./internal/service ./internal/dto -run '^TestPolicyOptimizer' -count=1` -> exit 0.
- [x] Repeat: `go test ./internal/service -run '^TestPolicyOptimizer(Apply|Compile|Bundle|Verify)' -count=10` -> exit 0.
- [x] Race: `go test -race ./internal/service -run '^TestPolicyOptimizer' -count=1` -> exit 0.
- [x] Full gate: `./init.sh` -> exit 0; `git diff --check` -> exit 0.
- [x] No real model calls, credentials, approvals, or immutable release publication were created.
- [ ] Next exact action: implement `feat-035` Gold lifecycle and regression evaluator in a new turn; do not start it in this feature turn.

### feat-033 Complete (2026-09-28)

- [x] Resumed the sole in-progress feature, Policy Optimizer Change Requests/Proposal/Critic/Resolver.
- [x] Identified the current changed worktree as the Policy Optimization implementation stream, with `feat-026` through `feat-032` complete and `feat-033` partially implemented but failing baseline.
- [x] Baseline `./init.sh` exited 1 because `LoadChangeRequests` used strict YAML decoding against DTOs that only had JSON tags; documented fields such as `change_id`, `actor_id`, `requested_changes`, `evidence_refs`, and `created_at` were rejected.
- [x] RED: `go test ./internal/service -run '^TestPolicyOptimizer(ChangeRequest|ChangeSet|Resolver)' -count=1 -v` -> exit 1 for the YAML-tag failure and for non-frozen patch operation names.
- [x] Implemented strict Change Request YAML loading support, frozen operation validation, Proposal validation, Critic family separation from Author/Resolver, Critic block handling, equal high-authority conflict blocking, and Change Set validation.
- [x] GREEN: `go test ./internal/service -run '^TestPolicyOptimizer(ChangeRequest|Proposal|Critic|Resolver)' -count=1 -v` -> exit 0.
- [x] Affected tests: `go test ./internal/service ./internal/dto -run '^TestPolicyOptimizer' -count=1` -> exit 0.
- [x] Repeat: `go test ./internal/service -run '^TestPolicyOptimizer(ChangeRequest|Proposal|Critic|Resolver)' -count=20` -> exit 0.
- [x] Race: `go test -race ./internal/service ./internal/dto -run '^TestPolicyOptimizer(ChangeRequest|Proposal|Critic|Resolver|Contract)' -count=5` -> exit 0.
- [x] Scope/diff: `scripts/verify-policy-optimizer-scope.sh` is not present; `git diff --check` -> exit 0.
- [x] Full gate: `./init.sh` -> exit 0.
- [x] No real model calls, credentials, generated candidate artifacts, approvals, or releases were created.
- [ ] Next exact action: implement `feat-034` deterministic compiler in a new turn; do not start it in this feature turn.

### Advertisement Scope Removed (2026-09-24)

- [x] User confirmed the Advertisement workstream is no longer needed.
- [x] Removed `feat-039` and `feat-040` from `feature_list.json`; existing advertisement code, docs, and local files were not physically deleted.
- [x] Remaining project scope is now Policy Optimization `feat-026` through `feat-038`.

### feat-026 Complete (2026-09-24)

- [x] Added strict Policy Optimizer config loading with closed YAML fields, config-relative path containment, model role/profile validation, exact batching/retry limits, candidate/preview version derivation, and semantic/runtime fingerprint separation.
- [x] Added closed DTO enums and validators for Audit Record and Change Request inputs.
- [x] Added `config/policy-optimizer.example.yaml` and the initial formal contract Schema at `policy-optimization/schemas/contracts.schema.json`.
- [x] RED: `go test ./internal/lib/configs -run '^TestPolicyOptimizerConfigRejectsUnknownAndInvalidContracts$' -count=1 -v` -> exit 1 because an escaping relative path was accepted.
- [x] GREEN: the same command -> exit 0 after path containment was implemented.
- [x] Repeat: `go test ./internal/lib/configs ./internal/dto -run '^TestPolicyOptimizer' -count=20` -> exit 0.
- [x] Race: `go test -race ./internal/lib/configs ./internal/dto -run '^TestPolicyOptimizer' -count=5` -> exit 0.
- [x] Full gate: `./init.sh` -> exit 0; `git diff --check` -> exit 0.
- [ ] Next exact action: implement `feat-027` in a new turn; do not start it in this feature turn.

### feat-027 Complete (2026-09-24)

- [x] Added the frozen 11-table `optimization_*` SQLite schema and `OpenPolicyOptimizer`.
- [x] Added `EnsureIteration`, `ClaimSkillRun`, `CompleteSkillRun`, `RecoverRunning`, and `ReadStatus` with semantic identity checks and recovery counters.
- [x] Added immutable `PolicyOptimizerArtifactStore` with temp-write, sync, atomic rename, SHA-256/size metadata, path containment, and overwrite rejection.
- [x] Focused GREEN: `go test ./internal/dao -run '^TestPolicyOptimizer(Store|Artifact)' -count=1 -v` -> exit 0.
- [x] Repeat/race: focused tests `-count=20` -> exit 0; race `-count=5` -> exit 0.
- [x] Full gate: `./init.sh` -> exit 0; `git diff --check` -> exit 0.
- [ ] Next exact action: implement `feat-028` Skill Runtime in a new turn.

### feat-028 Complete (2026-09-24)

- [x] Added the closed 13-Skill registry with strict manifest IDs, executor matching, required headings, input/output schemas, symlink/executable rejection, and repository assets.
- [x] Added deterministic `BuildPolicyOptimizerContext` with artifact hash checks, no silent truncation, token limits, and stable context hashes.
- [x] Added `PolicyOptimizerCaller` with transient retry, refusal reprompt, one format repair, fallback chains, cancellation propagation, and attempt recording boundary.
- [x] Focused GREEN, repeat `-count=20`, race `-count=5`, `./init.sh`, and `git diff --check` all passed.
- [ ] Next exact action: implement `feat-029` Audit Package inspection and approved mappings in a new turn.

### feat-029 Complete (2026-09-24)

- [x] Added strict Audit Package manifest types and loader with package path containment and source identity checks.
- [x] Added deterministic source inspection with SHA-256, expected/actual counts, mapping status, and no payload leakage in inspection results.
- [x] Added immutable Mapping approval write with approver/timestamp/source-hash binding and approved Mapping validation.
- [x] Focused GREEN, repeat `-count=20`, race `-count=5`, `./init.sh`, and `git diff --check` all passed.
- [ ] Next exact action: implement `feat-030` canonical normalization and disagreement selection in a new turn.

### feat-030 Complete (2026-09-24)

- [x] Added deterministic JSONL/CSV/Markdown row readers and RFC6901/exact-column mapping evaluation.
- [x] Added Canonical Audit Record generation with source-field preservation, approved-mapping hash enforcement, and atomic normalized JSONL output.
- [x] Added disagreement selection and strict sanitized Quality Event JSONL import.
- [x] Focused GREEN, repeat `-count=20`, race `-count=5`, `./init.sh`, and `git diff --check` all passed.
- [ ] Next exact action: implement `feat-031` deterministic stratification and Local Error Mining in a new turn.

### feat-031 Complete (2026-09-24)

- [x] Added deterministic SHA-256 stratification with exact 70/20/10 assignment and unique record placement.
- [x] Added 50/30/100 batch packing, legal remainder redistribution, and small-dataset handling.
- [x] Added a validated Local Mining execution boundary over frozen batches.
- [x] Focused GREEN, repeat `-count=20`, race `-count=5`, `./init.sh`, and `git diff --check` all passed.
- [ ] Next exact action: implement `feat-032` global merge, case attachment, and policy diagnosis in a new turn.

### feat-032 Complete (2026-09-24)

- [x] Added deterministic Global Pattern merge over Local Pattern artifacts with independent coverage/batch/source/model recomputation.
- [x] Added representative, random, and boundary case attachment with stable seeded ordering and cap enforcement.
- [x] Added Case Adjudication boundaries that reject Candidate Gold promotion to approved/core.
- [x] Added Policy Diagnosis batching with the closed 12-cause set.
- [x] Focused GREEN, repeat `-count=20`, race `-count=5`, `./init.sh`, and `git diff --check` all passed.
- [ ] Next exact action: implement `feat-033` Change Requests, Proposal, Critic, and Resolver in a new turn.

### feat-025 Final Verification Complete (2026-09-24)

- [x] Added transient retry/backoff to Safety Review preflight and wired the configured retry policy into CLI preflight.
- [x] Increased example model profiles to `max_tokens=4000` and expanded Judge, Router, Expert, and Arbiter fallback chains to three-model coverage where needed.
- [x] Focused Safety Review tests, focused race tests, `./init.sh`, `./scripts/verify-safety-review.sh`, scope, and `git diff --check` all passed after the changes.
- [x] Final fresh live eval `/private/tmp/safety-review-feat025-final.NETWw0` returned `eval=PASS rotations=2`.
- [x] `rotation-a`: `clean=44 quarantine=6 unsafe_resolved=19 unsafe_safe=0 unsafe_quarantine=1 boundary_acceptable=10`.
- [x] `rotation-b`: `clean=47 quarantine=3 unsafe_resolved=18 unsafe_safe=0 unsafe_quarantine=2 boundary_acceptable=10`.
- [x] `feat-025` is now `done`; Safety Review final acceptance and handoff are complete.
- [ ] Next workstream: Policy Optimization `feat-026` through `feat-038`. Advertisement `feat-039`/`feat-040` remain separate blocked workstreams.

### feat-025 Earlier Blocked Attempts (2026-09-24)

- [x] Startup and baseline passed: `pwd`, harness/spec/plan/state reads, `git status --short`, `git log --oneline -5`, `./init.sh` -> exit 0, `./scripts/verify-safety-review-scope.sh` -> PASS, key-presence check -> exit 0, Hidden Gold -> present.
- [x] `./scripts/verify-safety-review.sh` -> `Safety Review verification: PASS`.
- [x] Fresh real eval attempt 1 (`/private/tmp/safety-review-feat025-eval.JhSWz0`) completed both rotations but failed `unaccounted_stage`: `rotation-b/prompt` had a terminal-failed Expert on a non-quarantined item. Safety metrics otherwise passed (`rotation-a unsafe_resolved=18`, `rotation-b unsafe_resolved=20`, `unsafe_safe=0`).
- [x] Fresh real eval attempt 2 (`/private/tmp/safety-review-feat025-eval2.DANUJv`) failed both `unsafe recall` and `unaccounted_stage`: `rotation-a unsafe_resolved=17`, `unsafe_safe=0`, `unsafe_quarantine=3`; `rotation-b/response` also had a terminal-failed Expert on a non-quarantined item.
- [x] Fresh real eval attempt 3 (`/private/tmp/safety-review-feat025-eval3.QDR1pU`) completed `rotation-a` but failed `rotation-b/prompt` preflight with `error_category=preflight`, before final metrics.
- [ ] No code, thresholds, or Hidden Gold were changed. Required external action: stabilize the configured live model/provider chains or explicitly approve a contract change for terminal-stage accounting, then rerun a fresh two-rotation `independent_profiles` eval.
- [ ] `feat-024` remains `done`; `feat-025` is `blocked`; no feature is `in-progress`.

### feat-024 Completion (2026-09-23)

- [x] Wired the missing role-aware request validator into `SafetyReviewCaller`, so an invalid first model result now triggers the configured format-repair call instead of terminating immediately.
- [x] Recomputed redundant Expert `evidence_source` and Arbiter category, primary, and case-type projections from locally validated evidence; projection mistakes no longer quarantine a record by themselves.
- [x] Added a conservative Safe gate: if either Judge returns `unsafe` and no Expert establishes a category, the Arbiter result is projected to quarantine rather than Safe.
- [x] Added focused unit coverage for request-repair routing, projection normalization, and Judge-conflict quarantine.
- [x] Real smoke: 1 response row -> `clean=1 quarantine=0`; 3 response rows -> `clean=3 quarantine=0` with Expert and Arbiter executed.
- [x] Formal `independent_profiles` hidden-Gold eval passed: `rotation-a clean=48 quarantine=2 unsafe_resolved=20 unsafe_safe=0 unsafe_quarantine=0 boundary_acceptable=10`; `rotation-b clean=47 quarantine=3 unsafe_resolved=18 unsafe_safe=0 unsafe_quarantine=2 boundary_acceptable=10`.
- [x] Fresh gates: `./init.sh` -> exit 0; `./scripts/verify-safety-review.sh` -> `Safety Review verification: PASS`; `./scripts/verify-safety-review-scope.sh` -> `safety review scope: PASS`; `git diff --check` -> exit 0.
- [ ] Residual risk: `GLM-5.3-Flash` remains in the closed profile set but was not used by the passing eval because its role-specific preflight had been unstable; the passing eval used independent DeepSeek/Qwen/MiniMax chains.
- [ ] Do not start `feat-025` in this session; the next session must run its required final verification and handoff steps.

### feat-024 Scope Gate Repair (2026-09-23)

- [x] Root cause: `main_test.go` had already been expanded by later CLI/batch-review work, but the Safety Review scope manifest still pinned the older `feat-015` hash. Reverting it would remove currently passing user-owned tests, so the reviewed current file became the new protected baseline.
- [x] Verified affected root tests before changing the scope baseline: `go test . -run '^(TestRunReconcileBatchSendsMultipleRowsPerRequest|TestRunLabelReviewBatchUsesUnifiedInput|TestRunAdvertisementReviewBatch|TestSafetyReviewMain)' -count=1 -v` -> exit 0.
- [x] Updated `scripts/safety-review-protected-go.sha256` so `main_test.go` is protected at current SHA-256 `7ce7047314d9608e305d159db24a07885a543fbe55c0325452c84c27e4fb6b93`.
- [x] Updated `scripts/verify-safety-review-scope.sh` to keep checking the manifest hash and to explicitly whitelist only the current already-present advertisement/reconcile Go files whose basenames do not start with `safety_review_`. Future new non-Safety Go files remain blocked unless listed.
- [x] Verification: `/bin/bash -n scripts/verify-safety-review-scope.sh` -> exit 0; `./scripts/verify-safety-review-scope.sh` -> exit 0; `./scripts/verify-safety-review.sh` -> exit 0; `git diff --check` -> exit 0; final `./init.sh` -> exit 0.
- [ ] State remains `feat-024=blocked`, `feat-025=pending`, and no feature `in-progress`. The resolved blocker was only the local scope gate; the remaining blocker is strict live Schema compliance plus formal real `independent_profiles` eval.
- [ ] Next exact action: hand the implementation model a bounded `feat-024` continuation prompt to resume provider/live acceptance from the now-passing scope gate. Do not start `feat-025`.

### feat-024 Scope Gate Stop (2026-09-23)

- [x] Startup completed: `pwd` -> `/Users/lijiayang/venus/SendLLM`; required document/state reads; `git status --short`; `git log --oneline -5`.
- [x] Baseline `./init.sh` -> exit 0. `zsh -lc 'test -n "$AI_GATEWAY_API_KEY"'` -> exit 0 without printing the value.
- [ ] `./scripts/verify-safety-review-scope.sh` -> exit 1 with `protected Go file changed: main_test.go`.
- [ ] `main_test.go` was already modified at startup (`git diff --numstat` reported `312 0`); this turn did not edit or revert it. The protected manifest expected hash `f5304e559642505929d7043167b6a0bf0975942bb42bc4a90dee605c505a5564`, while the current file hash is `7ce7047314d9608e305d159db24a07885a543fbe55c0325452c84c27e4fb6b93`.
- [ ] Per `docs/safety-review-agent-harness.md` section 3, a protected-file mismatch stops work. Provider diagnosis and single-profile smoke were not run after this gate; no model call, task directory, SQLite artifact, output artifact, credential value, Prompt/Response, or raw model output was created or printed.
- [ ] Second 2026-09-23 recovery recheck: `./init.sh` -> exit 0, key-presence check -> exit 0, and the same `./scripts/verify-safety-review-scope.sh` failure (`protected Go file changed: main_test.go`) -> exit 1. Provider diagnostics and smoke were stopped again before any real request.
- [ ] Third 2026-09-23 recovery recheck: `./init.sh` -> exit 0, key-presence check -> exit 0, and `./scripts/verify-safety-review-scope.sh` again exited 1 with `protected Go file changed: main_test.go`. This is the same blocker on three consecutive resumed goal turns; the goal is marked blocked pending ownership/approval resolution.
- [ ] Required human action: resolve ownership/approval for the pre-existing `main_test.go` change (restore it to the protected manifest baseline, or explicitly authorize a reviewed manifest update). Do not start `feat-025`.
- [ ] State remains `feat-024=blocked`, `feat-025=pending`, no feature `in-progress`; provider recovery and formal `independent_profiles` Hidden Gold eval remain unfinished.

### feat-039 Provisional Advertisement Integration (2026-09-22)

- [x] User explicitly selected `data/task-013/task-013.reviewed.v8.jsonl` as the latest authoritative base and selected V2 `extended_info.attack_domain=advertisement` for the new category.
- [x] Confirmed base count/unique IDs `170743/170743`, advertisement count/unique IDs `58658/58658`, and cross-source `trace_id` overlap `0`.
- [x] Generated ignored provisional output `data/task-013/task-013.reviewed.v8.with-advertisement.provisional.jsonl`; did not overwrite the authoritative v8 base or any file under `data/final`.
- [x] Deterministic advertisement migration removed legacy `extended_info.risk_type`, set `attack_method=""`, set unsafe `attack_domain="advertisement"` and safe `attack_domain=""`, cleared `response` for prompt-only consistency, and preserved original `trace_id`, input order, `attack_scenario`, explanation, annotation, and remaining fields.
- [x] Independent verification PASS: output `229401` rows and `229401` unique IDs; first `170743` rows byte-identical to v8; appended advertisement IDs and order exactly match source; advertisement safe/unsafe `10006/48652`; bad domain `0`; old risk type `0`; non-empty attack method `0`; non-empty response `0`.
- [x] Output SHA-256 `2b18e5b179a42a42bd3544b4d9b447a0701b000557a8fe6a62f6920fcb372ab4`. Manifest: `data/task-013/task-013.reviewed.v8.with-advertisement.provisional.manifest.json`.
- [ ] Semantic review remains unresolved: both light models failed the human gate. This provisional integration must not be represented as human-cleaned or used to mark `feat-039` done.

**Next exact action:** clean advertisement records independently, then publish a new version by replacing only the original 58,658 advertisement `trace_id` values under the manifest replacement contract.

### feat-039 Current Session (2026-09-21)

- [x] User approved the advertisement review design and required every persistent artifact to keep the original source `trace_id` unchanged.
- [x] Approved design committed at `docs/superpowers/specs/2026-09-21-advertisement-dataset-lightweight-review-design.md` (`13d5cba`).
- [x] Startup workflow completed: repository root, harness, base design, feature state, progress, handoff, recent commits, and relevant batch-review implementation were inspected.
- [x] Baseline `./init.sh` exited 0 before implementation, including formatting, all tests, race tests, and `go vet`.
- [x] `feat-039` is the sole `in-progress` feature. It depends only on completed `feat-014`; blocked `feat-024`, pending `feat-025`, and Policy Optimization remain untouched.
- [x] Wrote and self-reviewed `docs/superpowers/plans/2026-09-21-advertisement-dataset-lightweight-review.md`; feature state has exactly one `in-progress` entry and the plan has no placeholders.
- [x] Added `docs/advertisement-review-agent-harness.md` with five ordered checkpoints, protected-file boundaries, required RED/GREEN and mutation evidence, the full automated test matrix, an independent twelve-point output-integrity check, deterministic 512-row pilot sampling, fixed Qwen/DeepSeek acceptance thresholds, and an explicit human gate before the 58,658-row run.
- [x] Added `docs/advertisement-review-coding-model-prompt.md` as the single entry point for the implementation model and routed `AGENTS.md` to the feature-specific design, plan, and Harness.
- [x] Stopped before implementation at the user's request. No advertisement-review production Go file remains from this session; the next model must begin with Task 1 RED evidence.
- [x] Harness handoff verification: `validate-harness.mjs --target /Users/lijiayang/venus/SendLLM` -> `100/100`; unique-active-feature `jq` assertion -> exit 0; no `advertisement_review*.go` implementation/test files exist; `git diff --check` -> exit 0; final `./init.sh` -> exit 0 (format, all tests, race, and vet).
- [ ] Implement the compact advertisement review protocol, JSON-array import, resumable batch execution, unchanged partition exports, CLI wiring, and fake-provider tests through RED/GREEN cycles.
- [ ] Run a stratified real-model pilot only after local gates pass; do not merge advertisement data into `data/final`.

**Next exact action:** give the implementation model `docs/advertisement-review-coding-model-prompt.md`; after its startup checks and Task 1 tests are written, run `go test ./internal/service -run '^Test(Parse|Import)AdvertisementReview' -count=1 -v` and record the valid RED.

**Current checkpoint (2026-09-21 17:47):** Task 1 import contract. Startup `pwd`, `git status --short`, `git log --oneline -5`, and `./init.sh` completed; baseline exit 0. The next exact RED command is `go test ./internal/service -run '^Test(Parse|Import)AdvertisementReview' -count=1 -v` after Task 1 tests are written. Protected-path/source hashes are stored outside the repository at `/private/tmp/feat-039-protected-hashes-20260921174701.sha256`.

**Task 1 evidence (2026-09-21 17:50):** RED `go test ./internal/service -run '^Test(Parse|Import)AdvertisementReview' -count=1 -v` -> exit 1 because `parseAdvertisementReviewInput` and `ImportAdvertisementReview` were absent. GREEN with the same command -> exit 0. Repeat `go test ./internal/service -run '^Test(Parse|Import)AdvertisementReview' -count=10` -> exit 0. Race `go test -race ./internal/service -run '^Test(Parse|Import)AdvertisementReview' -count=10` -> exit 0. Changed `internal/service/advertisement_review.go` and `internal/service/advertisement_review_test.go`. Next checkpoint: Task 2 compact request/result/packing RED command `go test ./internal/service -run '^TestAdvertisementReview(Request|Results|Packing)' -count=1 -v`.

**Task 2 evidence (2026-09-21 17:55):** RED `go test ./internal/service -run '^TestAdvertisementReview(Request|Results|Packing)' -count=1 -v` -> exit 1 because compact request, result validator, and packing APIs were absent. GREEN with the same command -> exit 0. Repeat `go test ./internal/service -run '^TestAdvertisementReview(Request|Results|Packing)' -count=10` -> exit 0. Race `go test -race ./internal/service -run '^TestAdvertisementReview(Request|Results|Packing)' -count=10` -> exit 0. Model requests contain only `i,p,s`; compact results enforce cardinality/index/enums/38-class closure/safe-risk consistency; packing is consecutive, greedy, count-capped, and retains an oversized single row. Next checkpoint: Task 3 runner/export RED command `go test ./internal/service -run '^TestAdvertisementReview(Partition|Export)' -count=1 -v`.

**Task 3 evidence (2026-09-21 18:05):** Partition/export RED `go test ./internal/service -run '^TestAdvertisementReview(Partition|Export)' -count=1 -v` -> exit 1 for missing issue/export APIs. Runner RED `go test ./internal/service -run '^TestAdvertisementReviewRunner' -count=1 -v` -> exit 1 for missing `AdvertisementReviewConfig` and `AdvertisementReview`. GREEN `go test ./internal/service -run '^TestAdvertisementReview' -count=1 -v` -> exit 0. Affected package `go test ./internal/service -count=1` -> exit 0. Race `go test -race ./internal/service -run '^TestAdvertisementReview' -count=10` -> exit 0. Coverage now includes clean/issue routing in both label directions, overlap, scenario, uncertain, multiple codes, provider/format failures, invalid-batch splitting, missing-result retry, resume without repeat calls, cancellation drain, original-object deep equality, complete/disjoint partitions, manifest one-to-one, and payload-free report. Next checkpoint: Task 4 CLI RED command `go test . -run '^TestRunAdvertisementReviewBatch' -count=1 -v`.

**Task 4 evidence (2026-09-21 18:30):** RED `go test . -run '^TestRunAdvertisementReviewBatch' -count=1 -v` -> exit 1 because `-mode advertisement-review-batch` was rejected as unknown arguments. GREEN with the same command -> exit 0. Affected `go test . ./internal/service ./internal/lib/configs -run \'AdvertisementReview|TestLoad\' -count=1 -v` -> exit 0. Race `go test -race . ./internal/service -run '^Test.*AdvertisementReview' -count=10` -> exit 0. The CLI fake E2E observes compact-only user payload, preserves both original IDs, creates all four outputs, prints aggregate counts, and returns 0. Prompt, compact Schema, Qwen config, and DeepSeek fallback config were already generated and are used unchanged; README and minimal main wiring were added. Next checkpoint: Task 5 local structural checks and full local gates before any real-model pilot.

### feat-024 Hidden Gold Created (Codex, 2026-09-21)

- [x] Created local ignored `Safety_Review_P04B_Hidden.jsonl` at the repository root with 50 P04-B records and exact `10/10/10/10/10` distribution: Prompt Unsafe, Prompt Safe/hard-negative, Response Unsafe, Response Safe/hard-negative, and boundary/insufficient-context.
- [x] The hidden file uses only the strict eval fields `trace_id`, `scene`, `prompt`, `response`, `gold_label`, `case_type`, `risk_type`, and `source`; all `trace_id` values are unique; every row has non-empty `prompt` and `response`.
- [x] `git check-ignore -v Safety_Review_P04B_Hidden.jsonl` confirms the file is ignored by `.gitignore`; no Hidden Gold payload should be committed.
- [x] Local validation script returned `hidden_gold_validation=PASS rows=50 unique_trace_ids=50 distribution=10/10/10/10/10`.
- [x] Focused eval tests passed: `go test ./internal/service ./internal/api/cli -run '^TestSafetyReview(Eval|Acceptance)' -count=1` -> exit 0.
- [x] Closing gates passed: `./scripts/verify-safety-review-scope.sh` -> exit 0 and `./scripts/verify-safety-review.sh` -> exit 0.
- [x] Post-Gold provider probe still blocks formal eval: key presence check was true without printing the value; `/models` returned HTTP 200 with 0 bytes and `json_parseable=false`; `/chat/completions` returned HTTP 200 with 0 bytes and `json_parseable=false`.
- [x] `feat-024` remains `blocked` because formal real `independent_profiles` eval has not passed; the latest known provider blocker is `provider_empty_completion_body`. `feat-025` remains `pending`.

### feat-024 Gateway Correction (Codex, 2026-09-21)

- [x] User supplied the current gateway matrix showing `deepseek-v4-pro` belongs under the Aliyun OpenAI-compatible Base URL, not `/ai/deepseek/openai`.
- [x] Updated the local real config source `模型配置.md` and `config/safety-review-single-model.example.yaml` to `https://aigateway.venusgroup.com.cn/ai/aliyun/openai`; kept the generic `safety-review.example.yaml` and `safety-review-eval.example.yaml` placeholder profiles on `provider.invalid` as required by tests.
- [x] Sanitized provider probes confirmed the old `/ai/deepseek/openai` endpoint still returns HTTP 200 with 0 bytes, while `/ai/aliyun/openai` returns parseable chat responses. Four model probes returned parseable chat bodies for `glm-5.2`, `qwen3-max`, `MiniMax-M2.5`, and `deepseek-v4-pro`; no key, Authorization value, Prompt, Response, or raw model output was printed.
- [x] A repository-external single-profile smoke used `/private/tmp/safety-review-aliyun-smoke.1qIvDQ` with three unlabeled rows and the Aliyun Base URL. It progressed past preflight and produced sanitized exports, but all three rows were quarantined with `invalid_result`; the run was interrupted after roughly ten minutes to stop further real-model spend. `run-status.json` recorded `status=interrupted`, `acceptance_state=unvalidated`, `clean=0`, `quarantine=3`.
- [x] Current blocker changed: API key and Base URL are now usable; `feat-024` remains blocked because model outputs do not yet satisfy the strict role Schemas in a real run and formal `independent_profiles` eval has not passed. `feat-025` remains `pending`.

### feat-024 Pre-feat-025 Gate Check (Codex, 2026-09-21)

- [x] Codex handoff gate confirmed repository root `/Users/lijiayang/venus/SendLLM`; `git status --short` and recent commits were reviewed without reverting unrelated dirty workspace changes.
- [x] Feature state remains dependency-safe: `feat-016` through `feat-023` are `done`, `feat-024=blocked`, `feat-025=pending`, and `jq '[.features[] | select(.status=="in-progress")] | length'` returned `0`.
- [x] Key prerequisite: `zsh -lc 'test -n "$AI_GATEWAY_API_KEY"'` -> exit 0, and no key value was printed.
- [x] Hidden Gold prerequisite: `test -f Safety_Review_P04B_Hidden.jsonl` -> exit 1. Formal `independent_profiles` eval and `feat-025` therefore remain blocked regardless of provider state.
- [x] Provider prerequisite rechecked once without printing Authorization, key, dataset Prompt/Response, or raw model output: `/models` returned HTTP 200 with 0 bytes and `json_parseable=false`; `/chat/completions` returned HTTP 200 with 0 bytes and `json_parseable=false`.
- [x] Local Hidden Gold search checked the repository, Desktop, and Downloads for `Safety_Review_P04B_Hidden.jsonl` and similar P04B Hidden/Gold JSONL names; no candidate file was found. A broader home-directory search produced no matches before being stopped to avoid wasting time.
- [x] Scope remained clean: `./scripts/verify-safety-review-scope.sh` -> exit 0. No single-profile smoke, formal eval, production code change, commit, run directory, output artifact, or SQLite artifact was created by this gate check.
- [x] Conclusion: the repository is frozen at the `feat-025` pre-start boundary, but `feat-025` is not startable until approved Hidden Gold is present and `feat-024` can complete formal `independent_profiles` live acceptance.

### feat-024 Provider Recovery Recheck (2026-09-21)

- [x] Startup recheck completed: `pwd` -> `/Users/lijiayang/venus/SendLLM`; required document/state reads; `git status --short`; `git log --oneline -5`; baseline `./init.sh` -> exit 0; `./scripts/verify-safety-review-scope.sh` -> exit 0.
- [x] State remains `feat-024=blocked`, `feat-025=pending`, and no feature is in progress. `zsh -lc 'test -n "$AI_GATEWAY_API_KEY"'` -> exit 0 without printing the value.
- [x] Provider diagnosis against the pinned DeepSeek gateway/model: `/models` returned HTTP 200 with 0 response bytes and body_nonempty=false; `/chat/completions` first returned HTTP 200 with 0 response bytes and body_nonempty=false, while the immediate retry returned HTTP 429 with 0 response bytes and body_nonempty=false. No Authorization, API key, Prompt, Response, or raw model output was printed.
- [x] The chat endpoint still has no non-empty parseable completion body, so the single-profile smoke was not retried. No model classification call was made, no new task/run directory or output artifact was created, and production code was unchanged.
- [x] Closing local gates: `go test ./internal/facade -run '^TestSafetyReviewOpenAIEmptySuccessBody$' -count=1 -v` -> exit 0; `go test ./internal/lib/configs ./internal/service ./internal/api/cli -run '^TestSafetyReview.*SingleProfile' -count=1` -> exit 0; `./init.sh` -> exit 0; `./scripts/verify-safety-review.sh` -> exit 0; `git diff --check` -> exit 0.
- [x] This recheck confirms the same provider blocker, now including an additional 429 empty-body response after the 200 empty-body request. `feat-024` remains blocked on `provider_empty_completion_body` plus the absent approved Hidden Gold/formal `independent_profiles` eval; `feat-025` remains pending.

### feat-024 Provider Recovery Recheck (Second Poll, 2026-09-21)

- [x] Startup completed again: `pwd` -> `/Users/lijiayang/venus/SendLLM`; required document/state reads; `git status --short`; `git log --oneline -5`; baseline `./init.sh` -> exit 0; `./scripts/verify-safety-review-scope.sh` -> exit 0.
- [x] State remains `feat-024=blocked`, `feat-025=pending`, and no feature is in progress. `zsh -lc 'test -n "$AI_GATEWAY_API_KEY"'` -> exit 0 without printing the value.
- [x] Provider diagnosis against the pinned DeepSeek gateway/model: `/models` returned HTTP 200 with 0 response bytes and body_nonempty=false; `/chat/completions` returned HTTP 200 with 0 response bytes, body_nonempty=false, and json_parseable=false. No Authorization, API key, Prompt, Response, or raw model output was printed.
- [x] The chat endpoint still has no non-empty parseable completion body, so the single-profile smoke was not retried. No model classification call was made, no new task/run directory or output artifact was created, and production code was unchanged.
- [x] Closing local gates for this poll: focused empty-success-body test -> exit 0; focused SingleProfile packages -> exit 0; `./init.sh` -> exit 0; `./scripts/verify-safety-review.sh` -> exit 0; `git diff --check` -> exit 0.
- [x] The same `provider_empty_completion_body` blocker persists across consecutive recovery checks. `feat-024` remains blocked on provider empty completions plus the absent approved Hidden Gold/formal `independent_profiles` eval; `feat-025` remains pending.

### feat-024 Single-Profile Real Smoke Provider Block (2026-09-20)

- [x] Startup completed: `pwd` -> `/Users/lijiayang/venus/SendLLM`; required documents/state reads; `git status --short`; `git log --oneline -5`; baseline `./init.sh` -> exit 0; `./scripts/verify-safety-review-scope.sh` -> exit 0.
- [x] State check: `feat-024=blocked`, `feat-025=pending`, and no feature is in progress. Real-key prerequisite `zsh -lc 'test -n "$AI_GATEWAY_API_KEY"'` -> exit 0; the value was not printed.
- [x] Focused single-profile gate: `go test ./internal/lib/configs ./internal/service ./internal/api/cli -run '^TestSafetyReview.*SingleProfile' -count=1 -v` -> exit 0.
- [x] Prepared a repository-external task at `/private/tmp/safety-review-single-profile-smoke.TKgl2A` from `config/safety-review-single-model.example.yaml`, with exactly three unlabeled rows: `single-profile-smoke-safe-001`, `single-profile-smoke-unsafe-001`, and `single-profile-smoke-boundary-001`. No Gold labels were added.
- [x] First `go run . safety-review run --config .../config.yaml` exited 1 with `error_category=policy` before a model request because the copied temporary config retained a repository-relative bundle path. The path was corrected to the immutable repository release path; this was a temporary-smoke configuration fix, not a production change.
- [x] Two subsequent `run` attempts both exited 1 with `error_category=preflight`. Real provider diagnostics showed authenticated `/models` access -> HTTP 200, while authenticated `chat/completions` probes with `json_object` and without `response_format`, including `max_tokens=2000`, all returned HTTP 200 with zero response bytes. An unauthenticated chat request returned HTTP 401. No request/response payload, key, or Authorization value was printed or persisted in the repository.
- [x] Durable smoke state: all 3 items remain `pending_initial`; `review_stages=9`; `review_decisions=0`; no `clean.jsonl` or `quarantine.jsonl` was produced. The boundary/unsafe/safe rows were therefore not classified and were not defaulted to Safe.
- [x] Codex follow-up added `TestSafetyReviewOpenAIEmptySuccessBody`, proving HTTP 200 with an empty body is treated as `malformed_response` rather than a successful completion. Focused facade tests and focused SingleProfile tests exited 0.
- [x] Final gates: `go clean -testcache && ./init.sh` -> exit 0; `./scripts/verify-safety-review.sh` -> exit 0; `./scripts/verify-safety-review-scope.sh` -> exit 0; both `/bin/bash -n` script checks -> exit 0; `git diff --check` -> exit 0; exact key-literal scan found 0 matches. Existing repository-local SQLite/run files predate this smoke; no new smoke artifact, credential, raw model output, or payload log was created in the repository.
- [x] Current blocker: the configured DeepSeek endpoint currently returns an empty 200 body, so operational preflight correctly fails. `feat-024` remains blocked also because approved Hidden Gold is absent and `independent_profiles` formal eval has not run. `feat-025` remains pending. Do not claim `single_profile` acceptance passed and do not start `feat-025`.

### feat-024 Single-Profile Real Smoke Attempt (2026-09-20)

- [x] Startup completed: `pwd` -> `/Users/lijiayang/venus/SendLLM`; full required document/state reads; `git status --short`; `git log --oneline -5`; baseline `./init.sh` -> exit 0; `./scripts/verify-safety-review-scope.sh` -> exit 0.
- [x] State check: `feat-024=blocked`, `feat-025=pending`, and no feature is in progress.
- [x] Real smoke prerequisite: `test -n "$AI_GATEWAY_API_KEY"` -> exit 1. The variable value was not printed.
- [x] Root cause check: Oh My Zsh had replaced the active startup path and the required env exports were still in `/Users/lijiayang/.zshrc.pre-oh-my-zsh`.
- [x] Restored the old API-key/proxy exports into the current `/Users/lijiayang/.zshrc` and `/Users/lijiayang/.zprofile`; `zsh -lc 'test -n "$AI_GATEWAY_API_KEY"'` and `zsh -ic 'test -n "$AI_GATEWAY_API_KEY"'` now exit 0 without printing the value.
- [x] Stopped before single-profile `validate` or `run`, made no real model call, and did not modify production code. No fake server or fake run was substituted.
- [x] Recovery condition was satisfied for the key; the current blockers are provider `chat/completions` returning HTTP 200 with zero response bytes and the still-missing approved Hidden Gold for formal eval.

### feat-024 Single-Profile Independent Review (2026-09-20)

- [x] Reviewed configuration validation, semantic fingerprinting, CLI side-effect ordering, role registry/runtime wiring, shared quota behavior, provider-failure quarantine, exports, and the example configuration. No production defect was found.
- [x] Strengthened the successful single-profile CLI test to use an Unsafe sample and prove that Judge A, Judge B, Router, Expert, and Arbiter each make a non-preflight classification request; the earlier Safe sample only exercised the three-stage shortcut.
- [x] The strengthened role-chain test passed once, with `-count=50`, and under `-race -count=20`.
- [x] The aggregate race gate exposed a test-fixture race between the fake HTTP handler and direct writes to failure-injection fields. Added locked setters/snapshot reads and replaced all direct writes; focused resume/single-profile race tests passed with `-count=20`.
- [x] Fresh `go clean -testcache && ./init.sh` passed formatting, all tests, full race, and vet. Scope, shell syntax, state consistency, and `git diff --check` also passed.
- [x] Operational `single_profile run` is implementation-verified but remains accuracy-unvalidated. `feat-024` stays blocked for real acceptance because approved Hidden Gold is absent and the current provider smoke failed preflight on empty HTTP 200 responses; `feat-025` stays pending.

### feat-024 Single-Profile Operational Repair (2026-09-20)

- [x] Startup completed: `pwd` -> `/Users/lijiayang/venus/SendLLM`; complete required documents/state reads; `git status --short`; `git log --oneline -5`; baseline `./init.sh` -> exit 0; `./scripts/verify-safety-review-scope.sh` -> exit 0.
- [x] RED 1: `go test ./internal/lib/configs ./internal/service ./internal/api/cli -run '^TestSafetyReview.*SingleProfile' -count=1 -v` first exited 2 because `SafetyReviewModelsConfig.ExecutionMode` was undefined, proving the execution-mode contract was absent.
- [x] RED 2: after config implementation, the same focused command exited 1 because `eval` on `single_profile` reported `error_category=input` instead of rejecting configuration before side effects.
- [x] Added `models.execution_mode` with backward-compatible `independent_profiles` default, strict `single_profile` validation for the sole `operational` profile/role mapping/shared quota, and semantic-fingerprint coverage for execution mode.
- [x] Added `config/safety-review-single-model.example.yaml` using the approved DeepSeek gateway/model/API env name without any key value.
- [x] Added config/service/CLI tests for legacy compatibility, valid/invalid single-profile contracts, fingerprint separation, five role chains, shared quota, five role-specific preflights, provider-failure quarantine, `acceptance_state=unvalidated`, and eval rejection before network/task-dir/SQLite side effects.
- [x] Updated Safety Review design, implementation plan, harness, README, and aggregate verification script to document that single-profile prompt isolation is not model independence and formal eval requires `independent_profiles` plus real Hidden Gold.
- [x] GREEN focused: `go test ./internal/lib/configs ./internal/service ./internal/api/cli -run '^TestSafetyReview.*SingleProfile' -count=1 -v` -> exit 0.
- [x] Affected packages: `go test ./internal/lib/configs ./internal/service ./internal/api/cli ./internal/dao . -count=1` -> exit 0.
- [x] Repetition: `go test ./internal/lib/configs ./internal/service ./internal/api/cli -run '^TestSafetyReview.*SingleProfile' -count=50` -> exit 0.
- [x] Race: `go test -race ./internal/lib/configs ./internal/service ./internal/api/cli -run '^TestSafetyReview.*SingleProfile' -count=20` -> exit 0.
- [x] Full clean gate: `go clean -testcache && ./init.sh` -> exit 0.
- [x] Aggregate gate: `./scripts/verify-safety-review.sh` -> exit 0.
- [x] Scope/syntax/diff: `./scripts/verify-safety-review-scope.sh` -> exit 0; both `/bin/bash -n` checks -> exit 0; `git diff --check` -> exit 0.
- [x] Static/security: line-length, Chinese-function-comment, dependency, credential, Authorization, Prompt/Response/raw-output, SQLite dump, and runtime-artifact scans passed. Matches contained only field/env names and synthetic test fixtures; no credential value was printed.
- [x] Real smoke prerequisite: read `模型配置.md`; `test -n "$AI_GATEWAY_API_KEY"` -> exit 1 in both normal and login shells, so no real model call or smoke run was made. This does not invalidate the passing fake implementation gates.
- [x] `test -f Safety_Review_P04B_Hidden.jsonl` -> exit 1. `feat-024` remains blocked for real acceptance; `feat-025` remains pending; no feature is in progress.

### feat-024 Independent Contract Review (2026-09-20)

- [x] Reviewed the model-only runtime contract tests and confirmed production `run` remains independent of Hidden Gold while `eval` remains strict.
- [x] Strengthened quarantine proof to require the expected `boundary-01` row and a non-Safe `manual_required` annotation.
- [x] Strengthened resume proof to count classification calls per `trace_id`, proving the completed item is not repeated and the interrupted item finishes its three initial stages.
- [x] Focused tests and focused race each passed with `-count=20`; scope and diff checks passed; `go clean -testcache && ./init.sh` exited 0.
- [x] No new production defect was found. `feat-024` remains blocked only for the real Hidden Gold acceptance claim; ordinary model-only `run` remains usable.

### feat-024 Model-Only Runtime Contract Repair (2026-09-18)

- [x] Startup completed on 2026-09-18: `pwd`; complete AGENTS/design/plan/harness/state reads; `git status --short`; `git log --oneline -5`; baseline `./init.sh` exited 0; `./scripts/verify-safety-review-scope.sh` exited 0.
- [x] Existing production behavior was already sufficient: ordinary `run` does not read Hidden Gold, uses SQLite, performs all-role preflight, exports all six artifacts, supports interruption/resume, and emits `acceptance_state=unvalidated`. This repair added focused contract tests rather than duplicating production behavior.
- [x] Added `internal/api/cli/safety_review_run_contract_test.go` covering model-only run without Gold, uncertain-item quarantine, interruption/resume without duplicate successful classification, and prohibition of `acceptance_state=passed`.
- [x] Updated `internal/api/cli/safety_review_eval_test.go`: renamed the missing-Gold case to `TestSafetyReviewEvalStillBlocksWhenHiddenGoldMissing`, added a model-output canary, and added a classification-count helper.
- [x] Mutation proof: temporarily removing `acceptance_state` from both report and run-status made all four new run-contract tests exit 1; restoring the field made the targeted command exit 0.
- [x] Focused GREEN: `go test ./internal/service ./internal/api/cli -run '^TestSafetyReview(Run|Eval|Export|Status|Acceptance)' -count=1 -v` exited 0.
- [x] Affected packages: `go test ./internal/service ./internal/api/cli ./internal/dao . -count=1` exited 0.
- [x] Repetition: `go test ./internal/service ./internal/api/cli -run '^TestSafetyReview(Run|Eval|Export|Status|Acceptance)' -count=50` exited 0.
- [x] Race: `go test -race ./internal/service ./internal/api/cli -run '^TestSafetyReview(Run|Eval|Export|Status|Acceptance)' -count=20` exited 0.
- [x] Full clean gate: `go clean -testcache && ./init.sh` exited 0.
- [x] Aggregated gate: `./scripts/verify-safety-review.sh` exited 0.
- [x] Scope/syntax/diff: `./scripts/verify-safety-review-scope.sh`, both `/bin/bash -n` checks, and `git diff --check` all exited 0.
- [x] Security/static: line-length, Chinese-comment, credential, Authorization, production logging, raw-output, and runtime-artifact scans passed. The only API-key matches were safe test env names and protocol field names; no credential value was printed.
- [x] `test -f Safety_Review_P04B_Hidden.jsonl` still exits 1. `feat-024` remains `blocked`; `feat-025` remains `pending`. Ordinary model-only `run` is usable, but real eval acceptance is not passed. Do not start `feat-025`.

### feat-024 Review Repair (2026-09-18)

- [x] Confirmed the repository still has no `Safety_Review_P04B_Hidden.jsonl`; the existing `Test_Input.jsonl` has only `prompt`, `response`, and `trace_id`, so it cannot replace approved P04-B Gold.
- [x] Fixed `eval` reporting to print actual per-rotation output metrics instead of hard-coded values; added a fake E2E regression assertion for the computed metrics.
- [x] Focused Eval/Acceptance/Live/Rotation, repetition `-count=50`, race `-count=20`, and `./scripts/verify-safety-review.sh` all exited 0; scope and diff checks passed.
- [ ] Real acceptance remains blocked. Required external action: provide the approved 50-row hidden Gold with exact `10/10/10/10/10` distribution at the configured local path, then rerun the real two-rotation gate. Do not copy or relabel `Test_Input.jsonl`; do not start `feat-025`.

### Runtime/Acceptance Boundary Revision (2026-09-18)

- [x] Corrected the design boundary: missing Hidden Gold blocks only `eval` accuracy claims and final production-readiness acceptance, not ordinary model-only `validate`/`run` execution.
- [x] Documented that model-only runs may complete with clean and quarantine exports; unresolved evidence is quarantined and never defaulted to Safe. Human review is optional post-run escalation.
- [x] Documented that pseudo-Gold, self-consistency, and model-generated consensus are diagnostics only and cannot replace approved Gold.
- [x] Added `acceptance_state=unvalidated` to model-only `report.json` and `run-status.json` exports, with export regression coverage; `go clean -testcache && ./init.sh` exited 0.

## Status

### feat-024 Current Session (2026-09-16)

- [x] Startup: confirmed `/Users/lijiayang/venus/SendLLM`; read AGENTS.md, base design, Safety Review design, implementation plan, Harness, feature list, progress, and handoff; recorded `git status --short` and `git log --oneline -5`.
- [x] Baseline: `./init.sh` exited 0 and `./scripts/verify-safety-review-scope.sh` exited 0 before edits.
- [x] `feat-023=done`; `feat-024` is the sole `in-progress` feature; `feat-025` remains `pending` and must not start.
- [x] Active scope is Task 10 only: blind role-rotation evaluation, independent artifact checks, anti-cheating tests, fake 50-row E2E with interruption/resume, and the mandatory real 50-row P04-B gate.
- [x] RED: `go test ./internal/service ./internal/api/cli -run '^TestSafetyReview(Eval|Acceptance|Live|Rotation)' -count=1 -v` exited 1 because the service evaluator and CLI `eval` command were absent.
- [x] Added `internal/service/safety_review_eval.go`: terminal-before-Gold check, strict 50-row Gold provenance/distribution, two-rotation grouping, formal result Schema validation, trace-set/duplicate checks, confusion/quarantine/boundary metrics, Expert-category and Response-method invariants, audit-stage accounting, and per-rotation gates.
- [x] Added `internal/api/cli/safety_review_eval.go` behavior in `safety_review_cli.go`: hidden input split by scene without reading Gold labels, two independent task dirs, A/B role swap, four scene runs, and evaluator invocation only after terminal exports.
- [x] Added `config/safety-review-result-schema.json` and `scripts/verify-safety-review.sh`; fixed Decision JSON to always emit `label` and `quarantine_reason` as required by the frozen Arbiter Schema.
- [x] GREEN focused: `go test ./internal/service ./internal/api/cli -run '^TestSafetyReview(Eval|Acceptance|Live|Rotation)' -count=1 -v` exited 0.
- [x] Affected packages: `go test ./internal/service ./internal/api/cli ./internal/dao . -count=1` exited 0.
- [x] Repetition: `go test ./internal/service ./internal/api/cli -run '^TestSafetyReview(Eval|Acceptance|Live|Rotation)' -count=50` exited 0.
- [x] Race: `go test -race ./internal/service ./internal/api/cli -run '^TestSafetyReview(Eval|Acceptance|Live|Rotation|Run|Status|Export)' -count=20` exited 0.
- [x] Fake E2E: `go test ./internal/api/cli -run '^TestSafetyReviewEvalCommandFakeE2E$' -count=1 -v` exited 0 with 50 synthetic rows, safe/unsafe/boundary/quarantine paths, interruption/resume, two rotations, separate task dirs/state DBs, and httptest only.
- [x] Full clean gate: `go clean -testcache && ./init.sh` exited 0; final post-edit `./init.sh` also exited 0.
- [x] Aggregated Safety Review gate: `./scripts/verify-safety-review.sh` exited 0.
- [x] Scope/syntax/diff/static: `./scripts/verify-safety-review-scope.sh`, `/bin/bash -n scripts/verify-safety-review-scope.sh`, `/bin/bash -n scripts/verify-safety-review.sh`, and `git diff --check` all exited 0. Line-length, Chinese-comment, dependency, credential, Authorization, Prompt/Response/raw-output, and runtime-artifact scans passed.
- [x] Real prerequisites: `test -f Safety_Review_P04B_Hidden.jsonl` exited 1; `test -f 模型配置.md` exited 0; `test -n "$AI_GATEWAY_API_KEY"` exited 0 without printing the value.
- [x] `feat-024=blocked` because the mandatory local hidden set is missing. Exact manual action: place the approved 50-row `Safety_Review_P04B_Hidden.jsonl` at the repository root, then rerun the startup workflow and real eval gate. Do not start `feat-025`.

### feat-023 Codex Post-Closeout Repair (2026-09-10)

### feat-023 Codex Post-Closeout Repair (2026-09-10)

- [x] Independent review found one CLI robustness gap after the external code model closeout: if
  `service.NewSafetyReviewExporter` returned an error, `run` reported `error_category=export` but still attempted
  `exporter.Export`, which could panic on a nil exporter in that failure branch.
- [x] Repair: `run` now keeps a zero-value `SafetyReviewExportStats` and only calls `Export` when exporter construction
  succeeds. Normal successful export output is unchanged; constructor failures remain task-fatal export errors.
- [x] Focused GREEN: `go test ./internal/service ./internal/api/cli -run '^TestSafetyReview(Status|Export|CLI)'
  -count=1 -v` exited 0.
- [x] Affected packages: `go test ./internal/service ./internal/api/cli ./internal/dao . -count=1` exited 0.
- [x] Race: `go test -race ./internal/service ./internal/api/cli -run
  '^TestSafetyReview(Status|Export|CLI|Run|Cancel|Resume)' -count=20` exited 0.
- [x] Full clean gate: `go clean -testcache && ./init.sh` exited 0.
- [x] Scope/syntax/diff/static: `./scripts/verify-safety-review-scope.sh`, `/bin/bash -n
  scripts/verify-safety-review-scope.sh`, `git diff --check`, and touched-file 120-character line scan all exited 0.
- [x] Security: targeted scan found only test/env/canary strings; no production credential, Authorization,
  Prompt/Response/raw-output, or SQLite payload logging was introduced.
- [x] `feat-023` remains `done`; `feat-024 Safety Review evaluation and live P04-B acceptance` remains `pending`.

### feat-023 Current Session (2026-09-10)

- [x] Startup: confirmed `/Users/lijiayang/venus/SendLLM`; read AGENTS.md, base design, Safety Review design, implementation plan, Harness, feature list, progress, and handoff; recorded `git status --short` and `git log --oneline -5`.
- [x] Baseline: `./init.sh` exited 0 and `./scripts/verify-safety-review-scope.sh` exited 0 before edits.
- [x] At startup, `feat-022=done`; `feat-023` was the sole `in-progress` feature and `feat-024` remained `pending`.
- [x] RED: `go test ./internal/service ./internal/api/cli -run '^TestSafetyReview(Status|Export|CLI)' -count=1 -v` exited 1. Service compilation listed missing Exporter/StatusReporter APIs, and `TestSafetyReviewCLIStatusAndWatch` failed because `status --task-dir` was unsupported.
- [x] Added `internal/service/safety_review_export.go` with SQLite-only clean/quarantine/audit/quality-events/report/run-status exports, input-order compatibility projection, empty arrays, stale replacement, same-directory temp -> write -> sync -> close -> rename -> chmod, temp cleanup, and failure injection.
- [x] Added `internal/service/safety_review_status.go` with compact status snapshots, throughput, ETA, error aggregation, read-only SQLite access, TTY/non-TTY watch, and payload-free output.
- [x] Added `status --task-dir [--watch]` in `internal/api/cli/safety_review_cli.go` and integrated full export generation into run completion; export/status failures are task-fatal while decisions remain persisted.
- [x] Added DAO read-only detailed-summary, terminal-item, stage/attempt-summary, task-ID lookup, and nullable-decision support in `safety_review_queries.go` and `safety_review_stages.go`.
- [x] GREEN focused: `go test ./internal/service ./internal/api/cli -run '^TestSafetyReview(Status|Export|CLI)' -count=1 -v` exited 0.
- [x] Affected packages: `go test ./internal/service ./internal/api/cli ./internal/dao . -count=1` exited 0.
- [x] Repetition: `go test ./internal/service ./internal/api/cli -run '^TestSafetyReview(Status|Export|CLI)' -count=50` exited 0.
- [x] Race: `go test -race ./internal/service ./internal/api/cli -run '^TestSafetyReview(Status|Export|CLI|Run|Cancel|Resume)' -count=20` exited 0.
- [x] Full clean gate: `go clean -testcache && ./init.sh` exited 0 with formatting, all package tests, all race tests, and `go vet ./...`.
- [x] Scope/syntax/diff: `./scripts/verify-safety-review-scope.sh`, `/bin/bash -n scripts/verify-safety-review-scope.sh`, and `git diff --check` all exited 0.
- [x] Static/security: 120-character line scan had no matches; Chinese function-comment adjacency exited 0; credential scan found only field/env names and synthetic test canaries; production Prompt/Response/raw-output logging scan had no matches; runtime-artifact review found no repository run/output/SQLite additions.
- [x] Mutation proof 1: temporarily skipping publication made `TestSafetyReviewExportWritesAtomicArtifacts` exit 1; restoration made focused GREEN exit 0.
- [x] Mutation proof 2: temporarily ignoring `Sync` made `TestSafetyReviewExportFailureInjection/sync` exit 1; restoration made focused GREEN exit 0.
- [x] Mutation proof 3: temporarily skipping temp cleanup made partial-write/sync cases exit 1 with remaining temporary files; restoration made focused GREEN exit 0.
- [x] Mutation proof 4: temporarily leaking decision rationale into audit made the payload-canary assertion exit 1; restoration made focused GREEN exit 0.
- [x] Manual review completed for the exporter, status reporter, CLI integration, DAO queries, tests, and state files. No `feat-024`, eval, live acceptance, Policy Optimization, or protected legacy Go behavior was implemented. No real model call, credential value, source Prompt/Response, raw model output, SQLite dump, repository run directory, output JSONL, or commit was created.
- [x] `feat-023` is complete. The next unblocked feature is `feat-024 Safety Review evaluation and live P04-B acceptance`; do not start it in this session.

### feat-022 Codex Post-Closeout Repair (2026-09-10)

- [x] Independent review found two `feat-022` contract gaps after the external code model closeout. CLI preflight still
  used one generic request instead of each role's release prompt and Schema, and `run-status.json` write failures were
  swallowed instead of becoming task-fatal status errors.
- [x] RED 1: after extending `TestSafetyReviewCLIRunOrdersPreflightBeforeClaims`, focused CLI testing failed because all
  preflights were detected as generic/unknown rather than exactly two calls for each of `judge_a`, `judge_b`, `router`,
  `expert`, and `arbiter`.
- [x] RED 2: `TestSafetyReviewCLIRunStatusWriteFailureIsFatal` failed before repair because a blocked
  `run_status` parent still returned exit 0.
- [x] Repair: `run` now builds role-specific harmless preflight requests from the verified release bundle: Judge A/B use
  their own prompts and `judgment` Schema, Router uses its policy summary and `router` Schema, Expert uses one
  deterministic enabled rule card and `expert` Schema, and Arbiter uses structured benign prior outputs plus the
  `arbiter` Schema. Any preflight request-construction error is a preflight failure rather than falling back silently.
- [x] Repair: `writeSafetyReviewStatusFile` now returns errors from summary read, JSON encoding, temp write, sync,
  rename, and chmod. `run` reports `error_category=status` and exits 1 when status writing fails on a non-interrupted
  run.
- [x] GREEN targeted: `go test ./internal/api/cli -run
  '^(TestSafetyReviewCLIRunOrdersPreflightBeforeClaims|TestSafetyReviewCLIRunStatusWriteFailureIsFatal)$' -count=1 -v`
  exited 0.
- [x] Focused GREEN: `go test ./internal/api/cli . -run '^TestSafetyReview(CLI|Run|Validate|Main)' -count=1 -v`
  exited 0.
- [x] Affected packages: `go test ./internal/api/cli ./internal/service ./internal/dao ./internal/lib/configs .
  -count=1` exited 0.
- [x] Race repetition: `go test -race ./internal/api/cli ./internal/service -run
  '^TestSafetyReview(CLI|Run|Validate|Main|Cancel|Resume)' -count=50` exited 0.
- [x] Full clean gate on final code: `go clean -testcache && ./init.sh` exited 0 with formatting, all package tests,
  all race tests, and `go vet ./...`.
- [x] Scope/syntax/diff/static: `./scripts/verify-safety-review-scope.sh`, `/bin/bash -n
  scripts/verify-safety-review-scope.sh`, `git diff --check`, and touched-file 120-character line scan all exited 0.
- [x] Security: targeted scan found only field/env/test names and synthetic redaction markers in tests; no production
  logging path for Prompt/Response, raw model output, Authorization, API key values, or SQLite payload dumps was added.
- [x] `feat-022` remains `done`; `feat-023 Safety Review status and atomic exports` remains `pending`. No `feat-023`
  implementation, real model call, repository run directory, output JSONL, SQLite artifact, credential, or commit was
  created by this repair.

### feat-022 Current Session (2026-09-10)

- [x] Startup: confirmed `/Users/lijiayang/venus/SendLLM`; read AGENTS.md, base design, Safety Review design, implementation plan Task 8, Harness, feature list, progress, and handoff; recorded `git status --short` and `git log --oneline -5`.
- [x] Baseline: `./init.sh` exited 0 and `./scripts/verify-safety-review-scope.sh` exited 0 before edits.
- [x] `feat-021=done`; `feat-022` is the sole `in-progress` feature; `feat-023` remains `pending` and must not start.
- [x] RED: `go test ./internal/api/cli . -run '^TestSafetyReview(CLI|Run|Validate|Main)' -count=1 -v` exited 1 because `internal/api/cli` has no production implementation and the root `run` entry has no `safety-review` dispatch.
- [x] Added `internal/api/cli/safety_review_cli.go` with `RunSafetyReview`, strict validate/run parsing, zero-network validation, fixed run startup order, model/registry/quota assembly, policy snapshots, durable import, all-role preflight, recovery, bounded cancellation drain, atomic run-status, and safe summaries.
- [x] Added the minimal `main.go` dispatch branch for `args[0] == "safety-review"`; no old CLI branch was refactored.
- [x] Added DAO attempt persistence for `SafetyReviewCaller`, per-role quota-group selection, and API env propagation into persisted attempts.
- [x] Added `internal/api/cli/safety_review_cli_test.go` and root `safety_review_main_test.go` covering validate zero network, command/usage failures, missing policy/input/path, shared API env, missing API env, preflight-before-claims, preflight failure with zero executions/decisions, resume without duplicate classification, cancellation drain/no new claim/atomic run-status/exit 130, payload canaries, main dispatch, and legacy regression.
- [x] GREEN: `go test ./internal/api/cli . -run '^TestSafetyReview(CLI|Run|Validate|Main)' -count=1 -v` exited 0.
- [x] Affected: `go test ./internal/api/cli ./internal/service ./internal/dao ./internal/lib/configs . -count=1` exited 0.
- [x] Cancellation/race: `go test -race ./internal/api/cli ./internal/service -run '^TestSafetyReview(CLI|Run|Validate|Main|Cancel|Resume)' -count=50` exited 0.
- [x] Full clean gate: `go clean -testcache && ./init.sh` exited 0 with formatting, all package tests, all race tests, and `go vet ./...`.
- [x] Scope/syntax/diff/static: `./scripts/verify-safety-review-scope.sh`, `/bin/bash -n scripts/verify-safety-review-scope.sh`, `git diff --check`, 120-character line scan, and Chinese function-comment adjacency scan all exited 0.
- [x] Security: credential scan found only field/env names and test environment names; no Authorization value or API key value was printed. Production logging/raw-output scan had zero matches; the only canary match is the test fixture that proves redaction.
- [x] Mutation proof 1: temporarily making validate execute preflight made `TestSafetyReviewCLIValidateZeroNetwork` exit 1 with 10 network requests; restoration made it pass.
- [x] Mutation proof 2: temporarily moving preflight after runner execution made `TestSafetyReviewCLIRunOrdersPreflightBeforeClaims` exit 1 because classification request 1 preceded all preflights; restoration made it pass.
- [x] Mutation proof 3: temporarily removing the stopping-store/drain wrapper made `TestSafetyReviewCLICancellationDrainsAndWritesStatus` exit 1 because only one classification completed; restoration made it pass.
- [x] Mutation proof 4: temporarily adding a prompt canary to run-status made `TestSafetyReviewCLIPayloadRedaction` exit 1; restoration made it pass.
- [x] Manual diff review completed. No `feat-023`, status/export/eval, Policy Optimization, protected legacy Go file, or unrelated legacy behavior was implemented. Tests use only local `httptest` and `t.TempDir`; no real model call, credential value, source Prompt/Response, raw model output, SQLite dump, repository run directory, output JSONL, or commit was created.
- [x] Historical closeout: `feat-022` was complete and `feat-023` was then the next unblocked feature.

### feat-021 Current Session (2026-09-09)

- [x] Startup: confirmed `/Users/lijiayang/venus/SendLLM`; read AGENTS.md, base design, Safety Review design, implementation plan Task 7, Harness, feature list, progress, and handoff; recorded `git status --short` and `git log --oneline -5`.
- [x] Baseline: `./init.sh` exited 0 and `./scripts/verify-safety-review-scope.sh` exited 0 before edits.
- [x] `feat-020=done`; `feat-021` is the sole `in-progress` feature; `feat-022` remains `pending` and must not start.
- [x] RED: `go test ./internal/service -run '^TestSafetyReview(Arbiter|Decision)' -count=1 -v` exited 1. The behavior-level failures proved fixed primary selection, uncertain-Expert Safe prohibition, and Arbiter scheduling were absent; the final pre-implementation run also listed missing Arbiter prior-output/request/projection APIs.
- [x] Added `internal/service/safety_review_arbiter.go`: fixed-field structured prior outputs, blind Arbiter request assembly, final decision projection, DAO decision conversion, and `model_stage_exhausted` quarantine construction.
- [x] Strengthened `SafetyReviewValidator.ValidateDecision`: primary selection now uses established categories, frozen `primary_priority`, and lexical tie-break; uncertain Experts cannot resolve Safe; existing category, evidence, case-type, Response ownership, and quarantine closed-set defenses remain active.
- [x] Integrated Arbiter into `safety_review_scheduler.go`: pending Arbiter stages are claimed, requests receive original item plus structured prior outputs, successful results are locally projected into final decisions, and terminal Arbiter failure writes `model_stage_exhausted` quarantine without another model call.
- [x] Added `internal/service/safety_review_arbiter_test.go` covering primary selection/ties, label matrix, no-majority behavior, conflicts, Response evidence/method ownership, formal case types, hard-negative prerequisites, absent `risk_level`, all quarantine reasons, request blindness, prior-output structure, projection, scheduler decision persistence, and terminal-failure quarantine.
- [x] GREEN: `go test ./internal/service -run '^TestSafetyReview(Arbiter|Decision)' -count=1 -v` exited 0.
- [x] Affected packages: `go test ./internal/service ./internal/dao -count=1` exited 0.
- [x] Repetition: `go test ./internal/service -run '^TestSafetyReview(Arbiter|Decision)' -count=50` exited 0.
- [x] Race: `go test -race ./internal/service -run '^TestSafetyReview(Arbiter|Decision)' -count=10` exited 0.
- [x] Full clean gate: `go clean -testcache && ./init.sh` exited 0 with formatting, all package tests, all race tests, and `go vet ./...`.
- [x] Scope/syntax/diff/static: `./scripts/verify-safety-review-scope.sh`, `/bin/bash -n scripts/verify-safety-review-scope.sh`, `git diff --check`, 120-character line scan, and Chinese function-comment adjacency scan all exited 0.
- [x] Security: credential scan found only safe `APIKeyEnv` field/test-env names; production logging and raw-output scans had zero matches. Test-only synthetic leak markers exist solely to prove Arbiter request blindness.
- [x] Mutation proof 1: temporarily allowing Unsafe without an established Expert made `TestSafetyReviewDecisionLabelMatrixAndNoMajority/no_established_category_cannot_unsafe` exit 1; restoration made it pass.
- [x] Mutation proof 2: temporarily allowing Response attack methods made `TestSafetyReviewDecisionResponseMethodProhibited` exit 1; restoration made it pass.
- [x] Mutation proof 3: temporarily counting prompt-only evidence as Response evidence made `TestSafetyReviewDecisionResponseEvidenceOwnership` exit 1; restoration made it pass.
- [x] Mutation proof 4: temporarily accepting Safe with risk fields made `TestSafetyReviewDecisionSafeMustClearCategories` exit 1; restoration made it pass.
- [x] Mutation proof 5: temporarily preferring higher primary priority made `TestSafetyReviewDecisionPrimarySelection/specific_over_other` exit 1; restoration made it pass.
- [x] Manual diff review completed. No `feat-022`, CLI, export, eval, Policy Optimization, protected legacy Go file, or `main.go` behavior was modified or added. No real model call, credential, source Prompt/Response, raw model output, SQLite payload, run directory, output artifact, or commit was created.
- [x] `feat-021` is complete. The next unblocked feature is `feat-022 Safety Review CLI run and validate`; do not start it in this session.
- [x] Codex post-closeout review: re-ran `go test ./internal/service -run '^TestSafetyReview(Arbiter|Decision)' -count=50`, `go test -race ./internal/service -run '^TestSafetyReview(Arbiter|Decision)' -count=10`, `go clean -testcache && ./init.sh`, `./scripts/verify-safety-review-scope.sh`, `/bin/bash -n scripts/verify-safety-review-scope.sh`, and `git diff --check`; all exited 0. Reviewed Arbiter/Validator/Scheduler boundaries against the design and found no blocking code defect. Updated the stale bottom `session-handoff.md` recommendation from `feat-021` to `feat-022`.

### feat-020 Codex Review Repair (2026-09-09)

- [x] Independent review found a real-store gap after the external code model closeout: the scheduler writes the
  complete dual-Safe zero-candidate shortcut decision on the last terminal initial stage, but
  `SafetyReviewStore.CompleteStage` rejected every non-Arbiter decision. Fake-store scheduler tests could not catch the
  SQLite path.
- [x] RED: `go test ./internal/dao -run '^TestSafetyReviewCompleteStageAllowsInitialSafeShortcut$' -count=1 -v`
  exited 1 with `safety review decision for non-arbiter stage "router"`.
- [x] Repair: DAO now permits only the approved initial-stage Safe shortcut decision shape: role is one of Judge A,
  Judge B, or Router; outcome is `succeeded`; item/final state are `resolved_safe`; label is `safe`; `case_type` is
  `typical`; attack arrays are empty; primary fields and quarantine reason are empty. Other non-Arbiter decisions still
  fail with `ErrInvalidTransition`.
- [x] GREEN: the same targeted command exited 0.
- [x] DAO focused: `go test ./internal/dao -run '^TestSafetyReview' -count=1 -v` exited 0.
- [x] Repetition already run after repair: `go test ./internal/dao -run '^TestSafetyReview' -count=100`,
  `go test ./internal/service -run '^TestSafetyReview(Router|Expert|Transition)' -count=50`, and
  `go test ./internal/service ./internal/dao -count=1` all exited 0.
- [x] Mutation proof: temporarily removing the initial-stage shortcut allow-list made the targeted DAO test exit 1;
  restoring it made the targeted test exit 0.
- [x] Final verification: `go test -race ./internal/dao -run '^TestSafetyReview' -count=10`,
  `go test -race ./internal/service -run '^TestSafetyReview(Router|Expert|Transition)' -count=10`,
  `go clean -testcache && ./init.sh`, `./scripts/verify-safety-review-scope.sh`, shell syntax check for
  `scripts/verify-safety-review-scope.sh`, `git diff --check`, and touched-Go 120-character line scan all exited 0.
- [x] Final handoff/security review: `session-handoff.md` next-action references now select only `feat-021`.
  Targeted payload/credential scan over touched Go files found only field names, SQL column names, and fixed synthetic
  test strings; no logging path, credential value, raw Prompt/Response, raw model output, or SQLite payload dump was
  added.
- [x] `feat-020` remains `done`; `feat-021 Safety Review Arbiter and decisions` remains the next unblocked feature.

### feat-020 Current Session (2026-09-09)

- [x] Startup: confirmed `/Users/lijiayang/venus/SendLLM`; read AGENTS.md, base design, Safety Review design, implementation plan Task 6, Harness, feature list, progress, and handoff; recorded `git status --short` and `git log --oneline -5`.
- [x] Baseline: `./init.sh` exited 0 and `./scripts/verify-safety-review-scope.sh` exited 0 before edits.
- [x] Implemented Router candidate normalization in `internal/service/safety_review_router.go`: Prompt 3+3 caps, Response 0+3 caps, duplicate merge and sorting, feature-reference validation, unknown/out-of-bundle conversion to a policy coverage gap, and specific-discrimination suppression over `other_discrimination`.
- [x] Added blind Router and single-card Expert request builders in `safety_review_router.go` and `safety_review_expert.go`; requests contain only their approved policy inputs and never A/B/Router/other-Expert outputs or original labels, explanations, and annotations.
- [x] Integrated scheduler transitions in `safety_review_scheduler.go`: any candidate creates one `expert:<axis>:<category>` stage and `awaiting_experts`; zero candidates with failed shortcut, Router failure/incomplete coverage, or policy gap creates `arbiter` and `pending_arbiter`; all Expert terminal states create Arbiter without making a final semantic decision. Persisted initial-stage recovery and `independence_degraded` behavior remain intact.
- [x] Added DAO support in `internal/dao/safety_review_stages.go`: claimed work carries task scene, role-stage snapshots carry axis/category, and `ReadRoleStageResults` supports Expert completion transitions.
- [x] RED: `go test ./internal/service -run '^TestSafetyReview(Router|Expert|Transition)' -count=1 -v` exited 1 because routing-plan, Router/Expert request, and Stage scene APIs were absent.
- [x] GREEN: the same focused command exited 0. It covers Prompt 3+3 and Response 0+3 caps, duplicates, unknown cards, bad feature refs, six parallel Experts, one call per candidate, Router/Expert blindness, policy gaps, uncertain routing, mixed established categories, all-excluded Arbiter eligibility, shortcut cases, any-candidate Expert creation, and Expert-over-Router shared quota priority.
- [x] Repetition: `go test ./internal/service -run '^TestSafetyReview(Router|Expert|Transition)' -count=50` exited 0.
- [x] Race: `go test -race ./internal/service -run '^TestSafetyReview(Router|Expert|Transition)' -count=10` exited 0.
- [x] Affected packages: `go test ./internal/service ./internal/dao -count=1` exited 0.
- [x] Full clean gate: `go clean -testcache && ./init.sh` exited 0 with formatting, all package tests, all race tests, and `go vet ./...`.
- [x] Scope/syntax/diff: `./scripts/verify-safety-review-scope.sh`, `/bin/bash -n scripts/verify-safety-review-scope.sh`, and `git diff --check` all exited 0.
- [x] Static/security: Chinese function-comment adjacency and 120-character line-length scans exited 0; credential scan found only safe `APIKeyEnv` field/test-env names; direct payload-logging and raw-output scans had zero matches.
- [x] Mutation proof 1: temporarily allowing Response method candidates made `TestSafetyReviewRouterEnforcesCandidateLimits/response_method` exit 1; restoration made focused GREEN exit 0.
- [x] Mutation proof 2: temporarily suppressing the unknown-category policy gap made `TestSafetyReviewRouterUnknownCategoryCreatesPolicyGap` exit 1; restoration made focused GREEN exit 0.
- [x] Mutation proof 3: temporarily concatenating `RawJSON` into the Expert prompt made `TestSafetyReviewExpertRequestLoadsOneBlindRuleCard` exit 1; restoration made focused GREEN exit 0.
- [x] Mutation proof 4: temporarily giving Router higher shared-quota priority made `TestSafetyReviewTransitionExpertQuotaPriority` exit 1; restoration made focused GREEN exit 0.
- [x] Manual diff review completed for the Router, Expert, scheduler, DAO, tests, and state files. No CLI, export, eval, Arbiter final semantics, or Policy Optimization behavior was modified or added. No real model call, credential, source Prompt/Response, raw model output, SQLite payload, run directory, output artifact, or commit was created.
- [x] `feat-020` is complete. The next unblocked feature is `feat-021 Safety Review Arbiter and decisions`; do not start it in this session.

### feat-019 Codex Review Repair (2026-09-09)

- [x] Reviewed the external code model closeout for `feat-019` against the approved Safety Review design, implementation plan, and harness.
- [x] Re-ran startup evidence locally: `pwd` confirmed `/Users/lijiayang/venus/SendLLM`; `./init.sh` exited 0 before repair review, though the first run used cached package results.
- [x] Finding: resumed items with one already `succeeded` initial stage could avoid the three-stage transition because scheduler only counted completions observed in the current process. A persisted `independence_degraded` value was also not read back, so a resumed item could incorrectly regain Safe shortcut eligibility.
- [x] RED: `go test ./internal/service -run '^TestSafetyReviewRunnerResume(SkipsSucceededStage|PreservesStoredDegradation)$' -count=1 -v` exited 1. The behavior-level failure left item state at `pending_initial` instead of `resolved_safe` or `pending_arbiter`.
- [x] Repair: added a persisted initial-stage snapshot boundary, persisted `IndependenceDegraded` through `CompleteStage`, and serialized only the short finalization window that reads sibling stage results, computes the transition, and commits completion. Model calls remain parallel.
- [x] Added DAO coverage proving initial-stage snapshots read result JSON and persisted independence degradation across reopen.
- [x] GREEN targeted: `go test ./internal/service -run '^TestSafetyReviewRunnerResume(SkipsSucceededStage|PreservesStoredDegradation)$' -count=1 -v` exited 0.
- [x] DAO targeted: `go test ./internal/dao -run '^TestSafetyReviewReadInitialStageResultsIncludesStoredDegradation$' -count=1 -v` exited 0.
- [x] Focused: `go test ./internal/service -run '^TestSafetyReview(Runner|Scheduler|Transition|Judges|Router)' -count=1 -v` and `go test ./internal/dao -run '^TestSafetyReview' -count=1 -v` both exited 0.
- [x] Affected packages: `go test ./internal/service ./internal/dao -count=1` exited 0 after the repair.
- [x] Repetition: `GOMAXPROCS=1 go test ./internal/service -run '^TestSafetyReview(Runner|Scheduler|Transition|Judges|Router)' -count=100` exited 0; normal `-count=100` exited 0.
- [x] Race: `go test -race ./internal/service -run '^TestSafetyReview(Runner|Scheduler|Transition|Judges|Router)' -count=20` exited 0; `go test -race ./internal/dao -run '^TestSafetyReview' -count=10` exited 0.
- [x] Full clean gate: `go clean -testcache && ./init.sh` exited 0 with formatting, all tests, all race tests, and `go vet ./...`.
- [x] Scope/syntax/diff/line length: `./scripts/verify-safety-review-scope.sh`, `/bin/bash -n scripts/verify-safety-review-scope.sh`, `git diff --check`, and added-file 120-character scan all exited 0.
- [x] Security scan over repaired production files found only field/column names such as `Prompt`, `Response`, `RawResponse`, and `api_key_env`; no payload logging or credential value was added.
- [x] `feat-019` remains `done`. The next unblocked feature is still `feat-020 Safety Review Router and category Experts`.

### feat-019 Current Session (2026-09-08)

- [x] Startup: confirmed `/Users/lijiayang/venus/SendLLM`; read AGENTS.md, base design, Safety Review design, implementation plan Task 5, Harness, feature list, progress, and handoff; recorded `git status --short` and `git log --oneline -5`.
- [x] Baseline: `./init.sh` exited 0 and `./scripts/verify-safety-review-scope.sh` exited 0 before edits.
- [x] `feat-017=done` and `feat-018=done`; `feat-019` is the sole `in-progress` feature; `feat-020` remains `pending` and must not start.
- [x] RED: `go test ./internal/service -run '^TestSafetyReview(Runner|Scheduler|Transition|Judges|Router)' -count=1 -v` exited 1. Build failures were undefined `service.SafetyReviewTransitionGate`, `service.SafetyReviewRunner`, `service.NewSafetyReviewRunner`, and `service.SafetyReviewRunnerConfig`, proving the scheduler/runner APIs are absent rather than a test syntax or fixture failure.
- [x] GREEN focused: `go test ./internal/service -run '^TestSafetyReview(Runner|Scheduler|Transition|Judges|Router)' -count=1 -v` exited 0 after implementing the parallel scheduler, transition gate, and role-specific result parsing.
- [x] Affected package: `go test ./internal/service -count=1` exited 0.
- [x] Repetition with `GOMAXPROCS=1`: `GOMAXPROCS=1 go test ./internal/service -run '^TestSafetyReview(Runner|Scheduler|Transition|Judges|Router)' -count=100` exited 0.
- [x] Repetition: `go test ./internal/service -run '^TestSafetyReview(Runner|Scheduler|Transition|Judges|Router)' -count=100` exited 0.
- [x] Race: `go test -race ./internal/service -run '^TestSafetyReview(Runner|Scheduler|Transition|Judges|Router)' -count=20` exited 0.
- [x] Mutation proof 1: temporarily allowing A/B-only Safe shortcut made the transition matrix exit 1; restoration made the focused command exit 0.
- [x] Mutation proof 2: temporarily removing the Router completion requirement made the Router-incomplete case exit 1; restoration made the focused command exit 0.
- [x] Mutation proof 3: temporarily allowing degraded independence to shortcut made the degradation case exit 1; restoration made the focused command exit 0.
- [x] Mutation proof 4: temporarily repeating each processed stage made the resume-skip test exit 1 because call count became 4 instead of 2; restoration made the focused command exit 0.
- [x] Mutation proof 5: temporarily removing worker drain/cancel made the context-cancel drain test exit 1; restoration made the focused command exit 0.
- [x] Full clean gate: `go clean -testcache && ./init.sh` exited 0 with formatting, all package tests, all race tests, and `go vet ./...` passing.
- [x] Scope/syntax/diff: `./scripts/verify-safety-review-scope.sh`, `/bin/bash -n scripts/verify-safety-review-scope.sh`, and `git diff --check` all exited 0.
- [x] Static/security: new Go basename, Chinese function-comment adjacency, 120-character line length, dependency drift, credential value, direct payload logging, payload logging, and runtime-artifact scans all exited 0.
- [x] Manual review completed for the scheduler, transition, Judge/Router result parsing, scheduler tests, and the three state files. No legacy runner/importer/store/schema, `main.go`, CLI, export, eval, or Policy Optimization behavior was modified or added.
- [x] `feat-019` is complete. The next unblocked feature is `feat-020 Safety Review Router and category Experts`; do not start it in this session.

### feat-018 Current Session (2026-09-08)

- [x] Startup: confirmed `/Users/lijiayang/venus/SendLLM`; read AGENTS.md, base design, Safety Review design, implementation plan Task 4, Harness, feature list, progress, and handoff; recorded `git status --short` and `git log --oneline -5`.
- [x] Baseline: `./init.sh` exited 0 and `./scripts/verify-safety-review-scope.sh` exited 0 before edits.
- [x] `feat-016=done` and `feat-017=done`; `feat-018` is the sole `in-progress` feature; `feat-019` remains `pending` and must not start.
- [x] RED: `go test ./internal/service ./internal/lib/limiter -run '^TestSafetyReview(Model|Preflight|Caller|Quota)' -count=1 -v` exited 1. Build failures were undefined `service.NewSafetyReviewCaller`, `service.SafetyReviewModelRegistry`, `service.SafetyReviewCallerConfig`, `limiter.NewSafetyReviewQuota`, and related types, proving the model execution controls are absent rather than a test syntax or fixture failure.
- [x] GREEN focused: `go test ./internal/service ./internal/lib/limiter -run '^TestSafetyReview(Model|Preflight|Caller|Quota)' -count=1 -v` exited 0 after implementing model chains, harmless preflight, caller retry/repair/refusal/fallback policy, and priority quota.
- [x] Affected packages: `go test ./internal/service ./internal/lib/limiter -count=1` exited 0.
- [x] Repetition: `go test ./internal/service ./internal/lib/limiter -run '^TestSafetyReview(Model|Preflight|Caller|Quota)' -count=50` exited 0.
- [x] Race: `go test -race ./internal/service ./internal/lib/limiter -run '^TestSafetyReview(Model|Preflight|Caller|Quota)' -count=10` exited 0.
- [x] Mutation proof 1: temporarily retrying authentication/bad-model errors on the same profile made `go test ./internal/service -run '^TestSafetyReviewCallerFallback$' -count=1 -v` exit 1 because the primary model was called twice; restoration made the focused command exit 0.
- [x] Mutation proof 2: temporarily ignoring `Retry-After` made `go test ./internal/service -run '^TestSafetyReviewCallerRetryAfter$' -count=1 -v` exit 1 because the Sleeper received 1ms instead of 2s; restoration made the focused command exit 0.
- [x] Mutation proof 3: temporarily skipping repair/refusal attempt persistence made `go test ./internal/service -run '^TestSafetyReviewCaller(Repair|RefusalReprompt)$' -count=1 -v` exit 1 with one persisted attempt instead of two; restoration made the focused command exit 0.
- [x] Mutation proof 4: temporarily giving Router higher shared-group priority than Expert made `go test ./internal/lib/limiter -run '^TestSafetyReviewQuotaExpertPriority$' -count=1 -v` exit 1; restoration made the focused command exit 0.
- [x] Final affected package: `go test ./internal/service ./internal/lib/limiter -count=1` exited 0 after timestamp and semaphore cleanup.
- [x] Final repetition: `go test ./internal/service ./internal/lib/limiter -run '^TestSafetyReview(Model|Preflight|Caller|Quota)' -count=50` exited 0.
- [x] Final race: `go test -race ./internal/service ./internal/lib/limiter -run '^TestSafetyReview(Model|Preflight|Caller|Quota)' -count=10` exited 0.
- [x] Final full clean gate: `go clean -testcache && ./init.sh` exited 0 with formatting, all package tests, all race tests, and vet passing.
- [x] Scope/syntax/diff: `./scripts/verify-safety-review-scope.sh`, `/bin/bash -n scripts/verify-safety-review-scope.sh`, and `git diff --check` all exited 0.
- [x] Static/security: new Go basename, Chinese function-comment adjacency, 120-character line length, dependency drift, credential value, direct payload logging, payload logging, and runtime-artifact scans all exited 0.
- [x] Manual review completed for the eight new Go files and tests. No legacy runner/importer/store/schema, `main.go`, scheduler, CLI, export, eval, or Policy Optimization behavior was modified or added.
- [x] Codex review repair: found and fixed two remaining `feat-018` contract gaps. Caller attempt persistence now passes a `SafetyReviewRecordedAttempt` containing `task_id`, `trace_id`, and `stage_key` alongside the safe attempt payload, so later store integration can persist the attempt against the correct stage. Preflight now rejects any missing role chain instead of silently skipping unconfigured roles.
- [x] Codex RED 1: `go test ./internal/service -run '^TestSafetyReviewCallerSuccess$' -count=1 -v` exited 1 because `SafetyReviewRecordedAttempt` did not exist and the recorder contract could not carry task/stage identity; after repair the same command exited 0.
- [x] Codex RED 2: `go test ./internal/service -run '^TestSafetyReviewPreflightRejectsMissingRole$' -count=1 -v` exited 1 because preflight accepted a registry containing only Judge A; after repair the same command exited 0.
- [x] Codex mutation proof 1: temporarily omitting `TaskID`/`TraceID`/`StageKey` from the recorded attempt made `TestSafetyReviewCallerSuccess` exit 1; restoration made it pass.
- [x] Codex mutation proof 2: temporarily making preflight continue on missing role chains made `TestSafetyReviewPreflightRejectsMissingRole` exit 1; restoration made it pass.
- [x] Codex final verification after repairs: `go test ./internal/service ./internal/lib/limiter -run '^TestSafetyReview(Model|Preflight|Caller|Quota)' -count=1 -v`, `go test ./internal/service ./internal/lib/limiter -run '^TestSafetyReview(Model|Preflight|Caller|Quota)' -count=50`, `go test -race ./internal/service ./internal/lib/limiter -run '^TestSafetyReview(Model|Preflight|Caller|Quota)' -count=10`, `go test ./internal/service ./internal/lib/limiter -count=1`, and `go clean -testcache && ./init.sh` all exited 0.
- [x] Codex final scope/security: `./scripts/verify-safety-review-scope.sh`, `/bin/bash -n scripts/verify-safety-review-scope.sh`, `git diff --check`, new-Go basename scan, dependency diff, Chinese comment-adjacency scan, 120-character line-length scan, and credential/payload logging scan all passed. The payload/credential `rg` scan exited 1 because it found no matches.
- [x] `feat-018` is complete. The next unblocked feature is `feat-019 Safety Review parallel A, B, and Router scheduling`; do not start it in this session.

### feat-017 Current Session (2026-09-08)

- [x] Startup: confirmed `/Users/lijiayang/venus/SendLLM`; read AGENTS.md, base design, Safety Review design, implementation plan, Harness, feature list, progress, and handoff; recorded `git status --short` and `git log --oneline -5`.
- [x] Baseline: `./init.sh` exited 0 and `./scripts/verify-safety-review-scope.sh` exited 0 before edits.
- [x] `feat-016=done`; `feat-017` is the sole `in-progress` feature; `feat-018` remains `pending` and must not start.
- [x] RED: `go test ./internal/dao -run '^TestSafetyReview' -count=1 -v` exited 1. Build failures were undefined `dao.SafetyReviewTask`, `dao.SafetyReviewStore`, `dao.SafetyReviewStageWork`, `dao.SafetyReviewAttempt`, `dao.SafetyReviewDecisionRecord`, and `dao.SafetyReviewStageCompletion`, proving the durable-state API and behavior are absent rather than a test syntax or fixture failure.
- [x] GREEN focused: `go test ./internal/dao -run '^TestSafetyReview' -count=1 -v` exited 0 after implementing the independent six-table schema, open/task identity, UseNumber import, claim/complete/recover transactions, and summary query.
- [x] Affected package: `go test ./internal/dao -count=1` exited 0.
- [x] Repetition: `go test ./internal/dao -run '^TestSafetyReview' -count=100` exited 0.
- [x] Mutation proof 1: temporarily allowing task fingerprint/scene/snapshot mismatch made `go test ./internal/dao -run '^TestSafetyReviewStoreEnsureTaskIdentity$' -count=1 -v` exit 1; restoration made the focused command exit 0.
- [x] Mutation proof 2: temporarily skipping duplicate source conflicts made `go test ./internal/dao -run '^TestSafetyReviewImportJSONLDuplicateRules$' -count=1 -v` exit 1; restoration made the focused command exit 0.
- [x] Mutation proof 3: temporarily resetting succeeded stages and attempt counters made `go test ./internal/dao -run '^TestSafetyReviewRecoverRunningAcrossReopen$' -count=1 -v` exit 1; restoration made the focused command exit 0.
- [x] Final affected package: `go test ./internal/dao -count=1` exited 0.
- [x] Final repetition: `go test ./internal/dao -run '^TestSafetyReview' -count=100` exited 0.
- [x] Final race: `go test -race ./internal/dao -run '^TestSafetyReview' -count=10` exited 0.
- [x] Full clean gate: `go clean -testcache && ./init.sh` exited 0 with formatting, all package tests, all race tests, and vet passing.
- [x] Final line-length repair and focused GREEN: `go test ./internal/dao -run '^TestSafetyReview' -count=1 -v` exited 0 after wrapping four overlong lines.
- [x] Final full clean gate on the exact final code: `go clean -testcache && ./init.sh` exited 0 with formatting, all package tests, all race tests, and vet passing.
- [x] Scope/syntax/diff: `./scripts/verify-safety-review-scope.sh`, `/bin/bash -n scripts/verify-safety-review-scope.sh`, and `git diff --check` all exited 0.
- [x] Static/security: new Go basename, Chinese function-comment adjacency, 120-character line length, dependency drift, credential value, direct payload logging, payload logging, and runtime-artifact scans all exited 0.
- [x] Manual review completed for the eight new Go files and tests plus the independent SQL schema. No legacy Store/schema/importer/runner code, `main.go`, scheduler, model caller, preflight, quota, CLI, export, eval, or Policy Optimization behavior was modified or added.
- [x] Codex review repair: found and fixed two remaining `feat-017` gaps. Prompt-scene import now preserves a non-empty `response` as context instead of rejecting the row; downstream stage creation now rejects invalid `stage_key` values that do not match `arbiter` or `expert:<axis>:<category>`.
- [x] Codex RED 1: `go test ./internal/dao -run '^TestSafetyReviewImportJSONLAllowsPromptSceneContextResponse$' -count=1 -v` exited 1 because Prompt rows with response context were rejected; after the import fix, the same command exited 0.
- [x] Codex RED 2: `go test ./internal/dao -run '^TestSafetyReviewCompleteStageRejectsInvalidDownstreamKey$' -count=1 -v` exited 1 because invalid downstream stage keys were accepted; after the validation fix, the same command exited 0.
- [x] Codex mutation proof 1: temporarily restoring the Prompt-with-response rejection made `TestSafetyReviewImportJSONLAllowsPromptSceneContextResponse` exit 1; restoration made it pass.
- [x] Codex mutation proof 2: temporarily allowing downstream key mismatches made `TestSafetyReviewCompleteStageRejectsInvalidDownstreamKey` exit 1; restoration made it pass.
- [x] Codex final verification after repairs: `go test ./internal/dao -run '^TestSafetyReview' -count=1 -v`, `go test ./internal/dao -run '^TestSafetyReview' -count=100`, `go test -race ./internal/dao -run '^TestSafetyReview' -count=10`, `go test ./internal/dao -count=1`, and `go clean -testcache && ./init.sh` all exited 0.
- [x] Codex final scope/security: `./scripts/verify-safety-review-scope.sh`, `/bin/bash -n scripts/verify-safety-review-scope.sh`, `git diff --check`, new-Go basename scan, dependency diff, Chinese comment-adjacency scan, 120-character line-length scan, and credential/payload logging scan all passed. The payload/credential `rg` scan exited 1 because it found no matches.
- [x] `feat-017` is complete. The next unblocked feature is `feat-018 Safety Review model execution controls`; do not start it in this session.

### feat-016 Codex Final Repair (2026-09-08)

**Supersedes the 2026-09-08 sustained closeout only for the repaired findings below.**
Independent review found two remaining contract gaps plus stale evidence wording: Prompt Unsafe development examples using
`attack_method` were rejected, Arbiter validation could still trust malformed non-established Expert assignments in some
paths, and one historical line-length claim did not match the then-current file.

- [x] Reopened `feat-016` as the only active feature; `feat-017` remained `pending` and no SQLite work was started.
- [x] Baseline: `./init.sh` exited 0 and `./scripts/verify-safety-review-scope.sh` exited 0 before repair edits.
- [x] RED: `go test ./internal/service -run '^(TestSafetyReviewPolicyAcceptsPromptUnsafeMethodExample|TestSafetyReviewValidatorDecisionDefendsNonEstablishedAssignments)$' -count=1 -v` exited 1. Failures proved Prompt `jailbreak` development examples were rejected and malformed `not_established` Expert assignment could be accepted by Decision validation.
- [x] Implemented the minimal repair: Development JSONL now accepts Prompt Unsafe examples backed by either `attack_method` or `attack_domain` cards enabled for Prompt, while Response Unsafe remains restricted to `attack_domain`; Arbiter input validation now checks every Expert category/axis/scene assignment before using non-established Experts for rule or exclusion calculations; removed the unused established-map parameter from decision category validation.
- [x] Wrapped the previously overlong occupation target-attributes line and the new long test comment; the latest added-Go line-length scan produced no output.
- [x] GREEN: same RED command exited 0 after the repair.
- [x] Focused GREEN: `go test ./internal/dto ./internal/service -run '^TestSafetyReview(Policy|Prompt|Validator|Contract)' -count=1 -v` exited 0.
- [x] Affected packages: `go test ./internal/dto ./internal/service -count=1` exited 0.
- [x] Targeted repetition: `go test ./internal/service -run '^(TestSafetyReviewPolicyAcceptsPromptUnsafeMethodExample|TestSafetyReviewValidatorDecisionDefendsNonEstablishedAssignments)$' -count=50` exited 0.
- [x] Race: `go test -race ./internal/dto ./internal/service -run '^TestSafetyReview(Policy|Prompt|Validator|Contract)' -count=1` and `go test -race ./internal/dto ./internal/service -count=1` both exited 0.
- [x] Mutation proof 1: temporarily restoring the old Prompt-method rejection made `TestSafetyReviewPolicyAcceptsPromptUnsafeMethodExample` exit 1; after restoration the targeted command exited 0.
- [x] Mutation proof 2: temporarily skipping non-established Expert assignment validation made `TestSafetyReviewValidatorDecisionDefendsNonEstablishedAssignments` exit 1; after restoration the targeted command exited 0.
- [x] Full clean gate: `go clean -testcache && ./init.sh` exited 0 with formatting, all package tests, full race tests, and `go vet ./...`.
- [x] Scope/security: `./scripts/verify-safety-review-scope.sh`, `/bin/bash -n scripts/verify-safety-review-scope.sh`, `git diff --check`, Go comment-adjacency scan, added-Go line-length scan, dependency diff, new-Go basename scan, and payload/credential scan all exited 0. The only text matches for `Authorization`/API-key terms were historical prohibition/evidence notes, not secret values.
- [x] Independent release manifest verification used Python standard library parsing and SHA-256, matched all 29 files and aggregate `6ed1b1fd8ee0707ba265570275444fa46501c57d10038d1fd50c4976c5648ef5`.
- [x] Existing local SQLite/data artifacts were observed in the dirty workspace, but none were added or modified by this repair; no real model call, raw model output, Prompt/Response payload, run directory, or commit was created.
- [x] `feat-016` is now closed as `done`. The next unblocked feature is `feat-017 Safety Review durable SQLite state`.

### Integrated Safety Review / Policy Optimization Design Revision (2026-09-07)

- [x] Added `docs/superpowers/specs/2026-09-07-policy-optimization-implementation-contract.md` before external code generation. It freezes exact config/model roles, CLI flags/exits, version grammar, state graphs, Artifact names/sensitivity/status, 11-table executable SQLite DDL, canonical external contracts, deterministic IDs/selection/70-20-10 batching, all 13 Skill instructions and Schema roots, Context/Prompt compilation, retry/failure behavior, and fixtures.
- [x] Resolved contract inconsistencies before implementation: `analyze` includes Regression; Preview has terminal `preview_ready` and cannot be promoted; direct compile retains a different-family Critic; Critiques have one deterministic aggregate report; target release version is explicit config; small stream remainders use deterministic mixed batches without changing 70/20/10 assignment.
- [x] Added `docs/safety-policy-coding-model-master-prompt.md` as the only prompt that needs to be handed to an external coding model. It selects exactly one feature and routes to the Safety Review or Policy Optimization specialized prompt.
- [x] Contract Freeze verification completed: extracted SQL executed successfully in `sqlite3 :memory:` with exactly 11 tables; extracted strict YAML parsed with exactly 8 model roles; 13 Skills, 91 required Skill sections, and 13 Schema-root rows were counted; all Markdown fences were balanced; placeholder/obsolete-path/trailing-whitespace scans had no findings; `feature_list.json` remained 38 unique dependency-valid features with no active feature; the 52-row disposition matrix remained complete; Harness validation scored 100/100; `git diff --check` and final `./init.sh` exited 0.
- [x] Before `feat-015`, no Go or runtime asset implementation had been authorized or started. `feat-015` is now complete; the next external code-model session must begin with `feat-016`.

- [x] Treated `/Users/lijiayang/Downloads/Safety Review 与 Policy Optimization Agent 综合设计修改意见.md` as design requirements and checked all 52 numbered changes plus 20 final principles.
- [x] Updated Safety Review to consume one verified immutable release bundle, keep compiled prompts inside that bundle, and emit sanitized `quality-events.jsonl` for declared Audit Packages.
- [x] Added the separate Policy Optimization design with Audit adapters/provenance, three state layers, 13 fixed portable Skills, deterministic/model authority boundaries, stratified mining, Change Requests, Critic/Resolver, deterministic compiler, regression, human approval, and immutable release.
- [x] Added `docs/policy-optimization-design-change-matrix.md`: 51 changes accepted, item 48 partially accepted because semantic prompt compression must first become an approved Policy change; no complete item rejected.
- [x] Revised the Safety Review implementation plan for bootstrap release verification, in-bundle prompts, quality events, and Candidate/Approved/Core Gold provenance.
- [x] Added the optimizer implementation plan and Harness with strict sequential `feat-026` through `feat-038`, exact interfaces/tests/failure injection, anti-shortcut gates, and real multi-model acceptance.
- [x] Added separate coding-model prompts for Safety Review and Policy Optimization and routed both workflows from `AGENTS.md`.
- [x] Documentation verification: `feature_list.json` parsed with 38 unique dependency-valid features and no active feature; the disposition matrix contained exactly 52 numbered rows; Markdown fences were even; placeholder and obsolete path/version scans had no findings; `git diff --check` exited 0.
- [x] Harness validation scored 100/100 with instructions/state/verification/scope/lifecycle all 5/5.
- [x] Post-revision `./init.sh` exited 0; formatting, `go test ./...`, `go test -race ./...`, and `go vet ./...` passed. This validates the unchanged code baseline, not any optimizer implementation.
- [x] Before `feat-015`, no Safety Review Go implementation had been authorized or performed. `feat-015` is now complete; Policy Optimization cannot start before `feat-025=done`.

### feat-015 Current Session

#### feat-015 Comment Rework (2026-09-07)

- [x] Startup baseline: `./init.sh` and `./scripts/verify-safety-review-scope.sh` both exited 0.
- [x] Rework reason: every function in `internal/lib/configs/safety_review_config_test.go` must have an immediately preceding concise Chinese comment; no production behavior may change.
- [x] RED: the strict static adjacency command exited 1 and listed exactly eight functions without an immediately preceding comment: `TestSafetyReviewLoadValidConfig`, `TestSafetyReviewLoadRejectsUnknownFields`, `TestSafetyReviewValidate`, `TestSafetyReviewRoleFallbacksUseDifferentFamilies`, `TestSafetyReviewSemanticFingerprintSeparatesSemanticAndRuntimeValues`, `TestSafetyReviewExampleConfigsUseReservedHost`, `writeSafetyReviewConfig`, and `validSafetyReviewYAML`.
- [x] GREEN: strict static adjacency check, `go test ./internal/lib/configs -run '^TestSafetyReview' -count=1 -v`, `./scripts/verify-safety-review-scope.sh`, and `git diff --check` all exited 0. Only comments were added; no production behavior changed.
- [x] `feat-015` comment rework restored to `done`.

### feat-016 Contract Rework (2026-09-07)

- [x] Baseline inherited from this session startup: `./init.sh` and `./scripts/verify-safety-review-scope.sh` both exited 0.
- [x] RED: `go test ./internal/dto ./internal/service -run '^TestSafetyReview(Policy|Prompt|Validator|Contract)' -count=1 -v` exited 1. Behavioral failures: semantic card path mutation and escaping symlink were accepted; all 13 malformed Development JSONL variants were accepted; provenance was human=12/synthetic=3 instead of 11/4; invalid scenes passed; mixed Response evidence was rejected; and required decision contract invariants were not enforced.
- [x] Implemented strict scene validation, established-Evidence ownership, exact established-category projection, required/empty primary fields, resolved/quarantine `case_type` rules, Safe-with-established prohibition, hard-negative present-exclusion prerequisites, non-typical evidence requirements, symlink rejection, card path/axis/ID and frozen 16-card contract checks, and line-by-line Development JSONL validation.
- [x] GREEN config rework: `go test ./internal/lib/configs -run '^TestSafetyReview' -count=1 -v` exited 0.
- [x] GREEN focused: `go test ./internal/dto ./internal/service -run '^TestSafetyReview(Policy|Prompt|Validator|Contract)' -count=1 -v` exited 0.
- [x] Affected packages: `go test ./internal/dto ./internal/service -count=1` exited 0.
- [x] Race focused: `go test -race ./internal/dto ./internal/service -run '^TestSafetyReview(Policy|Prompt|Validator|Contract)' -count=1` exited 0.
- [x] Race full packages: `go test -race ./internal/dto ./internal/service -count=1` exited 0.
- [x] Clean full gate: `go clean -testcache && ./init.sh` exited 0 with formatting, all tests, full race tests, and vet passing.
- [x] Scope/syntax/diff: `./scripts/verify-safety-review-scope.sh`, `/bin/bash -n scripts/verify-safety-review-scope.sh`, and `git diff --check` all exited 0.
- [x] Independent manifest verification: all 29 listed assets matched byte size and SHA-256; aggregate independently recomputed as `8736655dcc00c8d9764999d213df5e1c5bd2bd592b99ff10ad55bf2ac0b3172e`.
- [x] Security and hygiene: credential, payload-logging, symlink, sensitive-artifact, run-directory, dependency-drift, feat-015 comment-adjacency, and added-Go-line-length scans all exited 0.
- [x] Provenance correction: 15 total examples; 11 `human_reviewed` rows now match the external human review Prompt/Response text byte-for-byte; 4 rows are `synthetic`; the prompt-only case was reclassified from `human_reviewed` to `synthetic`. No sample text was printed.
- [x] Semantic mutation checks recompute copied bundle file hashes, sizes, and aggregate hash before loading, so path mutation and all 13 malformed JSONL cases fail semantic validation rather than stale-manifest checks.
- [x] Manual diff review completed. No SQLite file, run directory, generated output, real model call, credential, or raw model output was added or exposed.
- [x] `feat-016` rework complete. Next unblocked feature remains `feat-017`; do not start it in this session.

### feat-016 Sustained Contract Rework (2026-09-08)

**Supersedes the 2026-09-07 `feat-016` closeout.** Further review found additional contract defects that must be fixed before `feat-016` can remain `done`.

- [x] Mandatory startup: confirmed `/Users/lijiayang/venus/SendLLM`; read AGENTS.md, base and Safety Review designs, implementation plan, Harness, feature list, progress, and handoff; recorded `git status --short` and `git log --oneline -5`.
- [x] Baseline: `./init.sh` exited 0 and `./scripts/verify-safety-review-scope.sh` exited 0.
- [x] `feat-015` remains `done`; `feat-016` is the only `in-progress` feature; `feat-017` remains `pending` and must not start.
- [x] RED: `go test ./internal/dto ./internal/service -run '^TestSafetyReview(Policy|Prompt|Validator|Contract)' -count=1 -v` exited 1 with behavioral failures: symlinked release.yaml, bundle root, and parent directory were accepted; mutated `target_attributes` was accepted; empty `rule_ids`, `variant+safe`, and Response Unsafe method `risk_type` were accepted; provenance was human=11/synthetic=4 and `human-hard-001` had no fixed expected hash; rule-card JSON emitted PascalCase; Candidate/Condition/Exclusion nil arrays emitted `null`; Arbiter used `expert_results`, accepted incomplete prior outputs, and had contradictory prompt wording; Expert accepted E0/E999/nonexistent/duplicate/empty evidence refs, source mismatch, and present exclusion without refs; ValidateDecision accepted established Experts without evidence, prompt-only Response evidence, unrelated-card rules, and hard-negative exclusion evidence without closure.
- [x] Implemented the sustained fixes: component-by-component bundle symlink rejection, frozen `target_attributes`, strict common-policy and Development JSONL contracts, closed Expert evidence references, Expert-established decision defenses, hard-negative exclusion closure, exact Arbiter prior-output fields, `prior_outputs` prompt input, snake_case DTO JSON, empty-array serialization, and fixed human provenance hashes.
- [x] Corrected provenance to 10 `human_reviewed` plus 5 `synthetic` examples; renamed the non-human hard case to a synthetic ID without changing its text.
- [x] GREEN focused: `go test ./internal/dto ./internal/service -run '^TestSafetyReview(Policy|Prompt|Validator|Contract)' -count=1 -v` exited 0 after the final Arbiter prompt wording and manifest regeneration.
- [x] Affected packages: `go test ./internal/lib/configs ./internal/dto ./internal/service -count=1` exited 0.
- [x] Mutation proof 1: temporarily allowing invalid Expert evidence references made `go test ./internal/service -run '^TestSafetyReviewValidatorExpertEvidenceReferences$' -count=1 -v` exit 1; after restoration, the focused command exited 0.
- [x] Mutation proof 2: temporarily allowing parent-path symlinks made `go test ./internal/service -run '^TestSafetyReviewPolicyRejectsSymlinkedBundleComponents$' -count=1 -v` exit 1; after restoration, the focused command exited 0.
- [x] Mutation proof 3: changing a fixed human provenance expected hash made `go test ./internal/service -run '^TestSafetyReviewPolicyFixedHumanProvenanceHashes$' -count=1 -v` exit 1; after restoration, the focused command exited 0.
- [x] Race: `go test -race ./internal/dto ./internal/service -run '^TestSafetyReview(Policy|Prompt|Validator|Contract)' -count=1` and `go test -race ./internal/dto ./internal/service -count=1` both exited 0 after final edits.
- [x] Clean full gate: `go clean -testcache && ./init.sh` exited 0 with formatting, all package tests, all race tests, and vet passing.
- [x] Scope/syntax/diff: `./scripts/verify-safety-review-scope.sh`, `/bin/bash -n scripts/verify-safety-review-scope.sh`, and `git diff --check` all exited 0.
- [x] Independent manifest verification: all 29 listed assets matched byte size and SHA-256; aggregate independently recomputed as `6ed1b1fd8ee0707ba265570275444fa46501c57d10038d1fd50c4976c5648ef5`.
- [x] Static/security review: Chinese function-comment adjacency, Go basename, 120-character line length, dependency drift, credential value, Authorization value, direct payload logging, and runtime-artifact scans all exited 0. Policy prompt scans only matched the words `credentials`/`hidden reasoning` in prohibition sentences, not secret values.
- [x] Manual review completed for the eight Safety Review Go files and tests, the P04-B release assets, and the three state files. No real model call, API key, dataset sample text, SQLite state, run directory, output artifact, or raw model output was printed or committed.
- [x] `feat-016` sustained rework is complete. The next unblocked feature is `feat-017`; do not start it in this session.

#### feat-015 Rework (2026-09-07)

- [x] Rework baseline: `./init.sh` exited 0 and `./scripts/verify-safety-review-scope.sh` exited 0 before edits.
- [x] Rework reason: `feat-015` must reject `base_url` userinfo without echoing credentials, explicitly cover both valid scenes and all invalid scenes, assert exact config-relative and task-dir-relative path resolution, and ensure new Test/helper functions have concise Chinese comments.
- [x] RED: `go test ./internal/lib/configs -run '^TestSafetyReview' -count=1 -v` exited 1. The only failure was `TestSafetyReviewRejectsUserinfoWithoutCredentialLeak`: `LoadSafetyReview()` returned nil error for `https://user:password@glm.example.test/v1`. Scene and exact path-resolution assertions passed.
- [x] GREEN: the same focused command exited 0 after `validateSafetyReviewBaseURL` rejected `parsed.User != nil` with a credential-free `ErrInvalidConfig` message.
- [x] Focused repetition 2: the same focused command exited 0 again.
- [x] Affected package: `go test ./internal/lib/configs -count=1` exited 0.
- [x] Race: `go test -race ./internal/lib/configs -run '^TestSafetyReview' -count=1` and `go test -race ./internal/lib/configs -count=1` both exited 0.
- [x] Scope/diff: `./scripts/verify-safety-review-scope.sh` and `git diff --check` exited 0.
- [x] Full gate: post-rework `./init.sh` exited 0.
- [x] `feat-015` rework complete and restored to `done`.

### feat-016 Current Session

- [x] Baseline after `feat-015` rework: `./init.sh` and `./scripts/verify-safety-review-scope.sh` both exited 0 before `feat-016` edits.
- [x] RED: `go test ./internal/dto ./internal/service -run '^TestSafetyReview(Policy|Prompt|Validator|Contract)' -count=1 -v` exited 1 because the Safety Review DTOs, `LoadSafetyReviewPolicy`, prompt builder, validator, and policy sentinel were undefined.
- [x] GREEN: the same focused command exited 0 after implementing the DTOs, immutable bundle loader, prompts, schemas, 16 rule cards, human-reviewed examples, and role validators.
- [x] Mutation proof 1: temporarily disabling the Response attack-method prohibition made `go test ./internal/service -run '^TestSafetyReviewValidatorDecision$' -count=1 -v` exit 1 with `response method error = <nil>`; restoring the check made the same command exit 0.
- [x] Mutation proof 2: temporarily allowing an unknown required condition made `go test ./internal/service -run '^TestSafetyReviewValidatorExpert$' -count=1 -v` exit 1 with `unknown condition error = <nil>`; restoring exact condition-ID checks made the same command exit 0.
- [x] Mutation proof 3: temporarily allowing an unknown decisive exclusion made `go test ./internal/service -run '^TestSafetyReviewValidatorExpert$' -count=1 -v` exit 1 with `unknown exclusion error = <nil>`; restoring exact exclusion-ID checks made the same command exit 0.
- [x] Final focused GREEN: `go test ./internal/dto ./internal/service -run '^TestSafetyReview(Policy|Prompt|Validator|Contract)' -count=1 -v` exited 0.
- [x] Affected packages: `go test ./internal/dto ./internal/service -count=1` exited 0.
- [x] Race: `go test -race ./internal/dto ./internal/service -run '^TestSafetyReview(Policy|Prompt|Validator|Contract)' -count=1` and `go test -race ./internal/dto ./internal/service -count=1` both exited 0.
- [x] Clean full gate: `go clean -testcache && ./init.sh` exited 0 with formatting, all tests, full race tests, and vet passing.
- [x] Scope/syntax/diff: `./scripts/verify-safety-review-scope.sh`, `/bin/bash -n scripts/verify-safety-review-scope.sh`, and `git diff --check` all exited 0.
- [x] Security: credential, payload-logging, out-of-scope-service, sensitive-artifact, dependency-drift, and added-Go-line-length scans all exited 0.
- [x] Release integrity: 30 files; 16 rule cards; 6 prompts; 4 schemas; 12 `human_reviewed` and 3 `synthetic` examples; aggregate hash independently reproduced as `56326a4eb604b437af2b79f55f2a99efaa942fa7e9feb8257fcd155875a3ee80`.
- [x] Manual diff review completed for all eight new Go files, all 30 release assets, and the three state files. No real model call, credential, dataset payload, SQLite state, run directory, or raw model output was used or exposed.
- [x] `feat-016` complete. Next unblocked feature: `feat-017`; do not start it in this session because its required count=100, race count=10, reopen, and transaction gates need a fresh full feature run.

- [x] Startup baseline: `./init.sh` exited 0 before edits, including formatting, `go test ./...`, `go test -race ./...`, and `go vet ./...`.
- [x] Loaded `docs/safety-review-coding-model-prompt.md`, Safety Review Design, Implementation Plan, Harness, base design, `开发指南.md`, feature state, progress, handoff, and execution-plan instructions.
- [x] Pre-edit scope script was absent; created and ran `scripts/verify-safety-review-scope.sh` after hashing the current user-owned tracked Go files except `main.go`; baseline scope exit 0.
- [x] RED: `go test ./internal/lib/configs -run '^TestSafetyReview' -count=1 -v` exited 1 because `configs.LoadSafetyReview` and `configs.SafetyReviewConfig` were undefined.
- [x] GREEN: the same focused command exited 0 after implementing `LoadSafetyReview`, `SafetyReviewConfig.Validate`, path resolution, semantic fingerprinting, both example configs, and the scope script. The suite covers required fields, unknown keys, closed sets, references, family separation, limits, shared API env, output containment, relative paths, and semantic/runtime fingerprint separation.
- [x] Focused repetition 1: `go test ./internal/lib/configs -run '^TestSafetyReview' -count=1 -v` exited 0.
- [x] Focused repetition 2: the same command exited 0 again.
- [x] Affected package: `go test ./internal/lib/configs -count=1` exited 0.
- [x] Race: `go test -race ./internal/lib/configs -run '^TestSafetyReview' -count=1` and `go test -race ./internal/lib/configs -count=1` both exited 0.
- [x] Scope failure injection: adding untracked `internal/lib/configs/bad.go` made `./scripts/verify-safety-review-scope.sh` exit 1 with the required basename rejection; the temporary file was removed.
- [x] Protected-file failure injection: temporarily changing `internal/lib/configs/load.go` initially exposed a scope-script `read` parsing bug. After fixing the parser, the same mutation made the script exit 1 with `protected Go file changed`; the exact user-owned file content was restored and its SHA-256 matched the manifest again.
- [x] Restored scope check: `./scripts/verify-safety-review-scope.sh` and `/bin/bash -n scripts/verify-safety-review-scope.sh` exited 0.
- [x] Full gate: final `./init.sh` exited 0 with formatting, `go test ./...`, `go test -race ./...`, and `go vet ./...` passing.
- [x] Manifest-tamper failure injection: appending one line to `scripts/safety-review-protected-go.sha256` made `./scripts/verify-safety-review-scope.sh` exit 1 with `safety review scope manifest was modified`; the manifest was restored and its SHA-256 matched `08beed0ba276e69c38d0e1d46bbacfc9570617f03b54dc5b7b3bac3530a25c33`.
- [x] Manual review tightened the scope script so manifest paths must still exist and remain tracked; final `./scripts/verify-safety-review-scope.sh` and `/bin/bash -n` exited 0.
- [x] Final security/scope: `git diff --check`, scope script, credential-pattern scan, payload-logging scan, out-of-scope-service scan, and added-Go line-length check all exited 0.
- [x] Manual diff review completed for `config/safety-review.example.yaml`, `config/safety-review-eval.example.yaml`, `internal/lib/configs/safety_review_config.go`, `internal/lib/configs/safety_review_config_test.go`, `scripts/verify-safety-review-scope.sh`, `scripts/safety-review-protected-go.sha256`, `feature_list.json`, `progress.md`, and `session-handoff.md`.
- [x] Scope invariant: all pre-existing tracked Go files except `main.go` remain byte-identical to the kickoff manifest; only new Go basenames begin with `safety_review_`.
- [x] `feat-015` complete. Next unblocked feature: `feat-016`; do not start it in this session.

### Safety Review Design Handoff (2026-09-04)

- [x] 2026-09-07 revision: Router now runs in parallel with A/B for every item; only complete dual-Safe plus complete zero-candidate Router can bypass Expert/Arbiter.
- [x] 2026-09-07 revision: stable employment identities belong only to `occupation_discrimination`; removed them from `other_discrimination` and recorded `P04B-DECISION-001`.
- [x] 2026-09-07 revision: formalized all four `case_type` values and disabled `risk_level` throughout the P04-B V1 generated contract.
- [x] 2026-09-07 revision: human-reviewed P04-B cases are the development/regression core; shared API env names are allowed; preflight failure requires zero claims/executions/decisions.
- [x] 2026-09-07 revision: Unsafe Gold requires resolved Unsafe >=18, resolved Safe =0, quarantine <=2; 50 rows are explicitly only the V1 gate.

- [x] Approved the precision-first Prompt/Response classification flow; Pair is excluded.
- [x] Fixed orthogonal `attack_method`/`attack_domain` semantics, plural retention fields, primary projection, and strict `is_attack` meaning.
- [x] Fixed parallel blind A/B, bounded Router, per-category blind Experts, non-voting Arbiter, degradation, fallback, preflight, state, status, export, and quarantine behavior.
- [x] Defined the first implementation scope as the P04-B discrimination pilot plus six Prompt attack methods. Missing policy categories quarantine instead of defaulting Safe.
- [x] Added the approved design at `docs/superpowers/specs/2026-09-04-safety-review-pipeline-design.md`.
- [x] Added the exact implementation sequence at `docs/superpowers/plans/2026-09-04-safety-review-pipeline-implementation.md`.
- [x] Added the coding-agent gate at `docs/safety-review-agent-harness.md` and registered `feat-015` through `feat-025`.
- [x] Baseline before documentation edits: `./init.sh` exited 0 on 2026-09-04; formatting, all tests, race tests, and vet passed.
- [x] 2026-09-07: completed `feat-015` with strict configuration, semantic fingerprints, example configs, and protected-file scope verification. The next coding session must select only `feat-016`.

### Safety Review External Acceptance Inputs

- [ ] The final live gate requires an uncommitted local `Safety_Review_P04B_Hidden.jsonl` with the approved 10/10/10/10/10 composition.
- [ ] All configured primary and fallback model API environment variables must be available for real preflight and both A/B role rotations.
- [ ] Absence of either prerequisite blocks `feat-024`; it may not be recorded as a skipped pass.

### What's Done

- [x] Completed `feat-014`: added independent `-mode label-review-batch` for inputs that already use the final label fields, keeping existing `reconcile-batch` behavior unchanged.
- [x] Added `internal/service/label_review.go`: it imports unified label-review JSONL into SQLite, sends batch `items[]` requests with original labels/explanations, validates per-result annotations, retries/fails per item, and exports final MASB rows.
- [x] Added `cmd/prepare-xguard-v1`: maps XGuard v1 categories into final `attack_domain` values, keeps `attack_method` empty, maps XGuard boundary types to `case_type`, splits user/assistant/pair records into prompt and response datasets, and writes optional combined output atomically.
- [x] Added `prompts/label-review-batch-system.txt` and `config/task.label-review-batch.example.yaml` for the new standard review entrypoint.
- [x] Real XGuard dry-run used `/private/tmp` only and produced the expected counts: `prompt=106529 response=130931 combined=237460`; all three temporary JSONL files passed `jq -e .`.
- [x] Completed `feat-013`: added independent `-mode reconcile-batch` that sends one system prompt with multiple reconcile items per model request while keeping existing `-mode reconcile` behavior unchanged.
- [x] Added `runtime.batch_size` and `runtime.batch_max_input_tokens`; defaults keep old configs at single-item behavior and both settings are runtime-only, so they do not change the semantic fingerprint.
- [x] Batch reconcile keeps SQLite state per item: valid returned `results[]` entries are marked succeeded individually, missing or invalid entries are retried individually, and whole-request failures write attempts for every claimed item.
- [x] Batch context protection uses conservative token estimation; when `batch_max_input_tokens` is exceeded, the runner splits the claimed batch into smaller requests and only fails a single row if it still cannot fit alone.
- [x] Rebuilt `./sendllm`; use `zsh -lic './sendllm -mode reconcile-batch -config ./config/task.010.yaml'` with an isolated `task.id` and `task.state` for batch reconcile runs.
- [x] Added model API key round-robin for multi-key runs: configs now accept `model.api_key_envs`, OpenAI requests rotate keys in memory, semantic fingerprints ignore credential env changes, SQLite attempts record only the safe env name, and reconcile logs include `api_key_env`.
- [x] Fixed task-007 reconcile risk matching so mapped source attack methods only match model `attack_method`, mapped source attack domains only match model `attack_domain`, and valid supplemental model fields are preserved instead of forcing a risk-type change.
- [x] Completed `feat-012`: added independent `-mode reconcile` task, reusing SQLite state, bounded Runner retries, interruption recovery, and safe progress while exporting modified 8-4 new-label output.
- [x] Updated reconcile model input to include original label, mapped attack_method/attack_domain, original risk_type/risk_level/case_type/is_attack, and original explanation while keeping generated Schema unchanged.
- [x] Added per-run reconcile JSONL log file `final.reconcile-log.jsonl`, overwritten each run, ordered by input index, and recording success/difference/state per line.
- [x] Implemented source-path-based legacy risk mapping for 38-Categories directories, field-level original/model comparison, original reason reuse, and model explanation replacement on disagreement.
- [x] Added `config/task.reconcile.example.yaml` and updated the quick-start guide with reconcile mode commands and paths.
- [x] Added focused unit and integration tests for reconcile input, mapping, merge rules, and full import/run/export flow.
- [x] Implemented new label fields `attack_method` and `attack_domain` in `ExtendedInfo`, Validator, adjudicate flow, prompt-only prompt, and result/output Schemas while keeping legacy `risk_type` behavior compatible.
- [x] Added `config/result-schema-v2.json` and `sendllm_output_jsonl_v2.md` as the new downstream output contract.
- [x] Added `prompts/my-batch-prompt-only-system.txt` for prompt-only inputs, removing response-related instructions and adding model self-assessed `quality_score` guidance.
- [x] Added backward-compatible `quality_score` support to `dto.Annotation`, result Schema, and exported JSONL Schema, with 0 to 1 range validation.
- [x] Added the standalone Go command `cmd/merge-failed` that backfills `manual_required` failure rows from their original labels, uses `meta.source_fields.reason` as the explanation, merges them with successful rows, and atomically writes a new JSONL file.
- [x] Replaced the temporary Python merge script with the Go command and removed the Python source and bytecode cache.
- [x] Added table-driven tests for unsafe/safe annotation backfill, unknown-label rejection, source-field preservation, and duplicate-ID skipping.
- [x] Verified real `task-004` output: `success=794 failed=47 merged=841 duplicates=0`; `./init.sh` passed formatting, full tests, race tests, and `go vet ./...`.
- [x] Fixed adjudicate mode resume: existing successful output lines are counted and preserved, then the next run appends new MASB 8-4 rows starting from the corresponding input line.
- [x] Added safe adjudicate diagnostics at the CLI boundary so failures include line/category context without logging raw prompt, response, or model output.
- [x] Rebuilt the local `./sendllm` binary after the adjudicate resume fix.
- [x] Classified gateway `data_inspection_failed` HTTP 400 responses as `content_rejected` and made adjudicate mode record those single-row failures in `*.failed` while continuing later rows.
- [x] Verified the real adjudicate run advanced past original line 15: success output now has 15 rows and `final_8_4.jsonl.failed` has 1 safe failure row.
- [x] Minimized adjudicate model input while keeping `messages` as the factual judgment basis: model requests now include `messages`, normalized original/model labels, and reasons, but no `source`、`meta`、top-level `label` or full `annotation` object.
- [x] Completed `feat-011`: adjudicate mode now uses SQLite as its progress source, resets failed rows for restart retry, exports from terminal SQLite state, and uses the shared Runner/limiter for real concurrent model requests.
- [x] Adjusted adjudicate model input to include both reasons: original reason from `meta.source_fields.reason` and model reason from `annotation.explanation`, while still excluding unrelated passthrough fields.
- [x] Added adjudicate Ctrl+C terminal export: after manual interruption, current SQLite `succeeded` rows are exported to `final_8_4.jsonl` in the normal output format before the CLI returns interrupted.
- [x] Started `feat-007` to align SendLLM with upstream `compact_jsonl` records.
- [x] Recorded the approved compact JSONL design in `docs/superpowers/specs/2026-08-11-compact-jsonl-input-design.md`.
- [x] Baseline `./init.sh` exited 0 before code edits on 2026-08-11.
- [x] Completed `feat-007`: compact `id` is now the internal stable item ID, model input is extracted from `messages`, original fields including `label` are preserved, and SendLLM results are written only under top-level `annotation`.
- [x] Updated the opt-in live acceptance reader to validate nested `annotation` output.
- [x] Analyzed the latest 52 failed records from `task-2026-08-07-002` using SQLite metadata and failed JSONL summaries only, without printing raw dataset payloads.
- [x] Re-ran those 52 failed records in conservative local retry tasks: 50 succeeded in `task-2026-08-10-retry-52`, then 1 more succeeded in `task-2026-08-10-retry-2`; 1 record remains blocked by provider `content_filter`.
- [x] Fixed provider malformed-response handling so transient malformed or empty provider responses are retryable instead of failing after one attempt.
- [x] Re-exported the original 948 succeeded records from SQLite and generated a recovered 1000-record coverage set: 999 success rows and 1 failed row.
- [x] Merged the failed-record cover workflow into the normal CLI flow: after a completed run still has failures, the CLI automatically resets those failed records and performs one conservative single-concurrency cover retry before final export.
- [x] Changed failed JSONL export from diagnostic-only rows to manual-supplement templates that preserve source fields and include annotation placeholders plus failure diagnostics.
- [x] Completed the user-approved final re-review exception: Runner result bookkeeping now survives caller cancellation while peers drain, and legacy `source_hash` values migrate lazily from persisted `raw_json` without masking true conflicts.
- [x] Proved the fixes with focused RED/GREEN tests, a 50-run deterministic concurrency stress, full DAO/Runner tests, race detection, the Harness gate, and parser fuzzing.
- [x] Re-ran the required 50-record real-model end-to-end acceptance with the final latest code in a fresh temporary state/output directory; existing local acceptance artifacts were not overwritten.
- [x] Closed the final review fix wave: normalized SQLite retry timestamps with legacy migration, preserved large JSON integers during hashing, and separated Runner claim/work cancellation for graceful drain.
- [x] Moved all fallible construction before task/import persistence, rejected non-positive `max_tokens`, and independently enforced fixed annotation enums.
- [x] Preserved HTTP failure classification across bounded body-read failures, ignored Export staging artifacts, and aligned the example config with the verified gateway settings.
- [x] Added the requested DTO and Limiter coverage gaps and verified the complete change set with focused, race, fuzz, formatting, test, and vet gates.
- [x] Completed `feat-006`: dependency/format cleanup, parser fuzz smoke, full Harness gate, real-gateway capability probe, auditable provider failures, and 50/50 real-model acceptance.
- [x] Completed `feat-005`: ordered success/failure export, atomic file replacement, thin CLI wiring, safe progress, exit codes, and operator documentation.
- [x] Completed `feat-004`: atomic SQLite item transitions, resumable bounded Runner, durable retries, format repair, shared cooldown, cancellation, and payload-free progress.
- [x] Confirmed local single-process CLI scope.
- [x] Confirmed JSONL input and output, SQLite recovery, OpenAI-compatible API, and configurable prompts/model parameters.
- [x] Confirmed input fields: required `trace_id`, at least one non-empty `prompt` or optional `response`, with unknown fields preserved.
- [x] Confirmed concurrency, rate limiting, retry, structured validation, export, package layout, and test strategy.
- [x] Created the repository harness and concrete feature dependency list.
- [x] Translated the complete persisted design specification into Chinese while preserving technical identifiers.
- [x] User approved the persisted Chinese design specification.
- [x] Wrote and self-reviewed the seven-task implementation plan.
- [x] Added mandatory minimal-code and Chinese-comment rules to `AGENTS.md` and the plan gate.
- [x] Updated final acceptance to require 50-record real-model end-to-end success using `模型配置.md`.
- [x] Completed `feat-001`: Go module, typed strict YAML configuration, prompt/schema/risk loading, semantic fingerprint, and DTO contracts.
- [x] Completed `feat-002`: SQLite WAL state store, task semantic identity checks, normalized-source idempotency, atomic JSONL import, and parser fuzz entry point.
- [x] Completed `feat-003`: OpenAI-compatible Chat Completions facade, structured-output request modes, safe provider error classification, 4 MiB response cap, JSON Schema validation, and MASB cross-field validation.

### What's Next

1. Generate repo-local XGuard review inputs when ready, for example:
   `go run ./cmd/prepare-xguard-v1 -input data/v1-test/xguard_target_zh_en.jsonl -prompt-output data/v1-test/xguard-v1-prompt-label-review-input.jsonl -response-output data/v1-test/xguard-v1-response-label-review-input.jsonl -combined-output data/v1-test/xguard-v1-label-review-input.jsonl`.
2. Copy `config/task.label-review-batch.example.yaml` for prompt and response runs, then set distinct `task.id`, `task.input`, `task.output`, and `task.state` for each run before executing `./sendllm -mode label-review-batch -config ...`.
1. For batch reconcile, set `runtime.batch_size` to a small value first, for example 3-5, and set `runtime.batch_max_input_tokens` conservatively for the target model context window.
2. To avoid affecting a currently running task, do not run old and new binaries concurrently against the same SQLite state file. Stop the old process first, or use a new `task.id` and `task.state`.
3. For multi-key throughput, replace `model.api_key_env` with `model.api_key_envs` in the task config and set each environment variable. Key env changes are runtime credentials and do not change the semantic fingerprint.
1. Resume adjudication with `zsh -lic './sendllm -mode adjudicate -config ./config/task.003.dark.adjudicate.yaml'`; SQLite state controls continuation, and `runtime.concurrency` currently drives real parallel requests.
2. Deleting `final_8_4.jsonl` or `final_8_4.failed.jsonl` is safe for re-export because output files are no longer progress state; deleting `task-003-dark-adjudicate.db` starts the adjudicate task from scratch.
3. If gateway 429s rise at the current `runtime.concurrency: 400` / `requests_per_minute: 40000`, lower those config values and restart; succeeded rows will not be called again, failed rows will retry.

## Blockers / Risks

- [ ] The account concurrency limit is reported as approximately 500, but real acceptance observed transient 429 responses at concurrency 16 and 4; RPM and TPM remain undocumented.
- [ ] One latest batch record is consistently rejected by the provider with `finish_reason=content_filter`; completing that record requires manual labeling or a separate approved provider/model/prompt path.
- [x] This gateway rejected `json_schema` with HTTP 400 while accepting an otherwise equivalent `json_object` request with HTTP 200.
- [ ] `deepseek-v4-pro` uses completion budget for reasoning; `max_tokens=500` produced `finish_reason=length` with empty content, while the accepted run used 2000.
- [ ] The original design target of concurrency 64 is superseded by real-gateway evidence; the current formal recommendation is concurrency 4 with continued calibration against 429s.
- [ ] Graceful timeout cancellation depends on `Completer.Complete` honoring its context; Go cannot forcibly terminate a permanently noncompliant implementation.
- [ ] Legacy retry-time migration follows the approved single-process architecture and does not coordinate concurrent process opens.

## Decisions Made

- Use SQLite as the durable source of truth and JSONL only for import/export.
- Keep the first release as one local CLI command with no HTTP server or external queue.
- Use a bounded synchronous HTTP worker pool; concurrency is configurable up to 500.
- Preserve unknown input fields and deterministic input ordering during export.
- Use fixed core annotation fields plus task-configurable risk enums and extensions.
- Apply Rex-HH/uber_go_guide_cn through executable rules in `AGENTS.md`.
- Treat unnecessary code and abstractions as debt; keep the first release minimal and require concise Chinese Go comments.
- Final completion requires a real DeepSeek gateway run over all 50 records; fake-provider integration tests are intermediate evidence only.
- Configuration defaults follow the approved example: concurrency 64, shutdown timeout 30s, request attempts 5, format repairs 2, backoff 1s to 60s, explanation length 10 to 70, and model timeout 60s.
- The real gateway acceptance uses `json_object`; `json_schema` was rejected by the provider.
- The final 50-record run uses `max_tokens=2000` because audit metadata proved that 500 tokens could be exhausted by reasoning before final content.
- The configuration default of concurrency 64 is a historical design target, not the current operating recommendation; real evidence sets the starting point to 4 with continued calibration.
- Store retry instants as fixed-width, nine-digit UTC RFC3339 text so SQLite lexical order matches chronological order; normalize legacy values when opening the single-process state store.
- Upstream cancellation stops new claims and drains in-flight calls until `shutdown_timeout`; task-level failures still cancel claims and work immediately.
- Complete Validator, OpenAI, Limiter, and Runner construction before `EnsureTask` or Import so invalid configuration cannot leave durable state.
- Completed-result progress queries remain caller-cancelable; caller cancellation is converted to bounded graceful drain instead of task abort, so blocked progress bookkeeping cannot exceed `shutdown_timeout`.
- On a stored hash mismatch, duplicate import re-canonicalizes persisted `raw_json`; exact sources update `source_hash` in the import transaction, while different current hashes remain `ErrTraceConflict` even when legacy hashes collide.
- A completed CLI run performs exactly one conservative cover pass for final failed records before export, using single concurrency, RPM at most 10 when unbounded, and at least 8 request attempts.
- Failed JSONL rows preserve source fields and include manual placeholders instead of diagnostic-only rows, so users can complete the row and merge it into the normal dataset.
- Adjudicate mode now follows the same SQLite terminal-state contract as the main flow: `succeeded` rows are not re-sent, `processing` rows recover through Runner startup, and `failed` rows are reset to `pending` on each adjudicate startup.
- Adjudicate output JSONL files are export artifacts only. The success and failed files are atomically regenerated from SQLite, so they can be deleted without losing progress.
- Adjudicate interruption uses a bounded background export context, because the signal context is already canceled by Ctrl+C and cannot safely drive SQLite export queries.

## Files Modified This Session

- `internal/service/label_review.go`, `internal/service/label_review_test.go` - New standard label-review batch service, importer/exporter, merge rules, and tests.
- `cmd/prepare-xguard-v1/main.go`, `cmd/prepare-xguard-v1/main_test.go` - XGuard v1 mapping/splitting command and tests.
- `main.go`, `main_test.go` - CLI mode wiring and integration coverage for `label-review-batch`.
- `prompts/label-review-batch-system.txt`, `config/task.label-review-batch.example.yaml` - New prompt and example configuration for the standard label review pipeline.
- `feature_list.json`, `progress.md`, `session-handoff.md` - `feat-014` status and verification evidence.
- `internal/service/reconcile.go`, `reconcile_internal_test.go`, `reconcile_test.go` - New independent reconcile mode, mapping/merge logic, and tests.
- `main.go`, `config/task.reconcile.example.yaml`, `docs/quickstart-model-adjudicate.md` - CLI mode wiring, example config, and operator docs.
- `feature_list.json`, `progress.md`, `session-handoff.md` - `feat-012` status and verification evidence.
- `internal/service/runner.go`, `runner_internal_test.go` - Caller-cancellation progress race regression and work-context bookkeeping fix.
- `internal/dao/import.go`, `sqlite_test.go` - Legacy source-hash compatibility, lazy transactional migration, and collision regressions.
- `feature_list.json`, `progress.md`, `session-handoff.md` - User-approved exception closeout and verification evidence.
- `.gitignore`, `README.md`, `config/task.example.yaml` - Export residue exclusions and verified gateway operating defaults.
- `main.go`, `main_test.go` - Side-effect-free construction ordering and graceful shutdown timeout wiring.
- `internal/dao/` - Fixed-width retry timestamps, legacy migration, precise JSON number hashing, and regressions.
- `internal/facade/`, `internal/service/`, `internal/lib/configs/` - Stable provider classification, bounded audit prefixes, strict invariants, Runner lifecycle, and construction validation.
- `internal/dto/*_test.go`, `internal/lib/limiter/limiter_test.go` - Unknown-field and cooldown non-shortening coverage.
- `feature_list.json`, `progress.md`, `session-handoff.md` - Final feature status, verification evidence, decisions, and residual risks.
- `internal/service/live_acceptance_test.go` - Tracked opt-in acceptance test for existing local artifacts; it never calls the model and reports aggregate counts only.
- `feature_list.json`, `progress.md`, `session-handoff.md` - Review round 2 reproducibility and verification evidence only.
- `internal/facade/openai.go`, `openai_test.go` - Preserve bounded provider failure responses for SQLite audit on HTTP and malformed-completion errors.
- `feature_list.json`, `progress.md`, `session-handoff.md` - Final verification status, real-model evidence, provider differences, and operator next actions.
- `main.go`, `main_test.go` - Thin CLI assembly, safe exit mapping, structured-output propagation, resume failure export, and fake-provider integration tests.
- `internal/dao/items.go` - Ordered streaming accessors for succeeded and failed export records.
- `internal/service/exporter.go`, `exporter_test.go` - Deterministic merged JSONL export, safe failure JSONL, atomic staging, and failure preservation tests.
- `README.md` - Build, configuration, operation, resume, tuning, output, audit, exit-code, and at-least-once semantics guide.
- `feature_list.json`, `progress.md`, `session-handoff.md` - Task 6 status, evidence, risks, and next action.
- `internal/lib/tokenizer/` - Conservative provider-neutral token estimation and tests.
- `internal/lib/limiter/` - Concurrency, RPM, TPM, shared cooldown, and release-safety limits with race coverage.
- `internal/service/retry.go`, `internal/service/retry_test.go` - Retry backoff and provider-failure classification.
- `internal/dao/items.go`, `items_test.go`, `schema.sql` - Atomic claim, expected-state transitions, attempt persistence, counts, retry scheduling, and export fields.
- `internal/service/runner.go`, `runner_test.go`, `progress.go` - Owned worker lifecycle, retries, format repair, recovery, cancellation, and safe progress.
- `internal/lib/limiter/limiter.go` - Exposes the validated concurrency bound to the Runner worker owner.
- `AGENTS.md` - Repository workflow, scope, safety, and Go coding rules.
- `feature_list.json` - Ordered implementation features and dependencies.
- `progress.md` - Current durable project state.
- `session-handoff.md` - Restart instructions and decisions.
- `init.sh` - Standard verification entrypoint.
- `.gitignore` - Sensitive dataset and local-state exclusions.
- `docs/superpowers/specs/2026-08-05-sendllm-design.md` - Approved design captured for review.
- `go.mod`, `go.sum` - Go 1.24 module and Task 1 dependencies.
- `internal/lib/configs/` - Strict configuration loading, validation, and fingerprint tests.
- `internal/dto/` - Source, annotation, completion DTO contracts and tests.
- `config/`, `prompts/` - Example MASB task configuration, risk taxonomy, Schema, and system prompt.

## Evidence Of Completion

- [x] `feat-014` baseline: `./init.sh` exited 0 before edits.
- [x] Label-review service RED/GREEN: `go test ./internal/service -run '^TestLabelReviewBatchUsesUnifiedOriginalLabels$' -count=1 -v` first failed because `LabelReviewBatch` and `LabelReviewConfig` did not exist, then exited 0 after implementation.
- [x] Label-review CLI RED/GREEN: `go test . -run '^TestRunLabelReviewBatchUsesUnifiedInput$' -count=1 -v` first failed with `error_category=arguments` because `label-review-batch` was not wired, then exited 0 after CLI wiring.
- [x] XGuard prepare RED/GREEN: `go test ./cmd/prepare-xguard-v1 -count=1 -v` first failed because `run` did not exist, then exited 0 after the converter was implemented.
- [x] Affected package gate: `go test ./cmd/prepare-xguard-v1 ./internal/service . ./internal/lib/configs -count=1` exited 0.
- [x] Full ordinary test gate: `go test ./... -count=1` exited 0.
- [x] Real XGuard dry-run: `go run ./cmd/prepare-xguard-v1 -input data/v1-test/xguard_target_zh_en.jsonl ...` wrote only `/private/tmp` outputs and printed `prompt=106529 response=130931 combined=237460`.
- [x] Real XGuard JSONL validation: `jq -e .` over the three `/private/tmp/sendllm-xguard-prepare-*/*.jsonl` outputs exited 0.
- [x] Final gate: `git diff --check`, `go build -trimpath -o ./sendllm .`, and `./init.sh` all exited 0.
- [x] Multi-key RED/GREEN: `go test ./internal/lib/configs ./internal/facade ./internal/dao ./internal/service -run 'Test(APIKeys|SemanticFingerprintIgnoresAPIKeyEnvs|OpenAI_CompleteRoundRobinsAPIKeys|Store_ItemLogIncludesLastAPIKeyEnv|Store_OpenMigratesMissingAttemptAPIKeyEnvColumn|ReconcileWritesFreshRunLog)' -count=1 -v` first failed for missing config/API/facade/DAO/log fields, then exited 0 after implementation.
- [x] Multi-key affected packages: `go test ./internal/lib/configs ./internal/facade ./internal/dao ./internal/service -count=1` exited 0.
- [x] Task-007 reconcile risk-match RED/GREEN: `go test ./internal/service -run '^TestMergeReconcileAnnotation$' -count=1 -v` first failed because a mapped domain returned in model `attack_method` was treated as a match, then passed after requiring same-category matching while preserving valid supplemental fields.
- [x] Task-007 attack-label Validator gate: `go test ./internal/service -run '^TestValidator_NewAttackLabels$' -count=1 -v` exited 0, including regressions that reject domain enums in `attack_method` and method enums in `attack_domain`.
- [x] Task-007 reconcile focused gate: `go test ./internal/service -run '^Test(Reconcile|ParseReconcileInput|ReconcileRiskLabels|MappedReconcileLabelSafeEmptiesRiskFields|ReconcileCaseType|MergeReconcileAnnotation|BuildReconcileOutputMarshalsNewLabels|ReconcileRequestIncludesOriginalLabelAndMappedRisk)' -count=1 -v` exited 0.
- [x] Task-007 reconcile full gate: `./init.sh` exited 0 with formatting, all tests, full race tests, and `go vet ./...` passing.
- [x] `go test ./internal/service -run '^TestReconcile' -count=1 -v` exited 0, covering full reconcile import/run/export with original-reason reuse and new-label output.
- [x] `go test ./internal/service -run '^TestReconcileWritesFreshRunLog$' -count=1 -v` exited 0, proving the run log is replaced and emits ordered per-line success/difference/state records.
- [x] Focused reconcile unit tests for mapping, safe-label clearing, case-type mapping, annotation merge, and output marshaling all exited 0.
- [x] `go test ./... -count=1`, `go test -race ./internal/service ./internal/dao ./internal/lib/configs -count=1`, `go vet ./...`, `git diff --check`, and `go build -trimpath -o ./sendllm .` all exited 0.
- [x] Final `./init.sh` exited 0 with formatting, all tests, full race tests, and `go vet ./...` passing.
- [x] `go test ./internal/dto ./internal/service -count=1` exited 0 with new `TestAnnotationPreservesAttackLabels` and `TestValidator_NewAttackLabels` coverage for `attack_method`/`attack_domain`.
- [x] `./init.sh` exited 0 after new label, schema, prompt, and adjudicate changes.
- [x] `go test ./internal/dto ./internal/service -run 'TestAnnotation(Rejects|PreservesQualityScore)|TestValidator_Validate' -count=1` exited 0 after adding `quality_score` acceptance and rejecting non-number/out-of-range values.
- [x] `./init.sh` exited 0 after the prompt-only prompt and `quality_score` Schema/DTO changes.
- [x] `go test ./cmd/merge-failed -count=1 -v` exited 0, covering `TestBuildAnnotationUnsafe`, `TestBuildAnnotationSafe`, `TestBuildAnnotationRejectsUnknownLabel`, `TestTransformFailedRecordPreservesSource`, and `TestMergeRecordsSkipsDuplicateFailedID`.
- [x] Real merge smoke `go run ./cmd/merge-failed` exited 0 with `success=794 failed=47 merged=841 duplicates=0` and produced `data/task-004/tesk-004.merged.jsonl`.
- [x] `./init.sh` exited 0 after the Go command was added, including `go test ./...`, `go test -race ./...`, and `go vet ./...`.
- [x] Adjudicate resume RED: `go test ./internal/service -run '^TestAdjudicate' -count=1 -v` exited 1 because adjudicate called the model for an already exported line.
- [x] Adjudicate focused GREEN: `go test ./internal/service -run '^TestAdjudicate' -count=1 -v` exited 0 after counting existing output lines, appending output, and skipping completed input lines.
- [x] Adjudicate CLI regression: `go test . -run '^TestRunAdjudicateOutputsMASBFormat$' -count=1 -v` exited 0.
- [x] Adjudicate wider gates: `go test ./... -count=1`, `go test -race . ./internal/service ./internal/lib/configs -count=1`, `git diff --check`, `go build -trimpath -o ./sendllm .`, and final `./init.sh` all exited 0.
- [x] Adjudicate content rejection RED/GREEN: `go test ./internal/facade -run '^TestOpenAI_CompleteClassifiesContentRiskBadRequest$/inspection_code$' -count=1 -v` and `go test ./internal/service -run '^TestAdjudicateRecordsRejectedLineAndContinues$' -count=1 -v` first failed, then passed after classifying `data_inspection_failed` and recording per-row failed sidecar entries.
- [x] Adjudicate live smoke: a short real run of `zsh -lic './sendllm -mode adjudicate -config ./config/task.003.dark.adjudicate.yaml'` advanced past the previous line 15 blocker; it was manually interrupted after verification, leaving `final_8_4.jsonl` with 15 rows and `final_8_4.jsonl.failed` with 1 `content_rejected` row.
- [x] Adjudicate final gates after content-rejection continuation: `go test ./internal/facade ./internal/service -run '^(TestOpenAI_CompleteClassifiesContentRiskBadRequest|TestAdjudicate)' -count=1 -v`, `go test ./... -count=1`, `go test -race . ./internal/facade ./internal/service ./internal/lib/configs -count=1`, `git diff --check`, `go build -trimpath -o ./sendllm .`, and `./init.sh` all exited 0.
- [x] Adjudicate minimal-payload RED/GREEN: `go test ./internal/service -run '^TestAdjudicateSendsMinimalJudgmentPrompt$' -count=1 -v` first failed because full `annotation` was sent, then passed after retaining `messages` and labels while limiting `model_judgment` to `annotation.explanation`.
- [x] Adjudicate minimal-payload final gates: `go test . -run '^TestRunAdjudicateOutputsMASBFormat$' -count=1 -v`, `go test ./... -count=1`, `go test -race . ./internal/service ./internal/lib/configs -count=1`, `git diff --check`, `go build -trimpath -o ./sendllm .`, and `./init.sh` all exited 0.
- [x] SQLite-backed adjudicate focused gate: `GOCACHE=/private/tmp/sendllm-gocache go test ./internal/service -run '^(TestAdjudicate|TestRunner_UsesCustomClassificationRequest)' -count=1 -v` exited 0, covering SQLite resume, failed reset/retry, minimal adjudicate payload including both reasons, and configured real concurrency.
- [x] SQLite-backed adjudicate affected packages: `GOCACHE=/private/tmp/sendllm-gocache go test ./internal/dao ./internal/service ./internal/lib/configs -count=1` exited 0.
- [x] SQLite-backed adjudicate race gate: `GOCACHE=/private/tmp/sendllm-gocache go test -race ./internal/service ./internal/dao ./internal/lib/configs -count=1` exited 0.
- [x] SQLite-backed adjudicate build and hygiene: `GOCACHE=/private/tmp/sendllm-gocache go build -trimpath -o ./sendllm .` and `git diff --check` exited 0.
- [x] Adjudicate Ctrl+C export RED/GREEN: `go test ./internal/service -run '^TestAdjudicateExportsSucceededRowsAfterCancellation$' -count=1 -v` first failed because `final.jsonl` was not created after cancellation, then passed after adding a bounded terminal export context.
- [x] Adjudicate Ctrl+C focused gate: `go test ./internal/service -run '^TestAdjudicate' -count=1 -v`, `go test ./internal/dao ./internal/service ./internal/lib/configs -count=1`, `go test -race ./internal/service ./internal/dao ./internal/lib/configs -count=1`, and `go build -trimpath -o ./sendllm .` all exited 0.
- [x] Current full gate: `./init.sh` exited 0 with formatting, `go test ./...`, `go test -race ./...`, and `go vet ./...` passing.
- [x] `feat-007` baseline: `./init.sh` exited 0 before edits on 2026-08-11.
- [x] `feat-007` RED: `go test ./internal/dto ./internal/service -run 'Test(ParseSourceCompactJSONL|ParseSourceRejectsInvalidContent|ImportAcceptsCompactJSONL|ExportWritesOrderedMergedSuccessAndSafeFailures)' -count=1 -v` exited 1 because compact parsing still required legacy `trace_id`, Import rejected compact rows, and Export deleted compact `label` while writing generated fields at the top level.
- [x] `feat-007` GREEN focused tests: the same focused command exited 0 after compact parsing and nested annotation export were implemented.
- [x] `feat-007` wider tests: `go test ./... -count=1` exited 0.
- [x] `feat-007` full gate: `./init.sh` exited 0 with formatting, all tests, full race tests, and `go vet ./...` passing.
- [x] Round 2 Runner RED/GREEN: `go test ./internal/service -run '^TestRunner_RunDrainsPeerWhenProgressRacesWithCallerCancellation$' -count=1 -timeout=10s -v` first exited 1 because the peer received the caller cause during progress bookkeeping, then exited 0 after converting caller-canceled progress errors to graceful drain.
- [x] Round 2 review fix RED/GREEN: `go test ./internal/service -run '^TestRunner_RunBoundsDrainWhenProgressQueryBlocksAfterCallerCancellation$' -count=1 -timeout=5s -v` first exited 1 because Run did not honor `shutdown_timeout` while progress query was blocked, then exited 0 after keeping progress queries caller-cancelable.
- [x] Round 2 DAO RED/GREEN: `go test ./internal/dao -run '^TestImport_Add(LazilyMigratesLegacyHashForExactSource|RejectsDifferentSourceDespiteLegacyHashCollision)$' -count=1 -v` first exited 1 for exact large-integer and `1.0` legacy sources, then exited 0 after raw-source re-canonicalization and lazy migration; the true collision case remained a conflict.
- [x] Round 2 stability and related tests: the Runner race regression passed 50 consecutive runs; `go test ./internal/dao ./internal/service -run '^(TestImport_|TestRunner_)' -count=1` exited 0.
- [x] Round 2 race and full gate: `go test -race ./internal/dao ./internal/service -count=1` and `./init.sh` exited 0.
- [x] Round 2 fuzz: `go test -run=^$ -fuzz=FuzzParseJSONL -fuzztime=5s ./internal/service` exited 0 after 6,159 executions.
- [x] Round 2 hygiene: `git diff --check`, added-Go-line length, secret/payload logging, out-of-scope service, and sensitive-artifact scans passed with zero findings.
- [x] Round 2 real E2E: `zsh -lic 'exec /private/tmp/sendllm-live-final-moqKr4/sendllm -config /private/tmp/sendllm-live-final-moqKr4/task.yaml'` used the final latest binary, `json_object`, `max_tokens=2000`, concurrency 4, and a fresh temporary state/output; exit 0, stdout `added=50 skipped=0 succeeded=50 failed=0`.
- [x] Round 2 live acceptance: `SENDLLM_LIVE_CONFIG="$PWD/config/task.example.yaml" SENDLLM_LIVE_INPUT=/Users/lijiayang/venus/SendLLM/Test_Input.jsonl SENDLLM_LIVE_OUTPUT=/private/tmp/sendllm-live-final-moqKr4/output.jsonl SENDLLM_LIVE_STATE=/private/tmp/sendllm-live-final-moqKr4/state.db SENDLLM_LIVE_TASK_ID=sendllm-final-live-20260806-moqKr4 go test ./internal/service -run '^TestLiveAcceptance$' -count=1 -v` exited 0; stdout `live_acceptance=PASS input=50 output=50 trace_unique=50 trace_set_equal=50 schema_valid=50 business_valid=50 annotation_auto=50 failed_file=0 state_succeeded=50 state_failed=0`.
- [x] Final fix focused regressions across seven packages exited 0; the new tests were observed failing before their implementations where behavior changed.
- [x] Final fix race gate: `go test -race . ./internal/dao ./internal/facade ./internal/lib/limiter ./internal/service -count=1` exited 0.
- [x] Final fix fuzz gate: `go test -run=^$ -fuzz=FuzzParseJSONL -fuzztime=5s ./internal/service` exited 0 after 6,738 executions.
- [x] Final fix standard gate: `./init.sh` exited 0 with formatting, all tests, full race tests, and `go vet ./...` passing.
- [x] Final fix hygiene: `git diff --check`, added-Go-line length, Export ignore, secret/payload logging, out-of-scope service, and sensitive-artifact scans all passed with zero findings.
- [x] First final-fix wave used synthetic tests only; the later user-approved re-review exception re-ran the latest code through the 50-record real-model acceptance above.
- [x] Task 7 pinned non-secret config: `base_url=https://aigateway.venusgroup.com.cn/ai/deepseek/openai`, `model=deepseek-v4-pro`, `api_key_env=AI_GATEWAY_API_KEY`; `zsh -lic 'test -n "$AI_GATEWAY_API_KEY"'` exited 0 without printing its value.
- [x] Task 7 build: `go build -trimpath -o /private/tmp/sendllm-task7-final .` exited 0 with empty stdout; runtime production Go source and final Task 7 production Go source had no diff.
- [x] Task 7 dependency/format gate: `GOPROXY=https://proxy.golang.org,direct go mod tidy`, all-Go `gofmt`, and `git diff --check` exited 0 without dependency version drift; direct/indirect requirements and necessary checksums were normalized.
- [x] Task 7 parser fuzz: `go test -run=^$ -fuzz=FuzzParseJSONL -fuzztime=5s ./internal/service` exited 0 with 6,693 executions.
- [x] Task 7 audit RED/GREEN: `go test ./internal/facade -run '^TestOpenAI_CompletePreservesAuditableFailureResponse$' -v` first exited 1 because both synthetic HTTP 400 and malformed HTTP 200 cases returned empty `RawResponse`; the same command exited 0 after bounded response propagation. Facade tests, race tests, and `./init.sh` passed.
- [x] Task 7 real provider capability: `json_schema` returned HTTP 400 with a structured-output rejection; an otherwise equivalent `json_object` probe returned HTTP 200.
- [x] Task 7 real E2E: `zsh -lic 'exec /private/tmp/sendllm-task7-final -config /private/tmp/sendllm-task7-final.yaml'` used a fresh formal state, `json_object`, `max_tokens=2000`, and concurrency 4; exit 0, stdout `added=50 skipped=0 succeeded=50 failed=0`.
- [x] Task 7 output acceptance: `SENDLLM_LIVE_CONFIG="$PWD/config/task.example.yaml" SENDLLM_LIVE_INPUT=/Users/lijiayang/venus/SendLLM/Test_Input.jsonl SENDLLM_LIVE_OUTPUT=/Users/lijiayang/venus/SendLLM/Test_Output.jsonl SENDLLM_LIVE_STATE=/Users/lijiayang/venus/SendLLM/Test_State.db SENDLLM_LIVE_TASK_ID=sendllm-task7-real-final-20260806 go test ./internal/service -run '^TestLiveAcceptance$' -count=1 -v` exited 0; stdout `live_acceptance=PASS input=50 output=50 trace_unique=50 trace_set_equal=50 schema_valid=50 business_valid=50 annotation_auto=50 failed_file=0 state_succeeded=50 state_failed=0`.
- [x] Task 7 acceptance-test behavior: `go test ./internal/service -run '^TestLiveAcceptance$' -count=1 -v` exited 0 and skipped when artifact paths were unset. Temporarily changing `expectedLiveItems` from 50 to 51 made the exact live command exit 1 while every actual count remained 50; restoring 50 made it exit 0.
- [x] Review round 1 closeout: after commit `506da53`, `git status --short` exited 0 with empty stdout; `git log --oneline -8` exited 0 and was headed by `506da53`, `2ba55c7`, `656a0df`, `6a9f3aa`, `f504d0b`, `7ea4821`, `61401d5`, and `01ef1ec`.
- [x] Review round 2 scope: only the tracked acceptance test and its durable evidence are added; production behavior and the accepted local artifacts are unchanged.
- [x] Task 7 security gate: `rg -n 'Bearer |api[_-]?key|authorization|prompt.*slog|response.*slog' --glob '*.go' --glob '*.yaml' --glob '*.md' .` exited 0; sanitized review found 23 expected protocol/env/test/evidence lines, 0 credential values, and 0 payload-logging code paths. `rg -n 'ListenAndServe|redis|kafka|RabbitMQ|message[ _-]?queue|cron' --glob '*.go' .` exited 1 with no out-of-scope matches.
- [x] Task 6 focused verification: `go test ./internal/service -run TestExport`, `go test . -run TestRun`, and `go test -race . ./internal/service` passed.
- [x] Task 6 full gate: `./init.sh` passed formatting, all tests, full race tests, and `go vet ./...` on 2026-08-06.
- [x] Mode wiring mutation check: hard-coding `json_schema` made `TestRunWiresConfiguredStructuredOutputMode` fail; restoring `cfg.Model.StructuredOutput` passed.
- [x] Task 4 foundation verification: `go test ./internal/lib/tokenizer ./internal/lib/limiter ./internal/service -run 'Test(Estimate|Limiter|RetryPolicy|ClassifyFailure)'`, `go test -race ./internal/lib/limiter`, and `./init.sh` all passed.
- [x] Harness validation: `100/100`, all five subsystems scored `5/5`.
- [x] Design specification reviewed and approved by user.
- [x] Plan self-review: all design sections mapped, no placeholders found, cross-task interfaces aligned.
- [x] Task 1 verification: `./init.sh` passed formatting, unit tests, race tests, and vet.
- [x] Review fix: Go officially downloaded and verified `modernc.org/sqlite v1.34.5`; `go.sum` now includes module and go.mod checksums.
- [x] Task 2 verification: DAO/importer narrow tests, race tests, one-second JSONL fuzz smoke, and `./init.sh` all passed.
- [x] Task 5 verification: `go test ./internal/dao ./internal/service`, `go test -race ./internal/dao ./internal/service`, and `./init.sh` all passed.

## Notes For Next Session

All Harness features through `feat-011` are complete and no feature is active. The latest adjudicate binary is rebuilt at `./sendllm`; resume with `-mode adjudicate` and the configured SQLite state file. Ctrl+C now writes current successful adjudicate rows to the configured output file before returning exit 130. Re-run `./init.sh` after any change. For production batches, keep the API Key environment-only, use a new state file for semantic changes, and tune concurrency/RPM from config while watching 429s.


## feat-039 Checkpoint 5 Evidence (2026-09-22)

Feature: feat-039
Checkpoint: 5 (local verification and Qwen pilot; human gate pending)
Baseline: ./init.sh -> exit 0
RED: Task 1 `go test ./internal/service -run '^Test(Parse|Import)AdvertisementReview' -count=1 -v` -> exit 1; Task 2 `go test ./internal/service -run '^TestAdvertisementReview(Request|Results|Packing)' -count=1 -v` -> exit 1; Task 3 partition/export and runner RED commands -> exit 1; Task 4 `go test . -run '^TestRunAdvertisementReviewBatch' -count=1 -v` -> exit 1. Attempt-accounting regression RED failed because resume lost request/attempt aggregates and nested split counts were absent.
Changed: internal/service/advertisement_review.go; internal/service/advertisement_review_test.go; internal/service/advertisement_review_export.go; internal/service/advertisement_review_export_test.go; main.go; main_test.go; README.md; feature_list.json; progress.md; session-handoff.md; plus the supplied prompt/Schema/config files used unchanged.
GREEN: `go test ./internal/service -run '^TestAdvertisementReview|^Test(Parse|Import)AdvertisementReview' -count=1 -v` -> exit 0; `go test . -run '^TestRunAdvertisementReviewBatch' -count=1 -v` -> exit 0.
Repeat/race: `go test ./internal/service . -run '^Test.*AdvertisementReview' -count=10` -> exit 0; `go test -race ./internal/service . -run '^Test.*AdvertisementReview' -count=10` -> exit 0.
Full local gate: `go test ./... -count=1` -> exit 0; `go vet ./...` -> exit 0; `./init.sh` -> exit 0; `git diff --check` -> exit 0.
Scope/security: `sha256sum -c /private/tmp/feat-039-protected-hashes-20260921174701.sha256` -> exit 0 for source/data/final/DAO/DTO/facade/lib paths; production Authorization/API-key-value and payload-log scans -> no matches; `go.mod`/`go.sum` unchanged.
Mutation: original-ID mapping test -> exit 1 under mutation and passed after restore; request-blindness test -> exit 1 under mutation and passed after restore; missing-index/duplicate-zero test -> exit 1 under mutation and passed after restore; export deep-equality test -> exit 1 under mutation and passed after restore.
Full local fake run: 58,658 rows, CLI exit 0, clean=10006 issues=48652 failed=0; independent verifier PASS with attempts=58658 requests=3667 input_tokens=366700 output_tokens=58658; source SHA-256 unchanged.
Pilot: qwen3.5-flash, 512 rows, input SHA-256 039087872cff2ac1c246b5a52981c543c382a3ae2714d136b96bb2b4e7c42279; CLI exit 0; clean=282 issues=230 failed=0; 83 successful requests; input_tokens=326514 output_tokens=10673; independent partition verifier PASS. Artifacts: /private/tmp/feat039-pilot-qwen3.5-flash-20260921190946/run and worksheet /private/tmp/feat039-pilot-qwen3.5-flash-20260921190946/run/human-review-worksheet.csv (worksheet SHA-256 d29b85ccf5dc9d47a85ff22f428150cb2251d600a9cb38b13c1fe149242623dd).
Human gate: worksheet completed on 2026-09-22 at `/private/tmp/feat039-pilot-qwen3.5-flash-20260921190946/run/human-review-worksheet.csv`; SHA-256 `a2beaca2765be41df5a44fd0db9f735d9d479f2c490e0092c172461704bf876b`. Validation passed with 512 rows, `human_label` filled for 512/512, `human_legacy_overlap` filled for 512/512, non-human columns unchanged from backup SHA-256 `d29b85ccf5dc9d47a85ff22f428150cb2251d600a9cb38b13c1fe149242623dd`, human labels `unsafe=304 safe=208`, and human overlap `yes=162 no=350`. Human comparison shows `qwen3.5-flash` does not meet the fixed pilot gate: exact safe/unsafe accuracy including uncertain as wrong is `497/512=97.07%`, human-unsafe recall is `289/304=95.07%`, human-safe recall is `208/208=100%`, and 14 human-unsafe rows were classified `safe`. User still must explicitly decide whether to reject `qwen3.5-flash` or authorize a fresh Qwen rerun because of the post-fix invalid_result confirmation failure.
Full run: not authorized.
Residual risk: Model approval is still pending, and the separate post-fix Qwen confirmation run hit one exhausted invalid_result and was stopped at exit 130 with state preserved; no model switch was made.
Next action: Human decision: reject qwen3.5-flash or explicitly authorize a fresh Qwen rerun; do not start the 58,658-row run.


## feat-039 Human Gate and Fallback Evidence (2026-09-22)

Feature: feat-039
Checkpoint: pilot/model-selection gate
Baseline: human worksheet `/private/tmp/feat039-pilot-qwen3.5-flash-20260921190946/run/human-review-worksheet.csv` has 512/512 `human_label` and 512/512 `human_legacy_overlap` values; SHA-256 `a2beaca2765be41df5a44fd0db9f735d9d479f2c490e0092c172461704bf876b`.
Human truth: unsafe=304, safe=208; human-confirmed legacy overlap=yes for 162 rows.
Qwen: qwen3.5-flash -> FAIL. Exact accuracy 497/512=97.07% (<98%); human-unsafe recall 289/304=95.07% (<99%); 14 human-unsafe rows classified safe (must be zero); human-safe recall 208/208=100%; human-confirmed source-label errors routed 16/16=100%; human-confirmed overlaps routed 125/162=77.16% (<95%); 18 human-confirmed overlaps remained in clean; average occupancy 512/83=6.17 (<8), maximum occupancy 16, unresolved context overflow 0.
DeepSeek: deepseek-v4-flash, fresh task ID `advertisement-review-pilot-deepseek-v4-flash-v1`, fresh SQLite/output under `/private/tmp/feat039-pilot-qwen3.5-flash-20260921190946/deepseek-v4-flash-run` -> CLI exit 0; independent integrity verifier PASS with source=512 clean=415 issues=97 failed=0 attempts=608 requests=33 input_tokens=223595 output_tokens=56354.
DeepSeek quality: FAIL. Exact accuracy 497/512=97.07% (<98%); human-unsafe recall 291/304=95.72% (<99%); 12 human-unsafe rows classified safe (must be zero); human-safe recall 206/208=99.04%; source-label errors routed 14/16=87.5% (<100%); human-confirmed overlaps routed 18/162=11.11% (<95%); clean human-label-error leaks 2 and clean overlap leaks 100; average occupancy 512/33=15.52 (>=8), maximum occupancy 16, unresolved context overflow 0.
Human gate: qwen3.5-flash rejected; deepseek-v4-flash rejected. Both approved light models failed the fixed thresholds.
Full run: not authorized. Do not silently use a Pro model or lower thresholds.
Residual risk: Model quality/prompt behavior is insufficient for the fixed gate. The implementation is locally verified, but model selection is unresolved.
Next action: One human decision is required: stop feat-039 as model-selection blocked, explicitly approve a specified Pro-model pilot, or explicitly approve a prompt revision plus a new bounded pilot. Do not start the 58,658-row run.

## feat-039 Directed Cleaning Preparation (2026-09-22)

Feature: feat-039
Checkpoint: directed human/strong-model cleaning preparation
Baseline: `./init.sh` -> exit 0 before this workflow; source SHA-256 `3e9b43b4432fa5e60d7fd1ef7237fdf45266dc8dc4d9e6d881a5778df443fdf7`.
Changed: added `cmd/advertisement-clean-prep/{main,model,prepare,route,apply}.go` and `main_test.go`; updated `README.md`, `feature_list.json`, `progress.md`, and `session-handoff.md`. No source, provisional, JSONL data, model, or dependency was modified.
RED: `go test ./cmd/advertisement-clean-prep -run '^Test(Prepare|SelectP1|Route|Apply)' -count=1 -v` -> exit 1 for missing prepare/route/apply APIs. A subsequent independent verifier found and the focused prepare test then covered a P1 reviewed-ID exclusion defect before the final GREEN.
GREEN: `go test ./cmd/advertisement-clean-prep -count=1 -v` -> exit 0; `go test ./cmd/advertisement-clean-prep -count=10` -> exit 0; `go test -race ./cmd/advertisement-clean-prep -count=10` -> exit 0.
Full local gate: `go test ./... -count=1` -> exit 0; `go vet ./...` -> exit 0; `./init.sh` -> exit 0; `git diff --check` -> exit 0.
Prepare: `go run ./cmd/advertisement-clean-prep prepare -input data/ad/advertisement_dataset_final.json -worksheet /private/tmp/feat039-pilot-qwen3.5-flash-20260921190946/run/human-review-worksheet.csv -output-dir data/ad/directed-cleaning-prep -max-batch-tokens 80000` -> exit 0. Report: `reviewed_count=512`, `p0_count=225`, `p1_count=40`, `queue_count=265`, `batch_count=3`, `min_batch_tokens=57596`, `max_batch_tokens=79243`, `token_distribution={0-10000:0,10001-40000:0,40001-80000:3}`, `unique_trace_ids=265`, source SHA-256 unchanged, worksheet SHA-256 `a2beaca2765be41df5a44fd0db9f735d9d479f2c490e0092c172461704bf876b`.
Route empty-decisions drill: `route` with the generated empty `decisions.jsonl` -> exit 0, `first_pass_batches=3 decisions=0 second_pass=0 batches=0 unique_ids=0`; no `l=2` item can enter second-pass by test.
Apply empty-decisions drill: `apply` with the empty second-pass decisions -> exit 0, `input=58658 output=58658 decisions=0 modified=0 unchanged=58658`; independent verifier confirms exact trace_id order and JSON values equal source. `apply` refuses the protected provisional path.
Independent verification: PASS for P0 source-order/filter, P1=40 deterministic stratified sample, no reviewed IDs in either queue, 265 unique IDs, 3 blind batches with only `i,p`, mapping one-to-one, every batch <=80000 conservative input tokens, empty decisions, strict Schema, and unchanged source/provisional data.
Next action: provide the 3 blind batch files to the approved human/strong model; write the returned `{"r":[...]}` responses into `data/ad/directed-cleaning-prep/decisions.jsonl`, then run `route` and the final `apply` to a new output path. Do not call a real model from this repository.

## feat-040 Full Cascade Cleaning Design (2026-09-22)

- `feat-039` is now historical `blocked`: both Flash candidates failed the frozen pilot and the 265-row directed review is calibration evidence, not a full-dataset quality claim.
- Added the approved full-cleaning design at `docs/superpowers/specs/2026-09-22-advertisement-full-cleaning-design.md`.
- Added the task-by-task implementation plan at `docs/superpowers/plans/2026-09-22-advertisement-full-cleaning.md`.
- Added the direct handoff prompt at `docs/advertisement-full-cleaning-coding-model-prompt.md`.
- Added and froze the compact model prompt, strict result Schema, Qwen 3.5 Plus pilot YAML, and DeepSeek V4 Pro pilot YAML. The coding-model handoff explicitly requires consuming rather than rewriting them.
- Frozen model cascade: all 58,658 rows through `qwen3.5-plus`; only changes, overlap, uncertain, and failures through independent `deepseek-v4-pro`.
- Baseline before documentation changes: `./init.sh` exit 0. No real model calls, source writes, cleaned output, provisional replacement, or merge occurred.
- Next action: give the coding-model prompt to the implementation agent. It must complete local fake-provider gates and stop before the frozen Qwen pilot.

## feat-040 Startup Checkpoint (2026-09-22)

- Confirmed repository root `/Users/lijiayang/venus/SendLLM`; `feat-040` is the only `in-progress` feature and `feat-039` is historical `blocked`.
- Read `AGENTS.md`, the full-cleaning design/plan/coding prompt, the still-applicable review harness, base design, `开发指南.md`, feature state, progress, and session handoff.
- Baseline `./init.sh` -> exit 0.
- Saved protected pre-run hashes outside the repository at `/private/tmp/feat-040-protected-hashes-20260922151606.sha256` for the advertisement source, task-013 v8 baseline, existing provisional, full-review prompt, result Schema, and both frozen pilot configs.
- Task 1 checkpoint: implement calibration truth folding before any model runner work. Next exact RED command after tests are written: `go test ./cmd/advertisement-full-clean -run '^TestBuildCalibration' -count=1 -v`.

## feat-040 Task 1-6 Local Evidence (2026-09-22)

Feature: feat-040
Checkpoint: local-verified / frozen pilot prepared / awaiting human approval
Baseline: `./init.sh` -> exit 0
Red: Task 1 `go test ./cmd/advertisement-full-clean -run '^TestBuildCalibration' -count=1 -v` -> exit 1 for missing calibration APIs. Task 2 `go test ./internal/service -run '^TestAdvertisementFullReview' -count=1 -v` -> exit 1 for missing full-review service contract. Task 3 `go test ./cmd/advertisement-full-clean -run '^Test(Route|Adjudicate)' -count=1 -v` -> exit 1 for missing routing/adjudication. Task 4 `go test ./cmd/advertisement-full-clean -run '^Test(Apply|Replace)' -count=1 -v` -> exit 1 for missing apply/replace. Task 5 RED reconstruction: temporarily moving `evaluate.go` made `go test ./cmd/advertisement-full-clean -run '^TestEvaluatePilotThresholds$' -count=1 -v` exit 1 with undefined `evaluatePilot`/`evaluationConfig`; restoration made the same command exit 0. A provider-context rejection regression first failed because the runner treated it as task-fatal, then passed after bounded batch splitting was implemented.
Changed: added `cmd/advertisement-full-clean/{main,model,calibration,route,adjudicate,apply,replace,evaluate}.go` and matching tests; added `internal/service/advertisement_full_review.go` and `advertisement_full_review_test.go`; updated `README.md`, `feature_list.json`, `progress.md`, and `session-handoff.md`. Did not modify the frozen prompt, Schema, either pilot YAML, source advertisement data, authoritative v8 baseline, or provisional integration.
Green: `go test ./cmd/advertisement-full-clean ./internal/service -run '^(TestBuildCalibration|TestFrozenFullReview|TestAdvertisementFullReview|Test(Route|Adjudicate|Apply|Replace|Evaluate|Run))' -count=1 -v` -> exit 0.
Repeat/race: same focused selection with `-count=10` -> exit 0; `go test -race ... -count=10` -> exit 0.
Full local gate: `go test ./... -count=1` -> exit 0; `go vet ./...` -> exit 0; `./init.sh` -> exit 0; `git diff --check` -> exit 0.
Calibration: `go run ./cmd/advertisement-full-clean calibration -source data/ad/advertisement_dataset_final.json -worksheet /private/tmp/feat039-pilot-qwen3.5-flash-20260921190946/run/human-review-worksheet.csv -layer1-mapping data/ad/directed-cleaning-prep/review-batch-map.jsonl -layer1-decisions data/ad/directed-cleaning-prep/decisions.jsonl -layer2-mapping data/ad/directed-cleaning-prep/second-pass-strong-v1/review-batch-map.jsonl -layer2-decisions data/ad/directed-cleaning-prep/second-pass-strong-v1/decisions.jsonl -output-dir data/ad/full-cleaning` -> exit 0. Counts: total=777, worksheet=512, directed=265, prompt-development=389, holdout=388, unsafe=433, safe=344, overlap_yes=162. `calibration_sha256=ab55c5d0744d909f8e48a81254a14eeda2c7c3ffb96a84fa0775a1d15936ce20`; `pilot_sha256=9868351d8c838322603956abe6200548099e375ccf600668b6a34f43e244a79c`.
Independent verification: PASS for exact 777 unique IDs, 512+265 truth-source partition, no conflict, directed rows without fabricated overlap truth, deterministic 389/388 split, holdout pilot input with only `trace_id`/`prompt`, report/manifest free of prompt fields, and stable PILOT hash.
Mutation: original ID mapping -> focused exit 1 under mutation, exit 0 after restore; request blindness -> exit 1/0; missing-index rejection -> exit 1/0; agreement-only apply -> exit 1/0; overlap quarantine -> exit 1/0; non-ad replacement identity -> exit 1/0.
Protected: `sha256sum -c /private/tmp/feat-040-protected-hashes-20260922151606.sha256` -> all OK. Source SHA-256 remains `3e9b43b4432fa5e60d7fd1ef7237fdf45266dc8dc4d9e6d881a5778df443fdf7`; provisional remains `2b18e5b179a42a42bd3544b4d9b447a0701b000557a8fe6a62f6920fcb372ab4`.
Pilot preparation: `data/ad/full-cleaning/pilot/layer1.input.jsonl` has 388 holdout rows and input SHA-256 `9868351d8c838322603956abe6200548099e375ccf600668b6a34f43e244a79c`; `data/ad/full-cleaning/pilot/pilot-plan.json` records the frozen command, model, state/output/report paths, and protected hashes.
Full run: not authorized and not started. No real model call, full layer run, cleaned-v1 output, or provisional replacement was executed.
Residual risk: Model quality is entirely unproven until the approved real Qwen pilot runs and the holdout thresholds are evaluated.
Next action: user approval to run exactly `go run ./cmd/advertisement-full-clean run -config config/task.advertisement-full-review.qwen3.5-plus.yaml -report data/ad/full-cleaning/pilot/layer1-report.json`. Do not start the 58,658-row run.

## feat-040 Risk-Priority Real Pilot (2026-09-22)

- User selected the risk-priority experiment: 3,000 high-risk rows plus 1,000 stratified control rows.
- Selection is deterministic and excluded all 777 previously reviewed IDs. Outputs: `data/ad/full-cleaning/risk-priority/selection.manifest.jsonl`, `pilot.input.jsonl`, and `selection-report.json`; selection SHA-256 `ee6e274dfa069f0cf2a2d1b5e3e4e55b5c331789e985ffe97bec1b5a9251ee43`, pilot SHA-256 `12c73808c758eef1d1b661c322122e6b4454b53938a40e34bce3815be3f6cdb1`.
- Real `qwen3.5-plus` pilot completed with independent task `advertisement-full-review-risk-priority-qwen35-plus-v1`: 4,000/4,000 succeeded, 0 failed, 32 uncertain, 370 safe, 3,598 unsafe, 125 requests, average occupancy 32, input_tokens 656,789, output_tokens 59,825, context overflows 0.
- Cohort label-change suggestions: high-risk `51/3000=1.70%`; control `3/1000=0.30%`; enrichment `5.67x`. Local route produced 1,950 Pro candidates: label_change=54, legacy_overlap=1,864, uncertain=32.
- The risk-priority DeepSeek V4 Pro second layer was started as an independent persistent `screen` job after the Qwen and route steps. Its task ID/state/output/report are isolated under `data/ad/full-cleaning/risk-priority/routing/` and `layer2-report.json`.
- The full 58,658-row run remains unauthorized and not started.

## feat-040 Risk-Priority Pilot Closeout (2026-09-23)

- The detached Pro job finished: task `advertisement-full-review-risk-priority-deepseek-v4-pro-v1`, 1,950/1,950 succeeded, 0 failed, 2 uncertain, 97 safe, 1,851 unsafe, 75 requests, average occupancy 26, input_tokens 246,859, output_tokens 182,034, context overflows 0.
- Local adjudication: approved=12, legacy_overlap=1,864, quarantine=74, unchanged=2,050. Quarantine codes: disagreement=42, uncertain=32.
- Approved changes: 11 `unsafe -> safe` and 1 `safe -> unsafe`. Applying only these approved rows produced `data/ad/full-cleaning/risk-priority/cleaned.pilot.json` with 12 modified and 3,988 JSON-value-unchanged rows; independent ID/order and equality verification PASS.
- No legacy overlap was auto-applied. No full 58,658-row run, full cleaned-v1 output, or provisional replacement was executed.
- Next action: user review of the approved/overlap/quarantine manifests, then decide whether to expand sampling, apply the bounded pilot result, or stop.

## feat-040 Human Gate: Approved Changes Rejected (2026-09-23)

- User reviewed the 12 approved model changes and explicitly confirmed all 12 are rejected; original labels are retained.
- Accepted changes: 0. Rejected changes: 12. The 12 changes were 11 `unsafe -> safe` and 1 `safe -> unsafe`; 10 of the 12 came from `ChineseSafe` `unsafe -> safe`.
- `cleaned.pilot.json` is not an accepted result and must not be used.
- Recorded `data/ad/full-cleaning/risk-priority/human-review-approved.json` with status `rejected_confirmed`, outcome `bounded_qwen_pro_label_changes_failed_human_gate`, and `cleaned_pilot_valid_for_use=false`.
- The bounded Qwen/Pro label-change path failed the human gate. Do not expand it or start the full run without a new model/prompt decision.
- Final status: user explicitly stopped this model/prompt path. `feat-040=blocked`; no feature is active; no new model or prompt revision is authorized.
