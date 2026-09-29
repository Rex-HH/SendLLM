# 给 Policy Optimization 代码生成模型的启动提示词

你负责在 `/Users/lijiayang/venus/SendLLM` 中实现已经批准的 `policy-optimizer` 子命令。直接在仓库工作，不要只输出方案、伪代码或建议。

每轮只完成 `feature_list.json` 中依赖已满足的第一个 Policy Optimization 功能。只有 `feat-025=done` 后才允许开始 `feat-026`。若已有一个优化功能是 `in-progress`，恢复它；否则选择第一个 pending 功能。完成该功能的全部验证、状态记录和交接后停止，绝不提前实现下一功能。

## 权威资料

冲突时按以下顺序执行：

1. 用户当前明确要求。
2. `AGENTS.md`。
3. `docs/superpowers/specs/2026-09-04-safety-review-pipeline-design.md`。
4. `docs/superpowers/specs/2026-09-07-policy-optimization-agent-design.md`。
5. `docs/superpowers/specs/2026-09-07-policy-optimization-implementation-contract.md`。
6. `docs/policy-optimization-agent-harness.md`。
7. `docs/superpowers/plans/2026-09-07-policy-optimization-agent-implementation.md` 中当前功能的 Task。
8. 现有代码惯例。

必须完整阅读这些文件，不得依赖聊天摘要或自行改变状态、权限、Schema、模型职责、文件边界、阈值、重试、目录和验收。如果存在无法同时满足的实质冲突，停止编辑，报告精确文件/章节/冲突，不要自行选解释。

Implementation Contract 已冻结 CLI 参数、状态机、DDL、Artifact 路径、Schema 字段、13 个 Skill、Context、Compiler 和失败决策。不得重命名、删减、扩展或用“更通用”的实现替代；只有局部变量、私有小函数拆分及保持完全相同行为的标准库算法可自行决定。

## 启动顺序

1. 运行 `pwd`，确认是 `/Users/lijiayang/venus/SendLLM`。
2. 完整阅读上述资料，再读基础设计、`开发指南.md`、`feature_list.json`、`progress.md`、`session-handoff.md`。
3. 运行 `git status --short`、`git log --oneline -5`。所有开始前的改动均视为用户所有，不回退、不整理、不提交无关内容。
4. 确认 `feat-025=done`；否则停止并说明尚未满足依赖。
5. 运行 `./init.sh` 并记录真实退出码。基线失败先归因并写入状态，不能伪装成本轮问题或忽略。
6. 运行已存在的 `scripts/verify-policy-optimizer-scope.sh`；失败时停止，不改脚本绕过。
7. 选择唯一功能，标记 `in-progress`，立刻在 `progress.md` 写基线、范围和下一条 RED 命令。
8. 先写当前 Task 指定的失败测试并运行，确认因目标行为缺失而 RED。

## 不可越过的边界

- 所有新增 Go 文件名以 `policy_optimizer_` 开头；只修改实施计划当前 Task 列出的文件。
- 仅 `feat-036` 可以给 `main.go` 增加最小 `policy-optimizer` 分发分支；不重构旧分支。
- 不向 Safety Review、legacy Runner、reconcile、adjudicate、label-review 或旧 DTO/DAO/config/facade 文件追加优化逻辑。
- 仅通过不变的稳定接口复用 `service.Completer`、completion DTO、`facade.NewOpenAI` 和兼容工具。复用需要修改旧接口时先停下请求批准。
- 不新增依赖、HTTP 服务、队列、Redis、cron、动态插件、供应商原生 Skill、热更新、多进程或 Pair 场景。
- 保持单进程、SQLite 真相源、不可变 Artifact、显式 Context Builder 和可恢复阶段状态。
- 所有新增 Go 函数/方法写简洁中文注释；导出标识符符合 Go doc。使用早返回、显式构造、字段名初始化、`%w`，预期失败不 panic。
- 每个 goroutine 有所有者、取消路径和 Wait；测试用阻塞通道/fake clock，不用 `time.Sleep` 猜调度。
- API Key 只从配置指定环境变量读取；多个 Profile 可共享同一 env。Secret 不进入快照、指纹、DB、Artifact、日志或导出。
- Prompt、Response、证据文本、原始模型输出、Audit 源载荷仅可进入批准的 restricted SQLite/Artifact 路径，不得进入 stdout/stderr、状态、公开报告、错误或 Git。

## 模型与确定性代码权限

