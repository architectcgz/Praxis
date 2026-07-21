# WALAgent

**Work And Learn Agent** —— 面向学习者本人的多 Agent 编排桌面应用：一边借助 Work Agent 做事，一边用 Learn Agent 提问与获得反馈；上下文彼此隔离，必要时把学习结论可控地回流到工作侧。

> 本文档沉淀当前方案讨论结论与待定项，**不是最终实现规格**。实现前需继续对齐下文「待定决策」。

---

## 1. 要解决什么问题

在编程（及其他）学习过程中：

1. 需要 Agent **帮你推进工作**（写代码、改问题、完成练习）。
2. 需要随时 **打断提问**（原理、取舍、纠错、讲解），且 **提问不应污染 Work Agent 的上下文**。
3. 提问得到的结论 **有时对 Work 有帮助**，需要一种机制把 Learn 的结果 **组装进 Work 的后续输入**。
4. 需要 **笔记 Agent** 把值得保留的内容持久化。
5. 需要 **独立 App UI**：左侧任务列表，右侧 Agent 列表与上下文查看，并能召唤 Agent 提问。
6. **Agent 自我进化**（从结果自动改策略/技能）可考虑，但明确为 **后期**，不进入首期。

核心原则：

- **共享 Task Session 是协作真相**；各 Agent 线程上下文隔离。
- **消息可以飞，回流必须结构化且默认可审批**（不全文灌入 Work）。
- 学习的对象是 **你（用户）**，不是让 Agent 无人盯梢地自我进化。

---

## 2. 产品定位

| 概念 | 含义 |
|------|------|
| 名称 | WALAgent = **Work And Learn** Agent（不是 Write-Ahead Log） |
| 用户 | 学习者本人（个人学习/编程练习为主，场景可扩展） |
| 形态 | 独立桌面 App + monorepo 内的编排核心 |
| 协作模型 | 共享任务 Session + 多角色 Agent 隔离 transcript + Briefing 回流 |

与相近概念的边界：

| 系统 | 职责 | 与 WALAgent |
|------|------|-------------|
| 通用聊天 / 单会话助手 | 同一上下文里又做又问 | 本产品刻意 **拆开 Work / Learn 上下文** |
| 共享黑板 / 纯任务板 | 只存状态 | 本产品以 **编排 + UI + 回流** 为中心，状态是载体 |
| agentmemory 等记忆系统 | 长期知识记忆 | 首期 **不替代**；笔记可本地持久化，后续再考虑桥接 |
| Agent 自我进化 | 自动更新策略/技能 | **P3 以后**，非首期目标 |

---

## 3. 角色与职责

| 角色 | 职责 | 不负责（默认） |
|------|------|----------------|
| **Work Agent** | 执行当前任务（编程实现、改 bug、推进交付物） | 长篇教学闲聊；被问答对话直接污染 |
| **Learn Agent** | 用户打断后提问、讲解、纠错、给反馈 | 直接改业务代码（除非用户另有约定） |
| **Note Agent** | 将值得保留的内容整理并持久化为笔记 | 替代 Work 执行任务 |
| **Session / 编排层** | 任务列表、Agent 生命周期、打断/恢复、Briefing 审批与注入、审计 | 具体领域业务实现细节 |

主场景默认：**编程学习**；架构上保留 `scenario` 扩展（general 等）。

---

## 4. 关键交互（主路径）

```text
1. 在 Task Session 中启动 Work Agent A，开始写代码 / 推进任务
2. 用户打断 A → A 进入 paused（不销毁上下文）
3. 召唤 Learn Agent B 提问；B 使用独立 transcript，不进入 A 的历史
4. B 产出结构化 Briefing（Insight / 摘要 / 建议 / 相关路径）
5. 用户确认（默认）后，编排层将 Briefing 组装进 A 的「下一轮输入包」
6. 恢复 A 继续工作；需要时 Note Agent 把问答或结论写入笔记
```

### 4.1 打断与隔离

- Work 与 Learn **分线程、分 transcript**。
- 打断 Work = **暂停**，不是清空；恢复后接续原上下文 + 可选 Briefing。
- UI 可查看每个 Agent 的上下文/状态（running / paused / …）。

### 4.2 Learn → Work 通信（Briefing）

