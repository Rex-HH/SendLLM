# Advertisement Dataset Lightweight Review Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a resumable compact batch review that reads the existing advertisement JSON array, independently rejudges safe/unsafe, flags overlap with the original 38 risks and obvious scenario problems, and exports unchanged clean/problem partitions keyed by the original `trace_id`.

**Architecture:** Add an advertisement-specific service beside `label_review.go`, reusing the existing OpenAI-compatible `Completer`, limiter, retry policy, generic SQLite item store, and safe progress lifecycle. The model sees only a batch-local integer, prompt, and original scenario; deterministic local code maps the integer back to the persisted source `trace_id`, validates compact results, derives issue codes, and atomically publishes unchanged source rows plus a payload-free manifest/report.

**Tech Stack:** Go 1.24, standard library JSON/IO/concurrency, existing SQLite DAO, existing OpenAI-compatible facade, existing limiter/token estimator, YAML task configuration, local fake-provider tests.

## Global Constraints

- Use the original advertisement source `trace_id` unchanged in SQLite and every persistent output.
- The batch-local integer is request-scoped only and must never replace or be persisted as the stable ID.
- Do not modify `data/ad/advertisement_dataset_final.json` or merge any output into `data/final`.
- Do not send the original label, explanation, source, response, quality score, or advertisement big category to the model.
- Do not log raw prompt, response, model output, credentials, or dataset payloads.
- Treat uncertain, malformed, missing, exhausted, and provider-failed rows as issues; never default them into clean.
- Preserve every clean/problem source object unchanged apart from representing the source JSON array as JSONL.
- Keep `main.go` limited to configuration and dependency wiring.
- All new Go function comments are concise Chinese comments; exported comments start with the identifier name.
- Use only existing dependencies and the standard library.

---

### Task 1: Advertisement input and compact result contracts

**Files:**
- Create: `internal/service/advertisement_review.go`
- Create: `internal/service/advertisement_review_test.go`

**Interfaces:**
- Consumes: `dao.Store.BeginImport`, `dao.Import.Add`, existing `Completer`, `limiter.Limiter`, and `RetryPolicy`.
- Produces: `AdvertisementReviewConfig`, `AdvertisementReviewStats`, `AdvertisementReview`, `ImportAdvertisementReview`, `parseAdvertisementReviewInput`, and compact request/result types used by later tasks.

- [ ] **Step 1: Write failing JSON-array import tests**

Add tests that create a temporary store, import a synthetic JSON array with two original `trace_id` values, and assert both IDs and input order are preserved. Add table cases for duplicate ID, missing ID, non-prompt scene, empty prompt, invalid label, malformed array, non-object element, and trailing JSON.

```go
func TestImportAdvertisementReviewPreservesOriginalTraceID(t *testing.T) {
    input := `[{"trace_id":"ad-original-001","scene":"prompt","label":"safe","prompt":"普通介绍","extended_info":{"attack_scenario":""}},` +
        `{"trace_id":"ad-original-002","scene":"prompt","label":"unsafe","prompt":"添加联系方式","extended_info":{"attack_scenario":"wechat_contact"}}]`
    // Import and assert item log IDs are exactly ad-original-001 and ad-original-002 in that order.
}
```

- [ ] **Step 2: Run the focused test and verify RED**

Run: `go test ./internal/service -run '^Test(Parse|Import)AdvertisementReview' -count=1 -v`

Expected: build failure because advertisement review import and parser APIs do not exist.

- [ ] **Step 3: Implement the minimal input parser and JSON-array importer**

Define the source shape without discarding unknown fields:

```go
type advertisementReviewInput struct {
    TraceID      string                    `json:"trace_id"`
    Scene        string                    `json:"scene"`
    Label        string                    `json:"label"`
    Prompt       string                    `json:"prompt"`
    ExtendedInfo advertisementExtendedInfo `json:"extended_info"`
    Raw          json.RawMessage           `json:"-"`
}

type advertisementExtendedInfo struct {
    AttackScenario string `json:"attack_scenario"`
}
```

