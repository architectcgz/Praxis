## 包注释与架构文档

- 不允许为了说明包职责而创建 `doc.go`；架构与包职责文档统一写在仓库根目录的 `docs/`，模型协议约束写在 `docs/protocols/`。
- 代码注释只说明与具体实现相关的约束；不要用仅包含包声明和职责说明的 Go 文件占位。

## 数据命名与上下文职责

- 类型名描述数据是什么，不强调生成方式；包名已表达的领域不在类型名中重复。执行配置使用 `task.Config`，历史选择使用 `task.ContextSelection`，用户输入只保存 `Task.InputMessageID`；正文由 JSONL 保存。已选模型配置使用 `model.Config`，模型目录配置使用 `modelconfig.Model`。
- 模型目录解析使用 `ModelConfigResolver`、`ResolveTaskModel` 和 `ResolveDefaultTaskModel`；构建执行配置使用 `buildConfig`。Pending Task 保留模型选择请求，领取时解析配置。
- 历史消息选择与消息转换属于 `service/task`。启动时冻结 `MessageSequenceBoundary`、`ExcludedMessageIDs` 和 `SessionContextRefs`；恢复时使用既定值，不根据其他 Task 当前状态重新计算。
- `core/context` 是通用模型上下文模块，不允许依赖 Task、Session、TaskRepository、消息日志、队列或业务 service；共享事实实体放在 `core/session`。
- `ContextBuilder.Build` 只接收 `context.Content` 与 `context.Limits`，只负责通用结构校验、必要规范化、独立复制、顺序保持和必要原文的容量检查；可压缩历史在模型请求边界压缩，不能静默截断。
- TaskService 加载和选择历史、当前输入、续接消息与共享事实，转换成有序的 `[]context.ContextEntry`。必须保留原文的内容标记 `Protected`；压缩模块不得从业务实体或来源 Kind 推导保护规则。
- 消息流归属与历史选择分别命名：主 Agent 使用 Session 流，其他 Agent 使用各自的私有流；局部选择结果使用 `selectedMessages`，所选工具调用使用 `selectedToolCalls`。
- ToolResult 必须按 `(TaskID, CallID)` 关联 ToolCall，业务消息转换与历史配对位于 `service/task/context_adapter.go`；排除项不得通过延迟回执补全重新纳入。恢复工具回执保持调用声明顺序。
- 压缩保持 Protected 原文及其相对顺序；保护范围覆盖完整工具交互，未闭合工具链不可摘要。摘要输入必须移除展示思考和推理状态，保留区的状态原样续传。
- JSONL 只支持当前 Task 持久化结构，不提供格式迁移或兼容层；恢复拒绝未知字段和非 canonical 值，不能静默修改或删除用户数据。`InputMessageID` 创建后不可变；`Config` 与 `ContextSelection` 只允许 Pending → Starting 时确定，之后不可变；`Progress` 预算不得回退。

## 执行编排与活动状态

- `compose` 只装配组件与管理应用资源；`service` 处理 Wails 用例和持久化事务；`execution.Coordinator` 是进程级执行协调器，直接调用共享的 `loop.Runner`。
- 活动执行只保存在一份 `execution.ActiveTasks` 中。Task 准入事务通过窄接口预留身份，回滚只释放本次新建的预留；同一 Agent 在结算提交前不能预留另一项 Task。
- `loop` 负责模型、工具和 ReAct 迭代，不 import `execution` 或 `service`，不负责最终 Task 结算或队列派发。`execution` 不 import `service`，通过消费者定义的接口调用 Task 持久化用例。
- `repository.MessageWriter` 统一定义执行消息持久化，`loop.Event` 属于 Loop；Task 终态由 `task.TerminalEvent` 表达，Timing 事件由组合根转换。前端事件 kind 和字段保持稳定。
- 审批事务提交后调用 `ResumeApproved`；缓冲唤醒信号不得丢失，重复激活不得创建第二个 Loop。
- Pause / Stop 必须先提交控制意图，再传递取消；最终 Outcome 由 `Settle` 事务仲裁。结算失败保留占用及待提交结果，再次激活只重试结算；只有成功提交的 completed / yielded 才自动领取队列。
- Coordinator 的全局锁仅保护关闭和工作启动，不在锁下执行存储事务、Hook 或模型调用。关闭必须等待准备、恢复、Loop 和结算退出；等待超时不得关闭 JSONL Store 或日志，后续 Close 可以继续等待。
- Coordinator 每次进入或恢复 Loop 前通过 TaskService 加载通用内容；Loop 自己调用通用 ContextBuilder，并在消息恢复后重新读取已提交 Progress。
- 并发路径变更运行 `go test ./...`、`go test -race ./...` 和 `go vet ./...`；增加依赖门禁：`./internal/loop/...` 禁 `internal/(execution|service)`，`./internal/execution/...` 禁 `internal/service`。

- 通用上下文依赖门禁：`./internal/core/context/...` 禁 `internal/(core/(task|session|agent)|repository|service|infra|execution|loop)`；与根 `AGENTS.md` 中的其余门禁一并执行。

## 业务提交 Hook

- `infra/jsonl` 的 Store 与 Repository 只负责持久化，不得 import `internal/hooks`、保存 Dispatcher 或注册事务提交回调；业务调用方在事务成功返回后显式发布 `CommittedHook`。
- 用户输入由 TaskService / QueueService 发布，最终 Assistant 由 Loop 发布，Task 终态由 Settle 发布；幂等重试与回滚不通知，Hook 返回错误只记录日志，不改变业务结果。OperationHook 的执行生命周期保持独立。
- 提交后 Hook 在存储锁外使用非事务 Context，属于 best-effort 通知，不保证跨重启必达或并发提交的全局顺序；不得扩大现有通知范围。
- 依赖门禁：`./internal/infra/jsonl/...` 禁 `internal/hooks`。

## JSONL 存储与恢复

- `infra/jsonl` 只保存日志位置、查询元数据和缺失回执事实，不长期保存完整领域正文；消息索引按 Session 懒加载且缓存总条目数有界。事务使用变更集，不复制全局索引，不执行全图校验。
- `MessageStreams.List` / `LoadMessages` 保持只读，消息边界是同次读取的最后一条可见消息序号。`repository.MessageWriter` 的 Assistant、调用身份和最终输出证据必须原子保存，ToolResult 与输出字节累计幂等提交。
- TaskService 显式协调工具恢复；启动恢复先将遗留 running 调用收敛为 unknown，再启动 Coordinator。当前运行中的调用与审批等待不得被普通历史读取或正常业务恢复错误结束。
- `storage.json` 的 v2 标识是唯一格式入口，拒绝未知格式和未标识的非空数据目录；不提供迁移、猜测解析或静默清理。
- `go vet ./...` 后执行 `scripts/check-dependencies.sh`；并发修改仍执行 `go test -race ./...`。
