# Praxis 后端目录结构重构实施方案

> **依据**：目标结构与依赖边界以 [directory-structure.md](directory-structure.md) 为准，通用编码约束遵守 [../code-style.md](../code-style.md)。

---

## 一、总体策略与不变式

本次改造属于纯后端包结构与依赖边界整理，全程保证以下外部契约**零变更**：

- **前端与 Wails 协议不变**：`wails/bindings` 的全部公开方法签名、请求/响应 JSON tag 及 `praxis:session:<id>` 事件载荷格式保持不变，`frontend/` 无需改动。
- **持久化格式不变**：SQLite `sessions.db` 表结构、项目 `project.json`、`main.jsonl` 与子代理 `.jsonl` 条目编码格式保持不变。
- **配置文件格式不变**：`models.json`、`auth.json`、`tools.json`、`settings.json` 与 `agents/<id>/ROLE.md` 格式保持不变。
- **每阶段独立可编译**：共拆为 7 个原子阶段，每个阶段结束后必须通过 `go build ./...` 与 `go vet ./...`，支持逐阶段提交与回滚。

---

## 二、分阶段实施步骤

### 阶段 1：解除 `event -> session` 依赖倒置与显式注入 `Emitter`

**目标**：在不搬移目录的前提下，率先切断 `core/event -> core/session` 依赖边，并让 `Coordinator` 显式持有 `Emitter`。

1. **在 `internal/core/model/model.go` 导出 `Choice`**：
   - 将私有 `modelKey` 替换为公开值类型：
     ```go
     type Choice struct {
         ProviderID     string
         ModelID        string
         ReasoningLevel string
     }
     ```
   - `Catalog.models` 改为 `map[Choice]Config`。
   - `Catalog.Get(choice Choice) (Config, error)` 直接接收 `Choice`。
   - 在 `Option` 上新增 `DefaultChoice() Choice`，返回 `{ProviderID, ModelID, DefaultReasoningLevel}`。
2. **清理 `internal/core/session/`**：
   - 删除 `session.go` 中的 `ModelChoice` 定义（不留别名），`Session.choice`、`Configure`、`Choice()` 与 `info.go` 中的 `SessionInfo.ModelChoice` 统一改为 `modelapi.Choice`。
   - `Session.Configure` 中空选择回退改为 `choice = config.Models.DefaultModel().DefaultChoice()`。
   - `Session.RunConfiguration` 改为 `s.configuration.Models.Get(s.choice)`。
3. **切断 `internal/core/event/event.go` 对 `core/session` 的依赖**：
   - 删除 `praxis/internal/core/session` 导入；`SessionConfigReloaded.ModelChoice` 改为 `modelapi.Choice`。
4. **同步全链路 `model.Choice` 签名**：
   - `internal/repository/session.go`：`SetModelChoice` 参数改为 `modelapi.Choice`。
   - `internal/infra/storage/session.go`：`SetModelChoice` 参数改为 `modelapi.Choice`。
   - `internal/core/coordinator/session.go`：`SessionView.ModelChoice`、`SelectModel` 参数改为 `modelapi.Choice`；两处 `Models.Get(...)` 改为直接传 `choice`。
   - `internal/service/session/service.go`：`SelectModel` 参数改为 `modelapi.Choice`。
   - `wails/bindings/{services,bindings_session,bindings}.go`：接口签名与构造改为 `modelapi.Choice`。
5. **在 `Coordinator` 显式注入 `Emitter`**：
   - `internal/core/coordinator/coordinator.go`：`Config` 与 `Coordinator` 新增 `Emitter *coreevent.Emitter` / `emitter *coreevent.Emitter`。
   - `internal/bootstrap/coordinator.go`：向 `coordinator.Config` 传入 `Emitter: emitter`。
   - `internal/core/coordinator/{control,run,schedule,session,subagent,task}.go`：将 `s.loop.Emitter.Emit(...)` / `r.loop.Emitter.Emit(...)` 全部改为 `s.emitter.Emit(...)` / `r.emitter.Emit(...)`。

