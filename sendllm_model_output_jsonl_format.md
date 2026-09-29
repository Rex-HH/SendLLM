# SendLLM 成功模型输出 JSONL 对接格式

本文档描述 SendLLM 交给下一个模块消费的成功输出 JSONL 文件格式。每一行都是一个完整 JSON 对象，表示一条已经完成模型自动标注的样本。

## 输出文件

成功输出文件路径由任务配置决定：

```yaml
task:
  output: ../data/task-003-dark/output.jsonl
```

本文档只描述 `output.jsonl`，不描述 `output.failed.jsonl`。

## 单行结构

每一行保留上游 `compact_jsonl` 原始字段，并新增或覆盖顶层 `annotation`。

```json
{
  "id": "v2_authoritative:2e0c...",
  "source": {
    "dataset": "v2_authoritative",
    "path": "datasets/source.json",
    "index": 8624
  },
  "messages": [
    {"role": "user", "content": "用户输入"},
    {"role": "assistant", "content": "模型回复"}
  ],
  "label": {
    "value": "unsafe",
    "risk_type": "RT10_对抗性攻击",
    "risk_level": "high"
  },
  "meta": {
    "sample_id": "sample-1",
    "sample_type": "攻击正例"
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

## 字段契约

| 字段 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- |
| `id` | 是 | string | 上游稳定样本 ID，也是 SendLLM 内部稳定 ID。 |
| `source` | 否 | object | 上游源定位信息，原样透传。 |
| `messages` | 是 | array | 上游消息列表，原样透传。 |
| `label` | 是 | object | 上游原始标签，原样透传；不是 SendLLM 的模型判断。 |
| `meta` | 否 | object | 上游扩展信息，原样透传。 |
| `annotation` | 是 | object | SendLLM 生成的模型标注结果。 |
| `annotation.method` | 是 | string | 成功自动标注时固定为 `auto`。 |
| `annotation.is_attack` | 是 | boolean | 是否为攻击或风险样本。 |
| `annotation.case_type` | 是 | string | 只允许 `typical`、`borderline`、`variant`、`hard_negative`。 |
| `annotation.explanation` | 是 | string | 中文解释，长度 10 到 70 个字符。 |
| `annotation.quality_score` | 否 | number | 模型自评质量分，取值范围为 0 到 1；越高表示模型对自己的判断越有把握。 |
| `annotation.extended_info` | 否 | object | 风险扩展字段。 |
| `annotation.extended_info.risk_type` | 条件必填 | string | `is_attack=true` 时应存在。 |
| `annotation.extended_info.risk_level` | 条件必填 | string | `is_attack=true` 时应存在，只允许 `low`、`medium`、`high`。 |

下游模块应以 `annotation` 作为 SendLLM 的模型判断结果，不要把顶层 `label` 当作模型输出。

## 下游处理建议

下游模块建议按以下规则处理：

1. 每行都应满足 `annotation.method="auto"`。
2. 使用 `id` 作为稳定样本 ID 和去重键。
3. 允许并忽略未知字段。SendLLM 会透传上游新增字段。
4. 不要依赖文件追加语义。SendLLM 每次导出都是按 SQLite 状态重新生成成功结果快照。
5. 如果某条失败样本后续补跑成功，它会在新的成功输出快照中出现。

## 校验 Schema

成功输出行可参考：

```text
config/output-jsonl-schema.json
```

该 Schema 用于约束 SendLLM 成功结果的稳定字段。下游可以在此基础上增加自己的业务校验。
