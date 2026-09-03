# Model Stream

> 本文定义 `application/agent_runtime/stream.go` 对单次模型调用流的消费、实时转发、结果收敛和终止语义。
> 多轮 model/tool 流程见 [`loop.md`](loop.md)，模型请求构造见 [`model_request.md`](model_request.md)，AgentRuntime 总体职责见 [`../../application.md`](../../application.md)。

## 1. 角色

`stream.go` 是一次模型调用的事件泵。它消费 `ModelStream` 返回的 Provider-neutral 事件，并同时产生两类彼此独立的输出：

1. 将已校验的增量事件立即发布到运行时流端口，供外层 transport adapter 流式推送给前端；
2. 在内存中收敛本次模型调用的完整结果，供 `loop.go` 在流正常结束后写入 transcript 并决定下一步动作。

```text
ModelRequest
    -> ModelStream.Stream(executionContext, request)
    -> stream.go event pump
         ├── ExecutionStreamSink.TryPublish(delta)
         │       -> transport adapter
         │       -> frontend stream
         └── model-call accumulator
                 -> ModelCallResult
                 -> loop.go
```

前端流式输出和完整模型结果不是同一个返回边界。前端不等待 `ModelCallResult`；每个可展示 delta 在被消费后立即进入 `ExecutionStreamSink`。`ModelCallResult` 只用于 AgentRuntime 内部完成 transcript 写入和 model/tool loop 推进。

`stream.go` 负责：

- 使用当前 execution context 持续消费模型流；
- 校验并按原始顺序处理 Provider-neutral 事件；
- 为前端可观察内容生成有序的 execution stream event；
- 将 stream event 立即交给非阻塞的 `ExecutionStreamSink`；
- 同时收敛完整 text、thinking 和 tool use 内容；
- 增量检查输出资源限制；
- 识别正常完成、Provider 错误、取消和协议异常；
- 返回完整 `ModelCallResult` 或稳定分类的错误。

`stream.go` 不负责：

- 构造 `ModelRequest`；
- 控制多轮 model/tool loop；
- 授权或执行工具调用；
- 写入 transcript 或推进 durable sequence；
- 推进 `AgentExecution`、Agent 或 SessionContext 状态；
- retry、settlement、recovery 或跨 Agent 调度；
- 依赖 Wails、HTTP、SSE、WebSocket 或任何前端 DTO；
- 对前端连接执行订阅管理、重连或历史补发。

## 2. 两个输出边界

### 2.1 实时 Execution Stream

`ExecutionStreamSink` 是 AgentRuntime 使用的 core-owned 观察端口。它接收与前端协议无关的 `ExecutionStreamEvent`：

```text
ExecutionStreamEvent
├── AgentID
├── ExecutionID
├── ModelCallNumber
├── Sequence
├── Kind
├── ContentIndex?
├── TextDelta?
├── ToolCallID?
└── ToolName?
```

| 字段 | 用途 |
|---|---|
| `AgentID` | 将事件路由到所属 Agent 视图。 |
| `ExecutionID` | 隔离不同 execution，禁止把旧执行的 delta 拼接到新执行。 |
| `ModelCallNumber` | 区分同一 execution 内由 tool call 分隔的多次模型调用。 |
| `Sequence` | 当前 execution 内严格单调递增的观察序号，用于保持顺序和检测丢失。 |
| `Kind` | 表示 text delta、thinking delta、tool call ready、model call completed 或 model call aborted。 |
| `ContentIndex` | 保留一次模型响应内 content 的顺序和归属。 |
| `TextDelta` | 本次新增文本，不包含此前已经发送的完整累计文本。 |
| `ToolCallID`、`ToolName` | 表示已完成结构解析的工具调用；tool input 不默认暴露给前端。 |

`ExecutionStreamSink` 使用非阻塞语义，例如：

```go
type ExecutionStreamSink interface {
    TryPublish(ExecutionStreamEvent) bool
}
```

`TryPublish` 表示把事件交给外层的有界队列，而不是同步等待前端处理。返回 `false` 表示该观察事件未被接受；这可以记录指标，但不能导致模型调用失败或改变 execution outcome。

stream event 是 transient observation，不是 durable event。它允许因进程退出、前端断开或队列满而丢失。前端通过 `Sequence` 检测缺口，并通过 transcript/projection 查询恢复权威内容。

### 2.2 内部 Model Call Result

`stream.go` 在发布增量事件的同时收敛本次调用的完整结果：

```text
ModelCallResult
├── ResponseID
├── Content[]
│   ├── text
│   ├── thinking
│   └── tool_use
├── StopReason
└── OutputBytes
```

