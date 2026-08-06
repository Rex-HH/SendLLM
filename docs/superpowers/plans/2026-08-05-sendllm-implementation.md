# SendLLM 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**目标：** 构建一个本地 Go CLI，以可恢复、有界并发、严格结构校验的方式完成约 30,000 条 JSONL 大模型安全数据标注。

**架构：** SQLite 是任务和样本状态的唯一可信来源，JSONL 只负责导入与确定顺序导出。调度器通过有界 Worker Pool 调用兼容 OpenAI 的 HTTP 接口，并在本地完成 Schema、业务语义、重试和幂等控制。

**技术栈：** Go 1.24+、标准库 `flag`/`net/http`/`log/slog`/`database/sql`、`gopkg.in/yaml.v3`、`modernc.org/sqlite`、`golang.org/x/time/rate`、`github.com/santhosh-tekuri/jsonschema/v5`。

## 全局约束

- 首版只能是单机单进程 CLI，不增加 HTTP 服务、外部队列、Redis、定时任务或多实例租约。
- 主命令固定为 `sendllm -config <task.yaml>`；退出码为成功 `0`、存在最终失败样本 `2`、任务级致命错误 `1`、中断 `130`。
- 输入要求 `trace_id` 非空，且 `prompt` 和可选 `response` 至少一个非空；未知字段必须往返保留。
- SQLite 中已提交为 `succeeded` 的样本不得再次调用模型；崩溃窗口内的外部调用采用至少一次语义，最终状态按 `task_id + trace_id` 幂等。
- 并发配置范围固定为 1 至 500；RPM/TPM 为零时禁用对应主动限速器；429 必须触发共享冷却。
- API Key 只从配置指定的环境变量读取；日志、测试夹具和 Git 文件不得包含真实数据、完整模型回复或凭证。
- 同一份 `output.schema_file` 同时供模型 `json_schema` 模式和本地 Schema 校验使用。
- 所有 Go 代码遵循 `AGENTS.md` 中提炼的 Rex-HH/uber_go_guide_cn 规则。
- 代码首先服务于阅读和维护：使用当前需求所需的最少代码、最直接控制流和最少抽象，不创建未来功能框架，不添加重复或推测性校验。
- 所有新增 Go 注释使用规范中文；只注释导出契约、原因、不变量和并发所有权，不逐行复述代码。
- 每个任务采用测试先行；任务结束时更新 `feature_list.json`、`progress.md`、`session-handoff.md`，并提交独立 commit。
- 每次声称完成前运行任务的窄测试；最终必须通过 `./init.sh`。
- 最终验收必须使用 `模型配置.md` 指定的真实 DeepSeek 网关和 `AI_GATEWAY_API_KEY`，处理 `Test_Input.jsonl` 的全部 50 条记录并生成通过 Schema 的 `Test_Output.jsonl`；fake server 不能替代该门禁。

## 文件职责图

```text
main.go                                  CLI 参数、依赖装配、信号和退出码
internal/lib/configs/config.go           配置类型、默认值、校验、语义指纹
internal/lib/configs/load.go             YAML/提示词/风险闭集/Schema 文件加载
internal/dto/sample.go                   可扩展输入样本与模型输入
internal/dto/annotation.go               标注结果与扩展字段 JSON 往返
internal/dto/completion.go               模型消息、用量和供应商错误契约
internal/dao/sqlite.go                   SQLite 打开、关闭、迁移和任务身份
internal/dao/import.go                   事务导入与 trace_id 冲突判断
internal/dao/items.go                    领取、状态迁移、尝试记录和统计查询
internal/service/importer.go             JSONL 流式解析和导入协调
internal/service/validator.go            JSON Schema 与 MASB 语义校验
internal/facade/openai.go                OpenAI Chat Completions HTTP 适配器
internal/lib/tokenizer/estimate.go       保守 Token 数量估算
internal/lib/limiter/limiter.go           并发、RPM、TPM 和共享冷却
internal/service/retry.go                错误分类与完全抖动退避
internal/service/runner.go               可恢复调度、Worker 生命周期和修复调用
internal/service/exporter.go             成功/失败 JSONL 原子有序导出
internal/service/progress.go             无载荷聚合进度快照
config/task.example.yaml                 可运行任务配置示例
config/risk-types.yaml                   MASB 风险类型闭集
config/result-schema.json                模型和本地共用结果 Schema
prompts/masb-system.txt                  默认 MASB 系统提示词
README.md                                构建、运行、恢复、限速和结果说明
```

---

### Task 1：Go 工程、配置和数据契约

**对应 Harness 功能：** `feat-001`

**文件：**
- 创建：`go.mod`
- 创建：`internal/lib/configs/config.go`
- 创建：`internal/lib/configs/load.go`
- 创建：`internal/lib/configs/config_test.go`
- 创建：`internal/dto/sample.go`
- 创建：`internal/dto/sample_test.go`
- 创建：`internal/dto/annotation.go`
- 创建：`internal/dto/annotation_test.go`
- 创建：`internal/dto/completion.go`
- 创建：`config/task.example.yaml`
- 创建：`config/risk-types.yaml`
- 创建：`config/result-schema.json`
- 创建：`prompts/masb-system.txt`
- 修改：`feature_list.json`
- 修改：`progress.md`
- 修改：`session-handoff.md`

**接口：**
- 产出：`configs.Load(path string) (*configs.Config, error)`
- 产出：`(*configs.Config).Validate() error`
- 产出：`(*configs.Config).SemanticFingerprint() (string, error)`
- 产出：`(*configs.Config).APIKey() (string, error)`
- 产出：`dto.ParseSource(raw []byte) (dto.SourceSample, error)`
- 产出：`dto.SourceSample.ModelInput(scene string) dto.ModelInput`
- 产出：`dto.Annotation`、`dto.ExtendedInfo`、`dto.AnnotationMeta`、`dto.Message`、`dto.CompletionRequest`、`dto.CompletionResponse`、`dto.ProviderError`

