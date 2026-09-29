# Advertisement Review Coding-Agent Harness

This workflow governs implementation and execution of `feat-039`, the lightweight review of
`data/ad/advertisement_dataset_final.json`. It supplements `AGENTS.md`; the stricter rule wins.

The feature ends with two unchanged source partitions and an audit manifest. It does not normalize the four current
advertisement big classes, produce a corrected dataset, or merge anything into `data/final`.

## 1. Authority and mandatory startup

Read these files completely, in this order, before editing:

1. `AGENTS.md`
2. `docs/superpowers/specs/2026-08-05-sendllm-design.md`
3. `docs/superpowers/specs/2026-09-21-advertisement-dataset-lightweight-review-design.md`
4. `docs/advertisement-review-agent-harness.md`
5. `docs/superpowers/plans/2026-09-21-advertisement-dataset-lightweight-review.md`
6. `开发指南.md`
7. `feature_list.json`, `progress.md`, and `session-handoff.md`

Then run and record:

```bash
pwd
git status --short
git log --oneline -5
./init.sh
```

Stop unless `pwd` is `/Users/lijiayang/venus/SendLLM`. Treat every pre-existing modification and untracked file as
user-owned. Do not revert, reformat, stage, or commit it. `feat-039` must be the only `in-progress` feature; blocked
`feat-024`, pending `feat-025`, and `feat-026` through `feat-038` remain unchanged.

If the baseline fails, record the exact command, exit code, and whether the failure predates the task. Do not edit
unrelated code to make the baseline green.

## 2. One-feature, five-checkpoint transaction

Implement only `feat-039`, in the five plan tasks and in their stated order. Finish all RED/GREEN evidence for one
checkpoint before starting the next:

1. JSON-array import and original-ID preservation.
2. Compact model protocol, strict result validation, and token-aware packing.
3. Resumable execution, issue derivation, and unchanged partition export.
4. Prompt, Schema, example configuration, and minimal CLI wiring.
5. Local verification, real pilot, human gate, and only then an approved full run.

Use only `pending`, `in-progress`, `blocked`, and `done` in `feature_list.json`. Record `red-proven`, `green-focused`,
`local-verified`, `pilot-generated`, and `human-approved` as evidence in `progress.md`, not as new status values.

Implementation may be locally complete while `feat-039` remains `in-progress`. It becomes `done` only after the human
model-selection gate and approved full-run acceptance in sections 10 and 11 pass. Waiting for user review is a normal
human gate, not a reason to invent approval or silently continue.

## 3. Scope boundary

The only pre-approved implementation paths are:

- new `internal/service/advertisement_review.go`
- new `internal/service/advertisement_review_test.go`
- new `internal/service/advertisement_review_export.go`
- new `internal/service/advertisement_review_export_test.go`
- new `prompts/advertisement-review-batch-system.txt`
- new `config/advertisement-review-result-schema.json`
- new `config/task.advertisement-review.yaml`
- new `config/task.advertisement-review.deepseek-v4-flash.yaml`
- the smallest dispatch/wiring additions in `main.go` and `main_test.go`
- task-specific documentation in `README.md`, `feature_list.json`, `progress.md`, and `session-handoff.md`

All other paths are protected unless the approved plan explicitly names them. In particular, do not modify:

- the source file or any file under `data/final`
- existing `internal/dao`, `internal/dto`, `internal/facade`, `internal/lib`, or legacy service behavior
- `runner.go`, `label_review.go`, `adjudicate.go`, `reconcile.go`, or any `safety_review_*` implementation
- Safety Review or Policy Optimization configs, prompts, policy bundles, Schemas, features, or acceptance evidence
- `go.mod` or `go.sum`

Reuse existing stable interfaces. If the implementation cannot proceed without changing a protected interface or
adding a dependency, stop and request a design amendment. Do not broaden scope for convenience.

Before editing, save the current status and hashes of protected critical paths outside the repository. Before each
checkpoint closes, compare them and inspect every task-owned diff. A mismatch is a stop condition, not permission to
revert a user-owned file.

## 4. Frozen data and model contracts

### Stable identity

