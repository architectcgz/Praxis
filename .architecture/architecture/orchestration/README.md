# Agent 持久化编排模型

> 本文定义命令准入、SessionContext 版本、咨询流程、AgentExecution 状态、结算和恢复。
> 领域关系见 [`domain/structure.md`](../domain/structure.md)，源码归属见 [`directory-structure.md`](../directory-structure.md)，单 Agent 执行环见 [`application/agent_runtime/README.md`](../application/agent_runtime/README.md)，事实源见 [`storage/README.md`](../storage/README.md)。

command acknowledgement、execution、上下文提交、暂停和关闭的 deadline 统一见 [`timeout.md`](timeout.md)。

## 1. 核心规则

1. 持久化关系只有 `Project → Session → Agent → AgentExecution`。
2. 同一个 Agent 同时最多只有一个 active `AgentExecution`。
3. 每次 execution 启动时固定一个 `ContextRevision`，执行期间不自动读取后来追加的共享上下文。
4. Agent 的完整 transcript 是私有事实；只有显式提交的结论、决定或引用才进入 SessionContext。
5. 执行身份统一使用 `AgentExecutionID`，API、事件和 JSONL 使用 `executionId`。
6. 调用方等待超时不撤销已经 durable 的 command；后台必须继续完成 activation、settlement 和 recovery。

## 2. 持久化对象与 owner

| 对象 | 持久化事实 | 唯一 owner |
|---|---|---|
| `Session` | Project 归属、生命周期、当前上下文 revision 和工作目录属性 | Session application service |
| `SessionContextEntry` | Session 共享上下文的一条追加记录 | Session application service |
| `Agent` | Session 内稳定身份、角色、Profile、安全策略、状态和 transcript 引用 | Agent application service |
| `AgentExecution` | 一次执行的 start request、上下文 revision、输入快照、安全快照、状态、结果和失败码 | Execution application service |
| `ToolInvocation` | execution 下的规范化工具请求、审批、状态和结果引用 | Execution application service |
| `ManagedProcess` | Session 下长期受控进程的来源、命令快照、状态和结算 | `ManagedProcessCoordinator` |
| `AgentResult` / `Briefing` | Agent 产出的结构化候选结果 | Agent application service |
| `WaitCondition` | Agent 声明的外部依赖 | Execution application service |
| `AgentControlRequest` | Pause、Close 等需要异步收敛的控制命令 | Execution application service |
| Agent transcript | 一个 Agent 实际看过和产生的完整交互 | 该 Agent 的唯一 transcript writer |
| `AgentRuntime` | 进程内 actor、当前 execution 和 provider/tool 临时状态 | `application/agent_runtime.Service` |

Application service 负责产品状态、事务和单 Agent execution loop；orchestration 负责跨用例调度与恢复；AgentRuntime 只能执行已经创建的 AgentExecution，不能创建关系、提交共享上下文或修改其他 Agent 的 transcript。ManagedProcess 只能由 `execution/tool_invocation` application service 通过 `ManagedProcessCoordinator` durable 创建，AgentRuntime 不能把普通命令提升为长期进程。

## 3. SessionContext 协议

SessionContext 是追加式共享上下文。每次成功提交都在同一事务中检查 `ExpectedRevision`，并产生新的 revision：

```text
ReadContext(SessionID, Revision?) -> ContextSnapshot
AppendContext(SessionID, ExpectedRevision, Entry) -> ContextRevision
```

`Revision=0` 表示空上下文。读取未指定 revision 时返回当前版本；execution 启动时必须把读取到的版本写入 `AgentExecution.ContextRevision`。

```text
SessionContextEntry
├── SessionID
├── Revision
├── Kind: user_message | accepted_conclusion | decision | reference
├── SourceExecutionID?
├── Content
└── CreatedAt
```

上下文条目不可原地修改或删除。更正内容通过新的 `decision` 或 `accepted_conclusion` 条目表达。前端草稿和 Agent 中间推理不属于 SessionContext。

## 4. Agent 状态与 execution 状态

### 4.1 AgentState

| 状态 | 语义 | `SendInput` |
|---|---|---|
| `idle` | 没有 active execution，也没有未解决等待 | 创建 execution |
| `executing` | 存在 active execution | 拒绝，返回 `agent_executing` |
| `waiting` | 没有 active execution，但存在 WaitCondition | 创建 execution，保留等待 |
| `pausing` | Pause 或 Close 正在等待当前 execution 收敛 | 拒绝 |
| `paused` | 用户暂停完成，不自动启动 | 要求 Resume |
| `interrupted` | recovery 发现 execution 未正常结算 | 要求 Resume |
| `failed` | 上一次 execution 以技术错误结束 | 创建新的 execution |
| `closed` | Agent 已停止参与自动调度 | 创建 execution 时重新打开 |

