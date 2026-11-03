# 事件重构：目标目录与实施清单

## 一、背景、目标与边界

本文件规定事件重构，Agent 架构以 [agent-structure.md](agent-structure.md) 为准。两份方案共同采用 Agent／Execution／Turn 模型：Agent 长期运行，Execution 是一次完整执行，Turn 是 Loop 内的一次 ReAct 迭代。

| 方案 | 负责的改造 |
| --- | --- |
| `agent-structure.md` | 长期 Main Agent、一次性 Subagent、Execution／Turn 身份、Agent 分支队列、活动 Execution、Steer、调度、取消控制及执行与存储模型 |
| 本文件 | 通知信封、最小载荷、展示快照、发布位置、共享 Emitter、Wails 映射、前端消费和初始化快照一致性 |

执行服务协调 Execution、Loop 和 Session；Loop 负责多轮迭代，Session 负责历史接纳和持久化。通知不改变这些业务所有权，也不要求向 Loop 注入 execution.Run 或 Session。

目标是：**业务流程推进状态，事件描述已确定事实；前端消费展示投影，不接收运行对象，也不实现另一套 Agent／Execution 状态机。**

以下是目标契约和可执行清单，不表示代码已完成。后端相对路径以 `backend/` 为根。

## 二、事件最终架构

```text
Agent／ExecutionService／Loop／工具执行／Coordinator 的明确业务边界
    │
    ├── 直接完成所属业务操作与事实提交
    │
    └── 使用本次已持有的身份和值构造通知
              │
              ▼
        core/event.Emitter
        同步发布、不缓存、不查找业务对象
              │
              ▼
        wails/bindings.EmitAgentEvent
        显式映射公开展示字段
              │
              ▼
        praxis:agent-event
              │
              ▼
        frontend/src/api/agents.ts
        统一订阅与事件判别联合
              │
              ├── Agent 展示与历史
              ├── Execution 下各 Turn 的临时输出
              ├── Agent 分支待办队列
              └── 工具审批、计时、用量和错误展示
```

必须保留的边界：

- Emitter 不接收 Agent、execution.Run、ExecutionRecord、Session 或业务服务，不通过 ID 查找这些对象。
- Entry 类型归 `core/session/entry`。Loop 产生条目后，通过同步提交入口交给执行服务；执行服务调用 Session 接纳，再更新执行事实并发布通知。
- 通知不能负责保存 Entry、推进 Execution、领取 Task、启动 Subagent 或唤醒审批。核心操作是直接调用，不依赖订阅者存在。
- Hook 是显式扩展切入点；通知不是 Hook 的返回结果，也不能通过 Hook 隐式替代业务提交。
- `core/event.Event` 是进程内通知；`core/session.Event` 是持久化事实。两者不共用总线或编解码流程。
- 通知代表内存事实已接纳，不承诺对应日志已经追加成功。保存失败由实际业务调用方报告，不让事件层重试业务操作。
- Emitter 使用同步、注册顺序通知和锁外回调；不引入异步队列、事件重放缓存、逐订阅者 goroutine 或新的全局排序机制。

## 三、目标目录与文件操作

只列事件重构及共享内容契约相关的文件。Agent、Task 和业务编排目录按 Agent 方案确定，本清单不创建空壳模块。

