# 代码实施核对与待办

## 一、目标与范围

业务语义以 [architecture.md](architecture.md) 为准，类型所有权、目录和编码约定以 [code-style.md](code-style.md) 为准。本文列出后续实施需要核对的目标与顺序，不表示改造已经完成。

执行与历史按 Session、Run、Turn 和 Entry 组织。Session 是主任务的隔离范围；Core Coordinator 管理已加载会话、输入路由、队列和 Main Run 交接，Run 负责本次执行，Loop 负责 ReAct 迭代。

## 二、职责与调用核对

### 2.1 会话与任务调度

- `core/coordinator` 按 SessionID 定位队列和 Main Run 占用。
- 同一 Session 同时最多有一个活动 Main Run，占用覆盖准备、运行和必要收尾。
- 不同 Session 可以并行；切换查看对象不改变后台执行资格或清空队列。
- TaskQueue 只在内存中维护 FIFO、领取、取消待办和暂停恢复；Coordinator 决定何时启动下一项。队列不持久化，不把已启动的 Run 当作待办清理。
- Main Run 正常 `completed` 且队列未暂停时自动领取下一项；主动 `interrupted` 不自动推进后续任务。
- 历史写入失败时报告错误并暂停所属 Session 的队列，等待显式恢复。
- Main 的普通任务输入用于后续 Run，不注入当前 Run。
- Session 首次接纳用户输入时用原文生成标题，标题为空时才写入。
- 停止命令以 SessionID 和 RunID 定位活动执行。命中活动 Main Run 时暂停队列并取消该 Run，已结束的目标直接返回。
- 关闭 Session 停止全部所属执行并等待收尾，然后释放登记和调度资源，保留历史。

### 2.2 会话加载与配置

| 内容 | 所有者与契约 |
| --- | --- |
| 已加载 Session 的登记、查找和移除 | Core Coordinator 的私有 `sessionRegistry` |
| Session 的实际加载 | 装配时提供的 Repo 能力，成功即返回可用核心对象 |
| 日志读取、索引重建和必要中断收尾 | 加载实现内部完成，不向 Service 暴露恢复步骤 |
| 当前模型和推理等级 | Session |
| 已加载只读配置引用 | 按 Session 保存，通过明确入口替换 |
| 本次实际执行配置 | Run 开始时固定 |

`GetSession` 只定位已加载对象；`LoadSession` 按需加载并登记。Service 不维护另一份会话注册表，不逐条读取历史组装 Session。

配置文件是模型目录、Provider 凭据、工具启停和审批默认值的唯一编辑来源。`/reload` 只允许在目标 Session 没有活动 Run 时执行；Reload 期间禁止用户输入和新 Task 入队，并清理未领取的待办。成功后目标 Session 获取新的只读内存快照，其他 Session 保持旧配置引用，已冻结 Run 不受影响；失败时保留原引用。

### 2.3 Run 与 Loop

- Run 统一保存身份、配置、状态、结果、时间、统计和已接纳 Entry 的只读引用。
- Coordinator 确定执行配置并绑定 Session 提供的 Agent 历史。Run 构造完成后显式提交准备及输入事实，Loop 和工具审核共用已提交的输入 Entry。
- `Run.Execute` 组织准备、直接调用 Loop、子执行收尾和终态保存，完成必要收尾后返回。
- Provider 只负责一次模型请求尝试；模型请求重试由 `core/loop` 按业务策略负责，每次尝试使用新的 RequestID、独立计时和独立 ModelRequest 事实，复用原 TurnID。
- 首次请求失败后最多重试三次，共最多四次尝试；每次 ModelRequest 保存 `retryCount`，首次为 0，重试依次递增，不从请求顺序推导。
- Provider client 或流式基础设施负责 HTTP 响应体、解码协程和事件通道的释放，Core 只通过 Context 取消并消费统一事件。
- Run 通过明确业务行为维护私有状态和结果，不向外暴露任意改写执行记录的通用回调或逐字段 setter；查询只提供所需的只读结果。
- `activeRun` 等控制对象只保存 Run 引用、取消及就绪和完成资源，不复制业务状态。
- Loop 每轮读取已接纳历史，经 ContextBuilder 构建上下文，必要时压缩，再请求模型和执行工具。
- Loop 不领取 Task，不依赖 Run 的执行对象；Run、Loop 和 Session 共同使用 `core/entry`。
- Run 的生命周期使用 `starting`、`running`、`ending` 和 `ended`；`Outcome=interrupted` 表示用户主动停止或异常关闭恢复，FailureCode 区分具体原因。`yielded` 只表示 Subagent 完成后主动向 Main Run 交付结果；终态不重新启动同一个 Run。

