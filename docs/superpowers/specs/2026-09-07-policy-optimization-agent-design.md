# SendLLM Policy Optimization Agent Design

Date: 2026-09-07
Status: approved for implementation after Safety Review `feat-025`
Initial scope: P04-B policy optimization

## 1. Purpose and system boundary

Policy Optimization is a separate local, single-process workflow that improves Safety Review policy assets. It analyzes heterogeneous evidence, discovers recurring errors, proposes rule changes, compiles candidate prompts, runs regression, and prepares immutable releases for human approval.

It does not label production rows, modify a running Safety Review task, edit released policy in place, approve its own proposal, or publish without regression and explicit human approval. Models produce candidate artifacts only. SQLite, versioned files, and iteration state provide continuity; model conversation memory is never authoritative.

The two production boundaries are:

```text
Policy Optimization Agent --approved immutable bundle--> Safety Review
Safety Review --sanitized quality events + declared source refs--> Audit Package
```

Both live in the `sendllm` binary but use independent subcommands, configs, tables, task directories, Go files, and lifecycle owners.

## 2. Architecture

```text
Audit Package / Change Requests / Released Policy
                       |
                       v
        Source Interpreter + deterministic Normalizer
                       |
                canonical Audit Records
                       |
             deterministic Stratifier
                       |
          Local Error Pattern Miners (30-100 rows)
                       |
             Global Pattern Merger
                       |
          representative/raw-case validation
                       |
       Case Adjudicator + Policy Diagnoser
                       |
        Rule Author -> Independent Critic
                       |
                 Change Resolver
                       |
        deterministic Policy/Prompt Compiler
                       |
        Regression Evaluator and Diff Report
                       |
               Human Approval Artifact
                       |
              immutable Release Manager
```

Components:

- `Workflow Engine`: explicit stage graph for full analysis, data-plus-change, or direct compile modes.
- `Generic Skill Runtime`: loads provider-neutral skill instructions and Schemas, builds bounded context, invokes configured OpenAI-compatible models, validates results, and persists attempts.
- `Context Builder`: selects only current-stage policy, cards, artifacts, examples, input batch, permissions, and Schema.
- `Optimization Store`: durable iteration, source, record, batch, skill-run, artifact, candidate, regression, approval, and release state.
- `Artifact Store`: immutable files addressed by relative path and SHA-256.
- `Regression Runner`: evaluates old and candidate bundles over versioned regression suites and independently computes diffs/gates.
- `Release Manager`: verifies candidate, regression, critic, approval, and hashes before publishing a new version directory.

## 3. CLI and modes

All commands use the existing binary:

```bash
sendllm policy-optimizer validate --config policy-optimizer.yaml
sendllm policy-optimizer inspect --package audit-round-007 --config policy-optimizer.yaml
sendllm policy-optimizer mapping-approve --package audit-round-007 --source sft-v3-vs-gold --mapping mappings/sft-v3.candidate.yaml --approver USER_ID --config policy-optimizer.yaml
sendllm policy-optimizer analyze --package audit-round-007 --config policy-optimizer.yaml
sendllm policy-optimizer compile --policy p04b-v1.0 --change-request CR-007.yaml --config policy-optimizer.yaml
sendllm policy-optimizer compile --policy p04b-v1.0 --change-request CR-007.yaml --change-request CR-008.yaml --preview --config policy-optimizer.yaml
sendllm policy-optimizer regression --candidate p04b-v1.1-candidate.1 --config policy-optimizer.yaml
sendllm policy-optimizer gold-approve --candidate-gold iterations/ITER-007/gold/candidates.jsonl --approver USER_ID --config policy-optimizer.yaml
sendllm policy-optimizer gold-promote --approved-gold policy-optimization/regression/gold/approved.jsonl --approver USER_ID --config policy-optimizer.yaml
sendllm policy-optimizer approve --candidate p04b-v1.1-candidate.1 --approver USER_ID --config policy-optimizer.yaml
sendllm policy-optimizer release --candidate p04b-v1.1-candidate.1 --config policy-optimizer.yaml
sendllm policy-optimizer status --iteration-dir iterations/ITER-007
sendllm policy-optimizer status --iteration-dir iterations/ITER-007 --watch
```

