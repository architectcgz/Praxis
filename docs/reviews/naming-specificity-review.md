# Praxis 命名具体性与语义准确性审查

## 结论

发现 **9 项命名与职责表达问题：4 项中、5 项低，未确认高等级问题**。优先处理会混淆持久化副作用、任务领取和工具结果未知状态的名称，再处理局部类型名。

这些发现不等于已经发生运行错误。主要风险是维护者仅凭名称无法判断操作边界，尤其容易混淆“构建与领取”“追加消息与提交执行事实”“中断与结果未知”。不建议全仓按关键词替换，也不建议为了去掉 `Lifecycle`、`Context` 或 `Manager` 而新增抽象层、拆散原子事务。

| 编号 | 级别 | 问题 | 推荐方向 |
| --- | --- | --- | --- |
| 1 | 中 | `interrupt` 实际保存结果未知状态 | `markResultUnknown` |
| 2 | 中 | `BuildTask` 实际准备并领取已有队列任务 | `PrepareQueuedTask`、`TaskPreparer` |
| 3 | 中 | `AppendAssistant` 同时登记工具调用和完成证据 | `CommitAssistantOutput`、`TaskOutputWriter` |
| 4 | 中 | 消息查询包含未在 Port 契约说明的持久化修复 | 显式表达回执修复，保留既定 Port 方法 |
| 5 | 低 | `TaskLifecycle.Start/End` 未区分开始确认与终态结算 | `TaskTransitions.ConfirmRunning/Settle` |
| 6 | 低 | JSONL 仓储实现名没有完整表达存储对象 | 与现有领域 Port 名称对齐 |
| 7 | 低 | 前端参数对象被命名为 `*Context` | `*Options`，删除不必要的单字段包装 |
| 8 | 低 | `Entry` 没有区分共享事实与模型请求条目 | `SharedContextEntry` |
| 9 | 低 | 通用 Agent 队列的 owner 被命名为 `mainAgentID` | `agentID` |

## 审查依据与范围

- 规则：`C:\Users\Administrator\.pi\agent\AGENTS.md` 的“命名具体性与语义准确性”和“命名检查”。全局文件已包含用户提供的规则、检查清单与命名对照表。
- 项目：`E:\learn\projects\Praxis`，分支 `codex/llm-protocol-schema`。
- 基线 HEAD：`b5f2a0f1b9f7506e68058bb92f69380a232e1827`；审查对象包含当前未提交和未跟踪源码，不只检查该 commit。
- 方式：主 Agent 对 219 个生产 Go 文件、73 个 `frontend/src` 下的 TypeScript/TSX 文件进行声明与命名关键词扫描，再精读任务准入、执行、结算、工具授权、消息恢复、上下文构建和前端 hooks 的相关实现、调用方与测试源码。
- 不包含依赖目录、构建产物和前端生成的 Wails binding；并非逐行完整正确性或安全审计。
- 未修改业务代码，未运行构建、测试或开发服务。下述测试引用是源码证据，不是本次测试通过的证明。
- 链接与行号对应本次审查的工作区文件；未提交代码不能用远端 HEAD 链接代替。

## 问题

### 1. 中：`interrupt` 与实际保存的 `unknown` 终态不一致

**位置**