`Content` 保留模型响应中的原始顺序。一次响应可以同时包含 text、thinking 和多个 tool use；`loop.go` 根据该顺序构造 assistant messages，并使用 `ResponseID` 保留同一模型响应内各内容之间的关联。

`ModelCallResult` 不是给前端的聚合响应。它是 AgentRuntime 内部值，仅在流正常完成后用于：

- 构造完整 assistant messages；
- durable 追加 transcript；
- 更新 `AgentLoop.messages`；
- 判断执行工具还是结束本次 execution；
- 构造下一次 `ModelRequest`。

## 3. 事件处理顺序

Provider adapter 必须先将具体 SDK、HTTP、SSE 或 WebSocket 数据转换为 core-owned 的 model stream events。`stream.go` 不解析 Provider wire payload。

每个事件按以下固定顺序处理：

```text
receive provider-neutral event
    -> validate event structure and ordering
    -> check remaining output budget
    -> append to model-call accumulator
    -> allocate execution stream sequence
    -> TryPublish observable delta immediately
    -> wait for next event
```

| Provider-neutral 事件 | 收敛行为 | 实时流行为 |
|---|---|---|
| text delta | 追加到当前 text content。 | 立即发布 text delta。 |
| thinking delta | 追加到当前 thinking content 并保留签名。 | 按可见性策略发布 thinking delta；签名不发布。 |
| tool call | 追加完整 tool use content。 | 发布 tool call ready 元数据，不默认发布 tool input。 |
| complete | 固定 `StopReason` 并完成结果。 | 发布 model call completed；它不表示 execution settled。 |
| error | 终止本次调用。 | 发布 model call aborted，然后返回 Provider 错误。 |

Provider 协议可能把 tool call 参数拆成多个片段。片段拼接、协议字段映射和 JSON 解码属于 Provider adapter；`stream.go` 接收的 tool call 必须已经是完整的 Provider-neutral 结构。

空 delta 不生成 stream event，也不增加输出计数。未知事件、缺失 `ToolCallID`、缺失工具名、无效 tool input 或同一响应内重复的 `ToolCallID` 均属于 model-stream protocol error。

`stream.go` 只校验响应结构。工具是否在 capability grant 中、是否需要 approval、参数是否满足工具 schema，以及是否允许产生副作用，由 `loop.go` 在执行工具前完成。

## 4. 前端流式传输

`ExecutionStreamSink` 的外层实现负责把中立事件映射为具体前端传输协议：

```text
ExecutionStreamEvent
    -> application event publisher
    -> Wails / SSE / WebSocket adapter
    -> frontend execution stream store
    -> incremental rendering
```

transport adapter 必须遵守以下约束：

- 收到 delta 后及时发送，不等待本次模型调用或 execution 完成；
- 保持同一 `ExecutionID` 下的 `Sequence` 顺序；
- 不把不同 `ExecutionID` 或 `ModelCallNumber` 的内容拼接在一起；
- 前端断开或发送失败不能取消模型调用；
- 慢前端不能反向阻塞 Provider stream；
- 可以在传输队列内合并相邻 text delta，但不能等待完整响应后再一次性返回；
- 不把 transient stream event 当作 execution 状态事实。

Wails event name、SSE event type、WebSocket frame、JSON DTO 和渲染节流均属于 transport adapter，不进入 `stream.go`。

前端按 `(ExecutionID, ModelCallNumber, ContentIndex)` 增量渲染内容。发现 `Sequence` 缺口、页面重载或重新连接时，前端必须查询 durable transcript/projection，而不是要求 AgentRuntime 重放内存事件。

## 5. Transcript 与最终一致性

实时 delta 可以先于 transcript 中的完整 assistant message 到达前端。只有模型流明确 complete 且 `ModelCallResult` 校验通过后，`loop.go` 才执行 durable append：

```text
stream deltas -> frontend incremental preview

stream complete
    -> ModelCallResult
    -> build assistant messages
    -> append transcript entries
    -> fsync
    -> advance AgentLoop.messages
    -> execute tools or finish execution
```

流式预览不是 transcript。Provider 错误、取消、资源超限或进程退出时，前端可能已经显示尚未 durable 的部分文本。此时 transport adapter 发布 model call aborted，前端将该内容标记为未完成；execution 最终 settlement 后，前端重新查询 transcript/projection，并以 durable 内容覆盖预览。

execution settled 不是模型流事件。一次 execution 可能包含多次模型调用和工具执行，因此 model call completed 不能触发 execution 完成。settled 通知只能由拥有 settlement transaction 的应用服务在 durable commit 后发布。

## 6. 完成与中断

