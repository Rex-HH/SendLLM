# SendLLM Success Output JSONL Format

`sendllm_output_jsonl` 是 SendLLM 对外提供给下一模块的成功数据集格式。文件是 JSONL：每一行都是一个完整 JSON 对象，表示一条已经由模型完成安全判断的样本。

## 版本

- 格式名：`sendllm_output_jsonl`
- 适用文件：任务配置 `task.output` 指定的成功输出文件。
- 兼容策略：完整透传上游 `compact_jsonl` 原始字段；SendLLM 只新增或覆盖顶层 `annotation` 字段。
- 本文件只描述成功输出数据集，不描述 `*.failed.jsonl` 失败数据集。

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
  },
  "annotation": {
    "method": "auto",
    "is_attack": true,
    "case_type": "typical",
    "explanation": "该内容存在明确攻击意图。",
    "extended_info": {
      "risk_type": "RT10_对抗性攻击",
      "risk_level": "high",
      "attack_scenario": "jailbreak",
      "other": ""
    }
  }
}
```

## 字段说明

| 字段 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- |
| `id` | 是 | string | 上游稳定 ID，格式通常为 `<dataset_name>:<sha256>`。SendLLM 使用该字段作为内部稳定样本 ID。 |
| `source` | 是 | object | 上游源数据定位信息，原样透传。 |
| `source.dataset` | 是 | string | 上游数据集名称。 |
| `source.path` | 是 | string | 上游实际读取的源文件路径。 |
| `source.index` | 是 | integer | 上游源文件中的流式读取序号，从 0 开始。 |
| `messages` | 是 | array | 上游清洗后的消息列表，保持原顺序，原样透传。 |
| `messages[].role` | 是 | string | 消息角色，通常是 `user` 或 `assistant`。 |
| `messages[].content` | 是 | string | 消息内容。SendLLM 只读取首个 `user` 和首个 `assistant` 内容用于模型判断。 |
| `label` | 是 | object | 上游原始规范化标签，原样透传，不表示 SendLLM 的模型判断结果。 |
| `label.value` | 是 | string | 上游标签，只允许 `safe` 或 `unsafe`。 |
| `label.risk_type` | 否 | string | 上游 unsafe 样本风险类型；safe 样本通常省略。 |
| `label.risk_level` | 否 | string | 上游 unsafe 样本风险等级；safe 样本通常省略。 |
| `meta` | 否 | object | 上游可扩展元数据，原样透传。下一模块应忽略未知 `meta` 字段。 |
| `annotation` | 是 | object | SendLLM 自动生成的模型判断结果。 |
| `annotation.method` | 是 | string | 固定为 `auto`，表示该条成功记录由 SendLLM 自动标注。 |
| `annotation.is_attack` | 是 | boolean | SendLLM 判断该样本是否包含攻击或安全风险意图。 |
| `annotation.case_type` | 是 | string | 样本类型，只允许 `typical`、`borderline`、`variant`、`hard_negative`。 |
| `annotation.explanation` | 是 | string | 中文判断理由，长度为 10 至 70 个字符，不应复述原文。 |
| `annotation.extended_info` | 否 | object | 风险扩展信息。unsafe 样本通常包含该对象；safe 样本可能省略。 |
| `annotation.extended_info.risk_type` | 条件必填 | string | `annotation.is_attack=true` 时必填，取值来自任务配置的风险类型闭集。 |
| `annotation.extended_info.risk_level` | 条件必填 | string | `annotation.is_attack=true` 时必填，只允许 `low`、`medium`、`high`。 |
| `annotation.extended_info.attack_scenario` | 否 | string | 攻击场景或模型给出的细分描述。 |
| `annotation.extended_info.other` | 否 | string | 其他补充信息。 |

## 语义约定

- 下游模块应把 `annotation` 视为 SendLLM 的最终模型判断。
- 下游模块不应把顶层 `label` 当成 SendLLM 判断结果；它是上游输入标签，保留用于对比、训练或审计。
- SendLLM 保留上游所有字段，包括未来新增字段；下游应只依赖本文件列出的稳定字段。
- 如果原始输入已经包含顶层 `annotation`，成功导出时会被 SendLLM 生成的 `annotation` 覆盖。
- 成功输出文件只包含已经完成自动标注的记录，并按原始输入顺序排列。

## 下游对接建议

下一模块建议固定读取以下字段：

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
- `annotation.method`
- `annotation.is_attack`
- `annotation.case_type`
- `annotation.explanation`
- `annotation.extended_info.risk_type`
- `annotation.extended_info.risk_level`

下一模块应把 `meta`、`meta.source_fields` 和 `annotation.extended_info` 中未列出的字段当作可扩展字段处理，不要依赖固定完整字段集。
