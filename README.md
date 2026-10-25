# Praxis

**Praxis**：面向学习者本人的多 Agent 编排桌面应用。用户以 Main Agent 推进任务，通过明确的委派输入与能力授权让上下文隔离的 Subagent 分工协作。

- 技术形态：Go + Wails 3（桌面壳）+ React（前端），本机运行，无远程服务端
- 架构模型：一个 Project 可拥有多个 Workspace 和多个 Session；所有持久化关系使用 ProjectID、WorkspaceID、SessionID 等稳定身份
- 设计文档：`.architecture/architecture.md` 是目标架构入口

## 构建与验证

### 环境要求

- Go 1.26 或更高版本、Node.js 24 和 npm。
- Wails CLI 与前端 `@wailsio/runtime` 均固定为 `v3.0.0-beta.34`，该版本仍处于 Beta 阶段。
- Windows 需要 WebView2 Runtime；其他平台需安装 [Wails 3 对应的原生构建依赖](https://v3.wails.io/getting-started/installation/)。

安装匹配的 CLI，在仓库根目录执行：

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.34
```

确保 Go 的 `bin` 目录已加入 `PATH`，然后构建：

```sh
cd backend
wails3 build
```

`backend/Taskfile.yml` 依次完成依赖安装、绑定生成、前端构建、资源同步及 Go 生产构建，不启动开发服务。当前平台产物输出至 `backend/bin/`，Windows 为 `praxis.exe`。前端产物由 `frontend/scripts/sync-assets.mjs` 同步至 `backend/frontend/dist/`，再嵌入二进制。Windows 的图标、版本信息和 DPI manifest 一并编入产物；版本信息变更时需同步 `backend/build/config.yml`、`backend/build/windows/info.json` 和 manifest。

### 绑定与静态检查

修改桌面服务签名后，在 `backend/` 重新生成 TypeScript 接口和绑定，不手工修改 `frontend/bindings/`：

```sh
wails3 task bindings
go build ./...
go vet ./...
go list ./...
cd ../frontend
npm run check
```

前端 API 从生成的服务模块调用 Go；事件通过 `@wailsio/runtime` 的 `Events.On` 订阅，从事件的 `data` 读取业务载荷。桌面宿主负责系统目录对话框、业务服务就绪与退出时的资源释放。开发环境配置位于 `backend/build/config.yml`，仅在明确需要启动开发服务时使用 `wails3 task dev`。

### 人工验收与回滚

启动桌面应用后的人工验收应覆盖目录选择、项目和会话查询、模型配置保存、直接输入与 FIFO、审批、Pause/Stop、流式输出、错误提示及退出重启恢复。构建与静态检查不能代替这些桌面交互核验；验证恢复行为时使用独立的 `PRAXIS_DATA_ROOT`，不要覆盖真实用户数据。发布前保留匹配的程序与数据备份，出现异常时一并恢复。

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

每行 JSONL 只保存一个 Event。启动时一次性重放全部日志，将 Project、Workspace、Session、Main Execution 和 Entry 等记录解码到共享内存状态，之后普通查询与引用校验不再读取文件。Main 历史保留在内存仓储中，执行时据此建立本次 Runtime 与模型上下文；Subagent 的历史和执行过程只存在于本次 Runtime，不写日志，也不参与重启恢复。Agent 实例及其运行状态不作为仓储实体保存，Session 保留主 Agent 的身份和定义引用。

业务修改在内存接纳后即可继续，不等待磁盘，也不使用事务确认持久化。后台唯一写入者按接纳顺序保存记录、创建或删除会话文件；待写队列最多容纳 1024 项，不阻塞业务等待空位。队列满或文件操作失败时，将退化原因记录到 `state/praxis.log`，运行继续使用内存，但本次进程不再接纳后续落盘操作，防止日志出现序号或依赖缺口。正常关闭时尽力排空已接纳队列；不逐条 fsync，不承诺断电后数据完整。强制退出或持久化退化可能丢失新增记录、更新和删除，重启以实际存在的日志为准，已删除会话也可能再次出现。

恢复先在内存 Entry 中匹配工具调用与结果，为缺失回执补入 `tool_interrupted`，再恢复活跃 Main Execution；不恢复审批等待，不自动重跑可能已产生副作用的工具。日志路径查询是显式文件访问，返回的文件可能落后于内存，新会话尚未保存时可能不存在。

持久化只接受当前单 Event 结构，无法识别的完整日志行会明确报错，不自动迁移或修补。运行前应备份数据目录；需要回滚时同时恢复匹配的程序和数据备份。
