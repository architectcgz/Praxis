# Model Provider Request

> 本文定义 `internal/modelprovider` 将 core-owned `ModelRequest` 映射为具体 Provider wire request 的规则。adapter 生命周期见 [`provider.md`](provider.md)，流事件见 [`stream.md`](stream.md)，总览见 [`../../model_provider.md`](../../model_provider.md)。

## 1. 请求边界

`ModelRequest` 是应用层已经冻结的 Provider-neutral 输入。request encoder 只做协议映射，不重新读取 execution、Agent、SessionContext、模型 registry 或 transcript：

```text
core/runtime.ModelRequest
    -> protocol request encoder
    -> private wire DTO
    -> json.Marshal
    -> HTTP request body
```

请求 encoder 不改变输入对象，也不向调用方返回 Provider 专用类型。协议 DTO 仅在 `anthropicmessages`、`openaichat` 或 `openairesponses` 包内部可见。

稳定的协议结构使用明确的私有 struct。`map[string]any` 只能用于协议明确允许开放字段且没有稳定 schema 的扩展位置；tool schema、message、content block、tool call 和 reasoning 配置不得用无约束 map 表达。

## 2. 通用映射顺序

所有 adapter 按同一逻辑顺序生成请求：

1. 校验 `ExecutionID`、`ModelCallNumber`、model 选择和消息顺序。
2. 将 `Model.ModelID` 写入 Provider 的模型字段；`ProviderID` 只用于路由和诊断，不默认发送给远程 API。
3. 将 `SystemPrompt` 映射到协议规定的 system 或等价指令字段。
4. 按原始顺序转换 `Messages` 与嵌套 content。
5. 将 `Tools` 转换为 Provider 的 tool definition，并深拷贝 JSON schema。
6. 将 max output tokens、reasoning 和其他已校验能力映射到协议参数。
7. 对序列化后的 body 执行大小和敏感信息边界检查，再创建 HTTP request。

request encoder 不增加应用层没有提供的用户消息、工具、权限或上下文，也不在 Provider 不支持某项能力时静默删除该项内容。

## 3. System prompt 与 messages

`SystemPrompt` 与普通 `Messages` 是两个不同输入边界。协议支持顶层 system 字段时必须使用该字段；协议只有 message 列表时，adapter 按该协议规定的 system/developer role 映射，并保持指令位于请求开头。

消息转换必须保留以下属性：

| 属性 | 约束 |
|---|---|
| role | 只能从 core 允许的 `user`、`assistant`、`tool` 等值映射到协议合法 role。未知 role 直接失败。 |
| content order | 一个 message 内的 text、thinking、tool use 和 tool result 按原始 content 顺序编码。 |
| response identity | `ResponseID` 用于恢复 Provider 需要的 assistant content 关联，不作为新的 execution 身份。 |
| tool identity | `ToolCallID` 必须在 tool use 与 tool result 之间保持不变。 |
| text | 作为 text content 编码；空 text 是否省略必须由协议规则固定，不能按运行时随机决定。 |
| thinking | 只在协议和当前模型能力允许时编码；签名等校验字段按协议要求保留。 |
| tool input/output | 通过协议要求的结构化字段编码，不能把 JSON 随意转成展示文本。 |

Provider 可能要求 assistant tool call 与对应 tool result 紧邻出现，或要求将多个 content block 合并到一个消息。该调整只发生在协议 encoder 内，不能改变 core message 的语义顺序和 tool call identity。

不支持的 message content、缺少 tool call 关联 ID、tool result 引用未知调用或违反协议 role 顺序时，encoder 返回 protocol/configuration 错误，不发送请求。

## 4. Tool definition

每个 `ToolDefinition` 至少包含稳定的工具名、描述和输入 JSON schema。encoder 必须：

- 保留工具列表顺序，除非协议明确要求排序；
- 深拷贝 schema 的 raw JSON bytes，禁止引用调用方可变 buffer；
- 验证 schema 是合法 JSON，并按协议要求放入 `parameters`、`input_schema` 或对应字段；
- 保留 required、properties、additionalProperties 等 schema 语义；
- 拒绝空工具名、重复工具名和超过本地边界的 schema；
- 不把 capability grant、approval 状态、sandbox 信息或 credential 写入 schema。

`Tools` 表示模型可以看到的工具集合，不表示模型返回的 tool call 已获授权。授权、参数校验和副作用执行仍由 `application/agent_runtime/loop.go` 负责。

工具响应的关联使用 `ToolCallID`：

```text
assistant tool_use(id=call-7)
    -> tool result(tool_call_id=call-7)
```

