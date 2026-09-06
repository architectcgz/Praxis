# Workflow 模块

> 本文定义 Workflow 的模型、模块边界、Agent 与 AgentExecution 编排、上下文提交和恢复规则。
> 核心领域关系见 [`domain/structure.md`](../domain/structure.md)，AgentExecution 命令与状态见 [`orchestration/README.md`](../orchestration/README.md)。

## 1. 模块定位

Workflow 是核心领域之上的独立编排模块。核心领域保持以下 ownership：

```text
Project
└── Session
    └── Agent
        └── AgentExecution
```

Workflow 使用稳定 ID 引用 Session、Agent 和 AgentExecution，并通过核心命令编排它们：

```text
WorkflowDefinition
└── WorkflowInstance ──► SessionID
    └── WorkflowNodeInstance
        ├── AgentID
        └── AgentExecutionID[]
```

Workflow 拥有 WorkflowDefinition、WorkflowInstance 和 WorkflowNodeInstance。Session 拥有 Agent，Agent 拥有 AgentExecution。Workflow 取消、完成或删除时，核心领域对象及其 transcript、execution 历史和 SessionContext 继续保留。

## 2. WorkflowDefinition

WorkflowDefinition 是可复用、带 revision 的流程定义：

```text
WorkflowDefinition
├── WorkflowDefinitionID
├── Revision
├── Name
├── Nodes: WorkflowNodeDefinition[]
├── Edges: WorkflowEdgeDefinition[]
└── CreatedAt
```

WorkflowInstance 创建时固定 DefinitionRevision。Definition 的后续修改只影响新实例。

### 2.1 WorkflowNodeDefinition

节点定义描述如何选择 Agent、启动 execution、读取上下文和判断完成：

```text
WorkflowNodeDefinition
├── NodeKey
├── AgentBinding
├── ContextPolicy
├── ActivationPolicy
├── CompletionPolicy
└── ConcurrencyPolicy
```

- `AgentBinding` 声明为节点创建 Agent，或在实例化时绑定 Session 内的已有 Agent。
- `ContextPolicy` 生成 execution 使用的 `ContextRevision` 和 `ContextSelection`。
- `ActivationPolicy` 定义节点何时成为 ready。
- `CompletionPolicy` 定义 execution 结算、结果审批和上下文提交如何满足节点。
- `ConcurrencyPolicy` 限制该节点的并发实例数；同一 Agent 同时最多一个 active execution 的约束仍由核心层执行。

### 2.2 WorkflowEdgeDefinition

边定义上游节点与下游节点之间允许的推进条件：

```text
WorkflowEdgeDefinition
├── FromNodeKey
├── ToNodeKey
├── Trigger
└── Condition
```

`Trigger` 可以是 execution 完成、execution 失败、结论获批、上下文提交成功或用户显式操作。Condition 只能读取已持久化的 Workflow 投影和核心查询结果，不能执行任意脚本或直接产生副作用。

## 3. WorkflowInstance

WorkflowInstance 是某个 WorkflowDefinition 在一个 Session 中的持久化实例：

```text
WorkflowInstance
├── WorkflowInstanceID
├── WorkflowDefinitionID
├── DefinitionRevision
├── SessionID
├── State
├── CreatedAt
└── UpdatedAt
```

一个 WorkflowInstance 只绑定一个 Session，并且只能编排该 Session 下的 Agent 和 AgentExecution。

WorkflowInstance 状态：

```text
active | pausing | paused | completed | failed | cancelled
```

- `active`：允许 coordinator 推进 ready 节点。
- `pausing`：正在按显式策略暂停关联的 active execution。
- `paused`：停止派发新 execution。
- `completed`：所有必需节点满足完成条件。
- `failed`：流程无法根据定义继续推进。
- `cancelled`：流程停止推进，已持久化历史继续保留。

## 4. WorkflowNodeInstance

WorkflowNodeInstance 是定义节点在一个 WorkflowInstance 中的运行投影：

