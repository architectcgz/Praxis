# Praxis 后端目录结构约定

> **状态：已确认的后端目标目录结构与依赖边界约定，作为后续重构与新增模块的依据。**
> 本文仅描述目标状态，不表示现有代码已全部满足。核心业务语义以 [architecture.md](architecture.md) 为准，通用编码守则参见 [../code-style.md](../code-style.md)。

---

## 一、设计原则

后端目录按**产品功能模块与真实职责边界**组织，使用直白的业务与技术名称，让初次阅读代码的人能够直接按功能定位模块，同时通过清晰的依赖方向避免跨层交叉与循环依赖。

- **同名功能内聚在一起**：同一模块的接口契约与具体实现放在同一目录下，不按抽象架构层将单个功能横向割裂。
- **主干执行链路集中呈现**：Agent 任务调度、会话、执行、迭代、模型调用、审批与工具执行收拢在 `engine/` 下，便于顺着调用链阅读。
- **共享概念按边界独立成包**：具备独立算法、编解码规则或私有封装不变量的概念独立成包，利用 Go 包级可见性保护内部状态；避免将无关业务实体混入单一通用包，也不为零星辅助类型创建碎片微包。
- **单向依赖、消费方定义端口**：共享基础包与外围能力模块绝不反向依赖执行引擎；持久化端口由消费方按需声明最小接口，由装配层统一接线。

---

## 二、目标目录结构

以下路径以 `backend/` 为根：

```text
backend/
├── main.go                     # 程序入口
├── instance_unix.go            # Unix 单实例互斥
├── instance_windows.go         # Windows 单实例互斥
├── wails/                      # 桌面端宿主与前端协议边界
│   ├── app.go                  # 应用生命周期与事件桥接
│   ├── bindings/               # RPC 入口与前端 DTO 投影
│   ├── error/                  # 公开错误映射
│   └── validation/             # 外部输入边界校验
└── internal/
    ├── bootstrap/              # 启动与装配根：加载配置、打开存储、接线启动
    ├── service/                # 对外业务用例入口（供 wails/bindings 调用）
    │   ├── project/            # 项目创建与查询用例
    │   └── session/            # 会话管理、配置与执行控制用例
    │
    ├── engine/                 # Agent 核心执行引擎（任务调度到执行收尾的主干链路）
    │   ├── coordinator/        # 全局调度器：会话装卸、任务队列、主/子 Run 并发管理
    │   ├── session/            # 会话对象：SessionInfo、配置快照、活跃上下文窗口与历史绑定
    │   ├── run/                # 单次 Run 的准备、执行、结算与主/子代理协作
    │   ├── loop/               # ReAct 多轮迭代、工具批次屏障与上下文压缩编排
    │   ├── modelcall/          # 统一模型调用、重试、流收集与 ModelRequest 持久化
    │   ├── approval/           # 授权模式判定、AI 风险审核与人工审批等待
    │   ├── toolexecution/      # 工具参数解码、路径解析、调用执行与单调用取消
    │   └── hook/               # 执行期扩展 Hook 分发与生命周期契约
    │
    ├── provider/               # 大模型厂商协议（接口契约与各家实现统一归属此处）
    │   ├── provider.go         # Provider 接口、Request、统一流事件与 StopReason
    │   ├── router/             # 协议路由：按 APIFormat 分发至具体厂商适配器并复用代理客户端
    │   ├── anthropic/          # Anthropic Messages 协议适配器
    │   ├── openai_chat/        # OpenAI Chat Completions 协议适配器
    │   ├── openai_responses/   # OpenAI Responses 协议适配器
    │   ├── streaming/          # SSE 解析、流消费与用量归一化
    │   └── httpclient/         # HTTP 传输与代理配置
    │
    ├── tools/                  # 工具协议、注册目录、全部内置工具与输出边界
    │   ├── tool.go             # Tool 接口、ToolDefinition、ToolResult 与运行环境
    │   ├── catalog.go          # 只读工具目录与受限子集裁剪
    │   ├── registry.go         # 工具注册表
    │   ├── function.go         # 声明式工具定义与类型化参数绑定
    │   ├── list.go             # 本次 Run 的工具发现与按需加载
    │   ├── subagent.go         # 主/子代理协作工具
    │   ├── bash.go             # 命令执行
    │   ├── read_file.go        # 分段读取文件
    │   ├── apply_patch*.go     # 补丁参数校验、解析与应用
    │   └── output_*.go         # 工具输出转存和会话临时目录
    │
    ├── storage/                # 数据持久化实现
    │   ├── store.go            # SQLite 与日志目录生命周期管理
    │   ├── project.go          # 项目元数据持久化与本地目录准备
    │   ├── session.go          # 会话元数据持久化
    │   ├── load.go             # 会话日志加载与异常关闭恢复
    │   └── jsonl/              # 追加日志文件读写原语
    │
    ├── config/                 # 本地数据目录与配置文件加载
    │   ├── dataroot.go         # 数据根目录初始化与路径布局
    │   ├── settings.go         # 全局审批配置加载
    │   ├── models.go           # 模型目录与凭据配置加载
    │   ├── roles.go            # Agent 角色定义（ROLE.md）加载
    │   └── tools.go            # 工具启停配置加载
    │
    ├── event/                  # 进程内实时事件通知
    │   ├── emitter.go          # 启动时绑定接收方，同步转发通知
    │   └── event.go            # EventSource 与封闭的事件 Payload 集合
    │
    ├── model/                  # 大模型领域概念（专指 LLM 模型）
    ├── entry/                  # 历史事实条目、内容块、编解码与回执补齐
    ├── timing/                 # 操作耗时测量与只读快照
    ├── context/                # 模型上下文选择、token 预算校准与摘要压缩算法
    ├── agent/                  # Agent 角色、定义与状态值类型
    ├── task/                   # 待执行任务 Task 与内存 FIFO 队列 TaskQueue
    ├── project/                # Project 元数据模型与完整性校验
    ├── security/               # 审批模式 ApprovalMode、授权决定与风险评估结论
    ├── identity/               # 强类型领域标识
    ├── errorcode/              # 统一结构化错误码与哨兵错误
    ├── pathutil/               # 跨平台路径规范化与目录范围判断
    └── logging/                # 应用运行日志
```

