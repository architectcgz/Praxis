# Praxis 架构

本文档描述 Praxis 当前目标状态下的产品边界、运行时分层、核心数据关系、持久化边界和前后端通信边界。架构文档只描述可被代码直接验证的目标状态；具体实现以 `backend/` 和 `frontend/` 为准。

## 产品边界

Praxis 是面向个人学习者的本地多 Agent 编排桌面应用：

- 技术形态为 Go + Wails + React，所有核心能力在本机进程内运行，不依赖远程 Praxis 服务端。
- `Project` 管理项目，项目可以包含多个 `Workspace`；`Session` 绑定一个项目工作区并承载一次持续协作。
- `Agent` 是 Session 内的运行实例，绑定一个可复用的 `AgentDefinition`。默认定义包括 `primary`、`delegate`、`advisor` 和 `curator`。
- `Agent` 通过持久化的 `Turn` 执行用户输入、排队任务或恢复操作。执行期间可以调用受安全策略约束的工具。
- Session 共享上下文与每个 Agent 独立的 transcript 分开管理。共享上下文用于协作事实，transcript 用于保留单个 Agent 的完整对话和执行记录。

核心关系如下：

```text
Project
└── Workspace
    └── Session
        ├── Agent ── AgentDefinition
        ├── SessionContextEntry
        └── Turn ── ToolInvocation / QueuedWork / WaitCondition
```

## 运行时分层

运行调用与对象装配分别组织。前端调用链如下：

```text
React frontend
    │ Wails bindings / events
    ▼
backend/wails
    │ 应用服务接口
    ▼
internal/service
    │ 事务提交后激活
    ▼
internal/agent_runtime
    │ 注入的 TurnRunner
    ▼
internal/loop
    │ 模型与工具调用接口
    ├── internal/infra/providers
    └── internal/service/turn/tool_invocation
```

Go import 方向如下；`compose` 负责创建并连接具体对象，不参与业务执行链：

```text
internal/compose ──→ service / agent_runtime / loop / infra / core / tools/contracts
internal/service ──→ repository / agent_runtime / core / tools/contracts
internal/repository ──→ core / contracts
internal/loop ──→ agent_runtime / core / tools/contracts
internal/infra ──→ repository / core / tools/contracts
internal/agent_runtime ──→ core / tools/contracts
internal/core/model ──→ core/context / contracts / tools/contracts
internal/core 各业务包 ──→ core 内部依赖 / contracts / tools/contracts / utils/pathutil
```

依赖规则：

1. `internal/contracts` 提供跨包共享的 ID、错误、安全快照和协议值类型；不依赖业务实现。
2. `internal/core` 统一组织业务核心，包含 `agent`、`model`、`project`、`session`、`workspace`、`task`、`turn`、`context`、`workflow`、`security` 和 `tool_invocation`。各子包保存业务对象及其校验、状态转换与不变量；不依赖 `service`、`infra`、`agent_runtime`、`loop` 或 `compose`。`core` 仅作为目录分组，各业务包保持独立的 Go package，不设统一入口或转发层。
3. 接口与相关能力放在同一个包内：`internal/core/model` 定义模型快照、模型构建、请求、流事件和用量记录；`internal/core/model/config` 定义共享模型配置类型、模型目录、配置编辑接口和校验错误，供注册表、应用服务、Agent 配置校验和 Wails binding 使用；`internal/core/task` 保存 runtime 从任务队列取出并执行的任务及其生命周期；`internal/tools/contracts` 定义工具目录和工具调用契约；`internal/agent_runtime` 定义执行协调、持久化工具调用、消息、生命周期回调和 Agent 事件类型；`internal/repository` 定义持久化访问接口。接口不暴露存储引擎、文件格式、Provider 协议或 Wails 类型。
4. `internal/service` 实现产品用例，负责输入校验、幂等检查、事务内状态变更和事务提交后的 runtime 激活。
5. `internal/infra` 实现本地存储、配置加载与模型 Provider，工具执行器由 `internal/tools` 提供；这些实现可以依赖领域模型与能力接口，但领域模型不依赖具体实现。
6. `internal/compose` 是唯一的生产组合根，负责创建具体实现、注入依赖和释放进程级资源。`compose/agent_runtime.go` 集中连接 Runtime、模型 Provider、执行循环和工具调用服务。
7. `backend/wails` 只负责桌面生命周期、DTO、错误映射、binding 和事件转发；它不直接访问 repository 或文件存储。
8. `internal/agent_runtime` 管理执行协调，不 import `loop`、`service`、`infra` 或 `compose`。loop 和应用服务使用其执行接口与 Agent 事件；模型 Provider 使用 `internal/core/model`，具体对象由 `compose` 注入。

