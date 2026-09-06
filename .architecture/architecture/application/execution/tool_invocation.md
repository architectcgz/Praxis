# ToolInvocation Application Service

> 状态：规范性目标架构。本文定义 `application/execution/tool_invocation` 的用例、端口、事务、副作用、审批、幂等和恢复规则。
> ToolInvocation 状态机见 [`../../domain/execution/tool_invocation.md`](../../domain/execution/tool_invocation.md)，AgentRuntime loop 见 [`../agent_runtime/loop.md`](../agent_runtime/loop.md)，执行输入边界见 [`../agent_runtime/model_request.md`](../agent_runtime/model_request.md)。

## 1. 位置与职责

`toolinvocation.Service` 是 ToolInvocation 产品状态的唯一写入口，并实现 AgentRuntime 使用的 `runtime.ToolInvoker`。

```text
AgentRuntime
    -> runtime.ToolInvoker
        -> application/execution/tool_invocation.Service
            -> persistence ports
            -> ToolExecutor / product command ports
                -> tools / sandbox / managedprocess
```

该服务负责：

- 把完整的 Provider-neutral ToolCall 转换为 durable ToolInvocation；
- 校验 execution 归属、生命周期和当前 ExecutionSecuritySnapshot；
- 使用工具 schema 校验并规范化参数；
- 计算参数摘要并保证重复调用幂等；
- 判定 capability、路径、命令、网络和 approval 约束；
- 在副作用前持久化 `running`，在结果返回后持久化 settlement；
- 返回 AgentRuntime 可写入 transcript 的有界 ToolResult；
- 为 recovery 保留足以判定“可继续、已完成或结果未知”的事实。

该服务不解析 Provider wire event，不写 Agent transcript，不执行具体文件或进程操作，也不推进 AgentExecution 和 Agent 的最终状态。

## 2. 源码目录

```text
backend/internal/application/execution/tool_invocation/
├── service.go       Service、Config、依赖端口和构造函数
├── invoke.go        Invoke、准入、规范化、幂等和派发
├── approval.go      Approve、Deny 和 durable approval wait
└── settle.go        running、result、failure 和 unknown 结算
```

Go package identifier 使用：

```go
package toolinvocation
```

包内唯一应用服务类型命名为 `Service`，跨包引用为 `toolinvocation.Service`。

## 3. 内层契约

`internal/runtime/tool.go` 定义 Provider-neutral 边界：

```text
ToolCall
ToolInvocationContext
ToolResult
ToolInvoker
AuthorizedToolCall
ToolExecutor
```

职责区分：

| 契约 | 调用方 | 实现方 | 语义 |
|---|---|---|---|
| `ToolInvoker` | AgentRuntime | `toolinvocation.Service` | 提交完整工具调用用例，包含准入、审批、幂等和结算。 |
| `ToolExecutor` | `toolinvocation.Service` | `internal/tools` 或 sandbox adapter | 只执行已经 durable 批准的具体副作用。 |

AgentRuntime 不能持有或调用 `ToolExecutor`。具体工具 adapter 不能实现 `ToolInvoker` 绕过 application service。

`ToolCall` 是不可信运行时输入。Provider 提供的路径、`RequiresWrite`、网络标记或命令提示只能作为输入信息，授权要求必须由工具定义、规范化参数和 ExecutionSecuritySnapshot 重新推导。

`ToolInvocationContext` 至少携带 `ExecutionID`、`SessionID`、`AgentID`、CapabilityGrant 和 RuntimeExecutionSnapshot。所有值来自当前 durable AgentExecution 的防御性快照，不能由 Provider 或前端构造。

## 4. Service 依赖

`Service` 至少依赖以下窄端口：

```text
persistence.Tx
persistence.AgentExecutionRepository
persistence.ExecutionSecuritySnapshotRepository
persistence.ToolInvocationRepository
persistence.EventRepository
runtime.ToolCatalog
runtime.ToolExecutor
system.Clock
system.IDGenerator
ApprovalSignal
```

需要创建 ManagedProcess 或提交产品领域结果的工具通过独立的内层 command port 调用对应用例，不能让 `internal/tools` 直接访问 application service 或 repository。`compose` 只负责注入实现，不承载工具路由规则和业务状态转换。

## 5. Invoke 流程

