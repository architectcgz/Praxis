# 命令幂等简化实施计划

## 1. 目标

所有状态变更命令都携带调用方可稳定重试的身份。幂等事实由拥有业务结果的表保存，不使用通用 `CommandReceipt` 和独立 result JSON 文件。

核心规则：

```text
同一命令身份 + 相同规范化参数 = 返回既有结果
同一命令身份 + 不同规范化参数 = request conflict
不同命令身份 = 独立业务操作
```

JSONL message ID 只负责 transcript 追加幂等，不承担应用命令幂等。

## 2. 命令身份

| 命令 | 幂等身份 | 结果事实位置 |
|---|---|---|
| CreateProject | 调用方提供的 `ProjectID` 和 `WorkspaceID` | projects、workspaces |
| CreateSession | 调用方提供的 `SessionID` 和 `AgentID` | sessions、agents、初始 context revision |
| SendInput / Resume | `RequestID`，在 execution start identity 上唯一 | agent_executions |
| QueueWork | `WorkItemID` | queued_work_items |
| PauseAgent / CloseAgent | `AgentControlCommandID` | agent_control_commands |
| AppendSessionContext | `ContextEntryID` | session_context_entries |
| UpdateAgentSecurityPolicy | `(AgentID, PolicyRevision)` | agent_security_policies |
| CompleteContextDelivery | `DeliveryID` 和提交的 artifact receipt | context_deliveries.result_execution_id |

所有创建类 contract 要求调用方在发送前生成资源 ID。服务端不在 durable command 执行中临时生成调用方无法重现的资源身份。

## 3. 数据库约束

### 3.1 创建类命令

- `projects.id`、`workspaces.id`、`sessions.id`、`agents.id`、`queued_work_items.id` 保持主键唯一。
- 多对象创建必须在一个 SQLite 事务内完成。
- 重试时读取全部目标对象并比较规范化参数；全部一致返回既有结果，任一不一致返回 conflict。

### 3.2 Execution start

`agent_executions.start_request_id` 建立唯一约束。重试通过该字段返回既有 Execution，不生成新的 ExecutionID。

### 3.3 SessionContext append

为每个 Context Entry 增加调用方提供的 `entry_id`：

```text
PRIMARY KEY(entry_id)
UNIQUE(session_id, revision)
```

处理顺序：

1. 先按 `entry_id` 查询；相同内容返回既有 entry。
2. 不存在时检查 `ExpectedRevision`。
3. 追加下一 revision。
4. 并发唯一冲突后重新读取并判定幂等或 conflict。

### 3.4 Policy update

`agent_security_policies` 对 `(agent_id, revision)` 唯一，并保存规范化 policy digest。相同 revision 和 digest 返回既有 policy；digest 不同返回 conflict。

### 3.5 Delivery completion

`context_deliveries` 保存：

```text
artifact_entry_ref
result_execution_id
delivered_at
```

重复提交相同 artifact receipt 返回同一个 `result_execution_id`；不同 receipt 返回 conflict。

## 4. API 与前端

- Wails contract 将资源 ID 标记为必填。
- 前端在发起命令前生成并持有 ID，失败重试复用同一 ID。
- 前端只有在用户明确发起新操作时才生成新 ID。
- 网络重试、超时重试和应用层 retry 不得重新生成 ID。
- ID 不用于内容去重；相同内容的两次明确用户操作仍使用不同 ID。

## 5. Repository 幂等算法

每个 Repository 使用相同顺序：

```text
1. 按命令或资源身份读取
2. 已存在：比较规范化参数
3. 一致：返回既有对象
4. 不一致：返回 ErrRequestConflict
5. 不存在：执行写入
6. 唯一冲突：重新读取并再次比较
```

不能只依赖“先查后写”，数据库唯一约束负责关闭并发竞争窗口。

## 6. 删除范围

完成业务表幂等后删除：

- `domain/command.CommandReceipt`
- `persistence.CommandReceiptRepository`
- `command_receipts` 表
- Document Store 的 `command` collection
- `commandprotocol.FindReceipt`
- 各 application service 对 CommandReceipt 的依赖
- Receipt result payload 的 encode/decode 代码

参数规范化和 digest helper 可以保留，用于比较同一身份是否携带相同请求内容。

## 7. 实施步骤

### 阶段 1：补齐业务身份

- 为 CreateProject、CreateSession、Context append 的 contract 增加必填资源 ID。
- 为 delivery 增加 `result_execution_id`。
- 为 execution start、policy 和 context 增加唯一约束与 digest 字段。

### 阶段 2：逐命令迁移

按以下顺序迁移并测试：

1. QueueWork
2. Agent control
3. SendInput / Resume
4. AppendSessionContext
5. UpdateAgentSecurityPolicy
6. CompleteContextDelivery
7. CreateProject / CreateSession

### 阶段 3：删除通用 Receipt

- 删除 Repository、表、Document collection 和组合依赖。
- 更新 recovery，只依赖业务状态和稳定 ID。
- 更新 architecture 文档中的命令身份表。

## 8. 测试

每种命令至少覆盖：

- 同一 ID、相同参数连续调用两次，只产生一份业务结果；
- 同一 ID、不同参数返回 conflict；
- 两个并发调用只有一个创建成功，另一个返回同一结果；
- 事务在任一步骤失败时不留下部分业务对象；
- 服务重启后使用同一 ID 仍返回既有结果；
- JSONL 重复检查与命令幂等互不替代。

## 9. 风险与处理

| 风险 | 处理 |
|---|---|
| 调用方重试时生成新 ID | API client 在操作创建时生成一次，并在 retry 生命周期内保存 |
| 多对象创建只存在部分对象 | 所有对象和初始状态在同一 SQLite 事务中提交 |
| 并发请求绕过先查逻辑 | 依赖数据库主键或唯一约束，并在冲突后重新读取 |
| 参数比较受空白或顺序影响 | 写入前规范化，digest 基于规范化结构计算 |
| 业务表无法返回首次结果 | 将必要结果 ID 保存到拥有该状态转换的业务行 |

## 10. 验收条件

- [ ] 所有 durable command 都有稳定、可重试的业务身份。
- [ ] 同一身份不同参数统一返回 `ErrRequestConflict`。
- [ ] 创建类命令不依赖服务端临时生成且无法重现的资源 ID。
- [ ] `command_receipts` 表和 `CommandReceiptRepository` 已删除。
- [ ] Document Store 不存在 `command` collection 写入。
- [ ] 幂等性由数据库唯一约束关闭并发竞争窗口。
- [ ] `go test -race ./...`、`go test ./...` 和 `go vet ./...` 通过。

## 11. 建议提交拆分

1. `feat(命令): 为写操作补齐稳定业务身份`
2. `refactor(幂等): 将重试结果收敛到业务表`
3. `refactor(命令): 删除通用 CommandReceipt`
4. `test(幂等): 覆盖并发重试和参数冲突`