### 2.4 Turn 与 ModelRequest

- Turn 由 ReAct 迭代流程开始和结束，包含普通请求、必要审核、工具执行及结果接纳。
- 普通请求和它触发的风险审核关联同一个 Turn。
- 两轮之间的摘要属于 Run，不关联某个 Turn；摘要保存为正式 Entry。
- 每个 ModelRequest 独立记录实际状态、用量和计时，请求记录不推进 Turn。
- 每次模型请求尝试使用独立 RequestID 和计时；重试仍复用原 TurnID，失败、部分输出、已知用量和重试进度按实际事实保存。
- 已收到但未完成的模型输出不作为完整 Assistant Entry、可执行工具调用或下一次模型上下文。
- Turn 的展示序号可以从同一 Run 的历史顺序派生，不额外保存 Sequence。
- Turn 完成通过直接流程确定和记录，通知只表达已确定结果。

### 2.5 Entry、提交与历史读取

- `core/entry` 表达公共执行事实；持久化统一使用 Loop 和 Run 调用的 `AppendEntry`。
- Provider 事件由 `core/provider` 以协议无关类型表达；具体协议编码、解码和流资源管理位于 `infra/providers`。
- Provider 流必须区分正常完成、Provider 或协议错误、Context 取消和无终态关闭；通道关闭本身不是成功信号，一次请求最多有一个终态事件。
- 具体消息与执行元数据使用具体 Payload，ContextBuilder 按语义选择模型可见内容。
- Entry 保存条目标识、执行归属、来源和创建时间，具体数据由 Payload 持有。落盘时顶层 `kind` 标识载荷类型，`payload` 直接保存具体数据。
- 用户输入、助手回复和工具回执使用 `message` 载荷，`payload.role` 区分角色，正文与工具内容在 `payload.content` 中。摘要使用 `summary` 载荷；模型请求使用 `model_request` 载荷，阶段由 `payload.status` 表达。
- Entry 与前端 Event 分别设计，不复制通知路由信封。
- Session 和所属 Run 的存储归属在提交入口及必要存储元数据中保留；RequestID、CallID、报告和消息标识由有引用需要的 Payload 携带。
- 各日志按追加顺序读取，不要求所有 Entry 具有通用 ID 或 ParentID。
- 主历史跨多个 Run 持续保留，子代理的中间历史独立读取。
- 正文按需分页；模型上下文按摘要和保留内容选择范围，不受界面页大小限制。
- 同一历史可以多次压缩；下一次压缩使用最近摘要及其后的新增历史，不使用 `RetainedEntryIDs` 或额外的原始 Entry 引用和上下文边界标记。
- 摘要生成完成后即使 Run 被取消也完成保存；保存失败不能使用仅存在内存中的摘要继续执行。
- 每个 Session 只有一条连续主历史，不建立 Branch 或其他并行历史树；Entry、事件、查询和外部 API 不携带 BranchID。
- Run 的状态、请求和用量索引基于同一事实维护，不形成平行写入入口。
- 历史归属在执行准备、绑定提交入口时确定，调用方不在每次提交时重新选择主历史或子代理历史。
- 消费已经接纳的只读事实时，不重复比较完整正文、校验同一归属或进行入口准入判断；维护执行证据和索引不形成另一轮准入或写入。

### 2.6 记录、通知与前端

