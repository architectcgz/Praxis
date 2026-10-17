# 后端重复校验 Review

## 结论与范围

存在重复校验，已确认 **5 类问题：3 项中、2 项低**。主要重复发生在 `request → service → core 构造器 → JSONL 写入`，以及仓储读取完成后的 service 层。

审查依据是当前工作树的 [`backend/AGENTS.md`](../../backend/AGENTS.md#L3)，尤其是：

- 输入校验、规范化和默认值补充由输入层一次完成。
- 内部不得再次调用 `Validate`、重复检查同一字段、递归校验对象或仅为校验重新解析数据。
- 跨包、复制和类型转换不构成重新校验的理由。
- 校验方法及逻辑不得放入 `contracts`。

根目录 `AGENTS.md` 仍允许业务层构造器进行只读校验；本次对 backend 中重复检查输入字段的判断采用更具体的 `backend/AGENTS.md`。状态转换约束、事务最终关联与唯一性约束、真实权限检查、持久化恢复校验，需要与重复输入校验分别判断。

基线：`f47976b2cdf728c416900ba94f4a995d827dee62`，分支：`codex/execution-coordinator`。审查开始时根目录和 backend 的 `AGENTS.md` 已有本地修改，本报告按这些规则的当前内容审查。

## 1. 中：JSONL 普通写入重新解析并递归校验已构造成功的对象

**位置与证据**

- [`NewProject` 调用 `project.Validate`](../../backend/internal/core/project/project.go#L49)。
- [`NewWorkspace` 调用 `workspace.Validate`](../../backend/internal/core/workspace/workspace.go#L58)。
- [`CreateProject` 构造成功后直接 Save](../../backend/internal/service/project/service.go#L122)。
- [`save` 将对象编码为 JSON](../../backend/internal/infra/jsonl/codec.go#L188)。
- [`stage` 调用 `describe`](../../backend/internal/infra/jsonl/transaction.go#L170)。
- [`describe` 再解码 Project/Workspace](../../backend/internal/infra/jsonl/validation.go#L62)，并在 [第 214 行](../../backend/internal/infra/jsonl/validation.go#L214) 统一调用 `object.Validate()`。

**问题描述**

正常创建项目时，Project 和 Workspace 在构造器中完成全量校验，返回成功后没有字段修改。保存时却先 `json.Marshal`，再由 `describe` 解码出同形对象并全量 `Validate`，重复检查相同的身份、路径、枚举及时间字段。

可达链路：

```text
service/project.CreateProject
  → NewWorkspace / NewProject
    → 对象.Validate
  → Repository.Save
    → save → JSON 编码 → change → stage
      → describe → JSON 解码 → 同一对象.Validate
```

此外，`describe` 在 `object.Validate()` 成功后，还通过 [反射扫描身份字段](../../backend/internal/infra/jsonl/validation.go#L218)。例如 [`ModelUsageRecord.Validate`](../../backend/internal/core/model/model_usage.go#L53) 已检查 SessionID、AgentID、TaskID、TurnID 的 canonical 形式，这些字段紧接着又被反射循环检查。

**影响范围与风险**

这是普通实体写入共用的路径，重复解码与对象遍历会随写入发生，且把输入规则分散到了构造器和存储层。这里只确认存在额外处理，没有对性能损耗进行测量。

**修正建议**

正常写入使用已经处理完成的对象构建索引，删除对其重复执行的输入字段与全量对象校验；将日志恢复及磁盘读取的严格解码、字段校验留在对应输入边界。保留存储层对唯一性、归属、不可变字段和事务最终关联的检查。不能直接删除整个 `describe`：[`replay`](../../backend/internal/infra/jsonl/replay.go#L59) 和 [`readRef`](../../backend/internal/infra/jsonl/codec.go#L91) 也依赖它处理磁盘数据。

无需增加“已校验”标志、校验模式或同形包装对象。

**置信度：100/100。**

## 2. 中：core 构造器重新检查已经通过入口的同一输入字段

**位置与证据**

- [`ValidateCreateProject`](../../backend/wails/validation/validation.go#L112) 已检查 ProjectID 和 WorkspaceID 必填且已规范化。
- [`NewCreateProject`](../../backend/internal/request/commands.go#L26) 已 Trim 名称、Clean 路径，并检查名称非空、控制字符及绝对路径。
- [`service.Services.CreateProject`](../../backend/internal/service/desktop.go#L299) 只逐字段传递这些值。
- [`NewProject → Validate`](../../backend/internal/core/project/project.go#L49) 再检查相同 ID、名称非空与控制字符、路径绝对且规范化。
- [`NewWorkspace → Validate`](../../backend/internal/core/workspace/workspace.go#L58) 再检查相同 ID 和路径。

**问题描述**

从 Wails/request 到 service 再到 core 构造器，没有新的外部输入，也没有修改这些字段。构造器仍重查已成立的名称、ID 和路径条件。路径在 request 中已执行 `filepath.Clean`，内部又通过 [`IsAbsoluteNormalized`](../../backend/internal/utils/pathutil/path.go#L16) 执行 `filepath.Clean(path) == path`。

会话创建也有相同模式：[`ValidateCreateSession`](../../backend/wails/validation/validation.go#L123) 已检查身份字段，随后 [`NewSession`](../../backend/internal/core/session/session.go#L34) 和 [`NewAgent`](../../backend/internal/core/agent/agent.go#L59) 再检查相同 ID 的非空条件。

**影响范围与风险**

入口规则与领域构造器形成多个 owner，新增入口时难以明确哪些约束已经完成、哪些仍依赖深层补查。这直接违反 backend 对跨函数、跨包和复制后重复校验的限制。

**修正建议**

输入字段规则在入口一次完成，内部构造器直接使用 canonical 字段。新生成的状态、时间等业务约束须按其真实职责处理，不能把所有检查视为重复后整段删除。

特别是 `NewCreateProject` 目前只检查路径绝对性，路径的 `NUL/CR/LF` 限制仍在 `IsAbsoluteNormalized` 中。收敛时应先把这些必要限制前移到 request，再删除内部对相同输入字段的复查，避免产生校验缺口。磁盘恢复需要独立保留严格校验。

**置信度：100/100。**

## 3. 中：contracts 中仍包含明确禁止的校验与规范化逻辑

**位置与证据**

- [`ModelThinking.Validate`](../../backend/internal/contracts/model_thinking.go#L14) 检查模式与 token 预算。
- [`ApprovalRecord.Validate`](../../backend/internal/contracts/approval.go#L25) 检查来源、时间与摘要。
- [`NewYoloApproval`](../../backend/internal/contracts/approval.go#L39) 检查摘要非空。
- [`EmptyID`](../../backend/internal/contracts/errors.go#L134) 使用 `TrimSpace` 判断 ID。
- [`ToolCall.Snapshot`](../../backend/internal/contracts/tool_protocol.go#L17) 在复制数据时同时 Trim 调用 ID。

**问题描述**

这些不是仅描述数据结构的契约，而是输入校验或规范化实现，直接违反 [`backend/AGENTS.md:4`](../../backend/AGENTS.md#L4) 的放置要求。

已核实的调用包括 Project/Workspace/Session/Agent 的 `Validate` 调用 `contracts.EmptyID`；已批准工具记录的 [`Execution.Validate`](../../backend/internal/core/tool_execution/execution.go#L207) 还会进入 `ApprovalRecord.Validate`。因此 contracts 中的逻辑会随上面确认的递归校验路径一起反复执行。

**影响范围与风险**

输入规则被共享契约隐藏，调用层无法明确控制所检查的内容。`Snapshot` 的命名只体现复制，却还承担输入清洗，会模糊规范化的唯一 owner。

**修正建议**

contracts 保留共享值类型；将实际输入检查和规范化移到接收该输入的入口。复制函数只做复制。不要将整个 Validate 原样搬到另一个通用包继续供内部递归调用，否则重复校验仍然存在。

本项同时包含“校验放置错误”和“复制时清洗”两个与本次规则直接相关的问题；未据此推断模型或工具全链路的重复次数。

**置信度：100/100。**

## 4. 低：service 对仓储刚返回的对象再次全量 Validate

**位置与证据**

- [`workspaceForSession`](../../backend/internal/service/session/workspace_access.go#L21) 在 `workspaces.Get` 成功后立即于 [第 25 行](../../backend/internal/service/session/workspace_access.go#L25) 调用 `workspace.Validate()`。
- [`GetSessionUsageSummary`](../../backend/internal/service/session/usage.go#L32) 读取用量后，于 [第 47 行](../../backend/internal/service/session/usage.go#L47) 对每条记录调用 `record.Validate()`。
- [`compose`](../../backend/internal/compose/application.go#L225) 明确将 `UsageRecords` 绑定到 `jsonl.ModelUsageStore.ListSession`。
- [`ListSession`](../../backend/internal/infra/jsonl/model_usage.go#L39) 走 `listIndexed`；普通磁盘读取经过 [`readRef → describe`](../../backend/internal/infra/jsonl/codec.go#L91)，返回前已经调用同一对象的 `Validate`。

**问题描述**

文件预览读取 Workspace、用量汇总读取 ModelUsageRecord 时，存储读取边界已经完成严格校验。service 未修改返回对象，仍再次检查其结构、字段和 canonical 形式。用量记录的第二次校验还会重新遍历 ID，并再次执行 token 合法性判断。

**影响范围与风险**

每次文件预览和用量汇总都会触发额外校验；用量汇总的重复工作随记录数增长。service 同时承担了存储恢复规则，职责重复。

**修正建议**

删除这两个 service 中对未变更仓储对象的全量 `Validate`。保留工作区当前可用状态、会话归属、文件访问授权，以及汇总计算的溢出检查；这些是当前用例的业务约束。磁盘数据非法时由存储读取入口返回错误。

**置信度：100/100。**

## 5. 低：会话标题通过 request 校验后，Repository 又检查一次

**位置与证据**

- [`NewRenameSession`](../../backend/internal/request/commands.go#L112) 对标题 Trim 后拒绝空值。
- [`SessionBindings.RenameSession`](../../backend/wails/bindings/bindings_session.go#L142) 将成功构造的 request 交给 service。
- [`service/session.RenameSession`](../../backend/internal/service/session/service.go#L449) 原样传递 `canonical.Title`。
- [`SessionRepository.Rename`](../../backend/internal/infra/jsonl/repositories.go#L153) 再次执行 `title == "" || title != strings.TrimSpace(title)`。

**问题描述**

Repository 重查标题非空和规范化条件；从 request 返回到此处没有新输入或标题变化。这是同一字符串在同一调用链中的重复校验。

已检索后端全部 `Rename` 调用，当前这条会话标题写入路径只有上述 service 调用者。

**影响范围与风险**

影响会话重命名。实际计算成本很小，但标题输入规则存在两个 owner，直接违反 backend 的校验归属约定。

**修正建议**

保留 `request.NewRenameSession` 的输入处理，删除 Repository 的标题输入复查；Repository 只读取目标、修改标题和更新时间、保存事务结果。读取失败与事务约束继续正常返回。

**置信度：100/100。**

## 收敛边界与验证方式

建议先处理会话标题和 service 对仓储结果的重复 Validate，再收敛普通 JSONL 写入与 core 构造器的输入规则，最后整理 contracts 中的校验 owner。调整时须先补齐入口承担的规则，再删除内部复查。

以下检查有独立职责，本报告不建议随重复输入校验一起删除：

- 磁盘恢复、配置文件读取、Provider 响应和工具参数入口的严格解析。
- 事务最终的唯一性、引用归属、不可变字段和幂等冲突判断。
- 状态转换的前后关系、权限变化后的重新授权。
- 文件访问范围、`os.OpenRoot` 约束、实际读取大小和 UTF-8 检查。
- 用量汇总自身的算术溢出检查。

验证采用源码静态检索、全部相关调用者检索及真实调用链阅读。未修改业务代码，未启动服务，未新增或运行自动化测试，未执行构建。

## 审查限制与执行记录

已对 backend 的 `Validate`、`TrimSpace`、路径规范化及相关检查做静态检索，并完成上述请求、实体构造与存储链路的复核。本报告列出的均有前后两处源码证据；模型配置与工具执行链路未完成独立审查，不能据此认定这些区域不存在其他重复校验。

采用官方 [`code-review@claude-plugins-official` 审查命令](C:/Users/Administrator/.claude/plugins/marketplaces/claude-plugins-official/plugins/code-review/commands/code-review.md) 的规则对照、证据复核与置信度筛选原则；本次是本地现有代码审查，不涉及 PR 发布。

并行审查工作流 `bb7eb1e8-75bd-4535-bbf3-dc869c4a13df` 的两个子任务均在启动前失败，未产生独立审查结果：

```text
Background children require the host npm package (@earendil-works/pi-coding-agent)
with its dependencies; C:\Users\Administrator\AppData\Roaming\npm\node_modules\
@earendil-works\pi-coding-agent does not provide @earendil-works/pi-agent-core/node.
```

子任务启动 ID 分别为 `c42ba0d1-7ec6-4797-be92-a29848dd3d6e` 和 `fc648a65-68df-48da-91b9-c450e6ef81ff`。工作流已结束，未创建其他 worktree，未修改业务代码。上述结论来自主会话完成的直接源码审查。
