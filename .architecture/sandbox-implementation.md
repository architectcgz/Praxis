# Agent 沙箱实现方案

> 本文定义 [`sandbox.md`](sandbox.md) 的代码结构、持久化结构、运行时协议、Windows 隔离实现和交付顺序。
> 本方案只描述目标实现；领域层次见 [`architecture/structure.md`](architecture/structure.md)，执行超时见 [`architecture/agent-timeout-model.md`](architecture/agent-timeout-model.md)。

## 1. 实现范围

目标实现提供：

- 每个 Agent 持久化一份带 revision 的 AgentSecurityPolicy；
- 每个 AgentExecution 持久化一份不可变的 ExecutionSecuritySnapshot；
- ToolInvocation 的授权、审批、执行、结果和恢复状态；
- 每个 AgentExecution 独立的 Windows SandboxProcess；
- AppContainer 文件与对象隔离；
- Job Object 管理 `run_command` 启动的完整进程集合；
- 结构化文件工具和结构化命令执行；
- Session 级 ManagedProcess、ConPTY 挂载和显式停止；
- 沙箱不可用时拒绝 activation 或工具执行。

SandboxProcess 不具备网络 capability。实现不包含 CPU 配额、内存配额、任意 shell 字符串和宿主进程执行回退。

## 2. 模块与依赖

```text
AgentRuntime
    │ runtime.ToolRunner
    ▼
core/orchestrate.ToolBroker
    ├── ToolInvocationRepository
    ├── ToolCatalog
    ├── ToolApprovalWaiter
    ├── runtime.ToolExecutor
    │            ▲
    │            │ implements
    │   internal/tools.Executor
    │       └── sandbox.Client
    │                ▲
    │                │ IPC
    │       Windows SandboxProcess
    └── ManagedProcessCoordinator
             └── runtime.ManagedProcessLauncher
                              ▲
                              │ implements
                  managedprocess.Supervisor
```

依赖方向：

1. `AgentRuntime` 只依赖 `internal/core/runtime` 中的工具接口；
2. `ToolBroker` 位于 `internal/core/orchestrate`，是 ToolInvocation 状态的唯一写入口；
3. `internal/tools` 负责工具 schema、参数规范化和执行适配，不读取 SQLite；
4. `ManagedProcessCoordinator` 持久化 Session 进程状态，`internal/managedprocess` 持有 ConPTY、Job Object 和运行时连接；
5. `internal/sandbox` 负责进程、IPC 和平台隔离，不理解 Agent、Workflow 或审批；
6. `internal/compose` 组装 ToolBroker、工具实现、ManagedProcessSupervisor 和 Windows sandbox；
7. Workflow 只调用核心命令，不直接依赖 ToolBroker、ManagedProcessSupervisor 或 sandbox。

目标文件布局：

```text
backend/
├── main.go
├── cmd/
│   └── sandboxworker/
│       └── main_windows.go
└── internal/
    ├── core/
    │   ├── domain/
    │   │   ├── agent_security_policy.go
    │   │   ├── execution_security.go
    │   │   ├── tool_invocation.go
    │   │   └── managed_process.go
    │   ├── orchestrate/
    │   │   ├── security_resolver.go
    │   │   ├── security_commands.go
    │   │   ├── tool_broker.go
    │   │   ├── tool_approval.go
    │   │   └── managed_process.go
    │   ├── persistence/
    │   │   ├── security.go
    │   │   └── managed_process.go
    │   └── runtime/
    │       ├── tool.go
    │       ├── sandbox.go
    │       └── managed_process.go
    ├── tools/
    │   ├── catalog.go
    │   ├── normalize.go
    │   ├── filesystem.go
    │   └── command.go
    ├── sandbox/
    │   ├── client.go
    │   ├── protocol.go
    │   └── windows/
    │       ├── launcher_windows.go
    │       ├── appcontainer_windows.go
    │       ├── job_windows.go
    │       ├── acl_windows.go
    │       └── process_windows.go
    ├── managedprocess/
    │   ├── supervisor.go
    │   ├── terminal.go
    │   └── conpty_windows.go
    └── storage/sqlite/
        ├── agent_security_policy_repo.go
        ├── execution_security_repo.go
        ├── tool_invocation_repo.go
        └── managed_process_repo.go
```

