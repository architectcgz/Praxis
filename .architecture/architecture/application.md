# Application 目录

> 本文定义 `backend/internal/application` 的目标目录、应用服务职责、AgentRuntime、事务边界和依赖规则。
> 领域对象与状态机见 [`domain.md`](domain.md)，跨用例协调见 [`directory-structure.md`](directory-structure.md)，AgentRuntime 执行语义见 [`agent-runtime-model.md`](agent-runtime-model.md)，模型请求边界见 [`application/agent_runtime/model_request.md`](application/agent_runtime/model_request.md)。

## 1. 应用层边界

`internal/application` 承载产品写入用例和 AgentRuntime。应用服务负责命令准入、读取用例状态、调用领域行为、声明原子边界、调用内层端口，并在事务提交后协调运行时动作。

AgentRuntime 是应用层服务。它执行已经 durable 创建的 `AgentExecution`，协调 activation、取消、model request、tool call、transcript receipt 和 settlement 回调。Provider 协议、工具副作用和存储格式由外层适配器实现。

应用层不包含：

- Wails binding、外部请求 DTO 和公开错误映射；
- SQLite、JSONL、Provider SDK、工具和平台 API 的具体实现；
- 领域实体内部的状态转换与不变量；
- 跨应用服务的 scheduler、recovery、delivery 和 Workflow 流程；
- 只读列表、详情和审计投影。

## 2. 目录树

```text
backend/internal/application/
├── project/
│   ├── service.go                 Service、Config、共享端口和构造函数
│   └── create.go                  CreateProjectParams、Result 和用例
├── session/
│   ├── service.go                 Service、Config、共享端口和构造函数
│   ├── create.go                  Session 创建用例
│   └── context.go                 SessionContext 追加用例
├── agent/
│   ├── service.go                 Service、Config、共享端口和构造函数
│   ├── create.go                  Agent 创建用例
│   ├── policy.go                  AgentSecurityPolicy 更新用例
│   ├── agent_result.go            AgentResult 提交、批准和拒绝用例
│   └── briefing.go                Briefing 提交、批准和拒绝用例
├── execution/
│   ├── start/
│   │   ├── service.go             启动用例依赖和端口
│   │   ├── send_input.go          创建输入 execution
│   │   └── resume.go              创建恢复 execution
│   ├── control/
│   │   ├── service.go             控制用例依赖和端口
│   │   └── request_control.go     Pause、Close 请求及应用
│   ├── queue/
│   │   ├── service.go             排队用例依赖和端口
│   │   └── queue.go               入队、领取和完成
│   └── settlement/
│       ├── service.go             结算用例依赖和端口
│       └── settlement.go          start confirmation 与 durable settlement
└── agent_runtime/                 package agentruntime
    ├── doc.go                     包职责与并发边界
    ├── service.go                 Service、Config、端口和生命周期入口
    ├── registry.go                按 Agent 管理 runtime 与 generation
    ├── runtime.go                 单 Agent activation、cancel 和 close
    ├── loop.go                    model/tool loop 与停止条件
    ├── model_request.go           不可变 ModelRequest 构造
    ├── stream.go                  Provider-neutral stream 收集
    ├── output.go                  瞬时输出批处理与观察事件
    └── errors.go                  稳定运行时错误分类
```

目录名使用 `agent_runtime`，Go 包声明使用：

```go
package agentruntime
```

调用方按声明的包名引用：

```go
import "praxis/internal/application/agent_runtime"

runtime, err := agentruntime.NewService(config)
```

## 3. 产品应用服务

### 3.1 Project

`project.Service` 拥有 Project 写入用例。创建 Project 时，它在同一事务中建立 Project 及其初始 Workspace，并通过领域对象完成路径、名称和生命周期校验。文件系统路径选择与目录创建由调用方适配器在进入用例前完成。

### 3.2 Session

`session.Service` 拥有 Session 生命周期和 SessionContext 写入。每次上下文追加必须基于当前 revision 生成下一个单调递增 revision，并在一个事务内保存条目和 Session 状态。

### 3.3 Agent

`agent.Service` 拥有 Agent 身份、安全策略和结构化结果写入。创建 Agent 与保存初始 policy revision 在同一事务内完成。`AgentResult` 和 `Briefing` 只形成候选结果；写入 SessionContext 必须进入显式的 context append 用例。

## 4. Execution 应用服务

### 4.1 Start

`execution/start` 接受用户输入或恢复请求，读取固定的 SessionContext revision、Agent policy 和 Workspace 路径，构造不可变 `ExecutionInputSnapshot` 与 `ExecutionSecuritySnapshot`，并原子创建 `AgentExecution`、更新 Agent 状态和保存命令回执。

事务提交后，start 服务通过 `RuntimeActivator` 端口通知 scheduler。同步结果表示 execution 已 durable 创建，不等待 AgentRuntime 执行完成。

### 4.2 Control

`execution/control` 持久化 Pause、Close 等控制请求，并在事务提交后通过 `RuntimeCancellation` 端口通知 AgentRuntime。控制用例决定允许的产品状态转换；AgentRuntime 传播 cancellation 并报告实际结果。

### 4.3 Queue

`execution/queue` 管理 Agent 的 durable 工作队列。队列项不能代替 `AgentExecution`；领取队列项时必须通过 start 用例创建真实 execution，并以 `ExecutionID` 关联本次执行。

### 4.4 Settlement

`execution/settlement` 接收 AgentRuntime 已 durable 写入的 start 或 settlement receipt，在事务中推进 `AgentExecution`、Agent、WaitCondition、控制请求和队列项。重复 receipt 必须幂等，且不能仅根据进程内通知推断完成状态。

## 5. AgentRuntime

