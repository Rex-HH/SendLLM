# SendLLM Local Safety Annotation CLI Design

Date: 2026-08-05
Status: Awaiting final written-spec approval

## 1. Objective

Build a minimal local Go CLI that submits approximately 30,000 safety samples to an OpenAI-compatible LLM, validates structured safety annotations, persists all progress, resumes after interruption without repeating successful calls, and exports deterministic JSONL for downstream processing.

The first release is deliberately a single-machine, single-process batch tool. It does not include an HTTP service, external message queue, Redis, cron scheduler, UI, or multi-instance coordination.

## 2. Inputs And Outputs

### 2.1 Input JSONL

Each line is a JSON object. The supported initial fields are:

```json
{
  "trace_id": "unique-id",
  "prompt": "text to review",
  "response": "optional model response"
}
```

Rules:

- `trace_id` is required, non-empty, and stable across retries.
- `response` may be absent, `null`, an empty string, or a non-empty string. Missing and `null` values normalize to an empty string.
- At least one of `prompt` and `response` must be non-empty. This retains support for prompt-only, response-only, and pair review.
- Unknown input fields are retained as raw JSON and merged back into the exported record.
- Identical duplicate `trace_id` records are skipped. The same `trace_id` with different review content is an import conflict and stops import before model calls begin.
- Each record receives an immutable input sequence number for deterministic export.

### 2.2 Model Input

The system prompt is loaded once from the configured file. Each user message contains only the agreed structured payload:

```json
{
  "trace_id": "unique-id",
  "scene": "prompt",
  "prompt": "text to review",
  "response": ""
}
```

`scene` is configured as `prompt`, `response`, `pair`, or `auto`. In `auto` mode it is derived from which content fields are non-empty.

### 2.3 Model Result

The model returns annotation fields only and must not echo the source payload:

```json
{
  "label": "unsafe",
  "explanation": "该输入试图诱导模型绕过安全限制。",
  "extended_info": {
    "risk_type": "jailbreak",
    "risk_level": "high",
    "attack_scenario": "role_play",
    "case_type": "typical",
    "is_attack": true,
    "other": ""
  }
}
```

The fixed core fields follow `样本格式-8-4.md`. `risk_type` is a single value from a task-configured closed set. Additional allowed fields live under `extended_info` and may be extended by task configuration without changing the runner.

Validation rules include:

- `label` is `safe` or `unsafe`.
- `explanation` satisfies configured length limits, initially 10 to 70 characters.
- An unsafe result includes a valid `risk_type` and `risk_level`.
- `risk_level` is `low`, `medium`, or `high`.
- `case_type=hard_negative` requires `label=safe`.
- A configured JSON Schema validates optional extension fields.

For `label=safe`, `extended_info` may be absent. If present, it must not contain `risk_type` or `risk_level`, `is_attack` must be false, and the remaining configured fields must still pass schema validation. This permits safe `hard_negative` samples without attaching an unsafe risk category.

The exporter merges valid annotations with the original object and sets `annotation.method` to `auto`. Generated `label`, `explanation`, `extended_info`, and `annotation` replace any same-named source fields; other unknown source fields remain unchanged. The original model reply remains in SQLite for audit and is not copied into the formal output.

### 2.4 Export Files

- The configured output JSONL contains successful complete records ordered by original input sequence.
- A sibling `*.failed.jsonl` contains final failures with `trace_id`, failure category, safe diagnostic summary, and attempt count.
- Export uses a temporary file followed by an atomic rename, so a crash cannot leave a half-written official output.

## 3. Task Configuration

Each run uses one YAML file. Important runtime behavior is explicit and validated before import or network calls.