- The source `trace_id` is the SQLite item ID and the ID in every persistent artifact.
- Never generate, hash, prefix, suffix, normalize, or replace it.
- Batch-local `i` starts at zero and exists only inside one request/response snapshot.
- Local code, not the model, maps `i` back to the original `trace_id`.
- Exports preserve relative source order within each partition.

### Model visibility

The user payload contains exactly this semantic information:

```json
{"items":[{"i":0,"p":"prompt text","s":"wechat_contact"}]}
```

It must not contain original `trace_id`, `label`, `risk_type`, advertisement big class, `response`, `explanation`,
`source`, annotation metadata, or `quality_score`. All source rows have `scene=prompt`; non-empty stored responses are
therefore not model input.

### Compact result

The only accepted semantic result is:

```json
{"r":[{"i":0,"l":1,"x":"","s":0}]}
```

- `l`: `0=uncertain`, `1=safe`, `2=unsafe`.
- `x`: empty or one exact key from `config/risk-types.yaml`, which currently has 38 entries.
- `s`: `0=no obvious scenario problem`, `1=obvious scenario suspicion`.

Reject the whole affected result set when result count differs, an index is missing/duplicated/out of range, an enum is
invalid, `x` is outside the closed set, or `l=1` has non-empty `x`. Never repair or infer missing model fields.

### Deterministic local routing

Issue codes have this stable order:

```text
uncertain, label_error, legacy_overlap, scenario_suspect, provider_failure, invalid_result
```

A row is clean only when its valid model label is definite and agrees with the original label, `x` is empty, and
`s=0`. Any uncertainty, disagreement, overlap, scenario suspicion, exhausted request, context failure, or invalid
result routes the row to issues. One row may have multiple issue codes. The model never rewrites source records.

## 5. RED/GREEN evidence

For every plan checkpoint:

1. Write all required behavioral and failure tests first.
2. Run the exact focused command with `-count=1 -v`.
3. A valid RED fails because the approved behavior is absent or wrong, not because of syntax, fixture, unrelated
   baseline, network, panic, or an intentionally broken test.
4. Record command, exit code, and exact missing behavior immediately.
5. Add the smallest production change for that checkpoint.
6. Run the same command unchanged and obtain GREEN.
7. Run affected-package tests and the required race/repeat gate before advancing.

Do not delete, weaken, skip, or replace a RED assertion. Tests use synthetic payloads, `httptest`, `t.TempDir()`, fixed
clocks/jitter, and channel barriers. They do not use the real provider, real dataset text, or `time.Sleep` for
synchronization.

Critical invariants require negative proof: temporarily break original-ID mapping, request blindness, missing-index
rejection, and output deep equality one at a time; the named test must fail. Restore each mutation and rerun GREEN.
Never commit a mutation.

## 6. Required automated test matrix

The implementation is incomplete unless tests cover all of these cases:

| Area | Required evidence |
| --- | --- |
| Import | top-level array only; unknown fields preserved; source order and exact `trace_id`; duplicate/missing ID; non-object; malformed/trailing JSON; non-prompt scene; empty prompt; invalid label |
| Request blindness | serialized payload has only top-level `items` and item keys `i,p,s`; forbidden fields and forbidden source values are absent |
| Result validator | exact cardinality; unique/in-range indices; closed `l/s/x`; reject `safe+x`; accept uncertain and valid safe/unsafe |
| Packing | consecutive stable order; count cap; token cap; every non-final batch is greedy-maximal; single oversized row follows failure routing; context rejection splits recursively |
| Routing | agreement clean; both label-error directions; overlap; scenario suspicion; uncertain; multiple codes; provider and invalid-result exhaustion never clean |
| Resume | succeeded rows are never called again; interrupted processing resets safely; attempt history remains; task/config fingerprint mismatch is rejected |
| Concurrency | cancellation stops claims, owned goroutines drain, shared cooldown respected, no race under ten repeated runs |
| Export | four atomic outputs; input-order partitions; original object deep equality; complete/disjoint ID sets; manifest one-to-one with issue pool; no payload leakage |
| CLI fake E2E | two-row array through fake OpenAI endpoint; compact request observed; exact original IDs exported; documented exit codes; no external network |

Batch-utilization tests must prove greedy packing rather than only assert that a batch has multiple rows. Given a fixed
estimator, appending the first row from the next batch must exceed the configured input-token cap or the count cap.

