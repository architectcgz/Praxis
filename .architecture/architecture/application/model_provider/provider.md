# Model Provider Adapter

> 本文定义 `internal/modelprovider` 协议 adapter 的运行边界、构造方式、生命周期和错误语义。协议请求字段见 [`request.md`](request.md)，流解析见 [`stream.md`](stream.md)，HTTP 与安全约束见 [`http.md`](http.md)，总览见 [`../../model_provider.md`](../../model_provider.md)。

## 1. 位置与边界

`application/model_provider` 是模型 Provider 适配器的架构细节目录；实际 Go 实现位于 `backend/internal/modelprovider`。它不是 `internal/application` 的 Go 子包，也不拥有 AgentRuntime 的业务状态。

适配器位于 core runtime 端口与远程模型 API 之间：

```text
application/agent_runtime
    -> core/runtime.ModelStream
    -> modelprovider/<protocol>
    -> HTTP/SSE Provider API
```

adapter 负责把一次 `ModelRequest` 转换为具体 API 请求，把远程流转换为 `ModelStreamEvent`，并管理本次请求的网络资源。以下能力不属于 adapter：

- 解析 profile、Provider 配置文件或模型选择；
- 创建或修改 `AgentExecution`、Agent、SessionContext 和 transcript；
- 控制 model/tool loop、工具授权、approval 或 settlement；
- 决定跨 execution 的 retry、recovery 和调度；
- 生成 Wails、SSE、WebSocket 或其他前端 DTO。

协议子包按 API format 划分，而不是按厂商名称划分。兼容同一协议的第三方 endpoint 复用对应 adapter；厂商差异只有在协议字段确实不同且不能由配置表达时才进入专用实现。

## 2. 构造与装配

适配器只由 `compose` 创建。组合根先从 `modelregistry` 取得已校验的模型配置，再通过独立的 credential source 取得创建 adapter 所需的明文 credential：

```text
ModelSelection
    -> modelregistry.ResolveModel
    -> ResolvedModelConfig
    -> credential source.Resolve(providerID)
    -> compose selects API format
    -> protocol.New(adapter config)
    -> core/runtime.ModelStream
```

`ResolvedModelConfig` 不包含明文 credential，也不包含具体 adapter 类型。adapter 的内存配置可以包含 credential，但必须满足以下约束：

- 只在 adapter 实例和底层 HTTP transport 的内存范围内存在；
- 不提供序列化、导出、调试打印或公开 getter；
- 不复制到 `ModelRequest`、`ModelStreamEvent`、transcript、日志或错误文本；
- adapter 关闭后不保留可再次读取的 credential 引用。

每个协议包的 `New` 在创建阶段校验 endpoint、proxy、认证方式、协议版本和本地限制。创建失败返回稳定配置错误，不创建半可用的 `ModelStream`。`New` 不发起远程请求，也不验证模型是否存在；远程能力验证属于显式的模型发现或首次调用流程。

`compose` 对 `ModelAPIFormat` 必须使用穷举选择：

| API format | 构造函数归属 |
|---|---|
| `anthropic_messages` | `anthropicmessages.New` |
| `openai_chat_completions` | `openaichat.New` |
| `openai_responses` | `openairesponses.New` |

未知 format 返回稳定错误。禁止把未知 format 静默降级为 OpenAI Chat Completions 或其他兼容协议。

## 3. 单次 Stream 生命周期

`Stream` 是一次模型调用的唯一入口。一次调用只对应一个 execution context、一个 HTTP response body 和一个输出 channel：

```text
Stream(ctx, request)
    -> validate request boundary
    -> encode protocol request
    -> create HTTP request with ctx
    -> send request
    -> validate status and content type
    -> transfer body ownership to stream goroutine
    -> decode frames and normalize events
    -> emit one terminal event
    -> close body and output channel
```

生命周期规则如下：

1. HTTP request 创建、编码或发送失败时直接返回错误，不返回不可用 channel。
2. 非 2xx 响应在 body ownership 转移前读取限长错误内容并返回脱敏错误；不启动事件 goroutine。
3. 只有通过状态码和响应头检查的 streaming response 才把 body 所有权转给 adapter goroutine，并向调用方返回输出 channel。
4. channel 返回后，Provider error、协议错误、资源超限和提前 EOF 通过一个 `StreamError` terminal event 表达，不能再使用 `Stream` 的第二返回值。
5. goroutine 独占 response body 和输出 channel，并在所有返回路径执行 `body.Close` 与 channel close。
6. text、thinking 和已完成的 tool call 按远程到达顺序发送；不等待完整响应才能发送增量内容。
7. `StreamComplete`、`StreamError` 或 cancellation 只能形成一个 terminal 结果；terminal 之后禁止发送其他事件。
8. `context.Context` 取消必须同时终止 HTTP request、frame 读取和 channel 等待；消费者通过同一个 context 识别 cancellation，adapter 最终关闭 channel。