**阶段验收**：
- `cd backend && go build ./... && go vet ./...`
- `go list -f '{{.Imports}}' ./internal/core/event` 输出中不含 `praxis/internal/core/session`。

---

### 阶段 2：合并 `timing` 至 `entry`，将基础概念包与 `event` 提升至 `internal/`

**目标**：建立共享基础概念层路径，消除 `timing` 微包与 `utils/` 多余嵌套。

1. **将 `timing` 并入 `entry`**：
   - 将 `internal/core/timing/span.go` 迁入 `internal/entry/timing.go`（`package entry`）。
   - `Record` 命名为 `Timing`，保持 4 个导出字段及 JSON tag 不变（`StartedAt`、`FinishedAt`、`DurationMS`、`FirstResponseMS`），确保 `wails/bindings` 的 `(*Timing)(...)` 结构体指针转换零改动兼容。
   - `Span` 与 `NewSpan()` 保持同名。
   - 更新 `entry/{content_block,model_request,payload}.go`、`event/event.go`、`loop/compression.go`、`modelcall/caller.go`、`toolexecution/executor.go` 的类型与构造调用，删除 `internal/core/timing/`。
2. **提升基础概念包与 `event` 目录路径**：
   - `internal/core/model` -> `internal/model`
   - `internal/core/entry` -> `internal/entry`
   - `internal/core/context` -> `internal/context`
   - `internal/core/agent` -> `internal/agent`
   - `internal/core/task` -> `internal/task`
   - `internal/core/project` -> `internal/project`
   - `internal/core/security` -> `internal/security`
   - `internal/core/event` -> `internal/event`
   - `internal/utils/pathutil` -> `internal/pathutil`（移除空目录 `internal/utils/`）
3. **全局替换对应 import 路径**：
   - 同步更新 `internal/...` 与 `wails/...` 中的导入路径，清理不再需要区分包名的冗余导入别名（仅在包名与局部变量名冲突处保留必要别名）。

**阶段验收**：
- `cd backend && go build ./... && go vet ./...`

---

### 阶段 3：整合 `internal/tool/`（契约、输出与内置工具合包）

**目标**：将所有工具定义、注册表、临时文件输出与内置工具实现收拢至 `internal/tool/`，消除 `bootstrap/tools.go` 泛型包装层。

1. **迁移公共工具包与协作工具**：
   - `internal/core/tool/{tool,catalog,registry}.go` -> `internal/tool/`
   - `internal/core/tool/subagent/` -> `internal/tool/subagent/`
   - `internal/infra/tools/output/` -> `internal/tool/output/`
2. **合并三个内置工具（每个工具包直接实现 `tool.Tool`）**：
   - `internal/tool/bash/`：
     - 合并 `core/tool/bash/bash.go` 与 `infra/tools/bash/executor.go`（`package bash`），引用 `praxis/internal/tool`、`praxis/internal/tool/output`、`praxis/internal/pathutil`。
     - 导出 `New() tool.Tool`，通过内部结构体实现 `Definition()`、`NewArguments()`、`Prepare(arguments any, environment tool.Environment) (tool.PreparedCall, error)`（在 `Prepare` 中完成参数类型断言、业务校验并绑定 `call(ctx, prepared, environment)`）。
   - `internal/tool/readfile/`：
     - 合并 `core/tool/readfile/readfile.go` 与 `infra/tools/readfile/executor.go`，导出 `New() tool.Tool`，并实现 `ReadOnly() bool { return true }`。
   - `internal/tool/applypatch/`：
     - 合并 `core/tool/applypatch/{applypatch,content,parser}.go` 与 `infra/tools/applypatch/executor.go`，导出 `New() tool.Tool`。
3. **精简 `internal/bootstrap/tools.go`**：
   - 删除 `builtinTool[Arguments]` 泛型结构体与方法。
   - `buildToolRegistry` 直接注册 `readfile.New()`、`bash.New()`、`applypatch.New()` 及外部 `extensions`。
