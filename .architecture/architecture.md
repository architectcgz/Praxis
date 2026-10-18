# Praxis 架构规则

本文档只规定架构规则：分层与依赖方向、Port 归属、契约包边界、内核内部顺序，以及这些规则的执行方式。产品结构、数据布局、协议细节和验证流程不在本文档范围内，以 `backend/`、`frontend/` 和 `backend/scripts/check-dependencies.sh` 为准。

## 一、分层与依赖方向

依赖只能指向更内层（表中更靠上）。同层不新增依赖；允许的同层连接只有三类：父包与子包之间的值类型复用、调用方声明的 Port，以及本节表中明确列出的方向。

| 层 | 包 | 允许依赖 |
| --- | --- | --- |
| 基元 | `core/identity`、`core/fault`、`core/tool`、`core/security`、`system`、`utils/*`、`logging` | 无，仅标准库 |
| 内核值 | `core/context`、`core/context/compress`、`core/project`、`core/workspace`、`core/workflow` | 基元；同层只允许 `core/context/compress → core/context` |
| 内核模型 | `core/model/config`、`core/model`、`core/session` | 基元、内核值；同层只允许 `core/model → core/model/config` |
| 内核业务 | `core/task`、`core/agent`、`core/tool_execution` | 基元、内核值、内核模型 |
| 内核能力 | `repository`、`hooks` | 内核各层 |
| 基础设施 | `infra/*` | 内核各层、`repository`、`infra/*` 内部、第一节白名单 |
| 工具 | `tools/contracts`、`tools/shared`、`tools/{bash,read_file,apply_patch}`、`tools` | 基元、内核业务、`tools/*` 内部 |
| 执行 | `timing`、`tool_execution`、`loop`、`execution` | 基元、内核各层、内核能力、工具；同层只允许 `execution → loop` |
| 业务基础 | `service/{project,session,agent,model,task,runtime/queue}` | 基元、内核各层、内核能力、工具、执行 |
| 业务编排 | `service/runtime` | 业务基础及其允许项 |
| 输入 | `request` | 内核各层、`app/model/contracts`（见白名单） |
| 应用 | `app/*`、`app/*/contracts` | 内核各层、执行、业务、输入、自身和其他 `app/*/contracts`；契约只依赖标准库和其他契约 |
| 组合与传输 | `compose`、`wails/*`、`main` | 全部 |

包路径省略前缀 `internal/`。两条分层顺序无法表达、必须单独成立的约束：

- 除 `compose`、第一节白名单和 `main → infra/dataroot` 外，任何包不得 import `infra/*`。`repository` 与 `tools/contracts` 是内层实现对外暴露的唯一接口。
- `service/...` 不得 import `app`、`request`、`execution`、`loop`、`compose`、`wails`；`app/...` 不得 import `infra`、`repository`、`compose`、`wails`。`main` 只负责桌面生命周期和数据根解析，不参与业务装配。

允许的反向引用只有四处。新增反向引用必须先修改本节，并在门禁中列入白名单：

| 反向引用 | 理由 |
| --- | --- |
| `infra/workspacefs → service/session` | 实现 `WorkspaceTextReader`，需要该 Port 的哨兵错误 |
| `infra/jsonl → timing` | 实现 `timing.Store`，需要 `timing.Record` |
| `infra/toolconfig → tools/contracts` | 实现 `ToolCatalog`，需要契约值类型 |
| `request → app/model/contracts` | 输入层复用公开值类型；不得引用应用入口 |

## 二、Port 归属

- Port 由调用方声明、被调用方实现。声明方是发起调用的一侧，与包所在层无关：内层可以声明由外层实现的 Port，外层也可以声明由内层实现的 Port。
- 不为单一实现机械增加接口，不新增中间包；窄接口只声明消费方真正使用的能力，同一能力被多处消费时各自声明自己需要的部分。
- 实现方不得反向 import 声明方。需要引用 Port 的类型或哨兵错误时，只能按第一节白名单执行。
- 组合根负责把实现注入声明方。现成实现不匹配时由组合根定义适配器，不在业务包里做转换。