接口按职责命名为 `ToolRunner`、`ToolExecutor`、`ToolCatalog`、`SandboxLauncher` 和具体 repository。

## 3. 领域类型

### 3.1 AgentSecurityPolicy

```go
type AgentSecurityPolicy struct {
    Revision     uint64
    Capabilities CapabilityPolicy
    Sandbox      SandboxPolicy
    Approval     ApprovalPolicy
}

type CapabilityPolicy struct {
    AllowedTools       []ToolName
    ReadScopes         []PathScope
    WriteScopes        []PathScope
    AllowedExecutables []ExecutableRule
}
```

`PathScope` 使用 Project 内相对路径或用户显式选择的外部绝对根目录。`ExecutableRule` 约束 `run_command` 可以直接启动的 executable；shell 不是默认 command runner。

Agent 直接持有当前 AgentSecurityPolicy。Policy 没有独立 ID，`Revision` 在一个 Agent 内单调递增。

### 3.2 ExecutionSecuritySnapshot

```go
type ExecutionSecuritySnapshot struct {
    AgentPolicyRevision uint64
    CapabilityGrant     CapabilityGrant
    SandboxConstraints  SandboxConstraints
    ApprovalRules       []ApprovalRule
}

type CapabilityGrant struct {
    AllowedTools       []ToolName
    ReadRoots          []ResolvedPathRoot
    WriteRoots         []ResolvedPathRoot
    AllowedExecutables []ResolvedExecutable
}
```

CapabilityGrant 是 execution 拥有的值对象，与 ExecutionSecuritySnapshot 共享生命周期。SandboxConstraints 记录操作系统隔离需要执行的文件、环境和子进程约束，不包含 CPU 或内存配额。工具网络由系统安全基线统一禁止，不在 Agent policy 中保存开关。

### 3.3 ToolInvocation

```go
type ToolInvocation struct {
    ID                  ToolInvocationID
    ExecutionID         AgentExecutionID
    ProviderToolCallID  string
    Tool                ToolName
    NormalizedArguments []byte
    ArgumentsDigest     string
    ApprovalState       ToolApprovalState
    Status              ToolInvocationStatus
    ResultReference     string
    FailureCode         ToolFailureCode
    CreatedAt           time.Time
    StartedAt           time.Time
    SettledAt           time.Time
}
```

`(ExecutionID, ProviderToolCallID)` 唯一。同一 provider tool call 重试时必须返回既有 invocation；工具名或参数摘要不一致时返回 request conflict。

### 3.4 ManagedProcess

```go
type ManagedProcess struct {
    ID                    ManagedProcessID
    SessionID             SessionID
    StartedByExecutionID  AgentExecutionID
    StartedByInvocationID ToolInvocationID
    Command               CommandSnapshot
    Status                ManagedProcessStatus
    Outcome               ManagedProcessOutcome
    FailureCode           ManagedProcessFailureCode
    CreatedAt             time.Time
    StartedAt             time.Time
    SettledAt             time.Time
}
```

ManagedProcess 状态为 `starting | running | stopping | settled`。它引用来源 execution 的不可变 ExecutionSecuritySnapshot，不复制或扩大授权。PID、AppContainer SID、ConPTY handle、Job Object、terminal attachment 和实时输出属于 ManagedProcessSupervisor 的临时状态。

ManagedProcess 由 Session 持有。来源 AgentExecution 结算不改变 ManagedProcess 状态；Session 关闭、来源权限撤销、显式停止和应用 shutdown 会触发停止。

