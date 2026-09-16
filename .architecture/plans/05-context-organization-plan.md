# Context 组织实施计划

## 1. 目标

Context 按共享范围、生命周期和一致性边界拆分为四层：

```text
SessionContext
    Session 内可共享的、经过明确提交的事实

Agent Transcript
    一个 Agent 实际看到和产生的私有交互历史

Content Reference
    文件、diff、artifact、Note 和 Briefing 的外部内容引用

Execution Context Snapshot
    一次 AgentExecution 创建时固定的模型可见输入选择
```

Context 组织必须满足：

1. Agent 普通输出不会自动污染 Session 共享上下文。
2. Execution 运行期间不读取后续 SessionContext revision。
3. Execution 恢复时可以重建创建时看到的输入边界。
4. Context 选择结果可审计、可校验、可通过稳定 ID 去重。
5. Provider 只接收最终的 `ModelRequest`，不读取 Session、SQLite 或 JSONL。
6. Context 选择不引入向量数据库、自动摘要或不可重复的启发式依赖。

## 2. 四层职责

### 2.1 SessionContext

SessionContext 是 Session 级追加事实，只保存用户或系统明确提交给 Session 的信息：

```text
SessionContextEntry
├── EntryID
├── SessionID
├── Revision
├── Kind
├── SourceExecutionID?
├── ContentReference
├── ContentDigest
└── CreatedAt
```

允许的 `Kind`：

```text
user_message
decision
accepted_conclusion
reference
```

规则：

- `(SessionID, Revision)` 唯一且严格递增；
- `EntryID` 由命令调用方生成，作为追加命令的幂等身份；
- 内容只能追加，不能原地修改或删除；
- `SourceExecutionID` 只表示结论或引用的来源，不代表完整 transcript 归属改变；
- `accepted_conclusion`、`decision` 和 `reference` 必须通过明确命令提交；
- 普通 Agent 回答、工具调用、工具结果和中间推理不能自动写入；
- 大正文保存到 Document Store 或文件树，SQLite 只保存 entry metadata 和 reference。

`agent_message` 不作为通用共享类型。Agent 的普通消息属于私有 transcript，只有经过用户或 Workflow 明确采纳后才能成为共享 entry。

### 2.2 Agent Transcript

每个 Agent 独立拥有一份 JSONL transcript：

```text
session_header
execution_started
message
tool_invocation_receipt
tool_result_receipt
execution_settled
```

规则：

- 一个 Agent 只有一个 transcript writer；
- transcript 记录 Agent 实际看到和产生的交互；
- transcript 不自动成为 SessionContext；
- Tool arguments/result 只在对应 execution transcript 中保存；
- resume 创建新的 Execution，但继续使用同一 Agent transcript；
- transcript 使用稳定 `message_id`、`execution_id` 和 sequence 去重；
- 模型输入只读取确定的 sequence 范围，不无限读取整个文件；
- transcript 的读取不能改变 SessionContext revision。

### 2.3 Content Reference

Content Reference 只描述外部内容，不承担 Session 或 Execution 状态：

```text
ContentRef
├── Kind: file | diff | log | artifact
├── Path?
├── Digest?
├── Excerpt?
└── Bytes
```

来源包括：

- 工作区文件和 diff；
- Tool 输出或 patch；
- AgentResult、Briefing 和 Note；
- 已批准的 ContextDelivery；
- SessionContext 中引用的外部资料。

引用必须包含 digest 或稳定路径之一，并在 Execution 创建时复制到不可变快照。模型不通过路径自行读取文件；由 Context Assembler 根据安全快照加载允许的 excerpt 或结构化内容。

### 2.4 Execution Context Snapshot

一次 Execution 创建时生成不可变快照：

```text
ExecutionContextSnapshot
├── SessionRevision
├── SelectedEntries[]
│   ├── EntryID
│   ├── Revision
│   ├── Kind
│   └── ContentDigest
├── TranscriptThroughSequence
├── SelectedTranscriptMessages[]
│   ├── MessageID
│   ├── ExecutionID
│   └── Sequence
├── ContentReferences[]
├── CurrentInput
├── AssemblyVersion
└── SnapshotDigest
```

该快照属于 `AgentExecution`，保存到 execution document。SQLite 保存 `ContextRevision`、snapshot digest 和恢复查询字段。

快照一旦创建不得替换。SessionContext 后续 revision、Agent policy 后续 revision、模型配置变化和 transcript 后续消息只影响新的 Execution。

## 3. Context 选择算法

第一版使用确定性选择，不引入 embedding、向量检索、自动摘要或基于当前时间的隐式排序。

### 3.1 输入

Context Assembler 接收：

```text
SessionID
AgentID
ExecutionID
SessionRevision
CurrentInput
TranscriptCursor
ExecutionSecuritySnapshot
ContextBudget
```

它读取：