```text
AgentRuntime 已 durable 追加 tool_use message
    -> ToolInvoker.Invoke(ToolCall, ToolInvocationContext)
    -> 读取 AgentExecution 与 ExecutionSecuritySnapshot
    -> 读取工具定义、校验 schema、规范化参数
    -> 计算 ArgumentsDigest
    -> transaction: create or load ToolInvocation(requested)
    -> capability and approval decision
       ├── denied -> durable denied -> return denied result
       ├── awaiting_approval -> durable wait -> wait outside transaction
       └── approved -> durable approved
    -> transaction: approved -> running
    -> call ToolExecutor or product command port outside transaction
    -> transaction: durable succeed/fail/unknown
    -> return ToolResult
    -> AgentRuntime durable 追加 tool_result message
```

AgentRuntime 写 transcript，ToolInvocation service 写 SQLite。两者不共享事务；一致性通过 `ExecutionID`、`ToolInvocationID`、`ProviderToolCallID` 和 durable 状态对账。

## 6. 准入与参数规范化

准入顺序固定为：

1. 校验 context、ExecutionID、ProviderToolCallID 和工具名称；
2. 读取 execution 并确认其仍是当前 active execution；
3. 校验 SessionID、AgentID 与 invocation context 一致；
4. 从 durable ExecutionSecuritySnapshot 读取授权，不重新解释当前 Agent policy；
5. 根据 ToolCatalog 的 schema 解析并规范化参数；
6. 从规范化参数推导文件路径、命令、写入、网络和长期进程要求；
7. 计算 `ToolName + NormalizedArguments` 的稳定摘要；
8. 在 transaction 内创建或读取 ToolInvocation；
9. 执行 capability 和 approval 判定。

未知字段、重复语义字段、损坏 JSON、超限输入和无法规范化的路径必须在副作用前失败。稳定结构使用 typed value 和 JSON parser，不使用字符串拼接判断路径或命令。

## 7. 幂等协议

幂等键为：

```text
(ExecutionID, ProviderToolCallID)
```

处理规则：

| 已有状态 | 重复 Invoke 行为 |
|---|---|
| 参数摘要不同 | 返回 conflict，不修改已有记录。 |
| `requested` / `approved` | 继续同一 invocation 的准入或派发，不创建新记录。 |
| `awaiting_approval` | 复用同一等待，不创建第二个审批请求。 |
| `running` | 不再次执行；等待已有结果或返回明确 in-progress/unknown。 |
| `succeeded` / `failed` / `denied` / `interrupted` | 返回已保存的稳定结果。 |
| `unknown` | 返回结果未知，禁止自动重放。 |

参数摘要必须基于规范化后的结构计算。JSON 字段顺序、无意义空白和等价路径表示不能制造不同摘要；具有不同业务语义的参数不能折叠为相同摘要。

## 8. Approval

需要用户审批时，服务先 durable 保存 `awaiting_approval`，再在数据库事务之外等待。审批通知只用于唤醒，收到通知后必须重新读取 ToolInvocation 和 execution 状态。

```text
Approve(ToolInvocationID, ApprovalRecord)
Deny(ToolInvocationID, ReasonCode)
```

审批规则：

- Approval 只能放行 ExecutionSecuritySnapshot 已允许的操作，不能扩大工具、路径、命令或网络权限；
- 审批命令必须同时校验 ToolInvocationID、ArgumentsDigest 和安全策略 fingerprint，ApprovalRecord 作为该 invocation 的不可变审批事实保存；
- 修改参数必须建立新的 ProviderToolCallID 和 ToolInvocation，不能复用旧审批；
- execution cancellation、deadline 或 shutdown 会结束等待，并将尚未执行的 invocation 收敛为 `interrupted`；
- 等待期间不持有数据库 transaction、文件锁或 sandbox 资源。

## 9. 副作用与结算

数据库 transaction 不包裹 ToolExecutor、文件系统、进程、网络或 ManagedProcess 操作。执行顺序必须是：

```text
commit running
    -> execute side effect
    -> commit succeeded / failed / unknown
```

如果 executor 返回后结果 transaction 失败，不能假装副作用没有发生。记录保持 `running`，recovery 根据 executor receipt 或外部资源事实对账；无法证明结果时转为 `unknown`。

