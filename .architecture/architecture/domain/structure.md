# Praxis 领域结构

> 本文定义 Project、Session、Agent、AgentExecution 和 Session 运行资源的持久化关系、ownership、上下文边界和执行身份。
> 领域源码目录和文件职责见 [`README.md`](README.md)。
> 状态机与咨询流程见 [`orchestration/README.md`](../orchestration/README.md)，Workflow 编排见 [`workflow/README.md`](../workflow/README.md)，持久化事实源见 [`storage/README.md`](../storage/README.md)。

## 1. 持久化关系

```text
Project
└── Session (1..N)
    └── Agent (1..N)
        └── AgentExecution (1..N)

Session
├── SessionContextEntry (1..N)
└── ManagedProcess (0..N)

Agent
└── AgentTranscriptEntry (1..N)

AgentExecution
└── ToolInvocation (0..N)
```

`Project → Session → Agent → AgentExecution` 是协作主体层次。Session 直接拥有 Agent，Agent 直接拥有 AgentExecution。ManagedProcess 是 Session 拥有的运行资源，不构成新的 Agent 或上下文层次。

Workflow 是独立模块。它通过稳定 ID 引用并编排 Session 下的 Agent 与 AgentExecution，不改变本节的 ownership。

| 对象 | 承载 | 不承载 |
|---|---|---|
| `Project` | 用户选择的项目名称、路径、生命周期和 Session 索引 | Agent 状态、执行状态、Session 上下文正文 |
| `Session` | Project 归属、会话生命周期、SessionContext 索引和执行目录 | Agent 私有 transcript、单次执行的临时状态 |
| `Agent` | Session 内稳定的参与者身份、角色、Profile、安全策略、状态和 transcript 引用 | goroutine、Provider stream、单次执行输入 |
| `AgentExecution` | 一次执行的身份、父执行、上下文版本、输入快照、状态、结果和失败码 | 运行时 actor、逐 delta 流、其他执行的可变状态 |
| `ToolInvocation` | execution 下已准入的规范化工具调用、审批、状态、结果引用和失败码 | Provider wire event、具体工具实现、OS 进程句柄和 transcript 正文 |
| `SessionContextEntry` | 已提交到 Session 共享上下文的单条内容及版本 | 咨询过程中的中间消息和未批准输出 |
| `AgentTranscriptEntry` | 一个 Agent 实际收到或产生的完整私有交互记录 | 其他 Agent 的 transcript、未提交的 Session 事实 |
| `ManagedProcess` | Session 内显式启动的长期进程、来源 execution、命令快照、安全来源和生命周期 | Agent 身份、SessionContext、UI terminal attachment、OS 进程句柄 |

## 2. Project 与 Session

`Project` 是用户长期管理的项目目录。项目路径由用户选择。版本控制和文件操作能力按用户发起的具体操作现场验证。

`Session` 是 Project 下的一次长期协作上下文。一个 Project 可以拥有多个 Session。用户消息、Agent 消息、结论、决定和引用以上下文条目的形式进入 Session。

```text
Project
├── ProjectID
├── Name
├── Path
├── State
└── CreatedAt / UpdatedAt

Session
├── SessionID
├── ProjectID
├── State
└── CreatedAt / UpdatedAt
```

路径不是身份。Project 改名或移动时，稳定 ID 不变。每个 Session 只能属于一个 Project。

### 2.1 ManagedProcess

ManagedProcess 表示需要在来源 AgentExecution 结算后继续运行的受控进程，例如开发服务器、文件 watcher、调试器或交互式终端。它只能由已授权的 ToolInvocation 创建，创建后由 Session 持有。

```text
ManagedProcess
├── ManagedProcessID
├── SessionID
├── StartedByExecutionID
├── StartedByInvocationID
├── CommandSnapshot
├── Status / Outcome / FailureCode
└── CreatedAt / StartedAt / SettledAt
```

ManagedProcess 使用来源 execution 的 ExecutionSecuritySnapshot 建立自己的沙箱边界。命令、工作目录和安全边界在创建后不可扩大。前端通过临时 terminal attachment 读取输出或发送输入；attachment、ConPTY handle、PID 和 Job Object 都不持久化。

## 3. Agent 与 AgentExecution

`Agent` 是 Session 内稳定的参与者。Agent 可以承担主要工作、委派工作或独立咨询；具体行为由创建命令和执行输入决定。一个 Session 可以有多个 Agent。

