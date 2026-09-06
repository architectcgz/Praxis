# Praxis 产品与技术边界

> 本文定义 Praxis 的目标产品、领域关系、共享上下文、运行方式、持久化分工和交付范围。

领域关系见 [`domain/structure.md`](domain/structure.md)，Workflow 模块见 [`workflow/README.md`](workflow/README.md)，系统分层见 [`system-architecture.md`](system-architecture.md)，源码布局见 [`directory-structure.md`](directory-structure.md)，执行时序见 [`orchestration/README.md`](orchestration/README.md)。

## 1. 产品定位

Praxis 是面向个人学习与编程实践的多 Agent 桌面工作台。用户在一个 Project 中建立多个 Session；每个 Session 是一段可持续恢复的协作上下文，Session 内可以运行多个角色不同的 Agent。

产品提供：

- Project 与 Session 管理；
- Session 级共享上下文和 Agent 级私有 transcript；
- 多个 Agent 的主任务、委派和独立咨询协作；
- 可复用的 Workflow，用于编排 Session 内的 Agent 和 AgentExecution；
- 可审阅的能力授权、执行状态和结构化结论；
- 可暂停、恢复、排队、取消并在进程重启后恢复的 AgentExecution；
- Session 内可挂载、交互和显式停止的长期受控进程；
- 用户可控制的文件和工具能力；
- 与 Agent transcript 分离的长期 Note。

用户始终是协作的授权者。Profile 只提供默认行为，实际能力由 AgentSecurityPolicy、ExecutionSecuritySnapshot、沙箱和审批共同决定。

## 2. 产品领域模型

```text
Project (1..N Session)
└── Session (1..N Agent)
    ├── Agent (1..N AgentExecution)
    └── ManagedProcess (0..N)

Session
└── SessionContextEntry (append-only)
```

### 2.1 Project

Project 是用户选择的项目目录和 Session 索引。Project 保存名称、规范化路径、生命周期状态和时间戳。版本控制与文件操作能力在用户发起具体操作时由后端验证。

### 2.2 Session

Session 是 Project 下的协作上下文容器。

Session 的共享事实通过追加式 `SessionContextEntry` 保存。消息、结论、决定和引用可以进入共享上下文；Agent 的完整交互仍保存在各自 transcript 中。

Session 还拥有显式创建的 ManagedProcess。ManagedProcess 可以在来源 AgentExecution 结算后继续运行，用于开发服务器、watcher、调试器和交互式终端；它不进入 SessionContext，也不构成新的 Agent。

### 2.3 Agent 与 AgentExecution

- `Agent` 是 Session 内稳定的参与者身份，一个 Session 可以有多个 Agent，分别承担主要工作、委派任务或独立咨询。
- `AgentExecution` 是 Agent 的一次执行周期，使用 `ExecutionID` 作为唯一身份。
- 一个 execution 读取固定的 SessionContext revision 和 ContextSelection，并冻结自己的指令、模型和 ExecutionSecuritySnapshot。
- AgentExecution 可以通过 `ParentExecutionID` 表示委派或咨询来源，但不改变其 Agent ownership。

## 3. 共享上下文与私有询问

普通执行读取 SessionContext 的一个 revision，并将用户输入、模型输出和工具结果写入所属 Agent 的 transcript。只有被明确提交的内容才追加为新的 SessionContextEntry。

当用户在 Agent 写代码期间需要询问而又不希望污染主上下文时，系统执行以下流程：

```text
SessionContext revision 42
        │
        ├── 主 AgentExecution E1 暂停，保留 revision 42
        │
        └── 咨询 AgentExecution E2 读取 revision 42
                └── 私有 transcript，不写入 SessionContext

E2 结算出结论
        │
        └── 用户或编排命令显式提交 ContextEntry，产生 revision 43

E1 恢复并读取 revision 43
```

咨询问题、模型中间推理和未采纳答案不会自动共享。提交结论时使用期望 revision，若期间已有其他提交则拒绝隐式覆盖并要求重新审阅。咨询可以由临时 Agent 执行，也可以由已有 Agent 创建独立 execution。

