# 待讨论架构问题清单

本文记录当前后端代码审计发现的架构问题与边界缺陷，按影响程度排序，逐项讨论并确定方案。

---

## 1. `Session.Close` 生命周期断层：JSONL Writer 协程与文件句柄全局泄漏

- 状态：已完成
- 位置：
  - `backend/internal/infra/storage/load.go:13-36`
  - `backend/internal/core/coordinator/session.go:53-68`
  - `backend/internal/core/coordinator/session.go:123-136`
  - `backend/internal/core/coordinator/coordinator.go:187-213`
  - `backend/internal/service/session/service.go:118-139`
- 现状与问题：
  - `SessionRepository.Load` 会为每个 Agent（主 Agent 及所有 Subagent）启动常驻 `jsonl.Writer` goroutine 并持有打开的文件句柄，但全项目除 `load.go` 加载失败时的回滚外，没有任何地方调用 `Session.Close`。
  - `Coordinator.releaseSession` 关闭会话时只删除 map 登记，不关闭文件句柄与 Writer。
  - `Coordinator.Close` 退出应用时只清空 map，不关闭所有已加载会话。
  - `Coordinator.loadSession` 在 `Load` 成功后，若 `Get` / `State != SessionActive` / `loadConfig` 失败，直接返回错误，已打开的 `owner` 泄漏。
  - `SessionService.CleanupEmptySessions` 为判断空会话直接调用 `s.sessions.Load`，未关闭 `owner` 就执行 `s.sessions.Delete`（`os.RemoveAll`），在 Windows 上文件被占用会导致删除失败并泄漏协程。
- 确定方案：
  - 从 `repository.Session` 拆出 `SessionLoader`（仅含 `Load`），只注入给 `Coordinator`，由 `Coordinator` 独家拥有 `Session` 的打开与关闭权。
  - `Coordinator` 在 `loadSession` 失败回滚、`releaseSession` 释放会话、`Coordinator.Close` 应用退出三条路径上统一调用 `owner.Close(ctx)`。
  - `CleanupEmptySessions` 改为通过存储层轻量检查空日志（不 `Load` 启动 Writer），再执行删除。

---

## 2. `Coordinator` / `SessionService` / `ApprovalService` / `repository.Session` 职责交叉

- 状态：已完成（方向 A）
- 位置：
  - `backend/wails/bindings/services.go:26-56`
  - `backend/internal/service/session/service.go:16-139`
  - `backend/internal/service/approval/service.go:1-73`
  - `backend/internal/repository/session.go:11-22`
- 现状与问题：
  - 会话相关操作拆散在三个平行入口：`SelectSession`/`CloseSession`/`SelectModel`/`ReloadSessionConfig`/`AgentHistory` 在 `Coordinator`；`CreateSession`/`DeleteSession`/`RenameSession`/`ModelOptions`/`SelectedModel` 在 `SessionService`（其中后两者只是透传 `Coordinator.GetSession`）；审批配置又单独拆出一个仅 73 行的 `ApprovalService`，同时依赖 `SessionService` 和 `repository.Session`。
  - `repository.Session` 注释声明“提供会话元数据读写，不加载或持有会话历史”，但接口中仍包含 `Load(...) (*session.Session, error)`，导致 `SessionService` 可绕过 `Coordinator` 独立打开同一会话的 JSONL，存在双 Writer 并发写同一文件的隐患。
  - `service/session.Dependencies` 与 `Service` 存在未消费的死字段（`HookDispatcher`、`IDs`）。
- 确定方案：
  - 外部入口统一收归 `service` 层，`wails/bindings` 不再直接依赖 `core/coordinator` 或 `storage` 裸函数。
  - 合并 `approval.Service` 与 `SessionLogPath` 到 `service/session`；清理 `service/session` 中未使用的 `HookDispatcher` 和 `IDs` 字段。
  - `Coordinator` 退回 Core 内部，专注已打开会话的生命周期（`Load`/`Close`）、任务队列、Run/Subagent 调度与运行期并发控制；元数据读写与外部应用用例由 `service` 组合编排。

---

## 3. 子代理协作工具另起炉灶：`native_tools.go` 绕开 `toolexecution` 体系

- 状态：已完成
- 位置：
  - `backend/internal/core/coordinator/native_tools.go:21-183`
  - `backend/internal/core/coordinator/run.go:21-48`
  - `backend/internal/core/loop/tools.go:150-156`
