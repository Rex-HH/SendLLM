# SendLLM 本地安全标注 CLI 设计

日期：2026-08-05
状态：已批准

## 1. 目标

构建一个极简的本地 Go CLI，将大约 30,000 条安全样本提交给兼容 OpenAI 协议的大模型，校验模型返回的结构化安全标注，持久化全部处理进度，在程序中断后继续运行且不重复调用已经成功的记录，最终导出顺序确定的 JSONL 文件供后续流程使用。

首版明确限定为单机、单进程批处理工具，不包含 HTTP 服务、外部消息队列、Redis、定时调度、用户界面或多实例协调。

## 2. 输入与输出

### 2.1 输入 JSONL

每行是一个 JSON 对象。首版支持以下输入字段：

```json
{
  "trace_id": "unique-id",
  "prompt": "text to review",
  "response": "optional model response"
}
```

规则如下：

- `trace_id` 必填、非空，并且在重试过程中保持不变。
- `response` 可以缺失、为 `null`、空字符串或非空字符串。缺失和 `null` 在程序内部统一为空字符串。
- `prompt` 和 `response` 至少有一个非空，从而同时支持仅审查提示词、仅审查回复和成对审查。
- 未识别的输入字段以原始 JSON 形式保留，并在导出时合并回结果记录。
- `trace_id` 相同且内容完全一致的重复记录直接跳过；`trace_id` 相同但待审查内容不同属于导入冲突，必须在开始调用模型前终止导入。
- 每条记录获得一个不可变的输入序号，用于保证导出顺序确定。

### 2.2 模型输入

系统提示词从配置指定的文件中加载。每次调用的用户消息只包含预先约定的结构化数据：

```json
{
  "trace_id": "unique-id",
  "scene": "prompt",
  "prompt": "text to review",
  "response": ""
}
```

`scene` 可配置为 `prompt`、`response`、`pair` 或 `auto`。使用 `auto` 时，程序根据 `prompt` 和 `response` 中哪些字段非空来推导审查场景。

系统提示词文件是纯文本模板，必须各包含一次 `{{RISK_TYPES}}` 和 `{{RESULT_SCHEMA}}`。加载任务时，程序以稳定排序的风险分类 JSON 和 `output.schema_file` 的完整内容替换这两个占位符。替换后的提示词计入语义指纹；每条样本调用时仍只发送结构化用户数据。

### 2.3 模型结果

模型只返回标注字段，不应回显原始输入：

```json
{
  "label": "unsafe",
  "explanation": "该输入试图诱导模型绕过安全限制。",
  "extended_info": {
    "risk_type": "jailbreak",
    "risk_level": "high",
    "attack_scenario": "role_play",
    "case_type": "typical",
    "is_attack": true,
    "other": ""
  }
}
```

固定核心字段遵循 `样本格式-8-4.md`。`risk_type` 是从任务配置闭集中选择的单个值。任务可以通过配置扩展 `extended_info` 中允许的字段，而无需修改任务调度逻辑。

校验规则包括：

- `label` 只能是 `safe` 或 `unsafe`。
- `explanation` 必须满足配置的长度限制，初始范围为 10 至 70 个字符。
- `unsafe` 结果必须包含合法的 `risk_type` 和 `risk_level`。
- `risk_level` 只能是 `low`、`medium` 或 `high`。
- `case_type=hard_negative` 时，`label` 必须是 `safe`。
- 可选扩展字段必须通过任务配置的 JSON Schema 校验。

当 `label=safe` 时，可以省略 `extended_info`。如果保留该对象，则不能包含 `risk_type` 或 `risk_level`，`is_attack` 必须为 `false`，其余字段仍需通过 Schema 校验。这样既能表达安全的 `hard_negative` 样本，又不会错误附加不安全风险分类。

导出器把合法标注合并到原始对象中，并设置 `annotation.method` 为 `auto`。程序生成的 `label`、`explanation`、`extended_info` 和 `annotation` 会覆盖输入中同名字段，其他未知字段保持不变。模型原始回复留存在 SQLite 中用于审计，不写入正式结果文件。

