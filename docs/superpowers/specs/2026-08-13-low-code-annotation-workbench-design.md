# SendLLM 低代码数据集标注工作台未来设计

日期：2026-08-13
状态：讨论稿
适用范围：未来从当前 SendLLM 本地 Go CLI 演进为单机优先、可恢复、低代码、带前端页面的通用 JSONL 大模型标注工具。

## 1. 背景

SendLLM 当前版本已经完成本地单进程 CLI 的核心能力：读取 JSONL 数据集，调用 OpenAI-compatible 大模型，使用 SQLite 持久化任务状态，支持中断恢复、限速、重试、格式修复、失败 cover、确定顺序导出和安全日志约束。

未来目标是在保留这些能力的前提下，把 SendLLM 改造成一个通用低代码工具。用户可以在前端上传 JSONL 数据集和结构定义文件，通过页面选择字段、配置提示词、配置模型、配置模型输入结构、配置模型输出结构和最终输出格式，然后一键启动任务，系统自动完成标注并导出目标 JSONL 文件。

本设计不是当前首版 CLI 的范围扩张，也不要求立即实现 HTTP 服务、分布式队列、多实例协调或前端工程。它用于未来有时间时，指导 SendLLM 从 CLI 平滑演进为完整低代码标注工作台。

## 2. 产品目标

构建一个单机优先的低代码数据集标注工作台，使用户能通过页面完成以下流程：

1. 导入 JSONL 输入数据集。
2. 导入或生成输入结构定义文件。
3. 系统识别数据字段结构并展示为字段树。
4. 用户选择需要送入大模型的字段。
5. 用户配置模型输入内容结构。
6. 用户编辑提示词。
7. 用户配置 OpenAI-compatible 模型参数。
8. 用户配置模型输出结构文件。
9. 用户配置最终输出格式文件。
10. 用户一键启动任务。
11. 后端并发执行模型调用、校验、重试、恢复和导出。
12. 最终生成满足目标格式的 JSONL 输出文件。

首个低代码版本仍然优先单机部署。高可用能力聚焦于本地进程可重启、任务可恢复、SQLite 状态可靠、成功样本不重复调用和导出可再生成，而不是分布式多写者。

## 3. 非目标

第一阶段不实现以下能力：

- 不做多租户权限系统。
- 不做分布式 Worker 集群。
- 不引入 Kafka、Redis、RabbitMQ 或外部消息队列。
- 不把 SQLite 改成多实例并发写入数据库。
- 不实现任意脚本执行、用户自定义 JavaScript、jq 或 JSONata。
- 不把低代码能力设计成插件系统。
- 不让数据库表结构随每个数据集动态建表或动态改表。
- 不在日志、事件、错误响应中输出原始 prompt、response、完整模型回复或 API Key。

这些能力如果未来确实需要，应在本设计基础上单独立项，并先重新评估复杂度、安全边界和状态迁移策略。

## 4. 总体架构

系统分为四个逻辑层：

```text
Frontend UI
  - 项目与任务管理
  - JSONL 上传
  - Schema 与字段树预览
  - 字段选择
  - Prompt 编辑
  - 模型配置
  - 模型输入/输出结构配置
  - 最终输出映射配置
  - 任务进度与失败样本查看

API Layer
  - HTTP JSON API
  - 文件上传与保存
  - Job Draft 保存和校验
  - 任务启动、暂停、恢复、取消
  - SSE 任务事件
  - 输出下载

Worker Runtime
  - JSONL 导入
  - Schema 识别
  - 字段目录生成
  - Mapping 执行
  - SQLite 状态机
  - 并发模型调用
  - 限速、重试、格式修复和失败 cover
  - 最终导出

SQLite State
  - Job 元数据
  - Item 状态
  - Attempt 审计
  - Job command
  - Job event
```

短期部署可以是一个 Go 进程同时包含 API 和 Worker：

```text
Browser -> Go server/API -> in-process worker -> SQLite/files
```

中期可以拆成两个本地进程：

```text
Browser -> API process -> SQLite job_commands -> Worker process -> SQLite/files
```

