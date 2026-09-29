# 给代码生成模型的启动提示词

你现在负责在 `/Users/lijiayang/venus/SendLLM` 中实现已经批准的 `safety-review` 子命令。直接在该仓库中工作，不要只给方案、伪代码或建议。

本轮只完成 `feature_list.json` 中依赖已经完成的第一个 Safety Review 功能。当前预期是 `feat-015`；你必须读取实际状态确认，不能假设。如果已有一个 Safety Review 功能是 `in-progress`，则恢复该功能，不得另开第二个功能。完成本功能的所有验证、状态记录和交接后停止，不得顺手实现后续功能。

## 权威资料与优先级

按以下优先级执行，发生冲突时以前者为准：

1. 用户当前明确要求。
2. 仓库根目录 `AGENTS.md`。
3. `docs/superpowers/specs/2026-09-04-safety-review-pipeline-design.md`。
4. `docs/safety-review-agent-harness.md`。
5. `docs/superpowers/plans/2026-09-04-safety-review-pipeline-implementation.md` 中当前功能对应的 Task。
6. 现有代码惯例。

必须完整阅读这些文件，不得依赖聊天摘要，也不得自行重新设计已经确定的分类流程、字段、状态、模型职责、错误策略、目录或验收阈值。若资料之间存在无法同时满足的实质冲突，停止修改并向用户指出精确文件、章节和冲突点；不要自行选择一种解释。

## 启动步骤

严格按顺序执行：

1. 运行 `pwd`，确认工作目录是 `/Users/lijiayang/venus/SendLLM`。
2. 完整阅读 `AGENTS.md` 以及上面列出的设计、Harness、实施计划。
3. 阅读 `docs/superpowers/specs/2026-08-05-sendllm-design.md`、`开发指南.md`、`feature_list.json`、`progress.md` 和 `session-handoff.md`。
4. 运行 `git status --short` 和 `git log --oneline -5`。
5. 当前工作区可能已有大量用户修改。把所有开始前已存在的修改视为用户所有；不得回退、覆盖、整理或提交与当前功能无关的内容。
6. 运行 `./init.sh`，记录真实退出码和失败信息。基线失败时先判断是否与当前功能相关，并把基线失败写入 `progress.md`；不得把原有失败伪装成本轮引入。
7. 根据依赖选择唯一功能，将其状态改为 `in-progress`，立即在 `progress.md` 写明基线、范围和下一条命令。
8. 如果 `scripts/verify-safety-review-scope.sh` 已存在，编辑前先运行；失败时停止并报告，不得改脚本绕过。

## 实现纪律

- 严格执行当前 Task 的文件清单、接口名称、测试矩阵和行为，不提前创建后续 Task 的实现。
- 除实施计划明确允许的文件外，不修改既有 Go 文件。`main.go` 的最小分发修改只允许在 `feat-022` 进行。
- 所有新增 Go 文件名必须以 `safety_review_` 开头。
- 不得向 `runner.go`、`label_review.go`、`adjudicate.go`、`reconcile.go` 或其他旧业务文件追加 Safety Review 逻辑。
- 优先复用已批准的通用边界：`service.Completer`、completion DTO、`facade.NewOpenAI` 及兼容的基础工具；不得为了复用而修改旧接口。
- 不增加依赖，不引入插件系统、HTTP 服务、队列、Redis、cron、多进程、热更新、Pair 场景或批量模型请求。
- 实现保持最小、直接、可读。禁止反射、通用框架、隐式注册、可变包全局变量和 `init()`。
- 所有新增 Go 函数和方法都写简洁中文注释；导出标识符使用 Go doc 格式。
- 错误在包内返回并 `%w` 包装，只在 CLI 边界记录；预期错误不得 panic。
- 每个 goroutine 必须有明确所有者、取消路径和等待退出机制。
- 不得打印或日志记录 Prompt、Response、证据片段、完整模型输出、Authorization 或 API Key。原始审计内容只能进入批准的 SQLite 字段。
- 不得读取、修改或提交真实输入输出、隐藏评测集、数据库、运行目录、`.env` 或凭据。
- 未覆盖的风险类别必须进入政策覆盖缺口/隔离流程，不能被默认为 Safe，也不能由你根据类别名称编造规则。
- Router 必须与 A/B 一样对所有样本执行；A/B 双 Safe 只有在 Router 完整覆盖且零候选等全部条件满足时才能走 Safe 快捷路径。
- Router 只能输出可观察事实和召回候选，不能输出最终 Unsafe、最终风险类别或规则成立结论。
- 只有 Expert 能建立类别；Arbiter 不能新增、建立或选择任何 Expert 未判定为 `established` 的类别。
- P04-B V1 禁用 `risk_level`；不得在 Schema、最终决策或生成标注中恢复该字段。
- 多个模型 Profile 可以合法共享同一个 `api_key_env`；只禁止持久化或泄露真实 Secret。
- P04-B 规则资产以已批准的人工确认案例为 Development/Regression 核心，Synthetic 仅补充未覆盖分支；Hidden Gold 和批量数据仍禁止提交。
- Safety Review 只读取 `policy/releases/<version>/` 中通过 `release.yaml` 全路径/hash/size/aggregate 校验的不可变发布包；Prompt 是包内编译产物，不从独立可变目录加载。
- `quality-events.jsonl` 只输出设计规定的 ID、状态、类别和降级/分歧元数据；不得包含 Prompt、Response、证据、rationale 或模型原始输出。
- Candidate Gold 只可输出待人工复核，不得作为 Development/Hidden acceptance truth；只有具备 hash-bound 人工批准记录的 Approved/Core Gold 可用于指标。