原则：

- **禁止** 默认把 Learn 全文聊天塞进 Work。
- 回流单元为结构化 **Briefing**，建议字段包括：
  - 标题、摘要结论
  - 可执行建议列表
  - 相关文件/符号路径
  - 来源 Learn Agent、目标 Work Agent、状态（draft / pending_approval / approved / rejected / injected）
- 默认策略：**用户确认后再注入**（编程场景更安全）；自动注入可作为后续策略选项。
- Session 内保留审计记录：何时、由谁、注入了什么。

### 4.3 笔记

- Note Agent 负责持久化「对学习者有长期价值」的内容（概念、踩坑、命令备忘、会话结论等）。
- 笔记与 Agent transcript 分离，避免 UI/线程生命周期带走知识。

---

## 5. UI 方向（独立 App）

| 区域 | 内容 |
|------|------|
| 左侧 | Task / Session 列表 |
| 右侧 | 当前 Session 下的 Agent 列表 |
| 点击 Agent | 查看该 Agent 上下文（transcript）、状态 |
| 会话内操作 | 启动/打断 Work；召唤 Learn 提问；采纳 Briefing 回流到 Work；召唤 Note 记笔记 |

首期即做 App（用户不急上线，但希望产品形态从一开始就是桌面应用，而不是长期只有 CLI）。

---

## 6. 技术方向（讨论结论，实现前可再改）

| 项 | 当前倾向 | 说明 |
|----|----------|------|
| 仓库形态 | **Monorepo** | UI + 本机 runtime 同仓 |
| 桌面壳 | **Electron** | 首期就要 App |
| 渲染层 UI | **React** | 本项目选定 React |
| **要不要「服务端」** | **首期不要云端/远程 Server** | 见 §6.1 |
| **Agent 流程编排语言** | **TypeScript（已定）** | 见 §6.2；与 Electron/Node 同栈 |
| **本机 runtime 形态** | **Electron 主进程 + `packages/core`（已定）** | 首期不做 Go sidecar；领域逻辑放可单测的 TS core |
| UI ↔ 编排边界 | **已定：界面与 Agent 状态机分离** | Electron/React **只画界面**；状态机在 `packages/core` + 主进程，见 §6.1.2 |
| 状态存储 | **混合：SQLite + 文件树（JSONL 等）** | 见第 7 节；待确认 |
| 出网 | **仅调用 LLM Provider 时** | 无自建业务服 |

说明：曾讨论 Go「后端」；已澄清 **无远程服**；**编排语言已定为 TypeScript**（§6.2）。目录避免叫 `backend/`，优先 `packages/core` + `apps/desktop`。

### 6.1 要不要服务端？（概念澄清）

| 含义 | 是什么 | 本产品首期 |
|------|--------|------------|
| **A. 云端/远程业务服务端** | 账号、多端同步、托管会话 | **不需要** |
| **B. 本机 Agent Runtime** | 编排、落盘、调 LLM、管线程 | **需要**（语言另选） |
| **C. 第三方 API** | 模型厂商 | **可选出网** |

**结论：个人本机 App 首期不需要自建远程服务端**；需要的是本机 runtime（领域层），不必叫 server。

**何时才需要远程服？** 多设备同步、多用户、团队共享 Session、云端代管密钥跑 Agent 等——均非首期。

### 6.1.1 「为什么还要后端？」——指的不是服务器

日常说「App 要不要后端」常指：**要不要单独部署一台业务服务器**。答案是 **不要**。

架构里仍会出现「非 UI 的一层」，容易被顺口叫成 backend，更准确的名字是：

| 名称 | 实际是什么 | 跑在哪 |
|------|------------|--------|
| **渲染层 / UI** | 任务列表、Agent 列表、聊天视图、按钮 | Electron **渲染进程**（React） |
| **本机 Runtime / 领域层** | Session 状态机、打断/恢复、Briefing、笔记、落盘、调 LLM | Electron **主进程**（TypeScript）+ `packages/core` |
| **远程业务服务端** | 账号、云同步、托管会话 | **首期没有** |
| **模型厂商 API** | 生成回复 | 出网调用（可选），不是「我们的后端」 |

所以：