Use `json.Decoder` to require exactly one top-level array, decode each element into `json.RawMessage`, validate the required review fields, and add `dao.Item{TraceID: source.TraceID, RawJSON: source.Raw, Prompt: source.Prompt}`. Do not synthesize or normalize `TraceID`.

- [ ] **Step 4: Run the focused tests and verify GREEN**

Run: `go test ./internal/service -run '^Test(Parse|Import)AdvertisementReview' -count=1 -v`

Expected: all parser/import tests pass.

- [ ] **Step 5: Commit the self-contained import contract**

```bash
git add internal/service/advertisement_review.go internal/service/advertisement_review_test.go
git commit -m "feat: import advertisement review data"
```

### Task 2: Compact request, strict result validation, and token packing

**Files:**
- Modify: `internal/service/advertisement_review.go`
- Modify: `internal/service/advertisement_review_test.go`

**Interfaces:**
- Consumes: parsed `advertisementReviewInput`, `dto.CompletionRequest`, `tokenizer.Estimate`, and the configured original 38-risk map.
- Produces: `advertisementReviewBatchRequest`, `advertisementReviewBatchResults`, `packAdvertisementReviewBatches`, and validated `advertisementReviewDecision` values.

- [ ] **Step 1: Write failing request-minimization and validation tests**

Assert that the model payload contains only `i`, `p`, and `s`; it must not contain `trace_id`, original label, response, explanation, source, quality score, or original big category. Add literal validation cases for duplicate/missing/out-of-range `i`, invalid `l`, invalid `s`, unknown `x`, `safe+x`, and valid uncertain/safe/unsafe results.

```go
type advertisementReviewDecision struct {
    Index           int    `json:"i"`
    Label           int    `json:"l"`
    LegacyRisk      string `json:"x"`
    ScenarioSuspect int    `json:"s"`
}
```

- [ ] **Step 2: Run the compact-contract tests and verify RED**

Run: `go test ./internal/service -run '^TestAdvertisementReview(Request|Results|Packing)' -count=1 -v`

Expected: build failure for the missing request, result, and packing functions.

- [ ] **Step 3: Implement the compact protocol and strict local validator**

Build this user payload:

```json
{"items":[{"i":0,"p":"待复核提示词","s":"wechat_contact"}]}
```

Parse exactly one response object shaped as `{"r":[...]}`. Require one unique result for every input index, `l` in `0..2`, `s` in `0..1`, `x` empty or present in the original 38-risk map, and reject `l=1` with non-empty `x`.

- [ ] **Step 4: Implement greedy consecutive token packing**

Given one claimed slice, append consecutive rows while `tokenizer.Estimate` for the candidate request is within `BatchMaxInputTokens`. Keep `BatchSize` as the maximum claim count. A single oversized row remains a single group so normal failure handling can quarantine it.

- [ ] **Step 5: Run the focused tests and verify GREEN**

Run: `go test ./internal/service -run '^TestAdvertisementReview(Request|Results|Packing)' -count=1 -v`

Expected: all compact-contract and packing cases pass.

- [ ] **Step 6: Commit the model boundary**

```bash
git add internal/service/advertisement_review.go internal/service/advertisement_review_test.go
git commit -m "feat: add compact advertisement review protocol"
```

### Task 3: Resumable runner and unchanged partition export

**Files:**
- Modify: `internal/service/advertisement_review.go`
- Create: `internal/service/advertisement_review_export.go`
- Modify: `internal/service/advertisement_review_test.go`
- Create: `internal/service/advertisement_review_export_test.go`

**Interfaces:**
- Consumes: generic DAO claim/retry/success/failure APIs and validated compact decisions.
- Produces: `AdvertisementReview(ctx, cfg) (AdvertisementReviewStats, error)` and `ExportAdvertisementReview(ctx, store, taskID, cleanPath)`.

- [ ] **Step 1: Write failing issue-derivation and export tests**

Use synthetic rows to cover: exact label agreement goes clean; safe→unsafe and unsafe→safe create `label_error`; non-empty legacy risk creates `legacy_overlap`; scenario flag creates `scenario_suspect`; uncertain creates `uncertain`; one row can have multiple issues; final provider/format failures create an issue row. Assert source output objects are deeply equal to the original values and manifest rows contain only `trace_id`, issue codes, and optional candidate legacy risk.

