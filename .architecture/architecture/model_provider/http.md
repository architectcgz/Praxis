# Model Provider HTTP 与安全

> 本文定义 `internal/modelprovider` 的 URL、header、transport、响应校验、资源边界和敏感信息处理。adapter 生命周期见 [`provider.md`](provider.md)，请求映射见 [`request.md`](request.md)，流解析见 [`stream.md`](stream.md)，总览见 [`README.md`](README.md)。

## 1. HTTP client 边界

HTTP 能力集中在 `internal/modelprovider` 根包的共享实现中，协议子包只提供协议 endpoint、header 和 body encoder：

```text
protocol adapter
    -> shared HTTP client / transport
    -> remote Provider endpoint
```

共享实现负责 BaseURL、proxy、默认 client、状态码、content type、响应限制和错误脱敏。协议子包不得各自复制一套认证日志、proxy 处理或错误 body 读取逻辑。

默认 HTTP client 必须可被测试替换。adapter 不使用全局可变 client、全局 credential 或全局 response parser。任何 transport 自定义都基于已有 transport 的 clone，不能修改其他 adapter 或进程内其他请求的配置。

## 2. BaseURL 与 endpoint

`BaseURL` 和可选 `ProxyURL` 在 adapter 构造时统一校验与规范化：

- scheme 只接受配置允许的 `http` 或 `https`；
- 拒绝包含 userinfo、fragment 或不符合 URL 语法的地址；
- query 不用于承载 credential 或动态模型参数；
- 规范化尾部斜杠，协议 route 通过结构化 URL 操作追加；
- 保留合法的 base path，不能通过字符串拼接意外丢失或重复路径；
- endpoint route 由 API format 决定，不能根据 Provider 展示名猜测；
- 每次请求重新创建 URL 对象，不能原地修改共享 URL；
- URL、proxy 和 header 的诊断输出必须经过脱敏。

典型 route 归属如下：

| API format | 默认 route |
|---|---|
| `anthropic_messages` | `/v1/messages` |
| `openai_chat_completions` | `/v1/chat/completions` |
| `openai_responses` | `/v1/responses` |

配置的 base path 与协议 route 组合规则必须稳定并覆盖 contract test。用户配置的 endpoint 可以指向兼容服务，但 adapter 仍按 API format 使用对应 wire 协议。

生产配置是否强制 HTTPS 由产品安全策略决定；如果当前策略要求 HTTPS，校验必须在 BaseURL 进入 adapter 前失败。不得通过日志、错误或 query 泄漏 credential 来弥补非 TLS endpoint 的风险。

## 3. Request headers

header 由协议 adapter 通过共享请求构造能力设置。通用约束如下：

- `Content-Type` 与序列化 body 一致；
- streaming request 设置协议要求的 `Accept`，通常为 `text/event-stream`；
- OpenAI-compatible API 使用其协议规定的 Bearer 认证 header；
- Anthropic Messages 使用其协议规定的 API key 和版本 header；
- credential 只设置在内存中的 request header，不进入 URL、body、日志或错误；
- 用户自定义 header 若被支持，必须有明确 allowlist，不能允许覆盖 Host、认证、代理或 tracing 安全 header；
- 不把 execution ID、registry revision、内部路径或 security snapshot 自动写入远程 header。

请求发送前的诊断只能记录协议、host、route 名、model ID、request size 和 correlation ID。Authorization、API key、完整 header map 和完整 body 永远不得记录。

## 4. Response 校验

response body 的所有权按以下顺序转移：

```text
client.Do
    -> check status code
    -> check content type for streaming response
    -> check response headers and limits
    -> transfer body ownership to decoder goroutine
```

非 2xx response 不进入正常 SSE decoder。adapter 读取错误 body 时使用明确上限，并把超出的部分截断后标记为 truncated；原始内容只作为脱敏内部诊断，不作为稳定错误文本的唯一来源。

2xx streaming response 必须具有协议允许的 content type。若 content type 缺失、与预期不符或声明了不受支持的响应格式，返回 protocol/provider 错误。对于兼容服务存在合法 content type 变体时，使用显式 allowlist，不做任意字符串包含判断。

响应 header、error body、单个 SSE frame、单个 JSON event 和 tool argument accumulator 都必须有边界。不同边界分别统计，不能用 error body 限制替代 tool argument 限制。

## 5. Timeout 与 cancellation

HTTP request 使用传入的 execution context。timeout 层次由上层 execution policy 决定，adapter 不覆盖调用方 deadline：

```text
execution deadline / cancellation
    -> http.NewRequestWithContext
    -> transport dial / TLS / write / read
    -> SSE decode
    -> output channel close
```

transport 可以设置连接建立、TLS handshake、response header 和 idle read 等网络级上限，但这些超时不能破坏 execution context 的取消语义。context 取消后：

