# Praxis 目录结构

> 本文定义 Praxis 源码目录、Go 包职责、应用用例文件布局和依赖方向。
> 系统分层见 [`system-architecture.md`](system-architecture.md)，领域归属见 [`structure.md`](structure.md)，领域文件布局见 [`domain.md`](domain.md)。

## 1. 根目录

```text
Praxis/
├── backend/                 Go 后端、Wails binding 与进程入口
├── frontend/                React UI
├── scripts/                 构建与测试门禁
└── .architecture/           目标架构与实现规范
```

所有 Go 源码和后端工程文件位于 `backend/`。前端只能通过 `frontend/src/api/` 调用 Wails binding，不直接依赖后端内部类型。

## 2. 后端目录

```text
backend/
├── main.go                  桌面应用入口
├── app/                     Wails binding、公开错误映射和桌面生命周期
└── internal/
    ├── contracts/           binding 请求、响应、事件和快照 DTO
    ├── application/         面向产品操作的应用用例
    │   ├── execution/       AgentExecution 业务用例子包
    │   ├── project/         Project 写入用例
    │   ├── session/         Session 写入与上下文用例
    │   └── agent/           Agent policy 与结果用例
    ├── orchestration/       跨用例调度、投递和恢复
    ├── core/
    │   ├── domain/          领域实体、值对象、状态机和文件布局（见 `domain.md`）
    │   ├── command/         durable command 身份和回执协议
    │   ├── persistence/     repository 与 Tx 端口
    │   ├── projection/      只读快照、列表和审计事件投影
    │   ├── runtime/         Provider-neutral runtime 端口
    │   ├── session/         transcript 与 receipt 端口
    │   └── system/          clock、ID 和生命周期端口
    ├── agentruntime/        单 Agent execution loop
    ├── managedprocess/      Session 长期受控进程
    ├── workflow/            Workflow 定义、实例和 coordinator
    ├── storage/             SQLite、JSONL、blob 和 DataRoot 适配器
    ├── providers/           模型协议与 Provider registry
    ├── tools/               工具 schema、注册、规范化和执行适配器
    ├── compose/             唯一组合根
    └── logging/             诊断日志
```

## 3. 应用层

`internal/application` 按产品用例划分包。应用服务负责命令准入、业务流程、原子边界和持久化端口调用，不依赖 Wails、SQLite、Provider SDK 或具体 runtime 实现。

外部协议 DTO 与应用参数使用不同类型：

```text
contracts.SendInputRequest        Wails 传输请求
execution/start.SendInputParams  SendInput 用例参数
execution/start.SendInputResult  SendInput 用例结果
domain/execution.AgentExecution   持久化领域对象
```

`Request` 只用于外部协议或确实表示持久化请求的领域对象。应用用例参数使用 `Params`，不使用传输层 DTO，也不通过 `Command` 名称暗示独立的 CQRS 模型。

### 3.1 Execution 应用服务子包

```text
internal/application/execution/
├── start/                 execution 输入启动与恢复
│   ├── service.go         Service、Config、端口和构造函数
│   ├── send_input.go      SendInputParams、SendInputResult、SendInput
│   └── resume.go          ResumeParams、Resume
├── control/               execution 暂停与关闭控制
│   ├── service.go         Service、Config、端口和构造函数
│   └── request_control.go  RequestControlParams、RequestControl、ApplyControlRequest
├── queue/                 execution 排队工作
│   ├── service.go         Service、Config、端口和构造函数
│   └── queue.go           入队、出队和启动协调
├── settlement/            execution 启动确认与结算
│   ├── service.go         Service、Config、端口和构造函数
│   └── settlement.go      runtime start confirmation 与 durable settlement
```

`execution` 目录按业务能力划分为独立 Go 子包。每个子包的 `service.go` 只定义本业务用例共享的接收者和依赖，不放置具体用例的输入或输出参数；每个用例的 `Params`、`Result` 和实现放在同一个用例文件中。

例如 `start/service.go` 定义启动与恢复用例共享的依赖：

```go
type Service struct {
	tx         persistence.Tx
	agents     persistence.AgentRepository
	executions persistence.AgentExecutionRepository
	models     ModelResolver
	activator  ExecutionActivator
}
```

具体用例逻辑归对应业务子包。应用层输入使用各用例文件中的 `Params`；创建 execution 时，`start` 应用服务将参数和 core 端口读取结果组装为不可变的 `domain/execution.ExecutionInputSnapshot`，固定上下文选择、安全快照和 runtime 参数。`start/send_input.go` 创建新的 execution；`start/resume.go` 从 paused 或 interrupted 状态创建后续 execution；`control/request_control.go` 持久化 Pause 或 Close 请求并在提交后通知 runtime；`settlement/settlement.go` 接收 runtime 回调并更新权威执行状态。

### 3.2 Project、Session 与 Agent

```text
internal/application/project/
├── service.go             Project Service、Config、端口和构造函数
└── create.go              CreateProjectParams、CreateProjectResult、CreateProject

internal/application/session/
├── service.go
├── create.go
└── context.go

internal/application/agent/
├── service.go
├── policy.go
├── agent_result.go        AgentResult 结构化执行结果用例
└── briefing.go            Briefing 定向 Agent 交接用例
```

