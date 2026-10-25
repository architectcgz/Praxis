# Praxis Main 历史持久化与 Execution 运行时重构计划

## 一、背景与目标

会话历史就是 Main Agent 的历史，包含 Main 多次 Execution 产生的消息。Subagent 的内部对话不属于会话历史，只通过返回结果向调用方提供信息。

运行时历史以本次 Execution 为生命周期边界，由 Execution 持有，结束后释放。Main 在创建或恢复时加载持久化历史；Subagent 从明确传入的委派输入开始，仅使用内存历史，不加载或恢复此前的内部执行过程。

本次目标：

- JSONL 仅持久化 Main Agent 的消息历史，不保存 Subagent 内部历史。
- Subagent 的 Execution 状态、进度、请求消耗、计时、模型消息、Summary 和内部工具回执均保持内存态，不为恢复或展示写入 JSONL。
- Main 保留其历史及必要的执行恢复事实；Session、Project、Workspace 等独立业务数据的持久化不因消息范围调整而取消。
- Coordinator 独占管理执行权、取消、结算和资源释放；运行期间直接访问本次 Execution 数据。
- 工具审批与 Agent 类型无关：Main 和 Subagent 使用相同安全规则，审批状态属于当前活跃 ToolCall，仅保存在内存，不做重启恢复。
- 移除 Store、全局业务 State 和常驻查询投影，不设置换名的集中状态容器。
- Repository 接口位于 `internal/repository`，具体实现仍位于 `internal/infra/storage`，直接读写 `infra/jsonl`。
- 移除 `AssistantCommitter`，不新增只转发 Repository 调用的 Recorder、Writer 或内存版 Repository。

“仅持久化 Main 历史”是 Agent 历史的保留边界，不要求取消会话配置等独立持久化数据。Main 的恢复事实限于既有恢复语义所需的数据，不以此为由保留 Subagent 全过程或审批日志。

## 二、数据保留边界

| 数据 | Main Agent | Subagent |
| --- | --- | --- |
| 用户输入 / 委派输入 | Main 输入纳入持久化历史 | 由调用方显式传入，仅内存持有 |
| Assistant、思考块、内部 ToolCall | 纳入 Main 历史 | 仅内存持有 |
| ToolResult | 纳入 Main 历史 | 仅内存持有 |
| Summary 与 Replaces | 纳入 Main 历史 | 仅作用于本次内存历史 |
| Execution 进度和请求消耗 | 运行时持有，必要恢复事实按既有规则保存 | 仅内存，不写恢复记录 |
| 子执行计时和过程用量 | 不因委派而展开保存子执行过程 | 仅内存，结束后释放 |
| 工具审批请求、风险审核及人工决定 | 当前 ToolCall 的内存状态 | 当前 ToolCall 的内存状态 |
| 最终返回结果 | 按 Main 自己的消息或工具回执保存 | 返回调用方，不单独保存子执行历史 |
| 程序重启后恢复 | 加载 Main 历史和必要执行事实 | 不恢复历史、执行或审批等待 |

如果 Subagent 直接由 Main 调用，Main 保存自己的委派调用及对应结果；多层委派逐层返回结果，只有最终被 Main 接纳的内容进入会话历史。不展开复制子 Agent 的内部消息、工具结果或计时明细。

委派标识用于关联 Main 的调用和返回，不要求磁盘中存在一份 Subagent Execution 记录。持久化关系校验不能因此强制为子执行创建日志。

## 三、目标架构与职责

```text
Coordinator
    │
    ├── Main Execution
    │       ├── Repository 加载 Main 历史及必要恢复事实
    │       ├── Runtime 持有历史、ModelContext、进度和工具状态
    │       └── 新 Entry → Infra Repository → JSONL
    │
    ├── Subagent Execution
    │       ├── 委派输入 → Runtime 内存历史
    │       ├── 模型、工具和 Summary 直接推进内存历史
    │       └── 返回结果 → 调用方，不写子执行日志
    │
    └── 等待所属执行路径退出 → 完成结果交接和结算 → 释放运行时对象
```

