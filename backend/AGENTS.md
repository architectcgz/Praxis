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
- 变更后运行 `go build ./...` 和 `go vet ./...`，并发行为通过人工回归验证；增加依赖门禁：`./internal/loop/...` 禁 `internal/(execution|service)`，`./internal/execution/...` 禁 `internal/service`。

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
- JSONL 不创建或读取 `storage.json`，不设置存储版本字段；恢复只按当前数据结构严格校验，不提供迁移、兼容读取或静默清理。
- `go build ./...`、`go vet ./...` 后执行 `scripts/check-dependencies.sh`；不保留测试文件，不新增或运行自动化测试。

## 注释与错误信息语言

- Go 代码注释、GoDoc 和实现约束说明默认使用中文，保留标识符、协议字段和技术术语的原文。
- 后端自行生成的错误信息默认使用英文，包括 errors.New、fmt.Errorf、业务校验错误、工具错误和运行日志。
- 测试失败诊断信息使用英文；测试数据、用户内容和 Agent Prompt 不因语言规范而翻译。
- 错误信息应简洁准确，明确失败对象和原因；保留必要的标识符、文件路径以及底层错误上下文。
- 错误包装使用 %w 保留错误链，不因修改文案改变 errors.Is、errors.As、业务错误码或持久化语义。
- 不翻译第三方 Provider、操作系统及外部工具返回的原始错误；只要求 Praxis 自己添加的错误上下文为英文。
- 不因统一语言新增错误抽象、修改协议字段、重构业务逻辑或批量修改无关字符串。

## 执行限制与职责边界

- Task 固定使用 `core/task.MaxIterations = 30` 和 `MaxToolCalls = 100`。正常 ReAct 请求在事务内预留 Iterations；摘要只增加 ModelRequests。所有请求共用 NextSequence，拒绝重复和旧序号；审批恢复不得重扣工具额度。
- Assistant 的最终完成判断属于 Loop。Loop 注入 IDGenerator / Clock 构建工具 Execution，并通过 `repository.AssistantWrite` 明确提交 Message、ToolCalls 和可选 FinalOutput；JSONL 不解释 StopReason，不生成执行身份或时间。
- Task 与工具 Execution 的状态转换、不可变字段及预算单调性由 Core 的 ValidateUpdate 统一定义，领域命令复用同一转换规则；JSONL 仅委托校验并负责事务与跨对象一致性。
- 启动层在 DataRoot.Initialize 后排他创建缺失的 models.json、auth.json、tools.json、settings.json 与默认 Agent 文件，已有文件原样保留。Registry Load / ReloadConfig 只读校验；不得创建缺失配置、补角色、裁剪指令或修复凭据。Provider Key 由 request 规范化 ProviderID，密钥原样传递和保存。
- Loop 在原始调用提交后调用工具 Normalizer，按 Workspace 一次准备路径和参数，并将准备结果或错误交给 Executor 结算。审批及执行恢复复用持久化 NormalizedArguments / Path；Executor 不重复规范化，但仍检查当前工具、权限、审批与未知副作用。
- 单次操作统一使用 ask / auto / yolo / deny，不存在 allow。deny 优先于统一 yolo 和具体规则，未命中规则使用项目/全局默认动作。模型只返回 safe / dangerous / uncertain，危险直接拒绝、不确定才转人工。授权不变量与状态转换集中在 `core/tool_execution/authorization.go` 和 `risk_review.go`；调用层仅编排事务、外部审核和持久化，不能直接修改授权状态。
- 通用上下文压缩只包含算法，SummaryFormat 的 Prompt、标题和结果前缀由 compose 注入；摘要文本只在 Loop 的 Provider 响应边界规范化。
- Repository 的 limit=0 表示完整读取，不补默认值；桌面 service 入口把 0 转为 100 并拒绝负数。历史拼接先完整读取再分页；标题查询词由调用入口准备。
- Workspace 文本读取只返回 contracts 的通用文件错误，Session 预览用例映射业务文案；Document Store 不按字段名推断敏感数据。Task 失败消息原样保存。
- Task 恢复按当前领域约束校验执行预算，不猜测或修补计数，不自动改写持久化数据。
