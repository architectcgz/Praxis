# Agent 控制命令实施计划

## 1. 目标

对外 API 使用明确的业务动作：

```text
PauseAgent
CloseAgent
```

内部使用 `AgentControlCommand` 保存需要跨事务、runtime cancellation 和进程重启收敛的控制状态。`Control` 只作为 Pause 与 Close 的内部共同机制，不作为外部命令名称。

## 2. 命名

| 边界 | 名称 |
|---|---|
| Wails command | `PauseAgent`、`CloseAgent` |
| Contract | `PauseAgentRequest/Response`、`CloseAgentRequest/Response` |
| Application method | `PauseAgent`、`CloseAgent` |
| 内部持久化实体 | `AgentControlCommand` |
| ID | `AgentControlCommandID` |
| Kind | `AgentControlPause`、`AgentControlClose` |
| Status | `AgentControlPending`、`AgentControlApplied` |
| SQLite table | `agent_control_commands` |
| Recovery method | `ApplyPendingAgentControl` |

禁止使用 `request_control` 作为命令名。`Request` 只用于外部 transport DTO，不用于表达 Pause/Close 业务动作。

## 3. 领域对象

```text
AgentControlCommand
├── ID
├── AgentID
├── TargetExecutionID?
├── Kind: pause | close
├── Status: pending | applied
├── CreatedAt
└── AppliedAt?
```

不变量：

1. ID 由调用方在发送命令前生成，同时承担幂等身份。
2. active Agent 的控制命令固定当时的 `TargetExecutionID`。
3. `pending` 不允许 `AppliedAt`。
4. `applied` 必须有 `AppliedAt`。
5. 同一 ID 只能表示同一 Agent 和同一种 Kind。
6. 一个 Agent 同时只允许一个未完成的 control command。

## 4. PauseAgent

### 4.1 Agent 有 active Execution

```text
1. 插入 AgentControlCommand(kind=pause, status=pending)
2. Agent: executing -> pausing
3. 提交 SQLite 事务
4. 向 runtime 发送 cancellation(outcome=paused)
5. runtime 写入 execution_settled receipt
6. settlement transaction 将 Execution 结算为 paused
7. Agent: pausing -> paused
8. command: pending -> applied
```

### 4.2 Agent 没有 active Execution

在同一事务内将 Agent 转为 `paused`，并把 command 直接保存为 `applied`。不创建空 Execution，不发送 runtime cancellation。

## 5. CloseAgent

### 5.1 Agent 有 active Execution

```text
1. 插入 AgentControlCommand(kind=close, status=pending)
2. Agent: executing -> pausing
3. 提交 SQLite 事务
4. 向 runtime 发送 cancellation(outcome=interrupted)
5. settlement transaction 结算 Execution
6. Agent -> closed
7. command -> applied
```

### 5.2 Agent 没有 active Execution

在同一事务内将 Agent 转为 `closed`，并把 command 直接保存为 `applied`。

Close 保留 Agent、Execution、SessionContext 和 transcript，不删除历史数据。

## 6. 幂等与并发

`agent_control_commands.id` 是主键，不再写 CommandReceipt。

处理重复请求：

- 相同 ID、AgentID、Kind：返回既有 command；
- 相同 ID、不同 AgentID 或 Kind：返回 conflict；
- Agent 已处于目标稳定状态：保存或返回 `applied` command；
- Agent 已有其他 pending command：返回 agent unavailable；
- cancellation 发送失败：保留 pending command，由 recovery 继续收敛。

SQLite 增加部分唯一约束或等价事务检查，保证每个 Agent 最多一个 pending command。

## 7. Recovery

启动恢复扫描所有 `pending` command：

1. 读取 Agent 和固定的 TargetExecution。
2. TargetExecution 仍 active 时，保持 command pending，由 execution recovery 先收敛。
3. TargetExecution 已结算时，根据 Kind 将 Agent 投影为 paused 或 closed。
4. 将 command 标记为 applied。
5. 重复执行必须幂等。

Recovery 不根据 Event 推断控制状态，也不自动重放结果未知的模型或工具调用。

## 8. Event

Agent control 使用关系型 Event：

- Pause accepted：`agent_pausing`
- Pause completed：`agent_paused`
- Close completed：`agent_closed`

Event 保存 AgentID、ExecutionID 和 OccurredAt，不保存 document reference。Control command 表是恢复事实，Event 是审计事实。

## 9. 实施步骤

1. 将领域类型改为 `AgentControlCommand`，状态改为 `pending/applied`。
2. 将 persistence interface 和 SQLite table/repository 同步改名。
3. 将 application 对外方法拆为 `PauseAgent` 和 `CloseAgent`。
4. 将 Wails contract、binding 和前端 API 改为两个明确命令。
5. 删除 control application service 对 CommandReceipt 的依赖。
6. 将 settlement 和 recovery 接入 `ApplyPendingAgentControl`。
7. 更新 Event 创建点和查询投影。

## 10. 测试

- Pause active Agent：先 durable，再发送 cancellation，最终 paused/applied。
- Pause idle Agent：直接 paused/applied，不调用 runtime。
- Close active Agent：Execution 结算后 closed/applied。
- Close idle、paused、failed Agent：直接 closed/applied。
- 相同 command ID 重试返回既有结果。
- 相同 command ID 更换 Agent 或 Kind 返回 conflict。
- cancellation 失败后 command 保持 pending。
- 服务重启后 pending command 可以收敛。
- 并发 Pause/Close 只有一个 pending command 成功。

## 11. 验收条件

- [ ] 对外不存在名为 `RequestControl` 或 `request_control` 的命令。
- [ ] Pause 和 Close 在 contract、application 和前端 API 中语义明确。
- [ ] `AgentControlCommand` 自身承担幂等和恢复状态。
- [ ] Agent control 不依赖 CommandReceipt。
- [ ] pending command 可在进程重启后收敛。
- [ ] Event 不作为 control recovery 的事实源。
- [ ] `go test -race ./...`、`go test ./...` 和 `go vet ./...` 通过。

## 12. 建议提交拆分

1. `refactor(Agent控制): 将 ControlRequest 改为可恢复 Command`
2. `refactor(API): 拆分 PauseAgent 与 CloseAgent 命令`
3. `refactor(幂等): 使用 ControlCommandID 替代 CommandReceipt`
4. `test(Agent控制): 覆盖取消失败、重启恢复和并发命令`
