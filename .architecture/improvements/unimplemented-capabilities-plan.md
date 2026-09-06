# 未实现能力实施方案

## 1. 范围

本文只描述当前尚未形成生产实现的目标能力，以及它们在目标架构中的实施顺序：

- SessionContext 的追加、读取和 revision 校验；
- AgentSecurityPolicy、ExecutionSecuritySnapshot 的完整生成与持久化；
- ToolInvocation、工具调用 application service 和逐次审批；
- execution SandboxProcess、Windows worker 和平台隔离；
- Session 所有的 ManagedProcess、Supervisor 和 terminal attachment；
- Workflow definition、instance、node coordinator 和恢复；
- 上述能力所需的 SQLite、runtime、Wails binding、事件和 React 表面。

已有能力与目标架构的边界校正见 [`framework-migration-plan.md`](framework-migration-plan.md)。本文不重新定义 Project、Session、Agent、AgentExecution 的 ownership，也不通过新增中间聚合改变该关系。

## 2. 目标边界

```text
React UI
    -> app bindings / contracts
        -> core commands and queries
            -> AgentOrchestrator / WorkflowCoordinator
                -> AgentRuntime / tool_invocation application service / ManagedProcessCoordinator
                    -> core ports
                        -> storage / providers / tools / sandbox / managedprocess
```

依赖规则：

1. `domain` 只依赖标准库；`core` 不导入 Wails、SQLite driver、Provider SDK 或平台实现。
2. Workflow 只能调用 core 命令和查询，不能直接写 repository 或 transcript。
3. AgentRuntime 只能执行已 durable 的 AgentExecution；不能创建 Agent、提交 SessionContext 或决定审批。
4. `tool_invocation` application service 是 ToolInvocation 状态的唯一写入口；`internal/tools` 不读取 SQLite。
5. Sandbox 只负责 OS 进程、IPC 和隔离，不理解 Agent、Workflow 或产品状态。
6. ManagedProcessCoordinator 负责 Session 进程的产品状态；Supervisor 负责 PID、ConPTY、Job Object 等临时资源。
7. `compose` 是唯一组合根，所有具体实现通过显式依赖装配。

## 3. 能力设计

### 3.1 SessionContext

SessionContext 是 Session 级追加事实：

```text
SessionContextEntry
├── SessionID
├── Revision
├── Kind
├── SourceExecutionID?
├── Content
└── CreatedAt
```

核心命令：

```text
ReadContext(SessionID, Revision?) -> ContextSnapshot
AppendContext(SessionID, ExpectedRevision, Entry, RequestID) -> ContextRevision
```

实现要求：

- `(SessionID, Revision)` 唯一；追加必须在一个事务内检查 ExpectedRevision 并生成下一 revision。
- 内容只追加，不覆盖或删除；revision conflict 不能隐式合并。
- `ContextSelection` 在 execution 创建时从固定 revision 生成并冻结。
- transcript、草稿、未批准结果和中间推理不自动进入 SessionContext。

目标位置：`domain`、`orchestration`、`persistence`、`storage/sqlite`、`session`、`app`、`contracts` 和 `frontend/src/api/context.ts`。

### 3.2 SecurityPolicy 与 ExecutionSnapshot

每个 Agent 必须有带 revision 的 AgentSecurityPolicy。创建 execution 时由 SecurityResolver 计算不可变的 ExecutionSecuritySnapshot：

```text
SystemSecurityBaseline
    + AgentSecurityPolicy
    + ExecutionRestrictions
    + Project/Workspace path snapshot
    -> subset validation
    -> canonicalized ExecutionSecuritySnapshot
```

快照至少包括：policy revision、CapabilityGrant、resolved read/write roots、allowed tools/executables、sandbox constraints 和 approval rules。

实现要求：

- execution 请求只能收紧权限，不能超过 Agent policy 或系统基线。
- policy 扩大权限只影响新 execution；收紧权限要为 active execution 创建取消请求。
- execution 快照与 AgentExecution 一起 durable 保存，恢复时只读取快照，不重新解释当前 policy。
- Provider key、DataRoot、SQLite 和宿主进程句柄不能进入快照或 sandbox 输入。

目标位置：`domain`、`orchestration/security_resolver.go`、`persistence/security.go`、`storage/sqlite/*security*`、`app` 和 `contracts`。

### 3.3 ToolInvocation application service

每个 Provider tool call 映射为一个唯一 ToolInvocation：

```text
requested
├── denied
├── awaiting_approval -> approved -> running -> succeeded | failed
└── running -> interrupted | unknown
```

`tool_invocation.Service` 的职责：

