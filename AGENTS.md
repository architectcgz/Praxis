架构文档和代码只描述目标状态，不保留任何旧文档或“为什么不用旧方案”的历史说明
注释默认使用中文
禁止做出优化后自动创建优化总结文档

## 开发服务启动约定

- 禁止自动启动开发服务，包括 `npm run dev`、Vite、`npm run preview` 和会启动前端服务的 `wails dev`。
- 新会话、代码修改、调试、截图和测试均不构成启动服务的授权；只有用户明确要求时才允许启动。
- 验证优先使用构建、静态检查、单元测试或已有服务；必须新启动服务才能验证时，先说明原因并征得用户同意。
- 用户授权启动后，先检查已有进程和端口，不重复启动；临时验证结束后关闭本次启动的服务，除非用户明确要求保留。

## 前端版本

- React 使用 19.2.8（`frontend/package.json` 声明兼容范围为 `^19.1.0`，当前 lockfile 解析版本为 `19.2.8`）。
- `react-dom` 与 React 保持同版本；`@types/react` 和 `@types/react-dom` 使用 19.x 类型定义。

## Go 代码格式：复合字面量换行

- 多字段的 struct / map / slice 复合字面量，默认一个字段（或元素）一行，不要在 `{` 后把多个字段挤在同一行。
- 例外：只有一个字段、或该字面量作为简短的局部值且不超过 3 个字段时，可写在一行。

```go
// 推荐
agent := AgentInfo{
	Profile: string(view.Agent.Profile),
	State:   string(view.Agent.State),
}

// 不推荐
agent := AgentInfo{Profile: string(view.Agent.Profile), State: string(view.Agent.State)}
```

原因：gofmt 不做按行宽换行，换行取决于源码；写成多行可保证后续增删字段时 diff 更小、更清晰。

## 分层与依赖门禁

- 请求链固定为 `backend/wails → internal/request → internal/service`；Wails 只做 wire 形状校验、DTO 转换和错误映射，不解释 core 枚举、不构造 core 配置。
- `internal/service` 负责用例编排、事务边界和 Port 协调，可以依赖 `core`、`repository`、`contracts`、`request` 和窄 Port；不得直接 import `internal/agent_runtime`，也不得直接操作 `os` 文件系统。
- Agent 可见消息流的读写由 `repository.MessageStreams` 一个窄 Port 定义（`Append`/`List`/`LoadMessages`），唯一实现是 `infra/jsonl`：主 Agent 写 Session 流、其他 Agent 写私有流，日志键位由存储层决定。service 只持有该 Port，不自己判断写哪条流。
- 会话级消息存在性判断属于聚合仓储能力：用 `repository.SessionRepository.HasMessages`，不要在 service 里注入按流划分的消息仓储。
- 跨边界同形的执行参数使用 `internal/contracts` 的共享值类型，不为逐字段复制新增 adapter；只存在协议差异（事件、DTO、错误）时才在 `compose` 或 `wails` 转换。
- 消费方拥有窄接口：`internal/loop` 声明自身需要的执行接口，`internal/service/runtime` 声明 `TaskActivator`/`TaskStarter`；接口不放在无关的中间包。
- 依赖门禁在 `go vet` 之后执行：用 `go list -f '{{.ImportPath}} {{join .Imports " "}}'` 枚举生产包的直接 import，再匹配禁止前缀；有匹配即失败，无匹配即成功，`go list` 失败和 `rg` 工具错误必须原样失败，不得当成“无匹配”放过。
- 四项门禁前缀：`./wails/...` 禁 `internal/(core|agent_runtime|timing|repository|infra)`；`./internal/core/model/...` 禁 `internal/tools/contracts`；`./internal/service/...` 禁 `internal/agent_runtime` 和 `os`。

## 输入规范化与 TrimSpace 约定

- `TrimSpace` 只在输入边界执行，并由边界负责保存规范化后的值。输入边界包括 Wails/API 请求、Provider 响应、配置文件读取、持久化恢复和工具参数解析。
- 同一字段必须明确唯一的规范化 owner。后续业务层、执行器和适配器只使用已规范化的值，不重复 `TrimSpace`、`filepath.Clean` 或同类清洗。
- 输入层 Constructor 负责复制、规范化和校验，成功返回 canonical 对象；业务层构造器只接收 canonical 输入，负责复制与只读校验。`Validate` 不修改对象，不负责补默认值或再次清洗。
- Constructor 成功后，同一调用链不得立即重复调用 `Validate`。对象发生状态变更后，或从持久化介质恢复后，才重新执行 `Validate`。
- 持久化恢复属于不可信边界。恢复后的 `Validate` 必须拒绝空值、非法值和非 canonical 值，但不得把修正后的值静默写回对象。
- 用户输入、Session 标题、队列 Prompt 由 application service 入口规范化；项目和 Workspace 路径由项目服务入口规范化，之后只接受 absolute normalized path。
- 请求规范化与领域输入限制由 `internal/request` 统一负责：Wails binding 只构造 wire 形状的请求并做字段级形状检查，`request` 的 Constructor 负责复制、Trim、补展示默认值和引用校验，成功后返回 canonical request；service 只接收 canonical request，`core/model/config` 只定义类型、接口和只读校验，业务层不执行 Trim 或补默认值。配置文件恢复只读校验 canonical 数据，不清洗或静默修复。Agent 配置由配置加载器统一规范化；工具调用 ID 由 Provider stream 或工具调用入口统一规范化。
- 工具参数的 JSON 结构、未知字段、重复字段、大小限制和路径约束由工具调用边界及对应 normalizer 负责；执行器不重复清洗已规范化参数。
- 权限和安全检查不负责替业务输入清洗。授权函数只校验 canonical 路径和权限范围，不能通过隐式 trim 或 path clean 改变被授权对象。

推荐模式：

```go
value = strings.TrimSpace(value)
if value == "" {
	return err
}
```

后续代码直接判断 `value == ""`。只有持久化恢复校验需要拒绝非 canonical 字符串时，才比较原值与 `strings.TrimSpace(value)` 的结果。
