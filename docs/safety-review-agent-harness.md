# Safety Review Coding-Agent Harness

This workflow governs implementation of the approved Safety Review pipeline. It supplements `AGENTS.md`; stricter rule wins.

## 1. Mandatory startup

Run these steps in order on every coding session:

1. `pwd`; stop unless it is the SendLLM repository root.
2. Read `AGENTS.md` completely.
3. Read `docs/superpowers/specs/2026-09-04-safety-review-pipeline-design.md` completely.
4. Read `docs/superpowers/plans/2026-09-04-safety-review-pipeline-implementation.md` for the active feature.
5. Read `feature_list.json`, the top/current sections of `progress.md`, and `session-handoff.md`.
6. Run `git status --short` and `git log --oneline -5`. Treat pre-existing changes as user-owned.
7. Run `./init.sh` before edits and record its exit and any failure.
8. Run `scripts/verify-safety-review-scope.sh` when it exists. A protected-file mismatch stops work.
9. Select the first dependency-complete Safety Review feature. Exactly one feature may be `in-progress`.
10. Write the feature's failing tests before production code. Run them and record the expected failure.

Do not continue from chat memory alone. SQLite and repository state files are authoritative.

## 2. One-feature transaction

Each feature follows this state machine:

```text
pending
  -> in-progress (baseline recorded)
  -> red-proven (new test failed for the intended missing behavior)
  -> green-focused (focused test passed with -count=1)
  -> verified (race + full gate + scope/security passed)
  -> done (exact evidence persisted)
```

`feature_list.json` continues to use `pending`, `in-progress`, `blocked`, and `done`. Store `red-proven`, `green-focused`, and `verified` as dated evidence entries in `progress.md`; do not invent extra JSON status values.

A feature may become `blocked` only with the exact failing command, output category, attempts already made, and required external action. A difficult implementation is not a blocker.

## 3. Scope enforcement

- New Go basenames start with `safety_review_`.
- The only pre-approved existing production Go change is the small `main.go` subcommand dispatch in `feat-022`.
- Do not edit `runner.go`, `label_review.go`, `adjudicate.go`, `reconcile.go`, old DTO/config/store behavior, or legacy Schemas.
- Reuse existing code only through stable generic interfaces. If reuse requires changing an old interface, stop and request approval.
- Do not combine adjacent pending features “while already in the file.”
- Do not refactor unrelated old code or reformat untouched files.
- No new dependency is allowed. A proposed dependency requires a design amendment and user approval.

Before closing a feature, run the scope script. It must compare protected-file hashes captured at Safety Review kickoff and report every exception. A changed user-owned file is not automatically reverted; stop and resolve ownership.

## 4. RED/GREEN evidence rules

A valid RED run:

- executes the exact new test by name with `-count=1`;
- fails because approved behavior is absent or wrong;
- is recorded before production implementation;
- does not fail from syntax errors, broken fixtures, missing unrelated setup, or deliberately inserted panic.

A valid GREEN run:

- runs the same command and exits 0;
- keeps all assertions from RED;
- is followed by affected-package tests and race tests;
- does not rely on cache, skipped cases, network availability, execution order, or wall-clock sleeps.

For concurrency/state features, repeat deterministic focused tests at the count specified by the plan. A single passing run is not evidence.

Critical validators require mutation proof: temporarily invert the named invariant, prove the targeted test fails, then restore and rerun. Never commit a mutation.

## 5. Required verification layers

Every feature runs, in order:

1. Exact focused GREEN test with `-count=1 -v`.
2. All tests in affected packages with `-count=1`.
3. Required repeated scheduling/state tests.
4. `go test -race` for affected packages with required count.
5. `./init.sh`.
6. `scripts/verify-safety-review-scope.sh` when available.
7. `git diff --check`.
8. Security scan for key literals, Authorization logging, and Prompt/Response/raw-output logging.
9. `git diff --stat` and manual review of every changed file.