输出 channel 的 buffer 只用于吸收短时调度差异。adapter 不把完整响应存入 channel，也不通过无限增大 buffer 规避消费者背压。

## 4. 并发与实例状态

一个 adapter 实例可以服务多个不共享可变状态的 `Stream` 调用，但每次调用的解析状态必须独立：

```text
Provider instance
├── immutable endpoint / auth / transport config
├── Stream(E1) -> call state 1
└── Stream(E2) -> call state 2
```

以下状态不得放在 Provider 实例的共享字段中：

- 当前 response body 或输出 channel；
- `ResponseID`、content index 和 tool call accumulator；
- 当前 execution 的 output byte 计数；
- terminal 标记、错误和 cancellation 状态。

如果底层 SDK client 不是并发安全的，adapter 必须在内部封装并发保护，不能要求 `AgentRuntime` 串行化不同 execution。单一 Agent 的 execution 串行由 AgentRuntime 负责，不是 Provider adapter 的通用契约。

## 5. 错误语义

adapter 的错误出口由 channel 是否已交付决定：

| 阶段 | 分类 | 触发条件 | 处理约束 |
|---|---|---|---|
| channel 交付前 | configuration | endpoint、认证或协议配置不合法 | 由 `New` 或 `Stream` 第二返回值返回，不创建 channel。 |
| channel 交付前 | transport/provider | DNS、连接、TLS、request write 或非 2xx 响应 | 由 `Stream` 第二返回值返回，不启动解析 goroutine。 |
| channel 交付后 | provider | Provider 返回明确的流错误事件 | 发送一个脱敏的 `StreamError`。 |
| channel 交付后 | protocol | SSE frame、JSON、事件顺序或 tool call 不合法 | 发送一个 `StreamError`，不伪造 complete。 |
| channel 交付后 | truncated | response 在 terminal 事件前结束 | 发送一个 `StreamError`，已有部分内容不能作为成功结果。 |
| channel 交付后 | resource | frame、tool argument 或 adapter 本地边界超限 | 发送一个 `StreamError`，立即终止读取并释放 body。 |
| 任意阶段 | cancellation | execution context 被取消 | 保留 `context.Canceled`/`DeadlineExceeded` 语义；channel 已交付时停止发送并最终关闭。 |

原始 Provider body 只能作为限长、脱敏的内部诊断信息保留。禁止在错误中包含 Authorization、API key、完整 prompt、thinking signature、完整 tool input 或未脱敏的 Provider response。

## 6. 关闭与资源回收

`Stream` 的每一条退出路径都必须满足：

```text
stop reading
    -> cancel or observe ctx
    -> close response body
    -> release parser / accumulator state
    -> close output channel exactly once
```

取消发生后，adapter 不等待消费者继续接收，也不依赖 channel 被 drain 才能回收 HTTP 资源。底层 transport 返回后，goroutine 必须最终退出；长时间未退出应由诊断指标暴露，而不是通过泄漏 goroutine 掩盖。

Provider adapter 不负责 retry。一次流可能已经产生计费、文本 delta 或工具调用，自动重试会造成重复模型响应。需要重试时，由上层显式创建新的 model call，并通过新的 `ModelCallNumber` 和 execution 事件区分两次调用。

## 7. 测试边界

每个协议包使用 `httptest.Server` 或内存 reader 验证：

- `New` 的配置与 endpoint 校验；
- request 编码和认证 header 是否正确设置但不泄漏；
- response body 是否在成功、错误、EOF 和取消路径关闭；
- 多个并发 `Stream` 的状态隔离；
- terminal 后不再发送事件且 channel 最终关闭；
- Provider 错误、协议错误、超限和取消的稳定分类。

测试不得访问真实 Provider，也不得把 credential 写入 golden file、测试日志或失败断言文本。

## 8. 不变量

1. adapter 只实现 core runtime 的 Provider-neutral 模型端口。
2. 协议 wire DTO、SDK 类型和解析状态只存在于对应协议包。
3. adapter 实例不拥有 execution、transcript、工具授权或产品状态。
4. 每个 `Stream` 调用独立拥有 response body、解析状态和 terminal 状态。
5. 明确的 Provider terminal event 是成功的必要条件；EOF 本身不表示成功。
6. 取消会终止网络请求并关闭 channel，不依赖前端或消费者继续工作。
7. `compose` 是 adapter 选择和 credential 注入的唯一组合边界。