长期如果要分布式部署，再考虑使用 Postgres 或外部队列。但在保留 SQLite 的阶段，同一个任务同一时间必须只有一个 Worker owner。

## 5. 核心原则

### 5.1 SQLite 是状态源

JSONL 文件是导入和导出格式，不是 live work queue。任务状态、样本状态、尝试记录、成功结果和失败信息必须以 SQLite 为准。

输出 JSONL 是可再生成的产物。删除输出文件不应导致进度丢失；只要 SQLite state DB 存在，就可以重新导出。

### 5.2 数据库结构固定，任务结构动态

输入 JSONL 的结构可以不固定，但数据库表结构不应随任务动态变化。动态结构通过 JSON Schema、字段目录、输入映射、输出映射和 JSON 字段保存。

数据库表只保存通用状态列和 JSON 内容列，例如：

- `raw_json`
- `normalized_input_json`
- `model_result_json`
- `final_output_json`
- `field_catalog_json`
- `input_mapping_json`
- `output_mapping_json`

这样低代码能力体现在“JSON 到 JSON 的映射配置”上，而不是体现在动态建表上。

### 5.3 上下游通过接口解耦

Go 代码中，上游和下游必须通过小接口连接。接口定义在消费者侧，构造函数返回具体结构体。业务包不得依赖 SQLite、HTTP client 或模型 SDK 的具体类型，除非该包的职责就是实现对应适配器。

### 5.4 同级流程用 Step 组合

平行逻辑或同一级别的流程逻辑必须拆成独立文件，每个文件只负责一个 Step。Step 之间不互相调用，统一由 Workflow 组合。

示例：

```text
internal/workflow/
  workflow.go
  context.go
  step.go
  import_dataset.go
  inspect_schema.go
  build_field_catalog.go
  validate_mapping.go
  prepare_job_state.go
  run_annotation.go
  export_result.go
```

组合顺序只出现在一个地方：

```go
workflow := NewWorkflow(
    ImportDataset(store),
    InspectSchema(inspector),
    BuildFieldCatalog(builder),
    ValidateMapping(validator),
    PrepareJobState(store),
    RunAnnotation(runner),
    ExportResult(exporter),
)
```

新增或删除流程，应尽量只修改组合处和对应 Step 文件。

### 5.5 不过早插件化

低代码不等于插件系统。第一阶段使用显式 Go 组合和数据驱动配置，不使用反射、全局注册表、`init()` 自动注册或用户脚本执行。

## 6. 用户工作流

### 6.1 创建项目

用户在前端创建 Project。Project 是逻辑分组，可以包含多个 Dataset 和 Job。

### 6.2 上传数据集

用户上传：

- 输入 JSONL 文件。
- 可选输入 JSON Schema。
- 可选模型输出 JSON Schema。
- 可选最终输出 JSON Schema。
- 可选提示词模板。
- 可选模型配置模板。

系统保存文件引用，不在日志中输出文件内容。

### 6.3 结构识别

系统读取输入 JSONL 的前 N 行进行抽样，同时结合用户上传的输入 Schema，生成字段目录 `FieldCatalog`。

字段目录展示：

- 字段路径。
- 显示路径。
- 类型。
- 是否必填。
- 是否可空。
- 是否数组。
- 是否适合送入模型。
- 示例预览。
- 缺失比例。
- 结构稳定性。
- token 估算。

### 6.4 字段选择

用户通过页面选择哪些字段送入模型。系统生成 `input_mapping`，而不是直接把字段路径拼进提示词。

### 6.5 模型输入结构配置

用户确认送给模型的用户消息 JSON 结构。系统可根据 `input_mapping` 自动生成初始 `model_input_schema`，用户可以编辑。

### 6.6 提示词配置

用户编辑系统提示词模板。模板可引用固定占位符：

```text
{{MODEL_INPUT_SCHEMA}}
{{MODEL_OUTPUT_SCHEMA}}
{{FINAL_OUTPUT_SCHEMA}}
{{RISK_TYPES}}
```

最终替换后的提示词进入语义指纹。

### 6.7 模型配置

用户配置 OpenAI-compatible 模型：

