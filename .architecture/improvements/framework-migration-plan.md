# 目标架构迁移方案

## 1. 范围与基线

本方案以提交 `c059b11` 为代码基线，以 `.architecture/architecture/` 和 `.architecture/sandbox*.md` 为目标状态规范。重点是校正已经存在的代码与目标架构之间的边界、ownership 和数据语义差异。未实现能力的独立实施方案见 [`unimplemented-capabilities-plan.md`](unimplemented-capabilities-plan.md)。

迁移完成后，已有 Project、Session、Agent、AgentExecution、SQLite、JSONL、Provider、Wails 和 React 功能将全部落在目标边界内，且新增能力可以直接复用这些边界。

## 2. 已实现代码的目标差距

### 2.1 领域 ownership 不一致

目标关系是：

```text
Project
└── Session
    └── Agent
        └── AgentExecution
```

当前 `AgentGroup` 位于 Session 和 Agent 之间，并承载 `PrimaryAgentID`、`MaxConcurrent`。目标架构没有这一层；Agent 直接属于 Session，Agent 之间的并发限制属于编排策略或 Workflow 的 `ConcurrencyPolicy`。

迁移要求：

- 将 `Agent.SessionID` 作为唯一关系归属，移除生产用例对 `AgentGroup` 的强制依赖。
- 把 Primary、Delegate、Advisor 等角色保留在 Agent Profile/Policy，不用 group 表达角色。
- 将 `MaxConcurrent` 迁移为 Session/Workflow 的调度策略；同一 Agent 的单 execution 限制仍由 core 保证。
- 提供一次性数据迁移：为每个现有 group 成员补齐 Session 归属，保留可查询的迁移映射，完成后删除 group ownership 外键和 UI 投影。
- `CreateSession`、`ProjectSession`、`ProjectAgent` 和前端 agents 面板改为直接按 Session 查询 Agent。

### 2.2 Project、Workspace、Session 事实分散

目标模型要求 Project 保存用户选择的项目路径，Workspace 是可替换的执行目录资源。当前 Project 只有名称和 `DefaultWorkspaceID`，路径实际放在 Workspace；`compose.Application.CreateProject` 还固定把项目目录创建在 DataRoot 下。

迁移要求：

- 为 Project 增加规范化 `Path` 字段，并让 Project repository、SQLite 列和 DTO 以 Project.Path 为项目目录事实源。
- CreateProject 命令接收用户选择的路径；目录创建、访问检查和路径规范化留在 app/compose 适配层，core 只校验稳定引用和状态。
- Workspace 只保存执行目录的 kind、path、revision 和状态；Session 通过 WorkspaceID 引用当前执行资源，不把 Workspace 当作 Project 身份。
- Session 的 Goal 不再作为共享上下文的唯一正文；创建 Session 时追加初始 `SessionContextEntry`，Goal 仅作为查询投影或任务摘要。
- Project 移动、Workspace 移动和 Session 归属变更都通过 ID 和 revision 校验，禁止用路径作为外键。

### 2.3 Agent 持有了本应属于 execution 的输入

当前 Agent 持有 `TaskPacketID`、`ContextManifestID` 和 `GrantID`，execution 只保存这些引用以及简单的 `RuntimeExecutionSnapshot`。目标架构中 Agent 只拥有稳定身份、Profile、安全策略和状态；一次 execution 必须冻结自己的 ContextSelection 和 ExecutionSecuritySnapshot。

迁移要求：

- 将 TaskPacket、ContextManifest、CapabilityGrant 的使用快照移动到 AgentExecution 输入，Agent 只保留当前 Profile 和 AgentSecurityPolicy 引用。
- 为 AgentExecution 增加 `ParentExecutionID`、`ContextRevision`、`ContextSelection`、`ExecutionSecuritySnapshot` 和对应 revision/fingerprint 字段。
- 启动 execution 时在同一事务内读取 SessionContext、解析 policy、生成安全快照，再写入 `starting` execution；运行中不重新读取当前配置。
- `Resume`、queued work、context delivery 和后续 Workflow 启动都创建新的 execution，不复用已有 execution 输入或 runtime 状态。
- 将 `RuntimeExecutionSnapshot` 从“由 UI 传入的 sandbox/approval 字段”收敛为 core 生成的技术快照；命令请求不能扩大 Agent policy。

