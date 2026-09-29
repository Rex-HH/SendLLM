# Policy Optimization Implementation Contract Freeze

Date: 2026-09-07
Status: frozen implementation contract; no Go implementation authorized by this document
Scope: `feat-026` through `feat-038`

## 1. Authority and interpretation

This document removes implementation choices left open by the Policy Optimization design. For optimizer implementation, precedence is: current user instruction, `AGENTS.md`, this contract, Policy Optimization Design, Policy Optimization Implementation Plan, Harness, existing conventions.

Normative words are `MUST`, `MUST NOT`, `SHALL`, and `ONLY`. Examples do not widen closed sets. A coding model must stop and report the exact conflict instead of inventing a field, state, transition, Skill, authority, threshold, fallback, path, or release rule.

The subsystem remains one local process in the existing binary. It has no HTTP server, external queue, Redis, cron, dynamic Go plugin, native provider Skill dependency, multi-process owner, or hot reload.

## 2. Fixed identifiers and encoding

- All persisted text is UTF-8. JSON and JSONL are encoded without HTML escaping and end with one LF. YAML is UTF-8 with LF.
- Timestamps are UTC RFC3339Nano with exactly nine fractional digits, for example `2026-09-07T02:00:00.000000000Z`.
- SHA-256 is 64 lowercase hexadecimal characters over exact file bytes.
- IDs match `^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`; relative artifact paths use `/`, contain no empty, `.` or `..` segment, and never escape their declared root after symlink evaluation.
- Ordered JSON arrays remain ordered. Set-like arrays are deduplicated and lexical-byte sorted before canonical encoding.
- Canonical JSON recursively sorts object keys, preserves array order, preserves numbers with `json.Decoder.UseNumber`, emits no insignificant whitespace, and ends without LF when hashed as a value.
- Aggregate directory hash is SHA-256 over repeated `relative_path + "\n" + file_sha256 + "\n" + decimal_byte_size + "\n"` for manifest-listed files in lexical relative-path order. Manifest files never include their own aggregate hash input.
- Model-generated identifiers are rejected unless they are included in the call input's `allowed_ids` or use a supplied deterministic ID allocated before the call.

Closed enums:

```text
mode = analyze | compile
scene = prompt | response
executor = deterministic | model | model_with_deterministic_verifier
iteration_status = created | awaiting_mapping_approval | running | no_change | preview_ready | awaiting_regression | regression_failed | awaiting_release_approval | release_ready | released | interrupted | failed
stage_status = pending | running | retry_wait | succeeded | terminal_failed
artifact_sensitivity = public_policy | review_metadata | restricted_payload
authority = human_directive | security_team_decision | approved_gold_evidence | automated_proposal | third_party_proposal | observation
critic_verdict = accept | revise | block
gold_status = candidate | approved | core
comparison_type = label_mismatch | risk_mismatch | case_type_mismatch | policy_version_change | model_disagreement | human_selected | no_comparison
safe_error_category = network | timeout | rate_limited | server | authentication | bad_request | unsupported_mode | content_rejected | malformed_response | schema_invalid | semantic_invalid | refusal | context_too_large | mapping_required | mapping_invalid | source_invalid | authority_conflict | critic_blocked | compiler_invalid | regression_failed | approval_required | approval_invalid | release_conflict | artifact_corrupt | state_corrupt | canceled | internal
stage = preflight | inspect_sources | normalize_sources | select_disagreements | stratify | local_mining | global_merge | attach_cases | case_adjudication | policy_diagnosis | rule_authoring | independent_critic | load_change_requests | change_resolution | candidate_policy | prompt_compile | regression | release_approval | release
artifact_type = config_snapshot | base_release_snapshot | preflight_report | candidate_mapping | approved_mapping | normalized_records | assignment | batch | model_response | local_patterns | global_patterns | adjudication | diagnosis | proposal | direct_change_bundle | critique | critic_report | change_request | change_set | candidate_policy | compiled_prompt | schema | compile_manifest | candidate_gold | approved_gold | core_gold | regression_report | gate_report | approval | release_manifest | changelog | summary
```

### 2.1 Exact configuration shape

The strict YAML root has exactly these sections and fields. `models.profiles` keys are operator-defined IDs; every other mapping key shown is closed. Unknown keys fail.

```yaml
version: 1
iteration:
  id: ITER-007
  dir: ./iterations/ITER-007
policy:
  releases_dir: ./policy/releases
  base_version: p04b-v1.0
  target_version: p04b-v1.1
skills:
  root: ./policy-optimization/skills
  manifest: ./policy-optimization/skills/manifest.yaml
models:
  profiles:
    minimax_miner:
      family: minimax
      base_url: https://provider.invalid/v1
      api_key_env: MINIMAX_API_KEY
      name: MiniMax-M2.5
      structured_output: json_object
      max_tokens: 4000
      timeout: 90s
      quota_group: minimax_shared
    qwen_merger:
      family: qwen
      base_url: https://provider.invalid/v1
      api_key_env: QWEN_API_KEY
      name: qwen3-max
      structured_output: json_object
      max_tokens: 4000
      timeout: 90s
    qwen_critic:
      family: qwen
      base_url: https://provider.invalid/v1
      api_key_env: QWEN_API_KEY
      name: qwen3-max
      structured_output: json_object
      max_tokens: 4000
      timeout: 90s
    deepseek_policy:
      family: deepseek
      base_url: https://aigateway.venusgroup.com.cn/ai/deepseek/openai
      api_key_env: AI_GATEWAY_API_KEY
      name: deepseek-v4-pro
      structured_output: json_object
      max_tokens: 4000
      timeout: 90s
  roles:
    source_interpreter: {primary: minimax_miner, fallbacks: [], concurrency: 2, requests_per_minute: 0, tokens_per_minute: 0}
    local_miner: {primary: minimax_miner, fallbacks: [], concurrency: 4, requests_per_minute: 0, tokens_per_minute: 0}
    global_merger: {primary: qwen_merger, fallbacks: [], concurrency: 2, requests_per_minute: 0, tokens_per_minute: 0}
    case_adjudicator: {primary: deepseek_policy, fallbacks: [], concurrency: 2, requests_per_minute: 0, tokens_per_minute: 0}
    policy_diagnoser: {primary: deepseek_policy, fallbacks: [], concurrency: 2, requests_per_minute: 0, tokens_per_minute: 0}
    rule_author: {primary: deepseek_policy, fallbacks: [], concurrency: 1, requests_per_minute: 0, tokens_per_minute: 0}
    critic: {primary: qwen_critic, fallbacks: [], concurrency: 1, requests_per_minute: 0, tokens_per_minute: 0}
    change_resolver: {primary: deepseek_policy, fallbacks: [], concurrency: 1, requests_per_minute: 0, tokens_per_minute: 0}
  quota_groups:
    minimax_shared: {concurrency: 4, requests_per_minute: 0, tokens_per_minute: 0}
batching:
  homogeneous_percent: 70
  conflict_percent: 20
  random_percent: 10
  target_size: 50
  min_size: 30
  max_size: 100
context:
  max_input_tokens: 24000
  max_artifacts: 100
retry:
  transient_attempts_per_model: 3
  format_repair_attempts: 1
  refusal_reprompt_attempts: 1
  initial_backoff: 1s
  max_backoff: 60s
regression:
  contracts_dir: ./policy-optimization/regression/contracts
  approved_gold_dir: ./policy-optimization/regression/gold
  hidden_gold: ./Safety_Review_P04B_Hidden.jsonl
  gate_policy: ./policy-optimization/regression/gates/p04b-gate-v1.yaml
  safety_review_config: ./config/safety-review-eval.example.yaml
output:
  status_interval: 5s
  shutdown_timeout: 30s
```

Every role key is required. A role may use an empty fallback list but must retain one usable profile after preflight. Profile fields match the existing Safety Review OpenAI-compatible shape; `quota_group` is optional, all other shown profile fields are required. `family`, `base_url`, `api_key_env`, `name`, `structured_output`, `max_tokens`, and `timeout` are semantic. Runtime concurrency/rates, status interval, shutdown timeout, and retry timing are runtime-only; retry attempt counts are semantic.

Primary family constraints are MiniMax for `source_interpreter/local_miner`, Qwen for `global_merger/critic`, and DeepSeek for `case_adjudicator/policy_diagnoser/rule_author/change_resolver`. The union of Critic primary/fallback families must be disjoint from the union of Rule Author and Change Resolver families. Multiple profiles may share one `api_key_env`. Rate zero disables that rate. Positive concurrency is at most 500; positive durations and `max_tokens` are required.

