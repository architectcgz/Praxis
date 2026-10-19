# Praxis

**Praxis**：面向学习者本人的多 Agent 编排桌面应用。用户以 Primary Agent 推进任务，按可见、可审批的上下文与能力授权召唤 Delegate Agent 分工协作。

- 技术形态：Go + Wails（桌面壳）+ React（前端），本机运行，无远程服务端
- 架构模型：一个 Project 可拥有多个 Workspace 和多个 Session；所有持久化关系使用 ProjectID、WorkspaceID、SessionID 等稳定身份
- 设计文档：`.architecture/architecture.md` 是目标架构入口

## 本地数据目录

默认数据根目录为用户主目录下的 `.praxis`：

```text
.praxis/
├── config/                         配置与 Agent 定义
├── projects/
│   └── <projectID>/
│       └── sessions/
│           └── <sessionID>.jsonl    会话及其消息、执行记录
└── state/
    ├── projects.jsonl              Project 与 Workspace 索引日志
    ├── praxis.log                  应用运行日志
    └── writer.lock                 单进程写入锁
```

项目下的会话目录在首次有效提交时创建；启动时按项目扫描会话日志并恢复索引。同一会话的变更在一条 JSONL 记录中原子提交。