## 7. Output contract and independent integrity check

The four outputs are:

- `clean.original.jsonl`
- `issues.original.jsonl`
- `issues.manifest.jsonl`
- `review-report.json`

The first two contain source objects unchanged, aside from array-to-JSONL framing. The manifest contains only
`trace_id`, non-empty `issues`, and optional `candidate_legacy_risk`. The report contains aggregate counts,
distributions, batch/token utilization, and optional ID lists; it contains no prompt, response, explanation, raw model
output, Authorization data, or API key.

Acceptance must use an independent verifier, not the production export helper. It decodes the source and outputs and
proves:

1. every file parses and the source has exactly 58,658 rows;
2. the source has 58,658 non-empty, unique IDs;
3. `clean_count + issues_count = 58,658`;
4. each partition has unique IDs and their intersection is empty;
5. their union is exactly the source ID set;
6. every exported object is JSON-value-deep-equal to its source object for that ID;
7. relative source order is preserved in each partition;
8. manifest IDs are unique and exactly equal the issue-pool IDs;
9. every issue list is non-empty, closed, deduplicated, and in canonical order;
10. a non-empty candidate legacy risk is one of the 38 keys in `config/risk-types.yaml`;
11. aggregate report totals reconcile with partitions, manifest, attempts, and request statistics;
12. the SHA-256 of `data/ad/advertisement_dataset_final.json` is unchanged from pre-run evidence.

The verifier reports counts and failing IDs only. It never prints full records or dataset text.

## 8. Failure and exit semantics

- Exit `0`: every input row has a valid terminal compact decision. Semantic issues may exist and are expected.
- Exit `2`: at least one row exhausted provider/format/context handling. Those rows must still be in the issue pool.
- Other nonzero: task/config/import/export/integrity failure.

Provider `2xx` with an empty or malformed body is not success. Retryable requests follow the configured finite retry
policy. Invalid whole-batch output is split to isolate records. A single oversized or repeatedly invalid item becomes an
issue; it is never dropped or made safe. Resume uses SQLite, not existing JSONL output, as truth.

Atomic publication means a failed export cannot leave a mixed generation of the four final files. Stage, sync, close,
and rename as one owned publication step or remove incomplete staged files without touching the last complete set.

## 9. Local implementation acceptance

After all four implementation checkpoints, run fresh commands in this order:

```bash
gofmt -w internal/service/advertisement_review*.go main.go main_test.go
go test ./internal/service -run '^TestAdvertisementReview|^Test(Parse|Import)AdvertisementReview' -count=1 -v
go test . -run '^TestRunAdvertisementReviewBatch' -count=1 -v
go test ./internal/service . -run '^Test.*AdvertisementReview' -count=10
go test -race ./internal/service . -run '^Test.*AdvertisementReview' -count=10
go test ./... -count=1
go vet ./...
./init.sh
git diff --check
```

Also inspect task-owned diffs and scan them for credential literals, Authorization values, payload logging, raw model
output logging, and forbidden input fields in requests. All commands must exit zero. Do not treat test-cache output,
skipped tests, or a fake provider as live acceptance.

At this point update state as `local-verified`, leave `feat-039=in-progress`, and proceed only to the bounded pilot.

## 10. Deterministic real-model pilot and human gate

Generate the pilot outside tracked files, under `/private/tmp` or ignored `data/ad/review`. Keep source IDs unchanged.
Use a deterministic SHA-256 tie-breaker and build at most 512 rows:

- 192 safe rows: 48 from each prompt character-length band `<=40`, `41..120`, `121..400`, and `>400`; fill a band
  shortfall from the remaining safe rows in hash order.
- Up to 320 unsafe rows: for each of 16 scenarios choose up to 20, prioritizing five lowest `quality_score`, five
  shortest, five longest, and five remaining hash-ranked rows; deduplicate and fill to 20 from the scenario's remaining
  hash order. Fill a global shortfall from remaining unsafe rows in hash order.

Record only aggregate pilot distribution and a SHA-256 of the pilot file. Do not print its prompts or responses.

