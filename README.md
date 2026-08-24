# Praxis

**Praxis**：面向学习者本人的多 Agent 编排桌面应用。用户以 Primary Agent 推进任务，按可见、可审批的上下文与能力授权召唤 Delegate Agent 分工协作。

- 技术形态：Go + Wails（桌面壳）+ React（前端），本机运行，无远程服务端
- 当前状态：Wails 工程骨架、Go core / Agent runtime / storage baseline 已初始化；orchestration、binding、provider 和前端主流程仍在实施中
- 设计文档：维护在本地 `.arccgz-harness/docs/`（gitignore，不入提交），入口为 `.arccgz-harness/docs/README.md`