正常模型流必须以明确的 complete 事件结束。底层 channel 在 complete 之前关闭，视为响应被截断，不能把已经收敛的部分内容当作完整 assistant message。

| 终止条件 | 内部结果 | 实时流行为 |
|---|---|---|
| 收到 complete | 返回完整 `ModelCallResult`。 | 发布 model call completed。 |
| 收到 error | 返回稳定分类的 Provider 错误。 | 发布 model call aborted。 |
| execution context 取消 | 返回 cancellation。 | 尝试发布 model call aborted。 |
| 输出额度耗尽 | 返回 resource-limit 错误。 | 尝试发布 model call aborted。 |
| channel 提前关闭 | 返回 truncated-stream 错误。 | 发布 model call aborted。 |
| 未知或非法事件 | 返回 model-stream protocol error。 | 发布 model call aborted。 |

错误发生前已发送的 delta 不会被伪装成 durable assistant message。部分内容可以留在前端作为明确标记的未完成预览，但不得进入公开错误、普通日志或产品状态事件。

`stream.go` 不自行 retry。模型流可能已经产生计费和用户可见 delta，盲目重试会造成重复内容。重试策略必须显式创建新的 `ModelCallNumber`，并使前端能够区分原调用和重试调用。

## 7. Cancellation 与背压

`ModelStream.Stream` 和事件泵使用同一个 execution context：

```text
execution context
    -> ModelStream.Stream
    -> stream.go receive loop
    -> Provider adapter request
```

context 取消后，`stream.go` 不再等待新 Provider 事件。Provider adapter 必须响应 context、关闭网络请求并最终释放事件 channel；adapter 不能依赖消费者无限 drain 才能退出。

前端传输不参与该 cancellation 链。`ExecutionStreamSink` 必须通过有界、非阻塞队列隔离慢消费者，不能让 UI 渲染速度决定 Provider 读取速度。队列容量、相邻 delta 合并和丢弃策略由 observation/transport adapter 配置。

事件被丢弃时必须保留后续事件的原始 `Sequence`，不能重新编号掩盖缺口。adapter 应记录 dropped-event 指标，前端则通过序号缺口触发 durable 查询。

## 8. 输出限制

输出限制在事件消费过程中增量检查，不能等完整响应进入内存后再判断。计数以 UTF-8 字节为准，并包含所有模型生成的可持久化内容；Provider 上报的 token usage 只用于统计，不能代替本地限制。

`ExecutionRun` 拥有 execution 级累计计数。调用 `stream.go` 时传入当前剩余额度；成功返回后，`loop.go` 使用 `ModelCallResult.OutputBytes` 推进累计值。`stream.go` 不持有跨模型调用的资源计数。

达到限制后必须取消当前模型调用、发布 model call aborted 并返回 resource-limit 错误，不继续接收无限输出，也不把部分结果带入下一次模型请求。

## 9. 错误分类

`stream.go` 返回的错误至少区分：

| 分类 | 示例 |
|---|---|
| cancellation | pause、close、timeout 或 shutdown 导致 context 取消。 |
| provider | Provider 主动返回错误或连接失败。 |
| protocol | 未知事件、非法 tool call、缺少 complete 或提前关闭。 |
| resource limit | 本次调用使 execution 的模型输出超过限制。 |

`ExecutionStreamSink` 拒绝 transient event 不属于模型调用错误。Provider 原始错误可以作为内部 cause 保留，但稳定错误码不能依赖 Provider 文案。错误和观察事件不得泄漏 credential、请求 header、完整 prompt、thinking signature 或未经可见性过滤的 tool input。

## 10. 不变量

1. 一次事件泵恰好对应一次 `ModelStream.Stream` 调用。
2. 每个已校验、可观察的 delta 都立即尝试发布，前端不等待完整 `ModelCallResult`。
3. `ModelCallResult` 只供 AgentRuntime 内部持久化和 loop 推进，不是前端聚合响应。
4. `stream.go` 只消费 Provider-neutral 事件，不解析具体 Provider 协议。
5. 同一 execution 的 stream event sequence 严格单调递增，丢失时不重新编号。
6. 成功结果只在收到明确 complete 后产生；channel 提前关闭不是成功。
7. tool call 的结构校验属于 stream 处理，授权和执行属于 `loop.go`。
8. 输出限制在消费过程中增量执行，不能先无限缓存再检查。
9. 前端传输使用非阻塞端口，不能反向阻塞或改变模型调用结果。
10. `stream.go` 不写 transcript、不推进产品状态，也不决定下一轮模型调用。
11. model call completed 不等于 execution settled；settled 只能来自 durable settlement。
12. transient stream event 不能代替 durable transcript、receipt 或 execution projection。
