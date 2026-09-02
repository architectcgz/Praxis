# Domain 目录

> 本文定义 `backend/internal/core/domain` 的目标目录、Go 文件职责、领域包依赖和领域层边界。
> 领域对象关系见 [`structure.md`](structure.md)，应用用例布局见 [`directory-structure.md`](directory-structure.md)，系统分层见 [`system-architecture.md`](system-architecture.md)。

## 1. 领域层边界

`internal/core/domain` 只承载领域实体、值对象、领域事件、状态转换和不变量。每个子目录是独立 Go package；领域 package 可以依赖其他 domain package 和标准库，但不能依赖 `internal/application`、`app`、`contracts`、`internal/orchestration`、`internal/workflow`、`storage`、`providers`、`tools`、`agentruntime` 或 `compose`。

领域对象不持有 repository、数据库连接、Wails context、Provider client、runtime actor、goroutine、文件句柄或操作系统进程句柄。事务、跨聚合流程、外部副作用和持久化由 application、orchestration 和适配器负责。

命令的 RequestID、参数摘要和 durable receipt 属于 `internal/core/command` 的跨用例协议，不属于 `core/domain` 的业务对象。

## 2. 目录树

```text
backend/internal/core/domain/
├── foundation/
│   ├── errors.go            领域校验错误和状态转换错误
│   ├── events.go            DomainEvent 类型和快照
│   └── ids.go               所有稳定领域 ID 类型和生成函数
├── context/
│   ├── contentref.go        上下文内容引用
│   ├── contextmanifest.go   execution 使用的上下文清单
│   ├── helpers.go           context package 的校验和防御性复制
│   └── sessioncontext.go    SessionContext 追加条目和 revision
├── project/
│   ├── project.go            Project 实体和生命周期
│   └── helpers.go            project package 的路径规范化和校验
├── workspace/
│   ├── workspace.go          Workspace 实体、路径 revision 和生命周期
│   ├── workspacelease.go     Workspace 写租约
│   └── helpers.go            workspace package 的校验、事件和别名
├── session/
│   ├── session.go            Session 实体和生命周期
│   └── helpers.go            session package 的校验和别名
├── security/
│   ├── profile.go            Agent profile
│   ├── sandbox.go            Sandbox mode
│   ├── approval.go           approval mode 和 approval record
│   ├── granttemplate.go      工具、结果权限和默认授权模板
│   ├── security_policy.go    Agent policy 与 execution security snapshot
│   ├── capabilitygrant.go    capability grant、模型选择和资源限制
│   ├── policy.go             系统 Agent policy snapshot
│   └── helpers.go            security package 的校验和别名
├── execution/
│   ├── executionfailure.go   稳定 execution failure code
│   ├── execution.go          runtime execution snapshot
│   ├── agentexecution.go     AgentExecution、输入快照和生命周期
│   └── helpers.go            execution package 的校验、复制和别名
├── agent/
│   ├── agent.go              Agent 实体和 execution 生命周期
│   └── helpers.go            agent package 的校验、状态和别名
└── workflow/
    ├── artifacts.go          AgentResult、Briefing 和审核状态
    ├── delegation.go         Agent 间 DelegationRequest
    ├── orchestration.go      WaitCondition、ControlRequest 和 ContextDelivery
    ├── queuedwork.go         排队工作及其 execution 绑定
    ├── note.go               Note 实体
    └── helpers.go             workflow package 的校验、事件和别名
```

## 3. Package 依赖

```text
context      ─► foundation
project      ─► foundation
session      ─► foundation
security     ─► foundation
workspace    ─► foundation + security
execution    ─► foundation + context + security
agent        ─► foundation + execution + security
workflow     ─► foundation + context + execution + security
```

依赖箭头表示 package import 方向。`foundation` 不依赖其他 domain package；domain package 之间不能形成循环依赖。跨 package 的组合由 application 或 orchestration 完成，不能通过把状态写入移入某个 helper 文件来绕过 ownership。

## 4. Foundation

| 文件 | 用途 |
|---|---|
| `foundation/errors.go` | 定义统一的校验错误和非法状态转换错误，供领域对象报告稳定错误类别。 |
| `foundation/events.go` | 定义领域事件类型、事件身份、关联主体和不可变快照。 |
| `foundation/ids.go` | 定义 Project、Workspace、Session、Agent、Execution、Command、Workflow 和事件等稳定 ID 类型及生成函数。 |

## 5. Context

| 文件 | 用途 |
|---|---|
| `context/contentref.go` | 定义上下文内容引用的种类、路径、摘要、摘录和字节数，并校验引用边界。 |
| `context/contextmanifest.go` | 定义一次 execution 使用的 ContextManifest，保存摘要和来源引用。 |
| `context/sessioncontext.go` | 定义追加式 SessionContextEntry、内容种类和 revision 语义。 |
| `context/helpers.go` | 提供字段校验、错误包装和切片防御性复制。 |

## 6. Project、Workspace 与 Session

