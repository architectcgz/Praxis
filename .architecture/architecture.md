# Praxis 架构文档

本目录描述 Praxis 的目标产品边界、领域模型、系统分层、运行时语义、上下文协议和持久化规则。所有分册共同构成一套一致的目标状态。

## 文档结构

```text
.architecture/
├── architecture.md
├── sandbox.md
├── sandbox-implementation.md
└── architecture/
    ├── product-direction.md
    ├── system-architecture.md
    ├── structure.md
    ├── agent-orchestration-model.md
    ├── agent-runtime-model.md
    ├── agent-timeout-model.md
    ├── workflow.md
    └── storage-architecture.md
```

| 文档 | 负责内容 | 适合何时阅读 |
|---|---|---|
| [`sandbox.md`](sandbox.md) | Agent 长期安全策略、execution 安全快照、ManagedProcess、工具审批和操作系统隔离 | 设计 Agent 权限、工具执行和沙箱运行时 |
| [`sandbox-implementation.md`](sandbox-implementation.md) | 沙箱与 ManagedProcess 的代码结构、持久化、ToolBroker、Windows runtime、实施顺序和验证门禁 | 实现并验收 Agent 沙箱 |
| [`product-direction.md`](architecture/product-direction.md) | 产品定位、Project/Session/Agent 关系、共享上下文、咨询流程和产品边界 | 理解 Praxis 的目标产品 |
| [`system-architecture.md`](architecture/system-architecture.md) | 系统分层、依赖方向、UI 与 core 的通信、工程布局和验证边界 | 判断模块应该放在哪里、可以依赖谁 |
| [`structure.md`](architecture/structure.md) | 持久化主体关系、ManagedProcess、ownership、SessionContext、transcript 和授权对象 | 设计领域模型、命名和隔离边界 |
| [`agent-orchestration-model.md`](architecture/agent-orchestration-model.md) | 命令准入、上下文 revision、咨询、执行状态、结算和恢复 | 实现 durable command 与协作时序 |
| [`agent-runtime-model.md`](architecture/agent-runtime-model.md) | AgentRuntime actor、activation、execution loop、快照和取消传播 | 实现单 Agent 的执行环 |
| [`agent-timeout-model.md`](architecture/agent-timeout-model.md) | command、execution、暂停和 shutdown 的 timeout 语义 | 设计 deadline 和调用方等待行为 |
| [`workflow.md`](architecture/workflow.md) | 独立 Workflow 模块、节点实例、Agent/AgentExecution 编排、上下文提交和恢复 | 设计可复用的多 Agent 流程 |
| [`storage-architecture.md`](architecture/storage-architecture.md) | SQLite、SessionContext、per-Agent JSONL、文件存储、备份和恢复 | 设计持久化与跨存储对账 |

建议先按 `product-direction.md` → `system-architecture.md` → `structure.md` 建立整体认识，再按正在修改的职责下钻到其余分册。