`provider.invalid` is example-only and rejected by model-using commands. `validate` validates config and referenced static assets but does not require API env values or network. `compile --preview` does not require Regression/Gold paths to exist; `analyze` and `regression` require all Regression/Gold/Hidden paths and credentials. Secrets are loaded after strict validation and never enter normalized config, hashes, DB, Artifact, logs, or errors.

### 2.2 Deterministic IDs

- Canonical Record: `AR:` + SHA-256 hex of canonical JSON array `[package_id,source_id,sample_id]`.
- Batch: `B:H|C|R|M:` + six-digit one-based order within batch type; `M` contains more than one assignment stream after remainder packing.
- Local Pattern allowed IDs supplied per batch: `LP:<batch-id>:01` through `LP:<batch-id>:20`; more than 20 patterns is Schema-invalid and requires deterministic batch split/new call.
- Global Pattern IDs are allocated before each merge call from `GP:<iteration-id>:000001` upward in input Local-Pattern order; unused IDs remain unused.
- Case: `CASE:` + first 32 hex characters of the Record ID hash portion.
- Diagnosis and Proposal: `DG:<iteration-id>:000001` and `PR:<iteration-id>:000001`, monotonically allocated by sorted input work key before model calls.
- Direct bundle: `DIRECT:<iteration-id>`; Critique: `CT:` + first 32 hex characters of SHA-256 of proposal/direct-bundle ID.
- Change Set: `CS:<iteration-id>`; Candidate/version IDs follow section 3.
- Skill Run: `RUN:` + first 32 hex characters of SHA-256 of canonical `[iteration_id,stage,skill_id,work_id,input_sha256,context_sha256,attempt_number]`.
- Artifact: `ART:` + first 32 hex characters of SHA-256 of canonical `[iteration_id,artifact_type,relative_path,sha256]`.
- Regression: `RG:` + first 32 hex characters of SHA-256 of canonical `[iteration_id,candidate_sha256,suite_version,gate_policy_version,run_sequence]`.
- Gold: `G:` + SHA-256 hex of canonical `[sample_ref,source_sha256,policy_version]`; Approval: `AP:` + first 32 hex characters of SHA-256 of canonical `[subject_type,subject_id,subject_sha256,approver_id,created_at]`.

Before every model call, deterministic code supplies the finite allowed output IDs. A model may select only from them. Collision with different canonical input is fatal `state_corrupt`; no suffix or random replacement is allowed.

## 3. CLI contract

Global parsing uses `flag.FlagSet` with `ContinueOnError`. Unknown flags, positional arguments, duplicate singleton flags, missing values, empty IDs, and invalid combinations return 1 and print one concise usage line to stderr. Commands never print payload. `--config` is resolved first; command paths are resolved relative to the config file except `--package` and `--iteration-dir`, which are resolved relative to the current working directory.

| Command | Required | Optional | Network | Success state/exit |
|---|---|---|---|---|
| `validate` | `--config` | none | no | validated only, no durable mutation, 0 |
| `inspect` | `--config`, `--package` | none | only unknown/custom interpretation | inspection artifacts; 0 or 3 awaiting mapping approval |
| `mapping-approve` | `--config`, `--package`, `--source`, `--mapping`, `--approver` | none | no | immutable approved Mapping, 0 |
| `analyze` | `--config`, `--package` | repeatable `--change-request` | yes | complete through Regression; 0 for `no_change`, 3 when gates pass and release approval is needed, 1 when gates fail |
| `compile` | `--config`, `--policy`, one or more `--change-request` | `--preview` | Critic and Resolver use models | 0 for `no_change` or preview; otherwise `awaiting_regression` and 0 |
| `regression` | `--config`, `--candidate` | none | yes through Safety Review | `awaiting_release_approval` and 3 on pass; `regression_failed` and 1 on gate failure |
| `gold-approve` | `--config`, `--candidate-gold`, `--approver` | `--corrections` | no | new Approved Gold artifact, 0 |
| `gold-promote` | `--config`, `--approved-gold`, `--approver` | none | no | new Core Gold artifact, 0 |
| `approve` | `--config`, `--candidate`, `--approver` | `--note` | no | hash-bound policy approval and `release_ready`, 0 |
| `release` | `--config`, `--candidate` | none | no | immutable release and `released`, 0 |
| `status` | `--iteration-dir` | `--watch` | no | read-only snapshot/watch, 0 |

`--preview` is legal only on `compile`. A command waiting for an explicitly human-owned action returns 3, not 0 or 1. SIGINT/SIGTERM returns 130 after bounded drain. Config/validation/provider-fatal/state-corruption/release-integrity failure returns 1. `analyze` includes Regression; the explicit `regression` command supports direct compile and unchanged-candidate reruns.

Config requires `policy.base_version` and `policy.target_version`. Release IDs match `^p04b-v[1-9][0-9]*\.[0-9]+$`; target differs from base and must not already exist. A normal candidate is `<target_version>-candidate.1`; preview is `<target_version>-preview.1`; final release is exactly `<target_version>`. Candidate sequence is fixed at 1 because changed semantic inputs require a new iteration DB. `compile --policy` must equal configured `base_version`; Audit Package `current_policy_version` must also equal it. Preview cannot be promoted: run a new non-preview iteration with a new iteration ID.

Human commands require a nonempty operator-supplied `--approver`; the program does not default it from OS username or environment. Automatic commands cannot dispatch any approval command internally.

## 4. Stage graphs and transitions

### 4.1 Full analyze, with optional Change Requests

```text
created
 -> inspect_sources
 -> [awaiting_mapping_approval -> inspect_sources]
 -> normalize_sources
 -> select_disagreements
 -> stratify
 -> local_mining
 -> global_merge
 -> attach_cases
 -> case_adjudication
 -> policy_diagnosis
 -> rule_authoring
 -> [no_change when there is no Proposal and no Change Request]
 -> independent_critic
 -> change_resolution
 -> candidate_policy
 -> prompt_compile
 -> regression
 -> regression_failed | awaiting_release_approval
 -> release_ready
 -> released
```

### 4.2 Direct compile

```text
created
 -> load_change_requests
 -> independent_critic
 -> change_resolution
 -> [no_change when accepted_changes is empty and blocked_conflicts is empty]
 -> candidate_policy
 -> prompt_compile
 -> awaiting_regression
 -> regression
 -> regression_failed | awaiting_release_approval
 -> release_ready
 -> released
```

Preview stops after `prompt_compile`, marks the candidate `preview` and iteration `preview_ready`; it may be inspected but cannot enter Regression, Approval, or Release. A non-preview candidate is `regression_pending` and iteration `awaiting_regression`.

Only these resumptions are legal: `awaiting_mapping_approval` after a matching approved Mapping appears; `interrupted` after semantic hash equality; `retry_wait` after `next_attempt_at`; `regression_failed` after a new candidate or new explicit Regression run over unchanged bytes; `awaiting_release_approval` after matching approval; `release_ready` for release retry if no destination exists. `no_change`, `preview_ready`, `failed`, and `released` are terminal. A changed source/policy/Skill/Schema/semantic config requires a new iteration ID.

Every stage transition and active Artifact reference update is one SQLite transaction. Model work is never held inside a DB transaction. Claim uses compare-and-set `pending/retry_wait -> running`. Completion records attempt/artifact then changes state atomically. Recovery changes `running -> pending`, increments `recovery_count`, and does not alter attempt history.

## 5. Artifact layout and immutable manifest

The following first-success names are exact. A permitted rerun writes under the stage directory's `attempts/<four-digit-attempt>/` and makes that Artifact active in SQLite without overwriting the first-success file. Every reader resolves the active Artifact ID from SQLite and never assumes the first-success path is current.

```text
iterations/<iteration-id>/
  state.db
  manifest.yaml
  snapshots/config.normalized.json
  snapshots/base-release.yaml
  preflight/preflight-report.json
  model_attempts/<run-id>/response.bin
  inspections/<source-id>.candidate-mapping.yaml
  mappings/<source-id>.approved.yaml
  normalized/<source-id>.jsonl
  batches/assignment.json
  batches/<batch-id>.json
  local_patterns/<batch-id>.json
  global_patterns/global-patterns.json
  adjudications/<case-id>.json
  diagnoses/<diagnosis-id>.json
  proposals/<proposal-id>.json
  proposals/direct-change-bundle.json
  critic/<proposal-id>.json
  critic/critic-report.json
  change_requests/<change-id>.yaml
  resolved_changes/change-set.json
  candidate_policy/policy/**
  candidate_prompts/prompts/{judge-a,judge-b,router,expert,arbiter,refusal-reprompt}.txt
  candidate_prompts/schemas/{judgment,router,expert,arbiter}.json
  candidate_prompts/compile-manifest.json
  gold/candidate-gold.jsonl
  gold/approved/<gold-id>.json
  gold/core/<gold-id>.json
  regression/regression-report.json
  regression/gate-report.json
  approvals/<subject-type>-<subject-id>.json
  release/release-manifest.json
  release/changelog.md
  summary.md
```

