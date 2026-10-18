# Praxis 后端架构规范

本目录采用以下目标职责与依赖规则；与上级旧目录约定冲突时，以本文件为准。注释默认中文，Praxis 自身错误信息默认英文。

## 目录与职责

- `internal/core/`：独立的 ReAct Loop、ContextBuilder、模型流、工具契约、消息、Hook 机制和预约 Task 的纯状态转换。不得依赖 `service`、`infra`、`bootstrap`、`wails`，不得读取配置文件或操作文件系统。
- `internal/service/agent/`：Agent 与 AgentExecution 实体、查询和控制。`input/` 负责输入接纳、冻结上下文、进度、结算与恢复事务；`coordinator/` 负责 Coordinator、ActiveTasks、审批唤醒与预约交接。子包拥有实际行为，不增加转发层。
- `internal/service/task/`：预约输入、FIFO 查询与取消，以及预约仓储契约。Task 仅表示预约，持久状态为 `pending/dispatched/cancelled`。
- `internal/service/toolcall/`：工具调用准入、授权、风险审核编排与结算；纯调用事实和原子消息写入契约位于 `core/toolcall/`。
- `internal/service/session/`、`project/`、`model/`：各业务用例与事务。Service 不自行构建其他 Service，不直接进行文件 I/O。
- `internal/infra/`：JSONL、Provider 编解码、具体工具、文件系统，以及 Agent/Model/Tool 配置加载。仓储可以引用所属业务的实体与仓储契约，不调用业务流程，不承担审批与执行调度。
- `internal/bootstrap/`：唯一装配入口，创建实例、注入依赖、安排初始化和启动恢复，最后开放 Wails。不得承载运行期业务决策。
- `wails/`：请求入口、DTO 转换、错误映射与桌面宿主操作。输入边界负责规范化及校验；不得重复构建执行业务流程。
- `identity/`、`clock/`、`fault/`、`transaction/`、`security/` 等只保留明确的共享能力。目录与接口使用业务或能力名称，不引入 `port`、`adapter`、`runtime` 模块、`ExecutionSlots` 或无价值代理。

## 编排与依赖

1. 两个模块配合完成用例时，由拥有该用例的上层 Service 编排；模块只需要对方的局部能力时，在消费方声明窄接口并由 Bootstrap 注入。
2. 跨包类型由事实拥有者定义；只共享真正稳定的值类型，不为消除一次 import 建公共大包或复制同形结构。
3. 纯状态转换不执行 I/O；仓储负责存取，用例负责事务、权限和状态组合，Coordinator 负责执行与交接。
4. 不建立为了说明职责的 `doc.go`。架构文档统一放在 `docs/`，不创建重复的优化总结文档。

## 执行身份和事务

- 每次实际执行都有非空 `AgentExecutionID`；直接输入不创建 Task。预约在领取事务中创建 Execution，并通过可选 `SourceTaskID` 溯源。
- Agent 当前归属、模型请求、消息、工具调用、审批、控制、用量、计时和事件均绑定 ExecutionID。工具调用唯一性为 `(ExecutionID, ProviderToolCallID)`。
- 直接输入在一个事务中提交用户消息、幂等请求、Execution 和 Agent 归属；预约领取在一个事务中提交队首派发、Execution、冻结配置和 Agent 归属。提交失败释放本次 ActiveTasks 预留。
- Loop 只接收 `loop.Input`，进度能力绑定本次执行。Loop 不读取业务仓储，不接收 Agent/Task 实体，不决定持久化终态和后续预约。
- `model.ModelStopReason`、`loop.EndReason`、`agent.ExecutionOutcome` 分别表示模型响应、Loop 返回及业务结算，不混用。
- Assistant、ToolCalls 和最终输出证据保持原子提交；工具回执和累计字节保持幂等原子提交。
- 只有 Execution 结算事务提交且归属释放后，Coordinator 才能领取下一预约。Provider/执行错误默认保留待办并停止自动派发。
- 等待审批不结束 Execution，不释放归属；审批后用相同 ExecutionID 恢复。控制命令绑定目标 ExecutionID，已提交控制意图在结算事务中仲裁。
- 结算失败保留占用及结果，仅重试结算，禁止重放模型和工具。启动先处理遗留 running 工具的未知副作用，再恢复 Execution，最后检查空闲 Agent 的队列。

## 上下文与持久化

- 配置和历史可见边界在准入/领取时冻结；后续排队输入不能混入已启动的执行。工具调用与回执按 `(ExecutionID, CallID)` 配对。
- JSONL 只保存日志资源、序号和可重建轻量索引，正文按需读取，不建立全局业务对象缓存。
- 应用运行状态写入独立的 `state/` 目录，旧 `runtime/` 数据保持原样，不自动加载或覆盖。
- JSONL 按当前 Schema 严格恢复，不实现旧数据迁移、兼容读取或版本标记。验证使用独立临时数据目录，不覆盖已有用户数据。
- Store 与 Repository 不发布业务 Hook。业务事务提交后在锁外显式发布 best-effort Hook；幂等重试、回滚不重复通知，Hook 失败不撤销业务结果。

## 输入和实现规范

- 规范化、默认值及输入校验由真实入口一次完成，内部不重复 Trim、Clean、Validate 或重新解析已处理输入。磁盘恢复是独立的不可信输入边界，拒绝非 canonical 数据，不静默修复。
- Provider 保持各自协议 Schema 与 Encoder/Decoder；目录迁移不引入第三方拓展或改变协议语义。
- 多字段复合字面量默认一字段一行；简短局部值不超过三个字段时可同行。

## 验收

- 执行 `go test ./...`、`go vet ./...`、`bash scripts/check-dependencies.sh`。涉及并发身份、事务与恢复时补充有实际场景的回归测试；本迁移方案明确授权这些测试，不使用真实用户数据。
- 修改 Wails 签名后执行 `go run -tags bindings .` 生成绑定，不手工编辑 `wailsjs/`。随后在前端执行 `npm run check`。
- 不启动开发服务。验证至少覆盖直接输入、FIFO、幂等、审批、Pause/Stop 竞争、Provider 失败、结算失败、重启预算与未知副作用，并检查 Prompt Cache 与 token 归属。
