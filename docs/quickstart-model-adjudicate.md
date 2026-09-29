# SendLLM 快速上手：从配置到运行

本文面向没有接触过本项目的同学，只介绍三个功能：

1. 模型标记
2. 差异数据审核
3. 原始标签复核

所有命令都在仓库根目录执行。

## 1. 开始前要准备什么

一个任务主要涉及三类文件：

| 类型 | 作用 |
| --- | --- |
| YAML 配置 | 指定输入、输出、模型、提示词、并发、重试等 |
| 提示词 `.txt` | 告诉模型如何输出标注结果 |
| 输入 JSONL | 需要处理的原始数据 |

配置文件和提示词都放在仓库内，不需要每次手动传很多命令行参数。

## 2. 配置和提示词写在哪里

### 2.1 复制一个配置开始

建议先复制示例配置，再改成自己的任务：

```bash
cp config/task.example.yaml config/my-annotate.yaml
```

之后编辑 `config/my-annotate.yaml`。

### 2.2 必须关注的配置段

最核心的是 `task`：

```yaml
task:
  id: my-task-001
  input: ../data/my-task/input.jsonl
  output: ../data/my-task/output.jsonl
  state: ../data/my-task/state.db
```

字段说明：

| 字段 | 作用 |
| --- | --- |
| `id` | 任务 ID，恢复任务时使用 |
| `input` | 输入 JSONL |
| `output` | 成功结果 JSONL |
| `state` | SQLite 状态库，记录哪些样本已经完成 |

其次是 `model`、`prompt`、`runtime`、`retry`、`output`。完整字段见下面“配置项速查”。

### 2.3 提示词在哪里改

提示词路径写在 `prompt.system_file`。

当前项目里常用提示词文件：

| 功能 | 提示词文件 |
| --- | --- |
| 模型标记 | `prompts/my-batch-prompt-only-system.txt` |
| 差异数据审核 | `prompts/adjudicate-system.txt` |
| 原始标签复核 | `prompts/reconcile-system.txt` |

直接编辑这些 `.txt` 文件即可。提示词文件必须包含：

```text
{{RISK_TYPES}}
{{RESULT_SCHEMA}}
```

这两个占位符各出现一次，程序运行时会自动替换成风险类型和结果 Schema。

风险类型和结果 Schema 也可以改：

| 内容 | 配置项 | 当前文件 |
| --- | --- | --- |
| 风险类型 | `prompt.risk_types_file` | `config/risk-types.yaml` |
| 模型结果 Schema | `output.schema_file` | `config/result-schema-v2.json` |

### 2.4 配置项速查

| 配置段 | 常用字段 | 作用 |
| --- | --- | --- |
| `task` | `id` | 任务 ID |
| `task` | `input` | 输入 JSONL |
| `task` | `output` | 成功 JSONL 输出 |
| `task` | `state` | SQLite 状态库 |
| `model` | `base_url` | 模型服务地址 |
| `model` | `api_key_env` | API Key 环境变量名 |
| `model` | `name` | 模型名称 |
| `model` | `structured_output` | `json_schema`、`json_object` 或 `prompt_only` |
| `model` | `temperature` | 采样温度 |
| `model` | `top_p` | 采样 top_p |
| `model` | `max_tokens` | 最大生成 token 数 |
| `model` | `seed` | 采样种子 |
| `model` | `timeout` | 单次请求超时 |
| `model` | `extra_body` | 透传给供应商的额外参数 |
| `prompt` | `system_file` | 系统提示词文件 |
| `prompt` | `scene` | `prompt`、`response`、`pair` 或 `auto` |
| `prompt` | `risk_types_file` | 风险类型闭集文件 |
| `runtime` | `concurrency` | 普通请求并发数 |
| `runtime` | `requests_per_minute` | 普通请求 RPM |
| `runtime` | `tokens_per_minute` | 普通请求 TPM |
| `runtime` | `shutdown_timeout` | Ctrl+C 后在途请求等待时间 |
| `runtime` | `cover_concurrency` | 失败补跑并发数 |
| `runtime` | `cover_requests_per_minute` | 失败补跑 RPM |
| `retry` | `request_max_attempts` | 每条记录最大请求次数 |
| `retry` | `format_repair_attempts` | 格式错误后额外修复次数 |
| `retry` | `initial_backoff` | 首次重试退避时间 |
| `retry` | `max_backoff` | 最大重试退避时间 |
| `output` | `schema_file` | 结果 Schema 文件 |
| `output` | `explanation_min_length` | 解释最短长度 |
| `output` | `explanation_max_length` | 解释最长长度 |