- 现状与问题：
  - `nativeTool` 实现 `tool.Tool` 接口，但 `Prepare()` 永远返回错误，属于接口伪装。
  - 在 `native_tools.go` 中重复实现 JSON 严格解码 `decodeNative`（与 `toolexecution/executor.go` 的 `decodeToolArguments` 重复），同时绕开了工具 Hook、单调用停止控制（`StopToolCall` 无法停止阻塞在 `subagent_wait` 的原生调用）以及输出上限裁剪。
  - `nativeExecutor.BoundResult` 对 `e.base` 做无保护的接口类型断言，`loop/tools.go` 也通过隐式断言获取 `BoundResult`，表明 `loop.ToolExecutor` 接口漏定义了该能力。
  - `coordinator/run.go` 将 `&runner` 传给 `run.Execute`，又在 `prepareTools` 回调中通过闭包副作用覆写 `runner.Tools = catalog`，控制流隐晦。
- 确定方案：
  - 子代理协作工具的定义、参数校验和实现归属 `internal/tools/subagent.go`，`coordinator` 绑定本次 Run 的协作操作并组合工具目录。
  - 协作工具使用 `tools.Environment.CallID` 关联调用，在 `toolexecution.Executor` 中统一执行，并按声明免审批。
  - 在 `loop.ToolExecutor` 接口中显式声明 `BoundResult`，移除隐式类型断言。
  - 调整 `coordinator/run.go` 与 `run.Execute` 的工具准备返回，消除回调闭包修改 `runner.Tools` 的副作用。

---

## 4. 模型请求链路重复实现，且工具审核与工具执行强耦合

- 状态：已完成
- 位置：
  - `backend/internal/core/loop/model.go:160-195`
  - `backend/internal/core/toolexecution/review.go:98-284`
  - `backend/internal/core/toolexecution/authorize.go:1-186`
  - `backend/internal/core/toolexecution/approval.go:1-78`
  - `backend/internal/core/agent/run/run.go:202-243`
- 现状与问题：
  - `loop/model.go` 和 `toolexecution/review.go` 各自实现了一套完整的“容量评估 -> 记录 running -> `OperationProvider` Hook -> `Provider.Stream` 消费 -> 记录终态 `ModelRequest` -> 最多 4 次尝试重试”流程，且已出现行为漂移（`review.go:157` 正常完成时也写了 `Partial`，漏了 `BeforeModelRequest`/`AfterModelResponse`/`FirstResponse`）。
  - 工具审批等待、模型风险审核与工具参数准备/执行/输出限制混杂在 `toolexecution` 内，共享同一张 `active` 表和锁，导致 `toolexecution` 反向依赖 `Provider`、`ModelRequest` 落盘与审核 Prompt。
- 确定方案：
  - 抽取独立的底层**模型请求组件**（统一承载容量校验、Hook、计时与首响、流消费、`ModelRequest` 事实持久化与 4 次重试），供普通迭代、上下文摘要与风险审核三类请求共用（支持通过持久化回调兼容并发审核时的串行落盘）。
  - 将**工具授权/审批与风险审核**从 `toolexecution` 剥离为独立组件（负责 `ApprovalMode` 判定、调用模型请求组件执行风险审核、维护待审批列表与用户决策）；`toolexecution` 只保留工具解码、准备、执行、输出限制与单调用取消（通过每个调用独立的派生 `callCtx` 传递取消信号，取消调用即可同时中断审核、审批等待或执行中的工具，双方彻底解耦、不共享锁与状态表）。

---

## 5. 错误分类边界倒置：`loop.Failure` 成为全局错误总线与三套平行枚举

- 状态：已完成（落点 A）
- 位置：
  - `backend/internal/core/loop/run.go:58-141`
  - `backend/internal/core/agent/run/run.go:343-345`
  - `backend/internal/core/agent/run/settle.go:138-159`
  - `backend/internal/core/coordinator/run.go:31-79`
  - `backend/internal/core/entry/payload.go:34-52`
- 现状与问题：
  - `loop` 本应只是 ReAct 迭代引擎，但上层 `coordinator` 与 `agent/run` 反向依赖 `loop.NewFailure` / `loop.FailureClassification` 传递和判断存储失败、资源上限等错误。
  - `loop/run.go` 中存在 `loop.FailureCode`、一组全项目未使用的 `ErrBusy` / `ErrStorage` 等 `errorcode.Code` 常量及死函数 `fail()`，与 `entry.RunFailureCode` 构成多套平行枚举，跨层只能靠 `settle.go` 手工 `switch` 映射。