Modes:

- `analyze` (A): sources -> interpretation/normalization -> stratification -> local mining -> global merge -> adjudication/diagnosis -> proposal -> critic -> resolution -> compile -> regression. A passing run stops at `awaiting_release_approval`; a failing run stops at `regression_failed` with artifacts preserved.
- `analyze` with Change Requests (B): same complete flow, plus requests enter Change Resolver with mined proposals before compilation and regression.
- `compile` (C): current policy + one or more Change Requests -> independent Critic review of the requested change bundle -> resolution -> candidate policy -> prompt compile. It skips mining, adjudication, diagnosis, and Rule Authoring by design, but does not skip independent criticism.
- `compile --preview`: creates a candidate and compiled prompt preview but cannot mark regression passed, approve, or release.
- `regression`: mandatory before any release regardless of mode. It is invoked automatically by `analyze` and is also an explicit resumable command for direct-compile candidates or a rerun against unchanged candidate bytes.

All mutating workflow commands are resumable. `mapping-approve`, `gold-approve`, `gold-promote`, and `approve` are explicit local human actions that create hash-bound approval artifacts; `analyze`, `compile`, and `regression` never invoke them internally. `status` is read-only. Exit codes are 0 success, 1 fatal/config/gate failure, 3 awaiting human approval, and 130 interruption. Regression gate failure returns 1 while preserving artifacts.

## 4. Directory and immutable artifacts

```text
policy-optimization/
  skills/<skill-id>/SKILL.md
  skills/<skill-id>/input.schema.json
  skills/<skill-id>/output.schema.json
  skills/<skill-id>/examples/*.json
  schemas/*.json
  regression/contracts/*.yaml
policy/releases/p04b-v1.0/
  release.yaml
  policy/**
  prompts/**
  schemas/**
iterations/ITER-007/
  state.db
  manifest.yaml
  sources/
  mappings/
  normalized/
  batches/
  local_patterns/
  global_patterns/
  adjudications/
  diagnoses/
  proposals/
  critic/
  change_requests/
  resolved_changes/
  candidate_policy/
  candidate_prompts/
  regression/
  approvals/
  release/
  summary.md
```

`iterations/`, Audit Packages, mappings containing local paths, normalized records, model artifacts, and generated candidates are local and Git-ignored. Released policy, generic skills, Schemas, regression contracts, and minimal approved regression examples are version-controlled.

Every artifact row and manifest entry records relative path, media type, artifact type, SHA-256, byte size, producer stage/skill/model profile, parent artifact hashes, creation time, and sensitivity (`public_policy`, `review_metadata`, or `restricted_payload`). Existing artifacts are never overwritten; reruns create a new attempt artifact and update the stage's active reference transactionally.

## 5. Three state layers

Runtime call context contains only iteration ID, stage/skill, batch or case ID, policy version, task objective, and cancellation/deadline. It is not durable after the call.

Workflow state in SQLite records current stage, source/record/batch completion, local/global pattern counts, proposal/critic/resolution status, regression status, approvals, release status, retry counters, and next-attempt times.

Semantic artifacts are immutable files: patterns, proposals, decisions, candidate policy, compiled prompts, regression reports, summaries, and Change Requests. A later stage reads artifact references, not chat history or every earlier model response.

## 6. Generic Skill Runtime

### 6.1 Required skills

The V1 closed skill set is:

1. `audit-source-interpreter`
2. `audit-normalizer`
3. `disagreement-miner`
4. `local-error-pattern-miner`
5. `global-pattern-merger`
6. `case-adjudicator`
7. `policy-diagnoser`
8. `policy-rule-author`
9. `policy-critic`
10. `change-resolver`
11. `policy-prompt-compiler`
12. `safety-regression-evaluator`
13. `policy-release-manager`

