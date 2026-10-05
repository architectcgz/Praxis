# Turn 与 JSONL 存储方案

## 目标与边界

Praxis 使用本地单进程日志保存 Project、Workspace、Session、Agent、Turn、消息、工具调用、队列、上下文、控制命令和耗时记录。日志是事实来源，内存投影提供查询；模型配置、Agent 定义、凭据和附件仍使用各自的文件边界。

应用只读取当前唯一格式，不提供格式版本字段或运行时兼容分支。

## 领域模型

```text
Session
└── Turn
    ├── InputSnapshot
    │   ├── ModelContext
    │   ├── ModelSnapshot
    │   ├── SecuritySnapshot
    │   ├── WorkspacePath
    │   ├── ContextDigest
    │   ├── MessageSequenceBoundary
    │   └── CurrentInputMessageID
    ├── ToolInvocations
    └── Messages
```

- `Turn` 表示用户输入、队列任务或恢复操作对应的一次完整回合；一个 Turn 可以包含多个模型请求。
- `InputSnapshot` 保存 Agent 定义身份与版本、模型可见上下文、模型配置、安全策略和工作区。输入在创建时冻结，生命周期更新不能修改它。
- `ModelContext` 只包含 Provider 无关的系统提示与有序上下文条目。
- `SecuritySnapshot` 是 Sandbox、Approval、工具权限、资源限制和安全指纹的唯一来源。工具调用携带 `TurnID`、身份、工作区和安全指纹，由服务与持久化快照核对。
- `Step` 表示 Turn 内的模型请求序号，从 1 开始。前后端均使用 `turnId` 关联回合，使用 `step` 关联流式输出和模型请求。
- `ToolInvocation` 独立记录审批、执行和终态，消息的 `tool_call` / `tool_result` block 负责恢复模型对话。

Turn 状态为 `starting → running → settling → settled`，结果为 `completed`、`yielded`、`paused`、`failed` 或 `interrupted`。同一 Agent 只能有一个活动 Turn，同一 `(AgentID, RequestID)` 只能对应一个 Turn。

## 文件布局与归属

```text
runtime/
├── projects.jsonl
├── sessions/
│   └── <session-id>.jsonl
├── writer.lock
├── documents/
└── attachments/
```

`projects.jsonl` 保存 Project 与 Workspace，允许在同一提交中创建两者的相互引用。每个 Session 文件保存自身的全部运行事实。

Session ID 只存在于文件名，事件及 payload 不保存 `SessionID`、`sessionId` 或 `session_id`。Session 对象自身的 ID 也由文件名注入。文件名使用小写 ASCII 字母、数字、连字符和下划线，拒绝路径分隔符、设备名和非 canonical 值；避免大小写不敏感文件系统的身份冲突。

Session 消息不记录 owner ID；Agent 私有消息记录目标 Agent ID。读取时由 Store 恢复 Session 归属并校验引用。消息身份按 Session、owner、消息 ID 共同限定；用于区分 Session 的前缀只存在于内存键中。

Session 元数据投影承担目录与标题索引职责，标题搜索不读取消息正文。目录由 Session 日志重建，不另外维护一个需要双写的持久化目录文件。

## 提交格式

每一行是一个事务提交，包含同一日志文件的一组有序事件。换行是完整提交的边界。

```json
{
  "events": [
    {
      "seq": 1,
      "event_id": "event:1",
      "type": "session.saved",
      "collection": "session",
      "created_at": "2026-03-24T10:00:00Z",
      "payload": {
        "ProjectID": "project_example",
        "WorkspaceID": "workspace_example",
        "Title": "JSONL 存储设计",
        "State": "active",
        "CreatedAt": "2026-03-24T10:00:00Z",
        "UpdatedAt": "2026-03-24T10:00:00Z"
      }
    }
  ]
}
```

上例位于 `<session-id>.jsonl`。实际写入使用单行紧凑 JSON，并以换行结尾。

事件约束：

- `seq` 在文件内连续递增，Store 分配序号；消息序号等于其追加事件序号。
- `event_id` 为文件内的 `event:<seq>`，结合文件身份定位事件。
- `type` 表示变更行为，`collection` 指定投影类型，`key` 指定对象身份。Session 的 key 从文件名确定，因此省略。
- `payload` 保存提交时的对象状态；身份、冻结输入及工具参数的不可变约束由仓储校验。
- 时间保存为 UTC RFC3339Nano。

| 事件 | 内容 |
|---|---|
| `project.saved` / `workspace.saved` | 项目与工作区 |
| `session.saved` / `session.deleted` | 会话元数据或删除标记 |
| `agent.saved` / `policy.saved` | Agent 状态与策略版本 |
| `turn.saved` | Turn 创建、运行与结算状态 |
| `tool.saved` | 工具准入、审批、执行与结果 |
| `queue.saved` / `wait.saved` / `control.saved` | 队列、等待与控制状态 |
| `context.entry_appended` | 共享事实 |
| `message.appended` | 用户、assistant 和工具消息 |
| `timing.saved` | 独立操作耗时 |