1. 校验 ExecutionID、Agent、execution 状态和 tool call identity；
2. 规范化参数并生成 ArgumentsDigest；
3. 校验 CapabilityGrant、路径范围和 sandbox constraints；
4. 创建或读取 approval decision；
5. 将临时工具派发给 SandboxProcess，或将长期进程操作交给 ManagedProcessCoordinator；
6. durable 保存结果、failure code 和 transcript receipt。

同一 `(ExecutionID, ProviderToolCallID)` 必须幂等；参数摘要变化返回 conflict。Approval 只能放行已授权操作，不能扩大权限。

目标位置：`application/execution/tool_invocation/`、`runtime/tool.go`、`tools/catalog.go`、`tools/normalize.go` 和 SQLite repository。`tool_invocation.Service` 不直接 import `tools` 或 `managedprocess`，只通过 core port 派发已批准调用。

### 3.4 SandboxProcess 与 Windows worker

每个 AgentExecution 使用独立 SandboxProcess：

```text
Execution
    -> temporary directory
    -> restricted identity / AppContainer
    -> execution Job Object
    -> structured tool calls
    -> settlement
    -> terminate job and clean temporary resources
```

实现要求：

- 文件访问从快照中的授权根解析，拒绝符号链接、junction、reparse point 和路径逃逸。
- 不授予工具网络 capability；Provider 网络只存在于可信主进程。
- `run_command` 使用结构化 executable/arguments，所有子进程加入同一 Job Object。
- SandboxProcess 不能访问 DataRoot、SQLite、secrets、其他 execution 目录或 Provider credentials。
- Windows 不可用时拒绝 activation/tool execution，并返回稳定 failure code；禁止宿主进程回退。

目标位置：`internal/sandbox/client.go`、`protocol.go`、`windows/*_windows.go`、`cmd/sandboxworker/main_windows.go` 和 `runtime/sandbox.go`。

### 3.5 ManagedProcess

ManagedProcess 是 Session 拥有的长期进程，只能由 `start_managed_process` ToolInvocation 创建：

```text
starting -> running -> stopping -> settled
```

实现要求：

- 创建时绑定来源 ExecutionID、InvocationID 和安全快照，权限不能扩大。
- 从创建开始进入独立 Job Object、ConPTY 和受限 identity，不能从普通 `run_command` 子进程脱离。
- terminal attachment、PID、ConPTY handle、Job Object 和实时输出只存在 Supervisor 内存中。
- 用户或后续 execution 通过 StopManagedProcess、SendManagedProcessInput 控制；detach 不停止进程。
- graceful stop 超时后强制关闭 Job Object；重启时未结算进程标记 interrupted，不自动重启。

目标位置：`domain/managed_process.go`、`orchestration/managed_process.go`、`persistence/managed_process.go`、`internal/managedprocess/supervisor.go`、`terminal.go`、`conpty_windows.go` 和 SQLite repository。

### 3.6 Workflow

Workflow 是核心之上的独立模块：

```text
WorkflowDefinition
└── WorkflowInstance
    └── WorkflowNodeInstance
        ├── AgentID
        └── AgentExecutionID[]
```

实现要求：

- Definition 带 revision；Instance 固定 DefinitionRevision。
- Node 状态不能替代 AgentExecution 状态；所有执行身份引用真实 ExecutionID。
- dispatching 状态保存稳定 RequestID；跨 Workflow/Core 事务通过重试收敛。
- 节点启动读取固定 SessionContext revision，并调用核心 Start/Resume/Pause/Close/AppendContext 命令。
- 事件只用于唤醒，恢复必须通过 Workflow 查询和核心查询重新计算 ready/active/completed。
- Workflow 不直接修改 Agent policy、批准 ToolInvocation、写 transcript 或管理 OS 进程。

目标位置：`internal/workflow/definition.go`、`instance.go`、`coordinator.go`、`recovery.go`、`persistence/workflow.go`、`storage/sqlite/*workflow*`、`app`、`contracts` 和 `frontend/src/features/workflows`。

## 4. 实施顺序

### 阶段 1：领域与端口

- 添加上述领域类型、状态机、构造器、不可变快照和稳定错误码。
- 添加 persistence、runtime、tool、sandbox、managed process 和 Workflow 端口。
- 为每个端口提供内存 fake 与 contract test。

验收：目标包独立编译；core 不依赖外层实现；状态转换和幂等键有测试覆盖。

### 阶段 2：SessionContext 与安全快照

- 添加 SessionContext 表、revision 唯一约束和 append/read repository。
- 将 ContextSelection、Agent policy 和 ExecutionSecuritySnapshot 纳入 execution 创建事务。
- 为恢复增加 context revision 连续性校验和 policy snapshot 对账。