`application/agent_runtime.Service` 是 Agent 执行能力的应用层入口。它只接受已经 durable 创建且处于可 activation 状态的 `AgentExecution`，不创建 Project、Session、Agent 或 AgentExecution。

### 5.1 生命周期

`registry.go` 按 `AgentID` 保存进程内 runtime 和 generation。`runtime.go` 串行处理同一 Agent 的 activation、cancel 和 close，并保证同一 Agent 同时最多运行一个 execution。不同 Agent 可以由 orchestration 按并发策略独立激活。

```text
AgentRuntime.Service
    -> resolve Agent runtime
    -> activate durable AgentExecution
    -> append execution_started receipt
    -> confirm execution start
    -> execute model/tool loop
    -> append execution_settled receipt
    -> call settlement port
    -> release execution resources
```

activation、重复通知和 recovery 均以 `ExecutionID` 与 durable receipt 判定幂等，不能依靠 goroutine 是否存在推断产品状态。

### 5.2 Model/tool loop

`loop.go` 协调一个 execution 内的多轮模型与工具流程：

```text
ensure AgentLoop messages are ready
    -> build ModelRequest
    -> stream model response
    -> append assistant message
    -> no tool call: return completed
    -> execute granted tool calls
    -> append tool results
    -> next model request
```

AgentRuntime 负责：

- 从 execution 固定输入和 AgentLoop 当前 messages 构造不可变的 Provider-neutral `ModelRequest`；
- 执行 model calls、tool calls、输入和输出等资源限制；
- 根据 capability grant 过滤并校验工具调用；
- 将 assistant message、tool call 和 tool result 追加到所属 Agent transcript；
- 使用同一个 execution context 传播模型和工具取消；
- 在完成、主动让出、取消、资源超限或失败后产生稳定 outcome；
- receipt durable 后调用 execution settlement 端口。

AgentRuntime 不负责：

- 创建或直接修改 `AgentExecution`、Agent 和 SessionContext；
- 修改审批、Workflow、委派或其他 Agent 的 transcript；
- 解析具体 Provider HTTP 协议；
- 直接访问 SQLite、JSONL 文件或执行操作系统副作用；
- 决定跨 Agent 调度、投递和恢复策略。

### 5.3 端口

AgentRuntime 只依赖 core-owned 端口：

| 端口 | 用途 |
|---|---|
| `ModelStream` | 发起 Provider-neutral 模型请求并接收流事件 |
| `ToolRunner` | 执行经过授权的工具调用 |
| `TranscriptStore` | 读取所属 Agent transcript 并追加 message 与 receipt |
| `ExecutionLifecycle` | 确认 execution start 并提交 settlement |
| `Clock` | 产生可测试的时间 |

Provider、工具和 transcript adapter 由 `compose` 注入。AgentRuntime 不 import 这些具体实现。

## 6. 文件与类型规则

每个 application package 的 `service.go` 只定义该包共享的 `Service`、`Config`、构造函数和最小依赖端口。具体用例的 `Params`、`Result` 和方法放在同名业务文件中。

应用参数不复用 `contracts` DTO。命名遵循：

```text
contracts.SendInputRequest        外部传输请求
start.SendInputParams             应用用例输入
start.SendInputResult             应用用例结果
execution.AgentExecution          领域对象
```

需要抽象时，接口由使用方按最小能力定义。应用服务不集中声明全局 repository facade，不通过通用 `Execute`、`Handle` 或 `Manager` 隐藏具体业务语义。

Go 目录名可以与 package identifier 不同。`agent_runtime` 用下划线分隔应用层概念，包名 `agentruntime` 保持合法、简洁的 Go identifier。

## 7. 事务与副作用

应用服务决定一次写入用例中必须原子完成的 repository 操作，并通过 `persistence.Tx` 声明事务范围。应用层不接触 `sql.Tx`、数据库连接或隔离级别。

Provider 调用、工具执行、AgentRuntime activation、cancellation 和文件系统通知不进入数据库事务。事务先提交，再通过稳定 ID 和窄端口触发外部动作；失败与通知丢失由 durable 状态、幂等命令和 recovery 收敛。

AgentRuntime 不建立产品状态事务。它通过 transcript、model、tool 和 execution lifecycle 端口完成运行时工作，产品状态始终由对应 execution application service 更新。

## 8. 依赖规则

```text
app / workflow
       -> application
       -> core

orchestration
       -> application + core

application
       -> core/{domain,persistence,runtime,session,system}

storage / modelprovider / modelregistry / tools
       -> core

compose
       -> all concrete packages
```

`internal/application` 可以依赖标准库和 `internal/core`，不能 import `app`、`contracts`、`orchestration`、`workflow`、`storage`、`modelprovider`、`modelregistry`、`tools` 或 `compose`。

只读查询统一由 `internal/core/projection` 提供。跨 application package 的长流程由 `internal/orchestration` 协调；application service 不反向调用 orchestration，也不直接调用另一个产品域的具体 service。

## 9. 应用层不变量

1. 所有产品状态写入只经过拥有该状态的 application service。
2. 应用服务只依赖 core-owned 类型和端口，不依赖具体 adapter。
3. 外部协议 DTO 不进入应用服务和领域对象。
4. 领域状态转换由领域对象执行，application service 负责加载、调用和持久化。
5. 原子业务写入在一个 `persistence.Tx` 回调内完成，外部副作用在提交后执行。
6. `application/agent_runtime` 执行已创建的 durable execution，不创建或直接修改产品状态。
7. 同一 Agent 同时最多有一个 active execution；所有运行时幂等判断基于 `ExecutionID` 和 durable receipt。
8. application 不拥有跨用例调度、Workflow 状态或只读投影。
