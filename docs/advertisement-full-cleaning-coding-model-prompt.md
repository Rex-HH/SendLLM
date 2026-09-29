# 给代码模型的启动提示词：广告数据全量级联清洗

你负责在 `/Users/lijiayang/venus/SendLLM` 中实现 `feat-040`。直接修改仓库并完成测试，不要只输出建议、伪代码或重新设计方案。

## 权威顺序

1. 用户当前明确指令。
2. `AGENTS.md`。
3. `docs/superpowers/specs/2026-09-22-advertisement-full-cleaning-design.md`。
4. `docs/superpowers/plans/2026-09-22-advertisement-full-cleaning.md`。
5. `docs/advertisement-review-agent-harness.md` 中仍适用的原 ID、盲化、保护路径、SQLite 和人工门禁要求。
6. 现有代码惯例。

如果新全量设计与旧 `feat-039` 文档冲突，以新设计为准；但不得改写旧证据或声称两个 Flash 模型已通过。

## 当前事实

- 广告源有 58,658 条，源文件为 `data/ad/advertisement_dataset_final.json`。
- 权威基线是 `data/task-013/task-013.reviewed.v8.jsonl`。
- 已有 provisional 只能读取，禁止覆盖。
- `qwen3.5-flash` 和 `deepseek-v4-flash` 均未通过 512 条固定 pilot。
- 512 条 worksheet 有 label 和 legacy-overlap 人工真值。
- 265 条定向两遍强审只提供 label 真值，不能伪造 legacy-overlap 真值。
- 265 条工作的意义是校准与修正高风险区域，不是全量质量证明。

## 已冻结的关键文件

以下文件已经由强模型完成并属于本功能的权威输入，代码模型不得重新生成、改写措辞、调整模型、降低参数或扩大输出字段：

- `prompts/advertisement-full-review-system.txt`
- `config/advertisement-full-review-result-schema.json`
- `config/task.advertisement-full-review.qwen3.5-plus.yaml`
- `config/task.advertisement-full-review.deepseek-v4-pro.yaml`

实现应读取并测试这些文件。若现有配置加载器或运行接口无法满足其契约，先用失败测试证明精确缺口，再做最小兼容实现；不得通过修改冻结文件绕过测试。两个 YAML 都是 pilot 配置，通过人工门禁后，全量运行必须复制为全新 task ID、SQLite 和输出路径，禁止复用 pilot 状态。

## 启动顺序

1. 运行 `pwd`，确认仓库路径。
2. 完整阅读上述权威文件、`feature_list.json`、`progress.md` 和 `session-handoff.md`。
3. 运行 `git status --short` 和 `git log --oneline -5`；当前工作区很脏，所有既有修改和未跟踪文件均视为用户所有，禁止回退或整理。
4. 运行 `./init.sh` 并记录退出码。
5. 确认只有 `feat-040` 为 `in-progress`；`feat-039` 是历史 blocked 功能。
6. 保存源、权威基线和 provisional 的 SHA-256 到仓库外临时文件。
7. 严格按实施计划 Task 1 到 Task 6 顺序执行，每个 Task 都先 RED，再最小 GREEN。

## 实现边界

- 新命令放在 `cmd/advertisement-full-clean`，不要继续膨胀旧 `cmd/advertisement-clean-prep`。
- 复用现有 OpenAI facade、SQLite、retry、limiter 和 tokenizer；禁止复制通用基础设施或新增依赖。
- 模型边界保持 OpenAI 兼容，API Key 只读 `AI_GATEWAY_API_KEY`。
- 第一层固定 `qwen3.5-plus`，覆盖全部 58,658 条。
- 第二层固定 `deepseek-v4-pro`，只处理标签变更、legacy overlap、uncertain 和失败路由项。
- 两层必须使用独立 task ID、SQLite、语义指纹和输出目录。
- 任何 prompt、Schema、模型名、参数或 taxonomy 变化都必须使用新 task ID/state。

## 紧凑模型协议

请求只能是：

```json
{"items":[{"i":0,"p":"原始 prompt"}]}
```

禁止发送原 label、trace_id、source、response、explanation、annotation、risk_type、attack_scenario 或人工真值。

返回只能是：

```json
{"r":[{"i":0,"l":1,"x":""}]}
```

`l` 只能为 0/1/2；`x` 只能为空或现有 38 类 key；`l=1` 时 `x` 必须为空。数量、索引、顺序、重复和未知字段均严格本地校验，不得自动修补。

## 裁决与应用

- 未路由记录保持原样。
- 标签修改必须由 Qwen 与 Pro 独立一致确认。
- 不一致、uncertain、失败和缺项全部 quarantine，保持原样。
- 任意 legacy overlap 进入 manifest，保持 `attack_domain=advertisement`，不自动改为旧风险。
- approved safe 清空风险字段；approved unsafe 设置 `attack_domain=advertisement`、空 `attack_method`。
- `attack_scenario`、原始 `trace_id`、顺序和未知字段永远不改。
- 不允许覆盖源、权威基线或旧 provisional。

## 测试纪律

普通测试只用合成数据和 fake provider，不调用真实模型。并发测试不用 `time.Sleep`。新增 Go 注释必须简洁中文，导出符号符合 Go doc。每项执行：

1. 写完整失败测试。
2. 运行计划中的精确 RED 命令并确认因目标行为缺失而失败。
3. 写最小实现。
4. 同命令 GREEN。
5. 运行 `-count=10`、对应 race、受影响包、`go test ./... -count=1`、`go vet ./...`、`./init.sh` 和 `git diff --check`。

必须对原 ID 映射、模型输入盲化、缺失索引拒绝、agreement-only apply、overlap quarantine 和非广告记录逐值不变做 mutation 反证后恢复。

## 停止点

实现完成后只准备冻结 pilot，不调用真实模型，不启动 58,658 条全量任务，不生成最终 cleaned 或合并文件。把 `feat-040` 保持 `in-progress`，记录精确验证证据，并把唯一下一动作写为：用户批准运行 `qwen3.5-plus` 冻结 pilot。

若基线失败、现有接口不足、校准真值冲突、受保护文件哈希变化或同一失败连续三次，立即停止并记录证据；不得降低阈值、跳过测试、换模型或继续全量。

现在开始执行启动顺序，并只推进 `feat-040`。