---

## 三、模块职责与分包边界

### 1. 大模型概念包 `internal/model/`
- `model/` 严格限定为**大模型（LLM）领域**，只包含 `APIFormat`、`Choice`、`Option`、`CatalogEntry`、`Config`、`Catalog`、`ReasoningState`、`Usage`、`UsageTotals` 与 `ModelThinking`。
- `model.Choice`（`ProviderID`、`ModelID`、`ReasoningLevel`）表达指向 `model.Catalog` 的模型与推理等级选择坐标，由 `model.Catalog`、`engine/session`、`event`、`storage`、`service/session` 及 `wails/bindings` 共用；“空选择继承默认模型”的规则归属 `engine/session`。
- 不将 `Project`、`Task`、`Entry`、`ApprovalMode` 等非大模型概念放入 `model/`，避免术语歧义与包内封装边界退化。

### 2. 历史事实 `internal/entry/` 与操作计时 `internal/timing/`
- `entry/` 承载持久化事实 `Entry`、封闭 `Payload` 集合、封闭 `ContentBlock` 集合、`ModelRequest` 和工具调用配对补齐逻辑，不持有运行期句柄、锁或存储 I/O。
- `timing/` 仅依赖标准库，提供只读耗时快照 `Timing` 和运行期计时器 `Span`。模型调用、工具执行和上下文压缩负责计时边界；历史条目和事件只携带快照。

### 3. 核心执行引擎 `internal/engine/`
- 按并发所有权与生命周期划分子包，每个包维护清晰的锁边界与内部不变量：
  - `engine/coordinator`：管理已加载会话索引、按会话隔离的任务队列与活动 Run 控制句柄，显式持有 `event.Emitter` 发布调度与配置变更事件。
  - `engine/session`：承载会话元数据 `SessionInfo`、历史分页 `HistoryPage` 及运行期 `Session`（只读 `Configuration`、`AgentContext` 活跃窗口与 `AgentHistory` 绑定）。
  - `engine/run`：管理单次 Run 的生命周期、进度统计、终态提交与父子代理协作。
  - `engine/loop`：推进 ReAct 多轮迭代、只读并发/写串行工具屏障与 Turn 间压缩编排。
  - `engine/modelcall`：封装单次逻辑模型请求的容量检查、重试（最多 4 次尝试）、流消费、`ModelRequest` 落盘与事件通知。
  - `engine/approval`：维护等待人工审批的活跃调用表，执行审批模式判定与模型风险审核。
  - `engine/toolexecution`：负责工具参数严格解码、项目路径规范化、调用执行、输出上限控制与单调用取消。
  - `engine/hook`：提供执行期扩展 Hook 的注册、分发与操作作用域跟踪。

