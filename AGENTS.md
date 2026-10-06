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

## 输入规范化与 TrimSpace 约定

- `TrimSpace` 只在输入边界执行，并由边界负责保存规范化后的值。输入边界包括 Wails/API 请求、Provider 响应、配置文件读取、持久化恢复和工具参数解析。
- 同一字段必须明确唯一的规范化 owner。后续业务层、执行器和适配器只使用已规范化的值，不重复 `TrimSpace`、`filepath.Clean` 或同类清洗。
- Constructor 负责复制、规范化和校验，成功返回 canonical 对象；`Validate` 只读校验，不修改对象，不负责补默认值或再次清洗。
- Constructor 成功后，同一调用链不得立即重复调用 `Validate`。对象发生状态变更后，或从持久化介质恢复后，才重新执行 `Validate`。
- 持久化恢复属于不可信边界。恢复后的 `Validate` 必须拒绝空值、非法值和非 canonical 值，但不得把修正后的值静默写回对象。
- 用户输入、Session 标题、队列 Prompt 由 application service 入口规范化；项目和 Workspace 路径由项目服务入口规范化，之后只接受 absolute normalized path。
- 模型配置由 `model_registry.Prepare` 统一规范化；Agent 配置由配置加载器统一规范化；工具调用 ID 由 Provider stream 或工具调用入口统一规范化。
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