Public version-controlled outputs are only generic Skill assets, Schemas, Gate Policies, minimal approved regression examples/contracts, and final released bundles. Iterations, packages, normalized rows, candidate artifacts, real Gold, DBs, model output, and approval working files remain Git-ignored.

Every indexed Artifact manifest entry has exactly:

```json
{
  "artifact_id": "ART:ITER-007:change-set",
  "artifact_type": "change_set",
  "relative_path": "resolved_changes/change-set.json",
  "media_type": "application/json",
  "sha256": "64-lowercase-hex",
  "byte_size": 1234,
  "producer_stage": "change_resolution",
  "producer_skill": "change-resolver",
  "producer_model_profile": "deepseek_policy",
  "parent_sha256": ["64-lowercase-hex"],
  "sensitivity": "review_metadata",
  "created_at": "2026-09-07T02:00:00.000000000Z"
}
```

`producer_model_profile` is empty for deterministic artifacts. Existing final paths are never overwritten. A rerun first writes an immutable attempt path, validates it, then updates the active logical reference. Files are temp-write, file-sync, close, rename, parent-directory-sync, then indexed transactionally. On startup, orphan temp files are deleted; unindexed final files and indexed missing/hash-mismatched files are fatal.

Sensitivity is fixed: normalized rows, batches containing records, model request/response/context attempts, patterns, adjudications, diagnoses, proposals, Critiques, Change Sets, Candidate Gold, and any Artifact containing case rationale are `restricted_payload`; manifests, approved Mappings without sampled values, ID-only assignments, aggregate Regression/Gate reports, approvals, status, and summaries are `review_metadata`; source Policy, compiled prompts/Schemas, Regression Contracts, changelog, and released bundle files are `public_policy`. Public logs may use only `review_metadata` fields explicitly listed in status and never copy arbitrary Artifact text.

Status JSON is exactly `{iteration_id,mode,status,current_stage,base_policy_version,target_policy_version,sources,records,batches,skill_runs,patterns,candidate,regression,approval,release,throughput_per_minute,eta_seconds,recent_error_categories,next_action,updated_at}`. Nested counters contain integers only; `recent_error_categories` maps closed safe category to count; hashes may appear but paths are relative. `next_action` is one closed value: `continue|approve_mapping|run_regression|fix_regression|approve_release|release|none|inspect_failure`. TTY renders these fields in one refreshing panel; non-TTY emits the same compact JSON once per interval. Status never reads restricted Artifact bodies.

## 6. Exact SQLite schema

The implementation embeds this schema as `internal/dao/policy_optimizer_schema.sql`. It may add comments but MUST NOT add/drop/rename tables or columns. Index names are fixed.

