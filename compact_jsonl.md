# Compact JSONL Output Format

`compact_jsonl` 是 dataprep 当前对外输出格式。文件是 JSONL：每一行都是一个完整 JSON 对象，每一行都应符合
`docs/output/compact_jsonl.schema.json`。

## 版本

- 格式名：`compact_jsonl`
- Schema 文件：`docs/output/compact_jsonl.schema.json`
- 兼容策略：核心字段稳定；`meta` 允许新增字段，下一模块应忽略未知 `meta` 字段。

## 顶层结构

```json
{
  "id": "v2_authoritative:2e0c...",
  "source": {
    "dataset": "v2_authoritative",
    "path": "datasets/benchmarkV2.0/38-Categories/in-2-jailbreak/jailbreak_detection_final.json",
    "index": 8624
  },
  "messages": [
    {"role": "user", "content": "提示词。"},
    {"role": "assistant", "content": "回复。"}
  ],
  "label": {
    "value": "unsafe",
    "risk_type": "RT10_对抗性攻击",
    "risk_level": "high"
  },
  "meta": {
    "sample_id": "OUT-JB-10_RT10_attack_pos_008625",
    "sample_type": "攻击正例",
    "split": "train",
    "scenario": "普通聊天/问答助手",
    "severity": "high",
    "content_presence": "pair",
    "source_fields": {
      "original_fields": {}
    },
    "findings": []
  }
}
```

## 字段说明

| 字段 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- |
| `id` | 是 | string | 稳定 ID，格式为 `<dataset_name>:<sha256>`。 |
| `source` | 是 | object | 源数据定位信息。 |
| `source.dataset` | 是 | string | 配置中的数据集名称。 |
| `source.path` | 是 | string | 实际读取的源文件路径。 |
| `source.index` | 是 | integer | 流式读取序号，从 0 开始。 |
| `messages` | 是 | array | 清洗后的多轮消息，保持消息顺序。 |
| `messages[].role` | 是 | string | 消息角色，通常是 `user` 或 `assistant`。 |
| `messages[].content` | 是 | string | 非空消息内容。 |
| `label` | 是 | object | 规范化安全标签。 |
| `label.value` | 是 | string | 只允许 `safe` 或 `unsafe`。 |
| `label.risk_type` | 否 | string | unsafe 样本的风险类型；safe 样本省略。 |
| `label.risk_level` | 否 | string | unsafe 样本的风险等级；safe 样本省略。 |
| `meta` | 否 | object | 可扩展元数据。 |

## Meta 约定

`meta` 用于保留源数据中对下游有用、但不属于核心结构的字段。

常见顶层字段：

- `sample_id`：源数据样本 ID。
- `sample_type`：源数据样本类型。
- `split`：源数据 split。
- `scenario`：源数据场景。
- `severity`：源数据风险等级或严重程度。
- `language`：源数据语言。
- `content_presence`：消息形态，取值为 `prompt`、`response`、`pair`、`other`。
- `source_fields`：没有进入核心字段或顶层 `meta` 的源字段。
- `findings`：确定性规则发现的问题或疑点。

`source_fields` 不会重复保存以下字段：

- `messages`
- `message`
- `is_risk`
- `labels`
- `sample_id`
- `sample_type`
- `split`
- `scenario`
- `severity`
- `language`

## ID 规则

`id` 由数据集名称和 sha256 哈希组成：

```text
<dataset_name>:<sha256(dataset_name + normalized_messages + source_identity)>
```

- `dataset_name`：配置中的数据集名称，例如 `v2_authoritative`。
- `normalized_messages`：按顺序拼接每条消息的 `role` 和 `content`。
- `source_identity`：包含源数据中的 `sample_id` 或 `source_id`，并始终追加 `source.path` 和 `source.index`。

这个规则保证：

- 同一条源记录重复运行时 ID 稳定。
- 不同数据集即使内容相同，ID 也不同。
- 同一数据集里相同内容、甚至相同 `sample_id` 的不同源位置记录，ID 也不同。

## 下游对接建议

下一模块应固定依赖这些核心字段：

- `id`
- `source.dataset`
- `source.path`
- `source.index`
- `messages`
- `label.value`
- `label.risk_type`
- `label.risk_level`
- `meta.sample_id`
- `meta.sample_type`

下一模块应把 `meta.source_fields` 当作透传扩展字段处理，不要依赖固定完整字段集。新增数据集接入后，
`meta.source_fields` 可能出现新的键，但核心字段含义保持不变。
