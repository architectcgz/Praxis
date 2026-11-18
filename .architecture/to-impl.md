# Session 对外数据边界实施目标

## 背景

Session 内部保存执行事实 `entry.Entry`，前端需要的是可展示的历史消息和实时状态。内部 Entry 还包含模型请求、运行生命周期、压缩和模型上下文相关事实，不能直接作为前端消息协议。

## 目标

建立一次清晰的边界转换：

```text
内部 Entry
    ↓
HistoryMessage / ModelRequestResult / RunSummary
    ↓
查询响应或实时事件
```

Session、Run 和 Storage 继续使用内部领域类型；Wails 与前端只接收对外 DTO。

## 历史消息

`HistoryMessage` 只表达可展示的历史消息：

- 消息身份：`id`、`runId`、`turnId`、`createdAt`；
- 角色和消息类型；
- 文本、思考、工具调用和工具结果内容；
- 与消息直接关联的请求 ID 和耗时；
- 消息级错误。

`HistoryMessage` 不包含：

- `retryCount`；
- 模型请求的开始、完成和失败状态；
- 请求用量；
- 执行配置、运行句柄和模型续传状态。

`retryCount`、请求类型、请求状态、请求错误、请求用量和请求耗时属于 `ModelRequest` 事实及其请求事件。前端需要展示模型请求尝试时，从独立的请求事件或请求事实中构造请求活动视图。

## History 读取

Session 初始化和历史分页返回 `HistoryMessage`，内部流程为：

```text
JSONL 读取 Entry
    ↓
过滤内部事实并映射可展示内容
    ↓
返回 HistoryMessage
```

运行结束和普通条目追加事件也使用同一套消息 DTO。实时展示不通过 Entry ID 再次查询历史。

历史分页只用于恢复订阅前已经发生的内容和加载更早消息；运行期间新增内容直接随事件发送。

## Run 展示

`RunSummary` 只表达运行阶段和结束结果：

- 运行中只返回身份、阶段、开始时间和执行累计；
- 运行结束后附带不可变 `RunResult`；
- 结果中包含 `outcome`、结束时间和失败原因；
- 失败原因属于 `RunResult`，不作为运行阶段字段平铺到 `RunSummary`。

运行控制按 Agent 身份路由。停止指定 Subagent 时，由 Parent 持有并调用该 Subagent 的取消句柄，不按 `RunID` 查找活跃 Run。

## 用量

历史累计用量在 Session 初始化快照中返回一次。运行期间每个模型请求的结果事件直接携带本次用量，前端按请求身份去重并增量更新会话累计。

前端不在每次模型请求完成后重新查询会话用量。重新打开 Session 时，初始化快照负责恢复已经发生的历史累计。

以下字段必须保留未知与零值的区别：

- Provider 未返回的输入、输出或缓存计数为未知；
- Provider 明确返回零时保留零；
- 请求总数与已报告用量的请求数分别统计。

## 验证

完成实现后执行：

```text
cd backend
go list ./...
go build ./...
go vet ./...
cd ../frontend
npm run check
```

修改 Wails 对外签名后，在 `backend` 执行绑定生成命令，再执行前端检查。不得手动修改生成的绑定文件。