- [`backend/internal/tool_execution/state.go:142-152`](../../backend/internal/tool_execution/state.go#L142-L152)
- 调用方：[`backend/internal/tool_execution/executor.go:199-208`](../../backend/internal/tool_execution/executor.go#L199-L208)
- 状态定义与转换：[`backend/internal/core/tool_invocation/tool_invocation.go:314-322`](../../backend/internal/core/tool_invocation/tool_invocation.go#L314-L322)

**问题描述**：`Executor.interrupt` 不是向执行发送中断信号，也没有调用领域对象的 `Interrupt`。它在工具已经开始且被取消后，将失败码保存为 `ToolFailureResultUnknown`，调用 `MarkResultUnknown`，结算到 `ToolInvocationUnknown`。同一个模块的执行准入逻辑另有真正的 `current.Interrupt` 调用，两个不同终态却使用近乎相同的动作名。

**影响范围／风险**：隐藏“工具可能已经产生副作用、结果无法确认”的关键区别。维护者可能按名称复用错误的结算路径，或把结果未知理解成普通取消。当前已有防重复执行逻辑，本项不是声称该逻辑已经失效。

**修正建议**：将私有方法重命名为 `markResultUnknown`，同步唯一生产调用点；保持 `unknown` 状态、失败码、持久化和禁止自动重放的规则不变。调用方局部变量 `interrupted` 可同步改为 `unknownResult`。

**验证依据**：现有 `TestToolApprovalAndCancellationAreDurable` 的“执行后取消”分支明确要求 `ToolInvocationUnknown`；`TestUnknownSideEffectsCannotBeRepeatedWithAnotherCallID` 检查未知副作用不能通过换 CallID 重复执行，见 [`recovery_test.go:446-458`](../../backend/internal/service/runtime/recovery_test.go#L446-L458)、[`recovery_test.go:579-598`](../../backend/internal/service/runtime/recovery_test.go#L579-L598)。

### 2. 中：`BuildTask` 隐藏了队列领取、状态推进和执行绑定

**位置**

- [`backend/internal/agent_runtime/task.go:9-14`](../../backend/internal/agent_runtime/task.go#L9-L14)
- [`backend/internal/service/runtime/task/start/queued.go:13-78`](../../backend/internal/service/runtime/task/start/queued.go#L13-L78)
- 调用方：[`backend/internal/agent_runtime/runtime.go:167-177`](../../backend/internal/agent_runtime/runtime.go#L167-L177)

**问题描述**：方法接收已经存在的 Pending Task，不创建新的 Task 身份。它在事务内确认队首及准入条件、冻结输入、调用 `task.Prepare`、推进 Agent 状态、建立进程内执行绑定，再保存 Task 和 Agent。`TaskBuilder` 还提供 `ReclaimTask`，负责恢复已有执行身份，而非单纯构建对象。

**影响范围／风险**：`Build` 淡化了这是一条占用执行资格的命令，而不是可随意调用的上下文计算。对于重试、并发调用和事务回滚，理解偏差会直接影响正确的调用位置。

**修正建议**：使用 `TaskPreparer` 和 `PrepareQueuedTask`；保留已有 `ReclaimTask`。同步 runtime 配置、组合根和测试实现，不增加 Builder 与 Preparer 两套接口。

**可选方案**：如果团队将“领取”作为固定领域术语，可使用 `ClaimQueuedTask`。不应改成 `CreateTask`，因为身份本来已经存在。

### 3. 中：`MessageRecorder.AppendAssistant` 不只是追加消息

**位置**

- [`backend/internal/agent_runtime/messages.go:10-14`](../../backend/internal/agent_runtime/messages.go#L10-L14)
- [`backend/internal/infra/jsonl/assistant.go:14-56`](../../backend/internal/infra/jsonl/assistant.go#L14-L56)
- 调用方：[`backend/internal/loop/run.go:229-247`](../../backend/internal/loop/run.go#L229-L247)

**问题描述**：接口注释称其“只写……持久化消息”，但 `AppendAssistant` 在同一事务里追加 Assistant 消息、登记整批 requested 工具调用，并在最终响应时更新 Task 的 `FinalStopReason` 和 `FinalAssistantID`。它提交的是后续工具执行及恢复所依赖的事实，不只是某种消息角色的 Append。

**影响范围／风险**：接口消费者与测试替身容易只实现消息追加，而遗漏工具意图和完成证据；维护者也难以从方法名理解为什么这里必须跨消息、调用记录和 Task 保持原子性。

**修正建议**：接口可命名为 `TaskOutputWriter`，方法命名为 `CommitAssistantOutput`，同步实际实现、loop 调用和测试替身；注释明确提交内容、幂等身份和事务边界。普通 `Append` 可保留。

**约束**：消息与调用登记必须仍在同一事务内，不要拆成独立的 `AppendMessage`、`RegisterCalls`、`SaveProgress` 三次提交。

**验证依据**：[`recovery_test.go:286-313`](../../backend/internal/service/runtime/recovery_test.go#L286-L313) 的 `TestAssistantRegistrationIsAtomicAndIdempotent` 要求登记失败整批回滚、重复提交不产生重复调用；[`recovery_test.go:561-577`](../../backend/internal/service/runtime/recovery_test.go#L561-L577) 检查最终消息的完成证据。

### 4. 中：`List/LoadMessages` 的契约没有表达读取时的持久化修复

**位置**

- Port：[`backend/internal/repository/message.go:17-29`](../../backend/internal/repository/message.go#L17-L29)
- 实现：[`backend/internal/infra/jsonl/messages.go:45-105`](../../backend/internal/infra/jsonl/messages.go#L45-L105)
- 修复：[`backend/internal/infra/jsonl/tool_receipts.go:53-127`](../../backend/internal/infra/jsonl/tool_receipts.go#L53-L127)

**问题描述**：`List` 在存在回执缺口时开启事务并调用 `ensureToolReceipts`。该方法可能将工具调用结算为 interrupted 或 unknown、保存调用记录，再追加工具结果消息。`LoadMessages` 也通过 `List` 触发这一过程，但 Port 仅描述读取记录副本及序号边界。

**影响范围／风险**：调用方会低估查询的写锁、落盘和失败语义。例如历史读取可能因为回执修复写入失败而返回错误，不能理解成无副作用的投影查询。

**修正建议**：遵守项目既定的 `MessageStreams.Append/List/LoadMessages` 窄 Port，不直接改名或新增转发层。最小修正是将私有 helper 命名为 `reconcileToolReceipts`，并在 Port 与实现注释中明确“读取前可能原子结算遗留调用并补写缺失回执；写入失败则读取失败”。

**可选方案**：如果以后需要真正只读的查询契约，应单独规划显式恢复入口，并核验启动恢复、运行中取消和终态结算后的全部缺口补齐路径。不能仅为了名称把当前恢复逻辑删掉或仓促移出事务。

**验证依据**：[`recovery_test.go:313-398`](../../backend/internal/service/runtime/recovery_test.go#L313-L398) 明确测试读取修复一次、保留活跃及审批调用；[`recovery_test.go:851-874`](../../backend/internal/service/runtime/recovery_test.go#L851-L874) 明确测试修复写失败时读取失败并回滚。因此，本项是职责与契约表达问题，不把有意的读取修复判为功能缺陷。

### 5. 低：`TaskLifecycle.Start/End` 可更准确表达开始确认与终态结算

**位置**

- [`backend/internal/agent_runtime/task_lifecycle.go:10-20`](../../backend/internal/agent_runtime/task_lifecycle.go#L10-L20)
- [`backend/internal/service/runtime/task/lifecycle/service.go:96-125`](../../backend/internal/service/runtime/task/lifecycle/service.go#L96-L125)
- runtime 调用：[`backend/internal/agent_runtime/runtime.go:271-280`](../../backend/internal/agent_runtime/runtime.go#L271-L280)、[`runtime.go:332-367`](../../backend/internal/agent_runtime/runtime.go#L332-L367)

**问题描述**：该 Port 不拥有 Task 创建、队列准备、取消信号或执行调度。`Start` 只确认并持久化 Running 状态；`End` 对执行结果、控制命令和 Agent 状态做原子结算，返回仲裁后的终态。与此同时，`task/start` 包负责准入准备，runtime 负责真正运行 loop。泛称 Lifecycle 加上 Start/End 不利于区分这些 owner。

**影响范围／风险**：主要是维护和导航成本，容易把“开始确认”误当成“启动执行”，把“终态结算”误当成“释放进程资源”。未确认因此产生的运行错误。

**修正建议**：可将现有 Port 命名为 `TaskTransitions`，方法使用 `ConfirmRunning` 和 `Settle`；同一实现保留两项紧密相关的事务操作。文件、字段与 package 名称可在该边界整理时统一对齐，避免遗留两套术语。

**约束**：不因此引入完整状态机框架，也不把开始确认和结算机械拆成多个服务。`Lifecycle` 并非禁词，本项依据是当前职责已经能用更窄的名称准确表达。

### 6. 低：JSONL 仓储实现省略了关键业务对象

**位置**

- [`backend/internal/infra/jsonl/repositories.go:24-61`](../../backend/internal/infra/jsonl/repositories.go#L24-L61)
- 实际能力对应：[`repositories.go:425-434`](../../backend/internal/infra/jsonl/repositories.go#L425-L434)

**问题描述**：`ToolRepository` 保存的是工具调用，不是工具定义；`ContextRepository` 保存 Session 共享事实，不是模型请求上下文；`PolicyRepository` 和 `ControlRepository` 分别保存 Agent 安全策略与 Agent 控制命令。领域 Port 已有更具体的名称，实现却重新缩成宽泛类别。

**影响范围／风险**：跨包搜索和组合根装配时，需要继续打开方法签名才能确认对象；尤其 `ToolRepository` 容易与 `ToolRegistry`、工具配置混淆。

**修正建议**：直接对齐已有术语，不新增接口或适配层：

| 实现名 | 建议名 |
| --- | --- |
| `ToolRepository` | `ToolInvocationRepository` |
| `ContextRepository` | `SessionContextRepository` |
| `PolicyRepository` | `AgentSecurityPolicyRepository` |
| `ControlRepository` | `AgentControlCommandRepository` |

**兼容边界**：只调整 Go 标识符和引用，不顺便修改 JSONL collection、事件名或持久化字段。`ProjectRepository`、`TaskRepository` 等对象明确的名称应保留。

### 7. 低：前端 `*CommandContext` 实际是 hook 参数对象

**位置**

- [`frontend/src/features/workspace/useWorkspaceCommands.ts:10-25`](../../frontend/src/features/workspace/useWorkspaceCommands.ts#L10-L25)
- [`frontend/src/features/agents/useAgentCommands.ts:13-50`](../../frontend/src/features/agents/useAgentCommands.ts#L13-L50)
- [`frontend/src/features/sessions/useSessionCommands.ts:11-36`](../../frontend/src/features/sessions/useSessionCommands.ts#L11-L36)
- [`frontend/src/features/agents/useAgentData.ts:17-27`](../../frontend/src/features/agents/useAgentData.ts#L17-L27)
- 调用方：[`frontend/src/features/workspace/useProjectWorkspace.ts:20-68`](../../frontend/src/features/workspace/useProjectWorkspace.ts#L20-L68)

**问题描述**：`CommandContext`、`AgentCommandContext`、`SessionCommandContext` 都是显式传入的配置、状态和回调集合，不是 React Context，也没有独立的上下文传播职责。`AgentSessionContext` 更只有一个 `agents` 字段，却给它起了一个会话上下文名称。

**影响范围／风险**：容易误导阅读者寻找 Provider 或上下文 owner；局部类型没有把真实参数角色表达清楚。

**修正建议**：复用已有 `AgentDataOptions` 风格，依次命名为 `WorkspaceCommandOptions`、`AgentCommandOptions`、`SessionCommandOptions`。`AgentSessionContext` 可直接删除，让 `loadForSession` 接收 `AgentSnapshot[]`；方法名可进一步明确为 `loadSessionAgents`。

**约束**：只整理参数与动作名称，不把状态和 setter 包进新的 Context Provider，不拆分职责紧密相关的 command hooks。

### 8. 低：同一包的 `Entry` 与 `ContextEntry` 缺少来源层次区分

**位置**

- [`backend/internal/core/context/types.go:17-36`](../../backend/internal/core/context/types.go#L17-L36)
- [`types.go:85-96`](../../backend/internal/core/context/types.go#L85-L96)
- 输入转换：[`backend/internal/service/session/context.go:71-95`](../../backend/internal/service/session/context.go#L71-L95)
- 构建映射：[`backend/internal/core/context/builder.go:84-112`](../../backend/internal/core/context/builder.go#L84-L112)

**问题描述**：`Entry` 是带身份、来源 Task、摘要和时间的 Session 共享事实输入；`ContextEntry` 是模型请求中带角色与内容块的有序条目。同包还有持久化的 `SessionContextEntry`。`Entry` 这个泛称没有表达自己到底属于共享事实、消息历史还是模型请求。

**影响范围／风险**：维护上下文选择及恢复时，需要追踪字段和转换才能确认所处阶段，增加混淆共享事实与模型条目的成本。

**修正建议**：把输入投影命名为 `SharedContextEntry`，相应类别可命名为 `SharedContextEntryKind`。保持既有输入投影、持久化类型及模型请求类型的行为边界，不为命名新增转换层。

**约束**：`ModelContext`、`SessionContextEntry` 的 Context 均有明确的模型领域含义，不需要为了避词改成 Prompt 或通用 Data。

### 9. 低：队列隔离字段 `mainAgentID` 与实际参数范围不一致

**位置**

- [`backend/internal/service/runtime/queue/service.go:160-180`](../../backend/internal/service/runtime/queue/service.go#L160-L180)
- 唯一生产装配调用：[`backend/internal/compose/agent_runtime.go:104-112`](../../backend/internal/compose/agent_runtime.go#L104-L112)

**问题描述**：`runtimeFactory.New` 为指定 Agent 创建 runtime，将通用 `agentID` 传给 `NewTaskQueue`。队列构造器只校验 ID 非空，字段只用于 `FindNextPendingByAgent` 的隔离条件，没有校验或证明它是 main Agent。`mainAgentID` 把业务准入中“仅主 Agent 可预约”的规则误写成了通用队列 owner 的性质。

**影响范围／风险**：维护者可能误以为所有 runtime 都共享主 Agent 的队列，或认为构造器已经完成主 Agent 身份校验。当前队列查询本身仍按传入 ID 隔离，本项不是串队列缺陷。

**修正建议**：字段、构造参数及相关注释使用 `agentID`。主 Agent 的预约限制继续由现有准入逻辑负责，不在队列构造器重复增加身份判断。

## 检查后不建议机械调整的名称

- **领域状态**：`AgentState`、`ProjectState`、`SessionState`、`WorkspaceState` 等表达有限业务状态，不是无边界的状态管理器。
- **标准上下文**：Go `context.Context`；React `ModelUsageContext`、`FilePreviewContext`、`TimingContext`；模型领域 `ModelContext` 和调用归属 `ToolInvocationContext`。这些名称准确表达已有上下文概念。
- **有明确包域的角色**：`project.Service`、`session.Service`、`modelregistry.Registry`、`jsonl.Store` 不应只因包含 Service、Registry 或 Store 就添加冗余前缀。
- **相关操作集合**：`useAgentData` 管理当前会话 Agent 的选择、历史和实时输出，`useWorkspaceData` 负责工作区加载编排；多操作本身不足以证明需要拆分。模型配置 Port 的配置、凭据和发现能力也都被模型设置入口使用，不建议仅因 `ConfigManager` 中的 Manager 就拆成一批单实现接口；若统一具体命名，可考虑 `ModelSettings`。

## 修正边界与验证方式

1. 先做动作与语义对齐：第 1、2、3 项。保持事务、取消、幂等身份、预算和失败分类不变。
2. 第 4 项先补全窄 Port 的修复契约并明确内部动作名，不扩大为未授权的消息恢复重构。
3. 第 5—9 项按原有模块边界进行局部标识符整理。无需集中大范围改名，不保留仅为兼容内部旧名的转发包装。
4. 修改后检查全部生产调用和测试替身；Go 变更需要构建、相关测试、`go vet` 及项目依赖门禁，TypeScript 参数变更需要静态类型检查。
5. 优先复用现有原子登记、读取修复、结果未知防重放和恢复测试；只有确认存在覆盖缺口时才补最小测试。不要改动状态字符串、日志键位或 Wails wire 字段来配合内部命名。

本次只产出上述审查报告，没有应用任何重命名或行为调整。
