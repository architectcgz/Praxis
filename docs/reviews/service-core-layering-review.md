# Service / Core 分层 Review

## 结论

有不符合分层的情况，主要不是运行时调用顺序，而是编译依赖和数据契约越过了边界。

当前调用路径多数确实是 `Wails -> internal/service`，但 `backend/wails` 仍直接依赖 `core`、`agent_runtime` 和 `timing`，因此“通过 service 调用”尚未等于“Wails 与 core 解耦”。另外，`internal/service` 直接执行文件系统操作，`core/model` 依赖包含工具执行接口的 `tools/contracts`，这两处也使边界变得模糊。

当前工作树包含用户本地修改：删除 `backend/internal/core/model/runtime.go`，并新增拆分后的 model 文件。本文按当前文件树检查，不修改这些文件，也不把该本地改动本身作为分层问题。

## 做得比较好的部分

- 当前 `internal/core` 没有反向 import `service`、`infra`、`agent_runtime`、`loop` 或 `compose`，内核没有直接依赖具体存储和 Provider 实现。
- `repository` 作为持久化 Port，`infra/jsonl` 负责实现，`compose` 负责装配，这条主方向是清楚的。
- `Wails binding` 的运行时调用基本通过注入的 service 集完成，未发现 Wails 直接访问 repository 或 JSONL store。
- `core` 里的状态转换和不变量仍集中在各自业务包，没有把状态机散落到 Wails 层。

## 问题

### 高：Wails binding 的接口签名直接暴露 core 类型

位置：

