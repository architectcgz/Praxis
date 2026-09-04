# Model Provider Stream

> 本文定义 `internal/modelprovider` 对 SSE 和 Provider 专用事件的解析、tool call 聚合、terminal 语义和 `ModelStreamEvent` 归一化。adapter 生命周期见 [`provider.md`](provider.md)，请求映射见 [`request.md`](request.md)，总览见 [`../../model_provider.md`](../../model_provider.md)。

## 1. 解析层次

流解析分成三层，每层只处理自己的协议：

```text
HTTP response body
    -> SSE frame decoder
    -> protocol event decoder
    -> protocol state / tool accumulator
    -> core/runtime.ModelStreamEvent
```

- SSE frame decoder 只识别 `event`、`data`、空行和 frame 限制，不理解 Provider 业务字段；
- protocol event decoder 将 `data` JSON 解码为对应协议的私有 event DTO；
- protocol state 将多个 Provider event 组合成完整 content 和 tool call，再生成 Provider-neutral event；
- application/agent_runtime 只接收 `ModelStreamEvent`，不解析 SSE、SDK 类型或 Provider JSON。

协议 adapter 必须按流顺序处理事件。不能因为某个事件暂时无法生成完整 core event，就把原始 Provider event 泄漏给上层。

## 2. SSE frame 规则

SSE decoder 必须支持 Provider 常见的标准 framing：

1. 以空行结束一个 frame；
2. 支持 `LF` 和 `CRLF` 行结束；
3. 多个 `data:` 行按 SSE 规则拼接后再交给 JSON decoder；
4. `event:` 保存 event name，未提供时使用协议默认事件类型；
5. 注释行不产生业务事件，但不能突破 frame 大小限制；
6. 未知字段可以忽略，但 `data`、`event` 和 frame 边界必须严格处理；
7. 超过最大 frame bytes 时立即返回 resource 错误并停止读取；
8. EOF 只有在已经处理明确 terminal event 后才是正常关闭。

单行 `data:` 不能用无界 `bufio.Scanner` 默认限制替代明确的上限。decoder 要么设置可配置的最大 token，要么使用按字节计数的 reader，确保超大 frame 在内存增长前失败。

空 `data` frame、无法解码的 JSON、事件名与 payload 类型不匹配，均由 protocol adapter 按稳定 protocol 错误处理。Provider 的 keep-alive 注释和空白 frame 可以忽略，但不能将其当作 complete。

## 3. Provider-neutral event

协议 adapter 输出的事件只包含 core runtime 所需的语义。典型事件包括：

```text
ModelStreamEvent
├── TextDelta { ContentIndex, Text }
├── ThinkingDelta { ContentIndex, Text, Signature? }
├── ToolCall { ContentIndex, ToolCallID, Name, Input }
├── Complete { ResponseID, StopReason, Usage? }
└── Error { Code, Message }
```

具体类型以 `internal/core/runtime` 契约为准。adapter 必须遵守：

- text/thinking delta 只携带本次新增内容，不携带累计全文；
- `ContentIndex` 保留同一响应内 content 的归属和顺序；
- `ToolCall` 只在 input 已完整且 JSON 合法后发送；
- `ResponseID` 只作为本次模型响应关联信息；
- `Complete` 只能由协议明确的成功 terminal event 产生；
- Provider error、malformed event、truncated EOF 和 cancellation 不能伪装成 `Complete`。

空 text、空 thinking 和没有新增字段的 metadata event 不产生空 delta。除非 core 契约明确要求，usage 和 Provider 诊断字段不能取代内容事件。

## 4. 解析状态

每次 `Stream` 调用建立独立状态：

```text
idle
  -> response_started
  -> content_open
  -> tool_arguments_accumulating?
  -> terminal_success | terminal_error | cancelled | truncated
```

状态至少追踪：

- response ID；
- content index 与 content block 类型；
- tool call provider identity、core identity、name 和累计 argument bytes；
- 是否已收到可接受的 stop reason；
- 是否已发出 terminal event。

不允许跨 `Stream` 调用共享这些字段。状态转换违反协议顺序时立即失败，并在失败后停止读取远程 body。

一个响应若声明多个并行 tool call，accumulator 必须按稳定 call identity 隔离：

```text
accumulators[call-a] -> arguments for call-a
accumulators[call-b] -> arguments for call-b
```

不能把 content index、数组位置或到达顺序单独当作全局 tool identity。Provider 没有返回可关联 identity 时，adapter 只可使用同一响应内明确且可证明唯一的协议键；无法证明时返回 protocol 错误。

## 5. Anthropic Messages 事件

Anthropic Messages 常见的事件序列可归一化为：

| Provider 事件 | 归一化行为 |
|---|---|
| `message_start` | 建立 response ID 和初始响应状态。 |
| `content_block_start` | 建立 text、thinking 或 tool use content state。 |
| `content_block_delta` 的 text 类型 | 发送 text delta。 |
| `content_block_delta` 的 thinking 类型 | 发送 thinking delta，并保留协议要求的签名状态。 |
| `content_block_delta` 的 input JSON 类型 | 按 content block identity 累加 tool arguments。 |
| `content_block_stop` | 关闭对应 content block；tool use 仍需完成 JSON 校验。 |
| `message_delta` | 记录 stop reason、usage 和响应级完成信息。 |
| `message_stop` | 只有在响应状态完整且没有未关闭 accumulator 时产生 `Complete`。 |
| `error` | 产生 provider/protocol error，不产生 complete。 |