- `base_url`
- `api_key_env`
- `name`
- `structured_output`
- `temperature`
- `top_p`
- `max_tokens`
- `seed`
- `timeout`
- `extra_body`

API Key 只能从环境变量读取，不允许写入配置、数据库、日志或响应。

### 6.8 模型输出结构配置

用户上传或编辑 `model_output_schema`。系统使用同一份 Schema 做两件事：

1. 对支持结构化输出的模型发送约束。
2. 对模型返回结果做本地校验。

业务规则由单独配置表达，不完全依赖 JSON Schema。

### 6.9 最终输出格式配置

用户配置 `final_output_mapping` 和 `final_output_schema`。导出时系统将原始输入、模型输出和固定元数据合成为最终 JSONL 行，并校验每一行满足最终输出 Schema。

### 6.10 启动任务

用户点击启动后，API 先冻结 Job Draft，生成可运行 `JobSpec` 和 `semantic_fingerprint`，再创建启动命令。Worker 执行 JobSpec。

## 7. 结构识别设计

### 7.1 Canonical Schema

系统内部统一使用 JSON Schema Draft 2020-12 表达结构。如果用户上传其他描述格式，第一阶段不强制支持自动转换；可以要求用户提供 JSON Schema，或者系统通过抽样生成近似 Schema。

### 7.2 FieldCatalog

结构识别输出统一为 `FieldCatalog`：

```json
{
  "version": "field_catalog.v1",
  "fields": [
    {
      "path": "/messages/*/content",
      "display_path": "messages[].content",
      "types": ["string"],
      "required": false,
      "nullable": false,
      "array": true,
      "selectable": true,
      "source": "schema+sample",
      "confidence": "high",
      "missing_ratio": 0.02,
      "example_preview": "用户问题文本...",
      "token_estimate": 128
    }
  ]
}
```

字段路径规则：

- 底层使用 JSON Pointer 风格路径。
- 对象字段使用 `/meta/source`。
- 数组泛化使用 `*`，例如 `/messages/*/content`。
- 前端展示使用 `messages[].content`。

### 7.3 Schema 与样本合并

识别流程：

```text
input.schema.json
        +
input.jsonl 抽样
        |
        v
Schema Resolver
        |
        v
Field Catalog Builder
        |
        v
Frontend Field Tree
```

规则：

- Schema 声明优先。
- 样本抽样补充 Schema 未覆盖字段。
- 同一路径出现多个类型时记录 union type。
- 字段只在部分样本出现时标记 optional。
- 数组对象字段展开为 `items[].field`。
- 超大字段只保存安全预览和 token 估算。
- 结构不稳定字段可选，但前端必须提示风险。

### 7.4 稳定 ID

每个任务必须有 stable ID 规则。

优先顺序：

1. 用户选择唯一字段，例如 `/trace_id` 或 `/id`。
2. 系统检测唯一非空字符串字段并建议用户确认。
3. 若无可用字段，使用 `row_index + raw_hash` 生成内部 ID。

使用 `row_index + raw_hash` 时，可以支持中断恢复，但跨文件变更后的去重和恢复语义更弱，前端必须提示。

## 8. Mapping 设计

### 8.1 InputMapping

`input_mapping` 描述如何从原始 JSONL 行生成模型输入 JSON。

```json
{
  "version": "input_mapping.v1",
  "stable_id_path": "/id",
  "fields": [
    {
      "source_path": "/messages",
      "target_path": "/messages",
      "mode": "copy"
    },
    {
      "source_path": "/meta/source_fields/reason",
      "target_path": "/original_reason",
      "mode": "copy"
    },
    {
      "source_path": "/annotation/explanation",
      "target_path": "/model_reason",
      "mode": "copy"
    }
  ]
}
```

第一阶段支持的 transform：

- `copy`：复制字段。
- `constant`：写入常量。
- `rename`：等价于 copy 到新路径。
- `template`：简单字符串模板。
- `join_array`：拼接数组中的标量或对象字段。
- `pick_first_by_filter`：从对象数组中按字段条件取第一项。

不支持任意脚本、任意表达式或循环语言。

### 8.2 ModelInputSchema

`model_input_schema` 描述模型实际看到的用户消息 JSON 结构。

