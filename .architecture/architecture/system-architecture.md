# Praxis 系统架构

> 状态：规范性目标架构。本文描述系统分层、依赖方向、进程内通信、工程布局和验证边界。
> 领域身份见 [`structure.md`](structure.md)，持久化分工见 [`storage-architecture.md`](storage-architecture.md)，目录总入口见 [`../architecture.md`](../architecture.md)。

## 1. 系统分层

```text
frontend/ React UI
    │ Wails binding / event
    ▼
backend/app/ + internal/contracts/
    ├── 手动命令 ──────────────────────────┐
    └── Workflow 命令                      │
            ▼                              │
        internal/workflow/                 │
            │ 核心命令 / 查询              │
            ▼                              ▼
        backend/internal/core/
            │ core 定义的接口
            ▼
        agentruntime / managedprocess / storage / providers / tools / sandbox
            │
            ├── 本机 DataRoot
            └── 模型厂商 API

internal/compose/ 负责组装全部模块和适配器。
```

## 2. 依赖方向

依赖只能由外向内。`internal/core/domain` 是最内层，只允许 import 标准库。

| 禁止关系 | 原因 |
|---|---|
| `internal/core/**` 与 `internal/agentruntime/**` import Wails、SQLite driver、Provider SDK | core 与 runtime 必须能脱离桌面壳、数据库和厂商 SDK 单测 |
| `internal/core/**` 与 `internal/agentruntime/**` import `app`、`storage`、`providers`、`tools`、`contracts`、`compose` | 内层只依赖自己定义的接口 |
| Provider SDK 出现在 `internal/providers/` 之外 | 模型协议细节不得泄漏进领域 |
| SQLite driver 出现在 `internal/storage/` 之外 | 存储实现细节不得泄漏进领域 |
| `internal/managedprocess`、`internal/providers`、`internal/storage`、`internal/tools`、`internal/sandbox` import `internal/agentruntime` | 同层适配器只认 `internal/core/runtime` 的接口 |
| `internal/compose` 被 core 反向 import | compose 是组合根，只能被 `main` 使用 |
| `internal/core/**` import `internal/workflow` | Workflow 是调用核心命令的独立上层模块，核心领域不能依赖具体流程 |

## 3. 通信方向

```text
用户操作（手动 Agent 命令 / Workflow 命令）
        │
        ▼
React UI ──binding──► app 适配层
   ▲                    ├──手动命令──────────────► AgentOrchestrator
   │                    └──Workflow 命令──► WorkflowCoordinator
   │                                              │
   │                                              └──核心命令──► AgentOrchestrator
   └──────────── event / 快照投影 ◄──────────────────────────────┘
```

1. 手动 Agent 命令直接进入 `AgentOrchestrator`；Workflow 命令先改变 Workflow 状态，再由 `WorkflowCoordinator` 调用同一套核心命令。
2. `AgentOrchestrator` 是核心领域的唯一写入口；`WorkflowCoordinator` 是 Workflow 状态的唯一写入口。
3. 事件与快照是可丢失的投影。断线重连和 Workflow 恢复必须能通过查询重建状态，不依赖事件连续性。
4. 更换 UI 框架或增加 CLI 只需接入同一套命令与事件，不影响核心领域或 Workflow 模块。

## 4. 前端分层

| 目录 | 职责 |
|---|---|
| `frontend/src/app/` | 应用壳、路由骨架、错误边界 |
| `frontend/src/features/<domain>/` | 按产品域组织的视图、局部状态与交互 hook |
| `frontend/src/api/` | 唯一 binding 出入口；DTO 转换集中在此，功能模块不直接触碰 wailsjs |
| `frontend/src/styles/` | 全局 token 与分片样式 |

样式冲突通过组件范围、选择器层级、入口顺序或 CSS 自定义属性解决；`prefers-reduced-motion` 通过可继承的 motion token 实现。禁止使用 `!important` 提升优先级。

## 5. 工程布局