```sql
CREATE TABLE IF NOT EXISTS optimization_iterations (
  iteration_id TEXT PRIMARY KEY,
  mode TEXT NOT NULL CHECK (mode IN ('analyze','compile')),
  status TEXT NOT NULL CHECK (status IN ('created','awaiting_mapping_approval','running','no_change','preview_ready','awaiting_regression','regression_failed','awaiting_release_approval','release_ready','released','interrupted','failed')),
  current_stage TEXT NOT NULL,
  base_policy_version TEXT NOT NULL,
  base_policy_hash TEXT NOT NULL CHECK (length(base_policy_hash) = 64),
  config_hash TEXT NOT NULL CHECK (length(config_hash) = 64),
  semantic_hash TEXT NOT NULL CHECK (length(semantic_hash) = 64),
  objective_json TEXT NOT NULL CHECK (json_valid(objective_json)),
  permissions_json TEXT NOT NULL CHECK (json_valid(permissions_json)),
  fatal_error_category TEXT NOT NULL DEFAULT '',
  recovery_count INTEGER NOT NULL DEFAULT 0 CHECK (recovery_count >= 0),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS optimization_sources (
  iteration_id TEXT NOT NULL REFERENCES optimization_iterations(iteration_id) ON DELETE RESTRICT,
  source_id TEXT NOT NULL,
  source_type TEXT NOT NULL,
  source_path TEXT NOT NULL,
  source_format TEXT NOT NULL CHECK (source_format IN ('csv','jsonl','markdown')),
  source_sha256 TEXT NOT NULL CHECK (length(source_sha256) = 64),
  expected_count INTEGER NOT NULL CHECK (expected_count >= 0),
  actual_count INTEGER NOT NULL DEFAULT 0 CHECK (actual_count >= 0),
  selection_reason TEXT NOT NULL,
  trust_json TEXT NOT NULL CHECK (json_valid(trust_json)),
  mapping_status TEXT NOT NULL CHECK (mapping_status IN ('not_required','candidate','approved','invalidated')),
  mapping_sha256 TEXT NOT NULL DEFAULT '' CHECK (mapping_sha256 = '' OR length(mapping_sha256) = 64),
  normalization_status TEXT NOT NULL CHECK (normalization_status IN ('pending','running','succeeded','terminal_failed')),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (iteration_id, source_id)
);

CREATE TABLE IF NOT EXISTS optimization_records (
  iteration_id TEXT NOT NULL REFERENCES optimization_iterations(iteration_id) ON DELETE RESTRICT,
  record_id TEXT NOT NULL,
  source_id TEXT NOT NULL,
  sample_id TEXT NOT NULL,
  input_order INTEGER NOT NULL CHECK (input_order >= 0),
  scene TEXT NOT NULL CHECK (scene IN ('prompt','response')),
  policy_version TEXT NOT NULL,
  selection_reason TEXT NOT NULL,
  comparison_json TEXT NOT NULL CHECK (json_valid(comparison_json)),
  provenance_json TEXT NOT NULL CHECK (json_valid(provenance_json)),
  metadata_json TEXT NOT NULL CHECK (json_valid(metadata_json)),
  payload_artifact_id TEXT NOT NULL,
  canonical_sha256 TEXT NOT NULL CHECK (length(canonical_sha256) = 64),
  state TEXT NOT NULL CHECK (state IN ('normalized','selected','assigned','excluded')),
  created_at TEXT NOT NULL,
  PRIMARY KEY (iteration_id, record_id),
  UNIQUE (iteration_id, source_id, sample_id),
  FOREIGN KEY (iteration_id, source_id) REFERENCES optimization_sources(iteration_id, source_id) ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS optimization_batches (
  iteration_id TEXT NOT NULL REFERENCES optimization_iterations(iteration_id) ON DELETE RESTRICT,
  batch_id TEXT NOT NULL,
  batch_order INTEGER NOT NULL CHECK (batch_order >= 0),
  mix_type TEXT NOT NULL CHECK (mix_type IN ('homogeneous','conflict','random','mixed')),
  stratum_key TEXT NOT NULL,
  record_ids_json TEXT NOT NULL CHECK (json_valid(record_ids_json)),
  record_count INTEGER NOT NULL CHECK (record_count >= 0),
  input_sha256 TEXT NOT NULL CHECK (length(input_sha256) = 64),
  status TEXT NOT NULL CHECK (status IN ('pending','running','retry_wait','succeeded','terminal_failed')),
  attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
  recovery_count INTEGER NOT NULL DEFAULT 0 CHECK (recovery_count >= 0),
  next_attempt_at TEXT NOT NULL DEFAULT '',
  terminal_error_category TEXT NOT NULL DEFAULT '',
  active_artifact_id TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (iteration_id, batch_id),
  UNIQUE (iteration_id, batch_order)
);

CREATE TABLE IF NOT EXISTS optimization_skill_runs (
  run_id TEXT PRIMARY KEY,
  iteration_id TEXT NOT NULL REFERENCES optimization_iterations(iteration_id) ON DELETE RESTRICT,
  stage TEXT NOT NULL,
  skill_id TEXT NOT NULL,
  work_id TEXT NOT NULL,
  executor TEXT NOT NULL CHECK (executor IN ('deterministic','model','model_with_deterministic_verifier')),
  input_sha256 TEXT NOT NULL CHECK (length(input_sha256) = 64),
  context_sha256 TEXT NOT NULL CHECK (length(context_sha256) = 64),
  model_profile TEXT NOT NULL DEFAULT '',
  model_family TEXT NOT NULL DEFAULT '',
  attempt_number INTEGER NOT NULL CHECK (attempt_number >= 1),
  status TEXT NOT NULL CHECK (status IN ('pending','running','retry_wait','succeeded','terminal_failed')),
  error_category TEXT NOT NULL DEFAULT '',
  next_attempt_at TEXT NOT NULL DEFAULT '',
  fallback_from_run_id TEXT NOT NULL DEFAULT '',
  output_artifact_id TEXT NOT NULL DEFAULT '',
  prompt_tokens INTEGER NOT NULL DEFAULT 0 CHECK (prompt_tokens >= 0),
  completion_tokens INTEGER NOT NULL DEFAULT 0 CHECK (completion_tokens >= 0),
  started_at TEXT NOT NULL DEFAULT '',
  completed_at TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  UNIQUE (iteration_id, stage, skill_id, work_id, input_sha256, context_sha256, attempt_number)
);

CREATE TABLE IF NOT EXISTS optimization_artifacts (
  artifact_id TEXT PRIMARY KEY,
  iteration_id TEXT NOT NULL REFERENCES optimization_iterations(iteration_id) ON DELETE RESTRICT,
  artifact_type TEXT NOT NULL,
  relative_path TEXT NOT NULL,
  media_type TEXT NOT NULL,
  sha256 TEXT NOT NULL CHECK (length(sha256) = 64),
  byte_size INTEGER NOT NULL CHECK (byte_size >= 0),
  producer_stage TEXT NOT NULL,
  producer_skill TEXT NOT NULL DEFAULT '',
  producer_model_profile TEXT NOT NULL DEFAULT '',
  parent_sha256_json TEXT NOT NULL CHECK (json_valid(parent_sha256_json)),
  sensitivity TEXT NOT NULL CHECK (sensitivity IN ('public_policy','review_metadata','restricted_payload')),
  is_active INTEGER NOT NULL CHECK (is_active IN (0,1)),
  created_at TEXT NOT NULL,
  UNIQUE (iteration_id, relative_path)
);

CREATE TABLE IF NOT EXISTS optimization_patterns (
  iteration_id TEXT NOT NULL REFERENCES optimization_iterations(iteration_id) ON DELETE RESTRICT,
  pattern_id TEXT NOT NULL,
  pattern_level TEXT NOT NULL CHECK (pattern_level IN ('local','global')),
  status TEXT NOT NULL CHECK (status IN ('candidate','validated','rejected','superseded')),
  artifact_id TEXT NOT NULL REFERENCES optimization_artifacts(artifact_id) ON DELETE RESTRICT,
  coverage_count INTEGER NOT NULL CHECK (coverage_count >= 0),
  batch_count INTEGER NOT NULL CHECK (batch_count >= 0),
  source_distribution_json TEXT NOT NULL CHECK (json_valid(source_distribution_json)),
  model_distribution_json TEXT NOT NULL CHECK (json_valid(model_distribution_json)),
  created_at TEXT NOT NULL,
  PRIMARY KEY (iteration_id, pattern_id)
);

CREATE TABLE IF NOT EXISTS optimization_candidates (
  iteration_id TEXT NOT NULL REFERENCES optimization_iterations(iteration_id) ON DELETE RESTRICT,
  candidate_version TEXT NOT NULL,
  base_version TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('preview','regression_pending','regression_failed','awaiting_approval','release_ready','released','blocked')),
  change_set_artifact_id TEXT NOT NULL REFERENCES optimization_artifacts(artifact_id) ON DELETE RESTRICT,
  policy_artifact_id TEXT NOT NULL REFERENCES optimization_artifacts(artifact_id) ON DELETE RESTRICT,
  compile_manifest_artifact_id TEXT NOT NULL REFERENCES optimization_artifacts(artifact_id) ON DELETE RESTRICT,
  critic_artifact_id TEXT NOT NULL REFERENCES optimization_artifacts(artifact_id) ON DELETE RESTRICT,
  candidate_sha256 TEXT NOT NULL CHECK (length(candidate_sha256) = 64),
  blocked_conflicts_json TEXT NOT NULL CHECK (json_valid(blocked_conflicts_json)),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (iteration_id, candidate_version),
  UNIQUE (candidate_version)
);

CREATE TABLE IF NOT EXISTS optimization_regressions (
  regression_id TEXT PRIMARY KEY,
  iteration_id TEXT NOT NULL REFERENCES optimization_iterations(iteration_id) ON DELETE RESTRICT,
  candidate_version TEXT NOT NULL,
  candidate_sha256 TEXT NOT NULL CHECK (length(candidate_sha256) = 64),
  suite_version TEXT NOT NULL,
  gate_policy_version TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('running','passed','failed','terminal_failed')),
  rotation_a_artifact_id TEXT NOT NULL DEFAULT '',
  rotation_b_artifact_id TEXT NOT NULL DEFAULT '',
  report_artifact_id TEXT NOT NULL DEFAULT '',
  gate_artifact_id TEXT NOT NULL DEFAULT '',
  report_sha256 TEXT NOT NULL DEFAULT '' CHECK (report_sha256 = '' OR length(report_sha256) = 64),
  passed INTEGER NOT NULL DEFAULT 0 CHECK (passed IN (0,1)),
  created_at TEXT NOT NULL,
  completed_at TEXT NOT NULL DEFAULT '',
  FOREIGN KEY (iteration_id, candidate_version) REFERENCES optimization_candidates(iteration_id, candidate_version) ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS optimization_approvals (
  approval_id TEXT PRIMARY KEY,
  iteration_id TEXT NOT NULL REFERENCES optimization_iterations(iteration_id) ON DELETE RESTRICT,
  subject_type TEXT NOT NULL CHECK (subject_type IN ('mapping','gold','core_gold','policy_release')),
  subject_id TEXT NOT NULL,
  subject_sha256 TEXT NOT NULL CHECK (length(subject_sha256) = 64),
  related_sha256_json TEXT NOT NULL CHECK (json_valid(related_sha256_json)),
  approver_id TEXT NOT NULL,
  decision TEXT NOT NULL CHECK (decision IN ('approved','rejected')),
  note TEXT NOT NULL DEFAULT '',
  artifact_id TEXT NOT NULL REFERENCES optimization_artifacts(artifact_id) ON DELETE RESTRICT,
  created_at TEXT NOT NULL,
  UNIQUE (iteration_id, subject_type, subject_id, subject_sha256, approver_id)
);

CREATE TABLE IF NOT EXISTS optimization_releases (
  release_version TEXT PRIMARY KEY,
  iteration_id TEXT NOT NULL REFERENCES optimization_iterations(iteration_id) ON DELETE RESTRICT,
  candidate_version TEXT NOT NULL,
  base_version TEXT NOT NULL,
  candidate_sha256 TEXT NOT NULL CHECK (length(candidate_sha256) = 64),
  regression_sha256 TEXT NOT NULL CHECK (length(regression_sha256) = 64),
  approval_sha256 TEXT NOT NULL CHECK (length(approval_sha256) = 64),
  release_sha256 TEXT NOT NULL CHECK (length(release_sha256) = 64),
  release_path TEXT NOT NULL UNIQUE,
  released_at TEXT NOT NULL,
  FOREIGN KEY (iteration_id, candidate_version) REFERENCES optimization_candidates(iteration_id, candidate_version) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_optimization_sources_state ON optimization_sources(iteration_id, normalization_status, source_id);
CREATE INDEX IF NOT EXISTS idx_optimization_records_state ON optimization_records(iteration_id, state, input_order);
CREATE INDEX IF NOT EXISTS idx_optimization_batches_claim ON optimization_batches(iteration_id, status, next_attempt_at, batch_order);
CREATE INDEX IF NOT EXISTS idx_optimization_skill_runs_work ON optimization_skill_runs(iteration_id, stage, skill_id, work_id, status);
CREATE INDEX IF NOT EXISTS idx_optimization_artifacts_hash ON optimization_artifacts(iteration_id, sha256);
CREATE UNIQUE INDEX IF NOT EXISTS idx_optimization_artifacts_active_singleton ON optimization_artifacts(iteration_id, artifact_type) WHERE is_active = 1 AND artifact_type IN ('preflight_report','critic_report','change_set','compile_manifest','regression_report','gate_report','release_manifest');
CREATE INDEX IF NOT EXISTS idx_optimization_patterns_level ON optimization_patterns(iteration_id, pattern_level, status);
CREATE INDEX IF NOT EXISTS idx_optimization_regressions_candidate ON optimization_regressions(iteration_id, candidate_version, created_at);
CREATE INDEX IF NOT EXISTS idx_optimization_approvals_subject ON optimization_approvals(iteration_id, subject_type, subject_id, created_at);
```