### 2.4 Agent policy 与 Grant 语义不完整

当前 `agentpolicy.Store` 是全局 `agent-policy.json`，其结果在创建 Session 时直接生成 CapabilityGrant；Agent 没有带 revision 的独立安全策略，execution 也没有完整安全快照。当前 Wails command 还允许前端直接提交 `SandboxMode`、`ApprovalMode` 和 `Revision`。

迁移要求：

- 将全局策略文件定位为系统默认策略源；为每个 Agent 建立带 revision 的 AgentSecurityPolicy 事实。
- 新增 `SecurityResolver`：`SystemSecurityBaseline + AgentSecurityPolicy + ExecutionRestrictions -> ExecutionSecuritySnapshot`，并验证请求只能收紧权限。
- CapabilityGrant 作为 execution 快照中的不可变值对象保存；已完成 execution 永远保留创建时的 policy revision 和路径快照。
- 从 `contracts.SendInputRequest`、`ResumeRequest`、`QueueWorkRequest` 移除 sandbox/approval/revision 的用户可写字段；这些字段改为内部策略查询结果。
- 设置页只编辑 Agent policy 或系统默认策略，不直接编辑某次 execution 的授权。

### 2.5 execution 创建和命令幂等性不完整

当前 `SendInput`/`Resume` 的 execution 幂等性较完整，但 CreateProject、CreateSession、EnsurePrimaryAgent 和部分 delivery/control 流程由服务端临时生成 ID，调用方无法在响应超时后用同一命令身份收敛。ID 和时钟也由 domain 全局函数或 orchestrator fallback 直接生成，不利于确定性测试和恢复。

迁移要求：

- 所有改变持久化状态的命令都携带稳定 `RequestID`；RequestID 与命令参数摘要一起持久化，重复请求返回原结果，参数变化返回 conflict。
- CreateProject、CreateSession、CreateAgent、Context append、policy update、control、delivery 和 Workflow command 统一采用该协议。
- 将 IDGenerator 和 Clock 从 `core/system` 注入 orchestrator、domain factory 和 storage adapter；生产实现使用安全随机 ID，测试使用可控序列。
- 明确 command acknowledgement timeout、execution timeout、stop timeout 的 context 边界；调用方超时不得取消已 durable 的命令。

### 2.6 领域事件没有成为事实写入口

当前领域对象会返回 `DomainEvent`，SQLite 也有 `EventRepository`，但 `AgentOrchestrator` 没有装配或调用 EventRepository；Wails 只发布 transient provider output。目标架构要求 durable command 产生可查询审计事件，事件丢失时 UI/Workflow 仍可通过查询恢复。

迁移要求：

- 将 EventRepository 纳入 orchestrator 组合依赖。
- 每个 command transaction 同时保存领域状态和对应事件，事件 ID 幂等，事件 payload 不含密钥和 provider 原始错误。
- 将 transient output 明确为观察事件，不把它当作 execution 状态来源；settled 事件只能在 JSONL receipt durable 后发布。
- 增加按 Session、Agent、Execution 和 occurred_at 的查询接口，供 app、Workflow 和 recovery 使用。

### 2.7 查询边界绕过 core projection

当前 `compose.Application` 直接通过 store 查询 Project、Workspace、Session，并直接打开 agentlog 读取 Agent 消息；`AgentOrchestrator` 只负责部分 projection。目标架构要求 UI 获得 core 定义的快照，storage 细节不能进入 app service。

迁移要求：

- 在 `core/orchestrate` 建立 Project、Session、Agent、Execution、Context 和 event query service，统一负责归属校验、分页上限和快照拼装。
- compose 只实现这些 service 所需的 repository adapter，不在 `Application` 暴露绕过 core 的 store 读取方法。
- app binding 只依赖窄 query/command interface；frontend API 只处理稳定 DTO，不能根据 SQLite payload 或 JSONL 字段自行推断状态。
- 将 transcript 读取封装为 core/session 的 projection 接口，provider runtime 使用独立的 execution context 读取接口，避免 UI 查询结果影响模型输入。

### 2.8 runtime 与 Provider 实现位置不符合边界

当前 `compose/provider_runner.go` 同时负责读取 SQLite 中的 Agent、Grant、TaskPacket、ContextManifest，构造消息历史，调用 Provider stream，收集输出并拒绝 tool call。目标架构中 runtime 负责 execution loop 和 transcript receipt，Provider adapter 只负责协议，compose 只做装配。

