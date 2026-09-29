# 广告数据全量级联清洗设计

日期：2026-09-22
状态：已批准

## 1. 目标

对 `data/ad/advertisement_dataset_final.json` 的 58,658 条记录逐条进行全量语义复核，而不是用 512 条 pilot 或 265 条定向样本代替全量结论。流程必须可恢复、可审计、保留原始 `trace_id` 和输入顺序，并在人工门禁前禁止覆盖源数据、权威基线或 provisional 数据。

本功能编号为 `feat-040`。`feat-039` 保留为轻量模型试验和定向校准工作的历史记录；其两个 Flash 模型失败证据不得被重解释为全量授权。

## 2. 权威输入与保护对象

- 广告源：`data/ad/advertisement_dataset_final.json`，预期 58,658 条。
- 最新权威基线：`data/task-013/task-013.reviewed.v8.jsonl`。
- 已有 provisional：`data/task-013/task-013.reviewed.v8.with-advertisement.provisional.jsonl`。
- 512 条人工 worksheet：以 `progress.md` 中固定路径和 SHA-256 为准。
- 265 条定向强审：`data/ad/directed-cleaning-prep` 下两遍 decisions 与 mapping。

源文件、权威基线和已有 provisional 全部只读。任何清洗结果都写入新目录；最终替换也必须写新文件。

## 3. 校准真值

本地命令确定性生成冻结校准 manifest：

- 512 条 worksheet 提供 `label` 与既有 38 类风险重叠真值。
- 265 条定向强审提供 `label` 真值；其中第一遍 `l=2` 保持 unsafe，进入第二遍的记录以第二遍结果为最终值。
- 两部分必须按原始 `trace_id` 去重；任何冲突必须失败，不能择一覆盖。
- 265 条没有可靠的 38 类重叠真值，不得伪造 overlap 标签。
- 模型请求永远不包含真值、原标签、来源、解释或 `trace_id`。

校准集按 `trace_id` 的稳定哈希和标签/来源分层，冻结为 prompt-development 与 holdout 两部分。holdout 不能用于修改 prompt 或阈值。

## 4. 模型级联

### 4.1 第一层：全量初审

第一层固定使用：

```yaml
base_url: https://aigateway.venusgroup.com.cn/ai/aliyun/openai
model: qwen3.5-plus
api_key_env: AI_GATEWAY_API_KEY
structured_output: json_object
temperature: 0
top_p: 1
concurrency: 4
requests_per_minute: 30
```

Flash 模型已经失败，不得重新作为正式候选。第一层覆盖全部 58,658 条，而不是只处理抽样或原 unsafe 行。

模型输入只包含：

```json
{"items":[{"i":0,"p":"原始 prompt"}]}
```

模型输出只包含：

```json
{"r":[{"i":0,"l":1,"x":""}]}
```

- `l=0` uncertain，`l=1` safe，`l=2` unsafe。
- `x` 为空或 `config/risk-types.yaml` 中既有 38 类的一个 key。
- `l=1` 时 `x` 必须为空。
- 不输出解释、置信度、`trace_id`、广告小类或原文。

本地代码严格校验数量、索引唯一性、顺序和枚举；不得修补模型输出。

### 4.2 第二层：定向 Pro 复核

第二层固定使用：

```yaml
base_url: https://aigateway.venusgroup.com.cn/ai/aliyun/openai
model: deepseek-v4-pro
api_key_env: AI_GATEWAY_API_KEY
structured_output: json_object
temperature: 0
top_p: 1
concurrency: 4
requests_per_minute: 30
```

只有以下记录进入第二层：

1. 第一层 `l=0`。
2. 第一层结论与源标签不同。
3. 第一层 `x` 非空，即疑似同时命中既有风险。
4. 第一层供应商或格式失败。

第二层使用独立 task ID、SQLite、mapping、输出目录和语义指纹。输入仍只有 `i,p`，不能看到第一层或源标签。

## 5. 确定性裁决