Foreign references to active artifacts that cannot be expressed safely before insertion are validated in the same application transaction. No migration may modify legacy or `safety_review_*` tables.

## 7. Canonical external contracts

All JSON Schemas use draft 2020-12, `type: object`, `additionalProperties: false`, explicit `required`, integer bounds, closed enums, and string length bounds. Every ID max length is 128; title/short rationale 500; long rationale/limitations 4,000; evidence arrays max 100 unless a smaller bound is listed. Empty arrays encode as `[]`, never `null`.

### 7.1 Audit Package and Mapping

Audit manifest required fields are `version=1`, `package_id`, nonempty `objective[]`, `current_policy_version`, nonempty closed `analysis_permissions[]`, nonempty `sources[]`, and `change_requests[]`. A source requires `source_id`, closed `type`, relative `path`, `format`, `selection_reason`, `expected_records`, `sha256`, `mapping`, and `trust`. Known source adapters may set `mapping` empty only when their built-in mapping ID/version and source hash are recorded.

Approved Mapping fields are exactly those shown in Policy Optimization Design 7.1. JSONL paths use RFC 6901; CSV/Markdown paths are exact columns; Markdown is exactly one GFM table. `record_selector=each_record`; no defaults, scripts, expressions, transformations, recursive selectors, or computed values.

Canonical Audit Record required fields and bounds:

| Field | Type/contract |
|---|---|
| `record_id`, `sample_id`, `source_id`, `source_task` | ID string; `source_task` may be empty |
| `source_type`, `selection_reason`, `scene`, `policy_version` | nonempty closed/declared strings |
| `prompt`, `response` | strings; at least the scene-owned field is nonempty when payload analysis is permitted |
| `judgments` | 1-20 Judgment objects |
| `comparison` | `{left,right,type}`; indices exist and differ unless `no_comparison` |
| `metadata` | object containing `source_fields`; values preserve canonical JSON types |
| `provenance` | `{package_id,source_sha256,mapping_sha256,input_order}` |

A Judgment is exactly `{actor_type,actor_id,label,risk_types,case_type,authority,source_ref}`. `actor_type=human|gold|model|pipeline|policy`; `label=safe|unsafe|quarantine|unknown`; `risk_types` max 20 lexical unique; `case_type=typical|borderline|hard_negative|variant|unknown`. Candidate/observation authority is never Gold truth.

### 7.2 Patterns, adjudication, diagnosis

Local Pattern requires `pattern_id`, `title`, `hypothesis`, `cause_candidates[]`, `covered_record_ids[]`, `representative_record_ids[]`, `counterexample_record_ids[]`, `source_distribution`, `category_distribution`, `confidence=candidate`, and `limitations`. It cites at least two covered records unless `singleton_candidate=true`; all cited IDs are input-allowed.

Global Pattern requires `pattern_id`, `local_pattern_ids[]`, `description`, deterministic `coverage_count`, `batch_count`, `source_distribution`, `model_distribution`, `representative_record_ids[3..10]` when population permits, `random_record_ids[2..5]` when population permits, `boundary_record_ids[0..10]`, `historical_pattern_ids[]`, and `merge_rationale`. Model-supplied counts are ignored and replaced by deterministic values only after membership validation.

Case Adjudication requires `case_id`, `policy_version`, `applicable_rule_ids[]`, `scene`, `evidence_ownership`, `condition_results[]`, `exclusion_results[]`, `current_policy_result`, `ambiguity`, `proposed_gold_status=candidate|none`, and `rationale`. Each result is `{id,status,evidence_refs}` with status `satisfied|absent|present|unknown`; IDs exactly cover the selected card.

Diagnosis requires `diagnosis_id`, `global_pattern_ids[1..20]`, `causes[]` from the Design closed 12-cause set, `affected_rule_ids[]`, `affected_prompt_roles[]`, `affected_workflow_stages[]`, `evidence_refs[]`, `estimated_impact`, `policy_change_warranted`, and nonempty `alternative_explanations[]`.

### 7.3 Change, Critic, and approval contracts

Change Request is exactly the Design section 11 structure. `requested_changes` has 1-50 strings; `targets` has 1-20 closed target IDs loaded from the base release; `evidence_refs` max 100. Authority must be legal for source type: human -> human_directive/observation; security_team -> security_team_decision/observation; automated_proposal -> automated_proposal; third_party_model/external_research -> third_party_proposal/observation.

Proposal requires `proposal_id`, `problem`, deterministic `affected_count`, deterministic `source_distribution`, `current_rule_excerpts[]` each with `path/json_pointer/sha256/text`, `patch_operations[]`, `addressed_pattern_ids[]`, `regression_risks[]`, `proposed_regression_cases[]`, `proposed_contracts[]`, and `non_goals[]`.

Critique requires `critique_id`, `proposal_id`, `verdict`, `findings[]`, `counterexample_record_ids[]`, `required_changes[]`, `checked_contract_ids[]`, and `limitations[]`. A Finding is `{finding_id,severity,category,evidence_refs,rationale}`; severity `blocking|major|minor`; category is `loophole|over_breadth|under_coverage|taxonomy_conflict|evidence_ownership|case_type_conflict|security_privacy|regression`. Deterministic code creates `critic-report.json` with ordered critique IDs/hashes and aggregate verdict: `block` if any block, otherwise `revise` if any revise, otherwise `accept`.

Change Set requires `base_version`, `base_sha256`, `candidate_version`, `accepted_changes[]`, `rejected_changes[]`, `blocked_conflicts[]`, `affected_artifacts[]`, and `required_regression_contracts[]`. Each accepted change cites source request/proposal IDs, authority, exact target path, operation, pointer, old value hash, and new canonical value. Allowed operation is `add_yaml_field|replace_yaml_field|remove_yaml_field|add_versioned_card|add_decision|add_regression_contract`. Any blocked conflict makes compilation illegal.

Approval is exactly `{version:1,approval_id,subject_type,subject_id,subject_sha256,related_sha256,approver_id,decision,note,created_at}`. `related_sha256` is a lexical-key object. Policy release approval must contain `candidate`, `regression_report`, `gate_report`, and `critic_report`. Any mismatch invalidates it.

### 7.4 Gold and Regression

Gold Record is exactly `{gold_id,sample_ref,source_sha256,policy_version,status,label,risk_types,case_type,rule_ids,decision_ids,model_candidate_artifact,approval_artifact,successful_release_ids,challenge_ids,created_at,updated_at}`. Candidate requires model artifact and no approval; Approved requires human approval; Core requires Approved ancestry, at least three distinct successful releases, zero unresolved challenge, and explicit core approval.

Candidate Gold is assembled deterministically only from successful `case-adjudicator` outputs whose `proposed_gold_status=candidate`; no second model labels it. `gold-approve` validates the complete candidate file and an optional Corrections JSONL. Each Correction is exactly `{gold_id,label,risk_types,case_type,rule_ids,decision_ids,rationale}` and must reference one candidate. Candidates absent from Corrections are approved unchanged; unknown/duplicate correction IDs fail the whole command. The command emits one immutable Approved Gold Record and Approval per candidate in input order. `gold-promote` recomputes release history and unresolved challenges for every supplied Approved Gold Record; one ineligible record fails the whole promotion transaction.

Regression Contract is exactly `{version,contract_id,contract_version,policy_scope,description,suite_query,minimum_cases,expected,supersedes,human_directive_ref}`. `expected` permits only `label`, `minimum_pass_count`, `maximum_false_unsafe`, `maximum_false_safe`, and `maximum_quarantine`. Superseding a contract requires incremented version and Human Directive.

Regression Report has base/candidate release hashes, suite/gate versions, two independent rotation results, per-suite confusion/count deltas, fixed/newly-wrong IDs, Safe->Unsafe/Unsafe->Safe IDs, quarantine/category/rule/stage/fallback deltas, and every contract result. Gate Report independently recomputes each Design section 14 gate as `{gate_id,passed,observed,required,evidence_refs}` and has aggregate `passed` equal to logical AND.