| 组件 | 负责 | 不负责 |
| --- | --- | --- |
| Coordinator | 身份准入、独占运行权、Runtime 创建与释放、取消及结算编排 | 保存另一套历史或运行子执行恢复流程 |
| Execution Runtime | 本次历史、上下文、输入、进度及工具状态 | 长期缓存所有 Agent 历史、通过 Repository 回查已有数据 |
| Loop | 模型迭代、消息构造、压缩和历史推进 | 决定执行终态、释放运行权 |
| ExecutionTools | 授权、审核、审批等待、工具执行及结果返回 | 按 Agent 类型绕过审批、从 JSONL 回查活跃调用 |
| Execution Service | Main 加载与必要持久化、执行准备和结算业务 | 全局历史缓存、Subagent 过程持久化、Assistant 写入代理 |
| Repository 接口 | 持久化数据的保存、加载和恢复契约 | 依赖 Infra 或充当 Subagent 内存历史接口 |
| `infra/storage` | 实现 Repository，直接读写 JSONL，处理编解码、幂等和持久化约束 | Store 中转、运行时历史或审批状态管理 |
| `infra/jsonl` | 文件访问、读写同步、序号、可靠追加和断尾处理 | Entry、Execution、审批等业务状态投影 |

持久化依赖为 `业务调用方 → repository 接口 → infra/storage 具体实现 → infra/jsonl → 文件`。`infra/storage` 保留为实现目录，不保留 Store。

## 四、Execution 运行时数据

### 1. 所有权与释放

在 `core/agent/execution` 中表达运行时数据，使用具体类型，例如 `Runtime`。与持久化的 Execution 记录分离，不要求每个 Runtime 都有对应磁盘记录。

- Coordinator 创建并持有 Runtime，Loop、工具和结果交接路径只借用所需的数据或引用。
- 历史及 Entry ID 索引归本次执行所有，将 `core/agent/history.go` 的历史能力归入执行运行时。
- 共享 Runner、Executor、Service、Repository 和长期 Agent 对象不保存本次历史引用。
- 不新增全局 Execution → Runtime 查询注册表；控制定位复用 Coordinator 及活跃工具已有的生命周期索引，不作为历史取值中转。
- 正常完成、失败、中断和暂停均在所属路径退出、必要结算与结果交接完成后释放。
- Main 的结算失败保留结果和槽位，只重试必要持久化；Subagent 的结算是内存结果确认和交接，不调用 Execution Repository。
- 准备失败也释放已经建立的历史和资源。应用关闭先取消并等待执行及工具退出，再关闭共享 JSONL；超时不强制释放仍在使用的资源。

前端可以在执行活跃时取得 Subagent 的独立运行快照，结束后不承诺仍可查询其历史，不为历史展示延长 Runtime 生命周期。若结果交接需要暂存，只保存尚未交付的必要结果，消费后释放，不保留内部过程。

### 2. 原始历史与模型上下文

- 原始历史记录本次执行可用的消息，Main 包含加载的跨执行记录，Subagent 只包含显式输入和本次内部记录。
- `ModelContext` 是模型投影，可过滤、重排和压缩，不能代替原始历史。
- Summary 追加原始历史，`Replaces` 只改变有效上下文，不覆盖来源记录。
- 原始正文不可变，模型投影和 Hook 修改数据时复制必要部分，避免切片或指针共享造成历史修改。
- Main 使用保存结果中的 Sequence、CreatedAt；Subagent 在 Runtime 内分配有序序号和时间，不占用 Session JSONL 序号。
- Main / Subagent 的 Entry ID 都保持确定性规则；内存追加同样按 ID 去重并检测内容冲突。

### 3. 创建与恢复

Main 执行准备：