- [ ] **步骤 1：初始化模块并锁定依赖**

运行：

```bash
go mod init sendllm
go mod edit -go=1.24
go get gopkg.in/yaml.v3 modernc.org/sqlite golang.org/x/time/rate github.com/santhosh-tekuri/jsonschema/v5
```

预期：生成 `go.mod`/`go.sum`，模块名为 `sendllm`，Go 版本为 `1.24`。

- [ ] **步骤 2：先写配置失败测试**

在 `internal/lib/configs/config_test.go` 添加表驱动测试，至少覆盖默认值、并发 0/501、缺失 Schema、重试上限、相对路径解析、系统提示词占位符缺失或重复，以及受保护 `extra_body` 键：

```go
func TestLoad(t *testing.T) {
  tests := []struct {
    name    string
    yaml    string
    wantErr error
  }{
    {name: "rejects zero concurrency", yaml: validYAML("concurrency: 0"), wantErr: configs.ErrInvalidConfig},
    {name: "rejects concurrency above account ceiling", yaml: validYAML("concurrency: 501"), wantErr: configs.ErrInvalidConfig},
    {name: "rejects missing result schema", yaml: validYAML("schema_file: missing.json"), wantErr: configs.ErrInvalidConfig},
  }
  for _, tt := range tests {
    tt := tt
    t.Run(tt.name, func(t *testing.T) {
      t.Parallel()
      _, err := configs.Load(writeConfig(t, tt.yaml))
      if !errors.Is(err, tt.wantErr) {
        t.Fatalf("Load() error = %v, want %v", err, tt.wantErr)
      }
    })
  }
}
```

- [ ] **步骤 3：运行配置测试并确认失败**

运行：`go test ./internal/lib/configs -run TestLoad -v`

预期：因 `configs.Load` 尚不存在而编译失败。

- [ ] **步骤 4：实现类型化配置、默认值、加载和语义指纹**

核心类型必须采用以下字段和常量：

```go
const MaxConcurrency = 500

var ErrInvalidConfig = errors.New("configs: invalid configuration")

type Config struct {
  Task    TaskConfig    `yaml:"task"`
  Model   ModelConfig   `yaml:"model"`
  Prompt  PromptConfig  `yaml:"prompt"`
  Runtime RuntimeConfig `yaml:"runtime"`
  Retry   RetryConfig   `yaml:"retry"`
  Output  OutputConfig  `yaml:"output"`

  SystemPrompt []byte              `yaml:"-"`
  RiskTypes    map[string]string   `yaml:"-"`
  ResultSchema json.RawMessage     `yaml:"-"`
}

type RuntimeConfig struct {
  Concurrency      int           `yaml:"concurrency"`
  RequestsPerMinute int          `yaml:"requests_per_minute"`
  TokensPerMinute int            `yaml:"tokens_per_minute"`
  ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
}

type ModelConfig struct {
  BaseURL         string         `yaml:"base_url"`
  APIKeyEnv       string         `yaml:"api_key_env"`
  Name            string         `yaml:"name"`
  StructuredOutput string        `yaml:"structured_output"`
  Temperature     *float64       `yaml:"temperature"`
  TopP            *float64       `yaml:"top_p"`
  MaxTokens       int            `yaml:"max_tokens"`
  Seed            *int64         `yaml:"seed"`
  Timeout         time.Duration  `yaml:"timeout"`
  ExtraBody       map[string]any `yaml:"extra_body"`
}
```

`Load` 必须先用 `yaml.Decoder.KnownFields(true)` 解析，再以配置文件目录为基准解析输入、输出、状态、提示词、风险闭集和 Schema 路径，读取三个附属文件，应用明确默认值，最后调用 `Validate`。系统提示词必须各包含一次字面量 `{{RISK_TYPES}}` 和 `{{RESULT_SCHEMA}}`；加载器分别用按键排序后的风险分类 JSON 和完整 Schema JSON 替换，得到最终 `SystemPrompt`。不使用可执行模板语法。`ExtraBody` 必须拒绝 `model`、`messages`、`response_format`、`stream`、`temperature`、`top_p`、`max_tokens` 和 `seed` 等受客户端管理的键。语义指纹使用 SHA-256，对模型名称、全部类型化采样参数、排序后的 `ExtraBody`、结构化输出模式、场景、替换后的系统提示词、风险闭集排序结果和 Schema 原始内容进行稳定 JSON 编码；不得包含并发、限速、重试等待、状态路径或输出路径。

- [ ] **步骤 5：先写输入样本失败测试**

在 `internal/dto/sample_test.go` 覆盖缺失、`null` 和空 `response`，未知字段保留，以及两个内容字段都为空：

```go
func TestParseSource_ResponseOptional(t *testing.T) {
  give := []byte(`{"trace_id":"id-1","prompt":"hello","source":"CERT"}`)
  got, err := dto.ParseSource(give)
  if err != nil {
    t.Fatalf("ParseSource() error = %v", err)
  }
  if got.Response != "" {
    t.Errorf("Response = %q, want empty", got.Response)
  }
  if string(got.Extra["source"]) != `"CERT"` {
    t.Errorf("Extra[source] = %s", got.Extra["source"])
  }
}
```

- [ ] **步骤 6：实现可扩展输入和标注 JSON 契约**

使用显式自定义编解码保留未知字段：

