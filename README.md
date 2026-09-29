# SendLLM

SendLLM 是一个本地、单进程 Go CLI，用于把 JSONL 安全样本批量提交给兼容 OpenAI Chat Completions 协议的大模型，完成安全标注、失败补跑和综合裁决。它面向离线数据生产场景：任务可以中断、恢复、换配置调速，也可以在最终阶段导出给下游训练或质检模块使用的 JSONL 文件。

项目的核心原则很简单：SQLite 是唯一可信进度源，JSONL 文件只是导入和导出格式。已经写入 SQLite `succeeded` 状态的样本不会重复调用模型；输出文件可以删除后重新导出，但不能用输出文件替代状态库来判断任务进度。

## 功能概览

- 主标注流程：读取原始或 compact JSONL 样本，抽取待审查的 `prompt`、`response` 或 `messages`，请求模型输出结构化安全判断。
- 综合裁决流程：读取差异 JSONL，根据原始标签、模型标签、双方原因和 `messages` 内容，请最终大模型给出权威判断。
- SQLite 断点续跑：任务进度、尝试次数、错误分类、合法模型结果和审计载荷都持久化到本地 SQLite。
- 真实并发请求：按配置控制并发、RPM、TPM、退避、请求最大尝试次数和优雅退出时间。
- 失败处理：主流程结束后会自动进行一次保守失败补跑；裁决流程每次启动会把失败行重新置为待处理。
- 安全导出：成功与失败 JSONL 按输入顺序原子生成，失败文件保留可人工补标的安全占位字段。
- 安全日志：日志只输出任务 ID、计数、速率、ETA 和错误类别，不打印原始样本、完整模型输出或 API Key。

### Safety Review 的无 Gold 运行

Safety Review 的日常运行不要求 Hidden Gold。使用 `safety-review run --config <任务配置>` 即可让模型对新的未标注 JSONL 执行分类、清洗和导出；模型证据不足、策略覆盖不足、模型调用失败或多角色意见无法收敛的记录会进入 `quarantine`，不会被默认标记为 Safe。人工复核只是对隔离和抽样数据的后置升级通道，不是每条数据的前置条件。

Hidden Gold 只用于 `safety-review eval` 的准确率证明和最终生产验收。没有 Hidden Gold 时，运行结果可以使用，但不能声称通过 P04-B 真实验收；模型共识、伪 Gold 或自评结果也不能替代真实 Gold。

只有一个真实模型时可以使用 `config/safety-review-single-model.example.yaml` 的 `single_profile` 运营模式。五个角色仍分别使用各自的冻结 Prompt、Schema、阶段和尝试审计，并共享同一个 quota group；但多角色提示词隔离不等于模型独立性，单模型结果始终是 `acceptance_state=unvalidated`。正式 `eval` 只接受 `independent_profiles` 和真实 Hidden Gold。

少量数据试跑请看 [docs/quickstart-small-cleaning.md](docs/quickstart-small-cleaning.md)。该说明包含输入样例、单模型配置、`validate`、`run`、输出文件和提示词闭环迭代流程。

## 目录结构

```text
SendLLM/
├── config/                 # 示例任务配置、Schema、风险类型配置
├── prompts/                # 系统提示词模板
├── docs/                   # 设计文档和任务计划
├── internal/
│   ├── dao/                # SQLite 持久化、迁移、任务状态
│   ├── dto/                # JSONL、标注结果和模型协议结构
│   ├── facade/             # OpenAI-compatible HTTP 客户端
│   ├── lib/
│   │   ├── configs/        # YAML 配置加载、校验、语义指纹
│   │   ├── limiter/        # 并发、RPM、TPM、共享冷却
│   │   └── tokenizer/      # 保守 Token 估算
│   └── service/            # 导入、调度、重试、校验、导出、裁决
├── main.go                 # CLI 入口，只负责装配、信号和退出码
├── init.sh                 # 标准验证入口
└── README.md
```

## 构建与验证

需要 Go 1.24 或更高版本。

```bash
go build -trimpath -o ./sendllm .
./init.sh
```

`./init.sh` 会执行格式检查、普通测试、竞态检测和 `go vet`。默认测试使用本地模拟 OpenAI 服务，不消耗真实模型额度。

## 配置文件

建议从示例配置开始复制一份本地任务配置：