```text
backend/
├── internal/
│   ├── core/
│   │   ├── event/
│   │   │   ├── event.go                 修改：Execution／Turn 信封与最小类型化载荷
│   │   │   └── emitter.go               保留：同步发布订阅和取消订阅
│   │   ├── agent/execution/
│   │   │   └── snapshot.go              新增：只读执行状态快照，不包含运行能力
│   │   ├── session/entry/               保留：各模块共用的内容类型
│   │   ├── loop/
│   │   │   ├── turn.go                  接入：迭代边界通知
│   │   │   └── event_emitter.go          修改：Turn、模型流和模型用量通知组装
│   │   └── toolexecution/
│   │       ├── authorize.go             修改：审批通知来源与 CallID
│   │       └── review.go                检查：审核用量的 Execution／Turn／请求关联
│   ├── service/
│   │   ├── agent/
│   │   │   ├── query.go                 修改：Agent 状态和当前／最近 Execution 快照
│   │   │   └── execution/
│   │   │       ├── prepare.go           修改：输入接纳通知
│   │   │       ├── entries.go           修改：条目接纳通知
│   │   │       ├── settle.go            修改：Execution 终态通知的值组装
│   │   │       └── timing.go            修改：计时展示通知
│   │   └── coordinator/
│   │       ├── subagent.go              修改：子 Agent 登记和释放通知
│   │       └── configuration.go         修改：会话配置通知接入统一出口
│   └── bootstrap/
│       ├── application.go               检查：应用只创建一份共享 Emitter
│       └── coordinator.go               修改：通知依赖直接注入实际发布者
└── wails/
    ├── app.go                           保留：接入和退出时订阅／取消订阅
    ├── dto/
    │   ├── agent.go                     修改：公开信封、执行快照和最小事件 DTO
    │   ├── session.go                   修改：初始化快照中的 Agent 展示契约
    │   └── usage.go                     修改：请求级用量展示身份
    └── bindings/
        ├── bindings.go                  修改：逐种事件显式映射
        ├── bindings_agent.go            检查：查询只返回展示投影
        ├── bindings_session.go          检查：快照复用相同展示投影
        └── services.go                  检查：不暴露额外事件业务服务

frontend/src/
├── api/
│   ├── agents.ts                        修改：事件联合、执行快照、统一订阅
│   ├── models.ts                        修改：配置重载订阅使用同一事件出口
│   ├── sessions.ts                      修改：初始化快照的展示类型
│   └── timings.ts                       同步：Execution／Turn 身份
└── features/
    ├── agent/
    │   ├── agentStore.ts                修改：按 Execution／Turn 接纳通知
    │   ├── types.ts                     修改：临时输出和乐观输入归属
    │   ├── streaming.ts                 修改：Execution 下按 Turn 归并输出
    │   ├── ToolApprovals.tsx            修改：按 ExecutionID＋CallID 关联审批
    │   ├── SessionTokenUsage.tsx        修改：请求级用量通知归并
    │   ├── modelUsage.ts                修改：用量记录去重身份
    │   ├── AgentConversation.tsx        修改：失败原因读取 latestExecution
    │   └── output/                      同步：Turn 流式输出与 Execution 结果展示
    ├── session/useSessionData.ts        修改：分支队列、Entry 和分支 Head 通知
    └── workspace/
        ├── useWorkspaceData.ts          修改：生命周期通知消费
        └── useSessionPage.ts            修改：错误和配置通知过滤
```

事件定义集中在 `event.go`，不为每种事件建立一个文件。`snapshot.go` 是执行记录的只读投影，不包含 Agent 身份持有、调度、取消或持久化逻辑。

## 四、信封：Execution、Turn 和事件作用域

### 4.1 统一来源字段

| Go 字段 | 公开字段 | 语义 |
| --- | --- | --- |
| `SessionID` | `sessionId` | 所属会话 |
| `AgentID` | `agentId` | 事实所属 Agent，不是接收页面 |
| `BranchID` | `branchId` | Agent 对应的执行分支 |
| `BindingID` | `bindingId` | 适用时记录命令来源页面绑定，不表示队列或 Agent 所有权 |
| `ExecutionID` | `executionId` | 一次完整连续执行 |
| `TurnID` | `turnId` | Execution 内本次 ReAct 迭代，不适用时为空 |

公开信封包含 `kind` 和 `data`；不适用的身份省略。迭代模型流与工具调用通知提供 ExecutionID 和 TurnID。Execution 生命周期通知不需要 TurnID；会话配置通知只需要 SessionID。

Agent 与 Branch 共同确定运行展示范围。同一 Main Agent 的不同 Branch 不得因 AgentID 相同而互相覆盖。

### 4.2 生命周期与迭代通知

在事件定义、Wails DTO、前端类型和全部消费点使用以下契约：

