# Praxis History 与 Runtime Context 实现说明

## 1. 目标

Session History 使用 JSONL 保存 Agent 的完整执行事实，Runtime Context 只保存当前真正参与下一次模型请求的 Entry。两者通过 Session 的提交边界连接，Provider 只接收 Provider 中立的模型上下文，并在自己的适配器中完成协议编码。

实现直接采用目标格式和目标职责边界，不提供历史数据兼容解码、过渡适配器或新旧实现并存路径。验证使用独立的数据目录。

核心数据流如下：

```text
Entry
    ↓
HistoryStore.Append
    ↓ 成功
Session 更新 AgentContext
    ↓
发布 Event
```

模型请求数据流如下：

```text
Session.AgentContext + RunConfig
    ↓
Loop 组装 Provider Request
    ↓
Loop 执行请求预算和压缩策略
    ↓
Provider Encoder 编码具体协议
    ↓
Provider Stream
```

## 2. 核心概念

### 2.1 Entry

`Entry` 是 Agent 执行过程中产生并持久化的一条不可变领域记录。

要求：

- 核心领域类型统一为 `entry.Entry`。
- Entry 必须具有明确的 `RunID`、ID 和创建时间。
- Entry 不携带 `ModelVisible`、`Context` 等持久化选择字段。
- Entry 是否参与模型上下文由 Entry 的领域语义决定。
- 不定义 `HistoryEntry`、`ContextEntry` 或其他重复领域类型。

### 2.2 History

History 是某个 Agent 的全部持久化 Entry，使用一个独立的 JSONL 文件保存。

```text
Session
  ├── Main Agent      → main.jsonl
  ├── Subagent A      → subagents/A.jsonl
  └── Subagent B      → subagents/B.jsonl
```

AgentID 是 History 的一级作用域，由 HistoryStore 的方法参数传入，不建立 EntryID 到 AgentID 的全局索引。

所有成功持久化的 Entry 都属于 History，包括：

- User Input
- Assistant
- ToolCall
- ToolResult
- Summary
- ModelRequest Started / Completed / Failed
- Retry
- Usage
- Run 生命周期记录
- Terminal Entry

### 2.3 AgentContext

`AgentContext` 是 Session 持有的当前有效模型上下文，直接保存 Entry 引用：

```go
type AgentContext struct {
    Entries []*entry.Entry
}
```

AgentContext 不是完整 History Cache，也不保存 History-only Entry。

Session 在 Entry 成功持久化后按语义更新 AgentContext：

```text
Model Context Entry
    → AgentContext.Append

Summary
    → AgentContext = [Summary]

History-only Entry
    → 只进入 JSONL History
```

AgentContext 的构造和更新属于 Session 的提交与恢复逻辑。系统不设置独立的 `ContextBuilder` 组件。

### 2.4 JSONL Record

JSONL Record 是 Entry 编码后占据一整行的物理记录。

Storage 只理解：

- 文件路径
- Agent 作用域
- Record 边界
- byte offset
- Append、Sync、Close
- 正向或反向读取
- JSON 编解码

Storage 不理解 Summary、Run Terminal 或 Model Context 语义。

### 2.5 Event 与 Run Memory

```text
Event
    = 实时通知当前正在发生什么

Run Memory
    = 尚未正式进入模型 Context 的运行时协作状态

Entry / JSONL
    = 已经正式持久化的不可变历史事实
```

三者保持独立。未完成的 Subagent、待发送指令和待接收报告属于 Run Memory，不作为额外的 History Payload 持久化。

## 3. 模块职责

### 3.1 `core/entry`

负责：

- Entry 结构。
- ContentBlock 结构。
- Entry Payload 类型。
- Entry 是否参与 Model Context 的领域判断。
- Run、Turn、ModelRequest、Summary 和 Terminal 的事实语义。

不负责：

- 文件写入。
- History 分页。
- Provider 协议编码。
- 前端视觉聚合。

### 3.2 `core/session`