```text
WorkflowNodeInstance
├── WorkflowNodeInstanceID
├── WorkflowInstanceID
├── NodeKey
├── AgentID?
├── State
├── AgentExecutionRefs[]
├── CreatedAt
└── UpdatedAt
```

同一个 NodeKey 可以在循环或并发分支中产生多个 WorkflowNodeInstance。节点实例可以绑定一个稳定 Agent，并在初次执行、暂停恢复或显式重试中关联多个 AgentExecution。

```text
AgentExecutionRef
├── Sequence
├── AgentExecutionID
├── Trigger
└── RequestID
```

每个 `AgentExecutionID` 始终指向核心层拥有的真实 execution；Workflow 不创建另一种执行身份。

WorkflowNodeInstance 状态：

```text
waiting | ready | dispatching | active | awaiting_approval |
completed | failed | skipped | cancelled
```

节点状态描述依赖和流程进度。AgentExecution 状态描述真实执行生命周期。节点进入 `active` 或 `completed` 必须依据关联 execution 的核心投影，不能独立宣称 execution 已启动或已完成。

## 5. Agent 编排

Workflow 节点可以创建 Agent，也可以绑定 WorkflowInstance 所属 Session 中的已有 Agent。

创建 Agent 时，Workflow 生成稳定的幂等 RequestID 并调用核心命令：

```text
WorkflowNodeInstance(dispatching)
    -> CreateAgent(SessionID, AgentSpec, RequestID)
    <- AgentID
    -> 保存 NodeInstance.AgentID
```

绑定已有 Agent 时必须验证 `Agent.SessionID == WorkflowInstance.SessionID`。多个节点可以显式绑定同一 Agent，但 coordinator 必须遵守同一 Agent 同时最多一个 active execution 的核心约束。

WorkflowInstance 完成或取消不自动删除 Agent。需要关闭 Agent 时，Workflow 必须根据定义中的明确策略调用核心 Close 命令。

## 6. AgentExecution 编排

节点成为 ready 后，coordinator 构造 execution start request：

```text
WorkflowNodeInstance(ready)
    -> 读取 SessionContext revision
    -> 根据 ContextPolicy 生成 ContextSelection
    -> StartAgentExecution(
           AgentID,
           ContextRevision,
           ContextSelection,
           InputSnapshot,
           RequestID,
       )
    <- AgentExecutionID
    -> 保存 AgentExecutionID
    -> WorkflowNodeInstance(active)
```

Workflow 可以编排以下 execution 操作：

- 启动新的 AgentExecution；
- 请求暂停或取消 active AgentExecution；
- 为 paused 或 interrupted Agent 创建恢复 execution；
- 在策略允许时创建显式重试 execution；
- 监听 execution settlement 并推进后继节点。

每次 start、resume 和 retry 都产生新的 `AgentExecutionID`。Workflow 不复用已结算 execution，也不修改运行中 execution 的 ContextRevision、ContextSelection 或 InputSnapshot。

## 7. 命令与事件边界

Workflow 通过核心公开命令改变 Agent 领域状态：

```text
CreateAgent
StartAgentExecution
PauseAgent
ResumeAgent
CancelAgentExecution
CloseAgent
AppendSessionContext
```

Workflow 通过事件获得及时通知，并通过查询获得权威状态：

```text
AgentCreated
AgentExecutionStarted
AgentExecutionSettled
AgentPaused
AgentClosed
SessionContextAppended
```

事件可以重复或丢失，只用于唤醒 coordinator。Workflow 恢复和状态推进必须能够根据持久化 Workflow 状态以及核心查询结果重新计算。

核心模块不依赖 Workflow。手动创建 Agent、启动 execution 和提交上下文不需要 WorkflowInstance。

## 8. 幂等派发与恢复

Workflow 状态事务和核心命令事务属于不同模块。coordinator 使用持久化意图和稳定 RequestID 收敛跨模块操作：

```text
1. NodeInstance -> dispatching，保存 RequestID
2. commit Workflow transaction
3. 调用核心命令
4. 核心返回 AgentID 或 AgentExecutionID
5. 保存引用并推进 NodeInstance
```