### 2.5 哪些配置修改后需要换任务

以下语义配置变化时，不要继续使用旧的 `task.state`：

- 模型名
- 系统提示词
- 风险类型文件
- 结果 Schema
- `structured_output`
- `max_tokens`
- 其他会影响模型输出的模型参数

需要改成新的 `task.id` 和新的 `task.state`。`runtime`、`retry` 和输出路径通常可以在同一个状态库上调整。

## 3. 构建程序

改过 Go 代码后，需要重新构建：

```bash
go build -trimpath -o ./sendllm .
```

如果只改 YAML 或提示词，不需要重新构建。

## 4. 运行模型标记

模型标记用于读取普通 JSONL 输入，调用大模型生成安全标注，并输出带 `annotation` 的结果。

当前可用配置示例：

```text
config/task.005.retry-5000.yaml
```

运行命令：

```bash
zsh -lic './sendllm -config ./config/task.005.retry-5000.yaml'
```

默认模式就是 `annotate`，可以省略 `-mode`。

### 4.1 输入输出位置

输入、输出、状态库都在 YAML 的 `task` 段定义：

```yaml
task:
  id: task-005-retry-5000
  input: ../data/task-005-retry-5000/input.jsonl
  output: ../data/task-005-retry-5000/output.jsonl
  state: ../data/task-005-retry-5000/state.db
```

对应实际文件：

| 类型 | 位置 |
| --- | --- |
| 输入 | `data/task-005-retry-5000/input.jsonl` |
| 成功输出 | `data/task-005-retry-5000/output.jsonl` |
| 失败输出 | `data/task-005-retry-5000/output.failed.jsonl` |
| 进度状态库 | `data/task-005-retry-5000/state.db` |

输入 JSONL 每一行是原始样本。程序会保留原始字段，并在顶层新增或覆盖 `annotation`。

## 5. 运行差异数据审核

差异数据审核用于处理原始标签和模型标签不一致、需要最终模型确认的数据。

当前可用配置示例：

```text
config/task.003.dark.adjudicate.yaml
```

运行命令：

```bash
zsh -lic './sendllm -mode adjudicate -config ./config/task.003.dark.adjudicate.yaml'
```

这里必须显式写 `-mode adjudicate`。

### 5.1 输入输出位置

```yaml
task:
  id: task-003-dark-adjudicate
  input: ../data/task-003-dark-adjudicate/sendllm_differences.jsonl
  output: ../data/task-003-dark-adjudicate/final_8_4.jsonl
  state: ../data/task-003-dark-adjudicate/task-003-dark-adjudicate.db
```

对应实际文件：

| 类型 | 位置 |
| --- | --- |
| 输入 | `data/task-003-dark-adjudicate/sendllm_differences.jsonl` |
| 成功输出 | `data/task-003-dark-adjudicate/final_8_4.jsonl` |
| 失败输出 | `data/task-003-dark-adjudicate/final_8_4.failed.jsonl` |
| 进度状态库 | `data/task-003-dark-adjudicate/task-003-dark-adjudicate.db` |

差异审核输入必须包含 `original_label` 和 `model_label`。输出是下游 `样本格式-8-4` 结构，不是普通模型标记的 `annotation` 结构。