1. 取得 Main 的独占执行权，完成 Session 可写性准入。
2. 加载必要执行事实，在旧执行者已停止的前提下处理未完成工具调用。
3. 一次一致性读取取得 Main 已准入历史及边界，创建 Runtime，恢复输入、进度、调用配对和最终输出证据。
4. 已有可靠最终 Assistant 时直接结算；否则在本地历史上应用摘要替代、消息筛选、回执关联和 Context Hook，构建模型上下文。

加载必须包含当前 InputEntryID；尚未准入的排队输入不进入当前上下文。运行中新入队输入留给后续 Execution，不持续重载历史。

Subagent 执行准备：

1. 根据可信的 Agent 身份和委派关系创建 Runtime，不创建持久化 Execution 或输入记录。
2. 将显式委派输入作为初始历史，初始化进度和模型上下文。
3. 不加载 Main 的完整历史、其他 Agent 的历史或上次 Subagent 内部过程。
4. 结束后返回结果并释放；若需要再次调用，视为新执行并重新提供输入。

Main / Subagent 身份来自业务装配及 Session 的 MainAgentID，不接受模型输出或任意持久化开关改变身份。持久化分支在执行准备和写入调用方明确选择，不通过空实现 Repository 假装落盘成功。

## 五、运行期间的历史推进

### 1. Assistant 与工具结果

Main：

```text
构造 Entry → Repository.Append → 可靠保存确认
    → 使用返回 Entry 合入原始历史
    → 更新 ModelContext、完成证据和统计
    → 发出对应通知
```

Subagent：

```text
构造 Entry → Runtime 校验并按 ID 追加
    → 更新 ModelContext、完成证据和统计
    → 发出运行时通知
```

- 两条路径复用消息构造和上下文处理，不复制整套模型循环；Main 的持久化分支直接使用 Repository，不增加写入代理。
- 主动区分内存接纳与可靠保存，Subagent 通知不伪装成持久化成功；新增记录才触发一次对应通知。
- 工具返回结果由一个明确位置合入历史，不能工具层和 Loop 各累计一次。
- Main 写入失败分类为 FailureStorage，不推进依赖该记录的模型或工具；Subagent 的内存冲突或无效记录不得伪装成存储错误。
- 模型失败或取消时保留已有部分文本 / 思考输出：Main 在返回前使用 `context.WithoutCancel(ctx)` 完成必要保存；Subagent 只在当前 Runtime 中记录，不因失败转为落盘。
- 最终 Assistant 接纳后结束 Loop，不再生成后续输出；双方均不通过 Repository 回读当前已持有的消息或回执。

### 2. Summary

- 构造和压缩时机位于 Loop，通用 `core/context/compress` 不知道 Execution、Agent 身份或 Repository。
- Role 为 User，AuthorKind 为 System，AuthorID、ExecutionID、RequestID 来自当前执行。
- ID 保持 `summary:<ExecutionID>:<摘要哈希>`；Content 与 Replaces 的 JSON 编码、字段顺序及 SHA-256 前 12 字节规则不变。
- Main 保存 Summary 后合入原始历史和压缩结果；Subagent 仅在内存中合入，不调用 Repository。
- 所需 Summary 全部接纳成功后再切换模型上下文。Main 多条摘要部分保存后发生失败时保留已提交事实并终止失败路径，恢复时由持久化历史重建。

### 3. 请求进度与计时

- 主模型、摘要和风险审核共用本次执行的请求序号及消耗状态，由一个运行时拥有者更新。
- Main 保持必要的请求前持久化预留语义，预算在恢复后不回退；持久化失败不发起外部模型请求。
- Subagent 在内存中预留和累计，不调用进度、模型用量或计时 Repository，也不由共享 Recorder 间接落盘。
- Main 的用量记录不展开复制子执行明细；当前计划不新增长期子执行统计或审计需求。
- `ExecutionProgress` 及计时装配需绑定本次运行时身份，不通过共享 Service 每次读取 Execution Repository 推进状态。

