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

启动桌面应用后的人工验收应覆盖目录选择、项目和会话查询、模型配置保存、直接输入与 FIFO、审批、Pause/Stop、流式输出、错误提示及退出重启恢复。构建与静态检查不能代替这些桌面交互核验；验证恢复行为时使用独立的操作系统用户或虚拟机，不要覆盖真实用户数据。发布前保留匹配的程序与数据备份，出现异常时一并恢复。

## 本地数据目录

数据根目录固定为当前用户主目录下的 `.praxis`，不提供环境变量或应用配置覆盖入口：

```text
.praxis/
├── config/                         配置与 Agent 定义
├── projects/
│   └── <projectID>/
│       └── sessions/
│           └── <sessionID>.jsonl    消息、执行事实及 Context 保存结果
├── sessions.db                     SQLite 会话元数据
├── projects.jsonl                  Project 与 Workspace 索引日志
├── praxis.log                      应用运行日志
└── incomplete-tail-*               恢复时备份的不完整日志尾行（按需生成）
```

同一用户只允许运行一个 Praxis 实例。应用入口在启动 Wails、初始化数据目录和恢复业务之前获取独占运行权，获取失败直接退出。锁文件位于 `os.UserCacheDir()/praxis/instance.lock`；应用关闭后释放锁，但不删除锁文件。

会话元数据以 SQLite 为唯一来源，保存 SessionID、ProjectID、WorkspaceID、标题、状态、会话审批模式及时间戳；列表、标题搜索、重命名和审批模式读写不读取历史 JSONL。SessionID 与 ProjectID 共同确定会话日志路径，不保存绝对路径。主会话入口的 AgentID 由 SessionID 派生，定义固定为 `primary`，会话不保存独立的主 Agent 身份或定义引用。

审批模式只分全局与会话两级：全局模式保存在 `config/settings.json`，会话模式保存在 SQLite 的 `sessions.approval_mode`；值为 `NULL` 时继承当前全局模式。输入区的审批选择只影响当前会话，可选 `ask`（询问）、`auto`（模型审核）、`yolo`（跳过审核）或继承全局。每次工具调用开始授权时确定有效模式，之后修改配置不改变该次授权或已有人工等待；明确的 `deny` 规则始终优先。

每行 JSONL 只保存一个 Event。启动只重放项目与工作区汇总日志并打开会话数据库，不扫描会话历史，也不恢复旧 Execution。打开指定会话时才重放其 JSONL、校验历史并恢复 SessionContext；成功后复用内存状态，直到会话删除或 Store 关闭。单个会话日志损坏只使该会话访问失败，不阻止应用启动或查询其他会话。Session 统一管理原始历史及持续维护的 Context；普通条目增量追加，压缩结果确认后直接替换 Context 的条目列表。每轮请求只读取当前 Context，Run 只保留本次执行证据。完整压缩结果以 `context.saved` 保存，原始历史不被改写。

Agent 实例由各次 Execution 在创建 Run 时创建并持有，执行结束后随 Run 释放，Session 不维护 Agent 注册表。查询空闲主会话时只从元数据与执行事实投影展示，不创建运行实例。Subagent 的 Agent、历史和执行过程只存在于所属 Run，不写日志，也不参与重启恢复。

Loop 在工具返回后立即保存实际回执，再执行后置 Hook。Hook 可以修改正文，成功后更新同一历史条目，并同步后续模型使用的 Context 和本次 Run；失败则记录错误、保留实际结果并继续 Loop。正文修改以 `entry.updated` 记录，不改变条目身份、原始序号、创建时间或错误标记；历史保存失败会终止 Loop，不重新执行工具。

`storage` 统一管理 SQLite 会话元数据、事件编解码、日志布局、顺序追加和按需重放。`Store` 持有数据库连接、已载入历史、关闭状态和各日志的写入错误状态，使用同一把读写锁协调访问，并在关闭时释放数据库连接。JSONL 写入成功后才发布内存状态；追加 I/O 失败阻断该日志后续追加，须重新打开存储并重放确认。编码失败或写入前取消不阻断日志。不逐条 fsync，不承诺断电后的磁盘同步；不支持运行期间从外部修改文件。重放只接受完整日志行，不完整尾行先备份再截断，完整坏行明确报错。

创建会话先建立空 JSONL，再提交 SQLite 元数据；提交失败允许留下未被引用的空文件。删除由 Coordinator 确认没有活动执行后进行，以 SQLite 删除为提交点：数据库删除失败直接返回错误，不触碰 JSONL；数据库删除成功后释放会话内存并尽力删除 JSONL，清理失败允许留下孤儿文件，不回滚数据库删除。会话目录只由 SQLite 查询，不扫描、收养或重放未被引用的 JSONL。

会话首次重放时匹配工具调用与回执，读完后按调用顺序为缺失回执追加 `tool_interrupted`，落盘成功后才更新内存；重复调用、重复回执、回执缺少声明或归属冲突直接报错，修复失败则本次会话访问失败，不发布半成品历史。SessionContext 从最近保存的完整压缩结果及之后已准入的普通 Entry 初始化，包含补写的中断回执，并同步仍保留回执的后续正文更新；不重复追加或恢复已压缩掉的回执，不逐条重放摘要替代关系。未进入保存结果的摘要不参与恢复。首次初始化 Context 时将旧进程留下的未结束执行保存为 `interrupted`，不创建它们的 Run，不请求模型、不继续执行工具，也不恢复审批等待。新的主 Execution 只由新的用户输入触发。

持久化只接受当前单 Event 结构，无法识别的完整日志行会明确报错，不自动迁移或修补。运行前应备份数据目录；需要回滚时同时恢复匹配的程序和数据备份。
