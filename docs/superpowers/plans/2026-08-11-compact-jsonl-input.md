# Compact JSONL Input Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Read upstream `compact_jsonl` records, send only extracted prompt/response content to the model, and export model judgment inside a nested `annotation` object while preserving every other source field.

**Architecture:** Keep SQLite and Runner boundaries unchanged by mapping compact `id` to the existing internal `TraceID`. Limit changes to DTO parsing, import/export tests, export merge logic, and the exported JSON Schema.

**Tech Stack:** Go standard library JSON handling, existing SQLite DAO, existing service importer/exporter, existing `./init.sh` harness gate.

## Global Constraints

- Keep the first release a local, single-process CLI.
- Use SQLite as the durable source of truth.
- Preserve unknown input fields and retain input order during export.
- Read API keys only from the configured environment variable.
- Never log raw `prompt`, `response`, full model output, or other dataset payloads.
- All新增 Go 注释必须使用中文。

---

### Task 1: Compact Source Parsing

**Files:**
- Modify: `internal/dto/sample.go`
- Test: `internal/dto/sample_test.go`

**Interfaces:**
- Consumes: `dto.ParseSource(raw []byte) (dto.SourceSample, error)`.
- Produces: `SourceSample{TraceID, Prompt, Response, Raw}` where `TraceID` equals compact `id`.

- [x] **Step 1: Write the failing tests** for compact messages extraction and missing usable messages.
- [x] **Step 2: Run** `go test ./internal/dto -run 'TestParseSource(Compact|RejectsInvalidContent)' -count=1 -v` and confirm the compact case fails.
- [x] **Step 3: Implement compact parsing** by reading `id` and `messages`, extracting first user/assistant contents, and keeping raw JSON unchanged.
- [x] **Step 4: Re-run** the focused DTO command and confirm it passes.

### Task 2: Import And Export Contract

**Files:**
- Modify: `internal/service/importer_test.go`
- Modify: `internal/service/exporter.go`
- Modify: `internal/service/exporter_test.go`
- Modify: `config/output-jsonl-schema.json`

**Interfaces:**
- Consumes: stored `dao.ExportRecord.Annotation` as the existing model annotation JSON.
- Produces: output JSON where generated fields live under top-level `annotation`.

- [x] **Step 1: Write failing import/export tests** showing compact source rows use `id` internally and original `label` survives export.
- [x] **Step 2: Run** `go test ./internal/service -run 'Test(ImportAcceptsCompactJSONL|ExportWritesOrderedMergedSuccessAndSafeFailures)' -count=1 -v` and confirm the export contract fails.
- [x] **Step 3: Implement export merge** so success and failure rows replace only top-level `annotation`.
- [x] **Step 4: Update output Schema** to require nested annotation fields instead of top-level generated fields.
- [x] **Step 5: Re-run** the focused service command and confirm it passes.

### Task 3: Harness Closeout

**Files:**
- Modify: `feature_list.json`
- Modify: `progress.md`
- Modify: `session-handoff.md`

**Interfaces:**
- Consumes: fresh verification output.
- Produces: resumable harness state with exact evidence.

- [x] **Step 1: Run** `gofmt` on changed Go files.
- [x] **Step 2: Run** `go test ./internal/dto ./internal/service -count=1`.
- [x] **Step 3: Run** `./init.sh`.
- [x] **Step 4: Update harness state files with evidence and next action.
- [x] **Step 5: Review** `git diff --check` and `git diff --stat`.
