# Praxis 架构文档

本目录描述 Praxis 的目标产品边界、领域模型、系统分层、运行时语义、上下文协议和持久化规则。所有分册共同构成一套一致的目标状态。

## 文档结构

```text
.architecture/
├── architecture.md
└── architecture/
    ├── product-direction.md
    ├── system-architecture.md
    ├── directory-structure.md
    ├── application.md
    ├── model_provider.md
    ├── model_registry.md
    ├── domain.md
    ├── structure.md
    ├── agent-orchestration-model.md
    ├── agent-runtime-model.md
    ├── agent-timeout-model.md
    ├── workflow.md
    └── storage-architecture.md
```

| 文档 | 负责内容 | 适合何时阅读 |
|---|---|---|
| [`product-direction.md`](architecture/product-direction.md) | 产品定位、Project/Session/Agent 关系、共享上下文、咨询流程和产品边界 | 理解 Praxis 的目标产品 |
| [`system-architecture.md`](architecture/system-architecture.md) | 系统分层、依赖方向、UI 与 core 的通信、工程布局和验证边界 | 判断模块应该放在哪里、可以依赖谁 |
| [`directory-structure.md`](architecture/directory-structure.md) | 源码目录、Go 包职责、应用用例文件布局和 Tx 边界 | 新增或移动包、类型与文件 |
| [`application.md`](architecture/application.md) | `internal/application` 目录、应用服务、execution loop、事务和依赖边界 | 新增或修改写入用例与 Agent 执行环 |
| [`model_provider.md`](architecture/model_provider.md) | 模型协议 adapter、请求编码、SSE 解析和网络安全边界 | 新增或修改模型 API format 与 Provider 协议实现 |
| [`model_registry.md`](architecture/model_registry.md) | 模型配置、profile、credential、解析索引和并发更新规则 | 新增或修改模型配置与选择逻辑 |
| [`domain.md`](architecture/domain.md) | `core/domain` 子包、领域文件职责、领域依赖和领域层边界 | 新增或移动领域对象、值对象和状态机 |
| [`structure.md`](architecture/structure.md) | 持久化主体关系、ManagedProcess、ownership、SessionContext、transcript 和授权对象 | 设计领域模型、命名和隔离边界 |
| [`agent-orchestration-model.md`](architecture/agent-orchestration-model.md) | 命令准入、上下文 revision、咨询、执行状态、结算和恢复 | 实现 durable command 与协作时序 |
| [`agent-runtime-model.md`](architecture/agent-runtime-model.md) | Application AgentRuntime、activation、execution loop、receipt 和取消传播 | 实现单 Agent 的运行时生命周期 |
| [`agent-timeout-model.md`](architecture/agent-timeout-model.md) | command、execution、暂停和 shutdown 的 timeout 语义 | 设计 deadline 和调用方等待行为 |
| [`workflow.md`](architecture/workflow.md) | 独立 Workflow 模块、节点实例、Agent/AgentExecution 编排、上下文提交和恢复 | 设计可复用的多 Agent 流程 |
| [`storage-architecture.md`](architecture/storage-architecture.md) | SQLite、SessionContext、per-Agent JSONL、文件存储、备份和恢复 | 设计持久化与跨存储对账 |

建议先按 `product-direction.md` → `system-architecture.md` → `directory-structure.md` → `application.md` → `domain.md` → `structure.md` 建立整体认识，再按正在修改的职责下钻到其余分册。