```go
type SourceSample struct {
  TraceID  string
  Prompt   string
  Response string
  Extra    map[string]json.RawMessage
  Raw      json.RawMessage
}

type ExtendedInfo struct {
  RiskType      string
  RiskLevel     string
  AttackScenario string
  CaseType      string
  IsAttack      *bool
  Other         string
  Extra         map[string]json.RawMessage
}

type Annotation struct {
  Label        string        `json:"label"`
  Explanation  string        `json:"explanation"`
  ExtendedInfo *ExtendedInfo `json:"extended_info,omitempty"`
}

type AnnotationMeta struct {
  Method       string  `json:"method"`
  ReviewedBy   string  `json:"reviewed_by,omitempty"`
  QualityScore *float64 `json:"quality_score,omitempty"`
}

type ProviderError struct {
  Kind       ProviderErrorKind
  StatusCode int
  RetryAfter time.Duration
  Err        error
}

func (e *ProviderError) Error() string
func (e *ProviderError) Unwrap() error

type ProviderErrorKind string

const (
  ProviderNetwork           ProviderErrorKind = "network"
  ProviderTimeout           ProviderErrorKind = "timeout"
  ProviderRateLimited       ProviderErrorKind = "rate_limited"
  ProviderServer            ProviderErrorKind = "server"
  ProviderAuthentication    ProviderErrorKind = "authentication"
  ProviderBadRequest        ProviderErrorKind = "bad_request"
  ProviderContentRejected   ProviderErrorKind = "content_rejected"
  ProviderMalformedResponse ProviderErrorKind = "malformed_response"
)
```

`ParseSource` 解码到 `map[string]json.RawMessage`，读取并删除三个已知键，校验 `trace_id` 和内容字段，再复制剩余 map。标注顶层拒绝未知字段，只有 `extended_info` 允许并保留 Schema 已许可的扩展字段。

- [ ] **步骤 7：编写真实示例配置、风险闭集、Schema 和系统提示词**

`config/risk-types.yaml` 必须包含 `样本格式-8-4.md` 的全部英文风险枚举及中文说明。`config/result-schema.json` 必须要求 `label` 和 `explanation`，限制核心枚举，并允许 `extended_info` 中已列出的 MASB 字段；风险闭集由运行时语义校验二次执行。系统提示词必须包含两个字面占位符，要求只输出 JSON、不得回显原文、风险类型单选、解释长度 10 至 70 个字符，并说明安全结果规则。`config/task.example.yaml` 的模型配置使用真实验收值：`base_url: https://aigateway.venusgroup.com.cn/ai/deepseek/openai`、`name: deepseek-v4-pro`、`api_key_env: AI_GATEWAY_API_KEY`；不得写入 Key 值。

- [ ] **步骤 8：运行任务 1 测试和格式检查**

运行：

```bash
gofmt -w internal/lib/configs internal/dto
go test ./internal/lib/configs ./internal/dto
go vet ./internal/lib/configs ./internal/dto
```

预期：全部通过。

- [ ] **步骤 9：更新 Harness 状态并提交**

将 `feat-001.status` 设为 `done`，记录上一步命令；`progress.md` 的下一项改为 `feat-002`。

```bash
git add go.mod go.sum internal config prompts feature_list.json progress.md session-handoff.md
git commit -m "feat: add task configuration and data contracts"
```

---

### Task 2：JSONL 流式导入和 SQLite 状态

**对应 Harness 功能：** `feat-002`

**文件：**
- 创建：`internal/dao/sqlite.go`
- 创建：`internal/dao/schema.sql`
- 创建：`internal/dao/import.go`
- 创建：`internal/dao/items.go`
- 创建：`internal/dao/sqlite_test.go`
- 创建：`internal/service/importer.go`
- 创建：`internal/service/importer_test.go`
- 创建：`internal/service/testdata/valid.jsonl`
- 修改：`.gitignore`
- 修改：`feature_list.json`
- 修改：`progress.md`
- 修改：`session-handoff.md`

**接口：**
- 消费：`configs.Config.SemanticFingerprint()`、`dto.ParseSource([]byte)`
- 产出：`dao.Open(ctx context.Context, path string) (*dao.Store, error)`
- 产出：`(*dao.Store).EnsureTask(ctx context.Context, task dao.Task) error`
- 产出：`(*dao.Store).BeginImport(ctx context.Context, taskID string) (*dao.Import, error)`
- 产出：`(*dao.Import).Add(ctx context.Context, item dao.Item) (dao.ImportDisposition, error)`
- 产出：`(*dao.Import).Commit() error`、`(*dao.Import).Rollback() error`
- 产出：`service.Import(ctx context.Context, store *dao.Store, taskID string, r io.Reader) (service.ImportStats, error)`

DAO 的跨任务核心类型固定为：

```go
type ItemState string

const (
  ItemPending    ItemState = "pending"
  ItemProcessing ItemState = "processing"
  ItemRetryWait  ItemState = "retry_wait"
  ItemSucceeded  ItemState = "succeeded"
  ItemFailed     ItemState = "failed"
)

type Item struct {
  TaskID          string
  TraceID         string
  InputIndex      int64
  SourceHash      string
  RawJSON         []byte
  Prompt          string
  Response        string
  State           ItemState
  RequestAttempts int
  RepairAttempts  int
  NextAttemptAt   time.Time
}
```

- [ ] **步骤 1：先写 SQLite 任务身份和导入事务测试**

测试必须验证首次创建、相同语义指纹恢复、不同语义指纹拒绝、完全相同重复项跳过、冲突项回滚：

```go
func TestStore_EnsureTaskRejectsSemanticChange(t *testing.T) {
  store := openTestStore(t)
  ctx := context.Background()
  if err := store.EnsureTask(ctx, dao.Task{ID: "task-1", SemanticHash: "hash-a"}); err != nil {
    t.Fatalf("EnsureTask(first) error = %v", err)
  }
  err := store.EnsureTask(ctx, dao.Task{ID: "task-1", SemanticHash: "hash-b"})
  if !errors.Is(err, dao.ErrTaskMismatch) {
    t.Fatalf("EnsureTask(second) error = %v, want %v", err, dao.ErrTaskMismatch)
  }
}
```

