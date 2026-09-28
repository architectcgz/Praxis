# Praxis 架构

本文档描述 Praxis 当前目标状态下的产品边界、运行时分层、核心数据关系、持久化边界和前后端通信边界。架构文档只描述可被代码直接验证的目标状态；具体实现以 `backend/` 和 `frontend/` 为准。

## 产品边界

Praxis 是面向个人学习者的本地多 Agent 编排桌面应用：

- 技术形态为 Go + Wails + React，所有核心能力在本机进程内运行，不依赖远程 Praxis 服务端。
- `Project` 管理项目，项目可以包含多个 `Workspace`；`Session` 绑定一个项目工作区并承载一次持续协作。
- `Agent` 是 Session 内的运行实例，绑定一个可复用的 `AgentDefinition`。默认定义包括 `primary`、`delegate`、`advisor` 和 `curator`。
- `Agent` 通过持久化的 `AgentExecution` 执行用户输入、排队任务或恢复操作。执行期间可以调用受安全策略约束的工具。
- Session 共享上下文与每个 Agent 独立的 transcript 分开管理。共享上下文用于协作事实，transcript 用于保留单个 Agent 的完整对话和执行记录。

核心关系如下：

```text
Project
└── Workspace
    └── Session
        ├── Agent ── AgentDefinition
        ├── SessionContextEntry
        └── AgentExecution ── ToolInvocation / QueuedWork / WaitCondition
```

## 运行时分层

```text
React frontend
    │ Wails generated bindings / events
    ▼
backend/wails
    │ narrow service ports
    ▼
internal/service
    │ use cases + transaction boundary
    ├── internal/repository       持久化 Port
    ├── internal/runtime          Agent runtime Port 与 execution 生命周期
    ├── internal/modelconfig      模型配置 Port
    └── internal/compose          组合根
             │
             ├── internal/agent_runtime + internal/loop
             ├── internal/orchestration
             └── internal/infra
                    ├── SQLite / document / JSONL
                    ├── model registry / agent registry
                    ├── provider adapters
                    └── local tools
```

依赖规则：

1. `internal/contracts` 提供跨包共享的 ID、错误、快照和协议值类型；不依赖业务实现。
2. `internal/agent`、`project`、`session`、`workspace`、`execution`、`context`、`workflow`、`security` 和 `tool_invocation` 保存领域模型及其校验、状态转换与不变量。
3. `internal/repository`、`internal/runtime`、`internal/modelconfig` 和 `internal/tools/contracts` 定义 Port；Port 不暴露 SQLite、文件格式、Provider 协议或 Wails 类型。
4. `internal/service` 实现产品用例，负责输入校验、幂等检查、事务内状态变更和事务提交后的 runtime 激活。
5. `internal/infra` 实现本地存储、配置加载、模型 Provider 和工具执行器；它可以依赖领域模型与 Port，但领域模型不依赖 `infra`。
6. `internal/compose` 是唯一的生产组合根，负责创建具体实现、注入依赖和释放进程级资源。
7. `backend/wails` 只负责桌面生命周期、DTO、错误映射、binding 和事件转发；它不直接访问 repository、SQLite 或文件存储。

## 后端模块

| 路径 | 职责 |
|---|---|
| [`backend/internal/contracts`](../backend/internal/contracts) | 跨领域 ID、错误码、模型快照、安全快照、权限和共享协议类型 |
| [`backend/internal/agent`](../backend/internal/agent) | Agent、AgentDefinition 及 Agent 状态转换 |
| [`backend/internal/project`](../backend/internal/project) | Project 模型 |
| [`backend/internal/workspace`](../backend/internal/workspace) | Workspace 模型与工作区边界 |
| [`backend/internal/session`](../backend/internal/session) | Session 模型、标题和归档状态 |
| [`backend/internal/execution`](../backend/internal/execution) | AgentExecution、输入快照、执行状态与结算结果 |
| [`backend/internal/context`](../backend/internal/context) | Session 共享上下文、transcript 投影、上下文预算与 digest |
| [`backend/internal/workflow`](../backend/internal/workflow) | QueuedWork、WaitCondition、AgentControlCommand |
| [`backend/internal/security`](../backend/internal/security) | Agent 权限上限、Sandbox、Approval 和工具策略 |
| [`backend/internal/tool_invocation`](../backend/internal/tool_invocation) | 工具调用领域模型 |
| [`backend/internal/repository`](../backend/internal/repository) | Repository Port 与 SQLite 事务 Port |
| [`backend/internal/service`](../backend/internal/service) | Project、Session、Agent 和 execution 的应用服务，以及 Wails 前端服务集 |
| [`backend/internal/runtime`](../backend/internal/runtime) | Provider-neutral model、transcript、execution lifecycle 和 Agent runtime Port |
| [`backend/internal/loop`](../backend/internal/loop) | 单次 execution 的 model/tool turn loop、预算和 Provider 事件归一化 |
| [`backend/internal/agent_runtime`](../backend/internal/agent_runtime) | 将 `loop` 与工具调用服务组装成默认 `ExecutionRunner` |
| [`backend/internal/orchestration`](../backend/internal/orchestration) | 将已提交的 execution 激活到当前进程内的 Agent runtime |
| [`backend/internal/tools`](../backend/internal/tools) | 工具注册、工具契约、输入规范化和 `bash`、`read_file`、`list_dir` 工具 |
| [`backend/internal/infra`](../backend/internal/infra) | 所有本地基础设施适配器和外部模型 Provider 适配器 |
| [`backend/internal/compose`](../backend/internal/compose) | 生产组合根，打开数据根、注册依赖、创建服务和关闭资源 |
| [`backend/wails`](../backend/wails) | Wails 生命周期、binding、DTO、错误映射和桌面事件出口 |