4. **同步导入并清理空目录**：
   - 更新 `entry`、`core/provider`、`infra/providers/*`、`core/*`、`infra/toolregistry`、`bootstrap/*` 对 `tool` 的导入路径。
   - 删除 `internal/core/tool/` 与 `internal/infra/tools/`。

**阶段验收**：
- `cd backend && go build ./... && go vet ./...`
- 检查 `internal/tool/...` 不导入 `internal/event` 及任何运行期引擎包。

---

### 阶段 4：整合 `internal/provider/`（协议契约、路由与厂商适配器）

**目标**：将模型协议契约、协议路由器与厂商协议实现统一收归 `internal/provider/`，以 `provider/router` 隔离根包与子包方向避免循环依赖。

1. **迁移协议契约层**：
   - `internal/core/provider/{provider,tool_call_ids}.go` -> `internal/provider/`（`package provider`）。
2. **迁移传输与厂商适配器子包**：
   - `internal/infra/providers/httpclient/` -> `internal/provider/httpclient/`
   - `internal/infra/providers/streaming/` -> `internal/provider/streaming/`
   - `internal/infra/providers/anthropic/` -> `internal/provider/anthropic/`
   - `internal/infra/providers/openai_chat/` -> `internal/provider/openai_chat/`
   - `internal/infra/providers/openai_responses/` -> `internal/provider/openai_responses/`
3. **建立协议路由子包 `internal/provider/router/`**：
   - 将 `internal/infra/providers/provider.go` 迁入 `internal/provider/router/router.go`（`package router`）。
   - 保留 `Credentials`、`Dependencies`、`Router`（或 `Provider`）与 `New(deps Dependencies) *Router`，实现 `provider.Provider` 接口。
4. **同步导入并清理空目录**：
   - 更新 `bootstrap/configuration.go`（调用 `router.New(...)`）、`infra/modelregistry`（引用 `provider/httpclient`）、`core/{hook,session,modelcall,approval,loop}` 的导入路径。
   - 删除 `internal/core/provider/` 与 `internal/infra/providers/`。

**阶段验收**：
- `cd backend && go build ./... && go vet ./...`
- 确认 `internal/provider/...` 无循环依赖且不导入 `internal/event` 及运行期引擎包。

---

### 阶段 5：整合 `internal/config/`（统一本地配置与数据目录加载）

**目标**：将 `dataroot`、`settings`、`modelregistry`、`agentregistry`、`toolregistry` 合并为内聚的 `internal/config/` 包，消除重复错误类型与命名冲突。

1. **在 `internal/config/` 下整理文件与公开 API**：
   - `errors.go`：统一 `ConfigurationError{Path string, Err error}`（供模型配置与角色定义加载共用）。
   - `dataroot.go`：`DataRoot`、`ResolveDataRoot(root string) (DataRoot, error)`、`(d DataRoot) Initialize(ctx context.Context) error`。
   - `settings.go`：`Settings{ApprovalMode security.ApprovalMode}`、`LoadSettings(path string) (Settings, error)`。
   - `models.go` 与 `models_document.go`：
     - `ModelDocument`、`ModelGroup`、`ModelProvider`、`ModelRegistry`、`ProviderCredential`、`CredentialType`、`CredentialTypeAPIKey`、`ProviderCredentials`、`CredentialSnapshot`；
     - `LoadModels(modelsPath, credentialsPath string, client *http.Client) (*ModelRegistry, error)`。
   - `roles.go` 与 `role_parser.go`：
     - `LoadAgentDefinitions(root string) ([]agent.AgentDefinition, error)`（直接返回按 `ID` 排序的定义切片，清理无调用方的 `Registry.Definition(id)`）。
   - `tools.go`：
     - `LoadTools(path string, registry *tool.ToolRegistry) (tool.Config, error)`。