| Package | 文件 | 用途 |
|---|---|---|
| `project` | `project.go` | 定义 Project 身份、名称、规范化路径、Workspace 引用、状态和归档转换。 |
| `project` | `helpers.go` | 规范化绝对路径并提供 Project 校验和状态转换错误。 |
| `workspace` | `workspace.go` | 定义 Workspace 类型、路径 revision、可用状态以及移动、不可用和归档转换。 |
| `workspace` | `workspacelease.go` | 定义 Workspace 写租约的获取、校验和释放。 |
| `workspace` | `helpers.go` | 提供 Workspace 校验、领域事件构造和安全策略别名。 |
| `session` | `session.go` | 定义 Session 的 Project/Workspace 归属、目标摘要、状态和归档转换。 |
| `session` | `helpers.go` | 提供 Session 校验、错误构造和 foundation 别名。 |

Project、Workspace 和 Session 只表达资源身份与领域状态，不执行目录创建、移动、删除或其他文件系统副作用。

## 7. Security

| 文件 | 用途 |
|---|---|
| `security/profile.go` | 定义 Agent profile 及其合法值。 |
| `security/sandbox.go` | 定义 SandboxMode，以及工作区写入和网络能力判断。 |
| `security/approval.go` | 定义 approval 来源、模式和用户/策略批准记录。 |
| `security/granttemplate.go` | 定义工具名称、工作区访问级别、结果权限和默认授权模板。 |
| `security/security_policy.go` | 定义 AgentSecurityPolicy、执行限制、SandboxConstraints、ApprovalRule 和 ExecutionSecuritySnapshot。 |
| `security/capabilitygrant.go` | 定义模型选择、资源限制、CapabilityGrantSpec 和不可变 CapabilityGrant，并提供路径、工具和结果权限判断。 |
| `security/policy.go` | 定义系统级 AgentPolicySnapshot 及按 profile 查找默认授权模板的行为。 |
| `security/helpers.go` | 提供安全策略字段校验、错误构造和 foundation 别名。 |

安全策略定义权限上限和执行快照语义；密钥解析、Provider 调用和操作系统隔离属于外层适配器。

## 8. Execution 与 Agent

| Package | 文件 | 用途 |
|---|---|---|
| `execution` | `executionfailure.go` | 定义跨 SQLite、JSONL、runtime 和 binding 使用的稳定失败码。 |
| `execution` | `execution.go` | 定义不可变 RuntimeExecutionSnapshot，冻结 runtime 所需的技术约束。 |
| `execution` | `agentexecution.go` | 定义 ExecutionReason、Status、Outcome、ContextSelection、ExecutionInputSnapshot 和 AgentExecution 生命周期，包括创建、启动、结算和输入正文清理。 |
| `execution` | `helpers.go` | 提供 execution 字段校验、错误包装、快照复制和安全策略别名。 |
| `agent` | `agent.go` | 定义 Agent 身份、Session 归属、Profile、policy revision、状态和 Start/Resume/Pause/Settle/Close 转换。 |
| `agent` | `helpers.go` | 提供 Agent 状态校验、执行结果校验和 domain ID 别名。 |

`AgentExecution` 持有一次执行的输入和安全快照；`Agent` 不持有单次执行输入，也不持有 runtime actor 或 Provider stream。

`execution` 领域包不实现前端传输重试、退避、请求确认等待或 UI 状态。它只校验和转换一次已接受 execution 的生命周期与 outcome；命令重试由客户端和 application command admission 通过稳定 `RequestID` 协作完成。

## 9. Workflow

| 文件 | 用途 |
|---|---|
| `workflow/artifacts.go` | 定义 AgentResult、Briefing、审核状态以及提交、批准、拒绝状态转换。AgentResult 是结构化执行结果；Briefing 是面向指定目标 Agent 的内部简报投递载荷。 |
| `workflow/delegation.go` | 定义 Agent 间委派请求、目标、状态和批准/拒绝/取消转换。 |
| `workflow/orchestration.go` | 定义 WaitCondition、AgentControlRequest 和 ContextDelivery 的状态、目标和幂等转换。 |
| `workflow/queuedwork.go` | 定义独立排队工作、任务正文、execution 绑定和结算转换。 |
| `workflow/note.go` | 定义与 Session 关联的 Note 内容、范围和生命周期。 |
| `workflow/helpers.go` | 提供 Workflow 对象共用的校验、事件构造、切片复制和 domain package 别名。 |

Workflow domain 对象只表达流程事实和状态转换。Workflow coordinator、跨 Agent 投递、调度和恢复由上层 orchestration 负责。

## 10. 领域对象规则

1. 构造函数创建满足最小不变量的对象；`Validate` 检查完整持久化状态。
2. 状态转换方法只修改所属对象并返回领域事件或领域错误，不访问 repository 或执行外部副作用。
3. `Snapshot` 和相关复制函数返回防御性副本，调用方不能通过切片或 map 别名修改领域状态。
4. 所有 execution 输入、安全快照、上下文 revision 和 workflow 引用在对象创建后按定义保持不可变。
5. 领域层不把 Provider stream、transcript delta、goroutine、进程句柄或 UI 投影建模为持久化领域身份。
