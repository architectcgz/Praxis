# Agent 沙箱架构

> 本文定义 Agent 的长期安全边界、AgentExecution 的执行安全快照、工具授权、用户审批和操作系统隔离。
> 领域 ownership 见 [`architecture/structure.md`](architecture/structure.md)，执行生命周期见 [`architecture/agent-runtime-model.md`](architecture/agent-runtime-model.md)，Workflow 边界见 [`architecture/workflow.md`](architecture/workflow.md)，代码落地见 [`sandbox-implementation.md`](sandbox-implementation.md)。

## 1. 模型定位

Agent 始终受自身安全策略约束。Agent 是持久化身份，不对应一个持续存活的操作系统进程；AgentExecution 是 Agent 的一次执行，并在 activation 时创建实际的沙箱进程。

```text
Project
└── Session
    ├── Agent
    │   ├── AgentSecurityPolicy
    │   └── AgentExecution
    │       ├── ExecutionSecuritySnapshot
    │       ├── ToolInvocation[]
    │       └── SandboxProcess             临时运行资源
    └── ManagedProcess[]                   长期受控进程
```

`AgentSecurityPolicy` 是 Agent 拥有的值对象。`ExecutionSecuritySnapshot` 和 `ToolInvocation` 由 AgentExecution 拥有。`SandboxProcess` 是可以按持久化状态重建的运行时资源，不是领域实体，也不形成新的 ownership 层次。`ManagedProcess` 是 Session 持有的长期运行资源，可以在来源 AgentExecution 结算后继续运行。

Agent 只能在 AgentExecution 中通过 ToolInvocation 发起或控制外部副作用。模型产生的所有工具请求都必须关联唯一的 `ExecutionID` 并经过 ToolBroker。ManagedProcess 创建后由 Session 和 ManagedProcessSupervisor 持有，不表示 Agent 在 execution 之外继续执行。

## 2. AgentSecurityPolicy

`AgentSecurityPolicy` 定义 Agent 长期有效的权限上限：

```text
AgentSecurityPolicy
├── Revision
├── CapabilityPolicy
│   ├── AllowedTools
│   ├── ReadScopes
│   ├── WriteScopes
│   ├── AllowedExecutables
│   └── ExternalActions
├── SandboxPolicy
│   ├── EnvironmentPolicy
│   └── ChildProcessPolicy
└── ApprovalPolicy
    └── ApprovalRule[]
```

三部分分别表达：

| 部分 | 职责 |
|---|---|
| `CapabilityPolicy` | Agent 可以请求哪些工具、路径、命令和外部操作 |
| `SandboxPolicy` | 操作系统必须强制执行的文件、进程、网络和环境边界 |
| `ApprovalPolicy` | 已经位于允许范围内的操作是否仍需逐次取得用户批准 |

安全策略不保存 Provider 密钥、进程句柄或平台沙箱对象。操作系统隔离实现根据策略生成运行时配置，不反向成为领域模型的一部分。

### 2.1 文件范围

Agent policy 使用稳定的语义范围描述 Project 内路径，并允许记录用户显式选择的外部根目录。Project 内范围使用相对路径；execution activation 时基于当时的 Project 路径解析为规范化绝对根目录。

```text
ReadScopes
├── ProjectRoot
├── ProjectRelativePath("docs")
└── ExternalRoot(user-selected path)
```

路径授权必须区分读和写。拥有父目录的读取权限不自动获得写入、删除、重命名或执行权限。

### 2.2 命令与网络

命令授权至少约束可执行文件、参数规则、工作目录和子进程行为。`run_command` 接收结构化的 executable 与 arguments；调用 shell 必须是独立、明确授权的能力。

SandboxProcess 不具备网络能力。ProviderClient 使用的模型网络位于可信主进程中，不属于工具网络，也不向沙箱暴露 Provider 凭据。需要访问外部服务的产品能力必须通过可信主进程提供的结构化工具和独立授权实现，不能向任意命令开放网络。

### 2.3 审批规则

Approval 只决定一个已允许操作是否可以执行，不能增加 CapabilityPolicy 或放宽 SandboxPolicy。策略可以按工具、操作种类、路径、可执行文件和网络目标要求审批。

不在 AgentSecurityPolicy 内的请求直接拒绝，不创建可用于扩大权限的 approval。

## 3. ExecutionSecuritySnapshot

创建 AgentExecution 时，Orchestrator 根据系统安全基线、Agent 当前策略和 execution 请求的限制生成不可变快照：

```text
restrict(
    SystemSecurityBaseline,
    AgentSecurityPolicy,
    ExecutionRequestedRestrictions,
) -> ExecutionSecuritySnapshot
```

```text
ExecutionSecuritySnapshot
├── AgentPolicyRevision
├── CapabilityGrant
├── SandboxConstraints
├── ApprovalRules
├── ResolvedReadRoots
├── ResolvedWriteRoots
├── AllowedExecutables
└── EnvironmentConstraints
```