- 执行流程分别调用 `AppendEntry`、Emitter 和记录相关的观察 Hook；提交、执行证据接纳和通知发布的调用关系明确，不集中隐藏在通用追加包装函数中。
- 记录入口不隐式发布通知或调用观察 Hook，通知订阅者不保存或推进业务。
- SessionID 绑定到发布函数和 `praxis:session:<sessionId>` 通道。
- 通知统一传递 `kind` 和 `data`。Run 级通知携带 `agentId` 和 `runId`，按 Turn 定位的通知同时携带 `turnId`。
- RequestID、CallID 等同样按实际语义声明；消费端按事件种类缩窄类型，不用空身份补齐必需字段，不复制事件模型或增加内部重复校验。
- 每个已加载 Session 使用固定作用域的 sessionStore；普通数据与流式输出分别通知订阅者。
- Store 保存数据，纯函数接纳事件，Hook 组织异步请求、操作与缓存策略，组件负责展示。
- 操作 Hook 拥有各自的请求状态、结果、错误和过期响应检查，对外返回业务动作与状态；页面派生汇总反馈或禁用状态，不下发共享 `busy`、错误 setter 和内部请求 ref。
- 跨 Hook 协作只传必要身份和明确回调，不依赖整个页面状态对象，不增加通用命令框架。
- 目录按 task、run、agent、history 等功能语义组织，不把全部会话能力集中到一个文件。
- 初始化先订阅再加载，保留加载期间的新变化；旧响应和旧 Run 通知不覆盖新的状态。
- Run 和 Agent 的执行状态接纳后端明确给出的结果；停止请求只更新操作反馈，不直接置为已中断，Turn 开始和结束不推导执行状态或覆盖 Run 终态。
- 初始化只读取主 Agent 的历史首屏，不查询代理列表、不从日志恢复 Run 展示状态。会话信息和待办队列随首屏返回，后续 Run 和子代理状态来自实时通知。
- 历史分页返回 `HistoryMessage` 和独立的模型请求活动，用量随单次请求展示。持久化消息随追加通知交付；子代理历史通过委派回执按需打开。
- Run 开始通知提供开始时间，收尾通知不带附加数据，结束通知提供时间、结果、代理状态和最终的迭代及工具调用数。前端按通知信封中的 `runId` 跟踪执行，不查询 Run。停止子 Run 时由父 Run 按 RunID 定位自己持有的子执行句柄。
- 工具调用按 Provider 返回顺序登记；只读工具可以并行，修改文件或其他外部状态的工具按 Provider 顺序串行。结果按 CallID 配对，并按 Provider 顺序组成一个包含多个 ToolResultBlock 的工具结果 Entry。
- `deny` 是配置或策略产生的默认拒绝，`ask`、`auto`、`yolo` 是前端可选模式；人工拒绝保存为工具回执，审核中状态投影为 `waiting`。Hook 不得修改已经冻结的工具参数。
- Wails 接口的请求、返回值和事件类型由 `wails/bindings` 定义。绑定层负责入口校验、调用和结果投影，Core 不依赖绑定层。内部 RunRecord、控制句柄、凭据、内部配置和推理状态不得直接暴露。

### 2.7 子代理与失败收尾

- 子代理严格属于创建它的父 Run，通过 ParentRunID 和 ParentCallID 保留来源。
- 父 Run 持有子执行句柄和完成回调接收对象，不按会话当前 Run 重新定位接收者。
- Subagent 禁止递归创建；创建时冻结工具集合、工作目录和审批边界，可使用父 Run 工作目录但不拥有请求审批权限。
- 子执行保存结果后以 `yielded` 主动交付给绑定的父 Run；父 Run 通过明确的等待或收集工具接纳报告，不自动把报告正文注入上下文。
- 子 Run 不设整体时限。`subagent_wait` 单次等待最多 30 秒，到期返回 `timed_out`，尚未结束的子执行继续运行。`subagent_cancel` 发出取消信号后立即返回，子执行在后台完成收尾。
- 每个 Session 最多 10 个活动子 Run，取消收尾完成后才释放名额；不建立额外 Subagent 等待队列。
- 保存失败明确报告并停止相关执行，不为补日志重新执行模型或工具。
- 加载时只对必要候选进行中断收尾，不自动继续旧 Run。

### 2.8 Project、文件输出与外部边界

- Project 创建后工作目录路径不可变；不增加跨 Session 的文件锁或项目协调。
- 超大工具输出写入系统临时目录，不由 Project 或 Session 长期持有；FilePreview 功能移除，但保留 Agent 的 `read_file` 工具。
- 移除前端模型配置编辑页及对应的 `SaveModelConfig`、`SetProviderKey` 保存入口，配置文件成为唯一编辑来源。
- Branch 相关字段、接口、前端状态和存储数据全部清理；不提供旧 Branch 的兼容读取或迁移。实施时按唯一主历史保留范围核对存储清理，不新增兼容层。