1. HTTP request 立即收到取消信号；
2. reader 退出或返回 context error；
3. adapter 停止解析和发送新事件；
4. response body 被关闭；
5. output channel 关闭且 goroutine 最终退出。

前端连接和 observation sink 不属于 HTTP request 的 cancellation 链。前端断开不应自动取消模型调用，除非上层显式发出 execution control command。

## 6. Proxy 与 transport

proxy 配置是 Provider 请求的网络路由设置，不是模型配置的业务字段。共享 HTTP 实现必须：

- 只接受允许 scheme 的 proxy URL；
- 对 proxy URL 中的 userinfo、认证信息和错误文本脱敏；
- clone 基础 transport 后应用 proxy，避免修改全局 DefaultTransport；
- 明确是否支持 HTTP、HTTPS 和环境变量 proxy，并保持测试与生产一致；
- 保证不同 Provider 的 proxy 配置相互隔离；
- 在 adapter 关闭或替换时释放连接池资源。

proxy 不得通过 query、transcript、execution receipt 或公开配置投影暴露 credential。网络诊断记录 proxy 是否启用即可，不记录完整 proxy URL。

## 7. Retry 与响应错误

Provider adapter 默认不自动 retry。以下情况均直接结束本次调用并返回稳定分类：

- 认证失败、权限失败和模型不存在；
- rate limit、server error 和响应格式错误；
- 连接建立或 streaming 中途断开；
- content type、SSE frame 或 JSON 超限。

上层如需 retry，必须显式创建新的模型调用并承担重复请求、计费、部分输出和 tool call 的语义。adapter 不在 response body 已经产生内容后透明重试，也不重复发送相同的 `ModelCallNumber`。

错误分类可以保留 HTTP status、Provider error code 和 correlation ID，但不依赖 Provider 文案稳定识别业务状态。原始 response body 不直接返回 application、binding 或前端。

## 8. 敏感信息处理

credential 的流向限定为：

```text
modelregistry runtime resolver
    -> compose
    -> adapter in-memory config
    -> request Authorization header
```

credential 不得进入：

- `ModelRequest`、`ModelStreamEvent` 和 tool call；
- 公开配置 DTO、execution snapshot、SQLite、JSONL 和非敏感 backup；
- URL query、request body、Wails binding、projection 和前端事件；
- error string、structured log、metric label、trace attribute 和 test snapshot。

Provider 返回的 body 可能回显请求内容或内部 header。所有错误处理必须按潜在敏感数据处理，不在公共错误中包含原始 body；日志只保留脱敏后的 status、协议错误分类、大小和 correlation ID。

## 9. 可观测性

adapter 可以发布不含敏感信息的诊断指标和日志：

- protocol、model ID、Provider ID；
- request start、header received、terminal、cancel 和 failure；
- HTTP status、耗时、响应 bytes、SSE frame 数量；
- dropped/failed parse、tool argument limit 和 truncated stream 计数；
- correlation ID、execution ID 和 model call number，前提是这些值不作为 credential 或完整请求内容输出。

禁止将 prompt、assistant 全文、thinking signature、tool input、Authorization header 或原始 Provider error body 作为日志字段或 metric label。敏感值也不得通过 panic、debug dump 或测试失败快照间接输出。

## 10. HTTP 测试

共享 HTTP 与每个协议 adapter 至少覆盖：

- base URL 尾部斜杠、base path、非法 scheme、userinfo、query 和 fragment；
- endpoint route 和 content type allowlist；
- 各协议认证 header 的存在性与脱敏；
- 2xx streaming、非 2xx、缺失/错误 content type；
- 超大 error body、header、SSE frame 和连接中途 EOF；
- proxy transport clone 与不同实例之间的配置隔离；
- context cancellation 后 body close 和 goroutine 退出；
- 原始 response body、credential 和完整 request body 不进入错误与日志。

测试只使用 `httptest.Server`、自定义 RoundTripper 或内存 reader；不能访问真实 Provider。

## 11. 不变量

1. URL、header、proxy、status、content type 和错误脱敏由共享 HTTP 边界统一管理。
2. credential 只存在于 adapter 的短生命周期内存和认证 header 中。
3. 非 2xx response 不进入正常 Provider stream parser。
4. response body 只能由一个 adapter goroutine 拥有，并在所有路径关闭。
5. execution context 取消会终止 HTTP 请求、解析和输出 channel 生命周期。
6. 各类 response、frame、JSON event 和 tool argument 都有明确大小边界。
7. adapter 不自动 retry，不重复 model call，也不改变上层 execution outcome。
8. 日志、指标、trace 和错误不包含 credential 或未脱敏 Provider 内容。
