## 数据命名与上下文职责

- 类型名描述数据是什么，不强调生成方式；包名已表达的领域不在类型名中重复。Task 执行输入使用 `task.Input`，已选定并保存的模型配置使用 `model.Config`，模型目录配置使用 `modelconfig.Model`。
- 模型目录解析使用 `ModelConfigResolver`、`ResolveTaskModel` 和 `ResolveDefaultTaskModel`；构建执行输入使用 `buildTaskInput`。
- 历史消息选择属于 Task 层。启动时保存 `MessageSequenceBoundary`、`ExcludedMessageIDs` 和 `SessionContextRefs`；恢复时使用这些既定值，不根据其他 Task 的当前状态重新计算。
- 消息流归属与历史消息选择分别命名：主 Agent 使用 Session 消息流，其他 Agent 使用各自的私有流；局部消息选择结果使用 `selectedMessages`，所选工具调用使用 `selectedToolCalls`。
- `core/context.ContextBuilder` 只使用已选消息和共享条目构建 `ModelContext`，不查询 Task 状态或计算历史边界、排除项。
- ToolResult 必须按 `(TaskID, CallID)` 关联 ToolCall，历史回执的配对排序复用 `core/context/history.go`。

- JSONL 正常读写只支持当前持久化结构，不保留旧输入字段兼容逻辑。