`AgentExecution` 是 Agent 的一次完整执行周期。一次 execution 可以包含多个 model turn、tool call、暂停和保存点，但拥有唯一的 `ExecutionID`。同一 Agent 的多个 execution 顺序执行，执行之间不共享 Provider stream 或运行时临时状态。

```text
AgentExecution
├── ExecutionID
├── AgentID
├── SessionID
├── ParentExecutionID?       委派或咨询的来源执行
├── ContextRevision          启动时读取的 SessionContext 版本
├── ContextSelection         从该 revision 选入本次执行的条目或摘要
├── InputSnapshot            指令、模型和执行参数的不可变快照
├── ExecutionSecuritySnapshot 能力、沙箱和审批约束的不可变快照
├── Status
├── Outcome / FailureCode
└── CreatedAt / StartedAt / SettledAt
```

`SessionID` 可以作为冗余引用保存在 execution 中，用于数据库复合外键和一致性校验；它不改变 ownership：execution 始终由 Agent 拥有，Agent 始终由 Session 拥有。

## 4. 两种上下文

### 4.1 SessionContext

SessionContext 是 Session 级共享上下文，由追加写入的 `SessionContextEntry` 组成。每次提交产生单调递增的 `ContextRevision`。

```text
SessionContextEntry
├── SessionID
├── Revision
├── Kind: user_message | agent_message | accepted_conclusion | decision | reference
├── SourceExecutionID?
├── Content
└── CreatedAt
```

Agent 可以按 SessionID 查询上下文，并为 execution 选择完整上下文、指定条目或预算内摘要。一次 AgentExecution 必须固定自己读取的 revision 和 ContextSelection；执行过程中不能看到后来追加的条目，除非通过显式恢复或新的 execution 重新读取。

### 4.2 Agent transcript

每个 Agent 有一份独立 transcript，保存该 Agent 实际看到的用户消息、模型消息、工具调用、工具结果和 execution receipt。Transcript 是完整审计记录，不是 SessionContext 的自动镜像。

完整 transcript 不在 Agent 之间共享。只有经过显式提交的结论、决定或引用才会成为 SessionContextEntry。

## 5. 授权对象

| 对象 | 作用 | 可变性 |
|---|---|---|
| `AgentProfile` | 模型偏好和工作预设 | 可变；不等于权限 |
| `AgentSecurityPolicy` | Agent 的能力、沙箱和审批权限上限 | 带 revision；变更只通过 Agent 命令 |
| `ContextSelection` | 从固定 SessionContext revision 选入执行的内容 | 绑定 AgentExecution |
| `ExecutionSecuritySnapshot` | 本次执行的 CapabilityGrant、沙箱和审批约束 | 绑定 AgentExecution；创建后不可变 |

ContextSelection 和 ExecutionSecuritySnapshot 在 execution 创建时冻结。配置变化只影响之后创建的 execution，不能修改运行中的输入。

## 6. 领域不变量

1. 持久化协作主体关系是 `Project → Session → Agent → AgentExecution`；ManagedProcess 是 Session 运行资源，不改变该层次。
2. 每个 Session 恰好属于一个 Project；每个 Agent 恰好属于一个 Session；每个 execution 恰好属于一个 Agent。
3. 同一 Agent 同时最多有一个 active execution；同一 Session 下的不同 Agent 可以并发执行。
4. AgentExecution 读取的 `ContextRevision` 不可变；上下文追加必须产生新 revision，不能原地修改旧条目。
5. Agent transcript 只由所属 Agent 的唯一 writer 追加；其他 Agent 或 runtime 不得直接写入。
6. 咨询或委派的中间过程不得自动进入 SessionContext；只有显式提交的结果才能成为共享条目。
7. `ExecutionID` 是执行身份的唯一名称，所有 API、事件和 JSONL receipt 使用 `executionId`。
8. Project 路径和显示名称都不是关系身份，也不能作为外键。
9. 每个 ManagedProcess 恰好属于一个 Session，其来源 execution 和 invocation 必须属于同一 Session。
10. ManagedProcess 只能通过显式工具调用创建，不能由 `run_command` 的未退出子进程隐式产生。
11. 每个 ToolInvocation 恰好属于一个 AgentExecution；`ProviderToolCallID` 只在所属 execution 内参与幂等，不能作为全局身份。
