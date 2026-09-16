# 关系与文档存储边界实施计划

## 1. 目标

建立三个职责单一的基础设施 package：

```text
infrastructure/sqlite
    关系、状态、约束、索引、事务和恢复查询

infrastructure/document
    不可变 JSON 文档的内容寻址、原子发布和读取

infrastructure/storage
    组合 SQLite 行与外部文档，实现 persistence repository
```

`sqlite` 不导入、持有或调用 `document.Store`。应用组合根分别创建两种 Store，再由 `storage.Store` 组合。

## 2. 数据归属

### 2.1 纯 SQLite 对象

以下对象只需要关系字段和有限状态，直接由 SQLite Repository 实现：

- Project
- Workspace
- Session
- Agent
- WorkspaceWriteLease
- ContextDelivery
- AgentControlCommand
- DomainEvent

### 2.2 SQLite 与 Document 组合对象

| 对象 | SQLite | Document Store |
|---|---|---|
| AgentExecution | ID、归属、状态、时间、幂等键、恢复索引、`document_ref` | 完整 execution 输入和不可变快照 |
| ExecutionSecuritySnapshot | ExecutionID、policy revision、`document_ref` | capability、路径和 sandbox 约束 |
| ToolInvocation | ExecutionID、provider call ID、状态、时间、摘要、`document_ref` | arguments、result 和结构化错误内容 |
| AgentSecurityPolicy | AgentID、revision、时间、`document_ref` | 完整 policy |
| SessionContextEntry | SessionID、revision、kind、来源、时间、`document_ref` | context 正文 |
| QueuedWork | ID、顺序、状态、ExecutionID、时间、`document_ref` | prompt 和 execution input |
| WaitCondition | ID、归属、kind、mode、状态、`document_ref` | target 和 resolved target 列表 |
| DelegationRequest | ID、归属、profile、状态、`document_ref` | manifest、grant 和扩展内容 |
| AgentResult | ID、归属、状态、时间、`document_ref` | summary、路径和 evidence refs |
| Briefing | ID、归属、审核状态、时间、`body_ref` | Briefing 正文 |
| Note | ID、归属、标题、时间、`content_ref` | BodyRef、excerpt 和 tags |

Event 和命令幂等记录不进入 Document Store，分别由后续专项计划处理。

### 2.3 Agent JSONL

Agent JSONL 继续独立保存：

- transcript message；
- `execution_started` 和 `execution_settled` receipt；
- tool invocation/result receipt；
- artifact receipt。

Document Store 不复制 transcript，SQLite 不保存 provider 原始 payload。

## 3. Document Reference

完整引用是文档的唯一定位键：

```text
{collection}/{object_id}/sha256-{canonical_json_digest}.json
```

示例：

```text
execution/execution_123/sha256-a8f3...c91.json
tool-invocation/invocation_456/sha256-c4d2...701.json
session-context/session_123-8/sha256-f91a...330.json
```

规则：

1. `collection` 和 `object_id` 只能包含安全路径片段。
2. SHA-256 基于最终写入的 JSON 字节计算。
3. 同一对象和相同内容返回相同引用。
4. 同一对象内容变化产生新引用，旧文档保持不可变。
5. Store 只接受完整引用读取，不提供按业务 ID 扫描文件系统的查询。
6. 所有业务筛选先查询 SQLite，再按结果中的引用精确读取文档。

## 4. 写入与事务

组合 Repository 的写入顺序固定为：

```text
1. 校验领域对象
2. 确认当前 context 位于 sqlite.Store.InTx 内
3. 原子发布内容寻址文档
4. 在同一 SQLite 事务内写入关系字段和 reference
5. 提交 SQLite 事务
```

文档发布使用同目录临时文件、`fsync` 和原子 rename。SQLite 回滚时可以留下不可达文档，但不能提交指向缺失文档的引用。

读取顺序固定为：

```text
1. SQLite 按 ID、状态或关系条件查询 metadata 和 reference
2. Document Store 按 reference 读取 JSON
3. 校验文档身份与 SQLite 行一致
4. 组装并校验领域对象
```

身份校验至少覆盖对象 ID、父级 ID 和不可变 revision。SQLite 是关系和状态查询的事实源。

## 5. 实施步骤

### 阶段 1：Document Store

- 实现文件模式和内存模式。
- 实现路径校验、敏感字段拒绝、内容摘要和原子发布。
- 将文档根目录固定为 `{DataRoot}/runtime/documents`。
- 测试相同内容幂等、内容变化、非法引用、敏感字段和 Windows 路径兼容性。

### 阶段 2：SQLite 边界

- schema 删除通用 `payload` 列。
- 为组合对象增加关系字段和语义明确的 reference 列。
- `sqlite.Store` 只暴露事务、SQL executor、时钟、migration 和 integrity check。
- 纯关系 Repository 直接扫描显式列。
- 启动时验证目标 schema 的关键列；不兼容 schema 明确拒绝启动。

### 阶段 3：组合 Repository

- 将需要文档的 Repository 放入 `infrastructure/storage`。
- 由 `storage.Store` 持有 `*sqlite.Store` 和 `*document.Store`。
- 保持现有 `persistence` 接口，不向 application 暴露 reference。
- 为每个组合 Repository 增加一次写入、读取和事务回滚检查。

### 阶段 4：组合根

- `compose` 分别打开 SQLite 和 Document Store。
- 纯关系 Repository 从 `sqlite.Repositories` 获取。
- 组合 Repository 从 `storage.TargetRepositories` 获取。
- Artifact resolver 只读取允许暴露的结构化内容，不读取 transcript 或敏感字段。

## 6. 风险与处理

| 风险 | 处理 |
|---|---|
| SQLite 已提交但文档不存在 | 文档必须先成功发布，SQLite 才能提交 reference |
| SQLite 回滚留下不可达文件 | 允许保留；达到可测容量阈值后再增加离线 GC |
| 文档与 SQLite 身份不一致 | 组合读取时比较 ID、父级 ID 和 revision |
| 文件路径逃逸 | reference 必须恰好由三个安全路径片段组成 |
| 密钥进入 Document Store | 写入前递归拒绝 credential、authorization、secret 和 provider 原始 payload 字段 |
| 大量列表查询触发 N 次文件读取 | 列表只返回 Summary；详情查询再加载文档 |

## 7. 验收条件

- [ ] SQLite schema 不存在名为 `payload` 的列。
- [ ] `infrastructure/sqlite` 不导入 `infrastructure/document`。
- [ ] 纯关系对象读取不访问 Document Store。
- [ ] 组合对象可以从 SQLite metadata 和文档完整还原。
- [ ] 缺失、损坏或身份不一致的文档返回稳定错误。
- [ ] 所有产品 mutation 必须位于 SQLite 事务中。
- [ ] Windows 和 Unix 路径下的 document reference 均可读取。
- [ ] `go test ./...` 和 `go vet ./...` 通过。

## 8. 建议提交拆分

1. `feat(存储): 增加内容寻址 Document Store`
2. `refactor(SQLite): 使用显式关系列替代通用 payload`
3. `refactor(存储): 增加 SQLite 与 Document 组合 Repository`
4. `test(存储): 覆盖事务、引用和 schema 边界`