## 8. Skill Runtime and exact Skill instructions

`policy-optimization/skills/manifest.yaml` lists exactly 13 entries in the order below with `id`, `version: 1`, fixed `executor`, `input_schema`, `output_schema`, and model role when applicable. No runtime discovery from arbitrary directories occurs.

Every `SKILL.md` contains exactly these headings: `# <id>`, `## Objective`, `## Applicable Input`, `## Procedure`, `## Mandatory Checks`, `## Prohibitions`, `## Output Contract`, `## Failure Handling`. The implementer must place the statements below verbatim under the matching headings, changing only list punctuation needed for Markdown. It must not translate, paraphrase, merge, omit, or add instructions.

### 8.1 `audit-source-interpreter` (model, MiniMax)

**Objective:** Infer a candidate field and provenance Mapping for exactly one unknown or custom source.

**Applicable Input:** Use only the deterministic structural profile, at most 20 sampled records, source manifest, current Policy version, and supplied source, authority, and comparison enums.

**Procedure:** Identify the record unit, field meanings, actors, trust, comparison relation, suitable analyses, unsuitable analyses, and ambiguities. Emit a candidate Mapping only.

**Mandatory Checks:** Cite sampled field names, attach confidence to every inference, and preserve uncertain semantics as explicit ambiguity.

**Prohibitions:** Do not approve a Mapping, read or claim knowledge of unsampled rows, assign Gold authority, transform payload, or invent labels.

**Output Contract:** Return one object matching the supplied output Schema with candidate Mapping, `suitable_analyses`, `unsuitable_analyses`, `ambiguities`, and `sample_refs`.

**Failure Handling:** If any required semantic remains unresolved, return `needs_human_mapping`; never guess the missing mapping.

### 8.2 `audit-normalizer` (deterministic)

**Objective:** Convert one source into Canonical Audit Records without semantic inference.

**Applicable Input:** Use one source, its manifest entry, and one source-hash-bound approved Mapping.

**Procedure:** Apply the Mapping to every source record in order and canonicalize the resulting records.

**Mandatory Checks:** Validate required values, authority, comparison indices, scene ownership, duplicate IDs, canonical JSON, source count, and source hash.

**Prohibitions:** Do not call a model, change labels, fill defaults, interpret prose, or partially commit a source.

**Output Contract:** Produce normalized JSONL, ordered record IDs, exact count, and SHA-256.

**Failure Handling:** One invalid record rolls back every normalized-record database insert for that source and returns `source_invalid`.

### 8.3 `disagreement-miner` (deterministic)

**Objective:** Select high-value audit records using structured metadata only.

**Applicable Input:** Use Canonical Audit Record metadata, declared comparisons, Safety Review quality events, source type, and selection reason.

**Procedure:** Apply the fixed inclusion rules below in order, record the first inclusion reason plus all matching secondary reasons, and preserve canonical input order.

**Mandatory Checks:** Validate every compared judgment index and record an explicit exclusion reason for every unselected record.

**Prohibitions:** Do not inspect payload semantics, create a disagreement absent from structured judgments, resample a source, or alter provenance.

**Output Contract:** Produce ordered selected and excluded record IDs with exact selection rule IDs and counts.

**Failure Handling:** Invalid comparison/provenance fails the source selection with `source_invalid`; it is not treated as exclusion.

Fixed inclusion rules are:

1. Include every `human_selected` or `historical_error` source record.
2. Include any record whose declared comparison has unequal label, risk set, or case type; record each mismatch separately.
3. Include a Safety Review quality event when A/B labels differ, either Judge differs from the final label, Router has candidates while both Judges say Safe, Expert results conflict, terminal state is quarantine, policy coverage gap exists, fallback/independence degradation occurred, a stage terminally failed, or an error-pattern ID exists.
4. Include `policy_version_diff` when final label, primary category, risk set, case type, quarantine state, or established Expert set changed.
5. Include all `external_benchmark`/`third_party_model` records only when a comparison is declared; otherwise exclude as `no_comparison` unless the package source type is explicitly `random_sample`.
6. Include all `random_sample` records. Sampling happens before package creation; the optimizer does not resample that source.
7. Exclude every remaining record as `no_configured_signal`.

Duplicate sample content from different sources remains separate because provenance differs. Exact duplicate `source_id/sample_id` is rejected during normalization, not deduplicated here.

### 8.3.1 Exact stratification and batching

For each selected record, deterministic code creates `stratum_key` as canonical JSON array `[scene,source_type,source_id,sorted_candidate_risks,label_transition,comparison_type,expert_category]`; absent values are empty strings/arrays. It creates `rank_hash=SHA256(iteration_id + "\x00" + record_id)`.

For `N` records, targets are `H=floor(N*homogeneous_percent/100)`, `C=floor(N*conflict_percent/100)`, and `R=N-H-C`. Allocate H across strata by floor of `stratum_size*H/N`, then distribute remaining H slots by descending fractional remainder, lexical `stratum_key` tie-break. Within each stratum, lowest `rank_hash` records fill H. From the remaining pool, sort by `rank_hash` then record ID; first C become conflict and the rest R become random. This produces exact global integer totals without duplicate assignment.

Homogeneous records are ordered by lexical stratum then rank hash. Conflict records use round-robin selection across lexical nonempty strata so adjacent records differ in stratum whenever at least two strata remain. Random records use rank-hash order. Take full `target_size` batches from each H/C/R stream while leaving a stream remainder smaller than `target_size`; concatenate those remainders in H/C/R order and pack them into `mixed` batches. If the final batch is below `min_size`, append it to the previous batch when the result is at most `max_size`; otherwise move the minimum number of records from the previous batch so both are within bounds. A whole dataset below `min_size` forms one `B:M:000001` batch. Zero records forms zero batches. Batch type is H/C/R only when all members have that assignment; otherwise M. Final IDs are assigned after rebalancing in H, C, R, M order.

Tests independently recompute targets, assignment, ordering, and batch sizes. No pseudo-random generator, map iteration order, wall clock, or model output participates.

### 8.4 `local-error-pattern-miner` (model, MiniMax)

**Objective:** Discover candidate Local Error Patterns inside exactly one batch.

**Applicable Input:** Use one 30-100 row batch, relevant Policy and Rule Cards, supplied allowed causes and IDs, and Context Builder-selected historical summaries.

**Procedure:** Find repeated error patterns, root-cause hypotheses, covered records, representative records, and suspected counterexamples.

**Mandatory Checks:** Separate source-label error from Policy, Prompt, taxonomy, evidence, and workflow hypotheses; cite only supplied IDs; state limitations.

**Prohibitions:** Do not edit Policy, decide release, approve Gold, report authoritative counts, or merge patterns outside the batch.

**Output Contract:** Return one object matching the supplied Schema with zero to 20 Local Patterns. A one-case pattern must set `singleton_candidate=true`.

**Failure Handling:** If no coherent pattern exists, return an empty pattern array. Refusal or terminal failure blocks dependent merge but not unrelated batches.

### 8.5 `global-pattern-merger` (model with deterministic verifier, Qwen)

**Objective:** Propose cross-batch Global Pattern clusters from Local Pattern summaries.

**Applicable Input:** Use Local Pattern Artifacts, supplied aggregate metadata, historical pattern summaries, and allowed Global Pattern IDs; never use the full raw corpus.

**Procedure:** Cluster semantically equivalent Local Patterns, explain each merge, preserve conflicts and counterexamples, and propose representative and historical references.

**Mandatory Checks:** Assign or explicitly reject every input Local Pattern and give a rejection reason; cite only supplied IDs.

**Prohibitions:** Do not report trusted counts or distributions, load unsupplied raw rows, or discard contradictory patterns silently.

**Output Contract:** Return proposed Global Patterns matching the supplied Schema; deterministic code verifies membership, computes counts, and attaches cases.

**Failure Handling:** Unresolved cluster ambiguity remains separate patterns. Invalid membership or invented IDs returns `semantic_invalid`.

### 8.6 `case-adjudicator` (model, DeepSeek)

**Objective:** Resolve one policy-relevant case under the current Policy and expose remaining ambiguity.

**Applicable Input:** Use exactly one attached case, current Policy, relevant Rule Cards, scene evidence ownership, allowed IDs, and output matrix.

**Procedure:** Evaluate every required condition and decisive exclusion, derive the current-policy result, separately state any future-policy suggestion, and identify ambiguity.

**Mandatory Checks:** Cover every supplied condition and exclusion ID exactly once and cite only scene-legal evidence references.

