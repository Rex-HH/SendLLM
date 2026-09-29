# SendLLM Response 标注最简使用说明

这份说明给第一次使用 SendLLM 的同学。当前默认场景是只判断模型回复 `response` 是否有安全问题，`prompt` 只作为理解回复语义的上下文。

## 1. 进入目录

```bash
cd /Users/lijiayang/venus/SendLLM
```

## 2. 准备输入 JSONL

输入文件是一行一个 JSON 对象，推荐使用 compact JSONL 格式。response-only 任务至少要有一条 `user` 消息和一条 `assistant` 消息：

```json
{"id":"sample-1","source":{"dataset":"my_dataset","path":"datasets/my/file.jsonl","index":0},"messages":[{"role":"user","content":"用户提示词"},{"role":"assistant","content":"需要判断安全性的模型回复"}],"label":{"value":"unsafe","risk_type":"原始风险类型","risk_level":"medium"},"meta":{"sample_type":"明确正例","split":"train","source_fields":{"reason":"原始标签理由"}}}
```

必须改成自己的内容：

- `id`：每条样本唯一 ID。
- `source.dataset`：数据集名称。
- `source.path`：源文件路径或分类路径。
- `source.index`：源记录序号。
- `messages`：`user` 是提示词，`assistant` 是要判断的回复。
- `label`：原始标签，可作为参考。
- `meta.source_fields.reason`：原始标签理由，可为空但建议保留。

## 3. 复制配置文件

```bash
cp config/task.006.yaml config/my-response-task.yaml
```

## 4. 修改任务路径

打开 `config/my-response-task.yaml`，修改：

```yaml
task:
  id: my-response-task
  input: ../data/my-response-task/input.jsonl
  output: ../data/my-response-task/output.jsonl
  state: ../data/my-response-task/state.db
```

必须改：

- `task.id`：新任务用新 ID。
- `task.input`：你的输入 JSONL。
- `task.output`：成功输出文件。
- `task.state`：SQLite 进度文件，新任务必须用新的 state。

## 5. 修改 API Key（二选一）

单 key：

```yaml
model:
  api_key_env: AI_GATEWAY_API_KEY
```

多 key 轮询：

```yaml
model:
  api_key_envs:
    - AI_GATEWAY_API_KEY
    - OTHER_AI_GATEWAY_API_KEY
```

`api_key_env` 和 `api_key_envs` 只能保留一个。配置文件里只写环境变量名，不写 Key 值。

环境变量名 AI_GATEWAY_API_KEY 等， 和你配置里的 apikey 名称一致。

设置环境变量：

```bash
export AI_GATEWAY_API_KEY='你的key'
export OTHER_AI_GATEWAY_API_KEY='另一个key'
```

只检查是否存在，不要打印 Key：

```bash
test -n "$AI_GATEWAY_API_KEY"
```

## 6. 确认 response-only prompt

response-only 任务使用：

```yaml
prompt:
  system_file: ../prompts/my-batch-response-only-system.txt
  scene: auto
  risk_types_file: risk-types.yaml
```

提示词自己修改，在配置里写好文件位置即可

## 7. 调整速度

```yaml
runtime:
  concurrency: 12
  requests_per_minute: 180
```

如果 429、502、network 失败很多，就降低 `concurrency` 和 `requests_per_minute`。

修改 `concurrency` 和 `requests_per_minute` 只需要中断程序，重新运行即可，会继承进度。

除此之外其他配置如 输入文件，模型名称等，不要修改，如果修改只能新建新任务。

## 8. 运行

```bash
zsh -lic './sendllm -config ./config/my-response-task.yaml'
```

我的 api key 是写在zsh全局配置里的，所以使用zsh启动，看自己情况，问大模型

## 9. 中断和继续

运行中可以按 `Ctrl+C` 中断。继续时重新执行同一条命令：

```bash
zsh -lic './sendllm -config ./config/my-response-task.yaml'
```

已成功的样本不会重复请求模型。不要同时开两个进程跑同一个 `state.db`。

## 10. 查看输出

成功输出：

```text
data/my-response-task/output.jsonl
```

失败补标文件：

```text
data/my-response-task/output.failed.jsonl
```

SQLite 进度文件：

```text
data/my-response-task/state.db
```

多 key 运行时，reconcile 日志会记录 `api_key_env`。普通 response-only 标注主要看输出和失败文件。

## 11. 不要改这些字段继续旧任务

继续同一个旧任务时，不要修改：

- `task.id`
- `task.state`
- `model.name`
- `model.structured_output`
- `model.temperature`
- `model.top_p`
- `model.max_tokens`
- `model.seed`
- `model.extra_body`
- `prompt.system_file`
- `prompt.scene`
- `prompt.risk_types_file`
- `output.schema_file`
- `output.explanation_min_length`
- `output.explanation_max_length`

可以修改：

- `model.api_key_env` / `model.api_key_envs`
- `runtime.concurrency`
- `runtime.requests_per_minute`
- `runtime.tokens_per_minute`
- `runtime.shutdown_timeout`
- `retry.initial_backoff`
- `retry.max_backoff`