`service.go` 定义本产品域 application service 的具体 `Service` 结构体、共享依赖、`Config`、端口和构造函数；它不是另一个业务服务，也不要求在此定义 application service 接口。`create.go` 定义同一个 `Service` 结构体的 `CreateProject` 方法，并在同一文件放置该用例的 `Params` 和 `Result`。项目生命周期、路径或工作区的其他写入用例按业务新增独立文件，并继续使用该结构体接收者和依赖。Go 会将同一目录下的这些文件编译为同一个 package，因此文件拆分不改变 service 的归属；`session` 和 `agent` 遵循相同规则，输入输出参数不回填到 `service.go`。`agent_result.go` 承载结构化执行结果的提交、审批和拒绝用例；`briefing.go` 承载面向指定目标 Agent 的内部简报投递、审批和拒绝用例。`AgentResult` 与 `Briefing` 的领域实体、字段校验和状态转换属于 `core/domain/workflow`，application 文件只负责调用这些领域行为并完成事务和持久化。两者都不是 Provider stream 或通用输出参数容器。

新增写入用例时，继续在同一 package 下按用例新增文件，例如 `update.go` 中定义 `func (s *Service) UpdateProject(...)` 及其 `UpdateProjectParams`、`UpdateProjectResult`；如果操作具有更具体的业务语义，则使用 `rename.go`、`move.go` 等名称。需要抽象时，接口由使用方按最小能力定义，application service 包不在 `service.go` 集中声明调用方接口。

只读查询和快照投影不属于 application service，统一放在 core 的 projection 边界：

```text
internal/core/projection/
├── project.go             Project 与 Workspace 投影
├── session.go             Session 与 SessionContext 投影
├── agent.go               Agent 与 Agent policy 投影
├── execution.go           AgentExecution 与控制状态投影
└── event.go               审计事件投影
```

每个 application 包只拥有本产品域的写入用例。跨包流程由 `internal/orchestration` 协调，查询由 `internal/core/projection` 提供，领域状态转换仍由 `internal/core/domain` 中的对象执行。

## 4. Tx 边界

应用层决定一次用例中哪些 repository 操作必须原子完成。具体事务由 storage adapter 实现。事务端口统一命名为 `Tx`：

```go
package persistence

type Tx interface {
	InTx(context.Context, func(context.Context) error) error
}
```

`Tx` 不暴露 `sql.Tx`、SQLite 连接、隔离级别或提交实现。应用服务只通过回调声明原子范围，所有需要共同提交的 repository 使用回调收到的 context。

外部副作用不放入数据库事务。execution activation、runtime cancellation、Provider 调用和文件系统通知均在事务提交后执行，并通过稳定 ID、持久化状态和 recovery 收敛。

## 5. Orchestration

```text
internal/orchestration/
├── scheduler.go            激活已经 durable 创建的 execution
├── recovery.go             根据权威状态恢复未完成流程
├── delivery.go             跨 Agent 的持久化上下文投递
└── runtime_registry.go     按 Agent 管理 runtime 生命周期
```

orchestration 组合多个应用用例或 runtime 端口，不拥有领域状态。它可以调用 application service；application 不反向依赖 orchestration。需要由 scheduler、registry 或其他协调器实现的能力，由 application 或 core 定义窄端口并在 `compose` 中注入。

Execution settlement 属于 `application/execution`，因为它修改 AgentExecution 和 Agent 的权威状态。Scheduler 和 recovery 只负责触发、重试和收敛，不直接绕过应用服务写 repository。

## 6. Core

| 目录 | 职责 | 不包含 |
|---|---|---|
| `core/domain` | 实体、值对象、状态机和领域不变量 | repository、binding DTO、runtime actor |
| `core/command` | durable command 身份、参数摘要和结果回执 | 业务状态转换、传输 DTO、命令处理流程 |
| `core/persistence` | repository、查询和 `Tx` 端口 | SQLite driver、SQL、文件格式 |
| `core/projection` | 跨领域只读快照、列表和审计事件投影 | 状态写入、binding DTO、存储实现 |
| `core/runtime` | 模型流、工具执行和 execution lifecycle 端口 | Provider SDK、工具副作用实现 |
| `core/session` | transcript、receipt 和 session store 端口 | JSONL 文件实现 |
| `core/system` | clock、ID 和进程生命周期端口 | 平台 API 实现 |

`core/domain` 只依赖标准库。其余 core 包可以依赖 domain，但不能依赖 app、contracts、application、orchestration、storage、providers、tools 或 compose。

## 7. 适配器与组合

- `app` 把 `contracts` DTO 转换为 application `Params`，并把内部错误映射为公开错误码。
- `storage` 实现 core persistence、session 和 blob 端口。
- `providers` 和 `tools` 实现 core runtime 端口，工具由当前进程直接执行。
- `tools` 的 Agent 工具注册表不包含删除文件、目录或业务对象的工具，`compose` 只向 `agentruntime` 注入该注册表中的工具。
- `agentruntime` 执行已经由 application durable 创建的 execution，不创建产品关系或直接修改产品状态。
- `compose` 创建具体实现并注入 application、orchestration、workflow 和 app；其他包不得 import `compose`。

## 8. 依赖方向

```text
app ───────────────► application ─────────► core
workflow ──────────► application ─────────► core
orchestration ─────► application + core
agentruntime ──────► core
storage/providers/tools ──────────────► core
compose ───────────► all concrete packages
```

同层适配器不互相 import。跨适配器组合只发生在 `compose`。任何状态写入都必须经过拥有该状态的 application service，runtime、scheduler、workflow 和 binding 不直接调用写 repository。

## 9. 文件命名

Go 包路径使用简短、全小写、无下划线的名称。文件名按用例或职责命名：

- `service.go`：共享接收者、依赖、端口和构造函数；
- `<use_case>.go`：参数、结果和用例实现；
- `<projection>.go`：core 只读快照和列表投影；
- `repository`、`adapter`、`manager` 不作为无具体语义的通用包名；
- 文件名中的下划线只用于分隔语义，例如 `send_input.go` 和 `request_control.go`。