- 指定 Session revision 范围内的 SessionContext entries；
- Agent transcript 的明确 sequence 范围；
- 当前命令携带的 queued work、delivery 或用户输入；
- 已明确选择的 Briefing、Note、AgentResult 和 ContentRef。

### 3.2 优先级

在预算内按以下顺序选择：

1. 当前用户输入或当前 queued work prompt；
2. `decision`；
3. `accepted_conclusion`；
4. 显式提交的 `reference`、Briefing、Note 或 Artifact；
5. 最近的 `user_message`；
6. Agent transcript 中仍属于当前上下文窗口的历史消息。

所有选择都按完整 entry、message 或 content reference 进行，不能先拼接字符串再从中间截断。

### 3.3 选择结果

选择结果必须记录：

- Session revision；
- 每个 entry 的 EntryID、revision、kind 和 digest；
- transcript 的起止 sequence；
- 每个 transcript message 的稳定 ID；
- 每个外部内容的 digest、大小和来源；
- assembly version；
- 最终 snapshot digest。

如果内容超出预算，按优先级完整移除低优先级项，并返回被省略项的诊断信息；不能静默把 `EntryRevisions` 标记为已选择却不把对应内容放入模型输入。

## 4. 模型输入组织

`SystemPrompt` 和业务 Context 分开：

```text
SystemPrompt
    系统规则、Agent role、工具使用约束

Messages
    1. 选中的 SessionContext entries
    2. 选中的 Agent transcript messages
    3. Briefing / Note / Artifact 内容
    4. 当前用户输入或 queued work
    5. 当前 execution 新产生的 assistant/tool messages
```

SessionContext 内容作为带来源信息的普通 message/content block 传给模型，不能因为来自 Session 就提升为 system instruction。Context entry 中的用户文本、Agent 文本和外部文件内容都视为数据，不是系统规则。

每次 ModelStream 调用创建新的不可变 `ModelRequest`：

- `ModelRequest.Messages` 是快照副本；
- Provider adapter 不修改 AgentLoop 状态；
- 工具 schema 只来自 ExecutionSecuritySnapshot 过滤后的 capability grant；
- Provider 不接收 credential、SQLite 行、Document Store reference 或完整领域对象。

## 5. 持久化与事务

### 5.1 AppendContext

```text
1. 校验 EntryID、SessionID、Kind 和内容引用
2. 在 SQLite 事务内按 EntryID 检查幂等
3. 检查 ExpectedRevision
4. 生成下一个 Session revision
5. 发布正文 Document 或文件内容
6. 写入 session_context_entries metadata、reference、digest
7. 写入关系型 session_context_appended event
8. 提交事务
```

并发追加只能有一个调用成功；相同 EntryID 和相同内容返回既有 entry；相同 EntryID 携带不同内容返回 request conflict。

### 5.2 CreateExecution

```text
1. 校验 Agent、Session 和命令归属
2. 在同一事务中读取当前 Session revision
3. 读取完整的可选 entries 和 transcript cursor
4. Context Assembler 生成 ExecutionContextSnapshot
5. 生成 ExecutionSecuritySnapshot
6. 发布 execution document
7. 写入 agent_executions metadata 和 snapshot digest
8. Agent 标记为 executing
9. 写入 execution_started event
10. 提交事务
```

事务提交后 runtime 只使用已保存的 Execution snapshot，不重新读取 SessionContext 或当前 policy。

### 5.3 Document reference

Context 文档使用内容寻址引用：

```text
session-context/{entry_id}/sha256-{digest}.json
execution/{execution_id}/sha256-{digest}.json
```

SQLite 查询负责关系和 revision；Document Store 只按完整 reference 精确读取。Context Assembler 不扫描文件系统，也不通过正文内容反向搜索 Session。

## 6. Recovery

启动恢复顺序：

1. 修复 Agent JSONL partial tail；
2. 校验 SessionContext revision 连续性和 EntryID 唯一性；
3. 校验 execution document 与 SQLite 的 SessionID、AgentID、ContextRevision 和 digest；
4. 校验 transcript receipt 与 ExecutionID、snapshot sequence 的关系；
5. 对缺失 snapshot 或身份不一致的 Execution 置为 recovery error，不使用当前 Context 重建旧输入；
6. 只为新的 Execution 创建新的 Context snapshot；
7. 所有关键校验完成后开放 command admission。

Recovery 不重放模型调用、工具副作用或 Context append。Event 只用于审计和唤醒，不能代替 SessionContext、transcript 或 Execution snapshot。

## 7. 实施步骤

### 阶段 1：领域与持久化身份

- 为 SessionContextEntry 增加 EntryID、ContentReference 和 ContentDigest。
- 将 context 正文从关系字段移到 Document Store；SQLite 保留 metadata、revision 和 reference。
- 为 ExecutionInputSnapshot 增加明确的选中 entry、transcript cursor 和 snapshot digest。
- 补齐 `(session_id, revision)`、`entry_id` 和 execution snapshot 约束。