- [ ] **步骤 2：运行 DAO 测试并确认失败**

运行：`go test ./internal/dao -run 'TestStore_' -v`

预期：因 DAO 包尚不存在而失败。

- [ ] **步骤 3：实现 SQLite 打开、迁移和表结构**

使用 `//go:embed schema.sql`。`Open` 必须设置 WAL、foreign keys、busy timeout，并限制单个写连接：

```go
db.SetMaxOpenConns(1)
db.SetMaxIdleConns(1)
```

表结构必须包含 `tasks`、`items`、`attempts`。`items` 的主键为 `(task_id, trace_id)`，`UNIQUE(task_id, input_index)` 保证顺序唯一；状态使用 CHECK 约束限制为五个合法值。尝试表保存 `phase`、时间、HTTP 状态、错误类别、是否可重试、原始模型回复、校验错误和 Token 用量。

- [ ] **步骤 4：实现事务导入对象**

`Import.Add` 对规范化后的完整源 JSON 计算 SHA-256：先解码为 `any`，再用 `encoding/json` 重新编码，使对象键顺序和无意义空白不影响哈希。记录不存在时插入，哈希相同返回 `ImportSkipped`，哈希不同返回包裹 `ErrTraceConflict` 的错误。调用者必须 `defer imp.Rollback()`，只有全部 JSONL 行合法时调用 `Commit()`。

- [ ] **步骤 5：先写 JSONL 导入测试和 fuzz 入口**

覆盖空行拒绝、非法 JSON、超过 16 MiB 的单行、响应缺失、未知字段、冲突导致整批回滚：

```go
func FuzzParseJSONL(f *testing.F) {
  f.Add([]byte("{\"trace_id\":\"id-1\",\"prompt\":\"ok\"}\n"))
  f.Add([]byte("not-json\n"))
  f.Fuzz(func(t *testing.T, give []byte) {
    store := openTestStore(t)
    _, _ = service.Import(context.Background(), store, "task-fuzz", bytes.NewReader(give))
  })
}
```

- [ ] **步骤 6：实现流式 Importer**

使用 `bufio.Scanner` 并将最大 token 设置为 `16 << 20`。逐行调用 `dto.ParseSource`，输入序号从 1 开始；错误必须包含行号但保留 `errors.Is`。不得把全部文件加载进内存。冲突或解析错误时回滚整个导入事务。

- [ ] **步骤 7：运行任务 2 测试**

运行：

```bash
gofmt -w internal/dao internal/service
go test ./internal/dao ./internal/service -run 'Test(Store|Import)'
go test -race ./internal/dao ./internal/service -run 'Test(Store|Import)'
```

预期：全部通过，冲突测试确认数据库内没有半批数据。

- [ ] **步骤 8：更新 Harness 状态并提交**

将 `feat-002.status` 设为 `done` 并记录验证证据。

```bash
git add .gitignore internal/dao internal/service feature_list.json progress.md session-handoff.md
git commit -m "feat: persist resumable JSONL imports"
```

---

### Task 3：OpenAI 适配器和严格结果校验

**对应 Harness 功能：** `feat-003`

**文件：**
- 创建：`internal/service/completer.go`
- 创建：`internal/service/validator.go`
- 创建：`internal/service/validator_test.go`
- 创建：`internal/facade/openai.go`
- 创建：`internal/facade/openai_test.go`
- 修改：`internal/dto/completion.go`
- 修改：`feature_list.json`
- 修改：`progress.md`
- 修改：`session-handoff.md`

**接口：**
- 消费：`dto.Message`、`dto.Annotation`、配置中的 Schema 和模型参数
- 产出：`service.Completer`：`Complete(ctx context.Context, req dto.CompletionRequest) (dto.CompletionResponse, error)`
- 产出：`service.NewValidator(schema json.RawMessage, riskTypes map[string]string, minExplanation, maxExplanation int) (*service.Validator, error)`
- 产出：`(*service.Validator).Validate(raw []byte) (dto.Annotation, error)`
- 产出：`facade.NewOpenAI(cfg facade.Config) (*facade.OpenAI, error)`

边界配置和可修复校验错误固定为：

```go
// 位于 internal/facade 包。
type Config struct {
  BaseURL       string
  APIKey        string
  Model         string
  Temperature   *float64
  TopP          *float64
  MaxTokens     int
  Seed          *int64
  ExtraBody     map[string]any
  Timeout       time.Duration
  MaxConnections int
}

var ErrInvalidResult = errors.New("service: invalid model result")

type ValidationError struct {
  Problems []string
}

func (e *ValidationError) Error() string
func (e *ValidationError) Unwrap() error // 返回 ErrInvalidResult
```

- [ ] **步骤 1：先写结果验证矩阵**

表驱动测试至少包含合法 unsafe、合法 safe、合法 hard negative、非法 JSON、前后夹杂文字、未知顶层字段、未知风险类型、unsafe 缺失 risk level、safe 携带 risk type、解释过短：

```go
func TestValidator_Validate(t *testing.T) {
  tests := []struct {
    name    string
    give    string
    wantErr error
  }{
    {name: "accepts unsafe", give: validUnsafe, wantErr: nil},
    {name: "rejects surrounding prose", give: "result: " + validUnsafe, wantErr: service.ErrInvalidResult},
    {name: "rejects unknown risk", give: withRisk("unknown"), wantErr: service.ErrInvalidResult},
    {name: "rejects safe risk category", give: safeWithRisk, wantErr: service.ErrInvalidResult},
  }
  // 每个子测试创建独立 Validator，并使用 errors.Is 断言。
}
```

- [ ] **步骤 2：运行 Validator 测试并确认失败**

运行：`go test ./internal/service -run TestValidator -v`

预期：因 `NewValidator` 不存在而编译失败。

- [ ] **步骤 3：实现 Schema 编译和业务校验**