- **不需要** = 自建 HTTP 服务、数据库主机、登录体系  
- **需要** = 任何正经桌面 App 都有的 **「界面 vs 业务逻辑」分层**；在 Electron 里业务逻辑放在主进程更合适  

#### 若「不要任何后端」、业务全写在 React 里会怎样？

| 能力 | 只放在 React 组件里 | 放到主进程 TS runtime |
|------|---------------------|------------------------|
| 长时间跑 Work Agent、流式输出 | 组件卸载/切页易丢；难做全局单例 | 与窗口生命周期解耦 |
| 密钥、读用户工程目录、写数据目录 | 渲染进程权限与安全模型更别扭 | 主进程更合适（可配合预加载桥） |
| SQLite / 追加 JSONL | 能做但和 UI 耦合、难单测 | `packages/core` 可单测、可换 UI |
| 打断 Work + 并行 Learn 线程 | 状态散落在多个组件 | 统一状态机，单一真相 |
| 渲染崩溃 / 热更新 | 易带走正在跑的 Agent | Agent 可挂在主进程侧 |

**一句话：要的不是「服务器」，而是「别把 Agent 编排写在按钮 onClick 里」。**  
实现上这层就是已定的 **TypeScript 编排（主进程 + packages/core）**，通过 **Electron IPC** 和 React 说话——**本机进程内通信，不是公网后端。**

### 6.1.2 界面与状态机分离（已定，与用户共识一致）

**用户明确：Electron 只用来画界面；Agent 状态机必须与界面分开。**

| 层 | 职责 | 不职责 |
|----|------|--------|
| **Electron 渲染进程 + React** | 布局、列表、对话展示、按钮与表单、订阅并渲染状态 | 不拥有 Agent 状态机；不直接持久化真相；不直接持有「能否注入 Briefing」等业务规则 |
| **`packages/core`（TS）** | Task/Session/Agent 状态机、打断/恢复、Briefing 生命周期、编排规则 | 不依赖 React；不关心像素与路由 |
| **Electron 主进程** | 拉起/托管 core、IPC 适配、本机文件/密钥等系统能力、进程生命周期 | 不把 UI 组件逻辑写进主进程业务核心 |

通信方向（概念）：

```text
用户操作（点击打断 / 发送提问 / 批准 Briefing）
        │
        ▼
  React UI  ──命令 IPC──►  主进程适配  ──►  packages/core（状态机唯一写入口）
        ▲                                         │
        │                                         ├── 落盘
        └──────── 事件/快照 IPC ◄──────────────────┤
                                                  └── 调 LLM（若启用）
```

原则：

1. **UI 是投影，不是真相**：列表上显示的 paused/running 来自 core 的状态快照或事件流。  
2. **所有状态变更走 core 命令**（如 `PauseWork`、`ApproveBriefing`），禁止渲染进程直接改「权威状态」。  
3. **换皮不影响编排**：以后换 UI 框架或加 CLI，只要接同一套 core 命令/事件即可。

这与「不要远程服务端」不冲突：分离的是 **本机进程内的 UI vs 领域**，不是「要部署一台 server」。

### 6.2 为什么编排用 TypeScript（已定）

「Agent 流程」在本产品里主要是：

- Session / Agent **状态机**（idle → running → paused → …）
- **打断 / 恢复**、Briefing **审批与注入**
- 组装下一轮模型输入、流式输出、工具调用编排
- 读写 SQLite / JSONL、通过 IPC 推 UI 事件

这些是 **异步 I/O + 状态机 + JSON 变换**，不是重 CPU 数值计算。在已选定 **Electron + React** 的前提下：

| 维度 | **TypeScript 编排** | **Go 编排（sidecar）** |
|------|---------------------|-------------------------|
| 与 App 集成 | 主进程直接跑；IPC 一等公民 | 要多进程生命周期、端口/stdio、打包两个产物 |
| LLM / Agent 生态 | Anthropic/OpenAI 官方 SDK、流式、工具调用示例多在 JS/TS | 有客户端，但 agent/tooling 生态整体偏 TS |
| 类型共享 | 与 React 共用类型/zod/openapi 生成更顺 | 跨语言契约（OpenAPI/JSON Schema）额外成本 |
| 开发速度（个人项目） | 热更新、同仓同语言、调试一条链 | 前后端两套工具链 |
| 性能 / 并发 | 够用（瓶颈在 LLM 网络与 token） | 原生并发强，但本场景收益通常不明显 |
| 进程隔离 | 编排挂了可能影响主进程（可用 utilityProcess 缓解） | 崩溃隔离更好 |
| 与你其他 Go 服务复用 | 弱 | 若未来要无 UI 的 headless 调度器，Go 可复用 |

