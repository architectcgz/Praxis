# Model Provider

> 本文定义 `backend/internal/modelprovider` 的目标目录、模型协议适配器职责、流解析边界和依赖规则。
> Provider 配置聚合、Group、credential 与模型目录投影见 [`model_registry/README.md`](../model_registry/README.md)；Provider-neutral 请求与事件契约由 `internal/runtime` 定义；单次模型流的应用层处理见 [`application/agent_runtime/stream.md`](../application/agent_runtime/stream.md)。
> 实现细节分别见 [adapter 生命周期与装配](provider.md)、[request 映射](request.md)、[流解析与事件归一化](stream.md) 和 [HTTP 与安全](http.md)。

## 1. 角色

`internal/modelprovider` 是外层模型协议适配器。它把 core-owned `ModelRequest` 编码为具体远程 API 请求，并把 HTTP/SSE 响应转换为 core-owned `ModelStreamEvent`。

该目录按 API format 划分，而不是按用户配置的 Provider 名称划分。一个第三方服务只要实现 OpenAI Chat Completions 协议，就使用 `openaichat` adapter；它不需要由 OpenAI 官方运营。

```text
runtime.ModelRequest
    -> modelprovider protocol adapter
    -> remote HTTP/SSE API
    -> provider-specific wire events
    -> modelprovider protocol adapter
    -> runtime.ModelStreamEvent
```

`modelprovider` 负责：

- 构造具体协议的 URL、header 和 request body；
- 将 Provider-neutral message、tool definition 和 reasoning 配置映射为 wire payload；
- 发起带 execution context 的 HTTP streaming request；
- 解析 SSE frame 和 Provider 专用 JSON event；
- 拼接 Provider 分片返回的 tool call arguments；
- 将远程事件转换为 Provider-neutral model stream events；
- 关闭 response body，并在 cancellation 后释放网络资源；
- 对外部错误做脱敏和稳定分类。

`modelprovider` 不负责：

- 读取模型配置文件、profile 或 credential 文件；
- 根据 `ProviderID`、`ModelID` 或 `APIFormat` 选择 adapter；
- 构造 execution 的 `ModelRequest`；
- 控制 model/tool loop、工具授权或 execution settlement；
- 写 transcript、repository 或产品状态；
- 生成 Wails、SSE、WebSocket 等前端传输事件。

## 2. 目录结构

```text
backend/internal/modelprovider/
├── doc.go                         包职责和依赖约束
├── http.go                        HTTP client、BaseURL、proxy 和错误响应处理
├── anthropicmessages/             Anthropic Messages API
│   ├── provider.go                Config、Provider、New 和 ModelStream.Stream
│   ├── request.go                 ModelRequest 到 Anthropic request 的映射
│   ├── sse.go                     Anthropic SSE event 解析与规范化
│   ├── request_test.go            请求映射 contract test
│   └── sse_test.go                流分片、tool call 和终止语义测试
├── openaichat/                    OpenAI-compatible Chat Completions API
│   ├── provider.go                Config、Provider、New 和 ModelStream.Stream
│   ├── request.go                 Chat Completions 请求映射
│   ├── sse.go                     choices/delta SSE 解析与规范化
│   ├── request_test.go
│   └── sse_test.go
└── openairesponses/               OpenAI-compatible Responses API
    ├── provider.go                Config、Provider、New 和 ModelStream.Stream
    ├── request.go                 Responses API 请求映射
    ├── sse.go                     typed response event 解析与规范化
    ├── request_test.go
    └── sse_test.go
```

Go package 名与目录名一致：

```text
modelprovider
anthropicmessages
openaichat
openairesponses
```

协议子包之间不得互相 import。共享的 HTTP 安全规则放在根 `modelprovider` 包；只有出现协议无关且稳定的重复逻辑时才允许放入根包。

本架构文档的细节目录为：

```text
.architecture/architecture/model_provider/
├── README.md                      模块边界、源码目录和共同不变量
├── provider.md                    adapter 构造、装配、生命周期和错误语义
├── request.md                     Provider-neutral 请求到 wire DTO 的映射
├── stream.md                      SSE、协议事件、tool call 聚合和 terminal 语义
└── http.md                        URL、header、transport、资源限制和敏感信息边界
```

配置聚合和运行时解析归 [`model_registry`](../model_registry/README.md)，不放在协议 adapter 目录。生产实现统一位于 `backend/internal/modelprovider`，应用层只能通过 `internal/runtime.ModelStream` 使用它。

## 3. 文件职责

### 3.1 `provider.go`

每个协议包的 `provider.go` 定义：

- adapter 的 `Config`；
- 实现 `runtime.ModelStream` 的 `Provider`；
- 构造函数 `New`；
- `Stream(context.Context, ModelRequest)` 生命周期入口；
- request 创建、HTTP 调用、状态码检查和 response body 所有权。

`provider.go` 只协调同包内的 request encoder 和 SSE decoder，不包含大段 wire DTO 或事件 switch。

### 3.2 `request.go`

`request.go` 负责将 Provider-neutral 输入映射为协议请求：

