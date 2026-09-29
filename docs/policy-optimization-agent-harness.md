# Policy Optimization Coding-Agent Harness

This workflow governs implementation of the approved Policy Optimization Agent. It supplements `AGENTS.md`; the stricter rule wins.

## 1. Mandatory startup

Run in this exact order for every coding session:

1. Run `pwd`; stop unless it is the SendLLM repository root.
2. Read `AGENTS.md` completely.
3. Read both Safety Review and Policy Optimization design documents completely.
4. Read `docs/superpowers/specs/2026-09-07-policy-optimization-implementation-contract.md` completely.
5. Read the active Task in `docs/superpowers/plans/2026-09-07-policy-optimization-agent-implementation.md`.
6. Read `feature_list.json`, the current/top sections of `progress.md`, and `session-handoff.md`.
7. Run `git status --short` and `git log --oneline -5`; treat all pre-existing changes as user-owned.
8. Confirm `feat-025=done`. If not, stop; no optimizer production code may be created.
9. Run `./init.sh` and record its real exit before edits.
10. Run `scripts/verify-policy-optimizer-scope.sh` when present. A protected-file mismatch stops work.
11. Select exactly the first dependency-complete optimizer feature, or resume the sole `in-progress` feature.
12. Mark that feature `in-progress`, record baseline/scope/next RED command immediately, then write the failing test first.

Repository state, SQLite, immutable artifacts, and recorded hashes are authoritative. Chat history and model session memory are not.

## 2. One-feature transaction

```text
pending
  -> in-progress
  -> red-proven
  -> green-focused
  -> verified
  -> done
```

Only `pending`, `in-progress`, `blocked`, and `done` go into `feature_list.json`. Record intermediate dated states in `progress.md`. Do not start the next feature in the same implementation turn.

A feature is blocked only when an exact command/failure has persisted after the required evidence-based attempts or needs a required human/external action. Record the attempts, safe error category, and exact unblock action. Do not lower a gate, invent approval, or skip a model to avoid a blocker.

## 3. Scope and ownership

- New Go basenames start with `policy_optimizer_`.
- Only `feat-036` may make the minimal existing `main.go` dispatch edit.
- Do not append optimizer behavior to legacy, Safety Review, reconcile, adjudicate, label-review, Runner, DAO, DTO, config, or facade files.
- Stable generic interfaces may be consumed unchanged. If reuse requires modifying one, stop and obtain user approval.
- Do not implement a future feature's file behavior early, even when the file already exists.
- Do not rename, omit, or extend frozen CLI flags, SQL columns/indexes, states, transitions, Artifact paths, Schema fields, Skill executors, compiler sections, failure actions, or Gate thresholds.
- Do not add dependencies, services, queues, plugins, native provider Skills, hot reload, or multi-process coordination.
- One local process owns one iteration DB. Safety Review DBs are read-only only through declared regression/export contracts, never directly mutated.

The scope script captures protected-file hashes at optimizer kickoff and rejects unauthorized old-Go changes, unapproved Go basenames, sensitive generated directories, and a `main.go` change before `feat-036`.

## 4. Authority boundary

Coding agents must preserve these hard boundaries:

- Models may interpret unknown sources, mine patterns, adjudicate candidates, diagnose, author proposals, criticize, and propose a Change Set.
- Deterministic code alone applies approved mappings, normalizes, selects/stratifies, recomputes IDs/counts, validates authority, applies patches, compiles prompts, evaluates regression gates, verifies approvals, and releases.
- A model-proposed mapping remains unapproved.
- Candidate Gold is never acceptance truth. Gold approval/promotion is an explicit human action.
- A Critic `accept` is not release approval. A Critic `block` prevents compilation/release until superseded by a new coherent proposal/critique cycle.
- A Human Directive cannot be silently overridden. Equal high-authority conflict blocks.
- `compile --preview` cannot set regression, approval, or release state.
- Every release requires fresh regression and a matching explicit approval artifact.
- Released directories and their source/compiled artifacts are immutable.

## 5. RED/GREEN evidence

A valid RED:

- runs the exact named new test with `-count=1`;
- fails because approved behavior is missing or wrong;
- occurs before production implementation;
- is not a syntax, bad-fixture, unrelated baseline, artificial panic, or network failure.

A valid GREEN runs the same command, retains every RED assertion, exits 0, then passes affected-package and race gates. Concurrency/state tests use deterministic barriers and the plan's repeat count. Critical authority, compiler, gate, and release invariants require temporary mutation proof followed by restoration and a fresh pass.