**此前写「倾向 Go」的来源：** 你提过「agent 后端用 go」，当时按「远程/独立后端」惯性理解，**并不是**「Electron 里编排不能用 TS」。  
澄清本机 App 之后：**没有技术义务必须用 Go 做流程编排。**

**已定形态（用户确认编排用 TS）：**

```text
[ React 渲染进程 ]  UI only
        │  Electron IPC
        ▼
[ Electron 主进程 · TypeScript Runtime ]
  · Task / Session 编排
  · Work / Learn / Note 线程
  · 打断 / 恢复 / Briefing
  · 落盘（SQLite + JSONL）
  · 调用外部 LLM API
        │
        ▼
[ 本机数据目录 ]
```

结构约定（仍全 TS）：

- `packages/core`：纯 TS 领域与状态机（可单测，不依赖 Electron）
- 主进程只做 IPC 适配与进程生命周期
- 若担心主进程阻塞：Node `utilityProcess` / worker 跑 core（仍是 TS）

**首期明确不做 Go sidecar。** 若将来出现 headless 与 App 必须共用非 TS 实现、或强制进程隔离等硬需求，再单独评估，不作为当前路线。

### 6.3 本机形态对照（均无远程业务服）

| 形态 | 编排语言 | 说明 | 首期建议 |
|------|----------|------|----------|
| **A. Electron + TS runtime（主进程 / packages/core）** | TS | 单语言主路径 | **已定** |
| **B. Electron UI + Go sidecar** | Go | 双语言；隔离好、集成重 | 首期不做 |
| **C. Wails 等 Go 桌面 + 内嵌 UI** | Go | 与「Electron+React」选型不同 | 非当前路线 |
| **D. 以后加远程同步服** | 任意 | 多设备 | 非首期 |

### 6.4 Agent 状态机：手写还是用现成项目？（调研）

先把「状态机」拆成 **两层**，否则会在错误层级选框架：

| 层级 | 管什么 | 例子（本产品） |
|------|--------|----------------|
| **L1 产品编排状态机** | Session/Agent 生命周期、打断、Briefing 审批注入、与 UI 的命令/事件 | Work `running→paused`、Learn 独立线程、`Briefing pending→approved→injected`、Note 落笔记 |
| **L2 LLM 执行环** | 单次/多轮模型调用、tool call、流式输出、可选图编排与 checkpoint | Work 里「读文件→改代码→再问模型」循环 |

**结论（针对 WALAgent）：**

1. **L1 没有「装上就能当产品」的现成项目**——Work-and-Learn、双上下文隔离、可审批 Briefing 回流是本产品领域模型，任何通用 Agent 框架都不会替你定义这套语义。  
2. **L1 在 `packages/core` 手写显式状态、命令和事件**，不把产品状态机交给通用 Agent 框架。  
3. **单 Agent 生命周期也由 WAL 自建**，参考 Pi 的 phase、turn snapshot、save point、settlement、queue 和 conservative recovery；Provider SDK 只实现模型 I/O，不拥有生命周期。

#### 现成项目对照（npm 可查，约 2026-07）

