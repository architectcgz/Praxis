# Praxis 后端架构规范

本目录采用以下目标职责与依赖规则；与上级旧目录约定冲突时，以本文件为准。注释默认中文，Praxis 自身错误信息默认英文。

## 目录与职责

- `internal/core/`：Agent、Execution、Session、Project、Workspace、Task、ToolExecution、安全策略、计时和消息的实体与纯规则，以及 ContextBuilder、模型流、工具契约和 Hook 机制。不得定义仓储或依赖 `repository`、`loop`、`service`、`infra`、`bootstrap`、`wails`，不得读取配置文件、管理事务或操作文件系统。
- `internal/core/security/`：审批模式、默认策略继承、命令匹配与审批决策，以及风险审核结果类型，只包含值对象与纯规则。
- `internal/core/toolexecution/`：共享 Executor 协调单次工具调用的参数准备、授权、模型审核、人工审批等待、真实调用与回执提交。ExecutionTools 绑定已取得运行权的 AgentExecution、Workspace 和等待能力，是 Loop 唯一的完整工具执行入口；Execution 仅提供单次调用的 Run/Stop 取消句柄。Core 通过 Bootstrap 注入的业务操作接口协调外部能力，不直接访问仓储、配置文件或文件系统。
- `internal/repository/`：统一定义持久化读写、查询结果、原子消息写入和跨仓储事务契约，方法使用 Core 实体。只依赖 Core 与必要的纯基础能力，不包含业务编排、Hook 或具体存储实现。
- `internal/core/loop/`：一次 AgentExecution 内的 ReAct 迭代，调用 Provider 与本次 ExecutionTools，不解析审批模式或编排 Review。进度和消息提交通过注入的业务能力完成，不依赖 `repository`、`service`、`infra`、`bootstrap` 或 Wails。
- `internal/service/agent/`：Agent 的查询与控制。`execution/` 负责直接输入准入、冻结上下文、进度、结算与恢复事务；`coordinator/` 负责 Coordinator、ActiveTasks、审批唤醒与预约交接。实体位于 `core/agent`，子包拥有实际行为，不增加转发层。
- `internal/service/task/`：预约输入、FIFO 查询与取消，依赖 `repository.Task`。Task 仅表示预约，持久状态为 `pending/dispatched/cancelled`。
- `internal/service/toolexecution/`：工具调用归属校验、规则配置读取、审核输入准备与模型预算预留、审批事实查询和可靠 ToolResult 提交。不得保留 Execute、execute、authorize 或流程转发层，不管理活动工具、启动互斥或审批状态机。
- `internal/service/session/`、`project/`、`model/`：各业务用例与事务。Service 不自行构建其他 Service，不直接进行文件 I/O。
- `internal/infra/`：JSONL、Provider 编解码、具体工具、文件系统，以及 Agent/Model/Tool 配置加载。JSONL 引用 Core 实体并实现 Repository 接口；所有 Infra 均不得依赖 Service，不调用业务流程，不承担审批与执行调度。
- `internal/bootstrap/`：唯一装配入口，创建实例、注入依赖、安排初始化和启动恢复，最后开放 Wails。不得承载运行期业务决策。
- `wails/`：请求入口、DTO 转换、错误映射与桌面宿主操作。输入边界负责规范化及校验；不得重复构建执行业务流程。
- `identity/`、`clock/`、`fault/` 等只保留明确的共享能力，事务契约统一位于 Repository。目录与接口使用业务或能力名称，不引入 `port`、`adapter`、`runtime` 模块、`ExecutionSlots` 或无价值代理。

## 编排与依赖

1. 跨实体用例由拥有该用例的 Service 编排；单次工具调用的授权与执行流程由 Core Executor 拥有。Core 可声明完成流程必需的业务操作接口，由 Bootstrap 注入 Service；Service 负责跨实体读取、预算预留和持久化约束。持久化契约统一位于 Repository，不为简单仓储转发新增业务层或复制完整仓储接口。
2. 跨包类型由事实拥有者定义；只共享真正稳定的值类型，不为消除一次 import 建公共大包或复制同形结构。
3. 纯状态转换不执行 I/O；仓储负责存取，用例负责事务、权限和状态组合，Coordinator 负责执行与交接。
4. 不建立为了说明职责的 `doc.go`。架构文档统一放在 `docs/`，不创建重复的优化总结文档。
5. 错误先按职责归属：领域状态错误在 Core，业务用例错误在 Service，存储实现错误在 Infra。每个包默认集中于一个 `errors.go`，包括错误分类与简单消息转换；不为各实体新增 `*_errors.go`，不创建空错误文件。持久化失败码等领域值类型仍按实体职责放置。

## 执行身份和事务