## 6. Required verification layers

Run in order for every feature:

1. Exact focused GREEN: `-count=1 -v`.
2. All affected-package tests: `-count=1`.
3. Task-specific deterministic repeats, failure injection, fuzz, or mutation proof.
4. Affected-package race test with the plan's count.
5. `./init.sh`.
6. `scripts/verify-policy-optimizer-scope.sh` when present.
7. `git diff --check`.
8. Secret, Authorization, payload-logging, raw-output, and sensitive-artifact scans.
9. `git diff --stat` and manual review of every changed file.

`feat-037` also runs the optimizer verifier and fresh Safety Review bundle validation. `feat-038` also runs both verifier scripts, real multi-model preflight/iteration/regression, independent metric verification, human approval, and fresh release consumption.

## 7. Anti-shortcut tests

Tests and implementation must prove all of the following:

- Unknown/custom input cannot normalize without a source-hash-bound human-approved mapping.
- A model cannot invent trusted IDs, counts, policy text, authority, Gold status, or release state.
- Every record is assigned once to primary mining; 70/20/10 and 30-100 boundaries are independently recomputed.
- Global Pattern counts and case references are checked against canonical records.
- Direct compile makes zero mining/diagnosis model calls and still cannot release before regression.
- Author/resolver and Critic use different model families.
- Prompt compilation is byte-stable and covers every enabled policy/condition/exclusion/decision ID.
- Base and released directories remain byte-identical after candidate operations and failed release injection.
- Core Safe and Hard Negative false Unsafe stay zero; hidden Unsafe resolved Safe stays zero.
- Approval is invalid after any candidate, regression, or Critic byte changes.
- A fake success or model-reported metric cannot satisfy final acceptance.

Never alter fixtures, expected values, thresholds, authority, or hidden labels merely to make a failing gate pass.

## 8. Test and data hygiene

- Use table-driven tests with `give`/`want`; parallelize only isolated state.
- Use `t.TempDir()` for package, iteration, DB, candidate, and release fixtures.
- Inject clock, sleeper, jitter, filesystem failure points, and deterministic seed. Do not coordinate with `time.Sleep`.
- Fakes reject unexpected calls, role/family, context fields, artifacts, and stage order.
- Use fixed synthetic canaries for payload/secret leakage checks.
- Raw Prompt/Response, evidence, source rows, model output, and API keys may exist only in restricted test/state/artifact paths; never stdout/stderr, status, public summaries, or Git fixtures beyond approved minimal policy examples.
- Real Audit/Gold datasets, generated candidates/releases awaiting review, iteration DBs, and run outputs stay local and Git-ignored.

## 9. Human checkpoints

The coding agent must stop and report an exact operator action for:

- approving an unknown/custom source mapping;
- approving or promoting Gold;
- resolving conflicting equal high-authority directives;
- issuing a required Policy Directive;
- approving a candidate release;
- providing missing real credentials or approved Audit/Gold assets.

The agent may implement and test the command that records the action, but it may not fabricate the human identity/decision or call an approval command as part of automatic analyze/compile/regression flow. In final acceptance, the user must explicitly authorize the real approval.

## 10. State and shutdown

Update `feature_list.json`, `progress.md`, and `session-handoff.md` as soon as RED, GREEN, verification, blocker, or human-waiting state changes. Preserve old evidence and add a dated optimizer section.

Before ending:

1. Stop or drain every long command.
2. Run the narrow test for the latest edit.
3. Run the full required gate before marking done.
4. Persist exact commands/exits, artifact hashes, decisions, risks, and one next action.
5. Review status/diff and scan for scope/sensitive drift.
6. Leave one truthful state: `done`, `blocked`, or one `in-progress` feature with an exact next command.
7. Do not claim optimizer completion before `feat-038` and real release consumption pass.

## 11. Evidence format

```text
Feature: feat-NNN
Baseline: <command> -> exit N
RED: <exact command> -> exit N; <missing behavior>
Changed: <exact files>
GREEN: <same command> -> exit 0
Repeat/failure injection: <command/count> -> exit 0
Race: <command/count> -> exit 0
Full gate: ./init.sh -> exit 0
Scope/security: <commands> -> exits
Artifact/state checks: <hashes/invariants, no payload>
Real acceptance: <aggregate evidence or not required>
Human action: <artifact/hash/actor ID or not required>
Residual risk: <specific risk or none>
Next action: <one exact command or feature>
```

“Tests pass,” cached output, screenshots without commands, default skips, self-reported model counts, or automated approval are not completion evidence.
