---
name: dataset-quality-review
description: Use when reviewing SendLLM JSONL datasets for quality issues such as missing required fields, duplicate IDs, exact duplicate rows, is_attack/attack_method inconsistency, incomplete 38-risk-category coverage, explanation defects, or one-prompt-many-answer skew.
---

# SendLLM 数据集质量审查

对 SendLLM 的 JSONL 数据集做质量审查。审查只输出聚合计数、字段枚举、`trace_id` 和必要的解释片段，不得输出原始 `prompt`、`response`、完整样本或凭据。

## 基本规则

- 本 skill 只做质量检查和报告，不得修改输入数据，也不得生成派生数据集。
- 数据修复必须先由用户查看报告并明确下达指令，再在用户确认后另行执行。
- 每次报告必须使用中文。
- 先量化问题，再给出保留、隔离或修正建议。
- 对模糊样本优先进入复核池，不要静默修改。

## 必查项

1. JSONL 完整性：
   - 行数、空行、JSON 解析错误、非对象行
   - 缺失或重复 `trace_id`
2. 契约一致性：
   - `scene` 是否与请求或文件名推断一致
   - `annotation.method` 是否为 `auto`
   - `quality_score` 是否存在且在 `0..1`
3. 攻击一致性：
   - `is_attack=true` 时必须存在非空 `attack_method`
   - `is_attack=false` 时 `attack_method` 必须为空
   - 不检查 `label` 与 `is_attack` 的一致性，`label` 不能用于推断 `is_attack`
4. 风险类别覆盖：
   - 以 `config/risk-types.yaml` 的 38 类风险闭集为准
   - 报告已覆盖、缺失的风险类别和缺失数量
5. unsafe 必备字段：
   - `case_type` 不能为空
   - `risk_level` 不能为空
   - `attack_domain` 和 `attack_method` 不能同时为空
6. 全量字段：
   - 任意数据行的 `case_type` 都不能缺失或为空
7. safe 风险字段：
   - `label=safe` 时 `risk_level`、`attack_domain`、`attack_method` 都不能有值
8. 解释质量：
   - 长度必须在 10 到 70 个字符
   - 不得包含换行
   - 不得包含来源、血缘或内部处理语言
   - 只在明确说“模型/回复/AI/助手拒绝了请求”时才作为安全语义矛盾候选
9. 重复：
   - `(prompt,response,label)` 完全重复只保留一条
10. 一问多答：
   - 报告 `>1` 和 `>10` 的 prompt 组数、最大回答数、超额行数
   - 只报告问题和影响，不自动截断或改写数据

## 明确不算问题

- `label=unsafe` 但 `is_attack=false` 本身不是错误。
- 解释中出现“口径”不算流水线术语，因为常用于描述风险内容中的对外话术。
- `attack_scenario` 非空不算质量问题。
- “拒绝提供”“避免风险”“无风险提示”等普通风险描述不算解释矛盾；只有明确描述模型安全拒绝时才进入候选。
- “正例”“负例”“硬负例”等术语按业务说明暂不处理。

## 使用脚本

```bash
python3 .codex/skills/dataset-quality-review/scripts/review_jsonl.py \
  --input data/path/to/file.jsonl \
  --scene auto \
  --out-dir /tmp/dataset-quality-review
```

脚本默认读取 `config/risk-types.yaml` 作为风险闭集；如需显式指定，可添加 `--risk-types-file`。

`--out-dir` 只用于存放质量报告，不会生成 JSONL 数据文件。

## 报告结论

报告必须包含：

- 可直接使用或需要隔离的判断
- 关键问题数量
- 建议的修复方式和影响范围
- 精确报告文件路径
- 使用的检查或修复命令
