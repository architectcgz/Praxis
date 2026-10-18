# Praxis 后端架构

## 背景与目标

后端按业务概念和明确能力组织。类型属于拥有其语义与生命周期的模块；跨模块的 Agent Task 执行流程由 `execution` 编排，具体实现由 `bootstrap` 装配。正常的跨包类型引用直接复用，不为消除 import 复制数据模型。

## 模块与职责

| 模块 | 职责 |
| --- | --- |
| `agent` | Agent 实例、定义注册表、状态查询、固定目标的控制命令 |
| `session` | 会话创建、共享事实、会话目录、文件预览用例与用量查询 |
| `message` | 消息、内容块、消息流读取与工具回执写入契约 |
| `project` | 项目、初始工作区创建与项目授权设置 |
| `workspace` | 工作区模型与文本读取能力；`workspace/fs` 实现本地文件访问 |
| `task` | Task 模型、冻结配置、状态机、预算、最终输出证据与基础存取契约 |
| `task/service` | Task 创建、准入、上下文冻结、进度、队列、恢复与结算用例 |
| `context` | 模型可见内容、历史构建与压缩算法 |
| `model` | 模型目录、配置管理、凭据、模型构建与用量类型 |
| `model/provider/*` | 各 Provider 的请求编码、流式解码与协议状态机 |
| `tool` | 工具协议、注册表、可用性目录与结果大小边界 |
| `tool/arguments` | 工具参数 JSON 和路径输入边界 |
| `tool/builtin/*` | Bash、文件读取与补丁工具实现 |
| `toolcall` | 单次调用的授权、风险审核、审批、执行、结果与持久化契约 |
| `execution` | 进程级 Runtime、Coordinator、活动执行身份、审批续接与关闭协调 |
| `execution/loop` | 模型与工具迭代算法，不拥有 Task 生命周期 |
| `hook`、`timing` | 扩展通知与独立操作计时 |
| `storage/jsonl` | 共享日志资源、事务、序号、位置索引、重放与业务持久化实现 |
| `storage/document` | 内容寻址的不可变文档存储 |
| `transaction` | 单一后端的原子提交契约 |
| `identity`、`clock`、`fault` | 强类型身份与生成器、时间来源、错误分类 |
| `settings`、`dataroot`、`logging` | 全局设置、数据目录布局与诊断日志 |
| `bootstrap` | 进程级装配、配置发布与资源释放 |
| `wails` | 桌面宿主、请求规范化与校验、业务参数构造、DTO 投影与错误映射 |

## 调用与依赖方向

```text
Wails Binding
    ├── project.Service
    ├── session.Service
    ├── agent.Service
    ├── model.Service
    └── execution.Runtime
            ├── task/service.Service（Task 用例与持久化 FIFO 队列）
            ├── Coordinator 与进程内执行身份
            └── execution/loop.Runner
                    ├── model.ModelBuilder
                    └── toolcall.Executor → tool.Tool
```

- `task`、`task/service`、`agent`、`session`、`model`、`context` 不依赖 `execution`。
- `execution/loop` 不反向依赖父包，由消费者声明进度与执行接口。
- 业务模块不引用 `storage/jsonl`、`storage/document`、`bootstrap` 或 `wails`。
- `storage/jsonl` 不引用 `execution` 或 `hook`；存储不注册业务提交回调。
- `model` 不引用 `model/provider/*`，`tool` 不引用 `tool/builtin/*`，`workspace` 不引用 `workspace/fs`。
- `bootstrap` 连接具体实现与业务能力。Wails 只消费业务能力，不读取具体存储或构造 Provider。
- 接口按能力命名，优先放在消费者或业务所有者附近。`task.Store` 只提供读取与保存，执行协调由 `execution` 声明窄接口，Agent 展示查询由 `agent.TaskReader` 声明。

## 输入与协议边界

