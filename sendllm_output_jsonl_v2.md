# SendLLM 新标签输出 JSONL 格式

本文档描述 SendLLM 使用新标签体系完成提示词安全标注后，交付给下游模块的成功输出 JSONL 结构。

新标签体系将原 `extended_info.risk_type` 拆分为：

- `attack_method`：攻击方式，仅从“攻击与对抗类”风险中取值，并且只根据 `prompt` 判断。
- `attack_domain`：攻击内容领域，从“攻击与对抗类”之外的风险领域中取值，根据 `prompt` 与 `response` 综合判断；在纯提示词任务中只根据 `prompt` 判断。

## 单行结构

```json
{
  "id": "v2_authoritative:2e0c...",
  "source": {
    "dataset": "v2_authoritative",
    "path": "datasets/benchmarkV2.0/38-Categories/in-1-prompt-injection/samples.jsonl",
    "index": 0
  },
  "messages": [
    {
      "role": "user",
      "content": "用户提示词"
    }
  ],
  "label": {
    "value": "unsafe",
    "risk_type": "RT01",
    "risk_level": "low"
  },
  "meta": {
    "content_presence": "prompt",
    "sample_id": "IN_01_final_selection_v3_000001",
    "sample_type": "明确正例",
    "split": "train",
    "source_fields": {
      "reason": "输入直接表达指令覆盖，若模型服从会违反提示词注入边界。"
    }
  },
  "annotation": {
    "method": "auto",
    "is_attack": true,
    "case_type": "typical",
    "explanation": "该提示词试图覆盖原有指令并诱导模型输出指定内容。",
    "quality_score": 0.92,
    "extended_info": {
      "attack_method": "prompt_injection",
      "attack_domain": "",
      "risk_level": "high",
      "attack_scenario": "instruction_override",
      "other": ""
    }
  }
}
```

## 顶层字段

| 字段 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- |
| `id` | 是 | string | 稳定样本 ID，格式通常为 `<dataset_name>:<hash>`。 |
| `source` | 否 | object | 上游源数据定位信息，原样透传。 |
| `source.dataset` | 否 | string | 数据集名称。 |
| `source.path` | 否 | string | 源文件路径。 |
| `source.index` | 否 | integer | 源文件流式序号，从 0 开始。 |
| `messages` | 是 | array | 上游消息列表，原样透传。纯提示词任务通常只包含 `user` 消息。 |
| `messages[].role` | 是 | string | 消息角色，通常是 `user` 或 `assistant`。 |
| `messages[].content` | 是 | string | 消息内容。 |
| `label` | 是 | object | 上游原始标签，原样透传，不代表 SendLLM 的模型判断。 |
| `label.value` | 是 | string | `safe` 或 `unsafe`。 |
| `label.risk_type` | 否 | string | 上游旧标签，可继续透传；在新标签体系下不作为模型输出契约。 |
| `label.risk_level` | 否 | string | 上游风险等级，可继续透传。 |
| `meta` | 否 | object | 上游扩展元数据，原样透传。 |
| `annotation` | 是 | object | SendLLM 自动生成的最终模型标注。 |

## `annotation` 字段

| 字段 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- |
| `method` | 是 | string | 成功自动标注时固定为 `auto`。 |
| `is_attack` | 是 | boolean | 是否包含攻击或安全风险意图。 |
| `case_type` | 是 | string | `typical`、`borderline`、`variant`、`hard_negative` 之一。 |
| `explanation` | 是 | string | 中文判断理由，长度 10 到 70 个字符。 |
| `quality_score` | 是 | number | 模型自评质量分，范围 0 到 1；越高表示模型对自己的判断越有把握。 |
| `extended_info` | 是 | object | 新标签风险信息。 |

## `extended_info` 字段

| 字段 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- |
| `attack_method` | 是 | string | 攻击方式。仅从攻击与对抗类枚举中选择；无明确攻击方式时为空字符串。 |
| `attack_domain` | 是 | string | 攻击内容领域。仅从其他风险领域枚举中选择；无明确内容领域时为空字符串。 |
| `risk_level` | 条件必填 | string | `is_attack=true` 时必填，只允许 `low`、`medium`、`high`。 |
| `attack_scenario` | 否 | string | 攻击场景或细分描述。 |
| `other` | 否 | string | 其他补充信息。 |

## 新标签判定规则

- `is_attack=true` 时，`attack_method` 和 `attack_domain` 至少有一个非空，且必须填写 `risk_level`。
- `is_attack=false` 时，`attack_method` 和 `attack_domain` 都必须为空字符串，且不能填写 `risk_level`。
- `case_type=hard_negative` 时，`is_attack` 必须为 `false`，两个新标签字段均为空字符串。
- `attack_method` 只从“攻击与对抗类”选择，并只根据 `prompt` 判断。
- `attack_domain` 从其他风险领域选择，根据 `prompt` 与 `response` 判断；纯提示词任务只根据 `prompt` 判断。
- 如果 `prompt` 同时满足攻击方式和内容领域，优先选择 `attack_method`。

## 攻击方式枚举

攻击方式只允许以下值：

```text
prompt_injection
jailbreak
encoding_obfuscation
cross_language_attack
cross_modal_attack
multi_turn_jailbreak
financial_domain_attack
```

`attack_domain` 使用 `config/risk-types.yaml` 中除上述攻击方式外的其他风险枚举。

## 兼容说明

- 旧版输出中的 `annotation.extended_info.risk_type` 仍可能出现在历史文件中；新标签任务的正式输出不生成该字段。
- 下游模块应优先读取 `attack_method` 和 `attack_domain`，不要把 `label.risk_type` 当作模型判断结果。
- 允许忽略未知字段，`meta` 和 `source_fields` 可能继续扩展。

## 校验 Schema

- 模型结果 Schema：`config/result-schema-v2.json`
- 导出 JSONL 行 Schema：`config/output-jsonl-schema.json`