- [ ] **Step 2: Run export tests and verify RED**

Run: `go test ./internal/service -run '^TestAdvertisementReview(Partition|Export)' -count=1 -v`

Expected: build failure because partition/export APIs do not exist.

- [ ] **Step 3: Implement deterministic issue derivation**

Use issue codes in stable order:

```go
var advertisementIssueOrder = []string{
    "uncertain",
    "label_error",
    "legacy_overlap",
    "scenario_suspect",
    "provider_failure",
    "invalid_result",
}
```

Never change the source object. Persist the validated compact decision as SQLite annotation only.

- [ ] **Step 4: Implement the owned worker lifecycle and retry handling**

Mirror the existing batch runner ownership rules: reset interrupted processing, claim bounded slices, greedily pack them, run worker goroutines owned by one `WaitGroup`, stop new claims on cancellation, drain for `Shutdown`, record every attempt, retry retryable failures, split invalid whole-batch responses, and mark exhausted rows failed.

- [ ] **Step 5: Implement four staged outputs**

Treat `Task.Output` as `clean.original.jsonl` and publish sibling files:

- `issues.original.jsonl`
- `issues.manifest.jsonl`
- `review-report.json`

Iterate `Store.ForEachItemLog` in input order. Encode `RawJSON` directly into the appropriate JSONL file; derive manifest/report from state and compact decisions. Stage, sync, close, and rename all outputs without embedding prompts or model responses in manifest/report.

- [ ] **Step 6: Run integration, resume, cancellation, and export tests**

Run: `go test ./internal/service -run '^TestAdvertisementReview' -count=1 -v`

Expected: all service tests pass, including original-ID preservation, missing-result retry, no repeat call for succeeded rows, failure-to-issues behavior, output coverage, and cancellation drain.

- [ ] **Step 7: Run race coverage for the new runner**

Run: `go test -race ./internal/service -run '^TestAdvertisementReview' -count=10`

Expected: exit 0 with no race reports.

- [ ] **Step 8: Commit runner and export behavior**

```bash
git add internal/service/advertisement_review.go internal/service/advertisement_review_export.go \
  internal/service/advertisement_review_test.go internal/service/advertisement_review_export_test.go
git commit -m "feat: partition advertisement review results"
```

### Task 4: Prompt, Schema, example config, and CLI wiring

**Files:**
- Create: `prompts/advertisement-review-batch-system.txt`
- Create: `config/advertisement-review-result-schema.json`
- Create: `config/task.advertisement-review.yaml`
- Create: `config/task.advertisement-review.deepseek-v4-flash.yaml`
- Modify: `main.go`
- Modify: `main_test.go`
- Modify: `README.md`

**Interfaces:**
- Consumes: existing `configs.Load`, API-key loading, semantic fingerprint, OpenAI facade, limiter, and progress logger.
- Produces: `-mode advertisement-review-batch` with existing exit semantics and a ready-to-run Qwen Flash configuration.

- [ ] **Step 1: Write a failing CLI fake-provider test**

Create a temporary two-row JSON array and fake OpenAI server. Run `run` with `-mode advertisement-review-batch`; assert exit 0, the fake request contains the compact payload only, all four outputs exist, and both output rows retain their original `trace_id` values.

- [ ] **Step 2: Run the CLI test and verify RED**

Run: `go test . -run '^TestRunAdvertisementReviewBatch' -count=1 -v`

Expected: exit failure because the mode is not wired.

- [ ] **Step 3: Add the compact prompt and Schema**

The prompt must define independent label judgment, `advertisement` semantics, original 38-risk overlap, weak scenario checking, integer enums, and exact one-result-per-index behavior. The Schema must allow only `{"r":[{"i":integer,"l":0|1|2,"x":string,"s":0|1}]}`.

- [ ] **Step 4: Add the executable task configuration**

Use:

```yaml
model:
  base_url: https://aigateway.venusgroup.com.cn/ai/aliyun/openai
  api_key_env: AI_GATEWAY_API_KEY
  name: qwen3.5-flash
  structured_output: json_object
```