### 阶段 2：Context Assembler

- 新增 core-owned ContextAssembler port 和 application implementation。
- 实现确定性优先级、预算、完整项选择和 digest 生成。
- 禁止通过 `boundedContextSummary` 把所有内容拼成单一 SystemPrompt。
- 为当前输入、SessionContext、transcript、artifact 和外部 ContentRef 提供独立适配器。

### 阶段 3：Execution 输入冻结

- 在 Start、Resume、QueueWork 和 ContextDelivery 创建 Execution 时统一调用 Context Assembler。
- 将 snapshot 写入 execution document，并在 SQLite 保存 digest、revision 和 cursor。
- runtime 只接受固定 snapshot，不直接调用 SessionContext repository。

### 阶段 4：Runtime ModelRequest

- 根据 snapshot 构造 system prompt 与普通 messages。
- 为每次 ModelStream 调用生成不可变消息副本。
- transcript 读取改为 cursor/sequence 范围，不读取无界历史。
- 保证 tool use 与 tool result 成对保留。

### 阶段 5：共享内容提交与恢复

- 实现用户或 Workflow 明确提交 accepted conclusion、decision 和 reference 的命令。
- 增加 compare-and-append、EntryID 幂等和 digest 校验。
- 增加启动时 revision、document、JSONL 和 execution snapshot 对账。

## 8. 测试

### 8.1 领域测试

- revision 只能递增，不能覆盖或跳号；
- 空内容、非法 Kind、无效 ContentRef 被拒绝；
- 相同 EntryID 和相同内容幂等；
- 相同 EntryID 和不同内容返回 conflict；
- Execution snapshot 创建后不随 SessionContext 变化；
- snapshot digest 对字段顺序和内容变化稳定敏感。

### 8.2 Context Assembler 测试

- 优先级稳定且与调用顺序无关；
- 预算不足时按完整项移除低优先级内容；
- 选中列表与实际模型输入一致；
- 同一输入、同一 revision、同一 transcript cursor 产生相同 snapshot digest；
- Tool use 与 tool result 不会被拆开；
- 未选中的 SessionContext 不进入 ModelRequest；
- SessionContext 文本不会进入 SystemPrompt 规则区。

### 8.3 恢复测试

- 缺失 document reference 被发现并阻止错误重建；
- SQLite metadata 与 document 身份不一致被拒绝；
- transcript partial tail 修复后 sequence 仍连续；
- execution settlement 后新增 SessionContext 不改变旧 snapshot；
- 服务重启后新 Execution 读取最新 revision，旧 Execution 读取固定 revision。

### 8.4 Runtime 测试

- ModelRequest 是防御性副本；
- Provider 不能修改 AgentLoop message；
- runtime 不读取当前 policy 或后续 SessionContext；
- 超过 input/context window 时不调用 Provider；
- 不向 Provider 传递 credential 或存储 reference。

## 9. 风险与处理

| 风险 | 处理 |
|---|---|
| Context 过大 | 按确定性优先级和完整项预算选择，记录省略项 |
| 新 revision 影响运行中 Execution | Execution 创建时冻结 revision，运行中禁止重新读取 |
| Agent 输出污染共享上下文 | 只有明确的 append command 才能创建共享 entry |
| 文档与关系行不一致 | 读取时校验父 ID、revision、digest 和对象 ID |
| transcript 无限增长 | 使用 sequence cursor、消息预算和明确的历史窗口 |
| 内容被误当成系统指令 | SystemPrompt 只包含系统规则，业务内容进入普通 message |
| 自动摘要导致不可重复 | 第一版不做自动摘要；使用完整项选择和显式摘要 entry |

## 10. 验收条件

- [ ] SessionContext、Agent Transcript、ContentRef 和 Execution Snapshot 的 owner 清晰。
- [ ] 普通 Agent 输出不会自动写入 SessionContext。
- [ ] 每个 Execution 都保存 Session revision、选择项、transcript cursor 和 digest。
- [ ] Execution 恢复不依赖当前 Context 重建旧输入。
- [ ] Context Assembler 选择过程确定、可测试、可审计。
- [ ] SystemPrompt 与业务 Context 分离。
- [ ] Runtime 和 Provider 不直接读取 Context repository 或存储实现。
- [ ] Context 文档通过 `document_ref` 精确读取，不扫描文件系统。
- [ ] Event 和命令幂等记录不进入模型 Context。
- [ ] 通过 `go test ./...`、`go test -race ./...`、`go vet ./...`。

## 11. 建议提交拆分

1. `feat(Context): 增加追加式共享上下文身份和内容引用`
2. `refactor(Context): 增加确定性 Context Assembler`
3. `refactor(Execution): 冻结模型可见 Context 快照`
4. `refactor(Runtime): 按快照构造 ModelRequest`
5. `test(Context): 覆盖预算、幂等、恢复和输入隔离`

