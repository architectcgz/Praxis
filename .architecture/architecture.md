# Praxis 架构文档

本目录描述 Praxis 的目标产品边界、领域模型、系统分层、运行时语义、上下文协议和持久化规则。所有分册共同构成一套一致的目标状态。

## 组织规则

- `architecture/` 根目录只保存跨模块规范。
- 每个模块使用独立目录，并以 `README.md` 作为模块入口。
- 模块内的细节文档继续按源码职责下钻，不在根目录建立同名平行文档。
- 文档只描述目标状态；实现阶段计划单独保存在 `improvements/`，不作为架构规范引用。

## 文档结构

```text
.architecture/
├── architecture.md
├── architecture/
│   ├── product-direction.md
│   ├── system-architecture.md
│   ├── directory-structure.md
│   ├── naming-conventions.md
│   ├── application/
│   │   ├── README.md
│   │   ├── agent_runtime/
│   │   │   ├── README.md
│   │   │   ├── loop.md
│   │   │   ├── model_request.md
│   │   │   └── stream.md
│   │   └── execution/
│   │       └── tool_invocation.md
│   ├── domain/
│   │   ├── README.md
│   │   ├── structure.md
│   │   └── execution/
│   │       └── tool_invocation.md
│   ├── orchestration/
│   │   ├── README.md
│   │   └── timeout.md
│   ├── workflow/
│   │   └── README.md
│   ├── storage/
│   │   └── README.md
│   ├── model_provider/
│   │   ├── README.md
│   │   ├── provider.md
│   │   ├── request.md
│   │   ├── stream.md
│   │   └── http.md
│   └── model_registry/
│       └── README.md
└── improvements/
    ├── framework-migration-plan.md
    └── unimplemented-capabilities-plan.md
```

## 跨模块规范

| 文档 | 负责内容 | 适合何时阅读 |
|---|---|---|
| [`product-direction.md`](architecture/product-direction.md) | 产品定位、Project/Session/Agent 关系、共享上下文、咨询流程和产品边界 | 理解 Praxis 的目标产品 |
| [`system-architecture.md`](architecture/system-architecture.md) | 系统分层、依赖方向、UI 与 core 的通信、工程布局和验证边界 | 判断模块应该放在哪里、可以依赖谁 |
| [`directory-structure.md`](architecture/directory-structure.md) | 源码目录、Go 包职责、应用用例文件布局和 Tx 边界 | 新增或移动包、类型与文件 |
| [`naming-conventions.md`](architecture/naming-conventions.md) | Go、TypeScript、Wails contract、领域对象、应用用例、前端组件和文件的统一命名规则 | 新增或修改公开类型、组件、方法与文件名称 |

## 模块设计

| 模块入口 | 负责内容 | 细节文档 |
|---|---|---|
| [`application/`](architecture/application/README.md) | 产品写入用例、AgentRuntime、事务与副作用边界 | [AgentRuntime](architecture/application/agent_runtime/README.md)、[ToolInvocation 用例](architecture/application/execution/tool_invocation.md) |
| [`domain/`](architecture/domain/README.md) | `domain` 子包、领域对象、值对象、状态机和 ownership | [领域结构](architecture/domain/structure.md)、[ToolInvocation 模型](architecture/domain/execution/tool_invocation.md) |
| [`orchestration/`](architecture/orchestration/README.md) | durable command、上下文 revision、执行结算和恢复 | [超时模型](architecture/orchestration/timeout.md) |
| [`workflow/`](architecture/workflow/README.md) | Workflow 定义、实例、节点编排、上下文提交和恢复 | 模块入口包含完整设计 |
| [`storage/`](architecture/storage/README.md) | SQLite、SessionContext、per-Agent JSONL、文件存储和恢复 | 模块入口包含完整设计 |
| [`model_provider/`](architecture/model_provider/README.md) | 模型 API format、请求编码、流解析、HTTP 与 adapter 生命周期 | [adapter](architecture/model_provider/provider.md)、[request](architecture/model_provider/request.md)、[stream](architecture/model_provider/stream.md)、[HTTP](architecture/model_provider/http.md) |
| [`model_registry/`](architecture/model_registry/README.md) | Provider 配置聚合、Group、credential、模型目录和运行时解析 | 模块入口包含完整设计 |

建议先按 `product-direction.md` → `system-architecture.md` → `directory-structure.md` → `naming-conventions.md` 建立整体认识，再进入当前职责所属的模块目录。跨模块行为以 ownership 所在模块为准，其他文档只引用该定义，不建立第二套规则。
