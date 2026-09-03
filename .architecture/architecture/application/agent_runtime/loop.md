# AgentLoop

> 本文定义一个 Agent 的长期运行 loop、message 序列、execution 编排和并发边界。
> AgentRuntime 总体职责见 [`application.md`](../../application.md)，模型请求边界见 [`model_request.md`](model_request.md)，执行状态与 receipt 语义见 [`agent-runtime-model.md`](../../agent-runtime-model.md)，领域状态转换见 [`domain.md`](../../domain.md)。

## 1. 角色

`AgentLoop` 是一个 Agent 在当前进程中的长期 actor。每个 `AgentID` 至多对应一个 `AgentLoop`，由 `AgentRuntimeRegistry` 延迟创建并复用。

`AgentLoop` 负责：

- 串行处理该 Agent 的 activation、cancel、transcript append 和 close 命令；
- 持续维护该 Agent 的 `messages []Message` 及其 durable transcript sequence；
- 将新的 `AgentExecution` 组装成独立的执行状态；
- 协调 model call、tool call、transcript receipt 和 settlement callback；
- 在 execution 结束后释放 execution 局部资源，同时保留 AgentLoop 等待后续 execution。

`AgentLoop` 不代表新的领域实体。它的身份、当前 execution 和 messages 都是进程内状态，不能代替 SQLite 中的 `Agent`、`AgentExecution` 或 per-Agent transcript。

```text
AgentRuntimeRegistry
    -> AgentLoop(AgentID)
        -> messages []Message
        -> appliedTranscriptSequence
        -> active ExecutionRun?
        -> command processing
```

## 2. 长期 loop 与单次 execution

AgentLoop 在 runtime 创建后启动，在 runtime close 后停止。它可以依次执行多个 execution，但不复用上一次 execution 的局部状态。

```text
AgentLoop(agent-a)
    -> Activate(E1)
       -> ExecutionRun(E1)
       -> settle E1
       -> release E1 state
    -> Activate(E2)
       -> ExecutionRun(E2)
       -> settle E2
       -> release E2 state
    -> wait for next command
```

每个 `ExecutionRun` 都重新创建：

- `ExecutionID` 关联的运行状态；
- execution cancellation context；
- model call、tool call 和输入输出资源计数；
- `ExecutionInputSnapshot` 和 `ExecutionSecuritySnapshot` 的不可变引用；
- start、settlement 和失败结果的 receipt 对账状态。

E2 可以使用 E1 已经 durable 写入的 transcript 历史，但不能复用 E1 的 provider stream、取消 context、计数器或旧 snapshot。

loop 不把 turn 建模为持久化身份。`modelCallCount` 只是 `ExecutionRun` 的局部资源计数，用于限制一次 execution 的模型调用次数并终止无限 tool call 链；它在 execution 结算后释放。

## 3. 命令模型

AgentLoop 接收以下进程内命令：

| 命令 | 作用 |
|---|---|
| `Activate(execution, lifecycle)` | 接受一个已经 durable 创建且属于该 Agent 的 starting execution。 |
| `Cancel(executionID, outcome)` | 取消当前匹配的 execution，并保存目标 outcome。 |
| `AppendContextArtifact(artifact)` | 将已经批准的 context artifact 追加到该 Agent transcript，并推进 transcript sequence。 |
| `Close()` | 拒绝新命令，取消当前 execution，等待 receipt settlement，然后停止 loop。 |

命令按 Agent 串行化。相同 `ExecutionID` 的重复 activation 必须幂等；已有其他 active execution 时，新的 execution 不得进入 loop。

取消信号必须能直接触达当前 execution context。AgentLoop 不能因为等待不可控的 Provider 或工具调用而失去响应 cancel 和 close 的能力。

## 4. Message 序列

### 4.1 状态内容

AgentLoop 直接持有构造模型请求所需的消息序列。`Message` 是一条 Provider-neutral 消息，来源由 `Role` 表达，每条消息只包含一个带类型的 `Content`。message 不属于某个固定轮次，也不因新的 execution 开始而重新分组。