`NewValidator` 使用 `jsonschema.NewCompiler()` 加载内存 Schema。`Validate` 先把原始字节解码成 `any` 并确认只含一个完整 JSON 值，再执行 Schema 校验，然后严格解码 `dto.Annotation`，最后执行 MASB 跨字段规则。所有失败均包裹 `ErrInvalidResult`，并返回不包含原始安全数据的字段级摘要。

- [ ] **步骤 4：先写模拟 OpenAI 服务测试**

使用 `httptest.Server` 捕获请求并返回确定响应，覆盖三种 `response_format`、Authorization、usage、429/Retry-After、401、5xx、超大响应和 `finish_reason=content_filter`：

```go
func TestOpenAI_CompleteRateLimited(t *testing.T) {
  server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Retry-After", "2")
    http.Error(w, `{"error":{"message":"rate limited"}}`, http.StatusTooManyRequests)
  }))
  t.Cleanup(server.Close)

  client := newTestClient(t, server.URL)
  _, err := client.Complete(context.Background(), validCompletionRequest())
  var providerErr *dto.ProviderError
  if !errors.As(err, &providerErr) || providerErr.Kind != dto.ProviderRateLimited {
    t.Fatalf("Complete() error = %v, want rate-limited ProviderError", err)
  }
  if providerErr.RetryAfter != 2*time.Second {
    t.Errorf("RetryAfter = %v, want 2s", providerErr.RetryAfter)
  }
}
```

- [ ] **步骤 5：实现标准库 OpenAI Chat Completions 客户端**

客户端请求固定使用 `POST {base_url}/chat/completions`，显式设置 `Authorization: Bearer ...` 和 JSON Content-Type。按配置生成：

```go
type CompletionRequest struct {
  Messages []Message
  Schema   json.RawMessage
  Mode     string
}

type CompletionResponse struct {
  Content      []byte
  RawResponse  []byte
  FinishReason string
  Usage        Usage
}
```

`json_schema` 模式发送 `name=safety_annotation`、`strict=true` 和同一份 Schema；`json_object` 只发送 `{type: json_object}`；`prompt_only` 省略 `response_format`。响应体上限设为 4 MiB。HTTP 408/429/5xx 映射为可重试 ProviderError，401/403 为认证错误，400 为永久请求错误，内容过滤为记录级永久错误。解析 `Retry-After` 的秒数和 HTTP 日期两种形式。

请求 JSON 先写入受客户端管理的 `model`、`messages`、类型化采样参数和 `response_format`，再合并已经在配置层验证的 `extra_body`。HTTP Client timeout 使用模型配置值；Transport 的 `MaxIdleConns` 和 `MaxIdleConnsPerHost` 至少设置为任务并发数，`IdleConnTimeout` 设置为 90 秒，确保并发上调时不会被默认连接池限制。

- [ ] **步骤 6：运行任务 3 测试**

运行：

```bash
gofmt -w internal/service internal/facade internal/dto
go test ./internal/service -run TestValidator
go test ./internal/facade
go test -race ./internal/facade
```

预期：全部通过，测试输出不包含请求中的原始消息。

- [ ] **步骤 7：更新 Harness 状态并提交**

将 `feat-003.status` 设为 `done` 并记录验证证据。

```bash
git add internal/dto internal/service internal/facade feature_list.json progress.md session-handoff.md
git commit -m "feat: validate OpenAI structured annotations"
```

---

### Task 4：Token 估算、三重限速和重试策略

**对应 Harness 功能：** `feat-004` 的基础部分；本任务结束时保持 `in-progress`。

**文件：**
- 创建：`internal/lib/tokenizer/estimate.go`
- 创建：`internal/lib/tokenizer/estimate_test.go`
- 创建：`internal/lib/limiter/limiter.go`
- 创建：`internal/lib/limiter/limiter_test.go`
- 创建：`internal/service/retry.go`
- 创建：`internal/service/retry_test.go`
- 修改：`feature_list.json`
- 修改：`progress.md`
- 修改：`session-handoff.md`

**接口：**
- 产出：`tokenizer.Estimate(messages []dto.Message, maxOutputTokens int) int`
- 产出：`limiter.New(cfg limiter.Config) (*limiter.Limiter, error)`
- 产出：`(*limiter.Limiter).Acquire(ctx context.Context, estimatedTokens int) (release func(), err error)`
- 产出：`(*limiter.Limiter).Cooldown(until time.Time)`
- 产出：`service.RetryPolicy.Delay(attempt int, retryAfter time.Duration, jitter func(time.Duration) time.Duration) time.Duration`
- 产出：`service.ClassifyFailure(err error) service.FailureDecision`

重试契约固定为：

```go
type FailureDecision struct {
  Category       string
  Retry          bool
  GlobalCooldown bool
  RetryAfter     time.Duration
}

type RetryPolicy struct {
  MaxAttempts    int
  InitialBackoff time.Duration
  MaxBackoff     time.Duration
}
```

- [ ] **步骤 1：先写 Token 估算和重试分类测试**

Token 估算采用保守且与供应商无关的规则：消息固定开销加 `max(UTF-8 rune 数, 字节数/4)`，再加最大输出 Token。测试 ASCII、中文、空消息和多个消息。重试测试覆盖网络错误、context deadline、408、429、5xx、401、400 和内容拒绝。

- [ ] **步骤 2：运行窄测试并确认失败**

运行：

```bash
go test ./internal/lib/tokenizer
go test ./internal/service -run 'Test(RetryPolicy|ClassifyFailure)'
```

预期：因对应符号不存在而失败。

- [ ] **步骤 3：实现 Token 估算和完全抖动退避**

退避上限按 `min(maxBackoff, initialBackoff*2^(attempt-1))` 计算，jitter 返回 `[0, 上限]`。`Retry-After` 大于零时直接使用且不受普通退避上限截断，以供应商明确返回的等待时间为准。认证和永久请求错误返回 `Retry=false`，网络/超时/408/429/5xx 返回 `Retry=true`，429 同时返回 `GlobalCooldown=true`。

