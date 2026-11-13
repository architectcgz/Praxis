# 代码实施核对与待办

## 一、目标与范围

业务语义以 [architecture.md](architecture.md) 为准，类型所有权、目录和编码约定以 [code-style.md](code-style.md) 为准。本文列出后续实施需要核对的目标与顺序，不表示改造已经完成。

执行与历史按 Session、Run、Turn 和 Entry 组织。Session 是主任务的隔离范围；Core Coordinator 管理已加载会话、输入路由、队列和 Main Run 交接，Run 负责本次执行，Loop 负责 ReAct 迭代。

## 二、职责与调用核对

### 2.1 会话与任务调度

- `core/coordinator` 按 SessionID 定位队列和 Main Run 占用。
- 同一 Session 同时最多有一个活动 Main Run，占用覆盖准备、运行和必要收尾。
- 不同 Session 可以并行；切换查看对象不改变后台执行资格或清空队列。
- TaskQueue 封装 FIFO、领取、取消待办和暂停恢复；Coordinator 决定何时启动下一项。
- Main 的普通任务输入用于后续 Run，不注入当前 Run。
- 停止命令定位具体 RunID；较早命令不能在等待后作用于新的执行。
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

Reload 协调目标 Session 的暂停、停止和等待，再加载并替换配置引用。其他 Session 和已经固定的 Run 配置保持各自语义。模型失效阻塞与用户主动暂停分别表达。

### 2.3 Run 与 Loop

- Run 统一保存身份、配置、状态、结果、时间、统计和已接纳 Entry 的只读引用。
- `Run.Execute` 组织准备、直接调用 Loop、子执行收尾和终态保存，完成必要收尾后返回。
- `activeRun` 等控制对象只保存 Run 引用、取消及就绪和完成资源，不复制业务状态。
- Loop 每轮读取已接纳历史，经 ContextBuilder 构建上下文，必要时压缩，再请求模型和执行工具。
- Loop 不领取 Task，不依赖 Run 的执行对象；Run、Loop 和 Session 共同使用 `core/entry`。
- Run 的正常收尾使用 `ending`，中断使用 `interrupted`；终态不重新启动同一个 Run。

### 2.4 Turn 与 ModelRequest

- Turn 由 ReAct 迭代流程开始和结束，包含普通请求、必要审核、工具执行及结果接纳。
- 普通请求和它触发的风险审核关联同一个 Turn。
- 两轮之间的摘要属于 Run，不强行关联某个 Turn。
- 每个 ModelRequest 独立记录实际状态、用量和计时，请求记录不推进 Turn。
- Turn 的展示序号可以从同一 Run 的历史顺序派生，不额外保存 Sequence。
- Turn 完成通过直接流程确定和记录，通知只表达已确定结果。

### 2.5 Entry、提交与历史读取

- `core/entry` 表达公共执行事实；持久化统一使用 Loop 和 Run 调用的 `AppendEntry`。
- 具体消息与执行元数据使用具体 Payload，ContextBuilder 按语义选择模型可见内容。
- Entry 与前端 Event 分别设计，不复制通知路由信封。
- Session 和所属 Run 的存储归属在提交入口及必要存储元数据中保留；RequestID、CallID、报告和消息标识由有引用需要的 Payload 携带。
- 各日志按追加顺序读取，不要求所有 Entry 具有通用 ID 或 ParentID。
- 主历史跨多个 Run 持续保留，子代理的中间历史独立读取。
- 正文按需分页；模型上下文按摘要和保留内容选择范围，不受界面页大小限制。
- Run 的状态、请求和用量索引基于同一事实维护，不形成平行写入入口。
- 消费已经接纳的只读事实时，不重复进行入口准入判断。

### 2.6 记录、通知与前端

- 执行流程分别调用 `AppendEntry` 和 Emitter；记录入口不隐式发布，通知订阅者不保存或推进业务。
- SessionID 绑定到发布函数和 `praxis:session:<sessionId>` 通道。
- 通知统一传递 `kind`、可选 `runId` 和 `data`；TurnID、RequestID、CallID 等按具体事件需要提供。
- 每个已加载 Session 使用固定作用域的 sessionStore；普通数据与流式输出分别通知订阅者。
- Store 保存数据，纯函数接纳事件，Hook 组织异步请求和操作，组件负责展示。
- 目录按 task、run、agent、history 等功能语义组织，不把全部会话能力集中到一个文件。
- 初始化先订阅再加载，保留加载期间的新变化；旧响应和旧 Run 通知不覆盖新的状态。
- 停止请求不等于 Run 已中断；Turn 完成不等于 Run 已结束。
- 用量接纳包含普通、审核和摘要请求，不受聊天展示筛选影响。

### 2.7 子代理与失败收尾

- 子代理严格属于创建它的父 Run，通过 ParentRunID 和 ParentCallID 保留来源。
- 父 Run 持有子执行句柄和完成回调接收对象，不按会话当前 Run 重新定位接收者。
- 子执行保存结果后直接回调，父 Run 保存收到的引用后交付或唤醒等待。
- 每个 Session 最多 10 个子代理执行名额，取消收尾完成后才释放。
- 保存失败明确报告并停止相关执行，不为补日志重新执行模型或工具。
- 加载时只对必要候选进行中断收尾，不自动继续旧 Run。

## 三、实施顺序

1. 固定 Session 调度范围、连续日志归属及外部接口定位方式。
2. 落实 Core Coordinator、唯一 sessionRegistry 和 Repo 可用对象加载契约。
3. 统一 Run 和 `Run.Execute`，将共享记录与计时放入公共模块。
4. 收敛唯一 `AppendEntry`，区分 Turn 与模型请求，明确用量和结果的事实来源。
5. 对齐 Session 通道、前端 sessionStore、初始化、分页和流式输出职责。
6. 核对配置重载、停止、子代理回调、关闭和错误收尾的直接调用关系。

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

验证遵守仓库约定：后端使用 `go build ./...`、`go vet ./...`、`go list ./...`；签名变化时按已有流程重新生成 Wails 绑定，前端使用 `npm run check`。不新增或运行自动化测试，不自动启动开发服务；数据验证使用独立临时目录。