## 4. 协作工作流

1. 用户创建 Project 并选择项目路径。
2. 用户在 Project 下创建 Session。
3. Session 创建首个 Agent；后续可按需创建委派 Agent 或咨询 Agent。
4. 每次用户输入、委派、咨询或恢复都创建新的 AgentExecution，并记录读取的上下文 revision。
5. Agent 在私有 transcript 中工作；需要共享的内容通过结构化结果或显式 ContextEntry 提交。
6. 文件操作只允许发生在本次 ExecutionSecuritySnapshot 的路径范围内，并受沙箱和审批规则约束。
7. Session 可以暂停和恢复；暂停冻结当前执行的输入快照，不删除 Session、Agent 或 transcript。
8. 长期进程必须通过独立工具显式创建；普通 `run_command` 在所属 execution 结束时停止。

## 5. 能力与授权

| 对象 | 作用 |
|---|---|
| `ContextSelection` | 从固定 SessionContext revision 选入本次执行的条目或摘要 |
| `AgentSecurityPolicy` | Agent 长期有效的能力、沙箱和审批权限上限 |
| `ExecutionSecuritySnapshot` | 本次执行不可变的能力授权、沙箱和审批边界 |

ExecutionSecuritySnapshot 在 execution 创建时冻结。审批只能放行不超过 AgentSecurityPolicy 和 snapshot 的请求。交付或共享结论仍需要显式提交，不因自动批准而绕过上下文边界。

## 6. 用户界面

桌面界面以 Project 为一级导航，以 Session 为主要工作上下文：

- Project 面板列出项目和其 Session；
- Session 面板展示共享上下文、Agent 和执行历史；
- Session 面板展示 ManagedProcess 状态，并允许挂载终端、分离和停止；
- Agent 面板展示私有 transcript、能力快照、待处理结论和控制操作；
- 咨询视图显示独立的私有询问过程，并提供提交结论的明确操作；
- 所有命令由 Go binding 提交，界面只渲染快照和事件投影。

项目路径用于展示、文件操作和诊断，不参与 Project、Session、Agent 或 execution 的身份关联。

## 7. 运行与部署形态

Praxis 以单机 Wails 应用运行：

```text
React UI (WebView)
        │ Wails binding / event
Go application services + orchestration
        │
        ├── SQLite
        ├── Agent JSONL transcript
        ├── attachments / notes
        └── optional LLM provider APIs
```

React 负责界面与投影；Go app 负责桌面生命周期和 binding；`internal/application` 负责用例准入、事务边界、产品状态写入和 AgentRuntime；`internal/orchestration` 负责执行调度、投递和恢复；`internal` 定义领域状态与内层端口；`application/agent_runtime` 负责 activation、取消和单 Agent execution loop；Provider 适配器只处理模型协议。

## 8. 交付范围

核心范围包括 Project/Session 管理、SessionContext、隔离 Agent transcript、Primary/Delegate/Advisor 协作、咨询结论提交、能力授权、执行控制、长期 Note 和本地恢复。

可扩展能力包括更多工作目录创建策略、Git worktree 与 patch 审阅、非编程场景模板、外部记忆桥接和多设备协作，但必须保持稳定 ID、上下文 revision、transcript 隔离、Grant 校验、ExecutionID 关联和用户审批边界。

## 9. 工程约束

1. 领域模型只依赖标准库；外层通过 core 定义的接口接入存储、Provider、工具和 Wails。
2. 所有状态变更经过拥有对应状态的 application service；UI、scheduler、runtime 和 adapter 不能绕过用例准入。
3. 密钥不写入 Project 文件、SQLite、JSONL、日志或 binding DTO。
4. runtime phase、模型流和工具临时状态不提升为持久化身份；持久化执行身份始终是 `AgentExecutionID`。
5. SessionContext 只允许追加和显式提交；私有 transcript 不自动镜像为共享上下文。