### 4.2 AgentExecution 状态

```text
starting → running → settling → settled
```

`Outcome` 至少包括 `completed`、`yielded`、`paused`、`failed` 和 `interrupted`。`yielded` 表示 execution 主动结束并留下 WaitCondition，不表示仍占用 runtime。

## 5. Execution start

`SendInput`、`Resume`、咨询启动和已批准的独立任务都必须创建一个新的 AgentExecution。start transaction 至少完成：

1. 校验 Agent、Session 和请求归属；
2. 校验 `RequestID` 幂等性；
3. 读取当前 SessionContext 并固定 `ContextRevision`；
4. 保存尚未写入 transcript 的输入正文和不可变 `InputSnapshot`；
5. 创建 `AgentExecution(status=starting)` 并把 Agent 标记为 `executing`；
6. 提交低敏审计事件。

事务提交后由 scheduler 激活 runtime。同步响应只确认 execution start 已 durable，不等待模型完成。

## 6. 咨询流程

咨询用于在 Agent 编码期间回答一个问题，同时不把问题和推理过程污染主 Agent 的共享上下文：

```text
SessionContext revision 42
        │
        ├── 主 AgentExecution E1 暂停并结算为 paused
        │
        └── 咨询 AgentExecution E2 读取 revision 42
                └── 只写咨询 Agent 的私有 transcript

E2 结算出候选结论
        │
        └── 用户或显式命令以 ExpectedRevision=42 提交 ContextEntry
                └── 成功后产生 revision 43

主 Agent 通过 Resume 创建 E3，读取 revision 43
```

规则：

- 暂停的是主 execution，不是删除或冻结整个 Session；Session 仍可查询和追加经过批准的内容。
- 咨询输入、模型中间推理和未采纳答案只存在咨询 Agent 的 transcript。
- 结论提交采用 compare-and-append；revision 不匹配时拒绝隐式覆盖。
- 结论可以是 `accepted_conclusion`、`decision` 或 `reference`，并记录 `SourceExecutionID`。
- E3 是新的 AgentExecution，不能复用 E1 的 provider stream、取消上下文或旧输入快照。

## 7. WaitCondition 与结构化结果

Agent 可以在 execution settlement 时声明等待用户决定、审批或其他 Agent 结果：

```text
WaitCondition
├── ID / SessionID / AgentID
├── Kind: user_decision | approval | agent_result
├── TargetIDs
├── Mode: any | all
├── Status: pending | resolved | cancelled
└── CreatedByExecutionID
```

`AgentResult` 和 `Briefing` 是候选结构化结果。它们不会自动写入任何 Agent transcript 或 SessionContext。用户或编排规则必须发出显式 Context commit，才能把结果变成共享条目。

## 8. Pause、Resume、Close

### 8.1 Pause

有 active execution 时，Pause 创建 durable control request，取消当前 execution，等待 runtime 写入 `execution_settled(outcome=paused)`，然后把 Agent 标记为 `paused`。没有 active execution 时可以在一个事务中直接进入 `paused`。

### 8.2 Resume

Resume 只接受 `paused` 或 `interrupted` Agent，并创建新的 `AgentExecution(reason=resume)`。可选的新输入与 execution 一对一绑定；恢复时重新读取当前 SessionContext revision。

### 8.3 Close

Close 先让 active execution 完成取消和 settlement，再关闭 Agent。关闭保留 Agent、execution、SessionContext 和 transcript；关闭期间不允许写入该 Agent transcript。

## 9. Settlement 顺序

```text
AgentRuntime
  -> 完成 model/tool 边界
  -> append execution_settled(ExecutionID, Outcome, FailureCode)
  -> fsync Agent transcript
  -> ExecutionService.SettleExecution
       - 校验 active ExecutionID
       - 保存 outcome、usage 和 failure code
       - 创建或更新 WaitCondition
       - 完成 control request
       - 重新投影 AgentState
       - 发布产品事件
  -> commit
```

runtime transcript receipt durable 之前，产品 settlement 不得宣称 execution 已完成。settlement 不回滚已写入的 transcript、文件副作用或外部请求。

## 10. Recovery

启动时按以下顺序恢复：

1. readiness 保持关闭，拒绝新 command；
2. 修复 Agent transcript 的不完整尾部；
3. 按 `ExecutionID`、`RequestID` 和 receipt 对账 execution start 与 settlement；
4. 将没有 settlement receipt 的 active execution 收敛为 `interrupted`；
5. 重建 Session 当前 revision、Agent 状态、WaitCondition 和控制请求；
6. 重新激活可运行的 starting execution；
7. 所有关键扫描成功后才开放 command admission。

Recovery 不重放结果未知的模型调用、工具副作用或外部请求。重复 start、settlement 和 Context commit 都必须通过稳定 ID 或 expected revision 保证幂等。