## 后端模块

| 路径 | 职责 |
|---|---|
| [`backend/internal/contracts`](../backend/internal/contracts) | 跨领域 ID、错误码、安全快照、权限和共享协议类型 |
| [`backend/internal/core/agent`](../backend/internal/core/agent) | Agent、AgentDefinition 及 Agent 状态转换 |
| [`backend/internal/core/model`](../backend/internal/core/model) | 模型快照及校验、模型构建接口、Provider 请求与流事件、token 用量及持久化记录 |
| [`backend/internal/core/model/config`](../backend/internal/core/model/config) | 共享模型配置、Provider 和 Group 类型、模型目录及配置编辑接口；不包含凭据 |
| [`backend/internal/core/task`](../backend/internal/core/task) | runtime 任务队列中的任务、输入快照和执行生命周期 |
| [`backend/internal/core/project`](../backend/internal/core/project) | Project 模型 |
| [`backend/internal/core/workspace`](../backend/internal/core/workspace) | Workspace 模型与工作区边界 |
| [`backend/internal/core/session`](../backend/internal/core/session) | Session 模型、标题和归档状态 |
| [`backend/internal/core/turn`](../backend/internal/core/turn) | Turn、输入快照、执行状态与结算结果 |
| [`backend/internal/core/context`](../backend/internal/core/context) | Session 共享上下文、transcript 投影、上下文预算与 digest |
| [`backend/internal/core/workflow`](../backend/internal/core/workflow) | QueuedWork、WaitCondition、AgentControlCommand |
| [`backend/internal/core/security`](../backend/internal/core/security) | Agent 权限上限、Sandbox、Approval 和工具策略 |
| [`backend/internal/core/tool_invocation`](../backend/internal/core/tool_invocation) | 工具调用持久化身份、状态转换、结果及恢复不变量 |
| [`backend/internal/repository`](../backend/internal/repository) | 持久化、事务与一致消息读取接口 |
| [`backend/internal/service`](../backend/internal/service) | Project、Session、Agent 和 turn 的应用服务，以及 Wails 前端服务集 |
| [`backend/internal/loop`](../backend/internal/loop) | 单次 turn 的 model/tool step loop、预算和 Provider 事件归一化 |
| [`backend/internal/agent_runtime`](../backend/internal/agent_runtime) | Agent 执行协调、Runtime 注册与关闭，以及持久化工具调用、消息和 Agent 事件接口 |
| [`backend/internal/tools`](../backend/internal/tools) | 工具注册、工具契约、输入规范化和 `bash`、`read_file` 工具 |
| [`backend/internal/infra`](../backend/internal/infra) | 所有本地基础设施适配器和外部模型 Provider 适配器 |
| [`backend/internal/compose`](../backend/internal/compose) | 生产组合根，打开数据根、注册依赖、创建服务和关闭资源 |
| [`backend/wails`](../backend/wails) | Wails 生命周期、binding、DTO、错误映射和桌面事件出口 |

### 应用服务边界

[`internal/service`](../backend/internal/service) 通过窄接口组合用例：

- `service/project` 创建和查询 Project、Workspace。
- `service/session` 创建和查询 Session，并构建发送给 turn 的上下文。
- `service/agent` 查询 Agent、turn 和 transcript 展示数据。
- `service/turn/start` 创建用户输入或恢复 turn，冻结模型、上下文和安全输入快照。
- `service/turn/queue` 创建独立排队任务，并按 Agent FIFO 启动任务。
- `service/turn/control` 先记录 PauseAgent/CancelTurn 控制命令，再通知进程内 runtime 取消指定 Turn；取消不关闭 Agent。
- `service/turn/lifecycle` 接收 runtime 的开始与结束回调，将 Turn 推进到运行态，并以一个 JSONL 提交 Turn、Agent、QueuedWork 和控制命令的最终状态。
- `service/turn/tool_invocation` 负责工具调用的持久化准入、幂等检查、授权和结果结算；通过注入的工具目录执行已授权调用。
- `core/tool_invocation` 定义工具调用状态模型，仅依赖共享 contracts。恢复时，未启动的调用结算为 `interrupted`，运行中但结果无法确认的调用结算为终态 `unknown`，不得自动重放。
- `service/session.MessageStore` 按 Agent 的可见范围路由消息；一致读取接口由 `repository.MessageLoader` 定义，执行消息接口由 `agent_runtime.TurnMessageStore` 定义。
- `service/services.go` 实现 Wails 层需要的前端服务集，负责参数转换、结果投影和瞬时事件订阅。

