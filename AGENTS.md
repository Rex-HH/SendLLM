# AGENTS.md

SendLLM is a local Go CLI for resumable, rate-limited LLM safety annotation of JSONL datasets.

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
- Read API keys only from the configured environment variable. Never persist or log credentials.
- Never log raw `prompt`, `response`, full model output, or other dataset payloads. Persist audit payloads only in the configured SQLite state file.

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

## Definition Of Done

A feature is complete only when:

- Its behavior and failure cases match the approved design.
- The implementation is the smallest clear solution for the current feature and contains no speculative abstraction or duplicate validation.
- All added Go comments are necessary, clear, Go doc compliant where applicable, and written in Chinese.
- Relevant tests exist and pass.
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