- [ ] **步骤 4：先写 Limiter 并发和共享冷却测试**

测试两个 goroutine 在 `Concurrency=1` 时第二个必须等 release；调用 `Cooldown(now+30ms)` 后 Acquire 不得提前返回；RPM/TPM 为零时不得阻塞。测试必须使用超时 context，不能无限等待。

- [ ] **步骤 5：实现三重 Limiter**

```go
type Config struct {
  Concurrency       int
  RequestsPerMinute int
  TokensPerMinute   int
}

type Limiter struct {
  semaphore chan struct{} // 容量严格等于配置并发，用作有界背压。
  requests  *rate.Limiter
  tokens    *rate.Limiter
  mu        sync.Mutex
  coolUntil time.Time
}
```

Acquire 依次等待共享冷却、RPM、TPM，最后占用并发许可；任何等待都监听 `ctx.Done()`。RPM limiter 使用每秒 `rpm/60`、burst 1；TPM limiter 使用每秒 `tpm/60`、burst `tpm`，预估单次 Token 超过整分钟预算时返回 `ErrTokenBudgetExceeded`。`Cooldown` 只能延长、不能缩短当前共享等待期限。返回的 release 必须通过 `sync.Once` 保证重复调用安全。容量大于 1 的 channel 仅用于明确的并发信号量，注释说明其背压含义。

- [ ] **步骤 6：运行任务 4 基础测试和竞态检查**

运行：

```bash
gofmt -w internal/lib/tokenizer internal/lib/limiter internal/service
go test ./internal/lib/tokenizer ./internal/lib/limiter ./internal/service -run 'Test(Estimate|Limiter|RetryPolicy|ClassifyFailure)'
go test -race ./internal/lib/limiter
```

预期：全部通过。

- [ ] **步骤 7：更新 Harness 中间状态并提交**

将 `feat-004.status` 设为 `in-progress`，证据注明限速与重试基础已完成、Runner 尚未完成。

```bash
git add internal/lib internal/service feature_list.json progress.md session-handoff.md
git commit -m "feat: add rate limits and retry policy"
```

---

### Task 5：可恢复并发 Runner 和格式修复

**对应 Harness 功能：** 完成 `feat-004`

**文件：**
- 修改：`internal/dao/items.go`
- 创建：`internal/dao/items_test.go`
- 创建：`internal/service/runner.go`
- 创建：`internal/service/runner_test.go`
- 创建：`internal/service/progress.go`
- 修改：`feature_list.json`
- 修改：`progress.md`
- 修改：`session-handoff.md`

**接口：**
- 消费：`service.Completer`、`service.Validator`、`limiter.Limiter`、`service.RetryPolicy`、`dao.Store`
- 产出：`service.NewRunner(cfg service.RunnerConfig) (*service.Runner, error)`
- 产出：`(*service.Runner).Run(ctx context.Context) (service.Summary, error)`
- 产出：`(*dao.Store).ResetProcessing(ctx context.Context, taskID string) (int64, error)`
- 产出：`(*dao.Store).Claim(ctx context.Context, taskID string, limit int, now time.Time) ([]dao.Item, error)`
- 产出：`(*dao.Store).MarkSucceeded(ctx context.Context, taskID, traceID string, attempt dao.Attempt, annotation []byte) error`
- 产出：`(*dao.Store).ScheduleRetry(ctx context.Context, taskID, traceID string, attempt dao.Attempt, next time.Time, category, summary string) error`
- 产出：`(*dao.Store).MarkFailed(ctx context.Context, taskID, traceID string, attempt dao.Attempt, category, summary string) error`
- 产出：`(*dao.Store).Counts(ctx context.Context, taskID string) (dao.Counts, error)`
- 产出：`(*dao.Store).NextRetryAt(ctx context.Context, taskID string) (time.Time, bool, error)`

Runner 的构造契约固定为：

```go
type RunnerConfig struct {
  TaskID              string
  SystemPrompt         []byte
  Scene                string
  Schema               json.RawMessage
  Mode                 string // 来自 model.structured_output
  MaxOutputTokens      int
  RequestMaxAttempts   int
  FormatRepairAttempts int
  Store                *dao.Store
  Completer            Completer
  Validator            *Validator
  Limiter              *limiter.Limiter
  RetryPolicy          RetryPolicy
  Jitter               func(time.Duration) time.Duration
  OnProgress           func(Summary)
}

type Summary struct {
  Pending   int64
  Retrying  int64
  Succeeded int64
  Failed    int64
  Rate      float64
  ETA       time.Duration
}

type Attempt struct {
  Phase            string
  RequestNumber    int
  RepairNumber     int
  StartedAt        time.Time
  FinishedAt       time.Time
  HTTPStatus       int
  ErrorCategory    string
  Retryable        bool
  RawResponse      []byte
  ValidationErrors []string
  InputTokens      int
  OutputTokens     int
}

type Counts struct {
  Pending    int64
  Processing int64
  RetryWait  int64
  Succeeded  int64
  Failed     int64
}
```

- [ ] **步骤 1：先写 DAO 状态机测试**

验证合法路径 `pending -> processing -> succeeded`、`processing -> retry_wait -> processing`、最终失败、重启重置，以及 succeeded 无法再次 Claim。每个状态迁移使用 `WHERE state = expected` 并检查影响行数，非法迁移返回 `dao.ErrInvalidTransition`。

- [ ] **步骤 2：实现原子领取和状态迁移**

`Claim` 在事务中按 `input_index` 选择到期的 `pending/retry_wait`，批量改为 `processing` 后再返回完整记录。由于单进程和单写连接，不引入租约字段。`ResetProcessing` 只在启动恢复时调用。尝试记录和最终状态更新必须处于同一事务。

