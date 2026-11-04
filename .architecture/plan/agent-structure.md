# Praxis Agent / Run / Turn 架构重构方案

## 一、背景、目标与边界

本方案规定 Agent 的运行主体、执行层级、队列、控制、历史提交和模块职责。通知信封、展示快照和前端消费以 [directory-structure.md](directory-structure.md) 为准。

目标是：长期 Main Agent 持续接收任务；每项任务形成一次 Run，由 ReAct Loop 完成多个 Turn；Subagent 使用相同执行能力完成一次委派；执行状态和控制资源只有一个所有者。

本文只描述目标契约和实施清单，不表示代码已完成。后端相对路径以 `backend/` 为根。

### 1.1 执行层级

```text
Agent（长期运行）
  ├── Run A（一次完整执行）
  │     └── ReAct Loop
  │           ├── Turn 1：模型请求 → 工具执行 → 回执
  │           ├── Turn 2：模型请求 → 工具执行 → 回执
  │           └── Turn 3：模型请求 → 最终回答
  └── Run B（下一次完整执行）
        └── ReAct Loop
              └── Turns
```

该层级表示执行事实的归属，不表示 Run 对象负责调用或编排 Loop。

| 概念 | 职责 | 生命周期 |
| --- | --- | --- |
| AgentDefinition | 定义角色、指令和能力 | 配置生命周期 |
| Main Agent | 持有稳定身份，管理分支任务队列和活动 Run | Session 生命周期 |
| Subagent | 完成 Main Agent 的一次委派 | 单次委派 |
| Task | 等待执行的用户输入 | 入队至领取 |
| Run | Agent 的一次完整连续执行 | 准备、运行至结算 |
| Turn | Loop 内的一次 ReAct 迭代 | 一次迭代模型请求及其工具处理 |
| ToolExecution | 单个工具的授权与执行 | 单次工具执行 |
| Session | 组织会话历史、分支和持久化事实 | 会话生命周期 |
| RunService | 协调 Run、Loop 与 Session | 服务生命周期 |
| Coordinator | Agent 注册、页面绑定与跨 Agent 协调 | 应用生命周期 |

同一 Run 可以包含多个 Turn。单个 Turn 完成不代表 Run 已结束。模型请求的 `stopReason` 描述本次模型响应；最终 Run outcome 由执行服务在业务结束边界确定。

### 1.2 整体结构

```text
Application
  └── Coordinator
        ├── Agent Registry
        ├── Session Page Bindings
        └── Main Agent
              ├── Branch A
              │     ├── TaskQueue
              │     └── CurrentRun
              ├── Branch B
              │     ├── TaskQueue
              │     └── CurrentRun
              └── Subagents
                    ├── Subagent A → 一个 Run → 多个 Turn
                    └── Subagent B → 一个 Run → 多个 Turn
```

TaskQueue 和 CurrentRun 归 Agent 的对应 Branch 管理。相同 Main Agent 的不同 Branch 可以独立执行，同一 Branch 同时只有一个活动 Run。

## 二、Agent、Branch 与调度

### 2.1 长期 Agent 身份

Main Agent 的逻辑生命周期绑定 Session，不绑定单次 Run 或页面 Binding。Agent 由所属运行管理流程建立，Run 的创建不重新创建 Main Agent。

Agent 负责：

- 持有稳定的 Agent 身份、种类和定义关联。
- 管理所属 Branch 的 TaskQueue，保持 FIFO。
- 保证同一 Branch 的 Run 串行启动。
- 持有各 Branch 当前 Run 的运行句柄。
- 接收 Steer，并将停止、审批等控制交给目标 Run。
- 根据调度规则，在当前 Run 结束后领取下一项 Task。

Agent 不负责 Provider 协议、具体工具实现、模型上下文编码、JSONL 存储或全局页面管理。

Main Agent 的 AgentID 与 SessionID 保持既定关联。Branch 不额外创建新的 Agent 身份。

### 2.2 Branch 隔离

```text
Main Agent
  ├── Branch A
  │     ├── TaskQueue
  │     └── CurrentRun
  └── Branch B
        ├── TaskQueue
        └── CurrentRun
```

队列、活动句柄、停止目标和运行展示均按 AgentID＋BranchID 定位。不能仅用 AgentID 确定同一 Main Agent 的停止目标。

关闭页面只解除 Binding；不能直接据此删除 Agent、队列或执行历史。页面关闭是否触发停止，由明确的业务策略决定，不交给事件订阅者处理。

### 2.3 Agent 状态与 Run 结果

