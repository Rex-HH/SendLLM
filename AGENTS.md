# AGENTS.md

SendLLM is a local Go CLI for resumable, rate-limited LLM safety annotation of JSONL datasets.

## Advertisement Review Workflow

When implementing or reviewing the historical `advertisement-review-batch` mode, also read these files completely:

- `docs/superpowers/specs/2026-09-21-advertisement-dataset-lightweight-review-design.md`
- `docs/superpowers/plans/2026-09-21-advertisement-dataset-lightweight-review.md`
- `docs/advertisement-review-agent-harness.md`

The Advertisement Review harness is additive and stricter where it defines the original `trace_id` invariant,
model-input minimization, protected legacy files, staged acceptance, and the mandatory human gate between the real
pilot and the 58,658-row full run. `feat-039` is historical and blocked after both Flash pilots failed; do not restart
its full run or reinterpret its 265-row directed review as full coverage.

When implementing `feat-040`, also read these files completely:

- `docs/superpowers/specs/2026-09-22-advertisement-full-cleaning-design.md`
- `docs/superpowers/plans/2026-09-22-advertisement-full-cleaning.md`
- `docs/advertisement-full-cleaning-coding-model-prompt.md`

The newer full-cleaning design governs where it conflicts with the historical `feat-039` design. Implement only
`feat-040`, preserve every protected input, stop before the real pilot until the human gate is approved, and never
overwrite or merge into an existing dataset path.

## Safety Review Workflow

When implementing or reviewing the `safety-review` subcommand, also read these files completely:

- `docs/superpowers/specs/2026-09-04-safety-review-pipeline-design.md`
- `docs/superpowers/plans/2026-09-04-safety-review-pipeline-implementation.md`
- `docs/safety-review-agent-harness.md`

The Safety Review harness is additive and stricter where it defines feature scope, RED/GREEN evidence, protected legacy files, repeated concurrency checks, and real multi-model acceptance. Implement its `feat-015` through `feat-025` in dependency order, one active feature at a time.

## Policy Optimization Workflow

When implementing or reviewing the `policy-optimizer` subcommand, also read these files completely:

- `docs/superpowers/specs/2026-09-04-safety-review-pipeline-design.md`
- `docs/superpowers/specs/2026-09-07-policy-optimization-agent-design.md`
- `docs/superpowers/specs/2026-09-07-policy-optimization-implementation-contract.md`
- `docs/superpowers/plans/2026-09-07-policy-optimization-agent-implementation.md`
- `docs/policy-optimization-agent-harness.md`

Policy Optimization may start only after Safety Review `feat-025` is done. Implement `feat-026` through `feat-038` in dependency order, one active feature at a time. The frozen implementation contract governs exact CLI, state, DDL, Artifact, Schema, Skill, compiler, and failure behavior; coding agents may not redesign it. Models create candidate semantic artifacts only; deterministic code owns approved mappings, normalization, counts, patch application, Prompt compilation, regression gates, approval verification, and release. Human action owns mapping approval, Gold approval/promotion, Policy Directives, and release approval.

## Startup Workflow

Before writing code:

1. Run `pwd` and confirm this repository is the working directory.
2. Read this file completely.
3. Read `docs/superpowers/specs/2026-08-05-sendllm-design.md`.
4. Read `feature_list.json`, `progress.md`, and `session-handoff.md`.
5. Review recent commits with `git log --oneline -5` when Git is available.
6. Run `./init.sh`. If the baseline fails, record the failure before editing.
7. Select exactly one unblocked feature and mark it `in-progress`.

## Scope And Architecture