| 项目 | 大致定位 | 对 L1 产品编排 | 对 L2 LLM 环 | 本机 Electron 注意 |
|------|----------|----------------|--------------|-------------------|
| **D:\projects\pi** | 单 Agent 执行环、AgentHarness phase、turn snapshot、save point、Session 和 supervisor 生命周期设计 | 仅作为 WAL 自建 L1 / L2 的源码级参考 | 设计参考 | 不引入 `pi-agent-core`、`pi-coding-agent` 或 `pi-server` 生产依赖 |
| **@langchain/langgraph**（约 1.4.x） | 有状态 Agent/工作流的低层编排；durable execution、interrupt / human-in-the-loop、checkpoint | 不提供 Work/Learn/Briefing 产品模型；可把「一次 Work 运行」做成 graph，外层 L1 仍要自建 | **强** | 可在 Node 主进程跑；LangChain 生态与心智负担；桌面落盘要自己接 |
| **@openai/agents**（约 0.13.x） | 多 Agent、handoff、sessions、tools、HITL、sandbox agent | Handoff/Sessions ≠「暂停 Work 开 Learn 再 Briefing 回流」；硬套易语义错位 | **中强** | 偏 Agent 运行时；Sandbox 在 Windows 上有客户端限制说明 |
| **@mastra/core** / mastra CLI | TS Agent 应用框架（agent/tools/memory/workflow） | 通用 AI 应用骨架，不是 WAL 产品状态机 | **中强** | 与「极简 core + Electron 壳」可能叠床架屋 |
| **xstate**（约 5.x） | 通用 FSM / statecharts（与 LLM 无关） | **适合** 表达 L1 | 不负责调模型 | 轻、可单测；不自带 LLM |
| **ai**（Vercel AI SDK，约 7.x）+ provider 包 | 统一模型 API、流式、工具调用 | 不管产品 Session 状态机 | **强于 I/O 层** | 与 Electron/React 常见组合；不是多 Agent 产品编排器 |
| **@anthropic-ai/sdk** | 官方 Anthropic API 客户端 | 无 | 原始消息/流式 API | 最薄；L2 可直接用或经 AI SDK |

#### 推荐策略（文档级，实现前可再定）

| 策略 | 做法 | 何时选 |
|------|------|--------|
| **A. 推荐默认** | **手写 L1 + 手写单 Agent 生命周期**；Provider 通过窄 port 接官方 SDK 或统一模型客户端 | WAL 拥有 phase、durability、recovery 和产品语义 |
| **B. L1 用 XState** | 仍由 WAL 定义语义，但借 XState 表达 transition | 只有状态图复杂度被实际证明后再评估，P1 不引入 |
| **C. L2 上 LangGraph** | 单次 Work/Learn 运行内部用 graph；**外层 L1 仍自建** | 单次运行极复杂、强依赖 interrupt/checkpoint 时再上 |
| **D. 整盘 OpenAI Agents / Mastra** | 用框架 Session/Handoff 硬映射 Work/Learn | **不推荐作为 L1** |

**明确建议：L1 和单 Agent 生命周期都由 WAL 手写；Pi 只作为生命周期设计参考，Provider SDK 只处理模型协议。**

「现成项目」能买到的是 **执行环与通用 FSM 零件**，买不到的是 **你的产品状态机与 Briefing 协议**——那一层是本仓库的核心，适合自建并单测。

---

## 7. 落盘格式分析与推荐

### 7.1 要存什么（按访问形态分类）

| 数据 | 典型操作 | 体量特征 |
|------|----------|----------|
| Task / Session / Agent 元数据 | 列表、过滤、状态更新、关联查询 | 小；强结构化 |
| Agent 状态机（running/paused…） | 高频更新、UI 订阅 | 小；要一致 |
| Briefing（草稿→审批→注入） | 按 session/agent 查、状态流转、审计 | 中小；强结构化 |
| Transcript 消息 | 按 agent 追加、按时间/游标读、UI 滚动加载 | **大且只增为主** |
| 流式 token 中间态 | 可只内存或短缓存 | 可不落或短落 |
| Notes | 全文检索、按标签/session 查 | 中；半结构化 |
| 附件 / 代码快照 / 工具输出 | 按路径取整块 | **大块二进制或长文本** |
| 审计 / 事件（打断、注入、错误） | 追加、按时间回放 | 中；只增 |

核心张力：**元数据要查询友好；对话与附件要追加友好、别把 DB 撑成大 BLOB 坟场。**

### 7.2 候选方案对比

#### A. 纯目录：JSON 文件 + JSONL transcript

```text
data/
  tasks/{id}.json
  sessions/{id}/session.json
  sessions/{id}/agents/{agentId}.json
  sessions/{id}/agents/{agentId}/transcript.jsonl
  sessions/{id}/briefings/{id}.json
  notes/{id}.json
```