```bash
cp config/task.example.yaml config/task.yaml
```

配置中的相对路径都相对于配置文件所在目录解析。主要字段如下：

| 字段 | 说明 |
| --- | --- |
| `task.id` | 稳定任务 ID。同一个 SQLite 状态库内，语义配置不变时才能继续同一任务。 |
| `task.input` | 输入 JSONL 文件路径。 |
| `task.output` | 成功输出 JSONL 文件路径。失败文件会在同目录生成。 |
| `task.state` | SQLite 状态库路径，是任务进度和审计数据的唯一可信来源。 |
| `model` | OpenAI-compatible `base_url`、模型名、API Key 环境变量名或数组、结构化输出模式、采样参数和超时。 |
| `prompt` | 系统提示词、审查场景、风险类型闭集。 |
| `runtime` | 并发数、RPM、TPM、中断后的本地收尾超时。 |
| `retry` | 请求最大尝试次数、格式修复次数、退避范围。 |
| `output` | 结果 JSON Schema 和解释长度限制。 |

API Key 只从 `model.api_key_env` 或 `model.api_key_envs` 指定的环境变量读取。例如配置中写的是
`AI_GATEWAY_API_KEY` 时：

```bash
read -rs AI_GATEWAY_API_KEY
export AI_GATEWAY_API_KEY
```

不要把 Key 写入 YAML、SQLite、命令行、日志、README、提交记录或输出文件。

需要使用多个独立 Key 分摊请求时，可以把单个 `api_key_env` 改为数组，程序会按请求轮询这些环境变量：

```yaml
model:
  api_key_envs:
    - AI_GATEWAY_API_KEY_A
    - AI_GATEWAY_API_KEY_B
```

`api_key_env` 和 `api_key_envs` 二选一。Key 列表属于运行时凭据配置，不进入任务语义指纹；reconcile
运行日志只记录使用的环境变量名 `api_key_env`，不会记录 Key 值。

`model.structured_output` 必须显式配置：

- `json_schema`：把完整结果 Schema 发送给供应商，同时本地再校验一次。
- `json_object`：要求供应商返回 JSON 对象，本地仍按完整 Schema 校验。
- `prompt_only`：不发送 `response_format`，只靠提示词约束，本地仍按完整 Schema 校验。

`model.extra_body` 可透传供应商特有参数，但不能覆盖 `model`、`messages`、`response_format`、`stream` 等由客户端管理的核心协议字段。

## 主标注流程

主流程默认模式是 `annotate`，可以省略 `-mode`：

```bash
zsh -lic './sendllm -config ./config/task.yaml'
```

也可以显式指定：

```bash
zsh -lic './sendllm -mode annotate -config ./config/task.yaml'
```

启动后程序会导入输入 JSONL。相同任务再次启动时，已导入且内容一致的样本会跳过，遗留的 `processing` 会恢复为 `pending`，已成功样本不会再次请求模型。

进度日志示例：

```text
time=... level=INFO msg="task progress" task_id=... pending=... retrying=... succeeded=... failed=... rate=... eta=...
```

同一个成功数量可能在短时间内重复出现。这只是多个 Worker 同时汇报或进度轮询的结果，不代表同一条已成功数据被重复提交给模型。是否重复调用以 SQLite 中的样本状态为准。

## 主流程输入格式

项目支持两类输入。

### 传统输入

```json
{"trace_id":"sample-001","prompt":"待审查提示词","response":"可选回复"}
```

要求：

- `trace_id` 必填且非空。
- `prompt` 和 `response` 至少一个非空。
- 未识别字段会原样保留，并在导出时合并回结果记录。

### compact JSONL 输入

compact 输入使用 top-level `id` 作为稳定样本 ID，并从 `messages` 中抽取审查文本。

```json
{
  "id": "sample-001",
  "source": "dataset-name",
  "messages": [
    {"role": "user", "content": "用户消息"},
    {"role": "assistant", "content": "助手回复"}
  ],
  "label": {...},
  "meta": {...}
}
```

主流程会保留原始字段，不会把模型结果散落写到顶层，而是写入顶层 `annotation` 对象。详细格式见 `compact_jsonl.md` 和 `sendllm_output_jsonl.md`。

## 主流程输出格式

