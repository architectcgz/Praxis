架构文档和代码只描述目标状态，不保留任何旧文档或“为什么不用旧方案”的历史说明
注释默认使用中文
禁止做出优化后自动创建优化总结文档

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