Agent 的运行展示状态从对应 Branch 的活动 Run、等待行为和队列投影，不独立保存另一套执行状态机。

| 条件 | Agent 展示状态 |
| --- | --- |
| 没有活动 Run | Idle |
| Run 正在运行 | Executing |
| Run 等待工具审批 | Waiting |
| Run 正在响应停止 | Stopping |

失败、完成、yield 和中断记录在 `RunRecord.Outcome`。一次 Run 失败不表示长期 Agent 永久进入 Failed 状态。

查询应区分当前活动 Run 与最近已结束 Run。启动下一次 Run 不清除上一次结果；Agent 全局概览可以汇总各 Branch 的运行展示。

## 三、Run：完整执行记录与运行实例

### 3.1 RunRecord

`core/agent/run` 定义一次完整执行的事实，不承担 Loop 编排。

| 数据 | 含义 |
| --- | --- |
| `ID`、`AgentID`、`SessionID`、`BranchID` | 本次执行及所属范围 |
| `SourceTaskID` | Main Agent 从 FIFO 领取的来源任务 |
| `ParentRunID`、`ParentCallID` | 子执行对应的父执行和父工具调用 |
| `RequestID`、`InputEntryID` | 准入请求和输入条目关联 |
| `Config` | 本次执行冻结的定义、指令和模型配置 |
| `Status` | Run 生命周期阶段 |
| `Progress` | 模型请求、用量和输出等执行统计 |
| `CreatedAt`、`StartedAt`、`EndedAt` | 执行时间 |
| `Outcome`、`FailureCode`、`FailureMessage` | 最终结果与原因 |

RunRecord 在准备阶段就存在，运行过程中更新，结束后保留结果。它不是只在结束时产生的结果对象，也不是包含所有输出内容的完整结果包。

模型回答、用户输入、工具声明和工具回执通过关联的 Session Entry 表达，不嵌入 RunRecord。运行中的取消句柄、等待唤醒和上下文也不是持久化记录字段。

### 3.2 生命周期与结果

Run 保持四阶段生命周期：

```text
Starting → Running → Ending → Ended
```

最终 outcome 使用：

```text
Completed / Yielded / Failed / Interrupted
```

工具审批等待属于执行期间的行为，不为此增加另一套 Run 生命周期。

Turn 不承担上述完整执行状态机。它只标识本次迭代，提供模型响应结束原因和相关事实。

### 3.3 run.Run

`RunRecord` 是执行事实记录；`run.Run` 是本次执行从准备到结算的内存运行实例。

Run 持有：

- 本次 RunRecord。
- 本次已接纳 Entry 的只读引用及必要索引，用于执行证据和统计。
- 取消信号、准备阶段占用和工具审批唤醒能力。
- 待接纳的 Steer 输入及完成接纳边界。

Agent 持有其所属 Branch 的活动 Run。Coordinator 不保存第二套执行 ID、取消句柄、ending 标志或审批唤醒状态。

模型迭代上下文由本次 Loop 维护独立引用列表，Entry 内容按只读值共享；Run 不再维护另一份需要与 Loop 同步推进的模型上下文。

`run.Run` 不创建或持有可变 Agent 对象。它使用已经确定的身份和值，避免 Agent 持有 Run 时产生包循环依赖。

### 3.4 准备阶段与执行槽位

活动句柄必须覆盖准备期，不能等持久化完成或模型启动后才阻止同一 Branch 重复启动。

```text
Agent 领取 Task
    ↓
占用对应 Branch 的执行槽位
    ↓
执行服务准备输入、冻结配置并保存 RunRecord
    ↓
启动 Loop
    ↓
执行服务结算 Run
    ↓
释放槽位，向 Agent 返回结果
```

准备失败后释放槽位，并按既定规则处理 Task 的队首恢复。已接纳输入和请求身份的重复提交沿用现有准入约定，不通过事件触发业务重试。

Run 不负责领取后续 Task。下一项任务是否允许启动，由 Agent 的调度策略决定。

## 四、RunService、Loop 与 Session 的协作

### 4.1 编排所有权

由 `service/agent/run` 统一编排一次完整执行，不向 Loop 注入 run.Run、RunRecord 或 Session 服务。

```text
RunService
  ├── Run：生命周期、控制、统计和结果
  ├── Session：初始历史、条目接纳和持久化
  └── Loop：模型请求、工具调度和多轮迭代
```

执行服务负责：