## 4. 命令契约

### 4.1 Agent policy 命令

```text
CreateAgent(
    SessionID,
    Profile,
    SecurityPolicy,
    RequestID,
)

UpdateAgentSecurityPolicy(
    AgentID,
    ExpectedRevision,
    SecurityPolicy,
    RequestID,
)
```

Update 使用 ExpectedRevision 防止并发覆盖。扩大权限只影响之后创建的 execution；缩小权限时在同一事务中记录对 active execution 的取消请求，并停止派发新的 tool call。

### 4.2 Execution 命令

`SendInput`、`Resume` 和 Workflow execution 命令不接收 sandbox mode 或 approval mode。手动 execution 使用 Agent 当前策略；Workflow 可以携带只能缩小权限的 `ExecutionRestrictions`。

### 4.3 Tool approval 命令

```text
DecideToolInvocation(
    InvocationID,
    ArgumentsDigest,
    Decision: approve | deny,
    RequestID,
)
```

命令只接受 `awaiting_approval` 状态，必须精确匹配 ArgumentsDigest，并持久化决定、决定时间和 RequestID。决定提交后再唤醒进程内 waiter。

### 4.4 ManagedProcess 命令

`start_managed_process` 只能作为 ToolInvocation 执行。用户和后续 AgentExecution 使用以下命令控制已经存在的进程：

```text
StopManagedProcess(ManagedProcessID, RequestID)
SendManagedProcessInput(ManagedProcessID, Content, RequestID)
```

Agent 发起控制时仍通过 `stop_managed_process` 或 `send_process_input` ToolInvocation，并受当前 execution 的 capability 与 approval 校验。用户从 Session 界面直接控制时使用公开核心命令。Terminal attach/detach 只改变进程内订阅，不写 ManagedProcess 状态。

## 5. 安全快照生成

`security_resolver.go` 提供纯领域解析流程：

```text
SystemSecurityBaseline
    + AgentSecurityPolicy
    + ExecutionRestrictions
    + Project path snapshot
    -> validate subset
    -> canonicalize and resolve paths
    -> resolve executable identities
    -> ExecutionSecuritySnapshot
```

创建 execution 的流程：

1. 读取 Project、Agent 和当前 policy revision；
2. 在事务外完成文件系统路径解析；
3. 进入事务并再次校验 Project path 与 policy revision；
4. 按 RequestID 查询幂等结果；
5. 插入 AgentExecution 和 ExecutionSecuritySnapshot；
6. 把 Agent 标记为 executing；
7. 提交后尝试 activation。

路径或 policy revision 在第 2 至第 3 步发生变化时重新解析，不能用过期结果创建 execution。

## 6. SQLite 结构

目标 schema 包含：

```sql
CREATE TABLE agent_security_policies (
    agent_id TEXT NOT NULL,
    revision INTEGER NOT NULL CHECK (revision > 0),
    created_at TEXT NOT NULL,
    payload TEXT NOT NULL,
    PRIMARY KEY (agent_id, revision),
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE RESTRICT
);

CREATE TABLE execution_security_snapshots (
    execution_id TEXT PRIMARY KEY,
    agent_policy_revision INTEGER NOT NULL CHECK (agent_policy_revision > 0),
    payload TEXT NOT NULL,
    FOREIGN KEY (execution_id) REFERENCES agent_executions(id) ON DELETE RESTRICT
);

CREATE TABLE tool_invocations (
    id TEXT PRIMARY KEY,
    execution_id TEXT NOT NULL REFERENCES agent_executions(id) ON DELETE RESTRICT,
    provider_tool_call_id TEXT NOT NULL,
    tool TEXT NOT NULL,
    arguments_digest TEXT NOT NULL,
    approval_state TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    started_at TEXT,
    settled_at TEXT,
    payload TEXT NOT NULL,
    UNIQUE (execution_id, provider_tool_call_id)
);

CREATE TABLE managed_processes (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE RESTRICT,
    started_by_execution_id TEXT NOT NULL REFERENCES agent_executions(id) ON DELETE RESTRICT,
    started_by_invocation_id TEXT NOT NULL REFERENCES tool_invocations(id) ON DELETE RESTRICT,
    status TEXT NOT NULL,
    outcome TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    started_at TEXT,
    settled_at TEXT,
    payload TEXT NOT NULL,
    UNIQUE (started_by_invocation_id)
);
```