```text
Praxis/
├── backend/
│   ├── main.go
│   ├── cmd/
│   │   └── sandboxworker/     独立 Windows 沙箱 worker 入口
│   ├── app/                    Wails 领域 binding、event 适配、桌面生命周期
│   ├── internal/
│   │   ├── contracts/          binding 命令、事件与快照 DTO
│   │   ├── core/
│   │   │   ├── domain/         领域模型与状态机（仅标准库）
│   │   │   ├── orchestrate/    命令准入、事务编排、ToolBroker、调度、投递、恢复
│   │   │   ├── persistence/    持久化接口与事务边界
│   │   │   ├── runtime/        单 Agent 运行时接口
│   │   │   ├── session/        Agent session store 接口
│   │   │   └── system/         clock / id / 生命周期
│   │   ├── agentruntime/       单 Agent 运行时适配器
│   │   │   ├── execution/      单次 execution 的 phase、snapshot、model/tool loop、save point
│   │   │   ├── transcript/     runtime 侧 transcript event、receipt 与 flush 时序
│   │   │   └── runtimetest/    runtime 接口的可复用 fake 与测试支撑
│   │   ├── managedprocess/     Session 长期进程、ConPTY 挂载和进程生命周期
│   │   ├── workflow/           Workflow 定义、实例、节点编排与恢复
│   │   ├── storage/
│   │   │   ├── dataroot/       应用数据根、初始化与备份
│   │   │   ├── sqlite/         编排事实与事务 repository
│   │   │   ├── agentlog/       per-Agent JSONL transcript 适配器
│   │   │   └── blob/           附件、Note 与大对象文件适配器
│   │   ├── providers/
│   │   │   ├── anthropic/      Anthropic 模型协议适配器
│   │   │   ├── openaicompat/   OpenAI-compatible 模型协议适配器
│   │   │   └── registry/       Provider 与 model 注册解析
│   │   ├── tools/              工具 schema、参数规范化和执行适配器
│   │   ├── sandbox/            worker IPC、沙箱进程生命周期和 Windows 隔离
│   │   ├── compose/            组合根：装配具体适配器
│   │   └── logging/
│   ├── wails.json
│   └── go.mod
├── frontend/
│   └── src/
│       ├── app/                React 应用壳与错误边界
│       ├── features/           按产品域组织的页面、组件与交互
│       ├── shared/             binding API 与跨域基础设施
│       └── styles/             全局 token 与共享样式
├── scripts/                    仓库构建与测试门禁
└── .architecture/              架构、契约与实现计划文档
```

所有后端源码与工程文件位于 `backend/`；`frontend/` 是唯一位于 `backend/` 外的应用源码目录；根级脚本是仓库工具，不是后端源码。`compose` 是唯一允许同时看到内外层的包。

### 5.1 桌面适配层内部边界

```text
app/
├── app.go                  桌面生命周期宿主与 Wails binding 集合
├── services.go             各领域 binding 依赖的窄接口与显式装配参数
├── binding.go              共享 context、readiness、日志和调用跟踪
├── bindings_system.go      健康状态与桌面连通性
├── bindings_project.go     项目目录发现与创建
├── bindings_session.go     Session 列表、创建与投影
├── bindings_agent.go       Agent 投影、消息与历史
├── bindings_command.go     Agent 输入、恢复、控制与排队命令
├── bindings_process.go     ManagedProcess 查询、终端挂载、输入和停止
├── bindings_workflow.go    Workflow 定义、实例和控制命令
├── bindings_model.go       模型能力目录与配置编辑
├── events.go               Wails 事件发布
├── lifecycle.go            启动装配、DataRoot 与有界关闭
└── errors.go               内部错误到公开错误码的映射
```

每个 `*Bindings` 类型是独立的 Wails 暴露对象，只依赖自身领域的窄接口。`App` 维护共享桌面生命周期并返回完整 binding 集合。项目、Session、Agent、ManagedProcess、命令和模型能力分别通过对应领域接口装配。