## 写入、事务与幂等

`infra/jsonl.Store` 实现 `repository.TxRunner`，所有仓储共享同一个写入入口。

```text
获取进程内锁
  → 复制投影索引作为事务工作区
  → 在工作区校验幂等并执行关联变更
  → 校验引用、唯一性和最终业务状态
  → 编码一行提交，写入并 Sync
  → 发布新的内存投影
  → 返回应用服务，激活 runtime 或发布事件
```

- 同一事务只能写入一个 Session 文件，或只写入 Project/Workspace 文件。跨文件变更返回错误并回滚。
- 进程内锁串行分配序号；操作系统文件锁阻止同一数据根的第二个写入进程。进程退出时系统释放锁。
- 事务回调失败时丢弃工作区，不写入日志。
- 文件写入失败时尝试截断到提交前偏移并同步；回滚失败后封锁写入，要求重新打开日志确定事实。
- 相同消息身份与内容的重试返回已保存的消息和序号；内容不同返回冲突。
- Turn 输入和工具参数不可修改；工具调用身份由 `(TurnID, ProviderToolCallID)` 唯一限定，已完成调用返回持久化结果。
- 每次查询返回从投影解码得到的独立对象，调用方不能修改存储内部快照。

本地实现复制索引并校验完整引用图，写入成本随数据量增长。该实现面向个人本地数据规模；只有实测成为瓶颈后，再引入按 Session 划分索引或变更集校验。

## 恢复与损坏处理

1. 获取数据根写入锁。
2. Replay Project/Workspace 文件，再按 Session 文件名恢复每个 Session。
3. 校验事件结构、连续序号、对象身份、不可变字段和每个提交后的引用图。
4. 重建目录、消息、Turn、工具调用、队列、策略和上下文投影。
5. 将活动 Turn 和 Agent 收敛为 `interrupted`，并同步结算关联的运行队列项。
6. 未启动工具调用收敛为 `interrupted`，已经运行但没有终态的调用收敛为 `unknown`；追加错误结果消息，不重放副作用。
7. 未结束耗时记录收敛为未知时长的中断状态，不推算结束时间。
8. 恢复结果落盘后才接受新输入。

最后一行没有换行时属于未完成提交：先完整保存诊断副本，再截断日志到最后一个完整提交，随后才能追加。即使尾部 JSON 本身可以解析，也不将缺少提交换行的记录视为已提交。

完整行损坏、序号跳跃或重复、非法引用和快照篡改会阻止启动并报告文件位置。不会通过忽略中间记录继续执行。

删除 Session 追加删除标记，移除全部相关内存投影；启动 replay 后不会复活已删除 Session。日志保留至独立的数据清理操作，业务删除不声称立即擦除磁盘字节。

## 消息与模型上下文

主 Agent 读取 Session 消息，共享 ContextEntry 参与其上下文构建；协作 Agent 读取自己的私有消息，不因共享 Session ID 自动获得主 Agent 的历史。

创建 Turn 时：

1. 读取主体可见的共享事实与消息。
2. 按优先级和预算选择共享事实，按完整 Turn 选择历史消息。
3. 通过 `CurrentInputMessageID` 将当前输入加入一次。
4. 过滤 thinking block，保留 assistant tool call / tool result 顺序。
5. 计算 `ContextDigest`，冻结消息边界、ModelContext、模型和安全配置。

运行时从冻结上下文的独立副本开始，持久化新的 assistant 和工具结果后追加到当前上下文，供下一 Step 使用。Provider adapter 负责协议转换。历史消息按预算裁剪时不修改日志。

UI 以持久化消息和 Turn 作为最终事实；实时事件只用于增量显示。耗时记录通过 Turn ID 与模型 Step 的消息引用关联。

## 验证与发布边界

- 事务回滚、写盘失败和跨文件事务不会泄露未提交投影。
- 重复消息、冲突输入、冻结快照变更和双写入进程被正确处理。
- 并发消息追加保持连续事件序号；不同 Session 的同名消息相互隔离。
- Session 删除与标题查询在 replay 后保持一致。
- 尾部截断可修复；完整损坏行明确失败。
- 活动工具恢复为结果未知；重复工具调用读取持久化结果，不重新访问执行目标。
- 队列 Turn 恢复时同步释放 Agent 活动状态和运行队列项。
- 多步模型循环使用冻结输入和持久化工具结果，thinking 不进入后续 Provider 请求。
- Go 全量测试、前端构建与前端测试必须通过。

运行时只读写目标日志，不包含历史格式解释逻辑。