`agents` 保存当前 `security_policy_revision`。创建 Agent、保存 revision 1 和设置当前 revision 在一个事务内完成。更新 policy 时插入新 revision，再更新 Agent 当前 revision。

`agent_executions` 与 `execution_security_snapshots` 必须在同一事务中插入。读取 execution 时缺少 snapshot 属于存储损坏，不能使用默认权限继续运行。

创建 ManagedProcess 时，ToolInvocation 进入 `running` 和 ManagedProcess 进入 `starting` 必须在同一事务完成。Supervisor 报告进程 ready 后才能标记 `running` 并向 ToolInvocation 返回 ManagedProcessID。

## 7. ToolBroker

ToolBroker 实现 `runtime.ToolRunner`，执行固定流程：

```text
Execute tool call
    -> ToolCatalog 解析并规范化参数
    -> 计算 ArgumentsDigest
    -> 创建或读取 ToolInvocation
    -> 校验 execution 仍 active
    -> 校验 CapabilityGrant
    -> 解析 ApprovalRules
       ├── deny -> invocation denied
       ├── ask -> invocation awaiting_approval -> 等待 durable decision
       └── allow
    -> dispatch
       ├── 临时工具 -> ToolExecutor.Execute
       └── start/stop/input -> ManagedProcessCoordinator
    -> 持久化 succeeded | failed | interrupted | unknown
    -> 追加 tool result transcript receipt
```

ToolCatalog 输出工具的结构化需求：读取路径、写入路径、直接启动的 executable、arguments、working directory 和是否产生副作用。ToolBroker 根据这些需求校验 snapshot；不能信任 Provider 自报的 `RequiresWrite` 或 `RequiresNetwork`。

ToolBroker 先持久化 `running`，再调用 ToolExecutor。成功结果 durable 后才能向模型返回。进程中断且无法证明副作用结果时标记为 `unknown`，不自动重试。

`start_managed_process` 在 dispatch 前生成 ManagedProcessID，并由 ManagedProcessCoordinator 使用来源 ExecutionSecuritySnapshot 创建独立运行时。它不能调用 execution worker 先启动命令，也不能把 `run_command` 的子进程转换为 ManagedProcess。

进程内 `ToolApprovalWaiter` 只负责唤醒，不是事实源。应用恢复时，`awaiting_approval` invocation 收敛为 `interrupted`，所属 execution 收敛为 interrupted；后续继续工作必须创建新的 AgentExecution。

## 8. Model/tool loop

Provider runner 支持一个 execution 内的多轮模型与工具循环：

```text
model turn
    -> persist assistant text and tool requests
    -> sequentially execute tool calls through ToolBroker
    -> persist tool results
    -> construct next model turn
    -> repeat until model completes or execution ends
```

同一 model turn 的多个 tool call 按 Provider 返回顺序执行。顺序执行使审批、文件副作用和 transcript receipt 具有确定顺序。

提供给模型的 ToolDefinition 必须同时满足：

- ToolCatalog 存在该工具；
- ExecutionSecuritySnapshot 允许该工具；
- 当前平台 sandbox 支持该工具；
- 工具 schema 能被对应 Provider 表达。

## 9. Sandbox worker 协议

SandboxProcess 运行独立的 `praxis-sandbox-worker.exe`。可信主进程通过受限匿名 pipe 或 ACL 限定的 named pipe 通信，协议使用长度前缀 JSON frame：