负责：

- 持有每个 Agent 的 AgentContext。
- 通过 HistoryStore 提交 Entry。
- 实现 persist-first、memory-second。
- Summary 成功持久化后的 Context 替换。
- 启动时恢复 AgentContext。
- 向上层提供完整 HistoryPage。

不保存完整 History，不维护全量 Entry 索引，不维护 Run、ModelRequest 或 ToolCall 的长期重复 Projection。

### 3.3 `infra/jsonl`

负责：

- 每个文件一个 Writer。
- Append、Sync、Close 的串行执行。
- Append partial write 回滚。
- Truncate 失败后的 Writer 不可用状态。
- Reverse Scanner。
- Record Boundary 校验。
- incomplete tail 修复。

### 3.4 `infra/storage`

负责：

- AgentID 到 JSONL 文件的映射。
- Agent Writer 生命周期。
- Entry JSONL 编解码。
- HistoryStore 的具体实现。
- 反向读取和 cursor 计算。
- Session 打开与关闭时的物理资源管理。

不负责：

- Summary 判断。
- AgentContext 更新。
- Run Recovery 的业务决策。
- Subagent 报告拼装。

### 3.5 Coordinator、Run 与 Loop

Coordinator 是 Run 生命周期的唯一业务权威。

Run 负责：

- 当前执行状态。
- Run Memory。
- Entry 的业务顺序。
- Terminal 唯一性。
- 取消、超时、失败和正常完成的结算。

Loop 负责：

- 读取当前 AgentContext。
- 组装本次 Provider Request。
- 模型请求重试。
- 请求预算策略。
- 触发 Context Compression。
- 按业务顺序提交 Assistant、ToolCall 和 ToolResult。

Loop 不负责 Provider 协议编码。

### 3.6 Provider Encoder

Provider Encoder 负责 Provider 特有的请求编码：

- 角色映射。
- 文本和思考块编码。
- ToolCall / ToolResult 编码。
- Provider 特有的消息合并。
- Reasoning State 回放规则。
- Provider 请求结构和 JSON 编码。

Anthropic、OpenAI Chat 和 OpenAI Responses 可以拥有不同的编码方式，但都消费 Provider 中立的模型上下文。

### 3.7 请求容量与压缩

容量策略由 Loop 的请求编排逻辑拥有：

- 计算请求前的近似预算。
- 加入工具、输出预留和模型窗口约束。
- 判断是否触发压缩。
- 判断是否添加运行提示。
- 处理容量不足错误。

Context Builder 不负责容量估算，因为系统不设置 Context Builder。Provider Encoder 负责实际协议结构，Provider 负责最终请求边界的协议级校验。

## 4. HistoryStore 接口

HistoryStore 的核心接口如下：

```go
type HistoryStore interface {
    Append(
        context.Context,
        identity.AgentID,
        entry.Entry,
    ) error

    ReadBackward(
        context.Context,
        identity.AgentID,
        *int64,
        int,
    ) (HistoryPage, error)

    Sync(
        context.Context,
        identity.AgentID,
    ) error

    CommitTerminal(
        context.Context,
        identity.AgentID,
        entry.Entry,
    ) error
}
```

`CommitTerminal` 必须在同一个 Agent Writer 中连续执行：

```text
Append Terminal
    ↓
Sync
```

期间不插入同 Agent 的其他 Append。

Writer 的调用规则：

```go
select {
case writer.requests <- request:
case <-ctx.Done():
    return ctx.Err()
}

return <-request.result
```

请求一旦进入 Writer 队列，就必须等待最终结果。

## 5. Append 不变量

Entry 提交必须满足：

```text
构造 Entry
    ↓
Storage.Append
    ↓ 成功
更新 AgentContext
```

具体要求：