作用：

- 前端预览。
- 运行前校验 mapping 生成的输入。
- 提示词占位符替换。
- 进入语义指纹。

示例：

```json
{
  "type": "object",
  "required": ["trace_id", "messages"],
  "properties": {
    "trace_id": {"type": "string"},
    "messages": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["role", "content"],
        "properties": {
          "role": {"type": "string"},
          "content": {"type": "string"}
        }
      }
    },
    "original_reason": {"type": "string"},
    "model_reason": {"type": "string"}
  }
}
```

### 8.3 ModelOutputSchema

`model_output_schema` 描述模型必须返回的 JSON 对象。本地校验必须严格：

1. 只接受一个 JSON object。
2. 不静默接受对象前后解释文字。
3. 先做 JSON Schema 校验。
4. 再做业务规则校验。

业务规则第一阶段支持：

- `required_when`
- `forbidden_when`
- `enum_from_file`
- `min_length`
- `max_length`
- `equals_when`

### 8.4 FinalOutputMapping

`final_output_mapping` 描述如何合成最终输出 JSONL 行。

```json
{
  "version": "final_output_mapping.v1",
  "preserve_unknown_fields": true,
  "writes": [
    {
      "target_path": "/annotation/method",
      "value": "auto"
    },
    {
      "target_path": "/annotation/label",
      "source_path": "$model.label"
    },
    {
      "target_path": "/annotation/explanation",
      "source_path": "$model.explanation"
    },
    {
      "target_path": "/annotation/extended_info",
      "source_path": "$model.extended_info"
    }
  ],
  "remove_paths": []
}
```

导出流程：

```text
读取 raw_json
读取 model_result_json
应用 final_output_mapping
校验 final_output_schema
按 row_index 排序
写临时文件
原子 rename
```

## 9. 数据库设计

### 9.1 固定 Schema 原则

数据库表结构固定，不随输入数据集变化。动态结构保存为 JSON。

推荐表：

```text
projects
datasets
jobs
items
attempts
job_commands
job_events
```

### 9.2 projects

保存项目分组：

```text
id
name
created_at
updated_at
```

### 9.3 datasets

保存输入文件和结构识别结果：

```text
id
project_id
input_file_ref
input_schema_json
detected_schema_json
field_catalog_json
row_count
source_fingerprint
created_at
updated_at
```

### 9.4 jobs

保存任务配置和运行状态：

```text
id
project_id
dataset_id
status
semantic_fingerprint
job_spec_json
model_config_json
runtime_config_json
retry_config_json
state_db_ref
output_file_ref
failed_output_file_ref
created_at
updated_at
```

如果继续采用“每任务一个 SQLite state DB”，项目级数据库只保存 job 索引和文件引用；每个 job 的 items 和 attempts 可以存入独立 state DB。

### 9.5 items

保存每条输入记录的状态：

```text
id
job_id
stable_id
row_index
raw_json
raw_hash
normalized_input_json
input_hash
status
attempt_count
next_attempt_at
model_result_json
final_output_json
failure_category
failure_summary
created_at
updated_at
```

`status` 包括：

- `pending`
- `processing`
- `retry_wait`
- `succeeded`
- `failed`
- `canceled`

### 9.6 attempts

保存每次模型调用和修复尝试的审计摘要：

```text
id
item_id
attempt_no
stage
http_status
error_category
retryable
duration_ms
token_usage_json
validation_error_json
raw_response_ref
raw_response_prefix
created_at
```

原始模型回复只能保存在受控审计位置，不进入日志和普通 API 响应。

### 9.7 job_commands

API 和 Worker 拆分时使用本地持久命令表：

```text
id
job_id
command_type
idempotency_key
status
claimed_by
claimed_at
created_at
completed_at
error_summary
```

`command_type` 包括：

- `start`
- `pause`
- `resume`
- `cancel`
- `export`

### 9.8 job_events

保存任务事件供 SSE 查询和页面刷新恢复：

```text
id
job_id
event_type
event_json
created_at
```

事件不能包含原始数据载荷、完整模型输出或凭证。

## 10. 并发和 SQLite 写入

### 10.1 单写入器模型