验收：并发 append 只有一个成功；execution 能在不读取当前配置的情况下重建输入和授权；revision conflict 可重试且不会覆盖内容。

### 阶段 3：ToolInvocation application service 与结构化工具

- 添加 ToolInvocation 表、状态机、approval command 和结果引用。
- 添加 ToolCatalog、参数规范化和 `read_file`、`list_dir`、`search_text`、`run_command` 的接口。
- 将 runtime tool call 接入 `tool_invocation` application service；未装配工具返回稳定错误并完成 settlement。

验收：重复 provider tool call 不产生第二条 invocation；参数变化返回 conflict；未授权路径、工具或命令在副作用前被拒绝。

### 阶段 4：SandboxProcess 与 Windows worker

- 实现 sandbox IPC protocol、worker 生命周期、受限身份、Job Object、ACL 和路径解析。
- 将临时工具执行从可信主进程移入 SandboxProcess。
- 增加 Windows 专项测试和 sandbox unavailable 的拒绝路径。

验收：execution 结束会回收整个子进程树和临时目录；sandbox 无法访问 DataRoot、secrets、SQLite 和其他 execution；路径逃逸测试全部拒绝。

### 阶段 5：ManagedProcess

- 实现 ManagedProcessCoordinator、Supervisor、ConPTY attachment 和停止命令。
- 添加 Session 级进程查询、输入、停止和 recovery 收敛。
- 将 `start_managed_process`、`stop_managed_process`、`send_process_input` 接入 `tool_invocation` application service。

验收：进程可在来源 execution 结算后继续运行；停止超时强制回收；应用重启不会自动重放或重启未知进程。

### 阶段 6：Workflow

- 实现 Definition/Instance/Node repository 和 coordinator。
- 实现 Agent binding、execution dispatch、context commit、pause/resume/cancel 和完成条件。
- 将 Workflow recovery 接入应用启动顺序和事件唤醒。

验收：Workflow 只通过核心命令改变 Agent 状态；重复 dispatch 返回既有 Agent/Execution；事件丢失后查询能够重建节点状态。

### 阶段 7：桌面表面与完整门禁

- 增加 Context、Security、Tool、Process、Workflow 的 app bindings 和 contracts。
- 在 `frontend/src/api` 增加对应 API 模块，在 `features` 增加快照视图和控制操作。
- 补充端到端测试、recovery 测试、provider/tool/sandbox contract test 和 Windows 集成测试。

验收：binding DTO 不含 secrets；前端只渲染查询投影；命令超时、事件丢失和应用重启都能恢复；通过 `go vet ./...`、`CGO_ENABLED=1 go test -race ./...`、行长检查和 `npm run build`。

## 5. 持久化与恢复

新增 schema 按版本递增，至少包括：

```text
session_context_entries
agent_security_policies
execution_security_snapshots
tool_invocations
managed_processes
workflow_definitions
workflow_instances
workflow_node_instances
workflow_node_executions
```

跨 SQLite、JSONL 和 OS runtime 的事实链使用稳定身份：

| 操作 | 幂等身份 | recovery 依据 |
|---|---|---|
| Context append | RequestID + ExpectedRevision | 已提交 revision 或 conflict |
| Execution snapshot | ExecutionID | SQLite snapshot + Agent policy revision |
| Tool invocation | ExecutionID + ProviderToolCallID | invocation status + ArgumentsDigest |
| ManagedProcess start | ManagedProcessID + InvocationID | invocation result + process state |
| Workflow dispatch | NodeInstanceID + RequestID | node state + core query |

启动恢复顺序固定为：readiness=false、JSONL repair、context revision 校验、execution/security 对账、ToolInvocation 与 ManagedProcess 收敛、Workflow 收敛、starting execution 激活、readiness=true。结果未知的模型调用、工具副作用和进程不能自动重放。

## 6. 完成条件

- [ ] 每项能力都有 core-owned domain、persistence 和 runtime port。
- [ ] 所有副作用都经过 `tool_invocation` application service、ExecutionSecuritySnapshot 和对应 OS 边界。
- [ ] SessionContext 追加式 revision 和 Workflow dispatch 幂等协议可恢复。
- [ ] SandboxProcess 与 ManagedProcess 的临时资源不进入产品事实。
- [ ] Workflow 不拥有 Agent/Execution，不直接写 transcript 或 repository。
- [ ] app/frontend 只暴露稳定 DTO 和命令错误，不暴露 secrets 或存储 payload。
- [ ] 各阶段均有 fake、contract、recovery 和必要的平台集成测试。
