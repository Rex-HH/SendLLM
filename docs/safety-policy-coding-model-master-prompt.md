# 给外部代码生成模型的唯一总启动提示词

你负责在 `/Users/lijiayang/venus/SendLLM` 中依次实现 Safety Review 与 Policy Optimization。直接操作仓库，不要只输出代码建议。每次会话只允许完成一个依赖已经满足的 feature；完成全部验证和状态交接后必须停止。

## 第一步：确定唯一 feature

1. 运行 `pwd` 并确认仓库根目录。
2. 完整读取 `AGENTS.md`、`feature_list.json`、`progress.md`、`session-handoff.md`。
3. 如果存在唯一 `in-progress` feature，只恢复它；存在多个时停止并报告状态冲突。
4. 否则选择 ID 最小、状态为 `pending`、所有 dependencies 均为 `done` 的 feature。
5. 不得跳过 feature，不得并行实现两个 feature，不得把相邻任务合并。

## 第二步：按 feature 加载唯一工作规范

如果当前 feature 是 `feat-015` 至 `feat-025`：

- 完整读取并严格执行 `docs/safety-review-coding-model-prompt.md`。
- 完整读取该提示词列出的 Safety Review Design、Implementation Plan 和 Harness。
- 不读取或实现 `feat-026` 之后的优化代码。

如果当前 feature 是 `feat-026` 至 `feat-038`：

- 先确认 `feat-025=done`，否则停止。
- 完整读取并严格执行 `docs/policy-optimization-coding-model-prompt.md`。
- 完整读取 Policy Optimization Design、Implementation Contract Freeze、Implementation Plan 和 Harness。
- Implementation Contract 的 CLI、DDL、状态、Artifact、Schema、Skill、Compiler、失败决策和 Gate 均已冻结，不得自行重新设计。

如果选中的 feature 不在上述范围，按仓库原有 Harness 执行，不得推断它属于 Safety Review 或 Policy Optimization。

## 第三步：实现与停止条件

- 按专用提示词执行 baseline、scope gate、TDD RED、最小实现、同命令 GREEN、重复/并发/race/fuzz/mutation/failure injection、`./init.sh`、安全扫描和 diff 审阅。
- 所有开始前已有修改都属于用户；不得回退、覆盖、格式化或提交无关内容。
- 不修改当前 feature 未授权的文件，不提前建设未来功能，不用 fake/skip/缓存/模型自报指标替代规定验收。
- 需要 Mapping、Gold、Policy Directive、Release Approval、真实模型凭据或真实 Audit/Gold/Hidden 数据时，持久化状态并停止，报告唯一精确的人工动作；不得伪造或代替用户批准。
- 验证状态变化后立即更新 `feature_list.json`、`progress.md`、`session-handoff.md`。
- 除非用户明确要求，不创建 Git commit。
- 最终只报告当前唯一 feature、实际文件、精确 RED/GREEN/重复/race/full/scope/security 证据、Artifact/状态不变量、blocker/人工动作和下一个未阻塞 feature。不得开始下一个 feature。

现在执行第一步，并只完成选中的一个 feature。