单机部署下，多个 worker goroutine 可以并发调用模型，但 SQLite 写入应收敛到单个 `dbWriter` goroutine。

```text
Scheduler
  -> claim request
  -> DB Writer 标记 processing
  -> Workers 并发模型调用
  -> resultCh
  -> DB Writer 顺序提交 SQLite
```

写入链路：

```text
Worker 1 ─┐
Worker 2 ─┼─ resultCh ─> DB Writer ─> SQLite
Worker 3 ─┘
```

### 10.2 Channel 不是持久队列

`resultCh` 只做进程内协调，不是 durable queue。程序崩溃时，channel 内结果会丢失。SQLite 中的 `pending`、`processing`、`retry_wait`、`succeeded` 和 `failed` 才是可信状态。

启动恢复时，遗留 `processing` 应恢复为 `pending`。

### 10.3 DB Writer Channel

建议拆分：

```text
claimReqCh
resultCh
controlCh
```

`resultCh` 容量建议为 `runtime.concurrency` 或 `2 * runtime.concurrency`，形成自然背压。不得使用无限内存队列。

### 10.4 状态不变量

必须保持以下不变量：

1. `succeeded` 一旦提交，永不重新调用模型。
2. `processing` 只表示已领取但未提交终态。
3. Worker 不能直接改 SQLite。
4. 所有状态迁移由 DB Writer 在事务中完成。
5. `resultCh` 关闭前必须确保 worker 已退出。
6. DB Writer 退出前必须 drain 已产生结果，除非进程被强杀。
7. 任务级 fatal error 停止继续 claim，并进入可诊断状态。

## 11. API 协议设计

### 11.1 前端与 API

前端与 API 使用 REST JSON。任务进度使用 SSE，第一阶段不需要 WebSocket。

推荐接口：

```text
POST   /api/v1/projects
GET    /api/v1/projects

POST   /api/v1/datasets
POST   /api/v1/datasets/{id}/files/input
POST   /api/v1/datasets/{id}/files/input-schema
POST   /api/v1/datasets/{id}/inspect
GET    /api/v1/datasets/{id}/field-catalog

POST   /api/v1/jobs
GET    /api/v1/jobs/{id}
PUT    /api/v1/jobs/{id}/draft
POST   /api/v1/jobs/{id}/validate
POST   /api/v1/jobs/{id}/start
POST   /api/v1/jobs/{id}/pause
POST   /api/v1/jobs/{id}/resume
POST   /api/v1/jobs/{id}/cancel

GET    /api/v1/jobs/{id}/events
GET    /api/v1/jobs/{id}/progress
GET    /api/v1/jobs/{id}/failed-items
GET    /api/v1/jobs/{id}/download/output
GET    /api/v1/jobs/{id}/download/failed
```

### 11.2 Job Draft

前端保存可编辑 `JobDraft`：

```json
{
  "dataset_id": "ds_001",
  "stable_id_path": "/id",
  "input_mapping": {},
  "model_input_schema": {},
  "prompt": {
    "system_template": "你是安全标注员..."
  },
  "model": {
    "base_url": "https://example.com/openai",
    "api_key_env": "AI_GATEWAY_API_KEY",
    "name": "deepseek-v4-pro",
    "structured_output": "json_object",
    "temperature": 0,
    "max_tokens": 2000,
    "timeout": "60s"
  },
  "runtime": {
    "concurrency": 16,
    "requests_per_minute": 600,
    "tokens_per_minute": 0
  },
  "retry": {},
  "model_output_schema": {},
  "business_rules": [],
  "final_output_mapping": {},
  "final_output_schema": {}
}
```

### 11.3 Validate API

`POST /api/v1/jobs/{id}/validate` 不启动任务，只做预检查：

- input mapping 是否可执行。
- 模型输入是否符合 `model_input_schema`。
- 模型输出 Schema 是否可用。
- 最终输出 mapping 是否可执行。
- 最终输出是否满足 `final_output_schema`。
- API Key 环境变量名是否合法。
- 抽样生成模型输入预览。
- 抽样生成最终输出预览。
- 生成语义指纹。

返回：

