# Session Progress Log

## Current State

**Last Updated:** 2026-08-06
**Active Feature:** none

## Status

### What's Done

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

1. Preserve `Test_State.db` and `Test_Output.jsonl` as local acceptance artifacts; neither is committed.
2. Before the first 30,000-record batch, confirm the provider's real rate limits and create a new task ID/state file for any semantic configuration change.
3. Use `json_object` for this gateway and budget enough completion tokens for the model's reasoning before its final JSON.

## Blockers / Risks

- [ ] The account concurrency limit is reported as approximately 500, but real acceptance observed transient 429 responses at concurrency 16 and 4; RPM and TPM remain undocumented.
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

## Files Modified This Session

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

- [x] Final fix focused regressions across seven packages exited 0; the new tests were observed failing before their implementations where behavior changed.
- [x] Final fix race gate: `go test -race . ./internal/dao ./internal/facade ./internal/lib/limiter ./internal/service -count=1` exited 0.
- [x] Final fix fuzz gate: `go test -run=^$ -fuzz=FuzzParseJSONL -fuzztime=5s ./internal/service` exited 0 after 6,738 executions.
- [x] Final fix standard gate: `./init.sh` exited 0 with formatting, all tests, full race tests, and `go vet ./...` passing.
- [x] Final fix hygiene: `git diff --check`, added-Go-line length, Export ignore, secret/payload logging, out-of-scope service, and sensitive-artifact scans all passed with zero findings.
- [x] No real model request was made during the final fix wave; the historical 50-record acceptance evidence below was retained and not rerun.
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

All Harness features are complete and no feature is active. The final fix wave did not call the real model; the recorded 50-item live acceptance remains historical evidence. Re-run `./init.sh` after any change. For a production batch, keep the API Key environment-only, use a new state file for semantic changes, and start at concurrency 4 while continuing to calibrate against observed 429s.
