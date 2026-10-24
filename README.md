# Praxis

**Praxis**：面向学习者本人的多 Agent 编排桌面应用。用户以 Main Agent 推进任务，通过明确的委派输入与能力授权让上下文隔离的 Subagent 分工协作。

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
    └── praxis.log                  应用运行日志
```

同一用户只允许运行一个 Praxis 实例。应用入口在启动 Wails、初始化数据目录和恢复业务之前获取独占运行权，获取失败直接退出。锁文件位于 `os.UserCacheDir()/praxis/instance.lock`，不随 `PRAXIS_DATA_ROOT` 改变；应用关闭后释放锁，但不删除锁文件。

项目下的会话目录在首次有效写入时创建；每行 JSONL 只保存一个 Event。每个 Agent 按 AgentID 保存唯一消息历史，Session 对话直接使用 Main Agent 的历史与执行上下文，不维护第二份 Context；Subagent 使用自己的隔离历史。消息先追加到所属 Agent 的内存 History，再按顺序持久化；失败保留待写记录供重试，其他实体在 fsync 成功后更新内存。启动时按项目 Replay 日志，历史查询直接读取内存 Entry 列表，本次执行从所属 Agent 的有效历史构建模型上下文，执行结束后不在存储层保留该对象；多步骤业务保留已写入事实，并通过固定请求身份和执行状态继续恢复。

持久化只接受当前单 Event 结构，无法识别的完整日志行会明确报错，不自动迁移或修补。运行前应备份数据目录；需要回滚时同时恢复匹配的程序和数据备份。