1. 完成任务准入，准备 Run 和初始 Session Entry。
2. 绑定本次冻结模型、工具能力和身份值，操作计时随结果交给对应 Entry。
3. 启动 Loop，并提供条目提交、模型请求事实记录和运行中输入接纳等必要协作入口。
4. 将 Loop 产生的事实提交给 Session，更新本次执行记录。
5. 根据 Loop 返回信息及输入接纳边界确定最终 outcome，结算并释放资源。
6. 在事实已接纳后发布展示通知。

这些操作入口只暴露当前协作所需的值与错误，不暴露完整业务对象，不以通知或 Hook 隐藏核心提交。

### 4.2 Loop 的职责

`core/loop` 完整负责 ReAct 迭代：

1. 接收初始 Entry，构建并持有本次模型上下文。
2. 在安全迭代边界接纳新增输入。
3. 构建模型请求，消费模型响应。
4. 产生 Assistant Entry，交给外部提交入口接纳。
5. 根据工具声明调度工具执行，产生并提交真实回执。
6. 将已接纳 Entry 加入本地上下文，进入下一 Turn。
7. 判断模型是否已经给出最终回答，并在完成确认后返回。

Loop 可以接收 RunID、AgentID、BranchID、TurnID 等普通身份值及冻结配置；这不等于依赖它们对应的可变运行对象。

Loop 不持有 Agent、run.Run、RunRecord、Session 或存储服务，不调用执行服务的生命周期方法，也不决定队列调度。

继续复用现有 Provider、ToolExecutor 和 ContextBuilder。不为编排再建立第二个 Loop Runner 或通用执行框架。

### 4.3 初始历史与增量条目

Entry 类型继续定义在 `core/session/entry`，包含用户输入、模型输出、工具声明、真实回执和上下文摘要；它不是仅属于最终回答的输出类型。

```text
Session.BranchEntries
    ↓
执行服务准备初始 Entry
    ↓
Loop 构建本地上下文
    ↓
模型响应／工具执行产生新 Entry
    ↓
同步条目提交入口进入执行服务
    ├── Session 接纳并尝试追加日志
    ├── Run 接纳同一条目的引用，更新执行证据
    └── 发布条目通知
    ↓
返回已接纳 Entry／错误
    ↓
Loop 更新本地上下文并继续，或因错误退出
```

条目逐条提交，不等待整个 Run 结束后才统一回写。条目提交入口需要同步返回已接纳值和错误，不是可有可无的观察者回调，也不是事件总线订阅。

Session 负责确定历史父节点、创建时间和分支 Head。Loop 使用返回的已接纳条目，不自行维护第二套 Session 历史关系。

主执行写入主分支路径；子执行写入对应子 Agent 历史，不推进主分支 Head。历史跨 Run 保留，后续 Run 可以继续使用所属分支的上下文。

Session 接纳内存条目后追加日志，日志失败不撤销已接纳事实。执行服务仍接纳对应执行证据并报告保存错误；工具已经取得的真实回执不能改写为虚构失败，也不能为保存失败重新执行工具。

模型请求的编码和裁剪不改写原始 Entry。上下文摘要通过同一提交边界保存，改变后续上下文引用范围，但不删除原始历史。

### 4.4 Turn 与请求序号

```text
RunID
  ├── TurnID 1
  │     ├── 迭代模型请求
  │     ├── 工具审核请求（适用时）
  │     └── ToolExecutions
  ├── TurnID 2
  │     └── 迭代模型请求
  └── 轮后摘要请求（不伪造 Turn）
```

TurnID 表示一次 ReAct 迭代，可由 RunID 与对应迭代请求序号生成。不为 Turn 建立另一套完整执行管理器或复杂生命周期。

一次模型请求使用 Run 内稳定的 `requestSequence`。普通模型请求、工具风险审核和摘要分别分配请求序号；审核请求关联触发它的 Turn，轮后摘要没有 ReAct Turn 时不伪造归属。

TurnID 不替代模型请求唯一身份。模型请求记录、通知和用量查询统一使用 RunID＋requestSequence 归并。

## 五、Steer 与 Followup

### 5.1 Steer

Steer 是当前活动 Run 的新增输入，不是另一次 Run，也不加入普通 Task FIFO。

```text
Main Agent
  ├── TaskQueue → Followup
  └── CurrentRun
        ├── Turn 1
        ├── PendingSteer → 用户输入
        └── Turn 2 → 消费 Steer 后继续
```

规则：