### 2.4 导出文件

- 配置指定的输出 JSONL 包含所有成功记录，并按照原始输入序号排列。
- 同目录下的 `*.failed.jsonl` 包含最终失败记录，只记录 `trace_id`、失败类别、安全的诊断摘要和尝试次数。
- 导出时先写临时文件，再通过原子重命名替换正式文件，避免程序崩溃后留下半截结果文件。

## 3. 任务配置

每个任务使用一个 YAML 配置文件。影响运行行为的重要参数必须显式配置，并在导入数据或发起网络请求之前完成校验。

```yaml
task:
  id: masb-2026-08
  input: ./data/input.jsonl
  output: ./data/output.jsonl
  state: ./data/masb-2026-08.db

model:
  base_url: https://example.com/v1
  api_key_env: LLM_API_KEY
  name: model-name
  structured_output: json_schema
  temperature: 0
  top_p: 1
  max_tokens: 500
  seed: 42
  timeout: 60s
  extra_body:
    frequency_penalty: 0

prompt:
  system_file: ./prompts/masb-system.txt
  scene: auto
  risk_types_file: ./config/risk-types.yaml

runtime:
  concurrency: 64
  requests_per_minute: 0
  tokens_per_minute: 0
  shutdown_timeout: 30s

retry:
  request_max_attempts: 5
  format_repair_attempts: 2
  initial_backoff: 1s
  max_backoff: 60s

output:
  schema_file: ./config/result-schema.json
  explanation_min_length: 10
  explanation_max_length: 70
```

API Key 的值只能从 `api_key_env` 指定的环境变量读取，不能写入配置、SQLite、日志或输出文件。`structured_output` 必须显式设置为 `json_schema`、`json_object` 或 `prompt_only`，因为不同兼容供应商支持的结构化输出能力并不一致。

`temperature`、`top_p`、`max_tokens` 和 `seed` 是类型化常用参数。供应商特有参数可以放入 `extra_body` 并透传，但不得覆盖 `model`、`messages`、`response_format`、`stream` 或其他由客户端负责的协议核心字段。全部模型参数都计入语义指纹。

`output.schema_file` 指向完整的结果 JSON Schema。同一份 Schema 同时发送给支持 `json_schema` 的模型接口，并用于本地结果校验，避免模型约束和程序约束发生漂移。`risk_types_file` 仍作为风险闭集的唯一来源，由本地语义校验器进行二次约束。

并发数允许配置为 1 至 500，以匹配当前已知的账号并发上限。RPM 或 TPM 设置为零表示不启用对应的主动限速器。建议先以约 64 并发进行校准，再根据实际延迟、429 比例和供应商文档逐步提高。

## 4. 架构

仓库沿用 `开发指南.md` 中的分层命名，但不创建首版不需要的层：

```text
SendLLM/
├── internal/
│   ├── service/           # 任务执行、重试、校验协调和导出
│   ├── facade/            # OpenAI 兼容 HTTP 适配器
│   ├── dao/               # SQLite 持久化和迁移
│   ├── dto/               # 输入、输出和供应商协议结构
│   └── lib/
│       ├── configs/       # YAML 加载、默认值和校验
│       ├── limiter/       # 并发、RPM、TPM 和共享冷却
│       └── tokenizer/     # 保守的 Token 估算
├── config/
├── prompts/
├── testdata/
├── main.go
├── go.mod
└── README.md
```

`main.go` 只负责 CLI 参数、应用日志器、配置加载、依赖装配、操作系统信号和最终退出码。各内部包返回结构化错误，不记录数据载荷。

`service` 包定义其真正需要的窄模型调用接口，OpenAI `facade` 提供具体实现。SQLite 使用具体的本地存储实现；只有业务测试确实需要替换的边界才引入接口，避免为了抽象而抽象。