```json
{
  "ok": true,
  "semantic_fingerprint": "sha256:...",
  "warnings": [
    {
      "code": "optional_field_missing",
      "message": "字段 /meta/source 在 12.4% 抽样记录中缺失"
    }
  ],
  "preview": {
    "model_input": {},
    "final_output": {}
  }
}
```

### 11.4 错误响应

统一错误格式：

```json
{
  "error": {
    "code": "mapping_invalid",
    "message": "字段 /messages/*/content 不能直接 copy 到对象字段 /content",
    "details": {
      "field": "/messages/*/content"
    },
    "request_id": "req_..."
  }
}
```

错误响应不得包含原始数据载荷、完整模型输出或 API Key。

### 11.5 SSE 事件

`GET /api/v1/jobs/{id}/events` 返回任务事件。

进度事件：

```json
{
  "type": "progress",
  "job_id": "job_001",
  "status": "running",
  "counts": {
    "pending": 1200,
    "processing": 16,
    "retry_wait": 42,
    "succeeded": 8750,
    "failed": 3
  },
  "rate": {
    "items_per_minute": 380,
    "estimated_remaining_seconds": 210
  },
  "updated_at": "2026-08-13T10:00:00Z"
}
```

失败摘要事件：

```json
{
  "type": "failure_summary",
  "job_id": "job_001",
  "category": "rate_limited",
  "count": 12
}
```

### 11.6 API 与 Worker

单进程第一版可以使用 Go interface：

```go
type JobRuntime interface {
    Start(ctx context.Context, jobID string) error
    Pause(ctx context.Context, jobID string) error
    Resume(ctx context.Context, jobID string) error
    Cancel(ctx context.Context, jobID string) error
    Snapshot(ctx context.Context, jobID string) (JobSnapshot, error)
}
```

拆进程时改用 SQLite `job_commands` 表，不急着引入外部队列。

所有启动、暂停、恢复、取消 API 支持 `Idempotency-Key`。重复请求必须返回同一命令或当前状态，不重复启动任务。

## 12. JobSpec 和语义指纹

`JobSpec` 是 Job Draft 校验通过后的冻结运行规格。Worker 只执行 JobSpec，不接收散乱参数。

```json
{
  "version": "job_spec.v1",
  "job_id": "job_001",
  "dataset_id": "ds_001",
  "semantic_fingerprint": "sha256:...",
  "input_file_ref": "files/input.jsonl",
  "state_db_ref": "state/job_001.db",
  "field_catalog": {},
  "stable_id_path": "/id",
  "input_mapping": {},
  "model_input_schema": {},
  "prompt": {},
  "model": {},
  "runtime": {},
  "retry": {},
  "model_output_schema": {},
  "business_rules": [],
  "final_output_mapping": {},
  "final_output_schema": {}
}
```

语义指纹必须包含：

- 输入文件语义标识。
- stable ID 规则。
- field catalog 版本。
- input mapping。
- model input schema。
- prompt 最终内容。
- model name 和采样参数。
- structured output 模式。
- model output schema。
- business rules。
- final output mapping。
- final output schema。

恢复任务时，如果语义指纹不匹配，必须拒绝直接续跑。用户应创建新任务或 fork 任务。

运行参数如 concurrency、RPM、TPM、shutdown timeout 和导出路径可以允许调整，但是否进入语义指纹必须明确记录。

## 13. Worker Runtime

Worker 流程：

```text
Load JobSpec
  -> Ensure state DB
  -> Import JSONL if needed
  -> Recover processing items
  -> Start DB Writer
  -> Claim pending/retry_wait items
  -> Run worker pool
  -> Parse model response
  -> Validate schema and business rules
  -> Retry or repair when needed
  -> Commit terminal states
  -> Export outputs
```

模型调用保持 OpenAI-compatible 边界。业务逻辑依赖窄接口：

```go
type Completer interface {
    Complete(ctx context.Context, req CompletionRequest) (CompletionResult, error)
}
```

OpenAI 适配器位于 facade 层，不能泄漏 SDK 类型到业务层。

## 14. 重试、格式修复和失败 cover

错误分类：