| 声明方 | Port | 实现方 |
| --- | --- | --- |
| `loop` | `ToolExecutor` | `tool_execution.Executor` |
| `loop` | `TaskProgress` | `service/task.Service` |
| `loop` | `ToolExecutionReader` | `infra/jsonl` |
| `execution` | `TaskOperations` | `service/task.Service` |
| `execution` | `TaskQueue` | `service/runtime/queue.Service` |
| `execution` | `RecoverySource` | `compose` |
| `service/task` | `ExecutionSlots` | `execution.ActiveTasks` |
| `service/task` | `PrimaryAgentProvider` | `service/session.Service` |
| `service/task` | `AgentDefinitionProvider`、`ModelConfigResolver` | `compose` 的注册表适配器 |
| `service/runtime` | `TaskActivator` | `execution.Coordinator` |
| `service/runtime` | `TaskStarter` | `service/task.Service` |
| `service/agent` | `ExecutionControl` | `execution.ActiveTasks` |
| `service/session` | `WorkspaceTextReader` | `infra/workspacefs.Filesystem` |
| `service/project` | `WorkspaceDirectory` | `infra/workspacefs.Filesystem` |
| `timing` | `Store` | `infra/jsonl.TimingStore` |
| `repository` | 各聚合仓储、`MessageStreams`、`MessageWriter`、`TxRunner` | `infra/jsonl` |
| `tools/contracts` | `Tool`、`ToolCatalog` | `tools/{bash,read_file,apply_patch}`、`infra/toolconfig.Catalog` |
| `tool_execution` | `ApprovalCatalog`、本地窄接口 `TaskRepository` | `infra/toolconfig.Catalog`、`infra/jsonl` |

## 三、契约包边界

新增 `contracts` 包必须同时满足三条，缺任意一条就直接使用所有者包的类型：

1. 两侧由不同所有者维护、需要独立演进（例如前端协议与内核实体）。
2. 契约形状必须与内核形状不同：要裁剪字段、重命名，或不暴露内核枚举和执行配置。
3. 契约只依赖标准库和其他契约包，不含行为。

- 满足三条的只有 `app/<业务>/contracts`：它面向 Wails/前端协议，必须过滤内核字段（opaque 推理状态不进入公开消息、摘要和前端通知），并由门禁强制只依赖标准库。
- 内核内部共享同形值类型时，直接使用所有者包的类型：`core` 各包之间、`core` 与 `infra`/`loop` 之间不做包装、不做逐字段复制。`core` 及其子包内不得出现 `contracts` 包。
- 契约包之间可以互相依赖，但不得成环；owner 按数据所属业务线确定，不按页面需要确定。
- 契约只包含公开数据：身份用字符串，状态用稳定值，不暴露内核实体、枚举和执行配置；可变切片和计数指针在契约边界复制。

## 四、内核内部顺序

`core` 只是目录分组，不设统一入口或转发层。包内固定四档，依赖只能从后档指向前档：

| 档 | 包 | 同档内允许 | 禁止依赖 |
| --- | --- | --- | --- |
| 基元 | `identity`、`fault`、`tool`、`security` | 无 | 其他任何 `core` 包 |
| 值/规则 | `context`、`context/compress`、`project`、`workspace`、`workflow` | `context/compress → context` | `model`、`session`、`task`、`agent`、`tool_execution` |
| 模型与会话 | `model/config`、`model`、`session` | `model → model/config` | `task`、`agent`、`tool_execution` |
| 任务与执行 | `task`、`agent`、`tool_execution` | `agent → task` | 无额外限制 |

推论：`session` 引用 `context` 的 opaque 推理状态、`model` 引用 `context` 的通用上下文类型、`task` 引用 `model` 的冻结配置都落在规则内。

## 五、规则的执行方式

规则由 `backend/scripts/check-dependencies.sh` 在执行 `go vet ./...` 之后强制：用 `go list -f '{{.ImportPath}} {{join .Imports " "}}'` 枚举生产包的直接 import，匹配到禁止前缀即失败；`go list` 失败和 `rg` 工具错误必须原样失败，不得当成"无匹配"放过。

门禁必须覆盖以下边界：

- `wails/...` 不依赖 `service`、`core`、`execution`、`loop`、`timing`、`repository`、`infra`。
- `app/...` 不依赖 `infra`、`execution`、`loop`、`repository`、`compose`、`wails` 或直接文件系统操作；`app/*/contracts` 只依赖标准库和其他契约。
- `request/...` 不依赖 `service`、`repository`、`infra`、`execution`、`loop`、`compose`、`wails` 或应用入口包，只允许 `app/*/contracts`。
- `service/...` 不依赖 `app`、`request`、`infra`、`execution`、`loop`、`compose`、`wails` 或 `os`。
- `loop/...` 不依赖 `app`、`service`、`execution`；`execution/...` 不依赖 `app`、`service`。
- `infra/...` 不依赖 `app`、`request`、`execution`、`loop`、`compose`、`wails`，且除第一节白名单外不依赖 `service`。
- `repository/...`、`hooks/...`、`utils/...`、`system/...`、`tools/...` 只向内依赖，不依赖 `service`、`app`、`request`、`infra`、`execution`、`loop`、`compose`、`wails`。
- `core/...` 不依赖 `app`、`service`、`request`、`repository`、`infra`、`execution`、`loop`、`compose`、`hooks`、`tools`，并按第四节逐档禁止内核内部反向依赖。
- `core/context` 不得依赖 `core/task`、`core/session`、`core/agent`；`core/model` 不得依赖 `tools/contracts`；`infra/jsonl` 不得依赖 `hooks`。
- 全仓不得依赖集中契约入口。

规则变更必须同步本节和 `check-dependencies.sh`。