### 2.9 命名与局部表达

- Task、Run、Turn 的变量名、方法名和日志字段保持一致；Run 快照不命名为 `task`，`run_id`、`task_id` 和 `source_task_id` 记录各自对应的身份。
- 多步骤闭包、复杂对象和较长参数列表按语义换行；只有复用、复杂逻辑或明确概念需要时才抽取函数，不为压缩行数建立转发层、参数对象或辅助类型。

## 三、实施顺序

1. 固定 Session 调度范围、连续日志归属及外部接口定位方式。
2. 落实 Core Coordinator、唯一 sessionRegistry 和 Repo 可用对象加载契约。
3. 统一 Run 和 `Run.Execute`，将共享记录与计时放入公共模块。
4. 收敛唯一 `AppendEntry`，区分 Turn 与模型请求，明确用量和结果的事实来源。
5. 对齐 Session 通道、前端 sessionStore、初始化、分页和流式输出职责。
6. 将 `runToolCalls` 改为按 Provider 顺序登记、只读并行、修改工具顺序屏障和批量 ToolResult Entry。
7. 移除 `RetainedEntryIDs`，将压缩输入收敛为最近摘要及其后的历史；补齐摘要保存和取消边界。
8. 核对 Provider 一次尝试、Loop 最多四次尝试、ModelRequest 独立记录、部分响应保存及流终态契约。
9. 收敛 `/reload` 的无活动 Run 准入、输入封锁、队列清理和 Session 配置快照；移除前端模型配置保存入口。
10. 清理 Branch 字段、接口、前端状态和存储数据；移除 `PreviewFile` 入口；补齐子代理 `yielded` 交付和 Wails 结果投影。
11. 核对配置重载、停止、子代理回调、关闭和错误收尾的直接调用关系。

结构调整服务于这些职责，不机械地为每个概念创建管理器或通用接口。

## 四、风险与验证

核心风险是重复登记或执行状态、错误的完成目标、资源提前释放，以及日志和派生索引不一致。按单一所有者、固定接收对象和完整收尾边界处理。持久化格式实施时明确数据影响及匹配备份，不在文档阶段操作真实历史。

后续实施按以下场景人工核对：

1. Session A、B 并行，同一 Session 内 Main Run 按 FIFO 串行。
2. 切换会话不影响后台执行、待办、控制目标和用量接纳。
3. 模型选择按 Session 独立，已启动的 Run 配置稳定。
4. 普通请求与审核属于同一 Turn，摘要不伪造 Turn，用量按实际请求累计。
5. 历史分页与实时输出分开，浏览旧记录后提交任务仍接续当前主历史。
6. 没有通知订阅者时，提交、Run 收尾和下一项调度仍然正确。
7. 子执行回调始终交给创建它的父 Run；关闭等待主、子执行完整退出。
8. Run 资源释放后历史可查询，重启不重复执行模型或工具。
9. Provider 只执行一次尝试，重试由 `core/loop` 负责；每次尝试有独立 RequestID、计时和 ModelRequest 记录，但复用同一 TurnID。
10. 正常完成、Provider 错误、Context 取消和无终态关闭分别收尾；已收到的部分内容、用量和计时得到保存，未完成内容不进入正式 Entry、工具执行或下一次上下文。
12. 同一响应中的只读工具并行执行，修改工具按 Provider 顺序串行；结果按 CallID 和 Provider 顺序形成一个批量工具结果 Entry。
13. Subagent 完成后以 `yielded` 主动向绑定的 Main Run 交付结果；父 Run 取消时子 Run 立即取消，收尾完成后释放名额。
14. `/reload` 仅在无活动 Run 的 Session 执行，期间拒绝输入和入队；成功后只替换目标 Session 的配置快照。
15. Branch、模型配置编辑入口和 FilePreview 不再出现在目标代码、接口、事件或存储边界；Project 路径不可变，工具大输出使用系统临时目录。

验证遵守仓库约定：后端使用 `go build ./...`、`go vet ./...`、`go list ./...`；签名变化时按已有流程重新生成 Wails 绑定，前端使用 `npm run check`。不新增或运行自动化测试，不自动启动开发服务；数据验证使用独立临时目录。
