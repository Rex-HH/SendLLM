# 给广告数据复核代码模型的启动提示词

你负责在 `/Users/lijiayang/venus/SendLLM` 中实现已经批准的 `feat-039`：广告数据轻量复核。直接在仓库工作，
不要只输出方案、伪代码或建议。本任务只产出原样正常池、原样问题池、问题清单和聚合报告；不得修正原数据，
不得与最终数据集合并。

## 权威资料

发生冲突时按以下顺序执行：

1. 用户当前明确要求。
2. `AGENTS.md`。
3. `docs/superpowers/specs/2026-09-21-advertisement-dataset-lightweight-review-design.md`。
4. `docs/advertisement-review-agent-harness.md`。
5. `docs/superpowers/plans/2026-09-21-advertisement-dataset-lightweight-review.md`。
6. 现有代码惯例。

必须完整阅读上述文件，以及基础设计、`开发指南.md`、`feature_list.json`、`progress.md`、
`session-handoff.md`。不得依赖聊天摘要，不得重新设计已冻结的字段、分流逻辑、模型阶梯、输出或验收阈值。
若文件存在实质冲突，停止修改并报告精确文件和章节，不要自行选择解释。

## 本轮范围

- 只实现 `feat-039`，按实施计划 Task 1 到 Task 5 顺序推进；同一时间只允许这一项为 `in-progress`。
- 保持 `feat-024=blocked`、`feat-025=pending` 及 Policy Optimization 状态不变。
- 只能修改 Harness 第 3 节列出的文件。开始前已有的修改和未跟踪文件全部视为用户所有，不回退、不整理、
  不提交无关内容。
- 不修改 `data/ad/advertisement_dataset_final.json`，不修改 `data/final`，不写修正版，不执行合并。
- 不修改既有 DAO/DTO/facade/config/limiter/tokenizer/legacy service 接口；如果稳定接口不足，先停下请求设计变更。
- 不新增依赖。保持本地单进程、SQLite 真相源和 OpenAI 兼容模型边界。

## 启动顺序

1. `pwd`，必须是 `/Users/lijiayang/venus/SendLLM`。
2. 完整阅读权威资料和状态文件。
3. 运行 `git status --short`、`git log --oneline -5`，记录任务前工作区。
4. 运行 `./init.sh`，记录真实退出码；原有失败不得伪装成本轮引入。
5. 确认 `feat-039` 是唯一活动功能，并在 `progress.md` 记录当前 checkpoint 与下一条 RED 命令。
6. 保存受保护关键路径和原始广告文件的任务前哈希，存到仓库外；不要在仓库内生成锁定数据或输出。
7. 先写当前 checkpoint 的完整失败测试，运行实施计划规定的精确命令，证明有效 RED。

## 不可违反的不变量

- SQLite、manifest、报告、分流输出和后续交接一律使用原数据 `trace_id`；不得生成或改写 ID。
- 批内整数 `i` 仅用于一次请求，由本地请求快照映射回原 `trace_id`，不得持久化为稳定 ID。
- 模型输入的语义字段只能是 `i`、原 `prompt` 和原 `attack_scenario`；不能发送原 label、trace_id、
  risk_type、四个旧广告大类、response、explanation、source、quality_score 或 annotation。
- 模型输出固定为 `{"r":[{"i":0,"l":1,"x":"","s":0}]}`；本地严格验证数量、索引和闭集。
- `config/risk-types.yaml` 是原 38 类唯一闭集。第 39 类正式枚举是 `advertisement`，但本阶段不回写源记录。
- uncertain、缺项、重复索引、非法枚举、未知旧风险、格式错误、供应商失败和超长失败都只能进问题池，
  绝不能默认为正常。
- 正常池和问题池中的对象必须与原对象做 JSON 值级深相等；除数组转 JSONL 外不增删改任何字段。
- `issues.manifest.jsonl` 和 `review-report.json` 不得包含 prompt、response、explanation、完整模型输出或凭据。
- 不日志打印原始数据、模型完整输出、Authorization 或 API Key。

## 实现纪律

每个 Task 都严格执行 RED → 最小实现 → 同命令 GREEN → 受影响包 → repeat/race → 状态记录。RED 必须因目标
行为缺失而失败；语法、坏 fixture、网络、手工 panic 或无关基线失败不算。不得删除、弱化、skip 或改写断言。

所有普通测试使用合成数据和 fake provider，不调用真实网络。并发测试使用阻塞通道、fake clock/jitter 和
`t.TempDir()`，不使用 `time.Sleep` 猜调度。新增 Go 注释使用简洁中文；导出标识符遵循 Go doc。每个
goroutine 必须有所有者、取消路径和 Wait。错误包内返回并 `%w`，只在 CLI 边界记录。

必须完成 Harness 第 6 节完整测试矩阵，并对以下四项做临时 mutation 反证后恢复：原 ID 映射、模型输入盲化、
缺失索引拒绝、导出对象深相等。任何 mutation 不得提交。

## 本地完成门禁

实现后严格执行 Harness 第 9 节全部命令，包括 focused、重复十次、race 十次、全量测试、vet、`./init.sh`、
`git diff --check`、范围和敏感信息检查。使用独立校验器证明两个分区完整、互斥、并集等于输入，输出对象原样，
manifest 与问题池一一对应。Fake provider 只能证明实现正确，不能证明模型质量。

本地门禁通过后，把状态记为 `local-verified`，但保持 `feat-039=in-progress`。

## 真实模型门禁

按 Harness 第 10 节在仓库外生成确定性、最多 512 条的分层 pilot，保留原 `trace_id`。先用：

```text
base_url=https://aigateway.venusgroup.com.cn/ai/aliyun/openai
model=qwen3.5-flash
api_key_env=AI_GATEWAY_API_KEY
```

只检查环境变量非空，不打印值。Pilot 使用全新 task ID、SQLite 和输出目录。报告只含模型名、文件路径、输入
哈希和聚合计数。遇到供应商/格式失败要保留状态并停止，不能静默换模型。

生成 pilot 和本地人工复核 worksheet 后必须停止，请用户独立标注全部 pilot 的 safe/unsafe 与原 38 类重叠，
不得自己虚构人工答案。用户明确批准前，禁止启动 58,658 条全量任务。如果 Qwen 未达到 Harness 固定阈值，
才可使用 `config/task.advertisement-review.deepseek-v4-flash.yaml` 对同一 pilot 独立运行
`deepseek-v4-flash`；两者必须使用不同 task ID、SQLite 和输出目录。两者都不通过则停止，不自行上 Pro 或降低阈值。

只有用户明确指定并批准模型后，才执行 Harness 第 11 节全量运行和十二项独立完整性校验。全量通过表示“复核
分流完成”，不表示“修正完成”或“合并完成”。

## 状态与结束

每次验证变化立即更新 `feature_list.json`、`progress.md`、`session-handoff.md`，使用 Harness 第 12 节证据格式。
没有真实命令、退出码和独立校验，不得写“通过”。除非用户明确要求，不创建 commit。

每轮结束前停止所有长命令，检查 task-owned diff、受保护路径哈希、数据源哈希、生成物和敏感信息。只留下
`done`、有精确证据的 `blocked`，或 `in-progress` 加唯一下一命令。等待人工 pilot 结论时保持
`in-progress`，并清楚写明需要用户完成的 worksheet 和批准动作。

最终回复只报告：完成到哪个 checkpoint、实际修改文件、精确 RED/GREEN/repeat/race/full/scope/security
结果、pilot 聚合与路径（若执行）、人工门禁状态、下一条唯一动作。

现在执行启动顺序并只推进 `feat-039`。