execution 请求只能缩小 Agent 的权限。请求超出 AgentSecurityPolicy 时整个创建命令失败，不能静默扩大，也不能依靠后续 approval 通过。

快照与 AgentExecution 一起持久化。Activation、恢复、审计和 ToolBroker 校验只读取该快照，不重新解释当前 Agent policy。运行中的配置变化不能修改快照。

### 3.1 策略变更

修改 AgentSecurityPolicy 必须增加 `Revision`：

- 扩大权限只影响之后创建的 AgentExecution；
- 缩小或撤销权限时，Orchestrator 取消该 Agent 当前 active execution，并终止其 SandboxProcess；
- 已完成的 execution 保留原始 ExecutionSecuritySnapshot，用于审计和恢复判断。

## 4. 运行时结构

```text
可信 Praxis 主进程
├── AgentOrchestrator
│   ├── ToolBroker
│   └── ManagedProcessCoordinator
├── AgentRuntime(AgentID)
├── ProviderClient
├── SandboxProcess(ExecutionID)
│   └── run_command child processes
└── ManagedProcessSupervisor
    └── ManagedProcess runtime
```

`AgentRuntime` 负责 activation、模型循环、取消和结算，但不能直接执行有副作用的工具。ProviderClient 位于可信主进程中，模型凭据只在该边界内使用。

ToolBroker 是工具执行的唯一入口，负责：

1. 校验 Agent、AgentExecution 和 execution 状态；
2. 规范化工具参数并生成稳定摘要；
3. 校验 CapabilityGrant 和 SandboxConstraints；
4. 根据 ApprovalRules 创建或读取审批决定；
5. 把临时工具派发到对应 SandboxProcess，或把长期进程命令交给 ManagedProcessCoordinator；
6. 持久化结果并追加 Agent transcript receipt。

ToolBroker 的领域校验与 SandboxProcess 或 ManagedProcess runtime 的操作系统限制必须同时存在。受限进程不能因 Broker 漏判而获得快照之外的文件、网络、进程或凭据访问能力。

### 4.1 Execution SandboxProcess 生命周期

每个 AgentExecution 使用独立的 SandboxProcess：

```text
AgentExecution(starting)
    -> 创建 execution 临时目录
    -> 解析并校验安全快照中的路径根
    -> 创建受限 OS 身份和进程
    -> 把 worker 加入 execution 的进程生命周期边界
    -> AgentExecution(running)
    -> 执行 tool calls
    -> AgentExecution settling
    -> 终止 execution Job Object 中的全部进程
    -> 清理临时目录
```

不同 AgentExecution 不共享平台沙箱身份、进程、环境变量、当前目录、临时目录、打开的句柄或内存状态。平台沙箱身份由 `AgentID + ExecutionID` 派生，使操作系统文件权限与该 execution 的 ExecutionSecuritySnapshot 一致。execution 结算后删除对应身份和由 Praxis 添加的临时 ACL。

结构化文件工具不启动外部进程。`run_command` 启动的编译器、测试程序和构建工具可以继续创建子进程，这些进程必须自动进入 execution 的同一个 Job Object。取消、超时、结算或应用退出时关闭 Job Object，确保该 execution 启动的全部进程停止。Job Object、子进程列表和进程树都不是领域对象，也不持久化。

### 4.2 ManagedProcess 生命周期

需要长期运行或前台挂载的命令使用独立的 `start_managed_process` 工具：

```text
ToolInvocation(start_managed_process)
    -> 校验来源 ExecutionSecuritySnapshot
    -> 用户审批（按 ApprovalRules）
    -> 创建 ManagedProcess(starting)
    -> 创建独立 AppContainer identity、ConPTY 和 Job Object
    -> ManagedProcess(running)
    -> 返回 ManagedProcessID
    -> 来源 AgentExecution 可以结算

用户或后续 AgentExecution
    -> attach / detach terminal
    -> send_process_input
    -> stop_managed_process
    -> graceful shutdown
    -> 超时后关闭 ManagedProcess Job Object
    -> ManagedProcess(settled)
```

ManagedProcess 必须从创建时就进入自己的 Job Object，不能先作为 `run_command` 子进程启动后再脱离 execution。Terminal attachment 只表示 UI 或调用方当前连接到 ConPTY，不拥有进程生命周期；detach 不停止进程。

LLM 可以请求 graceful shutdown，但 ManagedProcessSupervisor 对最终回收负责。Session 关闭、来源权限被撤销或应用退出时，Supervisor 先请求优雅退出，再关闭对应 Job Object。ManagedProcess 不跨 Praxis 应用重启继续运行。

## 5. ToolInvocation 与审批

每次工具请求形成 AgentExecution 下的持久化记录：

```text
ToolInvocation
├── InvocationID
├── ExecutionID
├── Tool
├── NormalizedArguments
├── ArgumentsDigest
├── ApprovalState
├── Status
├── ResultReference?
└── CreatedAt / StartedAt / SettledAt
```

用户审批必须绑定 `InvocationID` 和 `ArgumentsDigest`。审批后参数发生任何变化都产生新的 ToolInvocation，并重新进行授权和审批。