1. Append 成功表示完整 JSONL Record 已存在。
2. Write 失败或 short write 时回滚到 Append 前的 offset。
3. Rollback 失败表示该 Agent History 已进入严重 Storage Error 状态。
4. Storage 不自动重试 Append。
5. Append 与 Sync 是不同保证。
6. 普通 Entry 不逐条 Sync。
7. 一个 Run 只在 Terminal Entry 成功追加后 Sync 一次。
8. Terminal Entry 后禁止继续写入该 Run。
9. 一个 Run 最多一个 Terminal。
10. 所有 Agent History Entry 都必须具有 RunID。

## 6. AgentContext 与请求组装

Session 持有当前 AgentContext。Loop 直接使用它组装 Provider 中立的请求上下文：

```go
request := providerapi.Request{
    Context: appcontext.ModelContext{
        SystemPrompt: params.SystemPrompt,
        Entries:      agentContext.Entries,
    },
    Model:           params.Model,
    Tools:           tools,
    MaxOutputTokens: params.Model.MaxOutputTokens,
    TurnID:          turnID,
}
```

`ModelContext` 只表示本次请求的 Provider 中立数据。它不筛选完整 History，不重排 History，不编码 Provider 协议。

如系统提示词继续属于 Run 配置，也可以将其作为 Request 字段直接传递，避免为一次结构组装增加独立 Builder。

## 7. Summary 行为

Summary 同时是：

- AgentContext 的压缩检查点。
- History 中的一条完整 Entry。
- Frontend 可展示的历史 Marker。

提交流程：

```text
Loop 生成 Summary
    ↓
HistoryStore.Append(Summary)
    ↓ 成功
AgentContext = [Summary]
    ↓
继续追加新的 Model Context Entry
```

原有 Entry 继续保存在 JSONL 中，AgentContext 只保留最新 Summary 及其后的有效 Entry。

## 8. Run Recovery 与 Runtime Context Recovery

两类恢复分别处理不同问题。

### 8.1 Storage Recovery

启动时首先修复物理 JSONL 问题：

- incomplete tail。
- partial trailing record。
- 不完整的最后一行。

这一层不理解 Run 或 Summary。

### 8.2 Run Recovery

对每个 Agent 读取最后一条完整 Entry：

```text
最后一条是 Terminal
    → 无需处理

最后一条不是 Terminal
    → 追加 RunInterrupted
    → Sync
```

RunInterrupted 只闭合 History，不恢复旧 Loop，不继续旧 Run，也不恢复 Subagent 执行。

### 8.3 Runtime Context Recovery

Run Recovery 完成后，从 EOF 反向扫描：

```text
History EOF
    ↑
反向读取 Entry
    ↑
收集参与 Model Context 的 Entry
    ↑
遇到最新 Summary 后停止
```

收集结果反转为旧到新顺序，形成 AgentContext。

## 9. Subagent

Subagent 是运行时对象，每个 Subagent 拥有独立 History。

规则：

- Subagent 未完成时只存在于 Run Memory。
- 应用退出后不恢复 Subagent 执行。
- `spawn_subagent(task)` 在 Subagent History 中形成普通 Initial Input Entry。
- Parent Instruction 在目标 Subagent History 中形成普通 Input Entry。
- Parent 接收 Report 时，只有真正进入 Parent AgentContext 的内容才创建 Entry。
- 跨 Agent Entry 必须自包含，直接保存模型需要的报告正文。
- 不通过 ReportID 查询其他 Agent 的 JSONL。
- 不建立跨 Agent History Lookup。

## 10. HistoryPage 与 Cursor

HistoryPage 返回完整 History：

```go
type HistoryPage struct {
    Entries    []entry.Entry
    NextCursor *int64
    HasMore    bool
}
```

接口使用 AgentID 和 byte-offset cursor：

```go
HistoryPage(
    ctx context.Context,
    agentID identity.AgentID,
    cursor *int64,
    limit int,
)
```

cursor 语义：

```text
nil
    从当前 EOF 开始

0
    BOF

N > 0
    从 N 之前继续反向读取
```

合法性要求：

```text
0 <= cursor <= fileSize
```

当 `cursor > 0` 时，必须满足：