```text
request_retryable
  - timeout
  - network
  - HTTP 408
  - HTTP 429
  - HTTP 5xx
  - provider malformed response

format_repairable
  - invalid JSON
  - schema validation failed
  - business rule failed
  - extra text around JSON

item_permanent
  - input mapping failed
  - item too large
  - content rejected
  - schema impossible for this row

task_fatal
  - auth failed
  - model config invalid
  - state DB corrupted
  - semantic fingerprint mismatch
```

策略：

- 网络和限速错误使用指数退避、随机 jitter、`Retry-After` 和共享冷却。
- 格式错误可发起修复调用。
- 修复调用只携带非法模型输出、Schema 和校验错误，不再次携带原始敏感样本。
- 原始尝试和修复尝试分别计数。
- 任务结束仍有失败时，可以执行一次保守 cover pass。
- cover pass 使用低并发、低 RPM、更高 max attempts 和更保守提示词。
- 内容拒绝不阻塞全局任务，进入 failed sidecar 和人工处理页。

## 15. 前端设计要点

第一版前端不做营销页，直接进入工作台。

主要页面：

```text
ProjectList
DatasetUpload
SchemaExplorer
FieldSelector
PromptEditor
ModelConfigForm
OutputSchemaEditor
ExportMappingEditor
JobValidatePreview
JobProgress
FailedItemsReview
DownloadOutputs
```

字段选择页面是核心体验：

- 左侧字段树。
- 中间模型输入结构预览。
- 右侧字段属性和 transform 配置。
- 底部展示抽样生成的模型输入 JSON。

任务运行页面：

- 总数、成功、失败、重试等待、处理中。
- 当前速率和预计剩余时间。
- 失败类别聚合。
- 暂停、恢复、取消、导出按钮。

失败样本页面：

- 只展示安全摘要和可人工处理字段。
- 不默认展示完整敏感 payload。
- 需要权限或显式操作才查看原始样本。

## 16. 安全和隐私

必须继承当前 SendLLM 的安全约束：

- API Key 只从环境变量读取。
- 不在配置、数据库普通字段、日志、响应或提交中保存 API Key 值。
- 不记录 Authorization 请求头。
- 不在日志中输出原始 prompt、response、完整模型输出或数据集 payload。
- 审计 payload 只能进入受控 SQLite 或受控文件引用。
- SSE、错误响应和失败摘要只返回安全分类和计数。
- 测试夹具使用合成数据。
- 输入数据集、输出数据集、SQLite state、raw model response、`.env` 必须被 Git 忽略。

## 17. 推荐项目结构

未来 Go 后端结构：

```text
SendLLM/
├── cmd/
│   ├── server/
│   └── worker/
├── internal/
│   ├── api/
│   ├── app/
│   ├── workflow/
│   ├── schema/
│   ├── mapping/
│   ├── runtime/
│   ├── facade/
│   ├── dao/
│   ├── files/
│   ├── dto/
│   └── lib/
│       ├── configs/
│       ├── limiter/
│       └── tokenizer/
├── web/
├── config/
├── prompts/
├── docs/
└── README.md
```

职责：

- `internal/api`：HTTP handlers、request/response DTO 转换。
- `internal/app`：用例服务，连接 API 和 workflow/runtime。
- `internal/workflow`：低代码 Job 准备流程 Step 组合。
- `internal/schema`：JSON Schema 解析、抽样识别、FieldCatalog。
- `internal/mapping`：JSON Pointer、InputMapping、FinalOutputMapping。
- `internal/runtime`：Runner、DB Writer、retry、repair、export。
- `internal/facade`：OpenAI-compatible provider。
- `internal/dao`：SQLite schema、事务、状态查询。
- `internal/files`：上传文件、输出文件、临时文件和原子替换。
- `internal/dto`：稳定协议结构。

## 18. 分阶段演进计划

### 阶段一：保留 CLI，抽出通用低代码内核

目标：

- 抽出 schema、mapping、JobSpec、workflow Step。
- 当前 CLI 可以从 YAML/JSON JobSpec 运行。
- 不加 HTTP 和前端。

产出：

- `internal/schema`
- `internal/mapping`
- `internal/workflow`
- `JobSpec`
- 更通用的 importer/exporter