`input_json_delta` 只能累加为 bytes，不能对每个片段单独解码后再重新编码，因为这样会改变字符串转义和数字表示。收到对应 block 的终止事件后，再对完整 JSON 做语法校验并生成 `ToolCall`。

## 6. OpenAI Chat Completions 事件

Chat Completions 通常通过 `choices[].delta` 提供增量，通过 `finish_reason` 和 `[DONE]` 表示结束：

- text content 直接转换为 text delta；
- reasoning 或其他受支持的增量字段按 capability 映射为 thinking delta；
- `tool_calls[].id`、index、function name 和 arguments 片段进入对应 accumulator；
- function arguments 片段必须按 call identity 拼接，不能按整个 response 拼接；
- `finish_reason=tool_calls` 要求所有已声明的 tool call 都能完成结构校验；
- `[DONE]` 只在已收到合法响应终止语义、没有未完成 tool accumulator 且未发生错误时产生 complete；
- Provider error event 或非 JSON `data` 内容按协议规则处理，不作为普通 text。

请求默认只要求一个 choice。若响应出现未请求的多个 choice，adapter 必须有明确的选择契约；目标协议默认返回 protocol error，不能无提示丢弃其他 choice 或合并不同 choice 的内容。

## 7. OpenAI Responses 事件

Responses API 使用带类型的 response event。adapter 按 event type 和 item identity 维护状态，例如：

- response created/in progress 事件建立 response 状态；
- output text delta 产生 text delta；
- reasoning 相关 delta 产生 thinking delta，并按协议保存签名或摘要关联；
- function call arguments delta 按 item/call identity 累积；
- function call arguments done 触发完整 JSON 校验和 `ToolCall`；
- response completed 只有在所有必需 item 已关闭时产生 complete；
- failed、incomplete、error 和取消事件产生对应错误或中断结果。

Responses 的 item identity、response ID 和 function call ID 必须分别保存。不能把 response ID 当作 tool call ID，也不能把不同 item 的 arguments 合并到同一个 tool call。

## 8. Tool arguments 与资源限制

tool arguments 是不可信的远程输入，adapter 必须为每个 call 设置累计 byte 上限：

```text
argument fragment
    -> check call-local accumulated bytes
    -> append owned bytes
    -> wait for provider completion marker
    -> parse one complete JSON value
    -> emit ToolCall
```

超过上限时通过 `StreamError` 结束当前流，并取消后续读取。未收到 completion marker 就 EOF、terminal 或 response close 时，不能把部分 JSON 作为 tool call。JSON 语法合法不等于工具可执行；schema、capability grant、approval 和副作用校验由 AgentRuntime 在调用 `ToolRunner` 前完成。

text 和 thinking 的 execution 级输出限制由 `application/agent_runtime/stream.go` 增量执行。adapter 仍应设置 frame、单个事件和必要的 wire response 上限，防止恶意 Provider 输入在进入应用层前耗尽内存。

## 9. Terminal 与 EOF

每次调用在 channel 交付后必须收敛到且只能收敛到一个 terminal 结果：

| 情况 | 结果 |
|---|---|
| 明确成功 terminal event | 发送一个 `StreamComplete`，关闭 channel。 |
| Provider error/failed event | 发送一个脱敏的 `StreamError`，关闭 channel。 |
| context 取消 | 停止发送，关闭 body 和 channel；消费者通过同一个 context 识别 cancellation。 |
| response 在 terminal 前 EOF | 发送 truncated `StreamError`，禁止发送 complete。 |
| malformed JSON 或未知必需事件 | 发送 protocol `StreamError`，禁止发送 complete。 |
| frame 或 accumulator 超限 | 发送 resource `StreamError`，禁止继续读取。 |

terminal event 因 context 取消而无法发送时，仍必须关闭 body 和 channel。下游消费者提前停止接收不会改变 adapter 的 terminal 语义和资源回收要求。

## 10. 流测试

每个协议 adapter 的 SSE test 至少覆盖：

- `LF`、`CRLF`、多行 `data:` 和注释 frame；
- keep-alive、空 frame、空 data 和未知非必需字段；
- text/thinking 多片段顺序；
- 多个并行 tool call 的 identity 隔离；
- argument JSON 跨 frame 拼接、转义、超限和 malformed JSON；
- complete、error、提前 EOF、未知事件和错误顺序；
- 取消时停止读取、关闭 body 和关闭 channel；
- Provider 原始错误与敏感字段不进入稳定错误文本。

测试用内存 reader 或 `httptest.Server` 逐帧发送数据，并验证事件序列，而不是只检查最终拼接文本。

## 11. 不变量

1. SSE framing、Provider event DTO 和协议状态只由对应 adapter 处理。
2. `ModelStreamEvent` 只表达 Provider-neutral 语义，不携带原始 wire payload。
3. text/thinking delta 按顺序增量发送，tool call 在完整 JSON 校验后只发送一次。
4. 并行 tool call 的 arguments 按 call identity 隔离，不按到达顺序盲目合并。
5. 成功必须来自明确 terminal event；terminal 前 EOF 一律是 truncated。
6. terminal 之后不再发送任何事件，response body 和 output channel 最终关闭。
7. frame、event 和 tool arguments 都有明确边界，不能无界增长内存。
8. adapter 不执行工具、不校验 capability grant，也不写 transcript 或产品状态。