| 优点 | 缺点 |
|------|------|
| 人眼可读、git/diff 友好、坏了好修 | 列表/关联查询要自建索引或全扫 |
| 实现直观；崩溃时单文件损坏面小 | 并发写同一 JSON 要小心（临时文件+rename） |
| 与「共享工作区/黑板」心智一致 | 跨 session 统计、搜索笔记较痛 |
| Go 实现零重依赖 | 状态机多处更新时易出现「半更新」 |

**适合：** 协议原型、极简 MVP、强调可审计可读。  
**不太适合单独扛：** 你要的 App 左侧任务列表 + 右侧多 agent + 审批流 + 笔记检索，一段时间后会痛。

#### B. 纯 SQLite（含 transcript 大字段）

| 优点 | 缺点 |
|------|------|
| 单文件备份；事务；查询/索引一流 | 超长 transcript/工具输出塞进行 → 膨胀、VACUUM、备份变慢 |
| Go 生态成熟（`database/sql` + modernc/sqlite 或 CGO） | 大文本 diff/外部查看不如文件直观 |
| 状态机与 Briefing 流转好做 | 误用 BLOB 会变成运维负担 |

**适合：** 元数据与关系。  
**风险：** 把「所有消息 content」无脑塞进一张 messages 表且不做分段/外置。

#### C. 纯嵌入式 KV（Badger / Bolt / Pebble）

| 优点 | 缺点 |
|------|------|
| 写路径快、嵌入式 | 查询表达力弱；二次索引要自己维护 |
| | 对 Task 列表 + Briefing 状态机不如 SQL 自然 |

**结论：** 本产品关系型查询多，**不作为主存**。

#### D. 混合（推荐）：SQLite 权威元数据 + 文件系统放大对象

```text
{DataRoot}/                          # 例如用户目录下的 walagent/
  walagent.db                        # SQLite：Task/Session/Agent/Briefing/Note 元数据与索引
  sessions/{sessionId}/
    agents/{agentId}/
      transcript.jsonl               # 只追加消息（或按 chunk 切分）
      transcript.idx                 # 可选：行偏移索引，加速尾部/分页
    attachments/{id}/...             # 大附件、工具输出原文
  notes/bodies/{noteId}.md           # 笔记正文（可选：短文仍可只在 DB）
  tmp/                               # 流式/未完成写入
```

**SQLite 里建议存：**

- Task、Session、Agent 行（状态、标题、外键、时间戳）
- Briefing 行（状态机、摘要字段、source/target agent）
- Agent Run、Briefing Delivery 和编排审计事件
- Note 元数据（标题、tags、session_id）；正文可 DB 或 `.md` 文件

Message 索引不作为 P1 必需项。首期先通过 WAL 自有 Agent Session API 读取 transcript；只有实际性能证据出现后，再增加 SQLite message index 或替换存储实现。

**文件系统里建议存：**

- 完整 transcript（JSONL 一行一条消息，或按 N MB 切 chunk）
- 附件、过长 tool result、代码 patch

| 优点 | 缺点 |
|------|------|
| 列表/审批/关联查询用 SQL | 两套存储，要约定「DB 与文件一致性」规则 |
| 大文本追加不胀爆单库 | 备份要 DB + 目录一起 |
| 坏 transcript 时可按文件隔离修复 | 实现量高于纯 A 或纯 B |
| 与本机 TS runtime 匹配，可直接使用 Node SQLite 和文件系统能力 | |
| 调试：打开 jsonl 就能看对话 | |

**一致性约定（规格级原则，非实现细节）：**

1. **产品元数据以 SQLite 事务为准**（状态切换、Briefing 审批、投递状态）。
2. **单 Agent transcript 由 WAL 自有、版本化的 Agent Session JSONL 负责**；P1 不双写逐消息 SQLite 索引。
3. Briefing 跨 SQLite / JSONL 注入使用唯一 `injectionKey` 和 delivery 记录对账，避免崩溃重试造成重复注入；详见编排架构文档。
4. 不做跨机复制首期；单机单写者（本机 TS runtime）可大幅简化锁模型。

### 7.3 按产品能力对照