### 4. 工具审批与控制

审批属于具体 ToolCall，与 Main / Subagent 分类无关：

- 双方使用相同的工具权限、风险审核和人工审批流程，内存执行不意味着跳过安全检查。
- 审批请求、审核结果和用户决定绑定当前活跃的 Execution / ToolCall，保持内存态，不写审批恢复日志。
- 前端审批列表读取活跃工具的审批快照，不从 JSONL 扫描请求、决定或工具状态。
- 外部控制入口只提交决定或取消信号并唤醒当前执行者；历史、工具完成回执和模型上下文由执行路径统一推进。
- 尚未启动工具收到停止信号时，由当前工具路径生成一次中断回执，不让控制协程与 Loop 并发追加历史。
- 工具调用结束、执行终止或进程退出后，审批状态失效；迟到决定或身份不匹配的决定不能作用到新调用。
- 重启不恢复待审批请求、不沿用旧批准自动启动工具，新调用重新检查并审批。
- Main 恢复时对没有可靠完成结果的调用补中断结果，不恢复审批或自动重跑可能已经产生副作用的工具。Subagent 不恢复，进程退出后的内部调用不重放。

## 六、委派结果与中断恢复

- 子执行将必要结果返回调用方，由调用方推进自己的历史；只有 Main 的委派调用与结果进入会话 JSONL。
- 多个子执行可并发返回，但不能直接修改 Main 的 ModelContext；Main 执行路径按明确的调用配对规则接纳结果。
- Main 的委派调用已保存、结果尚未保存时发生重启，按未完成调用处理，记录中断结果；不尝试恢复或自动重新委派原 Subagent。
- 子执行结果已返回内存但 Main 尚未可靠保存时崩溃，不视为结果已提交；以 Main 实际落盘记录为准。
- 已保存的 Main 委派结果无需子执行记录才能恢复，也不据此恢复子 Agent 实例。
- Subagent 状态、进度、计时和历史没有磁盘兜底，进程退出即丢失，这是明确的生命周期边界。
- Main 的 Session 命名、结算使用本次已持有的原始输入和结果，不为取得正文回查 Repository。
- 不承诺崩溃前模型或工具全部可重建为完成结果；没有可靠结果时不推断副作用尚未发生。

## 七、Repository 直接读写 JSONL

### 1. 接口与资源

- `internal/repository` 定义持久化契约，`internal/infra/storage` 具体实现直接持有共享 `*jsonl.Log` 及必要固定配置。
- 不存在 Store、全局业务 State、常驻历史或执行投影，不引入新的 Manager / Backend 代替它们。
- 组合根创建共享 JSONL 并注入 Repository，应用在所有执行退出后统一关闭；各 Repository 只借用资源。
- Main 的消息写入与历史加载使用 Entries；Subagent 内存历史不实现 Entries，也不经过 Repository 的成功空转路径。
- Main 历史写入边界检查 AgentID 与 Session.MainAgentID 一致，误将 Subagent Entry 交给持久化实现时明确拒绝，不静默丢弃或写入。
- Execution、进度、用量及计时等持久化入口同样遵守 Main 范围，不允许通过旁路保存子执行过程。

### 2. 读取与恢复

- Get / List 从 JSONL 扫描、过滤和归并，返回 Main 历史或其他独立业务的持久化结果。
- 更新型记录按事件顺序取最新有效事实，Main Entry 保持追加和不可变语义，Session 删除遵循独立业务规则。
- 记录与序号边界来自同次一致性读取，临时映射、配对数据和查询投影在调用或恢复完成后释放。
- 摘要替代在 Runtime 本地完成，移除 `EntryQuery.ForContext`；Main 的未准入输入过滤及恢复读取语义保留。
- Main 历史可在没有活跃执行时查询；Subagent 历史只通过活跃 Runtime 快照查看，执行结束或重启后不提供持久化历史。
- 启动恢复只加载 Main 执行及必要业务事实，不枚举并重建 Subagent，不恢复审批请求。
- 不预设全局业务索引、定点读取框架或缓存，用实际读写需求决定后续是否优化。