| 常量／载荷 | kind | 所属层级 |
| --- | --- | --- |
| `KindExecutionStarted`／`ExecutionStarted` | `execution_started` | 完整执行开始 |
| `KindExecutionInterruptRequested`／`ExecutionInterruptRequested` | `execution_interrupt_requested` | 完整执行的停止请求已接纳 |
| `KindExecutionEnded`／`ExecutionEnded` | `execution_ended` | 完整执行的唯一终态 |
| `KindTurnStarted`／`TurnStarted` | `turn_started` | 一次 ReAct 迭代开始 |
| `KindTurnCompleted`／`TurnCompleted` | `turn_completed` | 一次 ReAct 迭代收尾 |
| `KindAgentStateChanged`／`AgentStateChanged` | `agent_state_changed` | 未由生命周期通知覆盖的等待／恢复展示变化 |

生产者直接使用业务流程已经持有的身份值。event 包不解析字符串推断 Execution 与 Turn 的关系，不建立全局业务对象定位函数。

### 4.3 队列与页面

`task_queue_changed` 描述 Main Agent 某个 Branch 的完整待办展示快照，范围为 SessionID＋AgentID＋BranchID，不依赖 ExecutionID，不以 BindingID 过滤。

所有展示同一分支队列的页面接纳同一通知。来源 BindingID 即使存在，也只是关联信息。事件消费不得因关闭页面而删除 Agent 队列；页面关闭策略由实际业务流程决定。

## 五、目标通知清单与最小载荷

State、Kind 等展示字符串由发布流程从已有业务投影取得，event 包不另建状态枚举或状态机。

| kind | 载荷 | 发布时点与消费语义 |
| --- | --- | --- |
| `session_config_reloaded` | 空对象 | 指定会话配置替换成功；按 SessionID 刷新模型展示 |
| `entries_appended` | `entries` | 首次接纳完整条目后；来源标明 Agent、Execution 和适用 Turn |
| `execution_started` | `startedAt`、`agentState` | Execution 已启动；设置当前执行快照，不创建 Agent |
| `execution_interrupt_requested` | `appliedAt`、`agentState` | 业务控制已接受停止请求，不表示 Execution 结束 |
| `execution_ended` | `endedAt`、`outcome`、`failureCode`、`failureMessage`、`agentState`、`entries` | 唯一 Execution 终态；包括成功、失败、yield 和中断，保留本次完整展示条目 |
| `agent_state_changed` | `state`、`updatedAt` | 工具审批等待／恢复等未由生命周期通知覆盖的展示变化 |
| `turn_started` | 空对象 | 本次 ReAct 迭代开始，建立 Turn 临时输出，不启动另一 Execution |
| `provider_waiting` | 空对象 | 当前 Turn 的迭代模型请求进入等待 |
| `text_delta` | `text` | 当前 Turn 的文本增量 |
| `thinking_delta` | `text` | 可展示思考文本，不含内部推理签名或加密载荷 |
| `tool_call` | `callId`、`name`、`arguments` | Assistant 工具声明已接纳；通知本身不调用工具 |
| `tool_approval_required` | `callId` | 审批请求已可查询，通过 ExecutionID＋CallID 关联详情 |
| `tool_result` | `callId`、`result`、`isError` | 真实回执已接纳；不能改写回执或触发重试 |
| `turn_completed` | `stopReason` | 迭代输出／回执接纳后的 Turn 展示收尾，不代表整个 Execution 完成 |
| `model_usage` | `requestSequence`、`usage` | 一次模型请求事实已接纳后的用量展示，覆盖普通、摘要和审核请求 |
| `operation_timing` | `timing` 展示投影 | 操作计时更新，不携带完整 ExecutionRecord 或运行对象 |
| `subagent_started` | `name`、`definitionId`、`kind`、`createdAt`、`currentExecution` 快照、`entries`、`parentAgentId`、`parentExecutionId`、`callId` | 子 Agent 和单次 Execution 已登记，首次输出前发送 |
| `subagent_released` | 空对象 | 子运行资源已释放，不删除历史，也不作用于长期 Main Agent |
| `task_queue_changed` | `tasks` | Agent 分支队列变化后的完整展示快照 |
| `error` | `message` | 已经由业务错误路径处理的展示错误，不是 Execution 终态，不驱动状态机 |