The runtime does not use Codex-native Skill behavior. Each directory contains required `SKILL.md`, `input.schema.json`, and `output.schema.json`, plus an optional `examples/` directory. `SKILL.md` has required headings: Objective, Applicable Input, Procedure, Mandatory Checks, Prohibitions, Output Contract, Failure Handling. Unknown files are ignored and reported; missing required files, duplicate IDs, or unknown manifest skills fail validation.

The manifest fixes each Skill's executor. This is not selected dynamically by a model:

| Skill | Executor | Authority boundary |
|---|---|---|
| `audit-source-interpreter` | model | recommends a mapping only |
| `audit-normalizer` | deterministic | applies only an approved mapping |
| `disagreement-miner` | deterministic | selects records from declared metadata and quality events |
| `local-error-pattern-miner` | model | emits candidate Local Patterns |
| `global-pattern-merger` | model plus deterministic verifier | proposes merges; code recomputes membership and counts |
| `case-adjudicator` | model | emits candidate adjudications, never Gold approval |
| `policy-diagnoser` | model | emits candidate diagnoses |
| `policy-rule-author` | model | emits proposals only |
| `policy-critic` | model | emits independent critique only |
| `change-resolver` | model plus deterministic authority validator | proposes a Change Set; code enforces authority and references |
| `policy-prompt-compiler` | deterministic | applies approved normalized changes and renders prompts |
| `safety-regression-evaluator` | deterministic orchestration | invokes frozen Safety Review runs and recomputes gates |
| `policy-release-manager` | deterministic | verifies approval/hashes and atomically publishes |

Model-backed Skills pass through Context Builder and the model call policy. Deterministic Skills use the same input/output Schemas, artifact indexing, idempotency hashes, and audit trail but do not make an HTTP request. The runtime has no reflection, dynamic Go plugins, user-provided executables, or provider-native Skill dependency.

### 6.2 Context package

Before each model call, Context Builder creates this logical package:

```json
{
  "skill_id": "local-error-pattern-miner",
  "skill_version": 1,
  "iteration_id": "ITER-007",
  "task": {"objective": "...", "analysis_permissions": ["rule_problem"]},
  "policy": {"version": "p04b-v1.0", "relevant_rule_cards": []},
  "history": {"artifact_refs": [], "summaries": []},
  "input": {"batch_id": "B-001", "records": []},
  "output_schema": {}
}
```

The actual request uses: system = generic safety/data-handling boundary + Skill instructions; developer = current task, policy snippets, permissions, and Schema; user = current batch/case. Context Builder enforces configured byte/token limits, records included artifact hashes, and rejects overflow. It never silently truncates a record, rule card, Change Request, or Schema. A batch is split deterministically; an indivisible oversized case becomes a persisted failure.

Model output must be one JSON object matching the skill output Schema and local semantic rules. No free-form chain-of-thought is requested or persisted. Calls use the same retry/fallback/content-rejection policy as Safety Review through a new adapter around `service.Completer`; optimization business code does not depend on SDK/provider types.

### 6.3 Model roles

Configuration requires explicit profiles and these bindings:

| Work | Initial family | Constraint |
|---|---|---|
| source interpretation, local mining | MiniMax | separate per-skill limits |
| global merge | Qwen | does not receive all raw rows |
| case adjudication, diagnosis, rule author, change resolver | DeepSeek | candidate decisions only |
| independent critic | Qwen | family must differ from rule author/resolver |

Normalization, stratification, prompt compilation, metric calculation, gate evaluation, approval verification, and release copying are deterministic code. Models may recommend mappings or prompt wording, but cannot perform those authoritative operations.

## 7. Audit Package and provenance

An Audit Package is a directory with strict `manifest.yaml`, one or more source files, optional approved mappings, and Change Requests. External data may be CSV, JSONL, or Markdown. Arbitrary binary formats are rejected in V1.

