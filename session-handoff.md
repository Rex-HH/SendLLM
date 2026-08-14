# Session Handoff

## Active Work

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

- Resume adjudicate mode with `zsh -lic './sendllm -mode adjudicate -config ./config/task.003.dark.adjudicate.yaml'`. Delete only the output files when you want a clean re-export; delete the SQLite state file only when you intentionally want to recompute every row from scratch. Pressing Ctrl+C will now export currently succeeded rows before exiting.