```text
Host -> Worker
SandboxHello(ExecutionID, ProtocolVersion, SandboxSpec)
ToolRequest(InvocationID, Tool, NormalizedArguments)
Cancel(InvocationID)
Shutdown

Worker -> Host
Ready(ExecutionID)
ToolStarted(InvocationID)
ToolResult(InvocationID, Result)
ToolFailed(InvocationID, FailureCode)
```

Worker 不接收 Provider 凭据、SQLite 路径、DataRoot 路径或 Agent transcript 路径。协议拒绝未知字段、未知版本、重复但内容不同的 InvocationID 和超过 runtime contract 上限的 frame。

Worker 进程退出、协议损坏或 ExecutionID 不匹配时，Client 关闭 Job Object，并把 active invocation 收敛为 interrupted 或 unknown。

ManagedProcess 不通过 execution worker 启动。ManagedProcessSupervisor 根据已经 durable 的 ManagedProcessID 直接创建独立 AppContainer 进程、ConPTY 和 Job Object，并把 ready、exit 和 terminal output 事件回报给 ManagedProcessCoordinator。

## 10. Windows 隔离

### 10.1 AppContainer identity

Execution SandboxProcess 的 AppContainer profile name 由 `AgentID + ExecutionID` 稳定派生。ManagedProcess profile name 由 `AgentID + ManagedProcessID` 稳定派生。相同 activation 重试得到相同名称，不同运行时使用不同 SID。来源 ExecutionSecuritySnapshot 中的 Project 与外部路径范围转换为对应 AppContainer SID 的显式 ACL：

- ReadRoots 只授予读取和遍历；
- WriteRoots 只在声明目录授予创建、修改、删除和重命名所需权限；
- Sandbox 临时目录只允许宿主和该 SID 访问；
- DataRoot、数据库、配置和其他 execution 临时目录不授予访问权限。

Praxis 按 ExecutionID 或 ManagedProcessID 记录自己添加的 ACE。execution 或 ManagedProcess 结算后删除对应 ACE 和 AppContainer profile；启动恢复会清理没有 active owner 的残留项。不得删除或重写用户已有 ACL 条目。

### 10.2 Process creation

SandboxProcess 按以下顺序启动：

1. 创建 IPC pipes 和 execution 临时目录；
2. 创建 Job Object 并设置 `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`；
3. 使用 `PROC_THREAD_ATTRIBUTE_SECURITY_CAPABILITIES` 配置 AppContainer；
4. 使用显式 handle list 和最小环境创建 suspended worker；
5. 将 worker 分配到 Job Object；
6. resume worker 并完成 SandboxHello；
7. worker ready 后才把 execution 标记为 running。

Worker 启动的命令自动继承同一个 AppContainer 和 Job Object。`run_command` 使用 Windows process API 直接传 executable 与 arguments，不经 `cmd.exe` 或 PowerShell。允许的 executable 规则约束直接启动入口；所有后代仍受相同文件、网络和进程生命周期边界限制。

ManagedProcessSupervisor 使用独立的创建流程：

1. durable 创建 ManagedProcess(starting)；
2. 创建 ManagedProcess 专属 AppContainer profile、ACL、ConPTY 和 Job Object；
3. 使用结构化 CommandSnapshot 直接创建 suspended process；
4. 将进程分配到 ManagedProcess Job Object 后 resume；
5. 进程达到 ready 条件后持久化 ManagedProcess(running)；
6. attach/detach 只增减 ConPTY 订阅；
7. stop 时先发送 Ctrl+C 或工具声明的 graceful signal；
8. grace deadline 到期后关闭 Job Object；
9. 持久化 settlement，再清理 profile、ACL 和临时目录。

Windows 进程不能依靠从 execution Job Object 移除来实现长期运行。`start_managed_process` 必须在 CreateProcess 之前确定独立 ownership 和 Job Object。

### 10.3 Network