`ToolResult` 只有在对应 ToolInvocation settlement durable 后才能返回 AgentRuntime。过长输出先写入受管文件并 fsync，再把内容引用与 invocation result 一起提交；不能在 SQLite、transcript 或内存 channel 中无限缓存。

## 10. Transcript 边界

AgentRuntime 是所属 Agent transcript 的唯一 writer：

1. 完整 ToolCall 到达后先追加 `tool_use` message；
2. 调用 `ToolInvoker`；
3. ToolInvocation 已 durable 结算后追加 `tool_result` message；
4. tool result durable 后才能进入下一次 ModelRequest。

`toolinvocation.Service` 不直接打开 JSONL，也不把 SQLite payload 当作 transcript message。恢复发现已结算 invocation 缺少 tool result 时，由 AgentRuntime transcript owner 使用稳定 invocation identity 补齐一次；重复补齐必须幂等。

## 11. ManagedProcess 与产品命令

普通文件、搜索和短命令工具通过 ToolExecutor 执行，并受 execution cancellation 与 sandbox 生命周期约束。

`start_managed_process` 通过 ManagedProcess command port 创建 Session 持有的 durable ManagedProcess。成功结果必须包含 ManagedProcessID；ToolInvocation 只保存来源和结果引用，不拥有长期进程。`stop_managed_process` 和输入命令必须重新校验当前调用的 capability，不能继承来源 execution 之外的扩大权限。

`submit_result`、`submit_briefing` 和 `propose_delegate` 等产品命令通过对应 application command port 执行。它们仍有 ToolInvocation 记录，但领域对象由各自 application service 创建，具体 adapter 不直接写 repository。

## 12. Cancellation 与恢复

ToolExecutor 使用 execution context 或更短的 child deadline；child deadline 不能延长 execution deadline。

恢复规则：

| 状态 | 恢复行为 |
|---|---|
| `requested` | 重新执行无副作用的准入判定，或中断失去 active execution 的调用。 |
| `awaiting_approval` | active execution 已失效时标记 interrupted；不能脱离 execution 执行。 |
| `approved` | 只有能够证明从未派发时才允许继续，否则标记 unknown。 |
| `running` | 根据 durable executor receipt 对账；无证据时标记 unknown，禁止自动重放。 |
| 终止状态 | 不重新执行；只修复缺失投影或 transcript receipt。 |

Recovery 不根据进程内 goroutine、channel 或 UI 状态判断 invocation 是否完成。

## 13. 错误与可观测性

稳定错误至少区分：

```text
invalid_tool_call
tool_not_allowed
tool_approval_required
tool_approval_denied
tool_request_conflict
tool_execution_failed
tool_result_unknown
tool_resource_limit
tool_storage_failed
```

日志、事件和指标可以记录 ToolInvocationID、ExecutionID、工具名、状态、耗时、结果大小和稳定错误码。不得记录完整参数、文件正文、命令环境、credential、Provider 原始 body 或未经脱敏的 executor 输出。

## 14. 验证要求

至少覆盖：

- 同一 identity 与相同摘要只创建一个 invocation；
- 同一 identity 与不同摘要返回 conflict；
- schema、路径、命令、网络和 capability 在副作用前拒绝；
- approval 不能扩大快照权限，重复审批幂等；
- transaction 提交前不会调用 ToolExecutor；
- executor 成功但 settlement 失败时不会自动重放；
- cancellation、deadline 和 shutdown 能结束审批等待和 executor；
- 已结算结果可以幂等补齐 transcript receipt；
- 大结果转为内容引用且所有输入输出边界有上限；
- 日志、错误、事件和测试快照不泄漏敏感内容。

## 15. 不变量

1. 所有模型工具调用都通过 ToolInvoker 进入 `toolinvocation.Service`，AgentRuntime 不直接调用 ToolExecutor。
2. ToolInvocation 是工具调用状态的唯一产品事实；transcript 记录交互事实，但不替代 invocation 状态。
3. capability、approval 和幂等检查发生在任何副作用之前。
4. 外部副作用不进入数据库 transaction，且必须在 durable `running` 之后开始。
5. `running` 或 `unknown` invocation 不自动重放。
6. ToolResult 只在 invocation settlement durable 后返回 AgentRuntime。
7. `internal/tools`、sandbox 和 managedprocess adapter 不写 ToolInvocation repository。
8. ToolInvocation service 不写 transcript、不推进 AgentExecution，也不解析 Provider wire 协议。