字段约束：

- `execution_ended` 没有失败原因时，`failureCode`、`failureMessage` 为空字符串；`outcome` 为实际业务结果。
- `agentState` 表达 Agent／Branch 在该边界的运行展示状态。Execution 失败放在执行结果中，不把长期 Agent 永久标记为 Failed。
- 同一状态变化已由 Execution 生命周期通知描述时，不重复发布 `agent_state_changed`。
- `tasks` 每项只提供任务 ID、正文、顺序和创建时间，不发送完整队列对象、Agent 或页面 Binding。
- 条目投影中的 ID 用于展示归并，通知归属仍以信封为准。发布时使用同一份已确定身份，不在 Wails 和前端重复校验本应用构造的数据。

Execution 只有一个 `execution_ended` 终态出口。待办取消由 `task_queue_changed` 描述；停止请求由 `execution_interrupt_requested` 描述，不为同一次执行取消额外发布另一种终态。

### 5.1 通知示例

一次 Execution 失败后的通知如下。`agentState` 表达长期 Agent 的当前可用状态；失败原因属于该次 Execution。空 `entries` 只用于省略正文展示。

```json
{
  "kind": "execution_ended",
  "sessionId": "session-demo",
  "agentId": "session-demo",
  "branchId": "branch-main",
  "executionId": "execution-demo",
  "data": {
    "endedAt": "2026-01-01T12:00:00Z",
    "outcome": "failed",
    "failureCode": "provider",
    "failureMessage": "模型请求失败",
    "agentState": "idle",
    "entries": []
  }
}
```

Turn 收尾使用 `kind = turn_completed`，在信封同时提供 `executionId` 和 `turnId`，载荷只提供 `stopReason`。即使模型 `stopReason = completed`，前端也只收尾该 Turn，等待业务发布 Execution 终态。

### 5.2 请求级用量关联

TurnID 用于迭代归属，不替代一次模型请求的唯一标识。普通模型、工具风险审核和摘要请求都必须能够独立归并用量。

通知使用 Execution 内稳定的 `requestSequence`，前端以 ExecutionID＋requestSequence 去重，不新增请求 UUID、请求管理器或事件缓存。风险审核归属于触发它的 Turn；轮后摘要没有 ReAct Turn 时不伪造归属。

发布函数使用本次调用已持有的请求序号。会话用量查询记录提供同一序号，查询 DTO、通知和前端使用一致的去重键。序号在请求事实中的保留由执行模型负责，事件层不另建存储。

### 5.3 Steer 与最终回答

- Steer 接纳到当前 Execution 上下文后，用 `entries_appended` 展示用户条目；不发另一次 `execution_started`，不伪装为 FIFO 入队通知。
- Loop 消费 Steer 后可以进入后续 Turn；Turn 通知仍描述实际迭代。
- `turn_completed` 或模型 `stopReason` 不能作为前端判定 Execution 已结束的依据。
- 业务完成边界处理完应接纳的 Steer 后，才允许发布 `execution_ended`。
- 未消费 Steer 的可靠性、最终提交竞争和 Followup 策略由 Agent 方案决定，事件不额外承诺持久化或补发。

## 六、执行状态快照与初始化查询

### 6.1 只读执行快照

在 `internal/core/agent/execution/snapshot.go` 定义只读 `Snapshot`，从 ExecutionRecord 按需投影，公开 DTO 与前端称为 `ExecutionSnapshot`。

| 字段 | 公开字段 | 约束 |
| --- | --- | --- |
| `ID` | `id` | ExecutionID |
| `Status` | `status` | `starting`、`running`、`ending`、`ended` |
| `Outcome` | `outcome` | 未结束为空字符串 |
| `StartedAt` | `startedAt` | 未开始为 `null` |
| `EndedAt` | `endedAt` | 未结束为 `null` |
| `FailureCode` | `failureCode` | 无原因码为空字符串 |
| `FailureMessage` | `failureMessage` | 无原因说明为空字符串 |

