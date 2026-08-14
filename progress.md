# Session Progress Log

## Current State

**Last Updated:** 2026-08-13
**Active Feature:** none

## Status

### What's Done

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