**Prohibitions:** Do not read hidden Gold, approve Gold, create Rule IDs, transfer Prompt evidence to Response, or treat a future suggestion as current Policy.

**Output Contract:** Return exactly one Case Adjudication; `proposed_gold_status` is only `candidate` or `none`.

**Failure Handling:** Missing or irreducibly ambiguous evidence produces an explicit unknown result, never a guessed Safe or Unsafe.

### 8.7 `policy-diagnoser` (model, DeepSeek)

**Objective:** Diagnose whether validated error patterns arise from data, Policy, Prompt, taxonomy, evidence ownership, workflow, or model bias.

**Applicable Input:** Use 5-20 validated Global Patterns, attached representative, random, and boundary evidence, current Policy excerpts, and supplied IDs.

**Procedure:** Select only closed causes, test alternative explanations, identify affected exact IDs and stages, estimate impact, and decide whether a Policy change is warranted.

**Mandatory Checks:** Cite every conclusion to supplied Pattern or case references and include at least one alternative explanation.

**Prohibitions:** Do not draft patches, edit files, approve labels, create counts, or convert uncertainty into fact.

**Output Contract:** Return one or more Diagnoses matching the supplied Schema and allocated IDs.

**Failure Handling:** Insufficient evidence returns `incomplete_context` diagnosis and no Policy-change recommendation.

### 8.8 `policy-rule-author` (model, DeepSeek)

**Objective:** Draft minimal Policy Change Proposals for validated Diagnoses.

**Applicable Input:** Use validated Diagnoses, deterministic counts and distributions, exact current Policy excerpts and hashes, Change Requests as context, Regression Contracts, allowed targets, operations, and Proposal IDs.

**Procedure:** Draft minimal normalized patch operations and explain addressed patterns, regression risks, proposed cases and contracts, and explicit non-goals.

**Mandatory Checks:** Preserve current text hashes and connect every operation to evidence, an allowed target, and at least one regression obligation.

**Prohibitions:** Do not alter authority, acceptance gates, released files, deterministic counts, or issue approval.

**Output Contract:** Return zero or more Proposals matching the supplied Schema and allocated IDs.

**Failure Handling:** If evidence does not warrant Policy change, return no Proposal and cite the Diagnosis IDs as intentionally unchanged.

### 8.9 `policy-critic` (model, Qwen)

**Objective:** Independently attack each Proposal or Direct Change Bundle for policy and regression defects.

**Applicable Input:** Use one Proposal or deterministic Direct Change Bundle, current Policy, authority metadata, relevant contracts, and attached representative and counterexample cases. Direct compile uses `proposal_id=DIRECT:<iteration-id>`.

**Procedure:** Search for loopholes, over-breadth, under-coverage, taxonomy, evidence, and case-type conflict, privacy or security risk, and regression; attempt concrete counterexamples.

**Mandatory Checks:** Check every proposed/requested change and relevant Regression Contract; use a model family disjoint from Rule Author and Resolver.

**Prohibitions:** Do not approve or release, rewrite the Proposal, suppress Human Directive conflict, or claim regression success.

**Output Contract:** Return one Critique with `accept`, `revise`, or `block`. A block stops candidate creation; every revise requirement must later be accepted or blocked by Resolver.

**Failure Handling:** Insufficient evidence cannot yield accept; return revise with the exact missing evidence or block for a safety-critical unresolved contradiction.

### 8.10 `change-resolver` (model with deterministic verifier, DeepSeek)

**Objective:** Reconcile Proposals, Critiques, and Change Requests into one normalized candidate Change Set.

**Applicable Input:** Use current Policy, Proposals or Direct Change Bundle, Critiques, Change Requests, exact authority order, allowed targets, pointers and operations, current hashes, and one Change Set ID.

**Procedure:** Propose accepted and rejected changes and blocked conflicts; map every accepted semantic decision to an exact source path and JSON Pointer.

**Mandatory Checks:** Account for every input change and every Critic revise requirement; cite authority and old value hash; preserve higher authority.

**Prohibitions:** Do not override higher authority, silently resolve equal high-authority conflict, edit files, compile prompts, change gates, or approve.

**Output Contract:** Return exactly one Change Set. Deterministic verification rechecks authority, references, hashes, operation legality, Human Directive requirements, and conflicts.

**Failure Handling:** Any unresolved blocking conflict remains in `blocked_conflicts` and prevents candidate creation.

### 8.11 `policy-prompt-compiler` (deterministic)

**Objective:** Produce a byte-stable candidate Policy Bundle from one verified Change Set.

**Applicable Input:** Use one verified Change Set, one immutable base release, fixed templates, Schemas, coverage map, and explicitly selected Approved Examples.

**Procedure:** Copy the base, apply closed patch operations, validate source Policy, and render role templates in section 9.

**Mandatory Checks:** Verify old hashes, path containment, all enabled ID coverage, exact section order, output budgets, Schema parity, and two-run byte equality.

**Prohibitions:** Do not call a model, paraphrase, semantically compress, invent rules, choose examples, change Gate Policy, or mutate the base.

**Output Contract:** Produce candidate policy, six Prompt files, four Schemas, coverage map, and compile manifest with exact hashes and sizes.

**Failure Handling:** Any patch, Schema, coverage, path, budget, or determinism failure activates no candidate and returns `compiler_invalid`.

### 8.12 `safety-regression-evaluator` (deterministic orchestration)

**Objective:** Compare base and candidate Policy Bundles against frozen regression evidence and Gate Policy.

**Applicable Input:** Use immutable base/candidate bundles, Approved/Core Gold suites, Regression Contracts, Hidden Gold, Safety Review config, and two role rotations.

**Procedure:** Run fresh Safety Review tasks for base and candidate in each rotation, parse outputs independently, compute deltas, and evaluate every Gate.

**Mandatory Checks:** Verify exact ID sets, Schemas, suite membership, no Candidate Gold truth, independent rotations, stage accounting, and contract versions.

**Prohibitions:** Do not pool rotations, trust model or workflow metrics, change thresholds, skip failures, reuse outputs, or read Gold before terminal decisions.

**Output Contract:** Produce one Regression Report and one independently computed Gate Report with all hashes and evidence references.

**Failure Handling:** Gate failure preserves all artifacts, marks `regression_failed`, and returns exit 1; missing external prerequisites block rather than pass.

### 8.13 `policy-release-manager` (deterministic)

**Objective:** Publish one approved candidate as a new immutable Policy release.

**Applicable Input:** Use the active candidate, aggregate Critic Report, passed Regression and Gate Reports, matching policy approval, release root, and target version.

**Procedure:** Verify every prerequisite and hash, copy to a sibling staging directory, sync files and directories, atomically rename once, then verify the destination.

**Mandatory Checks:** Require unique target, empty blocked conflicts, non-block Critic, all Gates passed, matching approval, path containment, and unchanged base/candidate hashes.

**Prohibitions:** Do not call a model, create approval, overwrite, delete, reuse a release, edit base/candidate, or publish partial files.

**Output Contract:** Produce immutable release manifest, changelog, aggregate release hash, and one release database row.

**Failure Handling:** Any mismatch or destination collision performs no overwrite and returns `release_conflict`, `approval_invalid`, or `artifact_corrupt` as applicable.

### 8.14 Skill Schema root matrix

Every Skill input/output root is an object with `additionalProperties:false`. Reference objects use the contracts in section 7; `*_refs` contain IDs and SHA-256 only unless explicitly payload-bearing.

