# 少量数据清洗快速开始

这份说明用于先拿 5-20 条数据试跑 Safety Review 清洗流程。程序流程内不需要人工逐条标记；模型直接给出 `clean` 或 `quarantine`，无法稳定判断的记录进入 `quarantine`，不会默认改成 Safe。

## 1. 准备输入

输入文件是一行一个 JSON 对象。最少需要：

```json
{"trace_id":"small-001","scene":"response","prompt":"用户问题","response":"模型回复"}
```

字段说明：

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `trace_id` | 是 | 唯一样本 ID，重跑时不能变 |
| `scene` | 是 | `prompt` 或 `response` |
| `prompt` | 视场景 | Prompt 审核时必须有值 |
| `response` | 视场景 | Response 审核时必须有值 |

`scene` 必须与配置文件里的 `task.scene` 一致。一次 Safety Review 任务只能处理一个 scene；如果数据同时包含 `prompt` 和 `response`，请拆成两个输入文件并分别建任务。

可以直接复制仓库中的合成样例：

```bash
cp testdata/small-cleaning.example.jsonl ./small-input.jsonl
```

## 2. 配置模型

从单模型示例开始：

```bash
cp config/safety-review-single-model.example.yaml ./config/small-cleaning.yaml
```

把 `config/small-cleaning.yaml` 改成你的本地路径，例如：

```yaml
task:
  id: small-cleaning-001
  input: ../small-input.jsonl
  task_dir: ../runs/small-cleaning-001
  scene: response
policy:
  bundle_dir: ../policy/releases/p04b-v1.0
```

单模型配置里的五个角色和共享 quota 默认可以设为 `concurrency: 4`。如果全设为 `1`，所有角色会串行执行，3 条数据也可能等十几分钟；并发 4 更适合少量数据交互式试跑。

单模型模式足够做清洗试跑，但结果会标记为 `acceptance_state=unvalidated`。如果需要正式准确率验收，应使用 `config/safety-review-eval.example.yaml` 的 `independent_profiles` 配置和 Hidden Gold。

API Key 只从配置指定的环境变量读取，不要写进 YAML：

```bash
test -n "$AI_GATEWAY_API_KEY"
```

## 3. 先校验，不调用模型

```bash
go run . safety-review validate --config ./config/small-cleaning.yaml
```

看到 `validation=PASS` 后再运行。

## 4. 运行清洗

```bash
go run . safety-review run --config ./config/small-cleaning.yaml
```

运行时会输出关键阶段日志，但不打印样本内容：

```text
task_id=small-cleaning-001 status=preflight added=3 skipped=0
stage trace_id=small-clean-001 role=judge_a stage=judge:a state=running profile=operational
stage trace_id=small-clean-001 role=judge_a stage=judge:a state=succeeded profile=operational duration=25.3s error=
task_id=small-cleaning-001 status=running
task_id=small-cleaning-001 status=exporting
task_id=small-cleaning-001 status=completed added=3 skipped=0 decisions=3
```

单次模型调用通常需要几十秒；3 条数据也不是只调用 3 次模型，而是会经过 Judge A、Judge B、Router、Expert、Arbiter 等多个阶段。并发为 1 时这些阶段会串行，因此少量数据也可能耗时较长。

运行目录中会生成：

| 文件 | 说明 |
| --- | --- |
| `clean.jsonl` | 模型给出确定结论的样本 |
| `quarantine.jsonl` | 证据不足、策略缺口、模型失败或分歧样本 |
| `audit.jsonl` | 不含原始 Prompt/Response 和原始模型输出的审计摘要 |
| `quality-events.jsonl` | 可用于后续 Prompt/Policy 优化闭环的质量事件 |
| `report.json` | 聚合统计 |
| `run-status.json` | 最终或中断状态 |

SQLite 状态库是唯一进度源。中断后重新执行同一条命令会继续运行，不会重复调用已经成功的样本。

## 5. 查看结果

```bash
wc -l ./runs/small-cleaning-001/clean.jsonl
wc -l ./runs/small-cleaning-001/quarantine.jsonl
```

`quarantine.jsonl` 不应被当作失败数据删除。它表示当前 Prompt/Policy 无法稳定判断，适合用于下一轮提示词修订和回归。

## 6. 诊断每条数据的阶段判断

如果不满意最终标签，可以查看每个角色阶段的判断。该命令只输出结构化状态，不输出 Prompt、Response、evidence span、rationale 或原始模型输出：

```bash
go run . safety-review explain \
  --task-dir ./runs/small-cleaning-001 \
  --trace-id small-clean-001
```

也可以不指定 `--trace-id`，一次输出任务内所有样本的阶段判断：

```bash
go run . safety-review explain --task-dir ./runs/small-cleaning-001
```

输出是 JSONL，每一行对应一个阶段。重点看：

| 字段 | 含义 |
| --- | --- |
| `role` / `stage_key` | 当前是 Judge A、Judge B、Router、Expert 还是 Arbiter |
| `state` | 该阶段成功、失败或隔离 |
| `verdict` | 该角色自己的判断 |
| `attack_methods` / `attack_domains` | Judge 或 Arbiter 识别出的风险类别 |
| `method_candidates` / `domain_candidates` | Router 给出的候选，不是最终结论 |
| `conditions` / `decisive_exclusions` | Expert 对规则条件与排除项的状态 |
| `primary_*` / `case_type` / `quarantine_reason` | Arbiter 最终投影结果 |

## 7. 闭环修订提示词后重跑

推荐闭环是：

```text
模型标记
  -> 人工/强模型离线复核少量结果
  -> 修改提示词或 Change Set
  -> 自动 compile
  -> fresh Safety Review regression
  -> release
  -> 再次模型标记
```

程序内不需要人工逐条标记。离线复核产物通过 Change Set 或 Prompt 修订进入程序，随后由 `policy-optimizer analyze` / `compile` / `regression` 自动执行。

真正开始新一轮前，不要改旧任务的语义配置并复用同一个 SQLite。请使用新的 `task.id`、`task_dir` 和 clean/quarantine 输出目录。
