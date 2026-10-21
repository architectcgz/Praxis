# Praxis JSONL 单事件持久化方案

## 背景与目标

持久化以单个业务事实为写入单位。Service 编排写入顺序和失败恢复，Storage 管理进程内互斥、条件校验与内存 State，JSONL 管理可靠文件追加和恢复。

核心约束：

1. 每行 JSONL 是一个 Event，每次写入只发布该 Event 对应的状态。
2. Repository 的条件检查、序号分配、追加和索引更新处于同一把 Store 锁内。
3. 内存 State 保存已持久化实体及 `groups/unique` 索引，通过逐 Event Replay 重建。
4. 多步骤业务保留已落盘事实，以固定请求身份和明确的阶段状态恢复。
5. Execution 是执行状态权威来源；消息是输出与工具回执的权威来源。

## 职责与目录

```text
backend/
├── main.go                         # 应用单实例运行权与桌面入口
├── instance_unix.go                # Unix 非阻塞独占锁
├── instance_windows.go             # Windows 非阻塞独占锁
└── internal/
    ├── core/                       # 业务实体、不可变身份与状态转换
    ├── repository/                 # 查询、单事实写入及条件更新契约
    ├── service/                    # 准入、队列、工具、结算与恢复编排
    └── infra/
        ├── storage/
        │   ├── store.go            # 进程内互斥和生命周期
        │   ├── write.go            # 单事件校验、追加和索引更新
        │   ├── state.go            # 已提交实体及 groups/unique 索引
        │   ├── projection.go       # Agent、Execution 查询投影
        │   ├── relations.go        # 单实体引用与业务约束
        │   ├── replay.go           # 逐事件恢复 State
        │   └── repositories_codec.go
        └── jsonl/
            ├── codec.go            # Event 与严格 JSON 解码
            ├── replay.go           # 单 Event Append/Replay
            ├── durable_file.go     # fsync、短写与失败收敛
            └── log.go              # 文件寻址与日志游标
```

Service、Loop 和 Coordinator 通过 Repository 使用存储，不持有存储锁。持锁内部函数不能调用会再次获取 Store 锁的 Repository 入口。

## 单事件写入与恢复

Event 字段：

| 字段 | 含义与约束 |
| --- | --- |
| `seq` | 文件内从 1 开始连续递增的序号，不允许回绕 |
| `event_id` | 固定为 `event:<seq>` |
| `type` | 与实体及操作对应的事件类型 |
| `collection` | 实体集合 |
| `key` | 集合内稳定身份；消息含逻辑流身份 |
| `created_at` | 非零事件时间 |
| `payload` | 当前结构的实体正文；会话删除事件不含正文 |

写入顺序：

```text
获取 Store.mu
→ 读取正式 State 并检查条件版本、引用和唯一性
→ 分配 Event 序号；Message 使用同一序号
→ 追加单个 Event 并 fsync
→ 更新对应实体与索引
→ 释放 Store.mu
```

追加失败时不发布内存状态。文件写入结果无法确定时停止后续访问，重启后以日志恢复。首次文件发布、目录同步、短写处理均由 JSONL 负责。

启动按文件逐行解码、校验身份及关联后更新 State；完整坏行直接报错，不完整尾行先备份再截断。重复事件、日志序号跳跃、跨会话引用和删除之后的事件均被拒绝。恢复只接受当前结构，不提供迁移或兼容读取。

应用入口在初始化数据目录和启动 Wails 前获取用户级独占锁，路径固定为 `os.UserCacheDir()/praxis/instance.lock`，不随数据目录变化。持锁至 Wails 返回，关闭句柄但不删除锁文件；异常退出由操作系统释放锁。JSONL 不持有跨进程锁。

## 业务提交点与恢复规则