```yaml
version: 1
package_id: audit-round-007
objective:
  - discover_model_bias
  - discover_policy_gap
current_policy_version: p04b-v1.0
analysis_permissions:
  - label_error
  - rule_problem
  - prompt_problem
  - taxonomy_problem
  - router_problem
  - expert_problem
  - evidence_ownership_problem
  - workflow_problem
sources:
  - source_id: sft-v3-vs-gold
    type: sft_vs_gold
    path: sources/sft-v3.jsonl
    format: jsonl
    selection_reason: model_disagreement
    expected_records: 843
    mapping: mappings/sft-v3.yaml
    trust:
      gold_label: approved_gold
      model_label: candidate
change_requests: []
```

Allowed source types are `safety_review_disagreement`, `sft_vs_judge`, `sft_vs_gold`, `policy_version_diff`, `human_selected`, `third_party_model`, `external_benchmark`, `historical_error`, `random_sample`, and `custom`. Analysis permissions are a closed set. Paths must stay inside the package and hashes/counts are verified before processing.

### 7.1 Source interpretation and mapping approval

`inspect` runs deterministic file inspection and `audit-source-interpreter` for unknown/custom layouts. It produces a candidate mapping report describing fields, model/human provenance, comparison relation, trust, suitable analyses, unsuitable uses, and ambiguities. It never normalizes unknown input automatically.

Normalization requires a checked `mapping.yaml` with `status: approved`, `approved_by`, `approved_at`, source SHA-256, and explicit field paths. If mapping is absent or source hash changed, `analyze` stops with an approval-needed error. This prevents a model from assigning trust or label semantics on its own.

The approved Mapping uses this fixed shape:

```yaml
version: 1
source_id: sft-v3-vs-gold
source_sha256: "<64 lowercase hex characters>"
format: jsonl
status: approved
approved_by: USER_ID
approved_at: 2026-09-07T10:00:00+08:00
record_selector: each_record
fields:
  sample_id: /id
  scene: /scene
  prompt: /prompt
  response: /response
  source_task: /task
judgments:
  - actor_type: gold
    actor_id: gold-v4
    authority: approved_gold
    label: /gold/label
    risk_types: /gold/risk_types
  - actor_type: model
    actor_id: sft-v3
    authority: candidate
    label: /model/label
    risk_types: /model/risk_types
comparison:
  left: 0
  right: 1
  type: label_mismatch
```

For JSONL, field paths are RFC 6901 JSON Pointers evaluated against one line. For CSV and Markdown, field paths are exact case-sensitive column names; Markdown V1 accepts only one GitHub-style table with one record per body row, not arbitrary prose. `record_selector` is fixed to `each_record` in V1. Literal defaults, transformations, expressions, scripts, recursive selectors, and model-computed fields are forbidden. Missing mapped values fail that record; missing required mapping entries fail the source before any record is written. `mapping-approve` copies a candidate Mapping, injects the human approval fields and current source hash, validates it, and writes a new immutable approved Mapping; it never edits the candidate in place.

### 7.2 Canonical Audit Record

```json
{
  "record_id": "AR-000001",
  "sample_id": "source-stable-id",
  "source_id": "sft-v3-vs-gold",
  "source_type": "sft_vs_gold",
  "source_task": "sft-v3-eval",
  "selection_reason": "model_disagreement",
  "scene": "response",
  "prompt": "...",
  "response": "...",
  "judgments": [
    {"actor_type": "gold", "actor_id": "gold-v4", "label": "safe", "risk_types": [], "authority": "approved_gold"},
    {"actor_type": "model", "actor_id": "sft-v3", "label": "unsafe", "risk_types": ["ethnic_discrimination"], "authority": "candidate"}
  ],
  "policy_version": "p04b-v1.0",
  "comparison": {"left": 0, "right": 1, "type": "label_mismatch"},
  "metadata": {}
}
```

Required fields are record/sample/source IDs, source type, selection reason, scene, judgments, policy version, comparison, and provenance. Prompt/Response may be empty only when the declared analysis does not require payload inspection. Unknown source fields are preserved under `metadata.source_fields`. Every judgment carries actor, authority, and source reference; `candidate` judgments can never be treated as Gold.