`feat-024` and `feat-025` additionally run `scripts/verify-safety-review.sh` and real-model acceptance. Fake-provider tests never replace live acceptance.

## 6. Test independence

- Use table-driven tests with `give` and `want` fields.
- Use fixed synthetic strings for ordinary unit-test payloads. The approved P04-B policy bundle may commit only the minimal human-reviewed development/regression examples identified by the design; never commit bulk source data or Hidden Gold.
- Inject clocks, sleepers, and jitter. Do not use `time.Sleep` to coordinate tests.
- Use channels only for deterministic barriers and give every wait a test deadline.
- Use a fresh `t.TempDir()` SQLite DB unless a test explicitly verifies reopen/resume.
- Count model calls by role, profile, stage key, and trace ID.
- Fakes must reject unexpected calls and unexpected request fields.
- A test that merely confirms “some error” is insufficient when error category/state is contractual.
- Acceptance checks use a separate reader/validator path from production helpers.

## 7. No-shortcut rules

The agent must not:

- mark a required live test passed when it skipped;
- treat a missing Hidden Gold file as a reason to block only real `eval` acceptance, not ordinary `validate` or model-only `run`;
- substitute one model for a configured role without recording fallback and family degradation;
- lower acceptance thresholds, increase quarantine allowance, or alter hidden labels;
- read original labels before decisions are terminal;
- hard-code evaluation IDs or expected outputs;
- convert provider rejection into Safe, drop the row, or retry with obfuscated input;
- delete failed attempts or reset attempt counters on resume;
- use output JSONL as progress state;
- log data to make debugging easier;
- broaden a rule card from category names or model intuition;
- mark policy coverage complete when required cards are absent.
- finalize Safe from A/B alone without a complete zero-candidate Router result;
- let Router emit a policy verdict instead of observable features and recall candidates;
- let Arbiter create, establish, or select a category that no Expert established;
- emit `risk_level` in the P04-B V1 generated contract;
- reject multiple model profiles solely because they share one `api_key_env`.
- use `single_profile` for formal eval or claim its role-prompt isolation is model independence; single-profile results remain unvalidated.
- read role prompts from an independent mutable prompt directory; prompts must come from the verified immutable release bundle.
- omit or populate `quality-events.jsonl` with Prompt, Response, evidence, rationale, or raw model output.

The live acceptance intentionally includes anti-cheating mutations. Both must fail before restored settings can pass.

## 8. State updates

Update state immediately after each meaningful gate, not only at session end.

`feature_list.json` evidence contains concise final commands and exits. `progress.md` contains the active feature, RED/GREEN history, files changed, decisions, blockers, and next command. `session-handoff.md` contains enough exact context for a fresh agent to resume without chat history.

Never replace old evidence wholesale. Add a dated Safety Review section and keep current status at the top.

## 9. Session shutdown

1. Stop or drain every long-running process.
2. Run the narrow test for the last edit.
3. Run the full required gate if the feature is being marked done.
4. Update all three state files with exact evidence.
5. Review `git status --short` and `git diff` for scope drift.
6. Run sensitive-artifact and payload-logging scans.
7. Leave exactly one of these states: feature `done`, feature `blocked` with evidence, or feature `in-progress` with one exact next command.
8. Do not claim Safety Review completion until `feat-025` and both live rotations pass. Policy Optimization is a later independent feature chain.

## 10. Completion evidence format

```text
Feature: feat-NNN
Baseline: <command> -> exit N
RED: <command> -> exit N; failed because <exact missing behavior>
Changed: <exact files>
GREEN: <same command> -> exit 0
Repeat: <command/count> -> exit 0
Race: <command/count> -> exit 0
Full gate: ./init.sh -> exit 0
Scope/security: <commands> -> exits
Live acceptance: <aggregate counts only, or not required for this feature>
Residual risk: <specific risk or none>
Next action: <one exact command or feature>
```

Evidence such as “all tests passed,” screenshots without commands, cached output, or unrecorded manual inspection is not sufficient.
