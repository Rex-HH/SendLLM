# SendLLM

SendLLM 是一个本地、单进程 Go CLI，用兼容 OpenAI Chat Completions 的模型批量标注 JSONL 安全样本。SQLite 是运行状态和审计数据的唯一可信来源；输入与输出 JSONL 只用于导入和导出。

## 构建

需要 Go 1.24 或更高版本。

```bash
go build -o sendllm .
./init.sh
```

`./init.sh` 运行格式检查、普通测试、竞态检测和 `go vet`。

## 配置

从示例开始创建本地任务配置：

```bash
cp config/task.example.yaml config/task.yaml
```

配置中的相对路径都相对于任务配置文件所在目录解析。主要字段如下：

- `task`：稳定任务 ID、输入 JSONL、输出 JSONL 和 SQLite 状态文件路径。
- `model`：兼容 OpenAI 的 `base_url`、API Key 环境变量名、模型名、结构化输出模式、生成参数和超时。
- `prompt`：系统提示词、审查场景及风险类型文件。场景可为 `prompt`、`response`、`pair` 或 `auto`。
- `runtime`：并发、RPM、TPM 和中断后的本地收尾超时。
- `retry`：请求尝试次数、格式修复次数及退避范围。
- `output`：结果 JSON Schema 和解释长度范围。

API Key 只从 `model.api_key_env` 指定的环境变量读取。例如配置使用 `LLM_API_KEY` 时：

```bash
export LLM_API_KEY='your-local-secret'
```

不要把 Key 写入 YAML、SQLite、命令输出或版本库。CLI 日志只记录任务 ID、计数、耗时和错误类别，不记录输入、模型原始回复或 Authorization。

`model.structured_output` 必须显式选择供应商支持的模式：

- `json_schema`：向供应商发送完整结果 Schema，同时在本地用同一份 Schema 校验。
- `json_object`：要求供应商返回 JSON 对象，结果仍经过完整本地 Schema 校验。
- `prompt_only`：不发送 `response_format`，只依靠提示词约束，结果仍经过完整本地 Schema 校验。

当前示例针对已验证网关使用 `json_object`、`max_tokens: 2000` 和 `runtime.concurrency: 4`。该网关拒绝 `json_schema`，且较低的输出预算可能在模型生成最终 JSON 前耗尽。建议从并发 4 开始校准延迟和 HTTP 429 比例，再依据供应商配额逐步调整，配置上限为 500。`requests_per_minute: 0` 或 `tokens_per_minute: 0` 表示不启用对应的本地主动限速；这不代表供应商没有配额。

## 运行与恢复

```bash
./sendllm -config ./config/task.yaml
```

输入每行必须包含非空 `trace_id`，且 `prompt` 与 `response` 至少一个非空。未知输入字段会原样保留。相同配置再次运行时会复用 SQLite 状态：已经提交为 `succeeded` 的记录不会再次请求模型，遗留的 `processing` 记录会恢复为待处理。

按 `Ctrl-C` 或发送 `SIGTERM` 会停止领取新记录，并在 `runtime.shutdown_timeout` 内等待在途请求完成；超时后才取消剩余调用。程序随后在同一超时配置下导出已经进入终态的记录，并返回退出码 `130`；再次使用相同配置运行即可继续。

## 输出与审计

成功输出写入 `task.output`，按原输入顺序排列。每条记录保留未知源字段，模型生成的 `label`、`explanation`、`extended_info` 和 `annotation` 覆盖同名源字段，且 `annotation.method` 为 `auto`。

最终失败记录写入同目录失败文件。输出名以 `.jsonl` 结尾时替换为 `.failed.jsonl`；否则追加 `.failed.jsonl`。失败文件只包含 `trace_id`、错误类别、安全诊断摘要和尝试次数。两个文件都先完整写入同目录临时文件，刷新并同步后再替换正式文件。

SQLite 状态文件保存任务语义指纹、输入顺序、样本状态、请求与修复次数、重试时间、HTTP 状态、错误分类、校验错误、Token 用量、合法标注和模型原始回复。它可能包含敏感审计载荷，必须按数据集同等级别保护，且不得提交到版本库。

## 退出码

| 退出码 | 含义 |
| --- | --- |
| `0` | 所有导入记录均成功。 |
| `1` | 配置、密钥、输入、存储、认证或其他任务级错误。 |
| `2` | 处理完成，但存在最终失败记录。 |
| `130` | 任务尚未完成时被中断。 |

## 调用语义

已写入 SQLite `succeeded` 状态的记录不会重复调用。若进程在供应商已经处理请求、但本地成功事务尚未提交的极小窗口内崩溃，恢复后可能再次发送该记录。通用 OpenAI 兼容协议无法消除这个窗口，因此外部调用语义是至少一次；本地最终状态仍按 `task_id + trace_id` 幂等。