## 6. 运行原始标签复核

原始标签复核从模型标记任务的原始输入开始，请模型重新判断标签，并与原始标签按字段合并，最终输出修改后的 `样本格式-8-4` 新标签结构。

当前可用配置示例：

```text
config/task.reconcile.example.yaml
```

运行命令：

```bash
zsh -lic './sendllm -mode reconcile -config ./config/task.reconcile.example.yaml'
```

这里必须显式写 `-mode reconcile`。

### 6.1 输入输出位置

```yaml
task:
  id: reconcile-example
  input: ../data/reconcile-example/input.jsonl
  output: ../data/reconcile-example/final.jsonl
  state: ../data/reconcile-example/state.db
```

对应实际文件：

| 类型 | 位置 |
| --- | --- |
| 输入 | `data/reconcile-example/input.jsonl` |
| 成功输出 | `data/reconcile-example/final.jsonl` |
| 失败输出 | `data/reconcile-example/final.failed.jsonl` |
| 进度状态库 | `data/reconcile-example/state.db` |
| 运行差异日志 | `data/reconcile-example/final.reconcile-log.jsonl` |

输入复用模型标记任务的 compact JSONL。程序会用 `source.path` 的 38-Categories 目录桶映射原始风险标签，并和模型结果逐字段比较。

每次运行会覆盖生成一份新的 `reconcile-log.jsonl`，按原始输入顺序每行一条。成功且一致标记为 `succeeded_consistent`，成功但有变化标记为 `succeeded_changed` 并只记录字段前后对比；其他状态只记录当前流程状态。每行带 `run_status`，中断时为 `interrupted`。

## 7. 关键注意事项

### 7.1 不要用错 mode

- 普通标注输入没有 `original_label` / `model_label`，用默认 `annotate` 模式。
- 差异审核输入必须有 `original_label` / `model_label`，用 `adjudicate` 模式。

普通输入错误使用 `adjudicate` 时，会报 `required adjudicate fields are missing`。

### 7.2 SQLite 是进度源

`output.jsonl` 和失败文件只是导出结果，可以删除后重新导出。真正决定哪些样本已经成功的是 `state.db`。

### 7.3 semantic hash mismatch

同一个 `task.id` 使用同一个 `task.state` 时，语义配置不能变。如果必须改语义配置，请同时换新的 `task.id` 和新的 `task.state`，不要手动修改数据库里的 hash。

### 7.4 API Key

配置里只写环境变量名：

```yaml
api_key_env: AI_GATEWAY_API_KEY
```

运行前确认环境变量存在：

```bash
test -n "$AI_GATEWAY_API_KEY"
```

不要打印或提交 Key 的值。

### 7.5 自动重试配置

每条记录的请求重试在 `retry` 段：

```yaml
retry:
  request_max_attempts: 10
  format_repair_attempts: 2
  initial_backoff: 1s
  max_backoff: 20s
```

一轮结束后对失败记录的保守补跑限速在 `runtime` 段：

```yaml
runtime:
  cover_concurrency: 1
  cover_requests_per_minute: 6
```

### 7.6 Ctrl+C 中断与继续

任务运行中可以按 `Ctrl+C`。程序会停止领取新任务，并在 `shutdown_timeout` 内等待在途请求结束，然后导出当前已经成功的记录。

中断后重新运行同一条命令即可继续：

```bash
zsh -lic './sendllm -config ./config/task.005.retry-5000.yaml'
```

或：

```bash
zsh -lic './sendllm -mode adjudicate -config ./config/task.003.dark.adjudicate.yaml'
```

恢复规则：

- 已 `succeeded` 的样本不会重复请求模型。
- 残留的 `processing` 会恢复为待处理。
- 待处理和等待重试的样本会继续处理。
- 最终失败样本会按对应模式继续补跑或重新裁决。

未完成时中断，命令通常返回退出码 `130`；重新执行即可续跑。