成功输出写入 `task.output`，按原输入顺序排列。

传统输入的输出会保留源字段，并写入模型生成的安全判断字段。compact 输入的输出会保留源字段，并新增或覆盖 `annotation`：

```json
{
  "id": "sample-001",
  "source": "dataset-name",
  "messages": [],
  "label": {},
  "meta": {},
  "annotation": {
    "method": "auto",
    "is_attack": true,
    "case_type": "typical",
    "explanation": "简短原因",
    "extended_info": {
      "risk_type": "xxx",
      "risk_level": "high"
    }
  }
}
```

最终失败记录会写入同目录失败文件。若成功输出是 `output.jsonl`，失败文件通常是 `output.failed.jsonl`。失败行保留原始字段，并写入人工补标占位：

```json
{
  "id": "sample-001",
  "annotation": {
    "method": "manual_required",
    "is_attack": null,
    "case_type": "",
    "explanation": "",
    "extended_info": {}
  },
  "error_category": "rate_limited",
  "error_summary": "safe diagnostic summary",
  "attempts": 8
}
```

失败文件中的错误信息是安全摘要，不包含原始 `prompt`、`response`、完整 `messages` 内容或模型原文。

## 综合裁决流程

裁决模式用于处理“原始标签”和“模型判断标签”不一致或需要最终确认的数据。运行方式：

```bash
zsh -lic './sendllm -mode adjudicate -config ./config/task.003.dark.adjudicate.yaml'
```

裁决输入通常来自 `sendllm_difference_jsonl.md` 描述的差异 JSONL。模型请求不会发送整条原始数据，只发送最终判断必需的信息：

- `messages`：最终判断的事实依据。
- `original_label`：原始标签中的 `risk_type`、`risk_level`、`case_type`、`is_attack`。
- `model_label`：模型判断标签中的 `risk_type`、`risk_level`、`case_type`、`is_attack`。
- `original_reason`：原始标签原因，通常来自 `meta.source_fields.reason`。
- `model_reason`：模型判断原因，通常来自 `annotation.explanation`。

裁决输出按照 `样本格式-8-4.md` 的下游格式生成，只根据最终大模型的综合判断写入结果字段。典型字段包括：

```json
{
  "trace_id": "sample-001",
  "source": "dataset-name",
  "split": "train",
  "language": "zh",
  "scene": "pair",
  "label": "unsafe",
  "prompt": "用户消息",
  "response": "助手回复",
  "explanation": "最终判断原因",
  "extended_info": {
    "risk_type": "xxx",
    "risk_level": "high",
    "case_type": "typical",
    "is_attack": true
  },
  "annotation": {
    "method": "auto"
  }
}
```

裁决模式现在也使用 SQLite 状态库：可以随时中断、重新启动、继续处理；已成功行不会重发，失败行会在下一次启动时重新进入待处理队列。

## 中断与续跑

按 `Ctrl+C` 或发送 `SIGTERM` 后，程序会停止领取新任务，并在 `runtime.shutdown_timeout` 内等待在途请求完成。之后会导出已经进入终态的记录并返回退出码 `130`。

主流程和裁决流程都支持中断后重新运行同一命令：

```bash
zsh -lic './sendllm -mode adjudicate -config ./config/task.003.dark.adjudicate.yaml'
```

续跑规则：

- `succeeded`：不会重新发送给模型。
- `processing`：启动时恢复为可处理状态。
- `failed`：主流程完成后自动执行一次保守补跑；裁决流程启动时会重置失败行并重跑。
- 输出文件：只是导出结果，可以删除后重新生成。
- SQLite 状态库：保存真实进度，删除它等于从头开始任务。

如果 `Ctrl+C` 没有反应，通常是进程正在等待在途 HTTP 请求或本地导出完成。不要用 `Ctrl+Z` 当作正常停止方式；`Ctrl+Z` 只是挂起进程，旧进程仍然可能占用资源，后续需要手动杀掉。

## 可调配置与语义指纹

SendLLM 会为任务计算语义指纹，用来防止“同一个状态库里混入不同业务含义的结果”。如果更换模型、提示词或 Schema 后还复用旧状态库，可能会看到：

```text
dao: task semantic hash mismatch
```

这是保护机制，不是数据损坏。

通常可以在同一状态库上调整：