- 每次实际执行都有非空 `AgentExecutionID`；直接输入不创建 Task。预约在领取事务中创建 Execution，并通过可选 `SourceTaskID` 溯源。
- Agent 当前归属、模型请求、消息、工具调用、审批、控制、用量、计时和事件均绑定 ExecutionID。工具调用唯一性为 `(ExecutionID, ProviderToolCallID)`。
- 直接输入在一个事务中提交用户消息、幂等请求、Execution 和 Agent 归属；预约领取在一个事务中提交队首派发、Execution、冻结配置和 Agent 归属。提交失败释放本次 ActiveTasks 预留。
- Loop 只接收 `loop.Input`，进度业务能力绑定本次执行。工具执行查询和原子输出写入直接引用 Repository 契约；Loop 不接收 Agent/Task 实体，不决定 Execution 持久化终态和后续预约。
- `model.ModelStopReason`、`loop.EndReason`、`agent.ExecutionOutcome` 分别表示模型响应、Loop 返回及业务结算，不混用。
- Assistant、ToolCalls 和最终输出证据保持原子提交；工具回执和累计字节保持幂等原子提交。
- 只有 Execution 结算事务提交且归属释放后，Coordinator 才能领取下一预约。Provider/执行错误默认保留待办并停止自动派发。
- 等待审批时 Execution 保持 running，不新增持久化等待状态，不释放归属。保存用户决定后只唤醒同一 Loop，不重复触发 ExecutionStarted/ExecutionResumed。控制命令绑定目标 ExecutionID。
- 结算失败保留占用及结果，仅重试结算，禁止重放模型和工具。启动先确认旧执行者已停止，由 ToolExecution Service 为缺少 ToolResult 的调用追加 `No result provided` 错误回执，再恢复 Execution，最后检查空闲 Agent 的队列。

## 上下文与持久化

- 配置和历史可见边界在准入/领取时冻结；后续排队输入不能混入已启动的执行。工具调用与回执按 `(ExecutionID, CallID)` 配对。
- ToolCall 保存在 Assistant 消息，ToolResult 保存在 Tool 消息，是唯一完成记录；不另存结果正文、终态或独立 ToolExecutionID。审批请求、用户决定和 Review 保存独立可靠事实，不构建 Facts/ApprovalFacts 聚合，不写入 tool.started/tool.stop_requested。每次 ToolCall 开始授权时只读取一次内存规则，审批、审核与执行沿用本次冻结参数和规则决定；规则变更仅影响后续 ToolCall。同一调用只产生一次人工审批请求，用户决定按调用键绑定，不携带请求时间标识。
- Coordinator 仅在 Task 已领取、Execution 已准入且 TryRun 获得唯一运行权后，解析 WorkspacePath 并创建本次 ExecutionTools。Loop 和 Tool 不透传 WorkspacePath；Tool 接口为 Call(ctx, args)。Executor 先调用 BeforeToolExecution，再通过注入的工具能力准备并冻结参数，唤醒后不重复准备。
- 单次 Loop 由 ActiveTasks.TryRun 保证运行权；Session 后续输入先进入 FIFO Task 队列，不提前创建 Execution 或 ExecutionTools。Executor 不建立第二套运行权或防重复启动机制；启动临界区只协调 Execution 归属、取消回执与活动取消句柄，不重新读取或校验规则。规则读取由 Service 在配置读锁内完成；工具控制使用 Executor 控制锁后访问存储。模型和工具 I/O 均在锁外。
- 启动前 Stop 提交中断回执以阻止启动，运行中 Stop 取消活动句柄，迟到 Stop 不覆盖已有回执。活动索引仅供 Stop 定位取消句柄，本次 Execute 返回时移除。回执提交失败退出 Loop，由 Coordinator 在旧调用返回后补齐缺失回执，不重新执行工具。恢复只补 No result provided，不依赖审批或启动事实决定重放。
- 人工审批等待保留在同一 Loop 中，Coordinator 唤醒已有执行。恢复补写仅在旧 Loop 与工具已停止后进行，不能提前关闭当前活动工具的消息配对。
- JSONL 只保存日志资源、序号和可重建轻量索引，正文按需读取，不建立全局业务对象缓存。
- 应用运行状态写入独立的 `state/` 目录，旧 `runtime/` 数据保持原样，不自动加载或覆盖。
- JSONL 按当前 Schema 严格恢复，不实现旧数据迁移、兼容读取或版本标记。验证使用独立临时数据目录，不覆盖已有用户数据。
- Store 与 Repository 不发布业务 Hook。业务事务提交后在锁外显式发布 best-effort Hook；幂等重试、回滚不重复通知，Hook 失败不撤销业务结果。

## 输入和实现规范

- 规范化、默认值及输入校验由真实入口一次完成，内部不重复 Trim、Clean、Validate 或重新解析已处理输入。磁盘恢复是独立的不可信输入边界，拒绝非 canonical 数据，不静默修复。
- Provider 保持各自协议 Schema 与 Encoder/Decoder；目录迁移不引入第三方拓展或改变协议语义。
- 多字段复合字面量默认一字段一行；简短局部值不超过三个字段时可同行。

## 验收

- 执行 `go build ./...`、`go vet ./...`、`go list ./...`、基于 go list 直接 import 的分层检查。遵守根目录不保留测试文件的约定；使用独立临时数据核验事务、恢复和持久化，不使用真实用户数据。
- 修改 Wails 签名后执行 `go run -tags bindings .` 生成绑定，不手工编辑 `wailsjs/`。随后在前端执行 `npm run check`。
- 不启动开发服务。验证至少覆盖直接输入、FIFO、幂等、审批、Pause/Stop 竞争、Provider 失败、结算失败、重启预算与未知副作用，并检查 Prompt Cache 与 token 归属。