Run `qwen3.5-flash` first against the Aliyun OpenAI-compatible endpoint. Check only that
`AI_GATEWAY_API_KEY` is non-empty; never print it. Use a fresh task ID, state DB, and output directory. Live acceptance
requires parseable provider responses, all pilot rows terminal, independent integrity checks passing, and zero terminal
provider/invalid-result rows. Preserve failed state and stop on failure; do not silently switch models mid-run.

Then stop for human review. Produce a local, ignored review worksheet joining original row, model decision, and issue
codes by original `trace_id`. The user independently adjudicates every pilot row for `safe/unsafe` and legacy overlap;
scenario flags are inspected but do not alter data or decide model acceptance.

The light model passes only if the completed human worksheet proves all of the following:

- protocol-valid decisions: 100%; terminal provider/format failures: 0;
- safe/unsafe accuracy: at least 98%;
- human-unsafe recall: at least 99%, with zero human-unsafe rows classified `safe`;
- human-safe recall: at least 97%;
- human-confirmed source-label errors routed to issues: 100%;
- human-confirmed legacy overlaps routed to issues: at least 95%;
- no human-confirmed label error or legacy overlap appears in the clean partition;
- average items per successful request is at least 8, at least one request carries 16 items, and unresolved context
  overflow is zero.

An `uncertain` result is not counted as a correct safe/unsafe label, but its conservative issue routing is still valid.
If `qwen3.5-flash` fails any threshold, run the identical pilot once with `deepseek-v4-flash`, using separate state and
outputs, and apply the same human truth and thresholds. Choose the lighter model only when it passes all thresholds.
If neither passes, stop and ask the user; do not escalate to a Pro model or lower a threshold without approval.

The code model may generate pilot outputs but cannot invent the human worksheet decisions. Until the user explicitly
approves one candidate model, it must not start the full run.

## 11. Approved full-run acceptance

Only an explicit user approval naming the selected model authorizes the 58,658-row run. Use a fresh full-run state DB
and output directory. Never merge output into the existing final dataset.

Full-run acceptance requires:

- CLI exit `0` and terminal provider/format failures `0`;
- the independent integrity check in section 7 passes all twelve assertions;
- every SQLite source item is terminal exactly once, while attempt history is retained;
- report issue-code and label-direction counts reconcile with the manifest;
- average successful request occupancy is at least 8 and unresolved context overflow is zero;
- logs and public artifacts contain no prompt, response, explanation, raw model output, key, or Authorization value;
- original source SHA-256 is unchanged and no path under `data/final` changed;
- a resume drill on a separate bounded run proves successful rows are not requested twice;
- all local gates in section 9 pass again after the run.

The user then reviews all `label_error`, all `legacy_overlap` in manageable batches, all failures if any, and a
stratified clean sample across original label, all scenarios, and length bands. `scenario_suspect` remains advisory and
must not be auto-corrected.

Passing this section means the review and separation are complete. It does not authorize corrections, normalization to
`risk_type=advertisement`, or merging. Those are separate user-approved tasks.

## 12. State, evidence, and shutdown

Update state after every meaningful gate. Do not overwrite historical evidence. Use this exact evidence shape:

```text
Feature: feat-039
Checkpoint: <1..5 or pilot/full>
Baseline: <command> -> exit N
RED: <exact command> -> exit N; <intended missing behavior>
Changed: <exact task-owned files>
GREEN: <same command> -> exit 0
Repeat/race: <commands and counts> -> exits
Full local gate: ./init.sh -> exit N
Scope/security: <commands> -> exits
Pilot: <model, rows, input hash, aggregate counts, artifact paths, or not started>
Human gate: <approved model and worksheet evidence, or awaiting user>
Full run: <model, aggregate counts, integrity result, or not authorized>
Residual risk: <specific risk or none>
Next action: <one exact command or one human decision>
```

Before ending a session:

1. stop or drain all commands and owned goroutines;
2. run the narrow test for the last edit;
3. run all required gates if claiming that checkpoint complete;
4. update `feature_list.json`, `progress.md`, and `session-handoff.md` immediately;
5. review every task-owned diff and the protected-path comparison;
6. scan for secrets, raw payload logging, generated DB/output files, and accidental data changes;
7. do not stage or commit user-owned dirty files;
8. leave exactly one durable next action.

Evidence such as “tests passed,” a screenshot, a model's self-evaluation, skipped live work, or aggregate counts without
the independent integrity check is insufficient.