- 必须存在可继续接纳输入的活动 Run。
- 不立即中断正在进行的模型请求或工具执行。
- 执行服务在安全 Turn 边界取出待处理输入，提交 Session，并把已接纳 Entry 交给 Loop。
- 同一 Run 可以接收多次 Steer，Loop 根据新增输入继续后续 Turn。
- 当 Run 已进入不可继续的结束阶段，明确拒绝新的 Steer；由调用方选择提交为 Followup，不静默改变输入模式。

### 5.2 最终回答与完成确认

必须区分：

- 某个 Turn 的模型响应完成。
- 整个 Run 的业务完成。

已接受的 Steer 必须在 Run 最终提交前处理。输入接纳与结束确认使用所属 Run 的同一控制边界，不能先宣布 Run 已完成，再发现已接受输入并重新执行。

Loop 负责根据接纳的输入继续迭代；执行服务负责确认本次 Run 是否已经允许进入结束阶段。`FinalStopReason`、`FinalAssistantID` 和最终输出 Hook 应遵循该业务完成边界，不把某次模型 `stopReason` 直接当作最终提交。

### 5.3 待处理输入的可靠性边界

TaskQueue 和待处理 Steer 可以采用进程内机制。Steer 正式接纳为上下文条目时写入 Session；尚未消费的输入不承诺跨进程重启恢复。

如果业务要求提交成功的待处理输入在崩溃后仍可恢复，必须另行明确返回成功前的持久化与消费规则。本次架构不额外承诺该可靠性，也不为此新增输入队列框架。

### 5.4 Followup

Followup 使用 Agent 对应 Branch 的 TaskQueue，保持 FIFO：

```text
Run A 运行中
    ↓
Task B 入队
    ↓
Run A 结束
    ↓
Agent 按调度策略领取 Task B
    ↓
Run B
```

中断后是否自动执行剩余队列，遵守显式恢复调度规则，不因模块重组改变行为。

## 六、Subagent

### 6.1 生命周期与能力

```text
Created → Running → Report Result → Closed
```

一次 Subagent 只执行一个 Run，该 Run 内可以有多个 Turn。

Subagent：

- 只接收 Main Agent 的委派，不直接接收用户输入。
- 不拥有 TaskQueue，不支持 Followup 或 Steer。
- 不再委派 Subagent。
- 与 Main Agent 复用相同的 RunService 和 ReAct Loop。

### 6.2 调用与结果归属

```text
Main Agent
  └── Main Run
        └── Turn
              └── 委派工具调用
                    └── Subagent
                          └── Subagent Run
                                ├── Turn 1
                                ├── Turn 2
                                └── 最终结果
                                      ↓
                                父工具调用回执
                                      ↓
                                Main Run 继续下一 Turn
```

Coordinator 的 `RunSubagent` 负责父执行定位、子 Agent 登记、委派调用与资源交接；实际执行使用共享执行服务。

子执行通过 `ParentRunID`＋`ParentCallID` 确定来源。父调用所属 Turn 用于迭代归属，不替代完整父 Run 的身份。

子 Agent 的中间历史不直接合并到主 Agent 上下文。Main Agent 接纳的是对应委派工具的结果。

### 6.3 并发、取消与释放

一个 Main Run 可以委派多个独立 Subagent。每个子 Agent 使用独立 AgentID、RunID、上下文和条目耗时，将结果返回对应父 CallID，不占用主 Agent 的 TaskQueue。

并行执行必须由工具调度路径实际支持，不能只建立多个子 Agent 对象而仍串行等待所有调用。

子执行的取消关系从父 Run 派生。父执行取消后，子执行完成必要结算并向父调用提供明确的成功、失败或中断结果。

Closed 只表示子运行资源释放，不删除 RunRecord、工具记录或历史 Entry。子 Agent 登记通知必须先于其首次输出，资源释放通知不销毁长期 Main Agent。

## 七、Coordinator、页面与资源所有权

### 7.1 职责划分

| 职责 | 所有者 |
| --- | --- |
| Agent 注册、查找与跨 Agent 协调 | Coordinator |
| Session 页面 Binding、Branch 所有权协调 | Coordinator |
| TaskQueue、下一任务领取、当前执行句柄 | Agent 对应 Branch |
| 单次执行的生命周期、取消和审批唤醒状态 | run.Run |
| Run、Loop 和 Session 之间的业务编排 | RunService |
| 模型上下文和 ReAct 多轮迭代 | Loop |
| 条目历史、分支、日志接纳与查询 | Session |

Coordinator 路由命令到目标 Agent／Branch／Run，不额外保存一份完整活动运行状态，也不直接控制 ReAct Turn。

