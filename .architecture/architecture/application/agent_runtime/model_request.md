# ModelRequest

> 本文定义 AgentRuntime 向模型 Provider 发起一次调用时的请求结构、构造边界和不可变性。
> AgentLoop 的长期状态和执行流程见 [`loop.md`](loop.md)，AgentRuntime 总体职责见 [`../../application.md`](../../application.md)。

## 1. 角色

`ModelRequest` 是一次 `ModelStream` 调用的 Provider-neutral 不可变输入。`application/agent_runtime/model_request.go` 在每次调用 Provider 前，从当前 `ExecutionRun` 的固定输入和 `AgentLoop.messages` 构造新的 `ModelRequest`。

`ModelRequest` 不是领域实体，不拥有独立持久化身份，不代替 `AgentExecution` 或 transcript。它只在当前模型调用期间存在，模型流结束后即可释放。

```text
ExecutionRun + AgentLoop.messages
    -> build immutable ModelRequest
    -> validate request and input limits
    -> ModelStream.Stream(executionContext, request)
    -> collect provider-neutral stream events
```

## 2. 数据结构

`ModelRequest` 只包含 Provider 完成模型调用所需的输入和最小关联信息：

```text
ModelRequest
├── ExecutionID
├── ModelCallNumber
├── Model
│   ├── ProviderID
│   ├── ModelID
│   └── Reasoning
├── SystemPrompt
├── Messages []Message
└── Tools []ToolDefinition
```

| 字段 | 用途 |
|---|---|
| `ExecutionID` | 关联日志和输出事件；默认不进入 Provider wire payload。 |
| `ModelCallNumber` | 当前 execution 内从 1 开始的模型调用序号，用于资源计数和诊断，不构成持久化身份。 |
| `Model` | execution 启动时固定的 Provider、模型和 reasoning 选择。 |
| `SystemPrompt` | 已组装完成的系统指令，不包含延迟解析的业务对象。 |
| `Messages` | Provider 在本次调用中看到的完整有序 message 快照。 |
| `Tools` | 经 capability grant 过滤后可向模型声明的工具名称、描述和输入 schema。 |

`ModelRequest` 不包含 Provider credential、HTTP header、endpoint、SDK request 或 Provider 专用 content block。这些数据只存在于 Provider adapter。

## 3. 构造

每次 model call 都构造一个新的 `ModelRequest`：

1. 确认 `ExecutionRun` 仍是当前 AgentLoop 的 active execution，且 execution context 未取消。
2. 从 execution 固定输入取得 `ExecutionID`、`Model` 和构造 `SystemPrompt` 所需的内容。
3. 按 capability grant 过滤工具定义，仅把可声明的工具写入 `Tools`。
4. 按当前 durable transcript sequence 对应的顺序复制 `AgentLoop.messages`。
5. 设置当前 `ModelCallNumber`，校验请求结构和输入资源限制。
6. 使用当前 execution context 调用 `ModelStream.Stream`。

`ContextManifest` 和 `ContextSelection` 是构造模型可见内容的来源，不直接进入 `ModelRequest`。它们选中的内容必须在 execution 启动时物化为 `SystemPrompt` 或 `Message`，运行中不重新读取后续的 SessionContext revision。

## 4. 不可变性

`AgentLoop.messages` 是持续增长的运行状态，`ModelRequest.Messages` 是一次调用边界的防御性副本。构造时必须复制：

- message slice 和每个 message；
- tagged content 及其嵌套数据；
- tool input 与 tool schema 的 raw JSON bytes；
- 其他可变 slice、map 或 byte buffer。

Provider adapter 不得通过 `ModelRequest` 修改 AgentLoop 状态。AgentLoop 在模型流返回 assistant message 或 tool call 后，先将完整 message durable 追加到 transcript，再更新自己的 `messages`；已发出的 `ModelRequest` 不随之变化。

## 5. Execution 与安全边界

`ModelRequest` 不嵌入 `AgentExecution`、`ExecutionInputSnapshot`、`ExecutionSecuritySnapshot` 或 `RuntimeExecutionSnapshot`。这些对象由 `ExecutionRun` 保留，AgentRuntime 只把最终的模型可见输入投影到 `ModelRequest`。

`RuntimeExecutionSnapshot` 中的 sandbox mode、approval mode 和 security revision 属于工具副作用边界。它只在执行工具时与 capability grant 一起进入 `ToolExecutionContext`，不向模型 Provider 传递。

```text
ModelRequest
    -> ModelStream

ToolCall + CapabilityGrant + RuntimeExecutionSnapshot
    -> authorization and approval gate
    -> ToolExecutionContext
    -> ToolRunner
```

`Tools` 只表示允许向模型公布的工具集合，不代表 Provider 返回的 tool call 已获得执行授权。AgentRuntime 在每次工具执行前仍必须校验 `ToolCallID`、工具名称、输入结构、capability grant 和 approval gate。

## 6. Provider adapter 边界

Provider adapter 只负责将 `ModelRequest` 映射为具体协议：

- 将 `Model`、`SystemPrompt`、`Messages` 和 `Tools` 编码为 Provider wire payload；
- 按 `ResponseID` 组合同一 assistant response 的 content blocks；
- 保留 `ToolCallID` 在 tool use 与 tool result 之间的关联；
- 把 Provider 流转换为 core-owned 的 model stream events。

Provider adapter 不读取 SQLite、transcript、SessionContext、Agent policy 或 execution 状态，也不决定工具授权、执行结算和重试策略。

## 7. 资源限制

AgentRuntime 在发起 `ModelStream` 前，使用已构造的 `ModelRequest` 执行输入字节数和模型上下文窗口校验。`ExecutionRun` 负责 model call、tool call 和累计输出计数；`ModelRequest` 不持有可变计数器。

资源超限时不发起 Provider 调用，AgentRuntime 返回稳定的 resource-limit outcome，并按 execution settlement 流程写入 durable receipt。

## 8. 不变量

1. 一个 `ModelRequest` 恰好对应一次 `ModelStream` 调用。
2. `ModelRequest` 的 model-visible 内容只来自当前 execution 的固定输入和所属 AgentLoop 的 messages。
3. `Messages` 的顺序与当前 durable transcript sequence 一致，且不与 AgentLoop 共享可变存储。
4. `ModelRequest` 不包含完整 execution、runtime security snapshot、credential 或 Provider 协议对象。
5. execution 运行期间不通过构造新请求更换 model、context revision 或 capability grant。
6. `ExecutionID` 是关联信息，`ModelCallNumber` 是 execution 局部计数；两者不为 `ModelRequest` 建立新的持久化身份。
7. Provider adapter 只做协议映射，不读取或修改 AgentRuntime 和产品状态。