2. **同步调用方并删除旧目录**：
   - `backend/main.go`：改用 `config.ResolveDataRoot(rootPath)`。
   - `internal/bootstrap/application.go`：`Open` 参数改为 `config.DataRoot`。
   - `internal/bootstrap/configuration.go`：改用 `config.LoadSettings`、`config.LoadModels`、`config.LoadAgentDefinitions`、`config.LoadTools` 与 `config.ModelDocument` / `config.Settings`。
   - 删除 `internal/dataroot/`、`internal/settings/`、`internal/infra/{modelregistry,agentregistry,toolregistry}/`。

**阶段验收**：
- `cd backend && go build ./... && go vet ./...`
- 确认 `internal/config` 不依赖 `internal/event`、存储包及运行期引擎包。

---

### 阶段 6：迁入 `internal/engine/` 并收口 `wails/bindings` 边界

**目标**：将全部核心执行引擎包统一迁入 `internal/engine/`，移除空目录 `internal/core/`，并收口 `wails/bindings` 对 `engine/approval` 的直接依赖。

1. **迁移 8 个执行引擎包**：
   - `internal/core/hook` -> `internal/engine/hook`
   - `internal/core/session` -> `internal/engine/session`
   - `internal/core/toolexecution` -> `internal/engine/toolexecution`
   - `internal/core/modelcall` -> `internal/engine/modelcall`
   - `internal/core/approval` -> `internal/engine/approval`
   - `internal/core/loop` -> `internal/engine/loop`
   - `internal/core/run` -> `internal/engine/run`
   - `internal/core/coordinator` -> `internal/engine/coordinator`
2. **收口 `wails/bindings` 对 `engine/approval` 的依赖**：
   - 在 `internal/service/session/service.go` 中补充 `type ToolApproval = approval.Approval`（与已有的 `ToolDecision`、`ToolControl` 对齐）。
   - `wails/bindings/bindings_command.go` 删除 `approval` 导入，`publicToolApprovals` 参数改为 `[]sessionservice.ToolApproval`。
3. **全局更新导入并移除 `internal/core/`**：
   - 同步更新 `engine/*`、`repository`、`infra/storage`、`service/session`、`wails/bindings`、`bootstrap` 的导入路径。
   - 删除空目录 `internal/core/`。

**阶段验收**：
- `cd backend && go build ./... && go vet ./...`
- 核对 `wails/bindings` 不导入 `engine/{coordinator,run,loop,modelcall,approval,toolexecution}`。

---

### 阶段 7：整合 `internal/storage/` 并切换消费方最小存储接口

**目标**：将 `infra/storage`、`infra/jsonl` 与项目目录准备能力收归 `internal/storage/`，改由各消费方声明最小存储接口，彻底移除 `internal/repository/` 与 `internal/infra/`。

1. **迁移存储与目录准备代码**：
   - `internal/infra/jsonl/` -> `internal/storage/jsonl/`
   - `internal/infra/storage/` -> `internal/storage/`
   - 将 `internal/infra/filesystem/filesystem.go` 中的目录准备实现以 `LocalDirectory` 并入 `internal/storage/project.go`（保留 `EnsureExists(ctx, absolutePath) (bool, error)` 语义），删除 `internal/infra/filesystem/`。