`Dependencies` 是组合根使用的显式装配值，不是业务接口。`compose.Application` 可以同时满足多个窄接口，但各 binding 只接收并保存自己声明的能力。前端只能通过 `frontend/src/api/` 访问这些 Wails 对象，功能模块不直接依赖生成的类型名。

### 5.2 `agentruntime` 内部边界

```text
agentruntime ─► internal/core/{domain,runtime,session}
compose      ─► agentruntime + storage + providers
```

| 模块 | 唯一职责 | 明确不负责 |
|---|---|---|
| 根包 `agentruntime` | 按 Agent 串行化 activation、cancel 与 close；持有当前 execution；协调 transcript receipt 和产品回调 | 创建 durable execution、决定产品状态、调用具体存储或 Provider SDK |
| `internal/core/runtime` | 定义 model、tool、execution lifecycle 和不可变 turn 数据结构；提供防御性快照 | 实现具体 Provider、工具副作用或 JSONL 文件格式 |
| `runtimetest` | 提供 runtime 测试所需的公开边界和替身 | 被生产组合根依赖 |

#### 5.2.1 文件布局

```text
agentruntime/
├── doc.go                  包职责、依赖约束和并发模型
├── target_runtime.go       长期 runtime、activation、cancel、close 与 settlement
├── types.go                core runtime 类型别名和 UI observation 事件
└── errors.go               runtime 错误码
internal/core/runtime/
├── doc.go                  core-owned runtime boundary
├── execution_lifecycle.go  execution receipt 与产品状态回调
├── model.go                provider-neutral model stream 与 turn snapshot
├── tool.go                 provider-neutral tool execution contract
└── copy.go                 runtime 输入的防御性快照
```

文件职责遵循以下约束：

1. `target_runtime.go` 是 durable AgentExecution 进入 runtime 的唯一入口；它只接受 core 已创建的 execution。
2. runtime 固定执行“追加 `execution_settled` → flush → 产品 settlement callback → 释放资源”的顺序。
3. model、tool、execution lifecycle 和快照类型归 `internal/core/runtime`，不依赖具体 Provider 或存储。
4. JSONL 的具体格式和 receipt 查询归 `internal/core/session` 与 `internal/storage/agentlog`，runtime 只使用其接口。
5. 领域类型与接口类型归属 `internal/core`；`agentruntime` 只保存生命周期实现状态和适配器别名。

根包 `agentruntime` 是 `internal/core/runtime` 接口的实现边界。它不向 app、compose 或其他外层适配器暴露业务入口以外的状态写入能力，只依赖 core 定义的领域对象、runtime 接口和 session 接口，不得 import `storage`、`providers`、`tools` 或 `compose`。

### 5.3 Go 包命名

Go 包路径段使用简短、全小写、无下划线的名称；多词领域概念直接使用小写复合词。运行时包命名为 `agentruntime`，core runtime 契约包命名为 `runtime`；下划线只用于 Go 文件名中的语义分隔。

## 6. 验证边界

1. 依赖边界由 review 保证，不引入机械架构守卫脚本；`scripts/` 只承载构建与测试门禁。
2. 完整门禁为 `go vet`、`go test -race`、`lll` 行长检查、前端 `tsc --noEmit` 和 `npm run build`。
3. 行长超限必须靠重组代码解决，不得抬高上限或抑制告警。
4. 源码与脚本注释使用英文，解释业务规则、不变量、生命周期边界、副作用与非显然取舍，不复述语法。

## 7. 系统不变量

1. UI 是 core 状态的投影，不拥有权威状态，也不能绕过命令准入。
2. 依赖方向由外向内；core 与 agentruntime 不认识 Wails、SQLite driver、Provider SDK 或外层适配器。
3. Provider SDK 只负责模型协议，不拥有产品状态机、durability 或 recovery。
4. 组合与实现替换发生在 `compose`，领域语义不依赖具体适配器。
