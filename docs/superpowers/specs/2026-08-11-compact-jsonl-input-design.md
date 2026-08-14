# Compact JSONL 输入对接设计

日期：2026-08-11
状态：已批准

## 目标

将 SendLLM 的输入起点从旧的 `trace_id`、`prompt`、`response` 顶层结构切换到上游 `compact_jsonl` 结构。程序只从 `messages` 中抽取提示词和回复送入模型判断，导出时把判断结果作为独立 `annotation` 子项合并回原记录，其他字段保持透传。

## 输入契约

每行输入必须是符合 `compact_jsonl.md` 和 `compact_jsonl.schema.json` 的 JSON 对象。SendLLM 固定依赖以下字段：

- `id`：作为内部稳定样本 ID，映射到现有持久化层的 `trace_id`。
- `messages`：按顺序抽取第一条 `role=user` 的 `content` 作为 `prompt`，第一条 `role=assistant` 的 `content` 作为 `response`。

如果没有抽取到非空 `prompt` 或 `response`，导入失败。`source`、`label`、`meta` 和未知字段不参与模型输入，但原始 JSON 会完整保留用于导出。

## 输出契约

成功导出记录保留原始输入对象，仅新增或覆盖顶层 `annotation`：

```json
{
  "annotation": {
    "method": "auto",
    "is_attack": true,
    "case_type": "typical",
    "explanation": "模型判断说明",
    "extended_info": {
      "risk_type": "jailbreak",
      "risk_level": "high"
    }
  }
}
```

失败导出记录保留原始输入对象，仅新增或覆盖顶层 `annotation`：

```json
{
  "annotation": {
    "method": "manual_required",
    "is_attack": null,
    "case_type": "",
    "explanation": "",
    "extended_info": {},
    "error_category": "rate_limited",
    "error_summary": "safe diagnostic summary",
    "attempts": 3
  }
}
```

导出不得删除或改写上游 `label`，不得把 SendLLM 判断字段铺到顶层。若原输入已有 `annotation`，SendLLM 生成的顶层 `annotation` 覆盖它，其他字段保持原样。

## 验证

- DTO 单元测试覆盖 compact 解析、消息抽取、原始字段保留和无有效消息拒绝。
- 导入测试覆盖 compact JSONL 写入 SQLite 时的内部 ID、prompt、response。
- 导出测试覆盖成功和失败记录均保留原始 `label`，并把判断结果写入嵌套 `annotation`。
- 更新输出 JSON Schema，使正式成功输出允许 compact 原字段并要求 `annotation.method=auto` 及模型判断字段位于 `annotation` 内。