用例的通用顺序是：

```text
校验请求
  → JSONL 事务工作区内检查幂等性并更新持久化状态
  → transaction commit
  → 调度当前进程内 runtime
```

runtime 激活失败不会撤销已经提交的命令或 turn；结果通过持久化状态和可重试的生命周期处理对外暴露。

## Agent turn

### 状态与并发

- `Turn` 的状态为 `starting → running → ending → ended`。
- `ended` 固定最终 `Outcome`、`FailureCode` 和 `EndedAt`；`Outcome` 区分完成、失败、暂停和中断，Agent 中断后仍可接收新输入。
- 一个 Agent 同时最多有一个 active turn；日志投影的唯一性校验和 Agent 状态共同约束这一点。
- 同一个 `(AgentID, RequestID)` 只能对应一次用户输入 turn。重复请求返回已有记录，参数不一致则返回 request conflict。
- `QueuedWork` 是独立任务，不是聊天邮箱。任务在真正启动时才冻结当前 Session context、模型和安全快照，并按 Agent sequence FIFO 运行。
- `Agent runtime` 是单进程、单 Agent 的活动执行槽。它只接受属于自身且已经持久化的 turn；关闭时取消活动 turn，并在给定 deadline 内等待退出。
- 应用启动时将持久化的未结束 Turn 和 Agent 收敛为 `interrupted`；存在已提交的控制命令时保留暂停或取消结果。尚未运行的工具调用结算为 `interrupted`，已经运行但结果不明的调用结算为 `unknown`，不得自动重放。

### 执行链路

```text
Wails CommandBinding
  → service/turn/start 或 queue
  → repository transaction 创建 Turn
  → agent_runtime.Registry.Activate
  → agent_runtime.Runtime
  → agent_runtime.TurnRunner
  → loop.TurnEngine
  → core/model.ModelStream + service/turn/tool_invocation
  → 执行结果回调
  → service/turn/lifecycle
  → JSONL 提交最终状态
```

`loop.TurnEngine` 只拥有一次 Turn 的 model/tool step loop，不拥有 Agent 状态或最终结束事务。每次工具调用都必须先写入可对账的 tool-result receipt，再构造下一轮模型上下文；模型或工具失败必须映射为稳定的 `TurnFailureCode`。

控制命令使用 `CommandID` 保证幂等，使用 `AgentID + TargetTurnID` 固定取消目标；旧回合请求不会取消下一轮。结束事务优先应用此前已提交的控制命令，并保存 `FailureCode=request_canceled`，与系统中断区分。

生命周期服务只在首次结束事务提交成功后发布一个终态事件：用户暂停或取消为 `request_canceled`，其他结果为 `turn_ended`。两者均携带 `SessionID`、`AgentID`、`TurnID`、`Outcome`、`FailureCode` 和安全的失败详情；runtime 不另行发布终态事件。

模型构建由 `core/model.ModelBuilder` 定义，`infra/model_registry.Registry` 通过 `BuildModel` 根据冻结的 `core/model.ModelSnapshot` 创建 `core/model.Model`。`loop` 和计时包装器直接使用该契约。`core/task` 描述 runtime 取出并执行的队列任务，不负责模型构建或用量统计。

模型 Provider 通过 `core/model.ModelStream` 提供统一的流事件：文本增量、思考增量、工具调用、用量、完成和错误。当前 Provider adapter 位于：

- `infra/providers/anthropicmessages`
- `infra/providers/openai_chat`
- `infra/providers/openai_responses`
- `infra/providers/streaming`