- Wails Binding 是桌面请求的输入边界，负责字段复制、规范化、默认值补充和校验。简单处理直接写在对应入口，较长的模型配置转换放在 Binding 包内的 `model_config_request.go`。
- `CreateInput`、`SendInput` 等业务参数类型只描述业务操作所需的数据；业务包不提供桌面请求的规范化构造函数。Binding 使用已有 DTO 直接构造业务参数，不增加同形中间类型。
- 内部方法接收 canonical 输入，不重复 Trim、补默认值或清洗路径。持久化恢复只读校验，不静默修复。
- Binding 的模型配置转换直接生成 `model/config.Config`，并调用配置类型的只读校验；该校验也供配置文件恢复入口使用。Service 只检查依赖当前状态的 Agent 引用，不重复规范化或字段校验。
- 业务查询返回业务对象或明确的查询结果。消息、配置和事件在 Wails 出口一次投影为 `wails/dto`；opaque 推理状态不进入前端或摘要。
- 文件错误与受限文本读取能力属于 `workspace`。会话文件预览负责解析当前工作区并授权，`workspace/fs` 使用 `os.OpenRoot` 完成实际访问。

## 状态、生命周期与恢复

- `bootstrap.Application` 创建并拥有共享存储、日志、配置注册表、业务服务与一个 `execution.Runtime`。
- `bootstrap` 装配一个 `task/service.Service`，并把它同时作为 Task 用例和队列能力注入 Runtime；Runtime 创建并拥有唯一的 `ActiveTasks`、Coordinator 和默认 Loop。一次执行的 Context、续接消息与等待状态不存入业务单例。
- 启动时 Runtime 先协调上次进程的 running 工具调用，再恢复活动 Task，最后派发可执行队列。未知副作用不自动重试，等待人工审批的调用保持等待。
- 直接输入、预约输入、工具审批与控制命令使用稳定身份进行幂等仲裁；队列使用持久化单调序号维持 FIFO。
- 关闭时先拒绝新执行、取消并等待所有执行及结算退出，再清理空会话并关闭存储。执行退出超时时保留资源，后续允许继续关闭。
- 模型修改、会话创建与直接输入共用同一个配置锁；锁顺序为配置锁在外、JSONL 事务锁在内。配置重载在同一事务锁内发布模型、定义、工具与授权快照。

## 持久化与通知

- `storage/jsonl` 只创建一个共享 `Store`。各业务适配文件共用日志、锁、事务、序号和可重建索引，不各自建立底层资源或全局正文缓存。
- `message.Reader` 的消息集合与序号边界来自一次一致性读取；业务调用方不选择 Session 流或 Agent 私有流的日志键位。
- `toolcall.Writer` 将 Assistant 消息、工具调用身份和最终输出证据原子提交；工具回执与输出字节累计在同一事务中幂等提交。
- `transaction.Runner` 只保证同一 JSONL 后端的提交边界，不覆盖文档存储或其他独立后端。跨后端清理与恢复由业务用例或执行流程显式协调。
- 会话删除前的活动 Task 检查由会话删除用例在事务内执行；存储只提交删除事实及关联索引变化。
- 用户输入、最终 Assistant 与 Task 终态在事务成功后由业务调用方显式发布提交 Hook。回滚与幂等重试不通知；Hook 失败只记录日志，不改变提交结果。
- 执行事件是进程内瞬时通知，不能作为恢复依据或隐式核心编排机制。

## 风险与回滚边界

- Task 状态机、JSONL 字段和事件格式、Provider 协议、Wails DTO 及 FIFO 语义是独立于目录组织的稳定边界。
- 架构变更不得通过迁移标记、兼容包或自动重放工具副作用修补数据；发现格式或恢复问题时停止写入并保留原始日志。
- 发布回滚使用完整可构建版本与匹配的数据备份，不混用不同版本的执行组件和存储适配文件。

## 验证

```bash
cd backend
gofmt -l .
go build ./...
go vet ./...
```

前端协议涉及变更时重新生成 Wails 绑定，再执行前端静态检查与生产构建：

```bash
cd backend
go run -tags bindings .
cd ../frontend
npm run check
```

依赖方向通过代码审查和包导入检查核对，不设置依赖门禁脚本。人工回归覆盖输入幂等、FIFO、暂停与取消、批准与拒绝、启动恢复、未知副作用、会话删除、配置重载及资源关闭；不自动启动服务或运行自动化测试。