- 第一层与源标签一致且 `x=""`：保留原记录，不调用 Pro。
- 第一层建议改标签，且 Pro 给出相同标签、两者均非 uncertain：形成 approved label change。
- 两模型不一致、任一 uncertain、失败或缺项：进入 quarantine，保持原记录。
- 任一模型给出 `x`：进入 legacy-overlap manifest，保持 `attack_domain=advertisement`，不得自动改成旧风险类。
- `attack_scenario` 只透传，任何阶段都不得修改。
- 未经两模型一致确认，不得将 unsafe 自动改为 safe。

## 6. 输出

输出目录至少包含：

- `layer1/`：第一层 SQLite、聚合报告和安全失败清单。
- `layer2/`：第二层 SQLite、聚合报告和安全失败清单。
- `approved-changes.jsonl`：只含 `trace_id`、旧/新 label 和问题码，不含 prompt。
- `legacy-overlap.jsonl`：只含 `trace_id` 与一致确认或待复核的旧风险 key。
- `quarantine.jsonl`：只含 `trace_id` 与闭集问题码。
- `advertisement_dataset.cleaned-v1.json`：新的完整广告数据。
- `full-cleaning-report.json`：聚合计数、模型、输入哈希和门禁结果。

报告、manifest 和日志不得含 prompt、response、explanation、模型原文或凭据。模型原始回复只允许存在 SQLite 审计状态中。

## 7. 应用规则

- approved `unsafe -> safe`：设置 `label=safe`，清空 `risk_level`、`attack_domain`、`attack_method` 和广告风险类型字段；`attack_scenario` 原样保留。
- approved `safe -> unsafe`：设置 `label=unsafe`，设置 `attack_domain=advertisement`，`attack_method` 为空；保留原 `attack_scenario`，不得猜测小类。
- 未批准、重叠或隔离记录：JSON 值级原样保留。
- 所有未知字段、`trace_id` 和数组顺序必须保持。

## 8. Pilot 与人工门禁

先只运行冻结校准集，不得直接启动全量。holdout 固定门槛：

- provider/format 最终失败为 0。
- safe/unsafe accuracy `>=98%`。
- human-unsafe recall `>=99%`，且 human-unsafe 被判 safe 为 0。
- human-safe recall `>=97%`。
- worksheet 中源标签错误路由率 `100%`。
- worksheet 中 legacy overlap 路由率 `>=95%`，且自动 clean 中 overlap 泄漏为 0。
- 平均批装载 `>=8`，无未解决 context overflow。

Qwen pilot 不通过时停止，不得靠降低阈值启动全量。允许修改 prompt 后使用全新 task ID 重跑；仍不通过则必须由用户批准替代模型或终止。

## 9. 全量与最终替换门禁

只有用户查看 pilot 报告并明确批准后，才运行 58,658 条第一层和路由后的第二层。全量完成必须证明：

- 输入、cleaned 均为 58,658 条，`trace_id` 唯一且集合/顺序完全一致。
- 每个输入都具有第一层终态；每个第二层候选都具有第二层终态或 quarantine。
- approved、overlap、quarantine 互斥且可回溯。
- 非 approved 记录 JSON 值级不变。
- unsafe 的 `attack_domain=advertisement`；safe 风险字段为空。
- `attack_scenario` 全量逐值不变。
- 源、权威基线、旧 provisional 哈希不变。

随后才可按 `trace_id` 将 cleaned 广告记录替换进旧 provisional，并写入新文件 `task-013.reviewed.v8.with-advertisement.cleaned-v1.jsonl`。替换器必须证明非广告记录逐值不变、总行数不变且无 ID 增删。

## 10. 非目标

- 不重新分类或改写广告小类。
- 不用模型生成新的 `trace_id`、解释或数据正文。
- 不自动把 overlap 改成既有风险类型。
- 不在实现或测试阶段调用真实模型；真实 pilot 和全量均为独立人工门禁。
- 不覆盖任何既有数据文件。