Provider credential 由 `infra/model_registry` 私有解析并传入 Provider 工厂，不进入 Wails DTO、turn snapshot 或模型执行契约。`core/model.ModelUsage` 和 `ModelUsageRecord` 统一供 Provider、Agent 事件、用量存储及 Wails 用量查询使用。

### Context 与 transcript

- `SessionContextEntry` 是按创建时间和 ID 排序的 append-only 共享事实，类型包括用户消息、结论、决策和引用。
- `context.ContextBuilder` 按决策、已接受结论、引用、普通消息的优先级，在 Session 和 transcript 字节预算内构造 provider-neutral `ModelContext`。
- Turn 的 `InputSnapshot` 保存 `ModelContext`、消息边界、当前输入消息 ID、context digest、模型快照、安全快照和工作区；运行中不重新解释这些输入。
- 主 Agent 读取 Session 消息，协作 Agent 读取自身私有消息；共享文件不扩大消息可见范围。
- 前端收到的 Agent event 只用于即时渲染；`turn_ended` 或 `request_canceled` 之后必须重新读取持久化 transcript、Agent view、计时和用量，不能把瞬时事件当作事实来源。

## 持久化边界

### 数据根

[`internal/infra/dataroot`](../backend/internal/infra/dataroot) 统一解析 `PRAXIS_DATA_ROOT`，默认使用用户目录下的 `.praxis`。目录职责如下：

| 路径 | 内容 |
|---|---|
| `config/models.json` | Provider、Group、Model 和默认模型配置 |
| `config/auth.json` | Provider credential，由 model registry 私有读取 |
| `config/tools.json` | 工具权限配置 |
| `config/agents/<definition-id>/` | `agent.json` 与 `AGENT.md` |
| `runtime/projects.jsonl` | Project 与 Workspace 的提交日志 |
| `runtime/sessions/<session-id>.jsonl` | Session 内的全部运行事实，文件名提供 Session 身份 |
| `runtime/writer.lock` | 数据根单写入进程锁 |
| `runtime/documents/` | 独立的内容寻址文档 |
| `runtime/attachments/` | 本地附件存储边界 |
| `runtime/tmp/` | 原子写入和临时文件 |

### JSONL 与内存投影

- [`infra/jsonl`](../backend/internal/infra/jsonl) 实现全部业务仓储与 `TxRunner`。每行包含一个事务的有序事件，同一事务只写入一个日志文件，持久化成功后才发布内存投影。
- Session ID 只从文件名解析，事件和 payload 不重复保存。事件不带格式版本字段。
- Store 校验引用、唯一性、冻结输入和终态不变量；操作系统文件锁保证单进程写入，进程内锁串行提交。
- 启动 replay 重建 Session 目录、消息、Turn、工具调用和队列；标题查询使用内存目录，不扫描消息正文。
- 没有换行的尾部提交先留存诊断副本，再截断到最后一个完整提交。完整损坏行与非法引用会阻止启动。
- 删除 Session 追加删除标记并清除关联投影，物理日志清理由独立操作处理。
- [`infra/document`](../backend/internal/infra/document) 提供独立内容寻址文件存储；凭据与模型配置各有独立 owner。

## 配置与安全