AppContainer 不授予 `internetClient`、`privateNetworkClientServer` 或其他网络 capability。Sandbox worker、`run_command`、ManagedProcess 及其子进程都没有工具网络访问能力。ProviderClient 保持在可信主进程中。

### 10.4 Path enforcement

ToolBroker、worker 和 ManagedProcessSupervisor 都执行路径校验。进程创建前从授权根目录解析 executable 与 working directory，打开后检查最终路径仍位于对应根目录，并拒绝借助 symlink、junction 或 reparse point 逃离范围。

操作系统 ACL 是最终强制边界；字符串前缀判断不能作为路径授权依据。

## 11. 工具实现

首组工具：

| 工具 | 执行方式 | 关键约束 |
|---|---|---|
| `read_file` | worker 内结构化文件读取 | 只读根、最终路径校验、bounded output |
| `list_dir` | worker 内目录枚举 | 只读根、不跟随越界 reparse point |
| `search_text` | worker 内结构化搜索 | 只读根、取消传播、bounded output |
| `write_file` | worker 内文件写入 | 写根、明确 replace 语义、结果 receipt |
| `run_command` | worker 创建直接子进程 | executable、arguments、working directory 分离 |
| `start_managed_process` | Supervisor 创建独立进程运行时 | 独立 identity、ConPTY、Job Object 和 approval |
| `stop_managed_process` | Coordinator 请求 graceful stop | Session 归属、当前状态和调用权限 |
| `send_process_input` | Supervisor 写入 ConPTY | Session 归属、bounded input、调用权限 |
| `read_process_output` | Supervisor 读取 bounded buffer | Session 归属、输出截断标记 |

工具参数先解码到具体 struct，拒绝未知字段，再生成规范化 JSON。ArgumentsDigest 计算内容为：

```text
SHA-256(protocolVersion + toolName + canonicalArguments)
```

## 12. 前端与事件

前端提供 Agent 安全策略编辑和逐次工具审批，不在普通输入框旁提交 sandbox mode。

公开事件：

```text
AgentSecurityPolicyChanged
ToolApprovalRequested
ToolInvocationStarted
ToolInvocationSettled
ManagedProcessStarted
ManagedProcessOutput
ManagedProcessSettled
```

审批界面展示 Tool、规范化参数、文件范围、executable、working directory 和 ArgumentsDigest 的短指纹。Approve/Deny 按钮提交稳定 RequestID，重复点击返回既有决定。

Session 进程面板展示 ManagedProcess 状态和来源 execution。Attach 打开终端并订阅 ConPTY 输出，Detach 只关闭订阅，Stop 请求终止真实进程。前端不保存 PID、Job handle 或权威进程状态。

## 13. Composition 与失败策略

启动时 compose 完成：

1. 定位并验证 sandbox worker；
2. 检查 Windows 隔离 API；
3. 创建 sandbox launcher、tool executor 和 ManagedProcessSupervisor；
4. 创建 ManagedProcessCoordinator 和 ToolBroker；
5. 注入 AgentRuntime；
6. 完成一次无权限扩大能力的 sandbox self-check。

worker 缺失、校验失败、AppContainer 不可用或 self-check 失败时，应用把 sandbox 标记为 unavailable。需要本地工具的 AgentExecution activation 返回稳定错误；系统不能改用宿主文件 API 或 `os/exec` 执行请求。

## 14. 实施顺序

### 阶段 1：领域与存储

- 实现 AgentSecurityPolicy、ExecutionSecuritySnapshot 和 ToolInvocation；
- 实现 Session 拥有的 ManagedProcess 状态机；
- 实现 policy revision 与 snapshot resolver；
- 建立 SQLite 表、repository 和事务不变量；
- 更新 Agent 与 execution command DTO。

### 阶段 2：ToolBroker