- Keep the first release a local, single-process CLI. Do not add HTTP APIs, message queues, Redis, cron jobs, or multi-instance coordination.
- Follow the package boundaries in the design document and `开发指南.md`.
- Keep `main.go` limited to flags, config loading, dependency wiring, signals, and exit status.
- Use SQLite as the durable source of truth. JSONL files are import/export formats, not the live work queue.
- Preserve unknown input fields. Use `trace_id` as the stable item ID and retain input order during export.
- Keep the model boundary OpenAI-compatible. Business logic must depend on a narrow consumer-owned interface, not an SDK type.
- Keep released policy versions immutable. Safety Review reads a verified released bundle; Policy Optimization writes only new candidate and release directories.
- Read API keys only from the configured environment variable. Never persist or log credentials.
- Never log raw `prompt`, `response`, full model output, or other dataset payloads. Persist audit payloads only in the configured SQLite state file.

### Runtime availability versus acceptance

- Missing Hidden Gold blocks only the `eval` acceptance claim and final production-readiness feature; it never blocks `validate` or model-only `run` on a new dataset.
- A normal `run` may classify and clean unlabeled data. Insufficient evidence, policy gaps, provider failures, and unresolved disagreement must be quarantined, never converted to Safe because no human reviewer is available.
- Model-generated consensus, pseudo-Gold, self-evaluation, and disagreement scores may guide diagnostics and review priority, but never become acceptance truth.
- Human review is an optional escalation and sampling channel, not a per-record prerequisite for model-only execution.

## Go Code Standard