- `runtime.concurrency`
- `runtime.requests_per_minute`
- `runtime.tokens_per_minute`
- `runtime.shutdown_timeout`
- `retry.initial_backoff`
- `retry.max_backoff`
- `task.output`

通常需要使用新的 `task.id` 或新的 `task.state`：

- `model.base_url`
- `model.name`
- `model.structured_output`
- `model.temperature`
- `model.top_p`
- `model.max_tokens`
- `model.seed`
- 系统提示词内容
- 审查场景 `prompt.scene`
- 风险类型闭集
- 输出 JSON Schema

调并发、RPM、TPM 后需要重启进程才会生效；配置文件不会热加载。重启不会重跑已成功样本。

## 失败补跑与换模型

主流程完成后，如果还有最终失败样本，程序会自动执行一次保守补跑：使用更低并发和更保守的 RPM，再给失败样本额外尝试机会。补跑只执行一次，避免稳定失败样本无限消耗额度。

如果当前模型太慢或某些失败样本想换模型处理，建议把未成功集合导出成新的输入数据集，然后使用新的任务 ID、状态库和输出路径运行新模型。导出集合应以 SQLite 状态为准，条件通常是：

```sql
select raw_json
from items
where task_id = 'task_id_here'
  and state <> 'succeeded'
order by input_index;
```

换模型时不要复用旧状态库。旧任务和新任务可以输出到不同文件，最后再按 `trace_id` 或 `id` 做合并。

## 模型连通性测试

默认测试不会调用真实模型。如需检查真实网关是否可达，可以显式打开连通性测试：

```bash
SENDLLM_MODEL_CONNECTIVITY=1 \
SENDLLM_CONNECTIVITY_CONFIG=./config/task.yaml \
zsh -lic 'go test ./internal/facade -run "^TestOpenAI_LiveConnectivity$" -count=1 -v'
```

这个测试只读取配置中的模型连接参数并发送一个极小 JSON ping，不导入输入文件、不写 SQLite、不生成结果 JSONL，也不会打印 API Key 或模型原文。

## 并发与限速建议

`runtime.concurrency` 控制真实同时在途请求数，`requests_per_minute` 控制本地 RPM，`tokens_per_minute` 控制本地 TPM。配置为 `0` 表示不启用对应的本地主动限速器，但不代表供应商没有配额。

实际吞吐由三者共同决定：

```text
可持续速率 = min(
  并发数 / 平均响应耗时,
  RPM 限额,
  TPM 限额 / 平均单次 Token 消耗
)
```

建议从较低并发开始观察 429、502、响应耗时和失败率，再逐步调高。出现大量 `rate_limited` 时，优先降低并发或 RPM；出现 `finish_reason=length`、空内容或格式不完整时，优先检查 `max_tokens` 是否过低。

## 状态、审计与安全

SQLite 状态库会保存：

- 任务语义指纹和归一化配置摘要。
- 输入顺序、原始 JSON、样本状态、尝试次数、下一次重试时间。
- HTTP 状态、错误分类、校验错误、Token 用量。
- 通过校验的结构化标注。
- 有界保留的模型原始响应，用于本地审计。

状态库可能包含敏感数据，应按原始数据集同等级别保护。不要提交以下内容：

- 输入数据集。
- 输出 JSONL。
- 失败 JSONL。
- SQLite 状态库。
- 模型原始响应。
- `.env`。
- API Key 或 Authorization 信息。

## 退出码

| 退出码 | 含义 |
| --- | --- |
| `0` | 所有导入记录均成功。 |
| `1` | 配置、密钥、输入、存储、认证或其他任务级错误。 |
| `2` | 处理完成，但仍存在最终失败记录。 |
| `130` | 任务未完成时被中断。 |

## 常见问题

### 修改配置后需要关闭进程重启吗？

需要。当前版本不热加载配置。修改并发、RPM、TPM、输出路径或重试参数后，停止当前进程并重新运行命令即可。SQLite 会保证已成功样本不重跑。

### 输出文件已经存在，重跑会冲突吗？

不会以输出文件作为进度源。程序会从 SQLite 终态重新导出成功和失败文件，并使用临时文件加原子替换。已成功数据不会因为输出文件存在而重复请求模型。

### 失败文件里的样本后来成功了，失败文件会不会还保留旧失败？