Point input at `../data/ad/advertisement_dataset_final.json`, clean output at `../data/ad/review/clean.original.jsonl`, state at `../data/ad/review/advertisement-review.db`, risk types at the existing 38-class file, and result Schema/prompt at the new compact files. Start with conservative concurrency and a large maximum batch count governed by token packing.

Also add an isolated `deepseek-v4-flash` fallback configuration with a different task ID, SQLite state, and output directory. It is used only against the identical pilot after Qwen fails the fixed human gate; a run must never change model inside an existing task state.

- [ ] **Step 5: Wire the new mode in `main.go`**

Construct `service.AdvertisementReviewConfig` from the already loaded config, completer, limiter, risk map, retry policy, and progress callback. Print only aggregate clean/issues/failed counts. Return 2 when terminal provider/format failures exist and 0 when all rows received valid review decisions, regardless of whether semantic issues were found.

- [ ] **Step 6: Run CLI and affected-package tests**

Run: `go test . ./internal/service ./internal/lib/configs -run 'AdvertisementReview|TestLoad' -count=1 -v`

Expected: exit 0.

- [ ] **Step 7: Update operator documentation and commit**

Document the command, four outputs, original-ID guarantee, no-merge boundary, API-key safety, resume behavior, and the meaning of exit 0 versus issue counts.

```bash
git add main.go main_test.go README.md prompts/advertisement-review-batch-system.txt \
  config/advertisement-review-result-schema.json config/task.advertisement-review.yaml \
  config/task.advertisement-review.deepseek-v4-flash.yaml
git commit -m "feat: wire advertisement review batch mode"
```

### Task 5: Local verification, stratified pilot, and durable handoff

**Files:**
- Modify: `feature_list.json`
- Modify: `progress.md`
- Modify: `session-handoff.md`
- Runtime only, ignored: `data/ad/review/**`

**Interfaces:**
- Consumes: completed CLI and the real local advertisement file.
- Produces: verified local implementation evidence and, only if credentials are available, an initial stratified real-model pilot outside the final dataset.

- [ ] **Step 1: Run structural checks on the real input without printing payloads**

Report only counts for total rows, unique original `trace_id`, safe/unsafe, scenarios, empty prompts, and required fields. Do not print prompts, responses, or full records.

- [ ] **Step 2: Run focused and full local gates**

Run:

```bash
gofmt -w internal/service/advertisement_review*.go main.go main_test.go
go test ./internal/service . -run '^Test.*AdvertisementReview' -count=1 -v
go test -race ./internal/service . -run '^Test.*AdvertisementReview' -count=10
go test ./... -count=1
go vet ./...
./init.sh
git diff --check
```

Expected: every command exits 0.

- [ ] **Step 3: Build a deterministic stratified pilot input outside tracked data**

Select a small review set with original `trace_id` values, covering safe rows, every one of the 16 scenarios, short/medium/long prompts, and lower existing quality scores. Write the pilot under `/private/tmp` or ignored `data/ad/review`; do not commit it.

- [ ] **Step 4: Run the Qwen Flash pilot only when the API key is present**

Check key presence without printing it, use a fresh task ID/state, run the new mode, and report only aggregate counts and file paths. If the provider or format fails, preserve the SQLite audit state and stop rather than silently switching models.

- [ ] **Step 5: Inspect all pilot issues and sample pilot clean rows**

The user reviews every `label_error` and `legacy_overlap`, plus a stratified sample of clean rows. If Qwen Flash quality is insufficient, rerun the identical pilot with `deepseek-v4-flash` and compare aggregate/human-reviewed outcomes before any full run.

- [ ] **Step 6: Update durable evidence without claiming final merge readiness**

Record exact commands, exit codes, model used, aggregate pilot counts, unresolved risks, and next action. Keep `feat-039` in progress until implementation gates and the selected-model pilot are both complete; do not mark the dataset merged or production-ready.

- [ ] **Step 7: Commit coherent verified changes only**

```bash
git add feature_list.json progress.md session-handoff.md
git commit -m "docs: record advertisement review verification"
```