| 能力 | 纯 JSON/JSONL | 纯 SQLite | **混合（推荐）** |
|------|---------------|-----------|------------------|
| 左栏 Task 列表 | 一般 | 好 | **好** |
| 多 Agent 状态 + 暂停恢复 | 一般 | 好 | **好** |
| Transcript 长会话 | **好** | 差～中 | **好** |
| Briefing 审批流 | 中 | **好** | **好** |
| 笔记检索 | 差～中 | 好 | **好** |
| 人读/抢救数据 | **好** | 中 | **好** |
| 实现复杂度 | 低 | 中 | 中高 |
| 与本机 TS runtime | 好 | **好** | **好** |
| 日后接 agentmemory | 文件导出易 | SQL 导出易 | **两者都易** |

### 7.4 推荐结论（请你确认）

**首期推荐：方案 D — SQLite（权威结构化 + 索引）+ 目录文件（transcript JSONL + 附件）。**

理由对齐你的产品：

1. **打断 / 多 Agent / Briefing 状态机** 需要事务与查询 → SQLite。  
2. **编程场景对话 + 工具输出** 会很长 → 不宜整段塞 DB → JSONL/文件。  
3. **本机 runtime（推荐 TS 主进程）** 适合「单写者 + 本机接口」；混合存储在同一本地编排进程内收口最干净。  
4. 仍保留「打开目录能看到 transcript/笔记」的可抢救性，符合学习工具心智。  
5. P3 自进化若要加 Lesson 表/事件流，SQL 扩展自然；大对象仍走文件。

**不推荐首期：** 纯目录扛全站查询；纯 SQLite 塞全量 transcript；上 Postgres/MinIO 等过重栈。

**可选简化（若你希望 P0 更快）：**  
先 **「SQLite 元数据 + 每 agent 一个 transcript.jsonl」**，附件也先当文件；Note 正文可暂存 SQLite TEXT，等变长再外置。这是 D 的瘦身版，迁移成本低。

### 7.5 数据根目录建议

- **默认：** 用户数据目录（Windows 如 `%APPDATA%/WALAgent`），不进 git 项目仓。  
- **可配置：** 设置页指定根路径（换机拷贝整个目录 + `walagent.db`）。  
- **密钥：** 不进 DB 明文优先；本机 keychain / 系统凭据或加密配置文件（仍待定）。

### 7.6 JSONL 消息行（契约方向，非最终 schema）

每行一条 JSON，便于追加与流式读取，字段方向示例：

```json
{
  "id": "...",
  "role": "user|assistant|system|tool|briefing",
  "content": "...",
  "created_at": "RFC3339",
  "briefing_id": "optional",
  "meta": {}
}
```

P1 不要求建立 DB `messages` / `message_index` 表；由 WAL Agent Session API 负责 transcript 追加和分页读取。后续只有在大规模 transcript 的实际性能数据支持时，才增加索引或替换存储实现。

---

## 8. 分期

| 阶段 | 范围 |
|------|------|
| **P1** | Task Session 模型；Work / Learn / Note 三角色；打断与恢复；Briefing 审批回流；笔记持久化；**TS 编排（Electron 主进程 + packages/core）** + React App；**无远程服**；编程场景模板；**混合落盘**（待确认） |
| **P2** | 更强工程侧能力（文件树/diff 等）、多 Work 并行、回流策略可配置、非编程 scenario 模板、可选桥接外部记忆 |
| **P3** | Agent 自我进化（Lesson → 策略/技能更新等）——明确延后 |

### 8.1 首期明确非目标

- 多机集群 / 消息中间件级总线
- 通用社交聊天产品
- 替换 agentmemory 或做成通用记忆中台
- 默认无人值守的自动无限自学
- 默认 Learn 全文注入 Work
- Agent 自我进化闭环
- 重型独立 DB 服务（Postgres 等）作为首期依赖
- **自建云端/远程业务服务端**（账号体系、多端同步服等）

---

## 9. 方案演进摘要（讨论时间线）