迁移要求：

- 将 execution input materialization、turn loop、deadline、tool dispatch 和 settlement 顺序迁移到 `agentruntime/execution`。
- `internal/core/runtime` 只保留 provider-neutral ModelStream、ToolRunner、ExecutionLifecycle、snapshot 类型。
- Provider adapter 接收不可变 TurnSnapshot，不读取 SQLite、Agent policy 或 Wails context。
- runtime 通过 core/session 的 transcript port 写入消息和 receipt；compose/provider_runner 退化为 ModelStream 和 ToolRunner 的组合适配器。
- tool call 不再在 provider runner 中直接失败，而是进入 ToolBroker 端口；未装配时返回稳定 capability error 并完成 execution settlement。

### 2.9 transcript receipt 与输入投影不完整

当前 JSONL 已具备 header、execution start/settled 和 context artifact receipt，但消息追加通过 compose 的私有 `providerMessageStore` 类型断言完成，runtime 的公共 session port 没有消息读写契约。Provider 还固定读取最近 200 条消息，而不是 execution 的 ContextSelection。

迁移要求：

- 在 `core/session` 定义 runtime 所需的消息追加、消息读取和 receipt 对账接口，禁止依赖 storage 具体类型断言。
- 每个 execution 的 source input、assistant message、tool invocation/result 和 settlement 都以 execution ID 与 request identity 关联。
- 通过 ContextSelection 构造本次 TurnSnapshot；transcript 只提供该 Agent 私有历史，不自动等同于 SessionContext。
- 保留 JSONL 单 writer、partial-tail repair 和 fsync 顺序，并为重复 message、重复 start、重复 settlement 增加 contract test。

### 2.10 恢复扫描和容量边界是固定上限

当前 recovery 使用 `ListAll(..., 1000)`、`ListRecoverable(..., 1000)` 等固定批次，部分 projection 和 control 查询也使用硬编码 100。目标架构要求 recovery 依赖索引事实源，不能因超过固定数量而遗漏未收敛 execution、delivery 或 control。

迁移要求：

- 为 recovery repository 增加基于 cursor/稳定排序键的分页扫描，并记录 continuation 状态。
- recovery 顺序固定为 readiness=false、JSONL repair、receipt 对账、SessionContext revision 校验、execution/control/wait/delivery/process/workflow 收敛、starting 激活、readiness=true。
- 所有 UI 列表仍使用显式页大小，但 recovery 不以 UI 页大小作为完整性边界。
- 为超过单页数量、重复启动和中途重启补充恢复测试。

### 2.11 Provider secrets 泄漏到 binding DTO

当前 `GetModelConfig` 返回 `ProviderConfigOption.APIKey`，前端 settings 会接收完整密钥；这违反目标架构中“密钥不进入 binding DTO、SQLite、JSONL 或日志”的约束。

迁移要求：

- 配置查询只返回 `hasAPIKey` 或脱敏状态，不返回原始 API key。
- API key 使用单独的 set/clear command 写入 secrets 文件；保存模型配置与保存密钥分离。
- registry、日志和错误映射继续保证 provider 请求失败不包含密钥内容。
- 增加 binding contract test，确保 JSON 序列化结果不存在 `apiKey` 明文。

### 2.12 前端状态仍把 technical policy 当作 UI 本地状态

当前 `useProjectWorkspace` 固定维护 `sandboxMode`、`approvalMode` 和 `revision`，发送输入时将其作为 command 参数；workspace 也通过轮询和 transient output 拼接执行状态。目标前端只渲染 core projection，并通过事件触发查询刷新。

迁移要求：

- 删除前端对 execution security snapshot 的写入权；安全状态来自 Agent/Execution projection。
- 将 execution、context、control、delivery 和 event 查询纳入统一 query cache，事件只负责失效/唤醒。
- transient output 按 ExecutionID 管理，settled 后以 durable transcript/history 覆盖；不把轮询结果当作权威状态机。
- 保持 `frontend/src/api/` 为唯一 Wails 出入口，新增功能不直接引用生成 binding。

## 3. 迁移顺序

### 阶段 0：建立可验证的目标边界