- [ ] **步骤 3：先写 Runner 行为测试**

使用线程安全 fake Completer 记录每个 `trace_id` 的调用次数，覆盖：

```go
func TestRunner_DoesNotRepeatSucceededItems(t *testing.T) {
  store := seededStore(t, "pending", "succeeded")
  fake := &fakeCompleter{responses: map[string][]byte{"pending-id": []byte(validSafe)}}
  runner := newTestRunner(t, store, fake)
  summary, err := runner.Run(context.Background())
  if err != nil {
    t.Fatalf("Run() error = %v", err)
  }
  if fake.Calls("succeeded-id") != 0 {
    t.Errorf("succeeded item calls = %d, want 0", fake.Calls("succeeded-id"))
  }
  if summary.Succeeded != 2 {
    t.Errorf("Succeeded = %d, want 2", summary.Succeeded)
  }
}
```

其他用例包括并发上限不越界、429 共享冷却、5xx 延迟重试、401 暂停任务、无效 JSON 修复成功、修复耗尽后重新分类、最终失败、取消后 goroutine 全部退出、遗留 processing 在下次 Run 被恢复。

- [ ] **步骤 4：运行 Runner 测试并确认失败**

运行：`go test ./internal/service -run TestRunner -v`

预期：因 Runner 尚不存在而编译失败。

- [ ] **步骤 5：实现原始分类和格式修复消息构造**

原始请求消息为系统提示词加结构化 `dto.ModelInput`。修复请求使用固定系统消息“仅根据给定 Schema 修复 JSON”，用户消息只包含 `invalid_response`、`validation_errors` 和 `schema`，不得包含源 `prompt` 或 `response`。每次请求前调用 Token 估算和 Limiter Acquire，返回后立即 release。

- [ ] **步骤 6：实现 Runner 生命周期**

Runner 启动时执行 `ResetProcessing`，创建配置数量的 Worker，并用 `sync.WaitGroup` 跟踪。任务 channel 使用无缓冲交接。调度器小批量 Claim；没有可运行记录但存在 `retry_wait` 时等待 `NextRetryAt`；全部记录终态时关闭发送端并等待 Worker。所有等待和 HTTP 调用传递顶层 context。

Worker 将每次原始调用或修复调用记录为独立 attempt。成功时事务写入 attempt 和 annotation；可重试错误写入 `retry_wait` 及下次执行时间；记录级永久错误写入 `failed`；认证或全局配置错误取消内部 context 并向 Run 返回任务级错误。顶层 context 被取消时不强行使用已取消 context 更新数据库，遗留 `processing` 由下次启动恢复。

- [ ] **步骤 7：实现无载荷进度快照**

`Summary` 和进度回调只包含状态计数、完成速率和预计剩余时间。RunnerConfig 提供 `OnProgress func(Summary)`，默认空函数；不得传递 dto.SourceSample 或模型原始内容。

- [ ] **步骤 8：运行 Runner 测试和竞态检查**

运行：

```bash
gofmt -w internal/dao internal/service
go test ./internal/dao ./internal/service
go test -race ./internal/dao ./internal/service
```

预期：全部通过，没有 goroutine 泄漏、并发计数越界或 SQLite 锁错误。

- [ ] **步骤 9：更新 Harness 状态并提交**

将 `feat-004.status` 设为 `done`，证据记录普通测试和 race 测试。

```bash
git add internal/dao internal/service feature_list.json progress.md session-handoff.md
git commit -m "feat: run resumable concurrent annotations"
```

---

### Task 6：确定顺序导出、CLI 和操作文档

**对应 Harness 功能：** `feat-005`

**文件：**
- 创建：`internal/service/exporter.go`
- 创建：`internal/service/exporter_test.go`
- 创建：`main.go`
- 创建：`main_test.go`
- 创建：`README.md`
- 修改：`config/task.example.yaml`
- 修改：`feature_list.json`
- 修改：`progress.md`
- 修改：`session-handoff.md`

**接口：**
- 消费：前六个包的已定义接口
- 产出：`service.Export(ctx context.Context, store *dao.Store, taskID, outputPath string) (service.ExportStats, error)`
- 产出：`run(ctx context.Context, args []string, stdout, stderr io.Writer) int`
- 产出：`(*dao.Store).ForEachSucceeded(ctx context.Context, taskID string, visit func(dao.ExportRecord) error) error`
- 产出：`(*dao.Store).ForEachFailed(ctx context.Context, taskID string, visit func(dao.FailedRecord) error) error`

- [ ] **步骤 1：先写导出测试**

测试以乱序完成的三条样本为输入，断言成功文件仍按 `input_index` 排列，原未知字段保留，模型生成字段覆盖同名源字段，失败文件只含安全诊断字段。另一个测试在目标文件已有旧内容时验证成功原子替换，并在导出错误时保留旧文件。

- [ ] **步骤 2：实现原子有序导出**

DAO 提供按 `input_index` 流式查询成功和失败记录的方法。导出器逐行解码原始 `map[string]json.RawMessage`，覆盖 `label`、`explanation`、`extended_info` 和 `annotation`，其中 `annotation` 至少写入 `{"method":"auto"}`。使用 `bufio.Writer` 和 `json.Encoder` 写入目标目录中的 `os.CreateTemp` 文件，依次执行 Flush、Sync、Close，再调用 `os.Rename`。失败路径关闭并删除临时文件，不删除旧正式文件。失败文件名固定为：输出名以 `.jsonl` 结尾时替换为 `.failed.jsonl`，否则追加 `.failed.jsonl`。

- [ ] **步骤 3：先写 CLI 退出码测试**

通过注入临时配置和 fake HTTP Server 覆盖：全部成功返回 0、存在最终记录失败返回 2、缺失 API Key 返回 1、已取消 context 返回 130。测试捕获 stdout/stderr，断言其中不出现 fixture 的 prompt/response。