它表示某一时刻的执行状态，不是对话压缩摘要，也不是独立保存的执行结果对象。终态 outcome 及原因只是快照的一部分。

快照不包含 Config、SystemPrompt、Model、Progress、Context、Entry 或取消句柄，不另行持久化、缓存或维护可变状态。具体内容继续使用 `core/session/entry` 及历史展示字段。

Agent 查询、会话初始化快照和子 Agent 开始通知复用同一个只读投影，不各建同形业务模型。Wails 只负责映射公开字段。

### 6.2 Agent 查询

编辑 `service/agent/query.go`：

- `currentExecution`：当前活动执行快照，没有活动 Execution 时为空。
- `latestExecution`：最近已结束执行快照，没有结果时为空；启动下一个 Execution 不清除该结果。
- Agent 的 `state` 来自对应 Branch 的活动执行和队列，不从最近失败结果推导永久失败状态。
- 控制组件需要当前 ExecutionID 时，从 `currentExecution` 派生，不传递完整 ExecutionRecord。
- 空闲 Main Agent 按长期 Agent 模型投影；查询和事件消费不创建或销毁运行主体。

`core/agent/execution` 的记录和快照不依赖 event。event 可以引用执行快照、Entry 和计时等必要值类型，但不引用可变 Agent 或 execution.Run。展示状态和 Agent 种类由发布者转成值字段，避免事件与运行包循环引用。

## 七、发布点：具体文件与操作

发布代码归属服从 Agent 方案的业务边界。Loop 负责迭代，执行服务负责外部事实提交；下表不把业务提交改成通知副作用。

| 代码定位 | 要完成的事件改动 |
| --- | --- |
| `core/event/event.go` | 使用 Execution／Turn 信封和最小载荷；不包含完整执行记录或 `SourceForRun`；使用封闭 Payload 判别 |
| `core/event/emitter.go` | 纯发布订阅，不注入 Coordinator、Session、Run 或仓储 |
| `service/agent/execution/settle.go` 的 `ConfirmRunning`、`Settle` | 开始／终态确定后组装最小 Execution 通知；重复结算不重复发布 |
| `service/agent/execution/prepare.go` 的输入提交边界 | 输入接纳后发布 `entries_appended`，提供 Execution 身份；不由通知接纳输入 |
| `service/agent/execution/entries.go` 的条目提交入口 | Session 接纳后更新执行证据并通知；包含适用 Turn，不把父子历史混入同一来源 |
| `core/loop/turn.go` 的迭代边界 | 发布 `turn_started`、`turn_completed`；不从某次迭代结束推测完整执行终态 |
| `core/loop/event_emitter.go` 的 `emitModelEvent` | 使用本次 Execution 和 Turn 来源值，映射模型增量，不读取其他 Agent 或 Session |
| 工具真实回执的提交边界 | 以 ExecutionID＋CallID 关联，TurnID 标明迭代；通知不执行或重试工具 |
| `core/toolexecution/authorize.go` 的 `notifyWaiting` | 先有可查询审批事实，再发 `tool_approval_required`；使用当前调用身份，不调用全局 Run 定位函数 |
| Execution 的审批等待／恢复边界 | 发布 `agent_state_changed` 的只读状态，不为了通知新增可变 Agent 状态 |
| `service/agent/execution/usage.go` 及请求事实提交边界 | `model_usage` 显式提供请求序号；普通、摘要和审核请求使用同一契约 |
| `service/agent/execution/timing.go` | 发布执行计时投影，保存失败走业务错误路径，event 不处理补写 |
| `service/coordinator/subagent.go` 的登记／释放边界 | 开始通知只含元数据、执行快照和父调用关联；登记后、首次输出前发布 |
| `service/coordinator/control_service.go` 的控制接纳边界 | 控制请求由实际拥有者接受后通知，携带精确 Branch／Execution，不用事件路由停止命令 |
| Agent 分支队列的业务修改边界 | 发布完整队列展示快照，按 Agent＋Branch 广播，不绑定页面生命周期 |
| `service/coordinator/configuration.go` | 配置替换成功后走同一 Emitter，不由订阅者决定是否重载 |

