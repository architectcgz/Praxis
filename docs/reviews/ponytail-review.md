# Ponytail review

范围：当前工作树的 `backend`、`frontend` 和 `.architecture`；只检查过度工程、死代码和未消费的扩展点，不覆盖 correctness、security、performance。未修改业务代码。

backend/internal/application/services/execution/delivery/service.go:L1-248: delete: `delivery.Service` 没有 `NewService` 或任何调用方；删除整个用例，真正接入 ContextDelivery 时再从入口向下实现。
backend/internal/infrastructure/storage/artifact_resolver.go:L15-104; backend/internal/infrastructure/storage/repositories.go:L29,L62-64: delete: `ResolveContextArtifact` 只有自引用，连同 `Repositories.adapter` 转发一起删除；没有 delivery 入口就不保留解析器。
backend/internal/model/note.go:L1-70; backend/internal/repository/note.go:L1-13; backend/internal/infrastructure/storage/note_repo.go:L1-30; backend/internal/infrastructure/dataroot/dataroot.go:L29-30,L69-70,L94-95; backend/internal/infrastructure/sqlite/migrations/0001_schema.sql:L225-231: delete: Note 从未被 application service 或产品入口消费；删除整个未接入聚合、目录和表，新增笔记功能时再落地。
backend/internal/model/workspacelease.go:L1-98; backend/internal/model/delegation.go:L1-153: delete: 两个有状态聚合只有模型自测，没有 repository、application command 或 runtime 调用；删除到首个真实入口出现为止。
backend/internal/model/granttemplate.go:L7-25; backend/internal/tools/tool_registry.go:L27-40; backend/internal/infrastructure/agent_registry/config.go:L374-425: yagni: capability 配置声明 7 个 tool，但 registry 只实现 2 个；只保留已注册工具，`search_text`、`write_file`、`run_command`、`propose_delegate`、`submit_result` 在有 executor 前不要进入默认 policy。
backend/internal/infrastructure/sqlite/metadata.go:L11-20; backend/internal/infrastructure/sqlite/store.go:L239-258: delete: 未使用的 `exactlyOne` 和 `withValueTx`；staticcheck 已确认无调用方。
backend/internal/infrastructure/storage/store.go:L86-94,L105-113: delete: 未使用的 `exactlyOne` 和 `loadDocumentByID`；直接移除，不要为未来查询保留 helper。
backend/internal/model/capabilitygrant.go:L279-303; frontend/src/api/models.ts:L166-172: delete: 未使用的 `uniqueTools`、`uniqueResultPermissions` 和 `apiFormatValue`；让 staticcheck/TypeScript 保持零死代码。
backend/internal/infrastructure/providers/streaming/sse.go:L49-51: stdlib: 手写首个空格剥离；用 `strings.TrimPrefix(value, " ")`。
backend/internal/runtime/model.go:L65-70; backend/internal/infrastructure/providers/streaming/stream.go:L15-31: delete: `StopReason` 只被生产后丢弃；让 Decoder 直接返回 `error`，不要为未消费的停止原因维护整条返回链。
backend/internal/runtime/tool.go:L10-18; backend/internal/runtime/copy.go:L53-64; backend/internal/infrastructure/providers/streaming/tool.go:L20-37: shrink: `ToolCall.Input` 与 `Arguments` 是同一 JSON 的双字段别名；保留 `Input`，删除 reconcile 和重复赋值。
backend/internal/runtime/tool.go:L29-36; backend/internal/loop/execution_engine.go:L219-225; backend/internal/application/services/execution/tool_invocation/invoke.go:L113-119: shrink: `ToolResult.Content`/`Output` 被立即互相复制并清空；保留 `Content` 一个字段。
backend/internal/runtime/model.go:L79-92; backend/internal/runtime/copy.go:L76-82: yagni: `ExecutionTurnSnapshot` 的 execution/session/context/grant/turn 元数据当前只被填充和复制，provider 从未读取；只传 `Messages`、`SystemPrompt`、`Model`、输出预算和 `Tools`。
backend/internal/application/services/services.go:L297-340; backend/wails/app.go:L55: yagni: observer map、RWMutex 和递增 ID 只服务一个 `App.Attach` 订阅者；改成单个 callback，保留注销函数即可。
backend/internal/compose/application.go:L87-295: shrink: `Open` 重复十余次关闭 logger、registry、store；用一个 guarded `defer` 统一失败回滚，成功时解除 guard。
backend/internal/compose/application.go:L362-395: stdlib: 手写 runtime/store/log 三错误组合矩阵；用 `errors.Join` 汇总关闭错误。
frontend/src/api/commands.ts:L15; backend/wails/bindings/bindings_command.go:L50-111: delete: `Resume` 与 `QueueWork` 在前端 API 没有 binding 类型或调用方，当前只是未接入的后端暴露面；接入 UI 时再恢复端到端路径。
.architecture/architecture.md:L13-62: delete: 索引仍列出工作树中已经删除的二十多个文档；重建为当前目标状态目录，或直接删除这层目录清单。
nul:L1: delete: 空的未跟踪文件，纯粹是工作树噪音。

net: -900 lines possible.