### 7.2 Binding 与 Agent 生命周期

```text
Session
  ├── Main Agent
  │     ├── Branch A → TaskQueue + CurrentRun
  │     └── Branch B → TaskQueue + CurrentRun
  └── Page Bindings
        ├── Binding 1 → Branch A
        └── Binding 2 → Branch B
```

页面 Binding 只表示访问关系。多个页面可以展示同一分支的队列和执行；关闭某页不删除共享队列。

应用退出可以释放内存运行资源；再次打开 Session 时，根据持久化历史恢复逻辑身份和展示事实。历史恢复不等于自动恢复模型请求、工具执行或进程内待办队列。

## 八、目标目录与类型归属

```text
backend/internal/
├── core/
│   ├── agent/
│   │   ├── agent.go
│   │   ├── definition.go
│   │   ├── role.go
│   │   ├── errors.go
│   │   └── run/
│   │       ├── run.go
│   │       ├── run.go
│   │       ├── config.go
│   │       ├── usage.go
│   │       ├── failure.go
│   │       ├── model_request.go
│   │       ├── snapshot.go
│   │       └── timing/
│   ├── task/
│   │   ├── task.go
│   │   └── queue.go
│   ├── loop/
│   │   ├── run.go
│   │   ├── turn.go
│   │   ├── model.go
│   │   ├── tools.go
│   │   ├── context.go
│   │   └── ...
│   ├── session/
│   │   ├── session.go
│   │   ├── branches.go
│   │   ├── entries.go
│   │   ├── runs.go
│   │   ├── entry/
│   │   │   ├── entry.go
│   │   │   ├── tool_call.go
│   │   │   ├── reasoning_state.go
│   │   │   └── role/
│   │   └── ...
│   ├── toolexecution/
│   └── event/
├── service/
│   ├── agent/
│   │   ├── query.go
│   │   ├── definitions.go
│   │   └── run/
│   │       ├── service.go
│   │       ├── prepare.go
│   │       ├── run.go
│   │       ├── entries.go
│   │       ├── progress.go
│   │       ├── settle.go
│   │       └── ...
│   ├── coordinator/
│   └── session/
├── repository/
├── infra/
└── bootstrap/
```

目录约束：

- `core/agent` 保存长期身份和运行规则；`core/agent/run` 保存单次执行的事实与运行句柄，不反向依赖可变 Agent。
- `core/task` 定义 Agent 分支待办任务与 FIFO，不承担会话历史职责。
- `core/loop` 负责完整迭代，以 Turn 为单位组织迭代模型请求与工具处理。
- `core/session/entry` 是内部共享内容模型，Session、Loop、Context、工具和事件直接复用，不复制同形业务类型。
- `service/agent/run` 是一次执行的编排入口，业务提交不交给事件订阅者。
- `snapshot.go` 只提供 Run 的只读状态投影，供查询和通知复用；不持久化另一份快照，不形成第二套可变状态。公开契约见事件方案。
- 只按实际改造需要调整文件，不为目录完整创建空壳模块或重复管理器。

## 九、身份、工具关联与持久化

### 9.1 身份契约

| 字段／类型 | 语义 |
| --- | --- |
| `AgentID` | 稳定 Agent 身份 |
| `BranchID` | 会话执行分支 |
| `RunID` | 一次完整 Run |
| `TurnID` | Run 内一次 ReAct 迭代 |
| `ParentRunID` | 子执行对应的父完整执行 |
| `ParentCallID` | 委派来源工具调用 |
| `SourceTaskID` | Main Run 的来源任务 |
| `requestSequence` | Run 内一次模型请求的序号 |

记录、Entry、Provider、Hook、事件、计时和查询使用同一身份语义，不根据字符串格式推断另一层业务身份。

### 9.2 工具关联

工具调用与审批、回执、子任务关联使用 RunID＋CallID。TurnID 表示触发迭代，不替代工具调用定位。

CallID 遵守已有的执行内关联约定，不额外增加 ToolExecutionID、全局工具定位服务或为假设冲突建立通用身份重写层。

### 9.3 模型请求与用量

每次普通、审核和摘要模型请求记录所属 RunID、稳定请求序号及适用 TurnID。请求序号在请求事实中保留，查询和通知使用相同去重键。

一次 Turn 可以关联迭代请求和工具审核请求；没有 ReAct 迭代的摘要请求不创建伪 Turn。模型请求消耗由已接纳事实提交，不依赖展示事件是否送达。

### 9.4 Session 历史