## 8. Gold and regression lifecycle

Gold creation is model-assisted and human-approved:

```text
Top model initial label -> Candidate Gold -> human review -> Approved Gold
-> repeated stable use -> Core Gold
```

Candidate Gold is never used as acceptance truth. Approval records reviewer, policy version, decision/rule IDs, source hash, timestamp, and optional correction. Promotion from Approved to Core requires at least three successful regression releases, no unresolved challenge, and explicit human promotion approval; elapsed use alone is insufficient.

Regression suites maintain these strata independently: Core Gold, Approved Gold, Typical Safe, Typical Unsafe, Hard Negative, Borderline, Previous Error Cases, and deterministic Random Sample. A record can belong to multiple named suites but is counted once per gate population.

Every resolved error pattern creates a versioned Regression Contract, for example:

```yaml
contract_id: DISCRIMINATION-CONTRACT-001
policy_scope: p04b
description: 讨论现实歧视的 Hard Negative 必须保持 Safe
suite_query:
  tags: [discussion_of_discrimination, hard_negative]
minimum_cases: 10
expected:
  label: safe
  pass_count: 10
  false_unsafe: 0
```

Contracts are immutable per version. Changing an expectation creates a new contract version and requires a Human Directive referencing the superseded contract.

## 9. Stratification and large-scale analysis

The deterministic stratifier groups by scene, source, candidate risk, old-to-new label change, disagreement type, and Expert category. It produces three non-overlapping assignment streams using a stable SHA-256 seed from iteration ID:

- 70% homogeneous strata preserving dominant category/source patterns.
- 20% cross-label/conflict batches intentionally mixing confusable categories and disagreement types.
- 10% random-mix batches across the remaining population.

Percentages are configurable integers totaling 100. Each record is assigned once to primary mining; representative validation may reference it later. Batch size defaults to 50, minimum 30, maximum 100. A final remainder under 30 is merged with the nearest compatible batch; datasets under 30 form one batch.

Local miners only identify patterns, root-cause hypotheses, covered record IDs, representative candidates, and suspected counterexamples. They cannot change policy. Each Local Pattern cites at least two records unless marked `singleton_candidate`.

Global merge reads Local Pattern artifacts, not all raw rows. It clusters patterns and computes counts in deterministic code. Each Global Pattern must then attach 3-10 representative cases, 2-5 deterministic random cases, and all known boundary/counterexample cases up to a configured cap of 10. Case payloads are loaded only for validation/adjudication calls.

Policy diagnosis processes 5-20 Global Patterns per call. Allowed diagnosis causes are `source_label_error`, `rule_missing`, `rule_too_broad`, `rule_too_narrow`, `taxonomy_conflict`, `prompt_problem`, `router_problem`, `expert_problem`, `evidence_ownership_problem`, `workflow_problem`, `incomplete_context`, and `sft_model_bias`.

Batch analysis discovers patterns and statistics. Single-case adjudication handles borderline cases, human disagreement, policy-defining examples, critic counterexamples, and unresolved cases. No model call receives 2,000-10,000 raw records.

## 10. Pattern and diagnosis contracts

Local Pattern requires ID, title, hypothesis, cause candidates, covered record IDs, representative IDs, counterexample IDs, source/category distribution, confidence (`candidate` only), and limitations.

Global Pattern requires ID, merged Local IDs, normalized description, deterministic coverage count/batch count/source/model distributions, representative/random/boundary case references, historical pattern references, and merge rationale.

Case Adjudication requires case ID, applicable policy/rules, evidence ownership, condition/exclusion matrix, current-policy result, ambiguity, proposed Gold status, and concise rationale. It cannot approve Gold.

Diagnosis requires Global Pattern IDs, one or more allowed causes, affected rules/prompts/workflow stages, evidence references, estimated impact, whether policy change is warranted, and alternative explanations.

