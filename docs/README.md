# 文档索引

本目录是 WALAgent 的长期文档入口。当前仓库仍处于设计阶段，文档会区分“已确认产品方向”和“尚未实现的目标架构”。

## 阅读顺序

1. 先读 [`documentation-rules.md`](documentation-rules.md)，确认文档归属和验证规则。
2. 通过下方索引进入相关事实源或草案。
3. 实施前继续补齐对应 contract 和 plan，不直接把草案当作已验证代码事实。

## 当前入口

| 文档 | 状态 | 说明 |
|---|---|---|
| [`../README.md`](../README.md) | 产品方向 | 产品定位、已确认技术方向、待定决策和阶段范围 |
| [`architecture/agent-orchestration.md`](architecture/agent-orchestration.md) | 目标架构草案 | 参考 `D:\projects\pi` 的 WALAgent Agent 编排边界、状态机、数据流、持久化和恢复策略 |

## 预留路径

| 路径 | 用途 |
|---|---|
| `docs/contracts/` | command、event、IPC、Briefing schema 和持久化 schema |
| `docs/plan/` | 分阶段实施计划和验证 gate |
| `docs/reviews/` | 绑定 commit 或不可变 artifact 的 review 证据 |

预留路径在首次创建长期文档时，必须同步登记到 `documentation-rules.md` 和本索引。