## 强制 TDD

1. 先写当前功能要求的测试，不写生产实现。
2. 使用实施计划指定的精确测试命令并加 `-count=1`，证明测试因目标行为尚不存在或错误而失败。
3. RED 必须是有效行为失败；语法错误、坏 fixture、无关环境错误、手工 panic 或空实现崩溃不算 RED。
4. 立即把 RED 命令、退出码和关键失败原因记录到 `progress.md`。
5. 编写满足当前功能的最小实现。
6. 使用同一命令得到 GREEN，不得删除、弱化、跳过或改写原断言。
7. 按计划执行 mutation、重复并发、恢复、race 或故障注入验证。临时 mutation 必须恢复，且恢复后重新运行目标测试。
8. 测试不得依赖真实等待、随机调度或外网；使用 fake clock、fake jitter、阻塞通道、`httptest` 和 `t.TempDir()`。

## 完成门禁

当前功能只有在以下项目全部有新鲜证据时才能标记 `done`：

1. 与 RED 相同的 focused GREEN 命令，使用 `-count=1 -v`。
2. 受影响包全部测试，使用 `-count=1`。
3. 实施计划指定次数的重复/并发测试。
4. 受影响包 `go test -race`，使用计划指定次数。
5. `./init.sh` 完整通过。
6. `scripts/verify-safety-review-scope.sh` 通过（文件存在时）。
7. `git diff --check` 通过。
8. 敏感信息、载荷日志、越界文件和生成物扫描通过。
9. 手工审阅当前功能的每一处 diff，确认没有后续功能、无关重构或用户修改回退。

不能用“测试通过”、缓存结果、截图、默认 skip 或 fake provider 替代精确命令和退出码。最终实时验收功能缺少模型、Key 或隐藏评测集时必须标记 `blocked`，不能标记通过，也不能降低阈值；但不得因此阻止普通 `validate` 或无 Gold 的模型运行任务。无 Gold 的普通运行只能标记为 `unvalidated`，不允许伪造准确率或把模型自评当作 Gold。

## 状态和交接

验证状态发生变化后立即更新，不要等到最后一次性补写：

- `feature_list.json`：当前功能状态和简洁、精确的最终证据。
- `progress.md`：基线、RED、GREEN、变更文件、验证命令、风险和下一步。
- `session-handoff.md`：让一个没有聊天历史的新模型可以从仓库状态继续。

结束前停止或排空所有长时间命令，运行 `git status --short`，确认没有后台测试或服务仍在运行。除非用户明确要求，否则不要创建 Git commit。

最终回复只报告：

1. 本轮完成的唯一 feature。
2. 实际修改的文件。
3. RED、GREEN、race、全量门禁和范围/安全检查的精确结果。
4. 未解决风险或 blocker。
5. 下一个未阻塞 feature，但不要开始实现它。

现在开始执行启动步骤，然后完成唯一的当前功能。