### 3. 文件同步与可靠提交

- JSONL 仅持有文件句柄、路径、游标、锁和故障标记等设施数据，不理解 Main / Subagent 或审批业务。
- 不同 Repository 共享同一 JSONL 资源的读写互斥和序号分配，不能分别加锁保护同一文件。
- Repository 的既有记录读取、幂等和引用校验、序号分配及追加处于同次受保护操作中；跨文件约束覆盖相关读取和提交，首版可用共享 Log 统一锁串行化。
- 持锁内部入口不得再次获取同一把锁；不在文件锁内执行模型、工具或 Hook。
- List、并发追加、删除和关闭保持一致的文件可见边界。
- Main 相同 Entry ID 且内容一致返回已有 Entry 和 `inserted=false`，保留 Sequence / CreatedAt 归一化语义；内容冲突报错，不仅凭哈希判定相同。
- 可靠追加确认前不推进 Main Runtime 或发布保存成功事实；不保留内存预发布、pending / flush 业务队列。
- 追加失败或成功性不确定时返回存储错误，按文件故障规则停止后续依赖操作；只有安全可重复的确定性写入可重试，否则重开日志确认，不重跑模型或工具。
- 保留断尾修复、完整坏行报错和文件故障封锁。读取失败不能当作记录不存在。

### 4. 约束与删除范围

- Main 持久化记录保留字段合法性、作者、Session / Agent / Request / Execution 归属、摘要来源和工具回执配对校验。
- Main 的委派结果属于 Main ToolCall 的回执，子执行身份只作关联信息，不形成必须存在的持久化子 Execution 外键。
- Assistant 写入不重复判断 Execution.Active() 或 FinalAssistantID；执行权和终止顺序由 Coordinator 保证。
- 关系校验读取本次必要的持久化事实，不建立全局实体 State，不为校验创建 Runtime。
- 删除 Store 的创建、关闭、view、全局 state / stateEntry、histories、applyEvent 和 State 查询帮助函数。
- 计时、用量、审批和恢复入口不保留隐式 Store，也不把 State 换名搬入 JSONL。
- 无职责剩余的审批持久化 Repository、工具事件恢复代码和对应装配应删除；仍有职责的结构或授权规则不因删除审批日志被一起移除。
- JSONL 记录格式及 Main Entry 的确定性 ID 不变。本计划不新增历史日志迁移或兼容分支，也不主动清理已有用户日志；历史数据处理另行授权。

## 八、生命周期与并发边界

```text
占用执行槽位
    → 创建 Runtime：Main 加载，Subagent 接收显式输入
    → Loop 与所属工具推进本次历史
    → 所有运行及提交路径退出
    → Main 必要持久化结算 / Subagent 内存结果交接
    → 清除运行时引用并释放槽位
```

- 同一 Agent 只有一个执行者；旧 Loop、工具及必要结算未结束前，不启动该 Agent 的新执行。
- 不同 Agent 的 Runtime 隔离，Subagent 内部历史推进不争用 Session JSONL；多个 Main、输入提交及其他持久化业务仍需文件同步。
- 暂停、停止只发送信号，不提前释放历史或执行权。Subagent 释放后的再次调用为新执行，不恢复旧审批和过程。
- 历史、上下文和回执由执行路径单一推进，外部审批和取消不直接追加 ContextEntry。
- 不允许脱离本次生命周期的后台消息提交，不让闭包、共享 Hook 或 Executor 遗留 Runtime 引用。
- Session 准入在创建或准备边界完成，正常消息提交不经过 Service 重复准入；删除、归档与活动执行协调，不能先移除仍在使用的日志。

## 九、文件级实施清单

以下路径相对于 `backend/internal/`，实际拆分服从职责，不为清单额外创建代理层。