Provider 没有提供对应的关联字段时，adapter 必须拒绝无法可靠关联的请求或响应；不能依赖数组位置替代一个已经存在的稳定 ID。

## 5. Model 与 reasoning

模型字段来自 execution 已冻结的 `Model.ModelID`。adapter 不根据 ProviderID 猜测模型、不自动改写模型名，也不在远程返回未知模型时修改 registry。

Provider-neutral reasoning 配置先由 registry 规范化，再由 adapter 映射到协议字段。映射规则包括：

| 情况 | 行为 |
|---|---|
| 协议原生支持当前 reasoning level | 写入协议对应的 effort、budget、thinking 或等价字段。 |
| 协议支持 reasoning 但字段语义不同 | 使用明确的适配规则，并保留实际发送值用于内部诊断。 |
| 当前模型不支持该 level | 请求前返回稳定 configuration 错误。 |
| 协议不支持该能力 | 返回 unsupported-capability 错误，不能静默关闭 reasoning。 |
| Provider 返回与请求不一致的 reasoning 事件 | 按协议错误或 capability mismatch 处理，不能伪造正常 thinking。 |

请求中不发送 registry revision、credential、execution security snapshot 或内部 policy 字段。需要审计模型选择时使用 durable execution snapshot，而不是污染 Provider wire payload。

## 6. 协议映射边界

### 6.1 Anthropic Messages

- `SystemPrompt` 映射为顶层 `system` 内容；
- 普通 messages 使用 Anthropic role 和 content block；
- `tool_use` 使用 Provider 要求的调用 ID、name 和 JSON input；
- `tool_result` 使用对应的 `tool_use_id` 和结果内容；
- reasoning/thinking 使用模型支持的顶层 thinking 配置和 content block；
- 最大输出字段使用 Anthropic Messages 对应的字段，不复用 OpenAI 字段名。

### 6.2 OpenAI-compatible Chat Completions

- system prompt 进入 system 或协议允许的 developer message；
- assistant tool calls 编码为 `tool_calls[]`，保留每个 call 的 ID；
- tool result 编码为 `tool` role 并设置 `tool_call_id`；
- function name、arguments 和 JSON schema 进入协议定义的 function/tool 嵌套结构；
- reasoning 参数只有在目标 endpoint 明确支持时才发送。

### 6.3 OpenAI-compatible Responses

- system、user 和 assistant 内容按 Responses 的 input item/content part 结构编码；
- function call 与 function call output 使用协议要求的 call identity 和 output 字段；
- reasoning 配置使用 Responses 对应的参数，不复用 Chat Completions 的 `messages` 结构；
- 需要保留 response 关联时使用 Provider 要求的 previous response 或 item identity，但不把该 identity 当作 execution ID。

以上映射是三个协议包的责任边界。协议之间不得通过共享 wire DTO 或互相 import 复用字段定义。

## 7. 不可变性与安全

编码过程必须把所有嵌套可变值视为只读输入：

- 不修改 `Messages`、`Tools` 或其内部 content；
- 不持有调用方 `[]byte`、slice、map 的可变引用；
- 生成 request body 后，底层 HTTP request 只引用 adapter 自己拥有的 buffer；
- credential 只进入认证 header，不进入 body、URL query 或错误文本；
- 诊断信息只记录 protocol、model ID、status 和受控大小信息，不记录完整 body。

如果请求校验失败，必须在发送网络请求前返回。即使某个 Provider 可以容忍非法字段，adapter 也不能依靠远程错误来完成本地结构校验。

## 8. Contract test

每个协议包的 request test 至少覆盖：

- system prompt、message role 和 content 顺序；
- thinking 配置和不支持能力的失败；
- tool definition、JSON schema 深拷贝和工具顺序；
- 多个 tool call 的 ID 与 tool result 关联；
- max output tokens、model ID 和协议专用字段；
- 非法 role、重复工具名、损坏 schema、缺少关联 ID；
- body 中不包含 credential、execution security 或内部 revision；
- encoder 不修改输入对象。

测试应解析 JSON DTO 验证结构，不依赖 JSON 字段的偶然排列顺序。

## 9. 不变量

1. request encoder 只将 Provider-neutral 输入映射为本协议的私有 wire DTO。
2. Provider wire 类型不离开对应协议包，也不进入 core 或 application。
3. 请求发送前完成 role、tool identity、schema、reasoning 和资源边界校验。
4. tool call identity 在请求与响应的关联字段中保持稳定。
5. Provider 不支持的能力必须显式失败，不能静默降级或删除语义。
6. credential、security snapshot、registry revision 和内部状态不进入 Provider request body。
7. 编码过程不修改 `ModelRequest` 及其嵌套值。