若进程在第 3 至第 5 步之间退出，恢复时使用同一 RequestID 重试。核心命令必须返回既有 Agent 或 AgentExecution，不能重复创建。

WorkflowCoordinator 启动恢复顺序：

1. 加载未终结的 WorkflowInstance；
2. 查询 `dispatching` 和 `active` 节点引用的 Agent 与 AgentExecution；
3. 使用稳定 RequestID 补齐未确认的创建或启动；
4. 根据 execution 权威状态重建节点投影；
5. 重新评估 ready 节点；
6. Workflow 恢复完成后允许新的 Workflow 命令。

## 9. SessionContext 提交

AgentExecution 的 transcript 默认属于对应 Agent。Workflow 节点只能把候选结论提交为 SessionContextEntry：

```text
AgentExecution settled
    -> 产生候选结论
    -> CompletionPolicy 要求审批或自动形成候选提交
    -> AppendSessionContext(
           SessionID,
           ExpectedRevision,
           Entry,
           RequestID,
       )
    -> ContextRevision + 1
```

提交必须携带 ExpectedRevision。发生 revision conflict 时，WorkflowNodeInstance 保持 `awaiting_approval`，重新读取当前上下文并等待用户或策略再次确认；不得覆盖其他 Agent 已提交的内容。

下游节点只能读取已经提交的 SessionContext revision。未批准结果、咨询问题、中间推理和完整 Agent transcript 不会通过 Workflow 自动共享。

## 10. 咨询 Workflow

独立咨询可以由 Workflow 表达：

```text
pause_main
    -> 暂停主 Agent 的 active execution

consult
    -> 创建或绑定咨询 Agent
    -> 读取指定 SessionContext revision
    -> 启动咨询 AgentExecution
    -> 私有 transcript 保存问题与回答

accept_conclusion
    -> 用户审阅候选结论
    -> 追加 SessionContextEntry

resume_main
    -> 为主 Agent 创建新的 AgentExecution
    -> 读取包含结论的新 ContextRevision
```

Workflow 负责步骤依赖和命令派发；核心层负责 AgentExecution 状态、transcript 隔离和 SessionContext revision 校验。

## 11. Pause、Cancel 与完成

暂停 Workflow 默认只停止派发新的 AgentExecution。是否暂停已经 active 的 execution 必须由 Pause 命令的显式策略决定。

取消 Workflow 默认执行以下动作：

1. 将未派发节点标记为 cancelled；
2. 停止创建新的 Agent 和 AgentExecution；
3. 根据显式取消策略决定是否取消 active execution；
4. 保留 Workflow、Agent、AgentExecution、transcript 和 SessionContext 历史。

Workflow 完成只表示定义的完成条件已经满足，不代表 Session 完成，也不自动关闭 Session 下的 Agent。

Workflow 暂停、取消、完成或删除不自动停止 Session 拥有的 ManagedProcess。Workflow 需要停止进程时必须通过显式核心命令，并在定义中声明对应动作。

## 12. 模块不变量

1. 一个 WorkflowInstance 恰好绑定一个 Session。
2. WorkflowNodeInstance 只能引用该 Session 下的 Agent 和 AgentExecution。
3. Workflow 拥有流程状态，不拥有 Agent 或 AgentExecution。
4. WorkflowNodeInstance 状态不能替代 AgentExecution 的权威状态。
5. Workflow 通过幂等核心命令编排 Agent 和 AgentExecution，不直接写核心 repository。
6. Workflow 不直接写 Agent transcript；共享结果只能通过 AppendSessionContext 提交。
7. Context commit 必须检查 ExpectedRevision，不能覆盖并发提交。
8. Workflow 暂停、取消、完成或删除不能级联删除核心领域历史。
9. 核心模块不依赖 Workflow；没有 Workflow 时核心命令仍可独立使用。
10. ManagedProcess 由 Session 持有，不随 WorkflowInstance 或来源节点级联停止。