| 文件或目录 | 修改内容 |
| --- | --- |
| `core/agent/execution/`、`core/agent/history.go` | 明确 Runtime 与历史所有权，区分 Main 加载和 Subagent 内存创建，释放时不依赖磁盘子记录 |
| `core/loop/types.go`、`run.go` | 删除 AssistantCommitter，可信身份决定 Main 保存或 Subagent 内存接纳，复用模型循环和消息构造 |
| `core/loop/compression.go`、`tools.go` | Summary、工具回执统一合入本次历史，只有 Main 保存，区分内存接纳与持久化通知 |
| `service/agent/execution/context.go`、`context_content.go`、`prepare.go` | Main 加载已准入历史；Subagent 使用显式输入，不创建持久化输入或执行 |
| `service/agent/execution/assistant.go` | 删除，不新增写入代理 |
| `service/agent/execution/progress.go`、`timing.go`、`settle.go` | 子执行进度与计时仅内存，Main 必要恢复事实保存；使用本次输入和结果结算 |
| `service/coordinator/` | 管理两类 Runtime；只恢复 Main；取消、结果交接和槽位释放保持单一生命周期 |
| `core/toolexecution/`、`service/toolexecution/` | 所有 Agent 共享安全流程；审批及控制定位改用活跃内存调用，控制路径不直接追加回执；只恢复 Main 未完成调用并补中断结果 |
| `core/agent/execution/timing/`、模型用量相关写入 | 检查共享 Recorder / Hook 旁路，禁止子执行计时、审核明细或用量自动落盘 |
| `repository/entries.go` 及执行、工具相关接口 | Main 历史持久化契约，移除 ForContext；删除无剩余职责的审批恢复接口，不提供 Subagent 内存 Repository |
| `infra/storage/store.go`、`state.go` | 删除 Store 和集中业务 State |
| `infra/storage/` 其他文件 | Repository 留在 Infra，直接读写 JSONL；保留 Main 数据约束，删除子过程、审批持久化及常驻投影逻辑 |
| `infra/jsonl/` | 共享文件同步、可靠追加与一致性读取，无业务 State 或待写业务队列 |
| `service/session/entries.go`、`service/agent/entries.go` | 会话查询返回 Main 持久化历史；Subagent 仅提供活跃内存快照，结束后不兜底写日志 |
| 委派调用和结果交接路径 | 子结果逐层返回，Main 单独保存自己的调用与回执，不保存子内部历史或要求磁盘子外键 |
| `bootstrap/` 及应用资源管理 | 移除 Store、子执行恢复与审批日志装配，按执行身份绑定持久化和计时，统一关闭 JSONL |

本次仅更新本计划，不修改代码、其他架构文档或任何 AGENTS.md。实现完成后再同步 `.architecture/architecture.md` 中的历史归属、恢复和持久化范围。

## 十、实施顺序与范围

1. 明确 Main / Subagent 身份、Runtime 所有权和数据保留边界。
2. 贯通 Main 加载、Subagent 输入初始化及结束释放；恢复入口只处理 Main。
3. 调整 Loop、工具、Summary、请求进度、计时和结果交接，禁止子过程的直接或间接持久化。
4. 将审批和人工决定改为活跃 ToolCall 内存态，控制入口只发信号或决定，移除审批恢复。
5. 移除 AssistantCommitter、Store 和全局 State，全部 Infra Repository 直接读写共享 JSONL。
6. 核查 Main 未完成委派、工具中断、写入失败、暂停、停止和应用关闭分支。
7. 完成构建、静态检查和人工回归后同步架构文档。

不增加全局业务缓存、通用事件总线、替代写入服务或存储后端。不自动清理历史日志，不为保持历史查询体验而继续保存 Subagent 全过程。

## 十一、风险与回滚

