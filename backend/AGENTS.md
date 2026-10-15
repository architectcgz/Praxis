## 包注释与架构文档

- 不允许为了说明包职责而创建 `doc.go`；架构与包职责文档统一写在仓库根目录的 `docs/`，模型协议约束写在 `docs/protocols/`。
- 代码注释只说明与具体实现相关的约束；不要用仅包含包声明和职责说明的 Go 文件占位。

## 数据命名与上下文职责

- 类型名描述数据是什么，不强调生成方式；包名已表达的领域不在类型名中重复。Task 执行输入使用 `task.Input`，已选定并保存的模型配置使用 `model.Config`，模型目录配置使用 `modelconfig.Model`。
- 模型目录解析使用 `ModelConfigResolver`、`ResolveTaskModel` 和 `ResolveDefaultTaskModel`；构建执行输入使用 `buildTaskInput`。
- 历史消息选择属于 Task 层。启动时保存 `MessageSequenceBoundary`、`ExcludedMessageIDs` 和 `SessionContextRefs`；恢复时使用这些既定值，不根据其他 Task 的当前状态重新计算。
- 消息流归属与历史消息选择分别命名：主 Agent 使用 Session 消息流，其他 Agent 使用各自的私有流；局部消息选择结果使用 `selectedMessages`，所选工具调用使用 `selectedToolCalls`。
- `core/context.ContextBuilder` 只使用已选消息和共享条目构建 `ModelContext`，不查询 Task 状态或计算历史边界、排除项。
- ToolResult 必须按 `(TaskID, CallID)` 关联 ToolCall，历史回执的配对排序复用 `core/context/history.go`。

- JSONL 正常读写只支持当前持久化结构，不保留旧输入字段兼容逻辑。

## 执行编排与活动状态

- `compose` 只装配组件与管理应用资源；`service` 处理 Wails 用例和持久化事务；`execution.Coordinator` 是进程级执行协调器，直接调用共享的 `loop.Runner`。
- 活动执行只保存在一份 `execution.ActiveTasks` 中。Task 准入事务通过窄接口预留身份，回滚只释放本次新建的预留；同一 Agent 在结算提交前不能预留另一项 Task。
- `loop` 负责模型、工具和 ReAct 迭代，不 import `execution` 或 `service`，不负责最终 Task 结算或队列派发。`execution` 不 import `service`，通过消费者定义的接口调用 Task 持久化用例。
- `loop.MessageRecorder` 和 `loop.Event` 属于 Loop；Task 终态由 `task.TerminalEvent` 表达，Timing 事件由组合根转换。前端事件 kind 和字段保持稳定。
- 审批事务提交后调用 `ResumeApproved`；缓冲唤醒信号不得丢失，重复激活不得创建第二个 Loop。
- Pause / Stop 必须先提交控制意图，再传递取消；最终 Outcome 由 `Settle` 事务仲裁。结算失败保留占用及待提交结果，再次激活只重试结算；只有成功提交的 completed / yielded 才自动领取队列。
- Coordinator 的全局锁仅保护关闭和工作启动，不在锁下执行存储事务、Hook 或模型调用。关闭必须等待准备、恢复、Loop 和结算退出；等待超时不得关闭 JSONL Store 或日志，后续 Close 可以继续等待。
- 并发路径变更运行 `go test ./...`、`go test -race ./...` 和 `go vet ./...`；增加依赖门禁：`./internal/loop/...` 禁 `internal/(execution|service)`，`./internal/execution/...` 禁 `internal/service`。