The governing style reference is [Rex-HH/uber_go_guide_cn](https://github.com/Rex-HH/uber_go_guide_cn), version noted by that repository. Apply these executable rules:

- Run `gofmt`; use `goimports` when available. Code must pass `go vet` and project tests.
- Prefer early returns and the happy path with minimal nesting. Avoid unnecessary `else` branches.
- Handle each error once. Return errors from packages, wrap with `%w`, branch with `errors.Is`/`errors.As`, and log only at the application boundary.
- Do not use `panic` for expected failures. Only `main` decides process exit, and it exits once.
- Avoid mutable package globals and `init()`. Construct dependencies explicitly with validated `Config` values.
- Pass interfaces as values, never pointers to interfaces. Keep interfaces small, define them at consumer boundaries, and add compile-time satisfaction checks where useful.
- Store mutexes as named, non-pointer, non-embedded fields. Document the fields they protect.
- Every goroutine must have an owner, cancellation path, and tracked shutdown. Propagate `context.Context`; never fire and forget.
- Channels should be unbuffered or size one unless a larger capacity has a documented backpressure reason.
- Use `time.Time` for instants and `time.Duration` for intervals. Do not encode durations as untyped integers.
- Copy maps and slices when retaining or returning them across ownership boundaries.
- Use `defer` immediately after successfully acquiring resources that require cleanup.
- Initialize structs with field names. Avoid naked boolean parameters; use named types or config fields.
- Start `iota` enums at one unless zero is a deliberately useful default.
- Preallocate maps and slices when the final size is known or bounded and meaningful.
- Prefer table-driven tests with `give`/`want` fields. Use `t.Parallel()` only when state is isolated.
- Use standard-library testing by default. Add dependencies only when they provide a clear, maintained capability.

Local conventions and the approved design take precedence where the external guide offers alternatives. In particular, internal constructors use typed `Config` structs; functional options are reserved for genuinely extensible public APIs.

## Simplicity And Chinese Comments

以下规则是强制要求，优先级高于个人编码偏好：

- 代码首先是给人阅读的。优先选择最直接、最短且容易验证的实现，不为了展示技巧使用复杂控制流、反射、泛型框架或隐式魔法。
- 本次只交付最简可用版本。只实现设计文档明确要求的当前行为，不预先建设插件系统、通用框架、自适应策略、热更新或假设中的未来扩展。
- 代码量本身是维护负债。新增类型、接口、包或辅助层之前，必须证明它解决了当前真实边界、必要测试替换或明显重复；否则不要增加。
- 校验只覆盖外部输入、持久化状态和已确认业务不变量。不得增加多层重复校验、推测性规则或没有调用方处理方式的错误分类。
- 使用小函数、清晰命名、早返回和线性 happy path。能用标准库和普通数据结构清楚完成时，不引入新的依赖或设计模式。
- 任何会明显增加代码量或理解成本的方案，如果不是已批准设计的硬性要求，必须先向用户说明收益并获得确认。
- 所有新增 Go 代码注释必须使用中文，技术标识符可以保留英文。
- 导出标识符和包注释遵循 Go doc 格式，例如 `// Runner 负责...`、`// Package configs 提供...`，注释以被说明的名称开头。
- 注释应简洁说明“为什么”、契约、不变量、并发所有权或容易误解的边界；不要逐行翻译代码，不要用大量注释补偿复杂实现。
- 不添加记录开发过程、Agent 行为或临时思考的注释。代码本身应通过命名和结构表达大部分意图。

## Working Rules

- Work on one feature at a time, respecting dependencies in `feature_list.json`.
- Write or update tests before implementation where practical.
- Keep edits inside the active feature's scope; do not prebuild future layers.
- Update feature status and evidence as soon as verification changes, not only at session end.
- Do not mark a feature done without fresh command output from the required verification.
- Do not commit input datasets, output datasets, SQLite state, raw model responses, `.env`, or credentials.

## Verification Commands

Run the standard gate:

```bash
./init.sh
```

It checks formatting, unit/integration tests, the race detector, and `go vet` after `go.mod` exists.

## Final Live Acceptance

- 中间单元测试和集成测试使用本地模拟 OpenAI 服务，不消耗真实模型额度。
- 项目最终完成前必须读取仓库根目录的 `模型配置.md`，使用其中的真实 `base_url`、模型名和 API Key 环境变量名运行端到端测试。
- 当前真实验收固定使用 `base_url=https://aigateway.venusgroup.com.cn/ai/deepseek/openai`、`model=deepseek-v4-pro`、`api_key_env=AI_GATEWAY_API_KEY`。
- 最终端到端测试必须读取仓库根目录的 `Test_Input.jsonl`，真实调用模型，并生成 `Test_Output.jsonl`；不得以 fake server 的结果替代最终验收。
- 完成证据必须包括：CLI 退出码为 `0`、输入输出均为 50 条、`trace_id` 集合完全一致且无重复、每条输出通过正式结果 Schema 校验、失败记录数为 0。
- 不得在命令、日志、报告或提交中打印 `AI_GATEWAY_API_KEY` 的值。只检查环境变量是否非空。

## Definition Of Done

A feature is complete only when:

- Its behavior and failure cases match the approved design.
- The implementation is the smallest clear solution for the current feature and contains no speculative abstraction or duplicate validation.
- All added Go comments are necessary, clear, Go doc compliant where applicable, and written in Chinese.
- Relevant tests exist and pass.
- The real-model end-to-end acceptance in Final Live Acceptance passes; simulated-provider tests alone are insufficient for project completion.
- `./init.sh` passes from the repository root.
- No sensitive payload or credential appears in logs, fixtures, or Git changes.
- `feature_list.json`, `progress.md`, and `session-handoff.md` contain current evidence and the next action.
- The repository can be resumed by another agent using the Startup Workflow.

## End Of Session

1. Stop or drain all long-running commands.
2. Run the narrow relevant tests, then `./init.sh` when the codebase is runnable.
3. Update `feature_list.json` with status and exact verification evidence.
4. Update `progress.md` and `session-handoff.md` with files changed, decisions, risks, and next step.
5. Review `git diff` for scope drift and sensitive data.
6. Commit only when the work is coherent and verified.

## Escalation

- If a requirement contradicts the approved design, stop and ask the user before expanding scope.
- If the same verification failure persists after three evidence-based attempts, record it as a blocker with exact output.
- If a provider behavior is undocumented, preserve the raw failure in SQLite, use a fake-server regression test, and avoid guessing a protocol contract.