- 实现 ToolCatalog、参数规范化和 digest；
- 实现 ToolBroker、approval command 和 ToolInvocation recovery；
- 使用仅用于测试的 fake ToolExecutor 验证状态机；
- 未接入 Windows sandbox 前，生产 compose 不暴露本地工具。

### 阶段 3：Model/tool loop

- Provider adapter 输出完整 provider-neutral ToolCall；
- runtime 持久化 tool request 和 result；
- 支持顺序多轮 model/tool loop；
- 接入 execution cancellation 和 deadline。

### 阶段 4：Windows sandbox

- 构建并打包 sandbox worker；
- 实现 AppContainer profile、ACL、Job Object 和 IPC；
- 实现结构化文件工具与 run_command；
- 实现 ManagedProcess 独立 profile、ConPTY 和 Job Object；
- 完成生产 compose 的 fail-closed 接线。

### 阶段 5：前端与恢复

- 实现 Agent policy 编辑；
- 实现 approval 事件和决定命令；
- 展示 ToolInvocation 状态；
- 实现 Session 进程面板、terminal attach/detach 和 stop；
- 完成应用重启、worker crash 和 execution cancel 的恢复测试。

## 15. 验证门禁

### 15.1 领域测试

- execution restrictions 不能扩大 Agent policy；
- approval 不能放行 snapshot 外的操作；
- policy ExpectedRevision 冲突不会覆盖；
- 同一 provider tool call 的参数变化产生 conflict；
- policy 缩小时 active execution 被取消；
- ManagedProcess 来源 execution 和 invocation 必须属于同一 Session；
- `run_command` 不能转换为 ManagedProcess。

### 15.2 ToolBroker 测试

- 未授权工具、路径和 executable 在启动 worker 前被拒绝；
- approval 精确绑定 InvocationID 与 ArgumentsDigest；
- durable decision 先于 waiter 唤醒；
- cancellation 传播到 ToolExecutor；
- unknown 副作用不会自动重试；
- `start_managed_process` 在创建 OS 进程前 durable 建立 ownership。

### 15.3 Windows 集成测试

- 可以读取 ReadRoot，不能读取未授权目录；
- 可以写入 WriteRoot，不能写入只读根；
- junction 和 reparse point 不能逃离授权根；
- SandboxProcess 不能读取 DataRoot 和 Provider 配置；
- SandboxProcess 不能访问网络；
- `run_command` 的子进程自动进入 Job Object；
- 关闭 Job Object 后 worker 和全部子进程终止；
- ManagedProcess 使用独立 Job Object，来源 execution 结算后仍可运行；
- detach terminal 不停止 ManagedProcess；
- stop 先尝试 graceful shutdown，随后保证关闭 Job Object；
- ManagedProcess 不能访问来源 snapshot 之外的路径或网络；
- 一个 execution 不能访问另一个 execution 的临时目录；
- worker crash 后 invocation 和 execution 收敛到稳定状态。

### 15.4 端到端测试

- Provider tool call 经 ToolBroker 返回 tool result 并继续下一 model turn；
- 需要审批的调用在 durable decision 前不执行；
- 应用重启不重复执行结果未知的工具；
- 应用退出终止 ManagedProcess，重启后将未结算记录收敛为 interrupted；
- sandbox unavailable 时不会发生宿主侧副作用。

## 16. 完成条件

实现同时满足以下条件才允许在生产 compose 中启用本地工具：

1. 前端不能为单次 execution 任意指定 sandbox 或 approval mode；
2. AgentSecurityPolicy 和 ExecutionSecuritySnapshot 均已持久化并经过 subset 校验；
3. 所有本地工具只能通过 ToolBroker；
4. ToolInvocation、approval 和 ManagedProcess 可以审计并在重启后收敛；
5. Windows worker 通过文件、网络、进程终止和跨 execution 隔离测试；
6. ManagedProcess 可以独立挂载、分离、优雅停止并由 Job Object 保证最终回收；
7. sandbox 初始化失败时没有宿主执行回退路径。