```yaml
task:
  id: masb-2026-08
  input: ./data/input.jsonl
  output: ./data/output.jsonl
  state: ./data/masb-2026-08.db

model:
  base_url: https://example.com/v1
  api_key_env: LLM_API_KEY
  name: model-name
  structured_output: json_schema
  temperature: 0
  max_tokens: 500
  timeout: 60s

prompt:
  system_file: ./prompts/masb-system.txt
  scene: auto
  risk_types_file: ./config/risk-types.yaml

runtime:
  concurrency: 64
  requests_per_minute: 0
  tokens_per_minute: 0
  shutdown_timeout: 30s

retry:
  request_max_attempts: 5
  format_repair_attempts: 2
  initial_backoff: 1s
  max_backoff: 60s

output:
  explanation_min_length: 10
  explanation_max_length: 70
```

The API key value is read only from `api_key_env`; it is never written to configuration, SQLite, logs, or output. `structured_output` is explicitly one of `json_schema`, `json_object`, or `prompt_only` because compatible providers expose different capabilities.

Concurrency accepts 1 through 500 based on the reported account ceiling. Zero RPM or TPM disables that proactive limiter. A calibration run should begin around 64 concurrent requests and then increase based on observed latency, 429 rate, and provider documentation.

## 4. Architecture

The repository follows the naming in `开发指南.md` while omitting unused layers:

```text
SendLLM/
├── internal/
│   ├── service/           # Runner, retries, validation coordination, export
│   ├── facade/            # OpenAI-compatible HTTP adapter
│   ├── dao/               # SQLite persistence and migrations
│   ├── dto/               # Input, output, and provider wire structures
│   └── lib/
│       ├── configs/       # YAML loading, defaults, validation
│       ├── limiter/       # Concurrency, RPM, TPM, shared cooldown
│       └── tokenizer/     # Conservative token estimation
├── config/
├── prompts/
├── testdata/
├── main.go
├── go.mod
└── README.md
```

`main.go` owns CLI flags, application logger, configuration loading, dependency wiring, OS signals, and the final exit code. Packages return structured errors and do not log payloads.

The service defines the narrow model-calling behavior it consumes. The OpenAI facade provides a concrete implementation. SQLite is a concrete local store; abstraction is limited to boundaries required by service tests.

## 5. Persistent State And Recovery

SQLite is the source of truth and uses WAL mode with one application write connection. The schema records:

- task identity, normalized configuration fingerprint, prompt fingerprint, and task timestamps;
- item identity, input sequence, original JSON, normalized prompt/response, state, attempt counters, and next eligible time;
- every call attempt, phase, timing, HTTP status, error classification, retryability, validation details, token usage, and raw model response;
- the final validated annotation.

Item states are `pending`, `processing`, `retry_wait`, `succeeded`, and `failed`. State transitions and associated attempt data are transactional.

On startup, a matching task resumes from its database. A semantic fingerprint covers the model identity and sampling parameters, system prompt content, review scene policy, risk taxonomy, and output schema. A semantic fingerprint mismatch stops with a clear error unless the user chooses a new task/state file. Operational values such as concurrency, RPM/TPM limits, retry delays, shutdown timeout, and export path may change between resumptions. Stale `processing` items return to `pending`; `succeeded` items are never called again. Persisted attempt counts prevent restart loops from resetting retry limits.

## 6. Concurrency And Rate Limiting

The CLI uses bounded concurrency around ordinary synchronous HTTP calls:

```text
SQLite pending rows
  -> dispatcher
  -> concurrency + RPM + estimated TPM permits
  -> worker HTTP calls
  -> parse and validate
  -> transactional state update
```

The number of in-memory jobs remains bounded. The dispatcher reads small ordered batches, and every goroutine is tracked and cancellable through the top-level context.

The sustainable request rate is bounded by the minimum of concurrency/latency, RPM, and TPM/average-token-cost. Multiple calls on one API key cannot bypass provider quotas.

The HTTP transport is configured for the selected concurrency. Provider rate-limit headers are consumed when present but are not required. HTTP 429 triggers a shared cooldown across all workers so they do not continue attacking the same quota window.

On SIGINT or SIGTERM, the dispatcher stops claiming work, in-flight requests receive the configured drain window, remaining calls are cancelled after that deadline, and committed successes remain durable.