| 路径 | 权威事实与写入顺序 | 中间状态与恢复 |
| --- | --- | --- |
| Project 创建 | 保存 Project；默认 Workspace 由 Project 派生 | 其他 Workspace 独立保存；目录保留供重试 |
| Session 创建 | 保存 Session，再保存初始 Agent | 未有 Agent 的会话不进入就绪目录；固定身份重试补齐 Agent |
| Task 入队 | Session Service 将输入放入会话内存队列 | 不绑定 Agent 或模型，不写日志和用户消息；重启后不恢复 |
| Task 领取 | 队列锁内领取队首；准入时保存用户消息，再保存带 `SourceTaskID` 的 Execution | Task ID 派生固定请求身份；失败放回队首，Execution 保存成功后完成领取，孤立消息隐藏且不进入模型历史 |
| Task 取消 | 队列锁内移除尚未领取的输入 | 不保存取消记录，不影响当前 Execution |
| Assistant | 只保存 Assistant 消息 | 从最后一条有效 Assistant 派生完成证据；恢复已有完成证据时直接结算 |
| ToolResult | 只保存工具回执消息 | 按调用身份幂等配对，输出字节从回执派生 |
| Pause/Stop | 先保存绑定 Execution 的控制意图，再通知运行态 | 控制与结算竞争通过条件更新重新仲裁；迟到命令不影响后续执行 |
| 工具停止 | 先保存 `tool.stop_requested`，取消回执随后独立追加 | 回执缺失由业务恢复补齐；不因回执写入失败重跑工具 |
| 审批 | 独立保存 Approved 事件，启动前重新授权并保存 Started | 审批事实、启动事实和回执分别恢复；未知副作用不自动重试 |
| Execution 结算 | 条件保存 `Execution(ended)` | 终态即提交点；Hook、自动命名失败不改变业务终态，不能重跑模型或工具 |

Agent 的 `State/CurrentExecutionID`、控制命令的 `Status/AppliedAt`、Execution 的 `ToolCalls/FinalAssistantID/FinalStopReason` 均为查询投影，不单独持久化。Task 只包含 Session 待办身份、正文、队列序号和创建时间，由 `core/session.TaskQueue` 管理；`core/session.ActiveTasks` 保存共享的进程内执行占用与结算重试结果。模型输出消耗保存为 `ModelOutputBytes`，查询时加上工具回执正文的字节数得到总 `OutputBytes`。

没有有效 Execution 的输入不进入普通消息列表。冻结上下文时明确排除未准入输入，之后补齐也不能改变已冻结的历史选择；尚在内存队列中的 Task 不进入消息流。

Execution 条件更新版本来自自身事件与关联控制命令的最新序号。结算发生版本冲突时重新读取并仲裁，仅重试未提交的状态转换。`ended` 成功后控制命令的 `applied` 状态由终态派生，附属命名允许重试且不能覆盖人工标题。

恢复顺序为：确认旧执行者已停止，补齐缺失工具回执，再接管活动 Execution。内存待办与会话模型选择均不恢复。孤儿工具调用补写 `No result provided`，不自动重放副作用。普通运行中不能提前恢复仍活跃的工具。

提交后 Hook 由业务方在存储锁外显式发布，使用非事务 Context，属于 best-effort 通知；失败只记录日志，幂等重试不重复通知。

## 风险与回滚

- 多步骤写入失败可能保留前几个事实，调用方只能恢复该阶段，不能假设整体撤销。
- 启用前备份完整数据目录，不用真实用户数据进行故障验证。
- 不符合当前结构的数据明确报错，不静默忽略历史，不自动改写数据。
- 回滚时同时恢复匹配的程序和数据备份，禁止用不匹配的程序继续追加日志。

## 验证方式

静态验收：

```bash
cd backend
gofmt -l internal/infra/storage internal/infra/jsonl
go build ./...
go vet ./...
go list ./...
bash scripts/check-dependencies.sh
```

依赖门禁同时检查持久化单事件约束。不新增或运行自动化测试，不自动启动开发服务。

人工使用已有运行环境和独立临时数据目录，核验以下边界：

- 输入消息已写入、Execution 尚未创建时崩溃；重复请求补齐且孤立消息不混入历史。
- Session 内存队列的领取与取消竞争、并发入队的 FIFO 序号、准入失败重入及重启后的待办释放。
- 控制命令与结算竞争，终态确定后附属命名失败不触发重复执行。
- Assistant 或 ToolResult 已写入后崩溃，完成证据及预算统计恢复一致。
- 审批后重启、Started 后缺失回执、普通运行中的活动工具保护。
- JSONL 尾行截断、完整坏行、重复事件、序号跳跃和会话删除后事件。
- 应用单实例锁的重复启动拒绝及异常退出后的运行权释放。
