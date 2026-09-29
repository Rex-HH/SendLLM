# Advertisement Full Cleaning Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a resumable two-layer review and deterministic apply pipeline that reviews all 58,658 advertisement rows and produces a separately versioned cleaned dataset.

**Architecture:** Reuse the existing advertisement batch runner and SQLite durability, but add a `advertisement-full-clean` orchestration boundary. Qwen 3.5 Plus reviews every row; deterministic routing sends only label changes, overlap candidates, uncertain results, and failures to an independent DeepSeek V4 Pro task. Deterministic code owns comparison, quarantine, application, and final replacement.

**Tech Stack:** Go, SQLite, JSON/JSONL, YAML, OpenAI-compatible HTTP, standard library tests.

## Global Constraints

- Follow `AGENTS.md` and `docs/superpowers/specs/2026-09-22-advertisement-full-cleaning-design.md`.
- Preserve original `trace_id`, input order, unknown fields, and every `attack_scenario` value.
- Never overwrite source, authoritative baseline, or existing provisional files.
- Never log dataset payloads, model output, Authorization, or API key values.
- Use `apply_patch` for edits, TDD for every task, Chinese comments for new Go code, and no new dependency.
- Fake providers only during implementation. Stop after a bounded pilot artifact is prepared; real model execution requires the user gate.
- The prompt, result Schema, and both pilot YAML files already exist and are frozen; Task 2 validates and consumes them instead of recreating them.

---

### Task 1: Freeze calibration truth

**Files:**
- Create: `cmd/advertisement-full-clean/main.go`
- Create: `cmd/advertisement-full-clean/calibration.go`
- Create: `cmd/advertisement-full-clean/calibration_test.go`

**Interfaces:**
- Produces `calibration.jsonl` records containing `trace_id`, `label`, optional `legacy_overlap`, `truth_source`, and `split`.
- Consumes the 512-row worksheet and both directed-review decision layers plus mappings.

- [ ] Write tests proving 512+265 unique inputs, conflict rejection, missing mapping rejection, correct two-pass folding, absent overlap truth for the 265 set, deterministic split, and payload-free manifest.
- [ ] Run `go test ./cmd/advertisement-full-clean -run '^TestBuildCalibration' -count=1 -v`; expect failure because the command and folding logic do not exist.
- [ ] Implement the minimal parser/folder with strict closed enums and atomic output.
- [ ] Run the same command, then `go test -race ./cmd/advertisement-full-clean -run '^TestBuildCalibration' -count=10`; both must pass.

### Task 2: Add compact full-review contract and configs

**Files:**
- Read/validate: `prompts/advertisement-full-review-system.txt`
- Read/validate: `config/advertisement-full-review-result-schema.json`
- Read/validate: `config/task.advertisement-full-review.qwen3.5-plus.yaml`
- Read/validate: `config/task.advertisement-full-review.deepseek-v4-pro.yaml`
- Create: `internal/service/advertisement_full_review.go`
- Create: `internal/service/advertisement_full_review_test.go`

**Interfaces:**
- Input payload is exactly `{"items":[{"i":0,"p":"..."}]}`.
- Output item is exactly `{i int, l int, x string}` with `l` in `0..2` and `x` empty or in the existing 38-risk closed set.

- [ ] Write fake-provider tests for input blindness, batch packing, exact index coverage, duplicate/missing index rejection, enum rejection, `l=1/x!=empty` rejection, and no raw payload logging.
- [ ] Run `go test ./internal/service -run '^TestAdvertisementFullReview' -count=1 -v`; expect behavior-missing RED.
- [ ] Implement the smallest runner adapter by reusing existing batch durability; do not duplicate generic retry or limiter code.
- [ ] Run focused tests, repeat ten times, and run the focused race test ten times.

### Task 3: Implement deterministic Pro routing and adjudication

**Files:**
- Create: `cmd/advertisement-full-clean/route.go`
- Create: `cmd/advertisement-full-clean/route_test.go`
- Create: `cmd/advertisement-full-clean/adjudicate.go`
- Create: `cmd/advertisement-full-clean/adjudicate_test.go`