```text
AgentLoop
├── messages []Message
├── appliedTranscriptSequence
└── messagesReady

Message
├── ID
├── Role: user | assistant | tool
├── ResponseID?
└── Content MessageContent

MessageContent
├── Text { Value }
├── Thinking { Value, Signature? }
├── ToolUse { ToolCallID, Name, Input }
└── ToolResult { ToolCallID, Output, IsError }
```

`Content` 是 tagged union。其中 `text` 和 `thinking` 保存文本；`tool_use` 保存 `ToolCallID`、工具名和输入；`tool_result` 保存 `ToolCallID`、结果正文和错误标记。

一次模型响应可以产生多条 message，例如一条 text message 和两条 tool use message。这些 message 按产生顺序追加，并共享同一个 `ResponseID`。需要 content block 数组的 Provider adapter 根据 `ResponseID` 重新组合请求；AgentLoop 本身不维护嵌套的 content 数组。tool use 与 tool result 通过 `ToolCallID` 关联。

模型流中的 text delta 只累加到当前尚未完成的 text content；content 完成并 durable 写入后，才作为一条 message 追加到 `messages`。AgentLoop 不为每个流式 delta 创建 message。

### 4.2 Tool call 关联

Provider adapter 从模型响应中取得 `ToolCallID`；Provider 没有提供 ID 时，adapter 必须生成本次响应内唯一的 ID。AgentLoop 在执行工具前将 `ToolUse` durable 写入 transcript、追加到 `messages`，并在当前 `ExecutionRun` 中登记到 `pendingToolCalls`：

```text
assistant Message
└── ToolUse { ToolCallID: call-7, Name: read_file, Input: ... }

pendingToolCalls[call-7] -> ToolCall

tool Message
└── ToolResult { ToolCallID: call-7, Output: ..., IsError: false }
```

工具结果只能引用当前 execution 中尚未完成的 tool call。结果 durable 写入 transcript 并追加到 `messages` 后，从 `pendingToolCalls` 移除对应项；未知、重复或已经完成的 `ToolCallID` 必须拒绝。持久化幂等身份使用 `(ExecutionID, ToolCallID)`，不能假设 Provider 生成的 ID 在所有 execution 中全局唯一。

构造下一次 `ModelRequest` 时，Provider adapter 保留相同 ID：Anthropic 映射为 `tool_use.id` 与 `tool_result.tool_use_id`，OpenAI-compatible 协议映射为 `tool_calls[].id` 与 `tool_call_id`。

`messages` 按模型实际看到的顺序持续增长。receipt 或其他不构成 model message 的 transcript entry 仍然推进 `appliedTranscriptSequence`，但不进入 `messages`。

### 4.3 建立与延续

首次使用 AgentLoop 时，从该 Agent 的 transcript store 建立 `messages`。之后的 execution 直接使用 AgentLoop 当前的 messages，不重复读取完整 transcript，也不为 `ExecutionRun` 复制一份长期消息列表。

```text
first command
    -> initialize transcript
    -> load messages and sequence
    -> messagesReady = true

next execution
    -> verify appliedTranscriptSequence
    -> create ExecutionRun
    -> use AgentLoop.messages
```

`messages` 是 loop 运行所需的工作状态，durable transcript 是它的恢复来源和持久化事实。以下情况必须重新从 durable transcript 建立 messages：

- AgentLoop 刚刚创建或进程完成 recovery；
- `messagesReady` 为 false；
- durable sequence 与 `appliedTranscriptSequence` 不一致；
- transcript repair 或其他一致性检查要求丢弃内存状态。

### 4.4 状态推进

所有 transcript 写入都必须经过同一个 AgentLoop transcript owner，或通过等价的、带 sequence 校验的窄端口完成。写入顺序为：

```text
append entry
    -> fsync durable transcript
    -> advance appliedTranscriptSequence
    -> append Message when entry is model-visible
    -> acknowledge caller
```

fsync 失败时不得推进 `appliedTranscriptSequence` 或 `messages`。context delivery、execution start、assistant message、tool result 和 execution settlement 都必须遵守相同的 durable-before-memory 规则。