## 5. 持久化状态与恢复

SQLite 是唯一可信状态源，开启 WAL 模式，并由应用使用单个写连接。数据库记录以下信息：

- 任务标识、归一化配置指纹、提示词指纹和任务时间戳；
- 样本标识、输入序号、原始 JSON、归一化后的 `prompt`/`response`、处理状态、尝试次数和下次允许执行时间；
- 每次模型调用的阶段、耗时、HTTP 状态、错误分类、是否可重试、校验详情、Token 用量和模型原始回复；
- 最终通过校验的结构化标注。

样本状态包括 `pending`、`processing`、`retry_wait`、`succeeded` 和 `failed`。状态迁移及其对应的调用记录必须在事务中提交。

启动时，如果状态库中存在匹配任务，则从原进度继续。语义指纹包含模型标识和采样参数、系统提示词内容、审查场景策略、风险分类闭集及输出 Schema。如果语义指纹发生变化，程序必须给出明确错误并停止；用户需要使用新的任务或状态文件。并发数、RPM/TPM、重试等待时间、退出等待时间和导出路径等运行参数允许在恢复任务时调整。启动恢复时，遗留的 `processing` 记录重新变为 `pending`，`succeeded` 记录不再调用模型。尝试次数持久化，避免重启后重试次数清零而形成无限循环。

系统保证已经写入 `succeeded` 状态的记录不会再次调用模型。若进程在供应商已经处理请求、但本地尚未来得及提交成功事务的极小窗口内崩溃，恢复后可能再次发送该请求；通用 OpenAI 兼容协议无法提供跨供应商的严格外部恰好一次语义。因此调用语义是至少一次，而最终结果按 `task_id + trace_id` 幂等，只保留一条成功标注。

## 6. 并发与限速

CLI 在普通同步 HTTP 调用外使用有界并发：

```text
SQLite 待处理记录
  -> 调度器
  -> 并发许可 + RPM 许可 + 预估 TPM 许可
  -> Worker HTTP 调用
  -> 解析与校验
  -> 事务更新状态
```

内存中的任务数量始终有界。调度器每次读取一个小批次，并且所有 goroutine 都由顶层生命周期管理，可以通过上下文取消并等待退出。

可持续请求速率受以下三者的最小值约束：并发数除以平均响应时间、RPM，以及 TPM 除以平均单次 Token 消耗。使用同一个 API Key 发起更多并发请求不能绕过供应商配额。

HTTP Transport 根据选定并发数设置连接池。供应商返回限速响应头时程序会读取并使用，但不能依赖这些响应头一定存在。收到 HTTP 429 后触发全部 Worker 共享冷却，避免其他 Worker 继续冲击相同配额窗口。

收到 SIGINT 或 SIGTERM 后，调度器停止领取新任务，在配置的等待时间内允许在途请求完成，超过期限后取消剩余调用；已经事务提交的成功结果不会丢失。

## 7. 结构化输出与错误策略

模型回复按以下顺序严格处理：

1. 检查 HTTP 状态和有上限的响应体大小。
2. 提取 assistant 内容。
3. 只解析一个 JSON 对象，不静默接受对象前后的解释文字。
4. 校验 JSON Schema 和固定字段类型。
5. 校验跨字段业务规则。
6. 在同一事务中写入合法结果和本次尝试记录。

错误分为三类：

- 可重试调用错误：网络中断、超时、HTTP 408、429 和 5xx。
- 可修复输出错误：非法 JSON、字段缺失、枚举非法或字段语义矛盾。
- 永久错误：源输入非法、401/403、模型参数无效、超出上下文窗口或供应商明确拒绝内容。