- 固定包职责和依赖方向，补齐 `core/system` 的 Clock/IDGenerator 注入点。
- 为 core/domain、core/persistence、core/runtime、core/session、agentruntime、compose 建立 contract test 和最小 fake。
- 让 `go test ./...`、`go vet ./...`、前端 TypeScript 构建继续通过。

验收：目标边界可独立编译；没有新增代码从 core 反向依赖 app、storage、providers、tools、sandbox 或 compose。

### 阶段 1：校正 Project/Workspace/Session/Agent ownership

- 完成 Project.Path、SessionContext 初始入口和 Workspace 引用语义调整。
- 移除 AgentGroup 作为 Agent 所有权层次，迁移并发策略和现有 group 数据。
- 更新 SQLite typed columns、外键、repository、core projection、app DTO 和前端列表。

验收：数据库与领域校验只承认 Project -> Session -> Agent -> Execution 关系；所有现有数据可通过稳定 ID 查询；路径变化不改变身份。

### 阶段 2：校正 execution 输入、policy 和幂等协议

- 扩展 AgentExecution 的 ContextRevision、ContextSelection、ParentExecutionID 和安全快照。
- 引入 per-Agent policy、SecurityResolver 和 immutable execution snapshot。
- 统一所有 command 的 RequestID、参数摘要、ExpectedRevision、Clock 和 IDGenerator。
- 从 binding DTO 和前端 command 中移除 technical policy 写入字段。

验收：每个新 execution 都能独立重建完整输入和授权边界；同一 RequestID 重试不会重复创建或覆盖；配置变化不影响 active execution。

### 阶段 3：校正事件、查询和恢复边界

- 将 EventRepository 接入每个 durable command transaction，提供查询投影。
- 把 compose 的直接 repository/transcript 读取迁入 core query service。
- 将 recovery 固定为索引分页扫描和 receipt 对账，消除固定数量导致的遗漏。
- 将 JSONL 消息/receipt port 公开给 runtime，移除 providerMessageStore 私有断言。

验收：事件可丢失时 UI 和 recovery 仍能由查询重建；跨 SQLite/JSONL 操作可用稳定 ID 对账；超过单页数据仍能完整恢复。

### 阶段 4：校正 runtime、Provider 和前端技术边界

- 将 provider runner 中的 execution loop 和输入组装迁移到 agentruntime execution 区。
- Provider 只实现 ModelStream；工具调用统一进入 ToolBroker port。
- secrets binding 改为脱敏查询和独立 set/clear command。
- 前端改为 projection + event invalidation，移除本地 technical policy 和状态机推断。

验收：runtime 不读 SQLite 或 Provider 配置文件；Provider 不改变产品状态；binding DTO 不含密钥；前端不直接依赖存储格式。

## 4. 数据迁移与回滚

1. 每个 SQLite schema 版本只前进，不修改已发布 migration；新增 typed columns、索引和外键后再进行 payload 回填。
2. 迁移前对完整 DataRoot 做停止状态备份，备份包含 SQLite、JSONL、配置、secrets、attachments 和 notes。
3. AgentGroup 扁平化、Project.Path 回填、execution 输入迁移和 policy 导入都使用稳定 ID；回填结果可重复执行，冲突停止启动。
4. JSONL 不做跨 Agent 合并；只增加 receipt 或消息字段的兼容读取，partial tail repair 后再进行产品状态对账。
5. 任何迁移失败都保持 readiness=false，保留原数据库和 JSONL，不自动删除或覆盖用户数据。

## 5. 完成条件

- [ ] 已实现领域对象的 ownership、字段归属和状态转换与目标文档一致。
- [ ] Project/Workspace/Session/Agent/Execution 的关系、ID、revision 和路径语义已统一。
- [ ] 所有 durable command 具备 RequestID/ExpectedRevision 幂等协议。
- [ ] execution 输入和安全快照在创建时冻结，runtime 不重新解释当前 policy。
- [ ] 事件、查询、JSONL receipt 和 recovery 形成可对账的事实链。
- [ ] Provider/runtime/storage/app/frontend 依赖方向符合目标系统架构。
- [ ] 密钥不进入 binding DTO、日志、SQLite 或 JSONL。
- [ ] 通过 `go vet ./...`、`CGO_ENABLED=1 go test -race ./...`、行长检查和 `npm run build`。

完成本方案后，已实现能力与目标架构一致。未实现能力按 [`unimplemented-capabilities-plan.md`](unimplemented-capabilities-plan.md) 单独实施。