Every ID/reference is locally validated. Models cannot invent record IDs, counts, source distributions, current rule text, or policy versions; deterministic code supplies and verifies them.

## 11. Change Requests and authority

Change Requests are first-class inputs independent of mined data:

```yaml
version: 1
change_id: CR-2026-009
source:
  type: human
  actor_id: user-001
authority: human_directive
targets: [discrimination_policy, expert_prompt]
requested_changes:
  - "Response 不能自动继承 Prompt 中的歧视风险。"
rationale: "发现多条 Prompt 风险向 Response 误传案例。"
evidence_refs: []
created_at: 2026-09-07T10:00:00+08:00
```

Allowed source types: `human`, `security_team`, `automated_proposal`, `third_party_model`, `external_research`. Authority order is `human_directive` = `security_team_decision` > `approved_gold_evidence` > `automated_proposal` = `third_party_proposal` > `observation`.

Higher authority is not silently overridden. If two equal high-authority requests conflict, or a directive violates Schema, creates an internal policy contradiction, or cannot be implemented, Change Resolver marks the candidate blocked with exact conflict references. Lower-authority suggestions may be rejected with reasons.

## 12. Proposal, critic, and change resolution

Policy Rule Author produces a Proposal, never edits files. It includes problem, affected count/source distribution, current rule excerpts by hash, proposed normalized rule diff, addressed Global/Error Patterns, regression risks, new regression cases/contracts, and explicit non-goals.

Independent Critic must use a different model family from Rule Author and Change Resolver. It receives current policy, proposal, representative/counterexample cases, authority metadata, and relevant contracts. It searches for loopholes, over-breadth, under-coverage, taxonomy conflict, evidence-ownership violation, contradictory case types, security/privacy issues, and regressions. It emits `accept`, `revise`, or `block`, plus counterexamples and required changes. `accept` is not human approval.

Change Resolver consumes current policy, proposals, critic results, and Change Requests. It applies authority order, rejects conflicts, maps accepted changes to exact policy paths/IDs, and emits a normalized Change Set:

```json
{
  "base_version": "p04b-v1.0",
  "candidate_version": "p04b-v1.1-candidate.1",
  "accepted_changes": [],
  "rejected_changes": [],
  "blocked_conflicts": [],
  "affected_artifacts": [],
  "required_regression_contracts": []
}
```

No candidate is compiled with a blocked conflict. The resolver cannot edit released files or approve the result.

## 13. Policy and prompt compilation

Policy is the source of truth. Candidate policy is produced by a deterministic patch application over a copied base release, writing to a new candidate directory. Patch operations are closed: add/replace/remove a YAML field by exact JSON Pointer, add a new versioned card, add a decision, or add a regression contract. Removing a required condition or exclusion requires a Human Directive.

Prompt Compiler is deterministic code. It combines common policy, Rule Cards, Case Type Policy, Evidence Ownership, role definition, role Schema, and selected approved examples into Judge A, Judge B, Router, Expert, and Arbiter prompts. It can organize, render, deduplicate, and enforce size budgets; it cannot add conditions, reinterpret text, or widen/narrow policy.

Source role templates contain fixed section markers. Compilation fails if any enabled rule/condition/exclusion/decision ID is absent from the role's required coverage map, any unrecognized policy sentence appears outside source assets, output exceeds the configured role budget, or two runs over identical inputs produce different bytes. The compiler emits `compile-manifest.json` with all source and output hashes plus compiler version.

If a model proposes compressed wording, it is only a Change Request/Proposal to source policy. It is never inserted directly by Compiler.

## 14. Regression and comparison gates

Regression runs the same frozen evaluation harness against base and candidate versions in fresh task directories. It reports fixed totals and per-suite changes:

- fixed, unchanged-correct, and newly wrong counts;
- Safe -> Unsafe and Unsafe -> Safe;
- Borderline and quarantine changes;
- rule/category/primary-risk changes;
- model-stage failure/fallback changes;
- every Regression Contract result.

P04-B V1 candidate gates are:

1. Core Safe false Unsafe = 0.
2. Hard Negative false Unsafe = 0.
3. No existing Regression Contract regression.
4. Hidden Unsafe Gold resolved Safe = 0, resolved Unsafe >=18/20, quarantine <=2/20.
5. Hidden Safe/Hard-Negative false Unsafe = 0.
6. Total hidden quarantine <=12/50.
7. Every targeted Error Pattern improves or remains unchanged; a proposal claiming a fix must improve its named contract.
8. No new policy/schema/compiler integrity error and no unaccounted stage failure.
9. Both A/B role rotations pass independently.

Threshold changes require a Human Directive and a new gate-policy version; a candidate cannot change its own acceptance threshold. Regression metrics are recomputed by an independent reader, not trusted from model output.

## 15. Version and release

Versions are immutable directories: `p04b-v1.0`, `p04b-v1.1-candidate.1`, and `p04b-v1.1`. A candidate records exactly one base release and one Change Set. Released directories cannot be overwritten, deleted, or reused by the CLI.

`approve` creates an approval artifact containing candidate hash, regression report hash, critic report hash, approver ID, timestamp, decision `approved`, and optional note. This is an explicit local human action; models and automated workflows cannot call approval internally. Any candidate/regression change invalidates approval by hash mismatch.

`release` requires: candidate validation, no blocked Change Set conflict, critic not `block`, all mandatory regression gates passed, matching approval, unique target version, and clean atomic destination creation. It copies artifacts to a staging directory, verifies every hash, syncs, renames, and writes a changelog/release manifest. It never rewrites the base or candidate.

Automatic operations are analysis, mining, proposal, compile, critic, and regression. Human-owned operations are source mapping approval, Gold approval/promotion, Policy Directive, candidate release approval, and resolving conflicting high-authority directives.

## 16. Durable SQLite state

The dedicated database uses `optimization_*` tables only:

- `optimization_iterations`: iteration ID, mode, base policy, status, config/semantic hashes, objective, permissions, timestamps, fatal error.
- `optimization_sources`: source ID/type/path/hash/format/count, trust JSON, mapping status/hash, normalization state.
- `optimization_records`: canonical Audit Record identity, source/sample IDs, scene, payload artifact reference, provenance/comparison JSON, state.
- `optimization_batches`: stratum/mix type, ordered record refs, state, attempt counters.
- `optimization_skill_runs`: stage/skill/input-context hashes, model profile/family, state, retry/fallback data, output artifact, safe errors/tokens/times.
- `optimization_artifacts`: immutable artifact metadata and parent hashes.
- `optimization_patterns`: local/global IDs, status, artifact hash, deterministic counts.
- `optimization_candidates`: base/candidate versions, Change Set/policy/prompt/critic hashes and status.
- `optimization_regressions`: candidate, suite/gate versions, state, report hash, aggregate result.
- `optimization_approvals`: candidate/regression/critic hashes, approver, decision, artifact hash.
- `optimization_releases`: version, candidate/base, release hash/path/time.

Each stage transition and artifact-index update is transactional. A file is written/synced first under a temporary name, then the database transaction records its final hash/path after atomic rename. On resume, `running` skill runs return to `pending`; succeeded input-hash-matched runs are not repeated. Changed source/policy/skill/Schema/config semantic hashes require a new iteration. Runtime limits and status intervals may change on resume.

Iteration status is one of `created`, `awaiting_mapping_approval`, `running`, `no_change`, `preview_ready`, `awaiting_regression`, `regression_failed`, `awaiting_release_approval`, `release_ready`, `released`, `interrupted`, or `failed`. A stage run is `pending`, `running`, `retry_wait`, `succeeded`, or `terminal_failed`. `terminal_failed` is evidence, never silently converted to success. A command may move only along the mode-specific graph in section 3; reopening a terminal iteration is rejected, while `interrupted` and nonterminal waiting states are resumable when semantic hashes still match. `no_change` and `preview_ready` are successful terminal iterations; Preview cannot be promoted in place.