调用错误使用带完全随机抖动的指数退避，并受配置上限约束。存在 `Retry-After` 时优先遵守该值。`request_max_attempts` 统计原始分类尝试次数，包括在网络或 HTTP 层失败的尝试。`format_repair_attempts` 单独限制在分类请求成功返回但内容不合法后发起的修复调用。修复请求只携带不合法的模型回复、Schema 和校验错误，不再次携带原始敏感样本。修复次数耗尽后，仅当原始分类尝试尚未达到上限时，才允许重新执行一次原始分类。最终失败记录持久化并单独导出。

认证错误和全局配置错误会暂停整个任务。只影响某条数据的永久错误仅终止该条记录。

## 8. 可观测性与数据安全

结构化日志可以包含任务 ID、`trace_id`、状态、耗时、尝试次数、HTTP 状态和错误类别，但不能包含源 `prompt`、源 `response`、完整模型回复、Authorization 请求头、API Key 或敏感标注内容。

CLI 进度只报告成功数、待处理数、重试数、失败数、当前处理速率和预计剩余时间。高吞吐路径上的逐条成功日志仅允许在 Debug 级别启用。

数据集 JSONL、SQLite 文件、模型原始输出、环境文件和生成结果必须被 Git 忽略。测试夹具只能使用合成的非敏感文本。

## 9. CLI 行为

首版只有一个主要执行方式：

```bash
sendllm -config ./config/task.yaml
```

命令依次完成配置与输入校验、创建或恢复任务、运行到完成、中断或任务级致命错误，并导出当前已经进入终态的结果。使用相同配置再次执行时，从同一个 SQLite 状态继续。

退出码固定如下：所有导入记录均成功时返回 `0`；处理完成但存在一个或多个最终失败样本时返回 `2`；配置、认证、存储或其他任务级致命错误返回 `1`；任务尚未进入终态时被中断则返回 `130`。

## 10. 测试与验证

单元测试和集成测试覆盖以下内容：

- 配置默认值、必填字段、能力模式和非法限额；
- `response` 缺失、`null`、空字符串和非空值，至少一个待审查字段非空，未知字段往返，非法 JSONL、重复 ID 和冲突 ID；
- 结果 JSON 解码、响应体大小限制、风险枚举、解释长度和跨字段规则；
- 重试分类、随机抖动范围、`Retry-After`、共享冷却和取消；
- 通过模拟 HTTP 供应商覆盖成功、429、5xx、超时、非法输出、修复成功、修复耗尽、认证失败和内容拒绝；
- SQLite 状态迁移、遗留处理中状态恢复、重试次数持久化、配置指纹不匹配和成功记录不重复调用；
- 确定顺序导出和原子文件替换；
- 程序中断时不存在 goroutine 泄漏或数据竞争。

测试使用 Go 标准库、`httptest`、临时 SQLite 数据库、表驱动用例和解析器 fuzz 测试。中间自动化测试不得调用需要付费的真实模型 API。

最终项目验收必须使用真实模型请求。真实验收从仓库根目录 `模型配置.md` 读取 `base_url=https://aigateway.venusgroup.com.cn/ai/deepseek/openai`、`model=deepseek-v4-pro` 和 `api_key_env=AI_GATEWAY_API_KEY`，读取 `Test_Input.jsonl` 的 50 条记录，运行完整 CLI 并生成 `Test_Output.jsonl`。只有 CLI 返回 0、输出 50 条、输入输出 `trace_id` 集合完全一致且无重复、全部输出通过正式 Schema、失败记录数为 0，才能认定端到端测试通过。

仓库统一验证入口为：

```bash
./init.sh
```

该命令检查 `gofmt`、`go test ./...`、`go test -race ./...` 和 `go vet ./...`。

## 11. 实施边界

首版不实现自动自适应并发、多 API Key、供应商 Batch API、HTTP 管理接口、分布式租约、动态配置重载、可视化面板或人工审核流程。这些能力以后可以在不改变输入输出契约和持久化任务标识规则的前提下增加。

实施过程遵循 `AGENTS.md` 中的仓库 Harness，一次只处理一个功能，并在 `feature_list.json`、`progress.md` 和 `session-handoff.md` 中记录验证证据。