- 模型配置调用链为 `Wails binding → wails/validation.PrepareModelConfig → service.Services → core/model/config.ConfigManager → infra/model_registry.Registry`。Wails 接收前端配置请求，规范化后将 `ValidatedConfig` 交给 service；service 使用 canonical 配置校验 Agent 模型引用，再协调保存和记录日志。
- `wails/validation.PrepareModelConfig` 是模型配置的唯一规范化 owner，负责复制请求、Trim 字段、补展示名称和 DTO 转换；规范化 Provider ID 后再检查请求内引用，防止模型在转换时丢失。Wails 输入层通过 `core/model/config.NewValidatedConfig` 构造不可外部修改的 `ValidatedConfig`。
- `core/model/config.Config.Validate` 只读检查 canonical 值、结构、引用、能力预算和推理等级；业务层不执行 Trim 或补默认值。构造成功后不重复校验完整配置，后续加载、索引和模型构建直接使用 canonical 值。
- 配置文件恢复通过 `core/model/config.NewValidatedConfig` 只读校验已持久化的 canonical 数据，非法或非 canonical 值直接报错，不清洗字段或静默修复。
- 注册表的 `Save` 只接收已准备配置，负责原子持久化和更新内存索引；零值配置被拒绝，不重复规范化或业务校验。`Config` 返回独立副本。Wails 输入层将配置校验失败映射为共享的 `ValidationError`，持久化错误保留原始分类；URL 校验和代理解析只读验证 canonical 值。
- `infra/model_registry` 是模型配置的进程内唯一所有者，负责加载、校验、编辑配置、管理 credential、发现 Provider 模型，并在 task 激活时生成不可变的 `core/model.ModelSnapshot`。凭据类型和持久化索引只存在于注册表内部。
- `infra/agent_registry` 加载每个 AgentDefinition 的 `agent.json` 和 `AGENT.md`，校验默认模型引用，并生成初始 `AgentSecurityPolicy`。
- `core/security` 保存 Agent 的权限上限；`service/turn/start` 把当前策略冻结为 turn security snapshot，runtime 和工具执行只使用该快照。
- 工具必须先经过 registry 注册、配置校验和 turn 权限检查。工具 executor 不直接读取前端请求，也不绕过 sandbox、workspace scope 或 approval 规则。
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
│   ├── sessions/           Session 页面外壳、空状态和会话管理命令
│   ├── agents/             Agent 查询、对话、任务输入和控制命令
│   │   └── output/         消息、Markdown、工具结果与协作结果呈现
│   ├── settings/           设置总览
│   │   └── model-config/   Provider、Model 和配置编辑
│   ├── timing/             操作耗时查询、事件合并与展示
│   └── workspace/          页面组合、导航和选中状态
├── shared/                 跨功能的非页面逻辑
└── styles/                 全局 token、公共样式与 Workspace 样式入口
```

`frontend/src/api` 是 React feature 与 Wails generated binding 之间的唯一 API 适配边界。页面通过 API wrapper 发起命令和查询，通过 `praxis:agent-event` 接收即时事件，并在 Turn 结束后重新查询持久化数据。

`agents/useAgentData.ts` 管理查询、缓存、选中状态及过期响应保护，组合 `useAgentTaskInput.ts` 管理草稿、模型和 reasoning 选择。`useAgentCommands.ts` 执行输入与控制命令；`streaming.ts` 以纯函数合并实时事件并在持久化回填后去重。乐观消息保留发送时的 Session / Agent 归属，视图切换不得把消息展示到其他对话。

全局 token 与基础样式由 `styles/` 拥有，功能样式就近归属。Agent 对话容器与输出内容的样式分别由 `agents/conversation.css`、`agents/output/conversation.css` 管理；`styles/workspace.css` 是 Workspace 样式的唯一加载入口。

## 组合与生命周期

[`internal/compose/application.go`](../backend/internal/compose/application.go) 按以下顺序创建生产应用：

1. 解析并初始化 `DataRoot`。
2. 打开 runtime logger 和 JSONL Store，获取写入锁并 replay 日志。
3. 加载 model registry、tool config、tool registry、Agent registry 和 document store。
4. 创建 repository 和应用服务；由 [`compose/agent_runtime.go`](../backend/internal/compose/agent_runtime.go) 组装默认 runner、Provider 和 Runtime factory，将 Registry 注入启动与控制服务，再组装前端服务集。
5. 由 [`backend/main.go`](../backend/main.go) 将服务集注入 Wails，并注册前端 assets 和 binding。

关闭时先停止 Agent runtime，随后关闭 JSONL Store 和 runtime logger。所有后台 goroutine 都必须由 runtime 或其持有的 context 管理，不能由 Wails binding 临时创建无法回收的执行线程。

## 验证边界

- 业务状态转换和不变量在 `core` 各业务包测试中验证。
- Context digest、预算和 transcript 序号在 `core/context` 与 `loop` 测试中验证；执行状态回调和取消结算在 `agent_runtime` 与应用服务测试中验证。
- JSONL 的验证范围包括事务回滚、序号、身份隔离、唯一性、工具幂等和重启恢复。
- Provider 请求编码和流事件归一化在各 Provider adapter 测试中验证。
- Wails binding 只验证 DTO、错误映射、binding 不可用和服务注入行为；React feature 测试不直接依赖 Go 内部实现。
