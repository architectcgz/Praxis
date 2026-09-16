# Event 关系化实施计划

## 1. 目标

DomainEvent 是可查询的审计事实，完整保存在 SQLite。Event 不创建独立 JSON 文档，不包含任意 `Payload` map，也不依赖 `document_ref`。

Event 的用途是：

- 记录 durable command 已发生的低敏审计事实；
- 按 Session、Agent、Execution 和时间查询活动记录；
- 唤醒 UI 或调度器重新查询权威状态；
- 辅助排查状态转换，不替代业务表和 recovery 查询。

## 2. Event 类型

目标保留以下类型：

| 分类 | 类型 |
|---|---|
| Delegation | `delegation_pending`、`delegation_approved`、`delegation_rejected`、`delegation_cancelled` |
| Agent | `agent_started`、`agent_pausing`、`agent_paused`、`agent_settled`、`agent_failed`、`agent_interrupted`、`agent_closed`、`agent_created`、`agent_policy_updated` |
| Execution | `execution_started`、`execution_settled` |
| Queued Work | `queued_work_created`、`queued_work_started`、`queued_work_resumed`、`queued_work_settled`、`queued_work_cancelled` |
| Workspace Lease | `lease_acquired`、`lease_released` |
| Artifact | `artifact_submitted`、`artifact_approved`、`artifact_rejected` |
| Delivery | `delivery_created`、`delivery_delivered`、`delivery_failed` |
| Project / Session | `project_created`、`session_created`、`session_context_appended` |

## 3. 领域模型

`DomainEvent` 使用显式字段表达审计信息：

```text
ID / Type / OccurredAt
ProjectID / WorkspaceID / SessionID
AgentID / TargetAgentID / ExecutionID
WorkItemID / DelegationID / DeliveryID
ArtifactKind / ArtifactID
ApprovalSource
PolicyRevision
ContextRevision / ContextKind
ExecutionOutcome / FailureCode
```

字段按 EventType 组合使用。领域校验定义每种事件允许和必需的字段，禁止使用通用 map 扩展未知数据。

示例：

```text
project_created
    requires ProjectID, WorkspaceID

agent_policy_updated
    requires SessionID, AgentID, PolicyRevision

execution_settled
    requires SessionID, AgentID, ExecutionID, ExecutionOutcome
    allows FailureCode

artifact_approved
    requires SessionID, AgentID, ArtifactKind, ArtifactID
    allows TargetAgentID

session_context_appended
    requires SessionID, ContextRevision, ContextKind
```

## 4. SQLite Schema

`orchestration_events` 使用显式可空列：

```text
id                 PRIMARY KEY
event_type         NOT NULL
occurred_at        NOT NULL
project_id
workspace_id
session_id
agent_id
target_agent_id
execution_id
work_item_id
delegation_id
delivery_id
artifact_kind
artifact_id
approval_source
policy_revision
context_revision
context_kind
execution_outcome
failure_code
```

删除 `document_ref`。空关系使用 SQL `NULL`，不使用空字符串模拟缺失值。

索引：

```text
(session_id, occurred_at, id)
(agent_id, occurred_at, id)
(execution_id, occurred_at, id)
(event_type, occurred_at, id)
```

## 5. Repository

Event Repository 回到 `infrastructure/sqlite`：

- `Append` 在业务状态事务内插入一行；
- `ListBySession`、`ListByAgent`、`ListByExecution` 直接扫描 SQLite；
- 排序统一为 `(occurred_at, id)`；
- 分页使用稳定 cursor，不加载 Document Store；
- 重复 EventID 返回幂等成功或稳定冲突，取决于字段是否一致。

查询 Event 列表只产生一次 SQL 查询，不产生按条文件读取。

## 6. Application 调整

- 每个用例使用构造函数创建合法 Event，而不是先创建空对象再修改 map。
- Project、Workspace、Artifact、Policy、Context 和 Settlement 的附加信息写入显式字段。
- Event 只记录已经在同一事务内生效的状态转换。
- UI 通知可以携带 EventID，但仍通过 query service 获取权威列表。

## 7. 实施步骤

1. 扩展 `DomainEvent` 显式字段和逐类型校验。
2. 修改所有 Event 创建点，移除 `Payload`。
3. 修改 `orchestration_events` schema，删除 `document_ref` 并增加显式列和索引。
4. 将 Event Repository 移回 `infrastructure/sqlite`。
5. 从 `storage.Store`、Document collection 和组合装配中删除 Event。
6. 增加按 Session、Agent、Execution 查询和分页测试。
7. 增加断言，确保追加 Event 不创建 document 文件。

## 8. 风险与处理

| 风险 | 处理 |
|---|---|
| EventType 与字段组合失配 | 统一构造函数和 `Validate` 按类型校验 |
| 可空列过多 | Event 是有限审计模型；只增加真实使用的字段 |
| 将 Event 当作恢复事实 | Recovery 始终查询业务状态表，Event 只用于审计和唤醒 |
| Event 中泄漏正文或密钥 | 字段仅允许 ID、枚举、revision 和稳定 failure code |

## 9. 验收条件

- [ ] `DomainEvent` 不含 `Payload` map。
- [ ] `orchestration_events` 不含 `document_ref` 或 JSON payload。
- [ ] Event Repository 不依赖 Document Store。
- [ ] 31 种 EventType 均有合法字段组合测试。
- [ ] Event 查询只访问 SQLite，并使用稳定排序和分页。
- [ ] Event 不参与业务状态恢复决策。
- [ ] `go test ./...` 和 `go vet ./...` 通过。

## 10. 建议提交拆分

1. `refactor(Event): 使用显式审计字段替代 Payload`
2. `refactor(SQLite): 将 Event 持久化收敛为关系表`
3. `test(Event): 覆盖事件字段约束和分页查询`

