# Praxis

**Praxis**：面向学习者本人的多 Agent 编排桌面应用。用户以 Main Agent 推进任务，通过明确的委派输入与能力授权让上下文隔离的 Subagent 分工协作。

- 技术形态：Go + Wails 3（桌面壳）+ React（前端），本机运行，无远程服务端
- 架构模型：一个 Project 对应一个执行目录，可拥有多个 Session；会话通过 ProjectID 关联项目，工具执行与文件预览以项目目录为边界
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
│       ├── project.json            项目身份、名称与目录配置
│       └── sessions/
│           └── <sessionID>.jsonl    消息、执行事实及 Context 保存结果
├── sessions.db                     SQLite 会话元数据
├── praxis.log                      应用运行日志
└── incomplete-tail-*               恢复时备份的不完整日志尾行（按需生成）
```

同一用户只允许运行一个 Praxis 实例。应用入口在启动 Wails、初始化数据目录和恢复业务之前获取独占运行权，获取失败直接退出。锁文件位于 `os.UserCacheDir()/praxis/instance.lock`；应用关闭后释放锁，但不删除锁文件。

项目配置以 `projects/<projectID>/project.json` 为唯一来源，直接保存项目身份、名称、目录、状态和时间戳。查询直接读取文件，不维护长期内存副本，不加应用层锁，也不依赖数据库关闭状态；项目列表扫描配置目录，按更新时间倒序、项目 ID 升序排列。保存时通过同目录临时文件写入、同步并完整替换目标文件，失败时返回错误，不发布部分内容。配置缺失的目录不列入项目列表，查询遇到损坏配置明确报错。项目创建由项目服务串行处理，检查项目身份与目录冲突。

例如，Windows 上的 `projects/demo/project.json` 可以保存为以下结构；`Path` 必须是当前平台规范化后的绝对路径，项目 `ID` 必须与目录名一致：

```json
{
  "ID": "demo",
  "Name": "示例项目",
  "Path": "E:\\projects\\demo",
  "State": "active",
  "CreatedAt": "2026-01-01T00:00:00Z",
  "UpdatedAt": "2026-01-01T00:00:00Z"
}
```

会话元数据以 SQLite 为唯一来源，保存 SessionID、ProjectID、标题、状态、会话审批模式及时间戳；列表、标题搜索、重命名和审批模式读写不读取历史 JSONL。SessionID 与 ProjectID 共同确定会话日志路径，不保存绝对路径。主会话入口的 AgentID 由 SessionID 派生，定义固定为 `primary`，会话不保存独立的主 Agent 身份或定义引用。

审批模式只分全局与会话两级：全局模式保存在 `config/settings.json`，会话模式保存在 SQLite 的 `sessions.approval_mode`；值为 `NULL` 时继承当前全局模式。输入区的审批选择只影响当前会话，可选 `ask`（询问）、`auto`（模型审核）、`yolo`（跳过审核）或继承全局。每次工具调用开始授权时确定有效模式，之后修改配置不改变该次授权或已有人工等待；明确的 `deny` 规则始终优先。

每行 JSONL 只保存一个 Event。启动只初始化项目配置目录并打开会话数据库，不预加载项目配置，不扫描会话历史，也不恢复旧 Execution。打开指定会话时才重放其 JSONL、校验历史并恢复 SessionContext；成功后由 Session 业务服务持有并复用内存状态，直到会话删除或应用退出清理。单个会话日志损坏只使该会话访问失败，不阻止应用启动或查询其他会话。Session 统一管理原始历史及持续维护的 Context；普通条目增量追加，压缩结果确认后直接替换 Context 的条目列表。每轮请求只读取当前 Context，Run 只保留本次执行证据。完整压缩结果以 `context.saved` 保存，原始历史不被改写。

Agent 实例由各次 Execution 在创建 Run 时创建并持有，执行结束后随 Run 释放，Session 不维护 Agent 注册表。查询空闲主会话时只从元数据与执行事实投影展示，不创建运行实例。Subagent 的 Agent、历史和执行过程只存在于所属 Run，不写日志，也不参与重启恢复。

Loop 在工具返回后立即保存实际回执，再执行后置 Hook。Hook 可以修改正文，成功后更新同一历史条目，并同步后续模型使用的 Context 和本次 Run；失败则记录错误、保留实际结果并继续 Loop。正文修改以 `entry.updated` 记录，不改变条目身份、原始序号、创建时间或错误标记；历史保存失败会终止 Loop，不重新执行工具。

会话创建、工具执行和文件预览都根据 Session 的 ProjectID 读取项目记录，只有 active 项目可创建新会话或执行目录操作。工具相对路径按 `Project.Path` 解析，文件读取与补丁写入继续校验项目目录边界和符号链接，Bash 的工作目录必须位于项目内，但不提供进程级沙箱隔离。

`storage` 管理项目配置文件、SQLite 会话元数据和历史日志文件读写，不持有历史业务状态，也不解释 Event 载荷。`Store` 在关闭时释放数据库连接；会话元数据仓储通过原子 SQL 更新，不额外加仓储锁。手动改名和自动命名由业务层发起，统一携带读取到的原标题进行原子条件更新；原标题不匹配时返回冲突，不自动重试，且不修改标题或时间戳。自动标题在业务层生成，仅在读取到空标题时尝试写入，已有标题时直接跳过。Session 业务服务负责事件编解码、按需重放、查询、恢复决策和内存变更；每个会话拥有独立的锁、事件序号、消息、执行记录、用量、耗时及 Context，直接查询这些内存集合，不维护额外的消息或执行 ID 索引。会话注册表的锁只负责定位会话，不覆盖日志 I/O，不同会话的历史操作可以独立进行。

完整业务记录生成后立即追加一行 JSONL，追加成功后才推进事件序号和发布内存变更；不逐 token 写日志，不在每条记录、执行结束或退出时额外 fsync，磁盘刷写交给操作系统，不承诺断电持久化。编码、取消和写入错误均向调用方返回；追加 I/O 失败可能已经产生部分写入，不能据此认定操作可安全重试。不支持运行期间从外部修改文件。日志层读取时只交付完整行，不完整尾行先备份再截断；业务重放遇到完整坏行明确报错。

创建会话先建立空 JSONL，再提交 SQLite 元数据；提交失败允许留下未被引用的空文件。删除由 Coordinator 确认没有活动执行后进行，以 SQLite 删除为提交点：数据库删除失败直接返回错误，不触碰 JSONL；数据库删除成功后释放会话内存并尽力删除 JSONL，清理失败允许留下孤儿文件，不回滚数据库删除。会话目录只由 SQLite 查询，不扫描、收养或重放未被引用的 JSONL。

Session 业务服务在会话首次重放时匹配工具调用与回执，读完后按调用顺序为缺失回执追加 `tool_interrupted`，写入成功后才更新内存；重复调用、重复回执、回执缺少声明或归属冲突直接报错，修复失败则本次会话访问失败，不发布半成品历史。SessionContext 从最近保存的完整压缩结果及之后已准入的普通 Entry 初始化，包含补写的中断回执，并同步仍保留回执的后续正文更新；不重复追加或恢复已压缩掉的回执，不逐条重放摘要替代关系。未进入保存结果的摘要不参与恢复。首次初始化 Context 时将旧进程留下的未结束执行保存为 `interrupted`，不创建它们的 Run，不请求模型、不继续执行工具，也不恢复审批等待。新的主 Execution 只由新的用户输入触发。

会话历史只接受单 Event 结构，无法识别的完整日志行会明确报错，不自动迁移或修补。运行前应备份数据目录；需要回滚时同时恢复匹配的程序和数据备份。