最终导出会按 SQLite 当前终态重新生成。某条失败样本补跑成功后，会进入成功输出；失败文件中不应再保留它的最终失败记录。

### 为什么 `pending` 有时候会增长？

常见原因是重试或恢复动作把 `retry_wait`、`processing`、`failed` 重新放回待处理队列。它表示调度状态变化，不代表新增了输入数据。

### 为什么成功数量重复打印？

进度日志可能由多个 Worker 完成、状态更新或定时汇报触发，短时间内出现相同 `succeeded` 是正常现象。是否重复送模型以 SQLite 中每条样本的状态和尝试记录为准。

### 更换模型后报 semantic hash mismatch 怎么办？

使用新的 `task.id` 和新的 `task.state`。模型、提示词、Schema、风险类型、结构化输出模式都属于语义配置，不能和旧结果混在同一个状态库中。

### 可以删除输出文件重新跑吗？

可以删除输出文件并重新运行同一任务来重新导出。但不要删除 SQLite 状态库，除非你明确想从头开始并重新调用模型。

## 广告数据轻量复核

使用独立模式复核 `data/ad/advertisement_dataset_final.json`，不会修改原始文件，也不会与 `data/final` 合并：

```bash
./sendllm -config config/task.advertisement-review.yaml -mode advertisement-review-batch
```

模型只收到批内短序号、原始 `prompt` 和原始 `attack_scenario`；不会收到原 `trace_id`、原标签、旧回复、解释、来源、质量分或广告大类。SQLite、问题清单、报告和两个分区始终使用原始 `trace_id`，批内短序号只在一次请求内有效。

输出目录会生成四个独立文件：

- `clean.original.jsonl`：模型确认无问题的原记录，除 JSON 数组转 JSONL 外不变。
- `issues.original.jsonl`：标签不一致、uncertain、旧 38 类重叠、小类可疑、请求失败或格式失败的原记录。
- `issues.manifest.jsonl`：只包含原 `trace_id`、问题代码和候选旧风险。
- `review-report.json`：聚合计数和分布，不包含 prompt、回复、解释或模型原文。

同一任务重跑时以 SQLite 为真相源，已成功记录不会重复调用。退出码 `0` 表示全部记录取得有效复核结论，即使其中一些因语义问题进入问题池；`2` 表示存在最终请求或格式失败；其他非零表示任务级错误。API Key 只从 `AI_GATEWAY_API_KEY` 读取，不要写入配置、日志或提交产物。

## 定向人工/强模型清洗准备

`cmd/advertisement-clean-prep` 是本地的清洗准备命令，不调用模型，也不修改源数据或 provisional 文件。它复用已完成的 512 条人工 worksheet 结论，排除已审核 `trace_id`，生成 P0/P1 队列和最多 80,000 Token 的盲化批次：

```bash
go run ./cmd/advertisement-clean-prep prepare \
  -input data/ad/advertisement_dataset_final.json \
  -worksheet /private/tmp/feat039-pilot-qwen3.5-flash-20260921190946/run/human-review-worksheet.csv \
  -output-dir data/ad/directed-cleaning-prep \
  -max-batch-tokens 80000
```

`prepare` 产物包括：

- `p0.queue.jsonl`：未审核的 THUCNews/Wikipedia unsafe 记录。
- `p1.queue.jsonl`：未审核的 ChineseSafe unsafe 分层样本。
- `review-batches/batch-NNN.json`：模型输入只包含 `{"items":[{"i":0,"p":"..."}]}`。
- `review-batch-map.jsonl`：本地保存批内 `i` 到原 `trace_id` 的映射。
- `decisions.jsonl`：初始为空，后续每行追加一个 `{"r":[{"i":0,"l":1,"t":"news_context"}]}`。
- `review-result.schema.json`：严格输出 Schema，`t` 只能是 `news_context`、`actual_ad`、`quoted_ad`、`insufficient`。

第一遍 `route` 只把 `l=0` 或 `l=1` 的记录送入第二遍，`l=2` 保持原标签：

```bash
go run ./cmd/advertisement-clean-prep route \
  -queue data/ad/directed-cleaning-prep/queue.jsonl \
  -batch-dir data/ad/directed-cleaning-prep/review-batches \
  -mapping data/ad/directed-cleaning-prep/review-batch-map.jsonl \
  -decisions data/ad/directed-cleaning-prep/decisions.jsonl \
  -output-dir data/ad/directed-cleaning-prep/second-pass
```