- [`backend/wails/bindings/services.go:8-21`](../../backend/wails/bindings/services.go#L8)
- [`backend/wails/bindings/services.go:29-90`](../../backend/wails/bindings/services.go#L29)
- [`backend/wails/bindings/bindings_agent.go:5-7`](../../backend/wails/bindings/bindings_agent.go#L5)
- [`backend/wails/bindings/bindings_session.go:5`](../../backend/wails/bindings/bindings_session.go#L5)
- [`backend/wails/bindings/bindings_usage.go:4-21`](../../backend/wails/bindings/bindings_usage.go#L4)

`ProjectService`、`SessionService`、`AgentService`、`UsageService` 的返回值包含 `core/project.Project`、`core/session.Session`、`core/agent.Agent`、`core/task.Task`、`core/model.ModelUsageRecord` 等类型。binding 实现还直接使用 `core/agent.DisplayName`、`core/task.TaskEnded`、`core/session` 的消息角色和 block 类型。

这与架构文档中“binding 调用 service，再把领域结果转换为 Wails DTO”的要求不一致。当前虽然调用链是 `Wails -> service`，但 import graph 仍然是 `Wails -> core`。结果是 core 的字段、枚举和聚合结构变成了桌面层的编译契约，后续修改 core 会直接波及 Wails；同时 Wails 具备绕过 application service 读取和解释领域状态的能力。

建议：

- `backend/wails` 只依赖 `contracts` 和 `internal/service` 暴露的 application request/view 类型，不再 import `internal/core/*`。
- 在 `service/project`、`service/session`、`service/agent`、`service/runtime` 中提供面向用例的查询结果和命令参数，字段使用稳定的 ID、string、time 和简单集合；内部仍可自由使用 core entity。
- Wails binding 只做 DTO 转换和错误映射，不再判断 core 枚举或从 core entity 组装前端视图。

### 高：runtime event 原样穿过 Wails 边界

位置：

- [`backend/wails/bindings/bindings.go:7`](../../backend/wails/bindings/bindings.go#L7)
- [`backend/wails/bindings/bindings.go:52-61`](../../backend/wails/bindings/bindings.go#L52)
- [`backend/wails/bindings/services.go:77-80`](../../backend/wails/bindings/services.go#L77)
- [`backend/internal/agent_runtime/event.go:30-50`](../../backend/internal/agent_runtime/event.go#L30)

`Bindings.EmitAgentEvent` 接收并直接 `EventsEmit` 一个 `agentruntime.AgentEvent`。这个 event 内部又包含 `core/task.TaskOutcome`、`core/model.ModelUsage` 和 `timing.Record`。因此 runtime 的内部事件结构实际已经成为前端协议，Wails 也直接依赖 `agent_runtime` 和 `timing`。

建议定义 service-owned 的实时事件视图，例如只包含稳定字符串 ID、事件类型、文本、工具展示字段、失败码和用量展示字段；Wails 再把它映射为专用 `dto.AgentEvent`。runtime event 到 service event 的转换放在 service 或 compose 适配处，`backend/wails` 不应接收 `agentruntime.AgentEvent`。

### 中：Wails validation 负责 core 配置构造和领域约束

位置：

- [`backend/wails/validation/model_config.go:8-14`](../../backend/wails/validation/model_config.go#L8)
- [`backend/wails/validation/model_config.go:26-70`](../../backend/wails/validation/model_config.go#L26)
- [`backend/wails/validation/validation.go:12`](../../backend/wails/validation/validation.go#L12)
- [`backend/wails/validation/validation.go:110-126`](../../backend/wails/validation/validation.go#L110)
- [`backend/wails/bindings/bindings_model_config.go:55-77`](../../backend/wails/bindings/bindings_model_config.go#L55)

`PrepareModelConfig` 在 Wails 层构造 `core/model/config.Config`，并调用 `NewValidatedConfig`；队列 prompt 的最大长度还直接读取 `core/task.MaxInputBytes`。这已经不只是 DTO 形状校验，而是 application/core 输入规范化和领域校验。未来如果增加 CLI、自动化任务或其他桌面入口，规则很容易只在 Wails 路径生效。

建议保留 Wails 的字段存在性和 wire shape 校验，但把 canonical 化、大小限制、配置引用完整性和 `ValidatedConfig` 构造移动到 service 的入口。Wails 传入 transport-neutral request，service 返回 application validation error；这样输入规则只有一个 owner。

### 中：application service 直接执行文件系统操作

位置：

- [`backend/internal/service/services.go:17-18`](../../backend/internal/service/services.go#L17)
- [`backend/internal/service/services.go:141-160`](../../backend/internal/service/services.go#L141)
- [`backend/internal/service/session/file_preview.go:8-15`](../../backend/internal/service/session/file_preview.go#L8)
- [`backend/internal/service/session/file_preview.go:61-102`](../../backend/internal/service/session/file_preview.go#L61)

`service.Services.CreateProject` 直接调用 `os.Stat`、`os.MkdirAll`、`os.Remove`；`service/session` 直接调用 `os.OpenRoot` 读取工作区文件。这些是本地文件系统适配，不应由 application service 持有具体实现。当前架构又明确把本地基础设施归入 `internal/infra`，所以这里存在 service/infra 职责重叠。

建议保留 service 中的用例编排、路径授权和失败回收策略，注入窄的 `WorkspaceDirectory`、`WorkspaceTextReader` 等 Port，由 `infra` 实现并在 `compose` 装配。不要把 `os`、`OpenRoot` 或具体文件语义继续扩散到其他 service。

### 中：core/model 依赖包含执行器的 tools/contracts

位置：

- [`backend/internal/core/model/model_request.go:4-17`](../../backend/internal/core/model/model_request.go#L4)
- [`backend/internal/core/model/model_stream.go:4-28`](../../backend/internal/core/model/model_stream.go#L4)
- [`backend/internal/tools/contracts/call.go:23-35`](../../backend/internal/tools/contracts/call.go#L23)

`core/model` 只需要 provider-neutral 的 `ToolCall` 和 `ToolDefinition`，但当前依赖的 `tools/contracts` 同时定义了 `Tool.Execute`、`ToolCatalog`、`AuthorizedToolCall` 等执行侧能力。架构文档虽然把 `core -> tools/contracts` 列为允许方向，但该包实际不是纯数据契约，因此“tools/contracts 是中立 Port，还是工具执行层 API”并不清楚。

建议把模型侧需要的纯数据契约放到已有的 `internal/contracts` 或独立的中立 contracts 包；`Tool`、`ToolCatalog`、规范化和授权调用仍留在 `internal/tools/contracts`。这样 core 不会因为工具执行接口变化而变化。

### 中：service 的公开接口被 agent_runtime 类型污染

位置：

- [`backend/internal/service/runtime/task/turn/service.go:14-15`](../../backend/internal/service/runtime/task/turn/service.go#L14)
- [`backend/internal/service/runtime/task/turn/service.go:46-49`](../../backend/internal/service/runtime/task/turn/service.go#L46)
- [`backend/internal/service/runtime/task/lifecycle/service.go:21-31`](../../backend/internal/service/runtime/task/lifecycle/service.go#L21)
- [`backend/internal/service/runtime/task/tool_invocation/invoke.go:29-34`](../../backend/internal/service/runtime/task/tool_invocation/invoke.go#L29)

`service` 的生命周期、回合和工具调用用例直接把 `agentruntime.TurnParams`、`AgentEventObserver`、`ToolInvocationMetadata` 作为 API 类型。依赖方向按当前架构文档是允许的，但 application service 的公共接口因此绑定了进程内执行器的内部模型；它不再是可被其他入口复用的纯用例 API。

建议只保留窄的、业务语义明确的 service 参数和 callback，例如 `TurnExecutionContext`、`ToolInvocationContext`、`AgentEventObserver` 的 service-owned 版本，在 compose/runtime 边界做转换。该项优先级低于 Wails 直接依赖 core 的问题，可以在第一轮收敛 DTO 后处理。

## 建议的目标依赖图

```text
backend/wails
    -> internal/service 的 application ports / views
    -> internal/contracts

internal/service
    -> internal/core
    -> internal/repository
    -> 中立的 runtime/tool ports

internal/infra
    -> internal/repository / internal/core / 中立 contracts

internal/compose
    -> service / agent_runtime / loop / infra / core

internal/core
    -> core 内部依赖 / internal/contracts
```

关键约束是：`backend/wails` 不 import `internal/core`、`internal/agent_runtime`、`internal/timing`、`internal/repository` 或 `internal/infra`；`core` 不依赖 application service 和具体适配器；所有本地文件和 Provider 行为由 infra 实现。

## 推荐处理顺序

1. 先收敛 `wails/bindings` 的接口和 event：去掉 core/runtime/timing 类型，建立 service-owned view/request 和 Wails DTO 映射。
2. 把 `PrepareModelConfig`、Task 输入限制等领域输入规则移到 service，Wails 只做 wire 级校验。
3. 把 `CreateProject` 和 `PreviewFile` 的文件系统操作抽成窄 Port，由 infra 实现。
4. 拆分模型所需的纯工具数据契约与工具执行契约。
5. 最后再决定是否把 service/runtime 使用的 agent runtime 上下文改成中立类型。

## 审查范围

- 项目：`projects/Praxis/backend`
- 基线：`313b9f6f2af0bd7073a89da8a174f363489498a4`
- 检查方式：源码 import 扫描、关键接口和调用入口阅读、架构文档对照。
- 未运行构建或测试；本次目标是分层与依赖边界审查，不是行为回归审查。