1. 对比多种 Agent 通信方案后，倾向 **共享工作区 / 黑板** 一类可恢复协作。
2. 立项 `D:\projects\WALAgent`；名称一度被理解为 WAL 日志，后纠正为 **Work And Learn**。
3. 明确：Learn 服务的是 **用户学习**，不是首期 Agent 自进化；自进化可后期再做。
4. 运行时组织：用户要 **共享任务 Session**；**打断 Work → 独立 Learn 提问 → Briefing 回流 Work**；另需 **Note Agent**。
5. UI：独立 App；左任务列表、右 Agent 列表与上下文；可召唤提问。
6. 场景：主编程，可扩展。
7. 工程：monorepo；Electron；UI 用 **React**；首期就做 App（不急交付）。
8. 曾称「Agent 后端用 Go」；澄清 **不是云端服务端**；**用户已定：编排用 TypeScript**（Electron 主进程 + `packages/core`）。
9. **用户明确：Electron 只画界面，Agent 状态机与界面分离**（见 §6.1.2）——与「不要远程服、但要本机 runtime」一致，不是要服务器。
10. **落盘：完成方案对比；推荐 WAL SQLite + WAL Agent Session JSONL + 附件文件混合**（待用户最终确认）。
11. **Agent 生命周期方向**：L1 产品编排与单 Agent 生命周期均由 WAL 自建；Pi 仅作为 phase、snapshot、save point、settlement 和 recovery 设计参考（见 §6.4）。

---

## 10. 待定决策（实现前需对齐）

下列问题会改变目录、依赖与首期范围，**拍板前不应当作已定实现规格**：

1. ~~**要不要远程业务服务端**~~ → **首期不要**（见 §6.1）。
2. ~~**编排语言**~~ → **已定：TypeScript**（Electron 主进程 + `packages/core`；见 §6.2）。
3. ~~**界面与状态机是否分离**~~ → **已定：分离**；Electron/React 只画界面，状态机在 core（见 §6.1.2）。
4. **落盘格式** → 推荐 **SQLite + JSONL/附件文件混合**（见第 7 节）；**待你确认或改选**。
5. ~~**L1 状态机实现方式**~~ → **已定：手写 core**；P1 不引入 XState（见 §6.4）。
6. ~~**单 Agent 生命周期 owner**~~ → **已定：WAL 自建**；Pi 只作设计参考，Provider 客户端仍待选（见 §6.4 和编排架构文档）。
7. **LLM Provider**：runtime 直连哪些 API / 是否桥接本机 CLI。
8. **Work 如何读用户工程**：粘贴/指定路径、工作区、编辑器集成？
9. **Briefing 注入形态**：system 旁路、tool 结果、还是下一轮 user 包拼接？
10. **Note 存储细节**：DB 正文 vs `.md`；是否日后桥接 agentmemory？
11. **鉴权与密钥**：环境变量 / 钥匙串 / 设置页（仅本机）。
12. **IPC 形态** → 默认 **Electron 标准 IPC**（UI 发命令、core 推事件）；细节可实现期定。
13. **Monorepo 工具**：pnpm + Electron/Vite 等具体脚手架。

---

## 11. 建议的仓库形态（仅方向，未定实现）

```text
WALAgent/
  apps/desktop/          # Electron + React（渲染进程）+ TS 主进程入口
  packages/core/         # TS：Session/Agent 编排、Briefing、落盘端口（可单测）
  packages/shared/       # 跨主进程/渲染进程共享类型与契约
  docs/
  README.md
```

首期 **不包含** Go sidecar / `runtime-go/`。

当前仓库以文档为主；**以本 README 为准，未落地代码不代表已定实现。**

---

## 12. 下一步（文档阶段）

1. ~~确认编排语言~~ → **已定 TS**。
2. ~~界面与状态机分离~~ → **已定**（Electron 只画界面）。
3. 评审 [`docs/architecture/agent-orchestration.md`](docs/architecture/agent-orchestration.md) 中的 L1、Agent 生命周期、P1 单 active Agent 和持久化边界。
4. 用 faux model/tool port 先固定 phase、turn snapshot、save point、queue、abort、settlement 和 recovery 契约。
5. 继续拍板：Provider、workspace 权限与 Note 正文存储。
6. 规格稳定后再初始化 monorepo 骨架。

---

## 13. 一句话

**WALAgent 是本机桌面学习工作台：Electron/React 只画界面，Agent 状态机在 TypeScript core 中与 UI 分离；Work / Learn 上下文隔离、Briefing 可审批回流、Note 持久化；首期无远程服务端。**