### 应用服务边界

[`internal/service`](../backend/internal/service) 通过窄接口组合用例：

- `service/project` 创建和查询 Project、Workspace。
- `service/session` 创建和查询 Session，并构建发送给 execution 的上下文。
- `service/agent` 查询 Agent、execution 和 transcript 展示数据。
- `service/execution/start` 创建用户输入或恢复 execution，冻结模型、上下文和安全输入快照。
- `service/execution/queue` 创建独立排队任务，并按 Agent FIFO 启动任务。
- `service/execution/control` 先记录 Pause/Close 控制命令，再通知进程内 runtime 取消。
- `service/execution/settlement` 在 transcript settlement receipt 持久化后，以一个 SQLite transaction 结算 execution、Agent、QueuedWork 和控制命令。
- `service/services.go` 实现 Wails 层需要的前端服务集，负责参数转换、结果投影和瞬时事件订阅。

用例的通用顺序是：

```text
校验请求
  → SQLite transaction 内检查幂等性并更新持久化状态
  → transaction commit
  → 调度当前进程内 runtime
```

runtime 激活失败不会撤销已经提交的命令或 execution；结果通过持久化状态和可重试的生命周期处理对外暴露。

## Agent execution

### 状态与并发

- `AgentExecution` 的状态为 `starting → running → settling → settled`。
- 一个 Agent 同时最多有一个 active execution；SQLite 的唯一索引和 Agent 状态共同约束这一点。
- 同一个 `(AgentID, RequestID)` 只能对应一次用户输入 execution。重复请求返回已有记录，参数不一致则返回 request conflict。
- `QueuedWork` 是独立任务，不是聊天邮箱。任务在真正启动时才冻结当前 Session context、模型和安全快照，并按 Agent sequence FIFO 运行。
- `Agent runtime` 是单进程、单 Agent 的活动执行槽。它只接受属于自身且已经持久化的 execution；关闭时取消活动 execution，并在给定 deadline 内等待退出。

### 执行链路

```text
Wails CommandBinding
  → service/execution/start 或 queue
  → repository transaction 创建 AgentExecution
  → orchestration.Scheduler
  → runtime/agent.Runtime
  → agent_runtime.ExecutionRunner
  → loop.ExecutionEngine
  → runtime.ModelStream + tools
  → transcript receipt
  → service/execution/settlement
  → SQLite 更新最终状态
```

`loop.ExecutionEngine` 只拥有一次 execution 的 model/tool turn loop，不拥有 Agent 状态或最终结算。每次工具调用都必须先写入可对账的 tool-result receipt，再构造下一轮模型上下文；模型或工具失败必须映射为稳定的 `ExecutionFailureCode`。

模型 Provider 通过 `runtime.ModelStream` 提供统一的流事件：文本增量、思考增量、工具调用、完成和错误。当前 Provider adapter 位于：

- `infra/providers/anthropicmessages`
- `infra/providers/openai_chat`
- `infra/providers/openai_responses`
- `infra/providers/streaming`

Provider credential 只在 `infra/model_registry` 内解析和使用，不进入 Wails DTO、execution snapshot 或 Agent runtime contract。

### Context 与 transcript

- `SessionContextEntry` 是按 revision 递增的 append-only 共享事实，类型包括用户消息、结论、决策和引用。
- `context.ContextBuilder` 按决策、已接受结论、引用、普通消息的优先级，在 Session 和 transcript 字节预算内构造 provider-neutral `ExecutionContext`。
- execution 创建时保存 context revision、transcript sequence、context digest、模型快照和安全快照；运行中不重新解释这些输入。
- transcript 归属于单个 `(SessionID, AgentID)`，runtime 不允许读取其他 Agent 的 transcript。
- 前端收到的 Agent event 只用于即时渲染；`settled` 之后必须重新读取 durable transcript 和 Agent view，不能把瞬时事件当作事实来源。

## 持久化边界

### 数据根

[`internal/infra/dataroot`](../backend/internal/infra/dataroot) 统一解析 `PRAXIS_DATA_ROOT`，默认使用用户目录下的 `.praxis`。目录职责如下：

| 路径 | 内容 |
|---|---|
| `config/models.json` | Provider、Group、Model 和默认模型配置 |
| `config/auth.json` | Provider credential，由 model registry 私有读取 |
| `config/tools.json` | 工具权限配置 |
| `agents/<definition-id>/` | `agent.json` 与 `AGENT.md` |
| `runtime/praxis.db` | SQLite 关系索引、状态和事务元数据 |
| `runtime/documents/` | 受引用的上下文、策略、execution 和工具文档数据 |
| `runtime/agent-sessions/<session-id>/<agent-id>.jsonl` | Agent append-only transcript 和生命周期 receipt |
| `runtime/attachments/` | 本地附件存储边界 |
| `runtime/tmp/` | 原子写入和临时文件 |