Session 保存或组织以下持久化事实：

- ContextEntry 与 Branch。
- RunRecord。
- ModelRequestRecord。
- Entry 自身的耗时数据与工具相关记录。

日志事件和字段是独立的持久化契约，运行模型命名不改变存量记录的编码。RunID 和 TurnID 保持对应业务语义；任何新增持久化字段必须同步编解码与恢复边界，不能只修改内存类型。

持久化接纳、执行状态更新和展示通知的失败窗口由执行服务明确处理。事件不重试业务操作，历史重放不重新执行工具，也不自动恢复已中断的 Loop。

## 十、实施顺序

### 10.1 固定执行契约与协作边界

- 对齐所有记录、Provider、Hook 和通知中的 Run／Turn 语义。
- 明确 Entry 提交、模型请求事实记录和运行中输入接纳入口。
- 完成请求序号在记录、查询与通知中的一致表达。
- 将完整 ReAct 迭代收敛到 `core/loop`，由执行服务负责外部业务提交。

验收：单任务 ReAct、工具审批、条目历史和执行结算使用同一事实提交路径，Loop 不依赖完整 Run 或 Session 对象。

### 10.2 调整 Agent 生命周期与分支队列

- Main Agent 使用长期身份，不由 run.Run 创建。
- Agent 持有所属 Branch 的队列和活动运行句柄。
- TaskQueue 与页面 Binding 分离，保留 FIFO 和显式恢复调度规则。
- Agent 展示状态从活动执行事实派生。

验收：同一 Main Agent 连续完成多个 Run；分支并发、页面关闭和队列消费互不串扰。

### 10.3 收敛运行控制

- Run 覆盖准备、运行和结算阶段。
- Coordinator 不重复持有取消、ending 或审批唤醒状态。
- 控制接口通过 BranchID 和适用 RunID 精确定位目标。
- 准备失败、取消、保存失败和资源释放使用明确的直接调用路径。

验收：准备槽位、审批等待、停止和下一任务交接的所有者清楚，重复结算不重复提交或通知。

### 10.4 接入 Steer

- 在活动 Run 中接纳待处理输入。
- 执行服务在 Turn 边界完成 Session 提交，Loop 消费已接纳输入。
- 将最终输出与 Run 完成确认对齐。
- 保持进程内待处理输入的可靠性边界明确。

验收：Steer 不创建新的 Run；已接受输入在最终结算前处理，已有真实工具回执不丢失或改写。

### 10.5 统一 Subagent 与展示契约

- Subagent 单次执行、上报、释放，禁止嵌套委派。
- Main Agent 和 Subagent 共用执行服务与 Loop。
- 父 Run 取消传播到子执行。
- 通知、初始化快照、生成绑定与前端消费共同交付。
- 并行委派与工具调度配套实现，不扩展为另一套执行框架。

验收：子执行结果正确关联父调用，资源释放后历史仍可查询，父执行结束后不遗留本次委派资源。

## 十一、风险、回滚与验证

### 11.1 关键风险

- Run 与 Turn 是不同层级；Turn 完成或某次模型停止原因不能提前结束完整执行。
- Session 历史、Run 执行证据和 Loop 上下文共享只读 Entry，但各自的引用列表承担不同职责，不能维护两套相互校验的可变内容。
- 移交活动运行句柄必须覆盖准备阶段，避免同一 Branch 重复准入。
- Steer 接纳与最终结束使用同一业务控制边界，不能靠事件顺序解决业务竞争。
- 页面释放、子 Agent 释放和 Run 结束不删除长期 Main Agent 身份。
- 保存失败不能导致真实工具副作用重复执行；未消费的进程内输入不承诺跨重启恢复。

### 11.2 回滚边界

程序、前端、生成绑定与持久化数据使用匹配版本交付。涉及持久化字段变更时，以对应数据备份恢复，不在核心调用中叠加长期兼容状态机。

### 11.3 验证方式

按仓库已有流程执行构建和静态检查，不自动启动开发服务，不新增、保留或运行自动化测试。

```sh
cd backend
wails3 task bindings
go build ./...
go vet ./...
go list ./...
cd ../frontend
npm run check
```

仅通过源类型生成绑定，不手工修改生成文件。人工回归使用已有应用或取得明确启动授权后的应用，检查连续 Run、多 Turn、分支隔离、Steer、审批、取消、子执行和保存失败行为。

最终职责是：**Agent 持续运行，执行服务编排 Run、Loop 与 Session，Loop 迭代多个 Turn，Session 保存历史。**