## 7. Structured Output And Error Policy

Response handling is strict:

1. Check HTTP status and bounded response size.
2. Extract the assistant content.
3. Decode exactly one JSON object; do not silently accept surrounding prose.
4. Validate JSON Schema and fixed field types.
5. Validate cross-field business rules.
6. Commit the validated result and attempt record together.

Errors fall into three durable classes:

- Retryable call errors: network interruption, timeout, HTTP 408, 429, and 5xx.
- Repairable output errors: invalid JSON, missing fields, invalid enums, or semantic contradictions.
- Permanent errors: invalid source input, 401/403, invalid model parameters, context-window overflow, or explicit provider content rejection.

Call errors use exponential backoff with full jitter, bounded by configuration. `Retry-After` takes precedence. `request_max_attempts` counts original classification attempts, including attempts that fail at the transport or HTTP layer. `format_repair_attempts` is a separate ceiling for repair calls made after a syntactically successful but invalid classification response. A repair request receives only the invalid model response, schema, and validation errors; it does not receive the original sensitive sample again. After repair attempts are exhausted, one new original classification may be attempted only if the original-attempt ceiling has not been reached. Final item failures are persisted and exported separately.

Authentication and global configuration failures pause the whole task. Record-specific permanent failures affect only that item.

## 8. Observability And Data Safety

Structured logs include task ID, `trace_id`, state, latency, attempt, HTTP status, and error category. They exclude source prompt/response, raw model output, authorization headers, API keys, and sensitive annotations.

CLI progress reports aggregate counts for succeeded, pending, retrying, failed, current completion rate, and estimated remaining time. High-volume per-record success logs are debug-only.

Dataset JSONL, SQLite files, raw outputs, environment files, and generated model records are ignored by Git. Test fixtures use synthetic, non-sensitive text.

## 9. CLI Behavior

The first release has one primary invocation:

```bash
sendllm -config ./config/task.yaml
```

The command validates configuration and input, creates or resumes the task, runs until completion/interruption/fatal task error, and exports current terminal results. Repeating the same invocation resumes the same SQLite state.

Exit codes are fixed as follows: `0` when every imported record succeeds, `2` when processing completes with one or more terminal item failures, `1` for configuration/authentication/storage or another task-fatal failure, and `130` when interrupted before reaching a terminal task state.

## 10. Testing And Verification

Unit and integration coverage includes:

- configuration defaults, required fields, capability modes, and invalid limits;
- missing/null/empty response values, at-least-one-content validation, unknown-field round trips, malformed JSONL, duplicate IDs, and conflicting IDs;
- result JSON decoding, size limits, risk enums, explanation length, and cross-field rules;
- retry classification, jitter bounds, `Retry-After`, shared cooldown, and cancellation;
- fake HTTP provider cases for success, 429, 5xx, timeout, malformed output, successful repair, exhausted repair, authentication failure, and content rejection;
- SQLite state transitions, stale-processing recovery, retry persistence, config fingerprint mismatch, and no repeated calls for successful items;
- deterministic ordered export and atomic file replacement;
- graceful interruption without goroutine leaks or data races.

Tests use the Go standard library, `httptest`, temporary SQLite databases, table-driven cases, and parser fuzz targets. No automated test calls a paid model API.

The repository verification gate is:

```bash
./init.sh
```

It checks `gofmt`, `go test ./...`, `go test -race ./...`, and `go vet ./...`.

## 11. Implementation Boundaries

The first implementation does not include automatic adaptive concurrency, multiple API keys, provider Batch APIs, HTTP administration, distributed leases, live configuration reload, UI dashboards, or human review workflows. These can be added later without changing the input/output contract or the persisted task identity rules.

Implementation follows the repository Harness in `AGENTS.md`, one feature at a time, with verification evidence recorded in `feature_list.json`, `progress.md`, and `session-handoff.md`.
