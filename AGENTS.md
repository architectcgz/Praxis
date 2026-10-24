## AGENTS.md 修改约束

- 默认不修改本项目的任何 `AGENTS.md`，包括根目录与各子目录；只有用户明确要求时才允许修改。代码变更、重构或文档同步不构成修改授权。

架构文档和代码只描述目标状态，不保留任何旧文档或“为什么不用旧方案”的历史说明
注释默认使用中文
禁止做出优化后自动创建优化总结文档

## 开发服务启动约定

- 禁止自动启动开发服务，包括 `npm run dev`、Vite、`npm run preview` 和会启动前端服务的 `wails dev`。
- 新会话、代码修改、调试、截图和测试均不构成启动服务的授权；只有用户明确要求时才允许启动。
- 验证使用构建、静态检查、人工回归或已有服务；不保留测试文件，不新增或运行自动化测试。必须新启动服务才能验证时，先说明原因并征得用户同意。
- 用户授权启动后，先检查已有进程和端口，不重复启动；临时验证结束后关闭本次启动的服务，除非用户明确要求保留。

## 前端版本

- React 版本、API 和 Hooks 写法遵守 [`frontend/AGENTS.md`](frontend/AGENTS.md)；版本变更必须同步 `frontend/package.json` 和 `frontend/package-lock.json`。

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