最后使用第二遍最终 decisions 生成新版本。`apply` 只把最终 `l=1` 的记录从 unsafe 改为 safe，拒绝新增、删除、重复或改写 `trace_id`，未确认记录保持原值，并默认拒绝覆盖源文件和 provisional 文件：

```bash
go run ./cmd/advertisement-clean-prep apply \
  -input data/ad/advertisement_dataset_final.json \
  -output data/ad/directed-cleaning-prep/advertisement_dataset.cleaned-v1.json \
  -batch-dir data/ad/directed-cleaning-prep/second-pass/review-batches \
  -mapping data/ad/directed-cleaning-prep/second-pass/review-batch-map.jsonl \
  -decisions data/ad/directed-cleaning-prep/second-pass/decisions.jsonl
```

所有准备命令只输出队列数量、批次数量、Token 分布、ID 唯一性和 SHA-256 等聚合信息，不打印 prompt。

## 广告数据全量级联清洗

`cmd/advertisement-full-clean` 实现 `feat-040` 的本地两层全量清洗边界。它不覆盖广告源、task-013 权威基线或旧 provisional，并在真实 pilot 前要求人工批准。

生成冻结校准集和 holdout pilot 输入：

```bash
go run ./cmd/advertisement-full-clean calibration \
  -source data/ad/advertisement_dataset_final.json \
  -worksheet /private/tmp/feat039-pilot-qwen3.5-flash-20260921190946/run/human-review-worksheet.csv \
  -layer1-mapping data/ad/directed-cleaning-prep/review-batch-map.jsonl \
  -layer1-decisions data/ad/directed-cleaning-prep/decisions.jsonl \
  -layer2-mapping data/ad/directed-cleaning-prep/second-pass-strong-v1/review-batch-map.jsonl \
  -layer2-decisions data/ad/directed-cleaning-prep/second-pass-strong-v1/decisions.jsonl \
  -output-dir data/ad/full-cleaning
```

校准集只包含 `trace_id`、人工 label、已知 overlap 真值和 split；265 条定向强审不伪造 overlap 真值。holdout pilot 输入位于 `data/ad/full-cleaning/pilot/layer1.input.jsonl`。

第一层使用冻结配置 `config/task.advertisement-full-review.qwen3.5-plus.yaml`：

```bash
go run ./cmd/advertisement-full-clean run \
  -config config/task.advertisement-full-review.qwen3.5-plus.yaml \
  -report data/ad/full-cleaning/pilot/layer1-report.json
```

第一层终态后由本地 route 生成只含 `i,p` 的第二层输入，第二层使用 `config/task.advertisement-full-review.deepseek-v4-pro.yaml`，再执行 `adjudicate`、`apply` 和 `replace`。`apply` 只处理 approved manifest；`replace` 只替换旧 provisional 中匹配的广告 `trace_id` 并写新文件。模型请求和公开 report 均不包含 prompt。

本地门禁只验证 fake provider，不代表模型质量。真实 `qwen3.5-plus` pilot 必须由用户明确批准后执行；未通过 holdout 阈值前不得启动 58,658 条全量任务。

## 开发与验证

常用验证命令：

```bash
go test ./...
go test -race ./...
go vet ./...
./init.sh
```

项目最终真实验收要求见 `AGENTS.md` 的 `Final Live Acceptance`：真实模型端到端测试必须读取仓库根目录 `Test_Input.jsonl`，生成 `Test_Output.jsonl`，并证明输入输出均为 50 条、`trace_id` 集合一致、Schema 校验通过、失败记录数为 0。

## 相关文档

- `docs/superpowers/specs/2026-08-05-sendllm-design.md`：项目批准设计。
- `compact_jsonl.md`：compact JSONL 输入格式。
- `sendllm_output_jsonl.md`：主流程输出格式。
- `sendllm_model_output_jsonl_format.md`：模型输出 JSONL 格式说明。
- `sendllm_difference_jsonl.md`：裁决流程差异输入格式。
- `样本格式-8-4.md`：下游权威样本格式。
- `模型配置.md`：最终真实验收使用的模型连接配置说明。