| 风险 | 处理边界 |
| --- | --- |
| 子历史不写，但进度、计时或审核仍被共享组件落盘 | 逐条核查模型、工具、Recorder、Hook 和结算写入链路，子执行全过程保持内存 |
| Main 的委派结果引用不存在的磁盘子执行 | 将结果归属于 Main ToolCall，子身份只作关联，不建立强制子执行外键 |
| 子执行完成但 Main 尚未保存结果时崩溃 | 以 Main 已保存事实为准补中断，不自动重跑子任务或工具 |
| 审批入口与执行路径竞争生成回执 | 决定和取消只更新活跃调用状态并唤醒，回执由当前工具执行路径统一产生 |
| Subagent 历史不能在结束后查询 | 明确仅活跃期间可查看；展示需求不改变内存生命周期 |
| 删除 State 后持久化幂等、引用或并发约束缺失 | 在共享 JSONL 的同次受保护操作中读取并检查持久化事实 |
| 执行释放后正文仍被共享对象引用 | 核查闭包、Hook、Recorder、Executor 和查询快照的所有权及释放 |
| 扫描、冷读取和文件锁带来延迟 | 观察日志规模、扫描及锁等待，不预设全局业务缓存 |

记录格式保持不变。回滚先停止全部活动执行并关闭共享 JSONL，再整体回退代码与装配；不得让不同实现同时写同一文件。Subagent 和审批内存状态无法在回滚或重启后找回，不承诺补建这些过程记录。验证与回滚不使用真实用户数据。

## 十二、验证与验收

### 静态检查

- 在 `backend/` 执行 `go build ./...`、`go vet ./...`、`go list ./...`。
- 不新增、保留或运行自动化测试，不自动启动服务。人工验证使用已有服务或独立临时数据；必须新启动服务时先取得用户授权。
- 无 Store、全局业务 State、常驻执行投影、AssistantCommitter 或替代写入代理；Repository 实现仍在 Infra 并直接依赖 JSONL。
- 子执行输入、模型消息、Summary、工具回执、进度、计时和用量无落盘路径；Main 委派结果不复制子内部过程。
- 无审批日志写入或恢复路径，审批列表与决定只操作活跃 ToolCall。
- 正常运行不为读取已有输入、消息或回执查询 Repository；共享组件不长期持有 Runtime。

### 人工回归

1. Main 后续执行可加载跨执行历史，Subagent 只得到显式委派输入，不读取或污染 Main 上下文。
2. Subagent 多轮模型、工具、压缩和审核不产生子执行 JSONL 记录；进度与计时在活跃时可见，结束后释放。
3. Main 保存自己的 Assistant、Summary、ToolCall 和 ToolResult；委派完成只增加 Main 必要结果，不展开子历史。
4. Main 同 ID 写入保持幂等与内容冲突语义；Subagent 内存追加也不重复累计历史或统计。
5. 两类 Agent 使用相同授权与审批规则，批准、拒绝、停止、迟到决定均正确绑定当前调用，控制路径不并发修改历史。
6. 重启不恢复 Subagent 或审批等待，不沿用旧批准；Main 未完成工具或委派补中断结果，不重跑副作用。
7. Main 最终输出已可靠保存时恢复直接结算；结果只在内存、尚未保存时不视为已完成。
8. Main 请求预算恢复后不回退，Subagent 请求消耗仅内存，主模型与审核序号在各自执行内不重复。
9. Main 保存失败不推进上下文或工具，结算失败只重试必要持久化；Subagent 不因失败或结束写入兜底日志。
10. 暂停、停止及应用关闭等待所属执行和工具退出，完成必要结果交接后释放；无跨生命周期历史引用。
11. 并发子结果由 Main 执行路径接纳，多个持久化调用共享 JSONL 序号及同步，文件删除和断尾处理符合一致性契约。
12. Main 历史在执行结束后仍可从 JSONL 查询；Subagent 结束或重启后不提供历史恢复查询。

本次仅计划更新，不执行代码验证；实现完成后按本节验收。
