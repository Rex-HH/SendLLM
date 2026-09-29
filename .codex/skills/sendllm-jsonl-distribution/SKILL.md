---
name: sendllm-jsonl-distribution
description: Use when generating data distribution reports for SendLLM JSONL datasets, especially v2 response data, split summaries, or one-prompt-many-answer counts.
---

# SendLLM JSONL Distribution

为 SendLLM 仓库中的 JSONL 数据集生成聚合分布报告。该 skill 只做分布统计，不做数据清洗、质量审查或源文件修改。

## 使用方法

在仓库根目录运行：

```bash
python3 .codex/skills/sendllm-jsonl-distribution/scripts/generate_distribution.py \
  --input data/path/to/file.jsonl
```

默认在输入文件旁生成同名 `.distribution.md`。需要指定输出位置时添加 `--output`。运行后向用户报告生成路径、总行数、唯一 `trace_id` 数、唯一 prompt 数、最大单 prompt 回答数，以及主要 label 分布。

## 必须保留的报告口径

- 完整性：总行数、空行、JSON 解析错误、缺失/重复 `trace_id`、必备 `extended_info` 字段缺失或为 null。
- 一问多答：按 prompt 原文的 SHA-256 分组，报告唯一 prompt 数、回答数分布、`>1` 和 `>10` 组数、最大回答数、超额回答数。
- 主分布：label、language、case type、risk level、attack domain、attack method、attack scenario 空置情况、is attack、quality score、source、split、scene、annotation method。
- 交叉分布：label 与 language/case type/risk level/split，以及 unsafe 与 attack domain、risk level、attack domain。
- 输出文件不得包含原始 `prompt`、`response`、`explanation`、完整 trace_id 列表或其他可识别样本内容；只输出聚合计数、比例、枚举字段和 `<empty>` / `<non-empty>` 类别。
- 分布报告必须如实区分 `<missing>`、`<null>` 和 `<empty>`。若用户要求把数据中的缺失或 null 字段改为空字符串，先生成修正版数据文件，再重新生成报告；不得通过报告显示规则掩盖真实缺失。
- 报告必须包含 38 类覆盖检查，列出每个风险类别在 `attack_domain`、`attack_method` 和主分类中的数量，并标明是否存在。
- 报告必须体现 `unsafe` 中 `Attack Domain` 和 `Attack Method` 是否至少一个非空，同时输出至少一个非空和同时为空的数量。

如果用户只要求质量审查、清洗建议或样本泄露排查，改用 `dataset-quality-review`，不要用本 skill 替代。