| Skill | Required input properties | Required output properties |
|---|---|---|
| `audit-source-interpreter` | `iteration_ref,source_manifest,structural_profile,sample_records,policy_ref,allowed_enums,allowed_ids` | `status,candidate_mapping,suitable_analyses,unsuitable_analyses,ambiguities,sample_refs` |
| `audit-normalizer` | `iteration_ref,source_manifest,source_ref,approved_mapping` | `source_id,normalized_artifact_ref,record_ids,record_count,sha256` |
| `disagreement-miner` | `iteration_ref,record_metadata,quality_events,selection_rules` | `selected,excluded,selected_count,excluded_count` |
| `local-error-pattern-miner` | `iteration_ref,batch,policy_ref,rule_refs,historical_refs,allowed_causes,allowed_ids` | `batch_id,local_patterns,limitations` |
| `global-pattern-merger` | `iteration_ref,local_pattern_refs,aggregate_metadata,historical_refs,allowed_ids` | `global_patterns,rejected_local_patterns,limitations` |
| `case-adjudicator` | `iteration_ref,case,policy_ref,rule_refs,evidence_ownership,output_matrix,allowed_ids` | `adjudication` |
| `policy-diagnoser` | `iteration_ref,global_pattern_refs,case_refs,policy_excerpts,allowed_causes,allowed_ids` | `diagnoses,limitations` |
| `policy-rule-author` | `iteration_ref,diagnosis_refs,deterministic_metrics,policy_excerpts,change_request_refs,contract_refs,allowed_targets,allowed_operations,allowed_ids` | `proposals,unchanged_diagnosis_ids,limitations` |
| `policy-critic` | `iteration_ref,proposal_or_direct_bundle,policy_ref,authority_metadata,contract_refs,case_refs,allowed_ids` | `critique` |
| `change-resolver` | `iteration_ref,policy_ref,proposal_or_direct_bundle_refs,critique_refs,change_requests,authority_order,allowed_targets,allowed_operations,allowed_ids` | `change_set` |
| `policy-prompt-compiler` | `iteration_ref,base_release_ref,change_set_ref,template_refs,schema_refs,coverage_map_ref,approved_example_refs` | `candidate_ref,compiled_prompt_refs,schema_refs,coverage_map_ref,compile_manifest_ref` |
| `safety-regression-evaluator` | `iteration_ref,base_release_ref,candidate_ref,suite_refs,contract_refs,hidden_gold_ref,gate_policy_ref,safety_review_config_ref` | `regression_report_ref,gate_report_ref,passed` |
| `policy-release-manager` | `iteration_ref,candidate_ref,critic_report_ref,regression_report_ref,gate_report_ref,approval_ref,target_version,releases_dir` | `release_version,release_path,release_sha256,release_manifest_ref,changelog_ref` |

All arrays above are required even when empty unless their section 7 minimum is positive. `iteration_ref` is exactly `{iteration_id,semantic_hash}`; `policy_ref`/release refs are exactly `{version,sha256,artifact_id}`; generic Artifact refs are exactly `{artifact_id,sha256,artifact_type}`. `allowed_ids`, targets, operations, causes, and enums are lexical unique arrays supplied by deterministic code.

## 9. Context and prompt compilation

Model message order is exact:

1. System: fixed data-handling boundary, no chain-of-thought request, no approval authority, output-one-JSON-object requirement, followed by the Skill's `SKILL.md` content.
2. Developer: iteration objective, analysis permissions, current Policy version/hash, relevant exact Policy/card excerpts, allowed IDs/enums, prior Artifact summaries/hashes, and output Schema.
3. User: current source sample, batch, pattern group, case, Proposal, or Change inputs.

Context Builder includes whole indivisible objects or none. It never truncates strings/objects. It first removes optional historical summaries oldest-first, then deterministically splits a batch. An oversized single object becomes `context_too_large` terminal failure. The exact context package is canonical-hashed before call; payload-bearing packages are restricted artifacts.

Before the first model Skill claim, preflight calls every configured model/profile used by a model-backed Skill. The fixed user payload is `{"probe":"sendllm-policy-optimizer-preflight"}` and the fixed strict output Schema requires exactly `{"ok":true}`. Timeout/auth/model/mode/content-rejection/invalid-output failure marks that profile unusable; all required roles must retain at least one usable profile and Critic family separation. The sanitized `preflight-report.json` records profile name, family, result, latency, structured mode, and safe error category, never raw output or credentials. Preflight failure leaves zero model Skill claims and zero candidate mutation.

Compiled Safety Review prompt section order is exact:

```text
[ROLE]
[AUTHORITY_BOUNDARY]
[SCENE_AND_EVIDENCE_OWNERSHIP]
[COMMON_POLICY]
[ENABLED_RULE_INDEX]
[ROLE_SPECIFIC_RULES]
[CASE_TYPE_POLICY]
[DECISIONS]
[APPROVED_EXAMPLES]
[OUTPUT_SCHEMA]
[PROHIBITIONS]
```

Templates contain each marker exactly once and no policy prose outside markers. Judge A includes discovery guidance; Judge B includes decisive exclusions/false-positive guidance; Router includes observable feature vocabulary and category index but no full cards; Expert receives common policy plus one card at call construction; Arbiter includes final matrix and established-only restriction. Refusal reprompt includes no policy expansion.

`coverage-map.json` maps every enabled Policy/rule/condition/exclusion/decision ID to required roles. Minimum coverage: common/evidence/case/decision IDs -> A, B, Router where relevant, Expert, Arbiter; category definitions -> Router index, Expert full, Arbiter ID; required conditions/exclusions -> Expert full and Arbiter reference; method cards -> Prompt roles only. Missing or extra unknown IDs fail compilation. Approved examples are selected by explicit example IDs in source Policy, lexical order, with per-role size budgets from config. No model chooses examples at compile time.

## 10. Retry, failure, and continuation matrix

| Failure | Attempts/action | Stage/iteration effect |
|---|---|---|
| network, timeout, 408, 429, 5xx | 3 attempts per profile with persisted backoff; honor bounded Retry-After | retry_wait, then fallback |
| malformed JSON/Schema/semantic output | one repair on same profile with validation errors but no hidden payload | fallback after repair failure |
| textual refusal | one task reprompt on same profile | fallback after repeated refusal |
| provider content rejection | no obfuscation and no same-profile retry | immediate fallback |
| 401/403, unknown model, unsupported mode | circuit profile for iteration | fallback; fatal if required role has no usable profile |
| individual Local/adjudication work exhausted | persist terminal artifact | unrelated work continues; dependent candidate/release blocked |
| Global/Diagnosis/Author/Critic/Resolver exhausted | no valid downstream semantic artifact | candidate path blocked, iteration failed after independent work drains |
| mapping absent/changed source hash | no normalization attempt | awaiting_mapping_approval, exit 3 |
| invalid canonical source record | rollback that source's normalization writes | source terminal_failed; analyze returns 1 |
| context oversized indivisible object | no model call | terminal_failed; dependent path blocked |
| equal high-authority conflict | no patch/compile | candidate blocked, exit 1 |
| Critic `block` | no compile/release | candidate blocked until new proposal cycle |
| compiler/schema/hash/coverage nondeterminism | no candidate activation | iteration failed, exit 1 |
| Regression gate fail | preserve reports/candidate | regression_failed, exit 1 |
| approval hash mismatch | no release mutation | awaiting_release_approval, exit 3 |
| destination release exists | no overwrite | release failure, exit 1 |
| corrupt DB/indexed Artifact/hash | stop claims immediately | iteration failed, exit 1 |
| SIGINT/SIGTERM | stop claims, drain to timeout, recover running on reopen | interrupted, exit 130 |

Backoff is exponential from configured `initial_backoff` capped by `max_backoff`, with injected full jitter in `[0,delay]`. Tests use fake jitter. Attempt counters never reset on resume. Fallback attempts are separate rows linked by `fallback_from_run_id`.

Each HTTP response body is capped at 4 MiB through the existing OpenAI-compatible boundary. The bounded raw response is written only as a `restricted_payload` `model_response` Artifact; SQLite stores its Artifact ID and safe category, not response bytes. Repair/reprompt input contains the prior validation error and bounded prior model output only inside restricted Context, never logs.

## 11. Required deterministic acceptance fixtures

The implementation must create only synthetic committed fixtures:

- one 12-row source for adapter/provenance/authority tests;
- one generated 10,000-record test in memory for 70/20/10 and batch bounds;
- one minimal base release containing all required P04-B role/coverage IDs;
- one Change Request set containing high-authority success, lower-authority rejection, and equal-authority conflict;
- one fake regression set covering every gate pass/failure independently;
- canary strings for Prompt, Response, evidence, raw output, and API key that must be absent from public/log outputs.

Real Audit, Gold, hidden evaluation, provider outputs, DBs, candidates, and release approval are never fixtures and never committed. `feat-038` remains blocked when those external assets or explicit human actions are missing.

## 12. No remaining delegated design choices

The coding model may choose only local variable names, private helper decomposition, and equivalent standard-library algorithms that preserve byte/state/error behavior. It may not choose table/column/index names, states, transitions, CLI flags, artifact paths, enums, Schema fields, Skill executors/roles, authority, batching ratios/bounds, retry counts, compiler sections, Gate thresholds, approval ownership, or release semantics.

Provider endpoints, API key values, real Audit/Gold/Hidden paths, approver identity, and the content of future Human Directives are external runtime inputs, not implementation choices. Their absence blocks only the feature that requires them and never authorizes a substitute.