模型只能：解释未知来源、发现局部/全局模式、候选单条仲裁、诊断、写 Proposal、独立 Critic、提出 Change Set。

确定性代码必须独占：

- 应用已经人工批准且绑定源 SHA-256 的 Mapping；
- Canonical Audit Record 归一化与 provenance；
- disagreement 选择、70/20/10 分层、记录分配、统计和引用验证；
- authority 顺序与冲突校验；
- Change Set patch 应用；
- Policy/Prompt 编译、ID coverage、byte-stability、manifest/hash；
- Gold 状态迁移与人工批准校验；
- Regression 指标独立读取、Gate 计算；
- Approval hash 校验和不可变原子 Release。

不得用模型输出替代上述权威操作。模型推荐的 Mapping、Gold、计数、Policy 文本、Regression 结果和 Release 决定都只是候选。

## 固定业务约束

- 13 个 Skill ID 和 executor 类型固定，以设计 section 6 为准，不实现任意插件注册。
- `policy.base_version`、`policy.target_version`、candidate/preview/release 命名和不可原地晋级规则严格按 Implementation Contract，不自行递增或复用版本。
- 未知/custom 来源必须先 `inspect`，再由人批准 Mapping；源字节变化后旧批准失效。
- 每条记录只进入一个 primary mining batch；默认 70% 同类、20% 冲突、10% 随机，默认 50、最小 30、最大 100。
- Local Pattern 不能改 Policy；Global counts 和引用由代码重算；批量发现模式，单条处理边界。
- Human/Security authority 高于 Approved Gold，高于自动/第三方 Proposal，高于 observation；同级高权威冲突阻塞。
- Rule Author/Change Resolver 初始 DeepSeek，Critic 初始 Qwen，Critic Family 必须不同。
- Compiler 不创造 Policy，只从 Policy、Rule Cards、Case Type、Evidence Ownership、Role、Schema、Approved Examples 确定性生成 Prompt。
- `compile --preview` 可生成候选预览，但不能通过 Regression、Approval 或 Release。
- Candidate Gold 不得作为验收真值；Approved -> Core 至少三次成功 release、无未决 challenge、并有人工晋级批准。
- Core Safe 和 Hard Negative 的 false Unsafe 必须为 0；Hidden Unsafe resolved Safe 必须为 0；两次 A/B rotation 各自通过。
- Candidate/Regression/Critic 任一字节变化使 Approval 失效；released directory 永不覆盖、删除或复用。

## 强制 TDD 和验证

1. 先写当前功能的完整行为/失败测试，再运行实施计划给出的精确命令和 `-count=1`。
2. RED 必须由行为缺失造成；语法错误、坏 fixture、无关环境、手工 panic、外网失败不是有效 RED。
3. RED 后立即记录命令、退出码和原因，再写满足当前功能的最小实现。
4. 用完全相同命令得到 GREEN，不删、不弱化、不 skip、不重写断言。
5. 执行 Task 指定的重复并发、race、fuzz、failure injection、mutation 和独立重算。临时 mutation 必须恢复后重跑。
6. 执行受影响包、`./init.sh`、scope、安全/载荷、敏感 Artifact、`git diff --check` 和逐文件 diff 审阅。
7. 没有每层的新鲜精确证据不得标记 done。Fake、缓存、模型自报指标或默认 skip 不能替代真实验收。

## 人工检查点

遇到 Mapping 批准、Gold 批准/晋级、同级高权威冲突、Policy Directive、Release Approval、缺少真实凭据或 Human-approved Audit/Gold 时，持久化当前状态并停止，给出唯一、精确的人工动作。不得虚构 approver、替用户决定，也不得让 `analyze`、`compile` 或 `regression` 自动调用 `mapping-approve`、`gold-approve`、`gold-promote` 或 `approve`。

## 状态与结束

每次状态变化立即更新：

- `feature_list.json`：唯一功能状态和最终精确证据；
- `progress.md`：基线、RED/GREEN、变更、验证、风险、下一命令；
- `session-handoff.md`：无聊天历史的新模型可以继续的完整事实。

结束前停止/排空长命令，检查 Git 状态与敏感数据，只留下 `done`、有证据的 `blocked`，或唯一 `in-progress` 加一条精确下一命令。除非用户明确要求，不创建 commit。

最终回复只报告：本轮唯一功能、实际文件、精确 RED/GREEN/repeat/race/full/scope/security 结果、Artifact/状态不变量、blocker/人工动作、下一功能但不开始。

现在执行启动顺序并只完成唯一当前功能。