删除 `SourceForRun` 及相同职责的全局业务定位函数。发布者使用本次流程已持有的身份值；所属包内可以复用来源组装，但只返回普通 EventSource，不把 Run 传给 Emitter。

Publisher 必须在业务事实已接纳后、业务锁之外通知。Entry、参数字节和投影切片按只读值交付，订阅者不能修改；不为内部约定额外创建通用结构校验或深拷贝框架。

### 7.1 共享 Emitter 装配

- `bootstrap/application.go` 创建一份应用级 Emitter，注入实际发布流程与桌面展示适配。
- Coordinator 直接持有注入的 Emitter，不通过 `s.loop.Emitter` 获取。
- Agent、执行服务、Loop 和工具执行路径使用同一个实例，不创建局部总线。
- `wails/app.go` 接入时订阅，退出交接时取消；取消订阅不影响业务结算和资源释放。
- 通知改造不额外建立 Agent 注册表、运行句柄或服务装配层。

## 八、Wails 与前端：逐文件操作

### 8.1 后端传输边界

| 文件 | 具体操作 |
| --- | --- |
| `wails/dto/agent.go` | 信封提供 `executionId`、`turnId`；定义最小生命周期、迭代和 Agent 状态载荷及 ExecutionSnapshot，不引用完整 ExecutionRecord |
| `wails/bindings/bindings.go` | `EmitAgentEvent` 按 kind 逐字段映射；子 Agent 从元数据与快照构造公开投影，不读取完整 Config |
| `wails/dto/session.go`、`bindings_session.go` | 初始化快照提供同一 `currentExecution`／`latestExecution` 展示契约，不附带内部运行对象 |
| `wails/bindings/bindings_agent.go` | 返回相同 Agent 投影；消费事件后不要求补查完整 Agent |
| `wails/dto/usage.go` 和用量查询映射 | 提供与通知相同的 ExecutionID＋requestSequence 去重身份 |

DTO 明确使用 `AgentExecutionStarted`、`AgentExecutionInterruptRequested`、`AgentExecutionEnded`、`AgentTurnCompleted` 和 `AgentStateChanged`，绑定层 switch 与前端联合同步调整。

全部进程内展示通知统一映射到 `praxis:agent-event`。配置重载也使用该出口，以空载荷和信封 SessionID 表达，不另外发布配置专属 Wails 通道。

`Events.On` 从通知的 `data` 取得公开事件。不同时保留两份相同通知或未使用的专属通道订阅。

### 8.2 前端类型与订阅

编辑 `frontend/src/api/agents.ts`：

1. 不导入或导出完整 ExecutionRecord。
2. 使用生成的 ExecutionSnapshot，对外表达为同名快照类型；事件联合覆盖第五节完整 kind 清单。
3. 来源区分完整执行 `executionId` 和迭代 `turnId`，不混用。
4. `subscribeAgentEvents` 只订阅统一 Wails 通道，不在内部查询业务状态。

`frontend/src/api/models.ts` 的 `subscribeSessionConfigReloaded` 基于统一订阅筛选 `session_config_reloaded`，将信封 SessionID 交给现有调用方。该便利入口不创建第二条通知链。

### 8.3 Agent store 与流式输出

