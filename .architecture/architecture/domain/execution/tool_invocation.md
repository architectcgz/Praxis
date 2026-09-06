# ToolInvocation 领域模型

> 状态：规范性目标架构。本文定义 `domain/execution` 中 ToolInvocation 的身份、字段、状态机、结果和不变量。
> 应用用例见 [`application/execution/tool_invocation.md`](../../application/execution/tool_invocation.md)，execution 总体模型见 [`orchestration/README.md`](../../orchestration/README.md)，持久化分工见 [`storage/README.md`](../../storage/README.md)。

## 1. 模型定位

ToolInvocation 是 AgentExecution 拥有的持久化子实体，表示一个已经通过 application command admission 的模型工具调用。

以下对象不是 ToolInvocation：

- Provider wire event 或尚未拼接完整的 tool arguments；
- `runtime.ToolCall` 表达的未准入运行时输入；
- 工具 schema、注册表或具体工具实现；
- sandbox worker、PID、Job Object、ConPTY 或文件句柄；
- Agent transcript 中的 `tool_use` 和 `tool_result` message。

领域层只表达工具调用的业务事实和合法状态转换，不解析 Provider 协议、不访问 repository，也不执行副作用。

## 2. 文件位置

```text
backend/internal/domain/execution/
├── agentexecution.go
├── execution.go
├── executionfailure.go
├── tool_invocation.go
└── helpers.go
```

ToolInvocation 位于 `execution` package，因为它不能脱离 AgentExecution 独立存在，并且其准入条件依赖 execution 的身份与生命周期。它不建立新的顶层聚合，也不改变 `Agent -> AgentExecution` ownership。

## 3. 数据结构

```text
ToolInvocation
├── ID: ToolInvocationID
├── ExecutionID
├── SessionID
├── AgentID
├── ProviderToolCallID
├── Name: ToolName
├── NormalizedArguments
├── ArgumentsDigest
├── Status
├── Approval?
├── Result?
├── FailureCode
├── CreatedAt
├── ApprovedAt?
├── StartedAt?
└── SettledAt?
```

字段规则：

| 字段 | 规则 |
|---|---|
| `ID` | 服务端生成的稳定全局身份，不使用 Provider ID 替代。 |
| `ExecutionID` | 所属 AgentExecution，创建后不可变。 |
| `SessionID` / `AgentID` | 用于复合归属校验和查询；必须与所属 execution 一致。 |
| `ProviderToolCallID` | Provider-neutral call identity，只在一个 execution 内唯一。 |
| `Name` | 来自系统支持的稳定 `ToolName`，未知名称不能建立 invocation。 |
| `NormalizedArguments` | 通过对应 schema 校验并规范化后的不可变参数；必须执行防御性复制。 |
| `ArgumentsDigest` | 对工具名和规范化参数计算的稳定摘要，用于重复调用冲突检测。 |
| `Approval` | 已批准调用的不可变 ApprovalRecord；不能修改参数或扩大 capability grant。 |
| `Result` | 有界结果、内容引用、副作用标记和稳定错误分类，不保存无界输出。 |

Credential、完整 prompt、thinking、环境变量 secret、DataRoot 和原始 Provider body 不得进入 ToolInvocation。

## 4. 状态机

```text
requested
├── awaiting_approval
│   ├── approved
│   ├── denied
│   └── interrupted
├── approved
├── denied
└── interrupted

approved -> running
running
├── succeeded
├── failed
├── interrupted
└── unknown

unknown -> succeeded | failed
```

状态语义：

| 状态 | 语义 |
|---|---|
| `requested` | 调用身份和规范化参数已 durable 建立，尚未完成审批判定。 |
| `awaiting_approval` | 需要用户决定，尚未允许执行副作用。 |
| `approved` | 权限和审批均已通过，但 executor 尚未开始。 |
| `running` | 已 durable 记录开始执行，可能已经发生外部副作用。 |
| `succeeded` | executor 成功，结果或结果引用已保存。 |
| `failed` | executor 返回明确失败，失败码和有界结果已保存。 |
| `denied` | capability 或审批拒绝，未执行副作用。 |
| `interrupted` | 在可证明已停止的边界中断，不能宣称成功。 |
| `unknown` | 无法证明副作用是否发生或完成，禁止自动重放。 |

`succeeded`、`failed`、`denied` 和 `interrupted` 是终止状态。`unknown` 只能依据显式对账证据转为 `succeeded` 或 `failed`，不能由调用方猜测结算。

## 5. 领域行为

`tool_invocation.go` 至少提供以下语义明确的行为：

```text
NewToolInvocation
RequireApproval
Approve
Deny
Start
Succeed
Fail
Interrupt
MarkUnknown
Reconcile
Validate
Snapshot
```

行为约束：

1. `NewToolInvocation` 只接受已规范化参数和稳定摘要，不在领域对象内部解析任意 JSON schema。
2. `Approve` 保存 ApprovalRecord，但不能改变名称、参数、摘要、execution 或安全快照来源。
3. `Start` 必须发生在 `approved` 之后，并在副作用调用之前 durable 保存。
4. `Succeed` 和 `Fail` 只接受 `running` 状态，并保存有界结果或内容引用。
5. `Deny` 不产生 executor 调用；拒绝原因使用稳定分类，不能保存任意敏感错误文本。
6. 所有时间使用调用方注入值并规范化为 UTC；领域对象不读取系统时钟。

## 6. 身份与幂等

数据库和 repository 必须保证：

```text
UNIQUE (execution_id, provider_tool_call_id)
```

相同 `(ExecutionID, ProviderToolCallID)` 再次准入时：

- 工具名和 `ArgumentsDigest` 相同，返回已有 ToolInvocation；
- 工具名或摘要不同，返回 request conflict；
- 已终止调用返回已有结果，不创建第二条记录；
- `running` 或 `unknown` 调用不能以重复请求为理由再次执行。

ProviderToolCallID 不要求跨 execution 全局唯一。`ToolInvocationID` 是持久化关系、事件和结果引用使用的唯一全局身份。

## 7. 结果边界

ToolInvocationResult 至少表达：

```text
ToolInvocationResult
├── InlineContent?
├── ContentReference?
├── ErrorCode?
├── SideEffect
└── Truncated
```

结果只能选择有界内联内容或稳定内容引用。路径本身不能作为内容身份；引用必须包含可校验的类型、大小和摘要。展示文本、日志文本和 Provider tool result 由 application/runtime 显式投影，不能直接复用 storage payload。

## 8. 领域不变量

1. 每个 ToolInvocation 恰好属于一个 AgentExecution，且 SessionID、AgentID 与 execution 归属一致。
2. `(ExecutionID, ProviderToolCallID)` 唯一；相同 identity 的参数摘要不可变化。
3. 名称、规范化参数、摘要和 execution 归属在创建后不可变。
4. 未批准调用不能进入 `running`；审批不能扩大 ExecutionSecuritySnapshot。
5. 任何副作用开始前必须先 durable 进入 `running`。
6. `running` 且结果未知的调用不能自动重放，只能中断或通过证据对账。
7. 领域对象不持有 repository、ToolInvoker、ToolExecutor、进程资源或 transcript writer。
8. ToolCall、ToolDefinition 和 Provider event 是 runtime/protocol 对象，不是持久化领域实体。
