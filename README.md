# Praxis

**Praxis**：面向学习者本人的多 Agent 编排桌面应用。用户以 Primary Agent 推进任务，按可见、可审批的上下文与能力授权召唤 Delegate Agent 分工协作。

- 技术形态：Go + Wails（桌面壳）+ React（前端），本机运行，无远程服务端
- 架构模型：一个 Project 可拥有多个 Workspace 和多个 Session；所有持久化关系使用 ProjectID、WorkspaceID、SessionID 等稳定身份
- 设计文档：`.architecture/architecture.md` 是目标架构入口