**Interfaces:**
- `route` consumes source records and Layer-1 terminal decisions and creates blind Layer-2 batches plus local index mappings.
- `adjudicate` produces mutually exclusive approved changes, legacy overlap, and quarantine manifests.

- [ ] Write tests for all four route reasons, no route for unchanged/no-overlap results, blind Pro input, independent task identity, agreement-only label changes, disagreement quarantine, and overlap never being auto-applied.
- [ ] Run `go test ./cmd/advertisement-full-clean -run '^Test(Route|Adjudicate)' -count=1 -v`; expect RED.
- [ ] Implement linear deterministic routing and adjudication with problem-code enums.
- [ ] Run focused GREEN, `-count=10`, and `-race -count=10`.

### Task 4: Implement safe application and provisional replacement

**Files:**
- Create: `cmd/advertisement-full-clean/apply.go`
- Create: `cmd/advertisement-full-clean/apply_test.go`
- Create: `cmd/advertisement-full-clean/replace.go`
- Create: `cmd/advertisement-full-clean/replace_test.go`

**Interfaces:**
- `apply` writes a new 58,658-row advertisement JSON array.
- `replace` writes a new full JSONL by replacing only matching advertisement `trace_id` values.

- [ ] Write tests proving safe-field clearing, unsafe `attack_domain=advertisement`, empty `attack_method`, unchanged `attack_scenario`, unchanged unknown fields, no ID/order changes, protected-path refusal, and atomic output.
- [ ] Write replacement tests proving all expected ad IDs occur exactly once, non-ad rows are JSON-value identical, total count is unchanged, and missing/extra/duplicate IDs fail before writing.
- [ ] Run `go test ./cmd/advertisement-full-clean -run '^Test(Apply|Replace)' -count=1 -v`; expect RED.
- [ ] Implement minimal map-by-ID application and replacement, then run focused GREEN, repeat, and race gates.

### Task 5: Add pilot evaluation and CLI wiring

**Files:**
- Create: `cmd/advertisement-full-clean/evaluate.go`
- Create: `cmd/advertisement-full-clean/evaluate_test.go`
- Modify: `cmd/advertisement-full-clean/main.go`
- Modify: `README.md`

**Interfaces:**
- `evaluate` emits aggregate metrics only and exits nonzero if any frozen threshold fails.
- CLI subcommands are `calibration`, `route`, `adjudicate`, `apply`, `replace`, and `evaluate`.

- [ ] Add table-driven tests for every acceptance threshold, especially zero human-unsafe-to-safe errors and zero overlap leaks from clean.
- [ ] Add CLI integration tests for invalid paths, protected outputs, interrupted/resumed state, and count-only stdout.
- [ ] Run `go test ./cmd/advertisement-full-clean -count=1 -v`; expect RED before wiring, then implement minimal wiring and make it GREEN.
- [ ] Run `go test ./cmd/advertisement-full-clean -count=10`, its race equivalent, `go test ./... -count=1`, `go vet ./...`, and `./init.sh`.

### Task 6: Prepare the bounded pilot and stop at the human gate

**Files:**
- Modify: `feature_list.json`
- Modify: `progress.md`
- Modify: `session-handoff.md`

**Interfaces:**
- Produces a frozen calibration manifest, pilot configs, exact commands, expected hashes, and a human-readable aggregate report location.

- [ ] Run calibration generation and independently verify count, uniqueness, truth-source distribution, split stability, and absence of prompt text in manifests.
- [ ] Run all fake-provider gates and mutation checks for input blindness, original-ID mapping, missing-index rejection, agreement-only apply, overlap quarantine, and non-ad replacement identity.
- [ ] Record exact RED/GREEN/repeat/race/full/security evidence and source/protected hashes.
- [ ] Stop before the real Qwen pilot. Leave one next action: user approval to execute the frozen pilot command. Do not start the 58,658-row run.