```text
file[cursor-1] == '\n'
```

HistoryPage 的返回顺序为旧到新，Frontend 可以直接执行：

```ts
entries = [...page.entries, ...entries]
```

Backend 不做：

- UI 过滤。
- ToolCall / ToolResult 聚合。
- ModelRequest 聚合。
- Summary 隐藏。
- Retry 隐藏。
- 为视觉组件 over-read。

## 11. Wails 与 Frontend 边界

Wails 负责将每个 `entry.Entry` 映射为一个 `dto.Entry`：

```text
entry.Entry[]
    ↓
Wails 1:1 DTO 映射
    ↓
dto.Entry[]
```

必须满足：

```text
len(input) == len(output)
```

Frontend 负责：

- 保存已加载 History。
- 根据 cursor 加载更旧页面。
- 跨页重新构造完整 ViewModel。
- 通过 callID 组合 ToolCall 和 ToolResult。
- 聚合 ModelRequest attempt、失败和 Retry。
- 展示 Summary Marker。
- 展示 Run Terminal。

一个视觉组件可以由多个 Entry 构成，但这只属于 Frontend ViewModel，不改变 JSONL History 和 DTO 的 Entry 边界。

## 12. 实现顺序

### 第一步：Entry 领域模型

完成 `Entry`、Payload、Run Terminal、AgentContext 结构和参与模型上下文的规则。同步修改所有 Core、Loop、Provider 和 Wails 类型引用。

### 第二步：JSONL Writer 与 Reverse Scanner

完成 Writer 串行提交、Append 回滚、Sync、Close、Terminal Command、反向扫描、cursor 校验和 incomplete tail 修复。

### 第三步：HistoryStore

完成 Agent Writer 管理、JSONL 编解码、Append、ReadBackward、Sync、CommitTerminal 和资源关闭。

### 第四步：Session 与 AgentContext

完成 persist-first、memory-second、AgentContext 更新、Summary 替换和完整 HistoryPage 查询。Session 不保存完整 History。

### 第五步：启动恢复

按 Storage Recovery、Run Recovery、Runtime Context Recovery 的顺序完成启动恢复，并保证 Run Recovery 幂等。

### 第六步：Run、Coordinator、Loop 和 Compression

完成 Run Terminal 约束、Terminal Sync、ModelRequest History 记录、AgentContext 请求组装、容量策略和 Summary 压缩。

### 第七步：Subagent

完成独立 Agent History、普通 Input Entry、自包含 Parent Report Entry 和 Run Memory 协作状态。

### 第八步：HistoryPage 与 Wails

完成 byte-offset 分页、完整 Entry 返回、1:1 DTO 映射，并重新生成 Wails 绑定。

### 第九步：Frontend History ViewModel

完成数值 cursor、跨页加载、Entry 合并、视觉聚合和完整历史展示。

### 第十步：目标状态验证

删除无调用方的实现和类型，执行全仓库术语检查、后端构建与静态检查、Wails 绑定生成和前端检查。

## 13. 验证方式

后端验证：

```text
cd backend
go list ./...
go build ./...
go vet ./...
```

Wails 签名变化后：

```text
cd backend
go run -tags bindings .
```

前端验证：

```text
cd frontend
npm run check
```

使用独立临时数据目录验证以下行为：

- 单条 Record 跨越多个 I/O Buffer。
- 非法 cursor 被拒绝。
- partial write 回滚且不留下半条 Record。
- Writer 队列取消和提交后的等待语义正确。
- incomplete tail 被修复。
- Terminal Append 与 Sync 连续执行。
- RunInterrupted 恢复幂等。
- Runtime Context 在最新 Summary 处停止恢复。
- Summary 后的新 Entry 正确进入 AgentContext。
- Terminal 后继续追加被拒绝。
- HistoryPage 返回完整 Entry 且顺序为旧到新。
- ToolCall 与 ToolResult 跨页加载后可以由 Frontend 重新组合。