2. **在消费方声明最小存储接口**：
   - `internal/engine/coordinator/coordinator.go`：
     ```go
     type ProjectStore interface {
         Get(context.Context, identity.ProjectID) (project.Project, error)
     }

     type SessionStore interface {
         Get(context.Context, identity.SessionID) (session.SessionInfo, error)
         SetModelChoice(context.Context, identity.SessionID, model.Choice, time.Time) error
         UpdateTitle(ctx context.Context, id identity.SessionID, expectedTitle, title string, at time.Time) error
         SetApprovalMode(context.Context, identity.SessionID, *security.ApprovalMode, time.Time) (session.SessionInfo, error)
     }

     type SessionLoader interface {
         Load(context.Context, identity.SessionID) (*session.Session, error)
     }
     ```
   - `internal/service/project/service.go`：
     ```go
     type ProjectStore interface {
         Get(context.Context, identity.ProjectID) (project.Project, error)
         Save(context.Context, project.Project) error
         List(context.Context, int) ([]project.Project, error)
     }
     ```
   - `internal/service/session/service.go`：
     ```go
     type ProjectStore interface {
         Get(context.Context, identity.ProjectID) (project.Project, error)
     }

     type SessionStore interface {
         Get(context.Context, identity.SessionID) (session.SessionInfo, error)
         Create(context.Context, session.SessionInfo) error
         ListByProject(context.Context, identity.ProjectID, int) ([]session.SessionInfo, error)
         UpdateTitle(ctx context.Context, id identity.SessionID, expectedTitle, title string, at time.Time) error
         Delete(context.Context, identity.SessionID) error
         SessionLogPath(context.Context, identity.SessionID) (string, error)
         IsEmpty(context.Context, identity.SessionID) (bool, error)
     }
     ```
3. **移除 `internal/repository/` 与 `internal/infra/`**：
   - `internal/storage/{project,session}.go` 删除 `praxis/internal/repository` 导入及对应接口断言；
   - `internal/bootstrap/application.go` 改用 `praxis/internal/storage` 与 `Directory: storage.LocalDirectory{}`，编译期自动校验所有消费方接口契约；
   - 删除 `internal/repository/` 与空目录 `internal/infra/`。

**阶段验收**：
- `cd backend && go build ./... && go vet ./...`
- 确认 `internal/core/`、`internal/infra/`、`internal/repository/`、`internal/utils/`、`internal/dataroot/`、`internal/settings/` 均已彻底清除。

---

## 三、最终验证清单

全部 7 个阶段完成后，在 `backend/` 目录下执行以下检查（不新增自动化测试文件，不自动启动开发服务）：

### 1. 编译与静态检查
```powershell
cd E:\learn\projects\Praxis\backend
go build ./...
go vet ./...
```

### 2. 依赖红线核验
通过 `go list` 输出全量内部依赖图，对照 [directory-structure.md](directory-structure.md) 第四节逐条确认：

```powershell
cd E:\learn\projects\Praxis\backend
go list -f '{{.ImportPath}} -> {{range .Imports}}{{if eq (printf "%.7s" .) "praxis/"}}{{.}} {{end}}{{end}}' ./...
```

重点核对以下 6 项硬指标：
1. `praxis/internal/event`：导入集合中**无任何** `praxis/internal/engine/...`、`storage`、`provider`、`tool`、`config`。
2. `praxis/internal/{identity,errorcode,pathutil,logging,model,entry,context,agent,task,project,security}`：均**无** `engine/...`、`event`、`provider`、`config`、`storage`、`service`、`wails` 依赖（仅 `entry` 引用 `tool` 标识类型）。
3. `praxis/internal/provider/...`、`praxis/internal/tool/...`、`praxis/internal/config`：均**无** `engine/...`、`event`、`storage`、`service`、`wails` 依赖。
4. `praxis/internal/engine/...`：内部依赖严格单向（`hook / session / toolexecution / modelcall -> approval / loop -> run -> coordinator`），且不依赖 `storage`、`config`、`service`、`wails`。
5. `praxis/wails/bindings`：不直接导入 `engine/{coordinator,run,loop,modelcall,approval,toolexecution}`、`storage`、`config`、`provider`。
6. `praxis/internal/bootstrap`：唯一同时连接所有子系统的装配层。

### 3. 代码风格与目录整洁度复核
- 核对无遗留空目录、无死代码与无调用方的类型别名（遵守 [../code-style.md](../code-style.md) §3、§4）。
- 核对所有新增或修改的 Go 文件与注释均使用中文且不含 Emoji（遵守 [../code-style.md](../code-style.md) §5）。