AgentLoop 不接受其他 Agent 的 transcript 写入，也不使用另一个 Agent 的 `messages` 构造模型请求。

## 5. Execution 流程

```text
Activate(E)
    -> validate E belongs to AgentLoop
    -> ensure messages are ready
    -> create ExecutionRun(E)
    -> append execution_started and input
    -> fsync transcript
    -> append input to AgentLoop.messages
    -> confirm execution start
    -> build immutable ModelRequest from AgentLoop.messages
    -> stream model response
    -> append assistant and tool messages to transcript
    -> fsync and append them to AgentLoop.messages
    -> build the next ModelRequest when another model call is required
    -> append execution_settled
    -> fsync transcript
    -> call settlement port
    -> clear active ExecutionRun
    -> wait for next command
```

一次 execution 内的 model/tool loop 直接推进 AgentLoop 的 `messages`。每次调用 Provider 前，从当前 `messages` 创建不可变 `ModelRequest`；其中的消息切片是调用边界的防御性副本，不是另一份长期消息状态。每次 model call 都不重新读取 transcript。

每个新的 execution 仍然由 execution application service 固定自己的 SessionContext revision、模型选择、安全快照和 runtime 限制。AgentLoop 不能在执行中读取后来追加的共享 SessionContext，也不能用 `messages` 绕过 execution snapshot。

## 6. 取消、结算与关闭

### 6.1 Cancel

```text
control application service
    -> durable control request
    -> AgentLoop.Cancel(E.ID)
    -> cancel E context
    -> stop new model call and tool call
    -> append execution_settled
    -> settlement application service updates product state
```

已经完成的模型输出、工具副作用和 transcript append 不会因为 cancel 被删除。取消只改变 execution 的后续执行和最终 outcome。

### 6.2 Close

`Close` 先拒绝新的 activation，再取消 active execution。只有当前 execution 完成 durable settlement 后，AgentLoop 才能释放 transcript store、停止 command loop 并从 registry 移除。

Agent 的产品状态由 Agent domain 的 `Close` 转换和 application transaction 持久化；AgentLoop 的 `Close` 只管理进程内运行资源。

## 7. Recovery

AgentLoop 不依赖 goroutine 是否存在来判断产品状态。进程启动时：

1. recovery 修复并校验 Agent transcript；
2. 根据 durable receipt 对账 execution start 和 settlement；
3. 为仍可运行的 starting execution 创建或恢复 AgentLoop；
4. AgentLoop 从 transcript 建立 `messages` 和 `appliedTranscriptSequence`；
5. scheduler 将 starting execution 发送给对应 AgentLoop。

没有 settlement receipt 的 active execution 按 recovery 规则收敛为 `interrupted`，不能由 `messages` 或其他 loop 内存状态推断为已完成。

## 8. 边界与不变量

### 8.1 所属边界

| 能力 | 所属组件 |
|---|---|
| Agent 和 AgentExecution 产品状态转换 | `core/domain` + application service |
| AgentLoop 命令串行化、`messages` 和当前 execution | `application/agent_runtime` |
| 跨 Agent 调度、投递和 recovery | `orchestration` |
| Model、tool、transcript 和 lifecycle 端口契约 | `core` |
| JSONL 文件和 fsync 实现 | storage adapter |

### 8.2 不变量

1. 每个 Agent 同时最多运行一个 active execution。
2. 每个 execution 只属于一个 AgentLoop，重复 activation 不启动第二个执行环。
3. AgentLoop 的 `messages` 永远不能领先于 durable transcript。
4. `appliedTranscriptSequence` 不匹配时必须重建 `messages` 或拒绝使用，不得静默使用旧消息。
5. 每个 execution 的 cancel context、资源计数和 snapshot 都在 settlement 后释放。
6. AgentLoop 不创建或直接修改 Agent、AgentExecution、SessionContext 和其他产品状态。
7. AgentLoop 不读取其他 Agent transcript，不调用具体 Provider SDK 或具体工具实现。
8. 进程重启后，durable transcript 和 execution receipt 足以重建 AgentLoop 的必要内存状态。