- 确定方案：
  - 删除 `loop.Failure`、`loop.FailureCode`、未使用的 `loop.Err*` 常量与 `fail()`，解除 `coordinator` 和 `agent/run` 对 `loop` 错误体系的反向依赖。
  - 各边界直接返回标准类型化错误或哨兵错误（如存储边界返回 `ErrStorage`，模型请求组件返回 `ErrProvider` 或 `*ContextWindowExceededError`，契约错误返回 `ErrContract`）；`coordinator` 用 `errors.Is` 判断存储失败，`agent/run` 结算处统一映射为权威的 `entry.RunFailureCode`。

---

## 6. Subagent 名额校验非原子，且启动失败回滚不完整

- 状态：已完成
- 位置：
  - `backend/internal/core/coordinator/subagent.go:39-125`
  - `backend/internal/core/agent/run/subagent.go:60-64`
  - `backend/internal/core/agent/run/subagent.go:358-370`
- 现状与问题：
  - `startSubagent` 在第 39 行只读检查 `parent.ActiveSubagents() >= 10`，直到完成 `CreateSubagent` 和 `child.Prepare` 磁盘写入后，才在第 89 行调用 `RegisterSubagent`，名额检查与预留非原子。
  - 若第 114 行 `launchLocked` 失败，已登记进 `parent.collaboration.children` 的句柄未被移除，且 `SubagentStarted` 事件在启动成功前已提前发出。
  - 若 `CreateSubagent` 成功但后续 `Prepare` 失败，`Session.data.AgentContexts` 中已登记的空 `agentID` 与新建的空 `.jsonl` 文件未回滚。
- 确定方案：
  - 在 `agentrun.Run` 上把名额检查与占位合并为锁内原子操作（`ReserveSubagent` -> 准备后 `AttachSubagent` -> 失败时 `ReleaseSubagent`）。
  - 子 Run 登记成功后再发布 `SubagentStarted`，失败时统一释放尚未启动的占位和取消资源。子 Run 不设整体时限；`subagent_wait` 单次最多等待 30 秒，到期后未结束的子 Run 继续执行。
  - 仅在 `!prepared`（尚无任何记录写入）时关闭并清理新建的空子代理 Writer 与登记；已写入 `RunPrepared` 时保持走 `SettlePreparation` 写终态保留事实。

---

## 7. 上下文压缩的 `retained` 视图前后不一致与 `ModelContextFromEntries` 死分支

- 状态：已完成（方案 A）
- 位置：
  - `backend/internal/core/context/compress/compress.go:39-58`
  - `backend/internal/core/loop/compression.go:78-92`
  - `backend/internal/core/session/data.go:15-21`
  - `backend/internal/core/context/context.go:32-45`
- 现状与问题：
  - 压缩时 `splitHistory` 拆分出 `retained` 并组装 `[Summary, retained...]` 供当前轮使用，但 `Summary` 落盘后 `AgentContext.Accept` 立即把窗口重置为 `[Summary]`，导致当前轮与下一轮（及重启恢复后）的上下文构成不一致；同时 `Summary` 实际压缩的是全量 `SummaryInput`（含 `retained`），导致当前轮 `Summary` 与 `retained` 内容重叠。
  - `ModelContextFromEntries` 中遇到 `Summary` 时将 `entries[:i]` 移至 `Summary` 之后的逻辑，在 `AgentContext` 遇 `Summary` 重置且 `load.go` 遇 `Summary` 停止扫描的前提下永远走不到（若真触发还会把已被摘要的旧历史重新放回上下文）。
- 确定方案：
  - 保持 `Summary` 作为 append-only 日志的唯一截断点（摘要仍覆盖全量历史，`load.go` 遇 `Summary` 即停，不增加持久化字段耦合）；明确 `retained` 仅作压缩触发当轮的原文防失真补充。
  - 删除 `ModelContextFromEntries` 中失效的 `entries[:i]` 重排分支和冗余的 `SummaryInput` 字段，统一直接使用 `ModelEntries(entries)`，补清注释。

---

## 8. 功能缺陷：`StopToolCall` 漏传 `RunID` 导致单工具停止永远失败

- 状态：已完成
- 位置：
  - `backend/wails/bindings/bindings_command.go:207-221`
  - `backend/internal/core/toolexecution/control.go:48-54`
- 现状与问题：
  - `CommandBindings.StopToolCall` 构造 `execution.ToolControl` 时漏传 `RunID: identity.RunID(wire.RunID)`。
  - `Executor.controlledCall` 按 `e.active[Key{RunID: request.RunID, CallID: request.CallID}]` 查找，导致停止单个工具调用必定命中空项并返回 `ErrRequestConflict`。
- 确定方案：
  - 在 `CommandBindings.StopToolCall` 中补传 `RunID: identity.RunID(wire.RunID)`。