### 阶段二：本地 Web 单进程版

目标：

- 增加 Go HTTP server。
- 增加 React/TypeScript 前端。
- API 和 Worker 同进程。
- 通过页面完成上传、识别、配置、启动、进度和导出。

产出：

- `/api/v1` REST API
- SSE 进度
- `web/`
- 项目级 SQLite 或元数据 DB

### 阶段三：API 与 Worker 本地拆分

目标：

- API 进程和 Worker 进程拆分。
- 使用 SQLite `job_commands` 和 `job_events` 做本地持久协议。
- Worker 可独立重启并接管未完成任务。

产出：

- `cmd/server`
- `cmd/worker`
- job command loop
- worker heartbeat 和 owner 标识

### 阶段四：可选高可用增强

目标：

- 任务 lease。
- state DB 备份。
- 文件存储抽象。
- 更强人工失败处理。

不默认引入分布式队列。只有当单机部署成为瓶颈时，再讨论 Postgres 或队列。

## 19. 测试策略

测试必须覆盖：

- JSONL 抽样识别。
- JSON Schema 合并。
- FieldCatalog 生成。
- JSON Pointer 读取和写入。
- InputMapping transform。
- ModelInputSchema 校验。
- ModelOutputSchema 校验。
- BusinessRules 校验。
- FinalOutputMapping 合成。
- FinalOutputSchema 校验。
- DB Writer 顺序写入和 shutdown drain。
- Worker 并发、取消、重试、共享冷却。
- job command 幂等。
- SSE 事件不泄露 payload。
- API 错误响应不泄露 payload。
- 成功任务可从 SQLite 重新导出。
- 语义指纹变化拒绝恢复。

涉及 Go 并发的测试必须运行 race detector。涉及解析的输入复杂逻辑应增加 fuzz smoke。

## 20. 关键风险

### 20.1 低代码映射过度复杂

如果 transform 能力过多，会变成一门脚本语言。第一阶段只保留少量可测试 transform。

### 20.2 Schema 与样本不一致

真实 JSONL 经常不完全符合 Schema。系统应把识别结果标记为置信度和风险，而不是假装结构完美。

### 20.3 SQLite 被误用为分布式数据库

SQLite 适合单任务本地状态，不适合多实例并发写同一状态库。未来高可用必须围绕单 owner、恢复和备份设计。

### 20.4 前端预览泄露敏感数据

前端需要样本预览，但日志、事件和错误不能泄露 payload。前端预览应有明确权限、遮罩和采样限制。

### 20.5 Workflow Step 退化为隐式链条

如果 Step 之间互相调用，组合规则会失效。必须通过测试和 review 保证同级流程只在 Workflow 中排序。

## 21. 设计验收标准

未来实现完成时，应满足：

- 用户能通过页面导入 JSONL 和结构定义。
- 系统能生成字段树并支持页面化字段选择。
- 用户能配置模型输入结构、提示词、模型配置、模型输出结构和最终输出格式。
- 任务能一键启动、暂停、恢复、取消和导出。
- SQLite 是任务状态源。
- 输出文件可从 SQLite 重新生成。
- 成功样本不会重复调用模型。
- 输入结构变化不要求数据库动态改表。
- 多 goroutine 模型调用通过单 DB Writer 写 SQLite。
- API 与 Worker 协议具备幂等性。
- 上下游通过窄接口解耦。
- 同级流程按 Step 文件拆分并统一组合。
- 日志、事件、错误响应和测试不泄露敏感 payload 或凭证。
- 相关单元、集成、race 和 fuzz 测试通过。

## 22. 推荐下一步

当真正开始实现时，不要一次性建设完整平台。推荐第一个实施主题是：

```text
从当前 CLI 抽出 JobSpec + schema/mapping/workflow 内核，
让 CLI 可以通过通用 JobSpec 跑通当前已有标注任务。
```

这个阶段不需要前端和 HTTP，但能提前验证未来低代码系统最核心的数据抽象是否正确。如果 JobSpec、FieldCatalog、Mapping 和 Workflow Step 设计稳定，再建设 Web/API 层会轻松很多。