| 文件／分支 | 具体操作 |
| --- | --- |
| `agentStore.ts` 的 `execution_started` | 以信封 ExecutionID 和开始时间设置 `currentExecution`，保持长期 Agent 和 `latestExecution` |
| 中断请求通知 | 仅在目标执行仍是当前 Execution 时更新停止中展示，不提前清空当前执行 |
| `agent_state_changed` | 按 Agent＋Branch 和适用 Execution 更新展示，不复制业务状态机 |
| `execution_ended` | 合并本次条目，更新最近结果快照；仅清理相同 Execution 的当前快照、等待标记和临时输出 |
| `turn_started`、`turn_completed` | 只处理对应 Execution 下的 Turn 临时展示，不创建 Execution，不改写 Agent 的停止／等待状态 |
| `subagent_started` | 接纳子 Agent 元数据、`currentExecution` 和初始条目，不补查执行记录 |
| `subagent_released` | 清理对应子执行的运行展示，保留结果和历史，不移除长期 Main Agent |
| `types.ts`、`streaming.ts` | 临时输出按 Agent／Branch 范围内的 Execution 归属，内部使用 `turns` 和 `turnId` 表达迭代 |
| `AgentConversation.tsx`、`output/CollaborationViews.tsx` | 失败与中断读取 `latestExecution` 的 outcome 和原因，不使用 Agent 长期状态代替结果 |
| `output/StreamingViews.tsx`、`ToolCallViews.tsx` | 按 Turn 组装输出，工具配对使用 ExecutionID＋CallID |

较早 Execution 的终态或 Turn 通知不得覆盖较新 Execution 的运行展示，可以合并其历史。所有分支过滤先依据 SessionID＋BranchID，不能只按 AgentID 操作共享状态。

### 8.4 其他订阅者

- `ToolApprovals.tsx`：事件与详情使用同一 ExecutionID＋CallID，不把 TurnID 当作工具定位键。
- `useSessionData.ts`：队列按 Agent／Branch 接纳，不要求 BindingID 相等；主分支 Head 只受主历史影响。
- `useWorkspaceData.ts`：终态筛选使用 `execution_ended`；目录标题等元数据查询不附带重载历史或 Agent 状态。
- `useSessionPage.ts`：错误按会话、分支与适用 Execution 过滤，配置订阅使用统一出口。
- `SessionTokenUsage.tsx`、`modelUsage.ts`：按请求序号归并用量；独立用量查询不触发整页或 Agent 重载。
- 计时展示：使用 Execution 身份和适用 Turn 归属，不通过通知重建运行句柄。
- 乐观输入：归属使用 ExecutionID；Followup 尚未形成执行时保持空值，不为展示提前创建 Execution。

## 九、实施顺序与两份方案的衔接

### 9.1 接口分工

| 共同接缝 | Agent 方案交付 | 本事件方案交付 |
| --- | --- | --- |
| Execution／Turn | 类型、ID 和业务事实语义 | 信封、kind 和全部消费字段保持一致 |
| Agent／Branch 状态 | 活动执行、等待／停止和队列的权威投影 | 查询与通知值一致，不另存业务状态 |
| ExecutionService | 准入、Loop 与 Session 编排、Entry 与进度提交、结算 | 在既定边界发布最小通知 |
| 分支队列 | 队列归 Agent 对应 Branch | 按相同范围广播与消费 |
| Steer／Subagent | 输入接纳、最终提交、父子控制与释放 | 在已确定时点发条目、终态和子运行通知 |
| 模型请求与计时事实 | 稳定请求序号、Execution／Turn 及记录归属 | 可独立归并的用量与计时载荷 |
| 日志格式 | 记录字段、编解码和恢复边界 | 进程内通知不复用日志记录，不设计日志迁移 |

### 9.2 可执行批次

1. **确定身份与展示投影**：对齐 Execution／Turn，完成只读执行快照和请求序号接缝。
2. **收窄事件契约**：信封、kind、最小载荷和发布点一起修改，删除 `SourceForRun` 与完整记录载荷。
3. **接通桌面展示**：修改 Agent 查询、初始化快照、Wails 映射和统一配置通道，重新生成绑定。
4. **完成所有订阅消费**：store、Turn 流式输出、审批、队列、用量、计时和工作区订阅一起调整。
5. **跟随业务归属调整发布位置**：Loop 只发布所属迭代事实，执行服务在提交边界通知；不通过事件驱动核心执行。
6. **构建和人工验收**：确认没有同一事实双重发布、身份混用或由订阅者推进业务。