## 17. Scheduling, failure, and safety

Independent bounded pools exist for source interpretation, local mining, global merge, adjudication, diagnosis/authoring, critic, and model-assisted resolution. Deterministic stages run synchronously through the workflow owner. Per-skill and shared model-profile RPM/TPM/concurrency limits apply; downstream validation work has priority over new mining when sharing quota.

Network/timeouts/408/429/5xx receive three persisted attempts per profile; malformed structured output receives one repair; textual refusal receives one task re-prompt; provider content rejection advances fallback without obfuscation; auth/model/mode errors circuit the profile. Exhausted individual batches/cases become terminal artifacts and block dependent candidate/release stages, but unrelated batches continue. Corrupt/unwritable state, invalid immutable artifacts, or failed required preflight is iteration-fatal.

Logs/status contain IDs, stages, counts, rates, hashes, model profile names, safe error categories, and aggregate metrics. They never contain Audit Record Prompt/Response, model raw output, evidence text, source payload, or API keys. Restricted artifacts and state are Git-ignored.

## 18. Configuration

Policy Optimization uses a separate strict YAML config. Required sections are `iteration`, `policy`, `skills`, `models`, `batching`, `context`, `retry`, `regression`, and `output`. Unknown fields fail.

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
  profiles: {}
  roles: {}
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
  gate_policy: p04b-gate-v1
output:
  status_interval: 5s
  shutdown_timeout: 30s
```

The model profile/role schema reuses the Safety Review shape but has the bindings in section 6.3. Multiple profiles may share one API-key environment variable. Real secret values are read only during client wiring and excluded from config snapshots, state, artifacts, logs, and fingerprints.

## 19. Acceptance

V1 is complete only when synthetic/fake integration and one real P04-B optimization iteration prove:

- CSV, JSONL, and Markdown sources normalize to exact canonical records with approved provenance mappings.
- Unknown/custom input cannot normalize before mapping approval.
- 10,000 synthetic records are deterministically assigned once at 70/20/10 and batches stay 30-100 except a dataset smaller than 30.
- Interrupted local mining resumes without repeating succeeded skill runs.
- Global counts are independently recomputed and every Global Pattern has required representative/random/counterexample attachments.
- Direct compile mode skips mining but cannot release before regression.
- Human Directive authority and equal-authority conflict blocking behave exactly as specified.
- Critic family differs from author/resolver and a critic `block` prevents release.
- Two identical compilations are byte-for-byte equal and cover every enabled policy ID.
- Candidate changes never modify base/released files.
- Core Safe and Hard Negative false Unsafe remain zero; all P04-B regression and hidden gates pass.
- Changing candidate or regression bytes invalidates approval.
- Release is atomic, immutable, hash-valid, and consumable by a fresh Safety Review validate run.
- No payload or secret canary appears in logs, status, public reports, or Git changes.

Real optimization acceptance uses human-approved local Audit/Gold assets and configured models. If these or credentials are absent, final features are blocked, not skipped.

## 20. Code isolation

New Go files use `policy_optimizer_*.go`. Put CLI parsing in `internal/api/cli`, workflow/business logic in `internal/service`, state in `internal/dao`, contracts in `internal/dto`, strict config under `internal/lib/configs`, and quota helpers under `internal/lib/limiter`. Do not append to Safety Review or legacy runner files.

The only later existing production-file change is a minimal `main.go` dispatch for `policy-optimizer`, after Safety Review dispatch exists. Any shared helper must be consumed through an existing stable interface or implemented in a new adapter. No new dependency, dynamic Go plugin, external queue, HTTP service, or multi-process coordination is authorized.

Exact implementation behavior is frozen in `docs/superpowers/specs/2026-09-07-policy-optimization-implementation-contract.md`. Its CLI matrix, version grammar, state graphs, Artifact paths, SQL DDL, external/Skill contracts, deterministic IDs and stratification, Skill instructions, Prompt compiler order, retry/failure matrix, and acceptance fixtures are normative and override less-specific examples in this design.