### SQLite 与文件文档

- [`infra/sqlite`](../backend/internal/infra/sqlite) 维护 schema migration、关系约束、索引和 SQLite transaction。它保存 Project、Workspace、Session、Agent、AgentExecution、SessionContextEntry、security snapshot、ToolInvocation、QueuedWork、WaitCondition 和 control command 的关系元数据。
- [`infra/storage`](../backend/internal/infra/storage) 把关系索引和文件文档组合为 repository 实现。Repository Port 不关心底层文档文件格式。
- [`infra/document`](../backend/internal/infra/document) 负责被 `document_ref` 引用的本地文档内容；大字段不直接塞入关系表。
- [`infra/agentlog`](../backend/internal/infra/agentlog) 负责单 Agent JSONL 的顺序号、header 校验、fsync、消息和 execution receipt。receipt durable 后，应用服务才推进 SQLite 结算。

关系状态与文档内容必须保持可校验的引用关系：SQLite 负责查询、唯一性、外键和状态并发约束，文件适配器负责内容完整性和 append-only 顺序。

## 配置与安全

- `infra/model_registry` 是模型配置的进程内唯一所有者，负责加载、校验、编辑配置、管理 credential、发现 Provider 模型，并在 execution 创建时生成不可变的 `ExecutionModelSnapshot`。
- `infra/agent_registry` 加载每个 AgentDefinition 的 `agent.json` 和 `AGENT.md`，校验默认模型引用，并生成初始 `AgentSecurityPolicy`。
- `security` 保存 Agent 的权限上限；`service/execution/start` 把当前策略冻结为 execution security snapshot，runtime 和工具执行只使用该快照。
- 工具必须先经过 registry 注册、配置校验和 execution 权限检查。工具 executor 不直接读取前端请求，也不绕过 sandbox、workspace scope 或 approval 规则。
- Provider payload、API key、原始错误和内部路径不进入 Wails 对外 DTO；binding 层只返回稳定结果、用户可处理的错误码和安全的展示投影。

## Wails 与前端

### 后端出口

[`backend/wails/bindings`](../backend/wails/bindings) 定义消费方窄接口和五组绑定对象：Project、Session、Agent、Command、Model。每个 binding 只取得 `BindingContext`，调用注入的 `internal/service.Services`，再将领域结果转换为 [`wails/dto`](../backend/wails/dto) 类型。

[`backend/wails/app.go`](../backend/wails/app.go) 保存 Wails context、注入服务集、转发 Agent event，并在关闭时按 runtime、store、日志的生命周期顺序释放资源。Wails 层不得成为业务状态或持久化状态的 owner。

### 前端结构

```text
frontend/src/
├── api/                    Wails binding wrapper、类型、错误和事件订阅
├── app/                    应用入口与错误边界
├── components/ui/          可复用的基础 UI 组件
├── features/
│   ├── projects/           Project 与 Session 列表、创建和概览
│   ├── sessions/           Agent 对话、消息、任务输入和控制
│   ├── agents/             Agent 状态面板
│   ├── settings/           Provider、Model 和配置编辑
│   └── workspace/          页面组合、导航和选中状态
└── styles/                 全局与 workspace 样式
```

`frontend/src/api` 是 React feature 与 Wails generated binding 之间的唯一 API 适配边界。页面通过 API wrapper 发起命令和查询，通过 `praxis:agent-event` 接收即时事件，并在 execution 结算后重新查询 durable 数据。

## 组合与生命周期

[`internal/compose/application.go`](../backend/internal/compose/application.go) 按以下顺序创建生产应用：

1. 解析并初始化 `DataRoot`。
2. 打开 SQLite、运行 schema migration 和 runtime logger。
3. 加载 model registry、tool config、tool registry、Agent registry 和 document store。
4. 组合 repository、Agent runtime runner、runtime registry、scheduler、execution services、Project/Session services 和前端服务集。
5. 由 [`backend/main.go`](../backend/main.go) 将服务集注入 Wails，并注册前端 assets 和 binding。

关闭时先停止 Agent runtime，随后关闭 SQLite 和 runtime logger。所有后台 goroutine 都必须由 runtime 或其持有的 context 管理，不能由 Wails binding 临时创建无法回收的执行线程。

## 验证边界

- 领域状态转换和不变量在领域包测试中验证。
- Context digest、预算、transcript 序号和 receipt 顺序在 `context`、`loop` 和 `infra/agentlog` 测试中验证。
- SQLite 外键、唯一索引、migration 和 repository transaction 在 `infra/sqlite` 测试中验证。
- Provider 请求编码和流事件归一化在各 Provider adapter 测试中验证。
- Wails binding 只验证 DTO、错误映射、binding 不可用和服务注入行为；React feature 测试不直接依赖 Go 内部实现。