事件载荷、生成绑定和前端消费作为匹配的可编译单元交付，不建立兼容总线、双路执行或过渡状态机。

### 9.3 删除与保留清单

删除：

- event 包中的 `SourceForRun` 和同职责全局业务定位函数。
- 通知和前端 API 中的完整 ExecutionRecord、Config、SystemPrompt、完整 Model 与内部 Progress。
- 重复取消终态出口。
- 配置专属 Wails 通道及其重复订阅。
- 以 BindingID 判定 Agent 队列所有权的过滤。
- 为消费通知发起的完整 Agent 或整页历史补查。

保留：

- 完整 ExecutionRecord、持久化 Event、Session Entry 历史和明确的业务提交路径。
- 执行服务、长期 Main Agent、分支队列和各 Branch 唯一活动 Execution。
- ExecutionID 与 TurnID 的不同语义。
- Emitter 的纯同步发布订阅和已有前端高频／低频订阅分离。
- 真实工具回执、审批详情查询和独立用量汇总查询。

## 十、风险、回滚与验证

### 10.1 风险与回滚

- 通知、查询和生成绑定必须共同交付。前端不能把 Turn 当完整 Execution。
- Agent 状态与 Execution 结果分开展示，验证失败后继续运行、审批等待／恢复和停止中状态。
- 生命周期与 Entry 按真实接纳顺序发布；不同 Agent 并发通知不保证全局顺序，消费方按归属更新。
- 事件不承诺跨重启送达，不靠补发恢复业务；重新打开页面从业务快照取得历史和状态。
- 保存失败不撤销已接纳事实；终态通知保留已接纳条目，订阅失效不阻止结算。
- 程序、前端、生成绑定与数据备份保持匹配。持久化字段变更按 Agent 方案处理，不在事件层增加迁移实现。
- 不自动修改 AGENTS.md，不自动启动开发服务，不新增或运行自动化测试。

### 10.2 静态完成条件

- event 包不出现 Run、Session 定位或仓储能力。
- 公开 DTO 和 `frontend/src` 不引用完整 ExecutionRecord。
- 通知使用 ExecutionID／TurnID 的既定语义；Turn 完成不清理整个 Execution。
- 查询、初始化和子运行开始通知复用同一执行状态快照，不额外存储快照。
- 队列不按 BindingID 过滤；Main Agent 不因 Execution 结束或子运行释放被销毁。
- 全部 `SourceForRun`、重复取消终态、完整记录载荷和配置专属通道调用及订阅完成调整。
- 不存在 `Coordinator → Loop → Emitter` 能力借用，也不存在通知／Hook 推进核心状态的订阅者。

按仓库已有流程验证，不启动开发服务：

```sh
cd backend
wails3 task bindings
go build ./...
go vet ./...
go list ./...
cd ../frontend
npm run check
```

只通过源类型生成绑定，不手工修改 `frontend/bindings/`。

### 10.3 人工验收

使用已有应用或取得明确启动授权后的应用，在独立数据环境检查：

- 同一 Main Agent 连续完成多个 Execution，身份保留；单次失败不阻止后续执行展示。
- 同一 Agent 的不同 Branch 通知、队列和停止目标互不串扰。
- 同一 Execution 的多个 Turn 正确归并，Steer 不创建新 Execution，只有业务终态清理当前执行。
- 开始、等待、恢复、停止请求和终态由通知直接更新，不附带 Agent 或历史补查。
- 取消只有一个 Execution 终态；旧执行的终态或 Turn 收尾不清理新执行输出。
- 子 Agent 登记先于输出，释放只清理子运行资源；历史和结果保留。
- 工具调用、审批和回执按 ExecutionID＋CallID 配对，真实回执不被展示通知覆盖。
- 普通、审核和摘要请求用量独立归并，查询回填不重复计数；计时保持正确执行归属。
- 配置重载只更新对应会话的模型展示，统一通道仅通知一次。
- 日志追加失败时已接纳结果仍可展示，订阅关闭不会导致业务重跑或无法释放。