### 4. 外围能力模块 `provider/`、`tools/`、`config/`、`storage/`
- `provider/`：聚合中立的 `Provider` 接口、统一流事件与各厂商协议适配器；只负责单次请求编解码与流资源释放，不承担重试、压缩或 Run 状态管理。
- `tools/`：聚合 `Tool` 契约、目录注册表与内置工具；每项工具就近组织 JSON Schema、参数校验与执行实现。协作工具通过装配时传入的操作调用执行引擎，不反向依赖引擎包。
- `config/`：集中负责本地数据目录布局与各类用户可编辑配置文件的边界校验和加载。
- `storage/`：集中负责 SQLite 元数据读写、JSONL 追加日志与会话加载恢复，实现消费方声明的存储接口。

---

## 四、依赖方向红线

所有包依赖必须形成无环单向图，严格遵守以下约束：

```text
1. 基础概念包（identity / errorcode / pathutil / logging / model / entry / timing / context / agent / task / project / security）
   --> 仅依赖标准库与更底层基础包；严禁依赖 engine/*、event、provider、tools（仅 entry 依赖工具标识类型）、config、storage、service、wails

2. 外围能力包（provider / tools / config）与实时事件包（event）
   --> event 仅依赖基础概念包；外围能力包可依赖基础概念包及各自内部子包
   --> provider 可消费 tools 的工具协议；config 可消费 tools 的注册目录及 provider/httpclient 的代理配置能力
   --> 均严禁依赖 engine/*、storage、service、wails；外围能力包不得依赖 event

3. 执行引擎内部（engine/*）高层只依赖低层，从低到高依次为：
   hook -> session / toolexecution / modelcall -> approval / loop -> run -> coordinator
   （engine/* 不直接依赖 storage/、config/、service/、wails/）

4. 数据存储（storage）
   --> 依赖基础概念包与 engine/session（用于会话加载恢复），隐式实现消费方存储接口

5. 应用服务（service/*）
   --> 依赖基础概念包、engine/coordinator、engine/session 及自身声明的存储接口
   --> service/session 仅在公开服务类型中引用 engine/approval 和 engine/toolexecution 的值类型

6. 桌面协议边界（wails/*）
   --> 仅通过 service/* 调用业务能力，经启动时绑定的回调接收事件并推送前端通知，消费必要基础概念包与分页/元数据视图；
       严禁直接依赖 engine/coordinator、engine/run、engine/loop、engine/modelcall、engine/approval、engine/toolexecution、storage/、config/、provider/

7. 启动装配（bootstrap）
   --> 唯一全知装配层，负责实例化并连接 config、storage、provider、tools、event、engine/*、service/* 与 wails/bindings
```

### 关键防交叉守则
- **`event/` 严禁导入任何 `engine/*` 包**：`event/` 被整个执行引擎普遍依赖，事件载荷只使用基础概念包中的值对象与只读事实类型（例如 `SessionConfigReloaded` 使用 `model.Choice` 与 `security.ApprovalMode`），杜绝通过事件层产生向上依赖或循环依赖。
- **存储端口归消费方声明**：`engine/coordinator`、`service/project`、`service/session` 按各自真实使用的方法声明最小存储接口，`storage/` 提供实现并由 `bootstrap/` 注入，避免在引擎外部维护与核心双向互指的集中仓库接口包。
- **`event.Emitter` 显式注入**：需要发布事件的引擎组件（包括 `engine/coordinator`）均通过自身依赖结构体显式持有 `*event.Emitter`，不穿透其他组件字段借道发送。接收方在启动时绑定，发布过程中不维护订阅列表。