- system prompt 与 messages；
- text、thinking、tool use 和 tool result content；
- tool definition 与 JSON schema；
- model、max output tokens 和 reasoning 参数；
- Provider 要求的 role、content block 和 tool call 关联字段。

请求 DTO 为包内私有类型。禁止使用 `map[string]any` 表达稳定的协议结构；只有协议明确允许开放字段时才使用受控的 `json.RawMessage`。

### 3.3 `sse.go`

`sse.go` 只负责远程流协议：

- 拆分 SSE `event:` 和 `data:` frame；
- 解码 Provider 专用 event DTO；
- 按 content index 或 call ID 拼接 tool arguments；
- 保留 Provider 给出的 tool call ID 和 stop reason；
- 将 text delta、thinking delta、完整 tool call、complete 和 error 映射为 core event；
- 将未收到终止事件的 EOF 识别为 truncated stream。

`sse.go` 不调用 transcript、runtime observation sink 或前端 event publisher。Provider text delta 转换为 `ModelStreamEvent` 后立即写入 adapter 返回的 channel，由应用层 `stream.go` 继续处理。

## 4. API Format 映射

`modelregistry.ModelAPIFormat` 与 adapter 包一一对应：

| API format | Adapter package |
|---|---|
| `anthropic_messages` | `modelprovider/anthropicmessages` |
| `openai_chat_completions` | `modelprovider/openaichat` |
| `openai_responses` | `modelprovider/openairesponses` |

具体 adapter 的选择和实例化发生在 `compose`。`modelregistry` 提供已校验的模型、endpoint、能力和 credential；`compose` 根据 `APIFormat` 创建对应 adapter，并把它作为 `runtime.ModelStream` 注入 AgentRuntime。

## 5. Stream 约束

`ModelStream.Stream` 返回 Provider-neutral 事件 channel：

```go
type ModelStream interface {
    Stream(context.Context, ModelRequest) (<-chan ModelStreamEvent, error)
}
```

adapter 必须遵守：

1. HTTP 请求建立失败时直接返回错误，不创建不可用的 channel。
2. HTTP 请求成功后由 adapter goroutine 独占 response body 和输出 channel。
3. text delta 到达后立即转换并发送，不等待完整响应。
4. tool argument fragment 在 adapter 内按 call identity 拼接，完整后只发送一次 tool call。
5. 远程 complete 映射为一个 `StreamComplete`；远程 error 映射为一个 `StreamError`。
6. 未出现协议终止事件的 EOF 映射为错误，不能伪造成功 complete。
7. terminal event 后关闭 channel，不再发送其他事件。
8. context cancellation 必须终止 HTTP request 并最终关闭 channel。

channel buffer 只吸收短时调度差异，不承担完整响应缓存，也不能用扩大 buffer 掩盖消费者背压问题。

## 6. HTTP 与安全

根目录 `http.go` 统一提供：

- BaseURL 校验与尾部斜杠规范化；
- HTTP/HTTPS proxy 校验和 transport clone；
- 默认 HTTP client 选择；
- 非 2xx response 的限长、脱敏错误映射；
- content type 和 response body 大小边界的共享检查。

credential 只通过 adapter `Config` 进入内存，用于设置授权 header。它不得出现在：

- `ModelRequest`、`ModelStreamEvent` 或 tool call；
- SQLite、transcript 和审计事件；
- binding DTO 和前端事件；
- URL query、错误文本和诊断日志。

Provider 原始错误 body 可能包含 prompt、credential 或供应商内部信息。adapter 不直接向内层透传原始 body，只返回脱敏后的稳定错误及允许记录的诊断字段。

## 7. 依赖与装配

```text
modelprovider/*
    -> standard library / approved Provider SDK
    -> internal/runtime
    -> internal/domain/security

compose
    -> modelregistry
    -> concrete modelprovider protocol packages
```

`modelprovider` 不 import `app`、`contracts`、`application`、`orchestration`、`storage`、`modelregistry`、`tools` 或 `compose`。`modelregistry` 也不 import 具体协议包；两者只在 `compose` 中组合。

## 8. 验证要求

每个协议 adapter 至少覆盖：

- request role、content 和 tool schema 映射；
- 多个 text delta 的顺序；
- tool arguments 跨 frame 拼接；
- 多个并行 tool call 的 identity 隔离；
- complete、error、EOF 和 malformed JSON；
- context cancellation 与 response body 关闭；
- 非 2xx 响应和敏感信息脱敏；
- 超大 SSE frame、超大错误 body 和无界 tool arguments 防护。

测试使用 `httptest.Server` 或内存 reader，不访问真实 Provider。

## 9. 不变量

1. Provider wire 类型和 SDK 类型不离开对应协议包。
2. 每个 API format 由一个明确的协议子包实现。
3. adapter 只做协议映射和网络资源管理，不拥有 execution 流程。
4. text delta 被增量发送，不在 adapter 内聚合为完整前端响应。
5. tool arguments 必须按 call identity 完整拼接后再产生 Provider-neutral tool call。
6. EOF 不等于成功；成功必须来自明确的 Provider terminal event。
7. credential 不进入 core 类型、持久化、日志或前端。
8. adapter 选择只发生在 `compose`，协议包和 registry 互不依赖。