```text
requested
├── denied
├── awaiting_approval -> approved -> running -> succeeded | failed
└── running -> interrupted | unknown
```

进程退出时仍处于 `running` 的非幂等操作收敛为 `unknown`，恢复时不能自动重新执行。读取类或具备明确幂等键的工具只有在工具契约允许时才能重试。

## 6. 操作系统隔离

Windows 沙箱实现必须组合以下边界：

- AppContainer 或满足同等文件与对象隔离保证的受限身份；
- 独立 Job Object，分别管理 execution 临时命令和每个 ManagedProcess 的进程集合；
- 显式文件访问范围，不继承宿主进程的宽泛目录权限；
- 不授予网络 capability；
- 最小环境变量集合和句柄继承集合。

Job Object 只负责进程生命周期，不能单独作为文件或网络安全边界。AgentSecurityPolicy 和 ExecutionSecuritySnapshot 不定义 CPU 或内存配额。Execution、model call 和 tool call 的 deadline 由执行超时模型负责，工具输出的有界读取由 runtime contract 负责。

路径访问必须从已授权根目录的安全句柄开始解析，拒绝通过符号链接、junction、reparse point、大小写变化或路径规范化逃离根目录。命令工作目录和所有文件参数使用同一套路径解析规则。

SandboxProcess 不能访问：

- Praxis DataRoot；
- SQLite 数据库和备份；
- Provider 凭据和应用配置密钥；
- 其他 AgentExecution 的临时目录和进程；
- 未出现在 ExecutionSecuritySnapshot 中的 Project 或外部路径。

ManagedProcess 使用来源 ExecutionSecuritySnapshot 建立独立 AppContainer identity，其文件和网络边界不得超过该 snapshot。ManagedProcess 不能访问 execution 临时目录、其他 ManagedProcess runtime 或 Praxis 权威存储。

## 7. Workflow 边界

Workflow 可以在启动 AgentExecution 时提出更严格的执行限制，也可以派发暂停和取消命令。Workflow 不能：

- 修改 AgentSecurityPolicy；
- 请求超过 AgentSecurityPolicy 的 execution 权限；
- 直接批准 ToolInvocation；
- 绕过 ToolBroker 调用工具；
- 复用已结算 execution 的 SandboxProcess。

Workflow 需要更高权限的 Agent 时，必须等待用户通过核心命令修改 AgentSecurityPolicy，再创建新的 AgentExecution。

Workflow 暂停、取消或完成不自动停止 Session 拥有的 ManagedProcess。需要停止时必须通过显式核心命令或已授权的 `stop_managed_process` 工具执行。

## 8. 恢复与审计

SandboxProcess 不参与持久化恢复。应用重启后，Orchestrator 根据 AgentExecution、ExecutionSecuritySnapshot、ToolInvocation 和 transcript receipt 收敛状态：

- 未启动工具可以按原快照重新派发；
- 已完成工具直接使用持久化结果；
- 执行中断且无法证明结果的工具标记为 `unknown`；
- 恢复 execution 时创建新的 SandboxProcess，不复用旧进程状态。

ManagedProcessSupervisor 与 Praxis 主进程共同退出，Job Object 保证其托管进程停止。启动恢复把数据库中仍处于 `starting`、`running` 或 `stopping` 的 ManagedProcess 标记为 `interrupted`，清理对应 AppContainer profile 和 ACL，不自动重启命令。

安全审计必须能够从 `ExecutionID` 追溯 Agent policy revision、有效快照、每次工具参数摘要、approval 决定、执行结果和 failure code；从 `ManagedProcessID` 必须能够追溯来源 ExecutionID、InvocationID 和安全快照。

## 9. 不变量

1. 每个 Agent 必须拥有一个带 revision 的 AgentSecurityPolicy。
2. Agent 只能在 AgentExecution 中通过 ToolInvocation 发起或控制外部副作用。
3. 每个 AgentExecution 必须持久化一个不可变的 ExecutionSecuritySnapshot。
4. ExecutionSecuritySnapshot 不能超过所属 Agent 的安全策略和系统安全基线。
5. 所有工具调用必须同时通过 capability、sandbox 和 approval 校验。
6. Approval 不能扩大 CapabilityGrant 或放宽 SandboxConstraints。
7. `run_command` 及其子进程归所属 execution；execution 结算后必须终止对应 Job Object 中的全部进程。
8. Provider 凭据、Praxis DataRoot 和权威存储不能进入 SandboxProcess 或 ManagedProcess runtime。
9. Workflow 只能缩小 execution 权限，不能修改或绕过 Agent 的安全边界。
10. SandboxProcess 是临时运行资源，不是领域实体或持久化 ownership 层次。
11. ManagedProcess 必须通过显式工具创建并由 Session 持有，不能从 execution Job Object 中分离产生。
12. LLM 可以请求停止 ManagedProcess，但最终回收责任始终属于 ManagedProcessSupervisor。