- [ ] **步骤 4：实现薄 `main.go`**

```go
func main() {
  ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
  defer stop()
  os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
```

`run` 只解析 `-config`，装配配置、slog、SQLite、Importer、Validator、OpenAI、Limiter、Runner 和 Exporter。应用日志使用 `slog` 文本处理器，字段只包含任务 ID、trace ID、计数、耗时、状态码和错误类别。中断判断优先于一般错误映射，确保返回 130。

- [ ] **步骤 5：编写 README 操作手册**

README 必须说明构建、API Key 环境变量、配置字段、运行/中断/恢复、输出和失败文件、SQLite 审计内容、并发从 64 校准到 500、RPM/TPM 为零的含义、结构化输出三种模式、退出码，以及至少一次调用语义的崩溃窗口。示例不得包含真实 endpoint、Key 或数据。

- [ ] **步骤 6：运行任务 6 测试**

运行：

```bash
gofmt -w main.go main_test.go internal/service
go test ./internal/service -run TestExport
go test . -run TestRun
go test -race . ./internal/service
```

预期：全部通过，临时目录中不存在遗留导出临时文件。

- [ ] **步骤 7：更新 Harness 状态并提交**

将 `feat-005.status` 设为 `done` 并记录验证证据。

```bash
git add main.go main_test.go internal/service README.md config feature_list.json progress.md session-handoff.md
git commit -m "feat: add CLI and ordered JSONL export"
```

---

### Task 7：全量验证、fuzz 冒烟和真实模型端到端交付

**对应 Harness 功能：** `feat-006`

**文件：**
- 修改：`go.mod`
- 修改：`go.sum`
- 修改：`feature_list.json`
- 修改：`progress.md`
- 修改：`session-handoff.md`
- 按失败结果修改：仅限前述任务创建的实现或测试文件

**接口：**
- 消费：完整 CLI 和 Harness 验证入口
- 产出：可从干净检出构建、测试、恢复并通过真实模型端到端验收的首版 SendLLM

- [ ] **步骤 1：整理依赖和格式**

运行：

```bash
go mod tidy
gofmt -w .
git diff --check
```

预期：`go.mod`/`go.sum` 仅包含计划列出的直接依赖和必要间接依赖，不存在格式或空白错误。

- [ ] **步骤 2：运行解析器 fuzz 冒烟**

运行：

```bash
go test -run=^$ -fuzz=FuzzParseJSONL -fuzztime=5s ./internal/service
```

预期：5 秒内无 panic、hang 或内存失控；若发现输入，保留最小回归样本并增加普通单元测试后重新运行。

- [ ] **步骤 3：运行完整 Harness 验证**

运行：`./init.sh`

预期：格式检查、`go test ./...`、`go test -race ./...` 和 `go vet ./...` 全部通过。

- [ ] **步骤 4：确认真实模型验收前置条件且不泄露密钥**

读取仓库根目录 `模型配置.md`，确认 base URL 为 `https://aigateway.venusgroup.com.cn/ai/deepseek/openai`、模型名为 `deepseek-v4-pro`、Key 环境变量名为 `AI_GATEWAY_API_KEY`。只运行 `test -n "$AI_GATEWAY_API_KEY"` 检查环境变量存在，不得输出其值。验证 `Test_Input.jsonl` 恰好 50 行、每行 JSON 合法、`trace_id` 非空且唯一。

- [ ] **步骤 5：使用真实模型运行完整端到端流程**

创建不含密钥值的本地任务配置，输入指向仓库根目录 `Test_Input.jsonl`，输出指向 `Test_Output.jsonl`，状态库使用 `Test_State.db`，模型和 Key 环境变量名使用上一步固定值。结构化输出优先使用该网关实测支持的 `json_schema`；若网关明确返回不支持该模式，则保留测试证据并改用设计允许的 `json_object`。运行编译后的真实 CLI，不得使用 fake server 或替换 Completer。

预期：CLI 退出码为 `0`，真实网关完成全部 50 条模型调用，生成 `Test_Output.jsonl`，失败记录数为 0。

- [ ] **步骤 6：验证真实输出完整性**

使用本地校验命令确认：输入输出都为 50 条；输出 `trace_id` 非空、唯一且集合与输入完全一致；每条输出包含合法 `label`、`explanation`、`annotation.method=auto`，unsafe 记录包含闭集内的 `risk_type` 和合法 `risk_level`；完整输出逐条通过 `config/result-schema.json` 和业务语义校验。命令只能输出计数和通过/失败摘要，不得输出 prompt、response 或模型原始内容。

- [ ] **步骤 7：审查安全和范围**

运行：

```bash
git status --short
git diff --check
rg -n 'Bearer |api[_-]?key|authorization|prompt.*slog|response.*slog' --glob '*.go' --glob '*.yaml' --glob '*.md' .
```

预期：只出现文档中的占位环境变量名和协议字段，不出现真实凭证或把载荷写入日志的代码；没有 HTTP 服务、队列或其他超出首版范围的模块。

- [ ] **步骤 8：更新最终 Harness 证据**

将 `feat-006.status` 设为 `done`，在 `feature_list.json` 记录 `./init.sh`、fuzz 和真实模型端到端命令，在 `progress.md` 写明 50/50 真实输出验证证据，在 `session-handoff.md` 记录构建/运行命令、真实端到端结果、已知的供应商差异风险和正式批次首次建议并发 64。

- [ ] **步骤 9：提交最终验证状态**

```bash
git add go.mod go.sum feature_list.json progress.md session-handoff.md
git commit -m "chore: verify SendLLM batch workflow"
```

- [ ] **步骤 10：最终提交检查**

运行：

```bash
git status --short
git log --oneline -8
```

预期：工作区干净，每个任务有独立提交，最近提交包含完整验证证据。
