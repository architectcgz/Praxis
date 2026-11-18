# Praxis Session History 与 JSONL 持久化设计整理

## 1. 总体职责

```text
Coordinator
    ↓
Run 生命周期、Loop、Cancel、Timeout、Subagent、Recovery 决策

Core / Session
    ↓
Agent Runtime Context
Entry 提交
HistoryPage
Runtime Context Recovery

Storage / JSONL
    ↓
每 Agent 一个 Writer
Append / Sync / Close
Reverse Record Scanner
byte-offset Cursor
文件完整性恢复

Context Builder
    ↓
接收已经有效的 AgentContext
构造 Provider Model Context

Wails
    ↓
Entry → dto.Entry
1:1 DTO 映射

Frontend
    ↓
保存已加载 History
视觉聚合与展示
```

核心原则：

> JSONL 保存完整 History；AgentContext 只保存当前真正有效的 Model Context；ContextBuilder 不负责从完整 History 中筛选 Entry。

---

# Entry 命名

## 2. 核心类型统一命名为 Entry

原有：

```go
type ContextEntry struct {
    ...
}
```

统一重命名为：

```go
type Entry struct {
    ...
}
```

位于：

```text
core/entry
```

代码中使用：

```go
entry.Entry
```

不再以 `ContextEntry` 作为正式领域类型名称。

---

## 3. 不新增 HistoryEntry 类型

不定义：

```go
type HistoryEntry struct {
    ...
}
```

因为同一个 Entry 既可以存在于完整 History 中，也可能进入 AgentContext。

例如：

```text
User
Assistant
ToolResult
Summary
```

既是 History 中的 Entry，也是 Model Context 的组成部分。

因此底层领域对象统一为：

```text
Entry
```

而不是根据其当前所在集合命名。

---

## 4. “History Entry”只是语境描述

文档或讨论中可以说：

```text
History Entry
```

其含义只是：

> 当前正在讨论 History 中的一条 `entry.Entry`。

它不是新的代码类型。

同理，如果需要描述：

```text
model-context Entry
```

表示：

> 当前会进入 AgentContext 的某条 `entry.Entry`。

也不是新的代码类型。

---

## 5. 不再使用 Context Entry 作为正式术语

正式术语统一为：

```text
Entry
History
AgentContext
JSONL Record
Event
```

定义：

```text
Entry
= Agent 执行过程中产生并持久化的一条不可变领域记录

History
= 某个 Agent 按顺序保存的全部 Entry

AgentContext
= 当前真正参与模型上下文的 Entry 集合

JSONL Record
= Entry 编码进 JSONL 后的一条物理记录

Event
= 实时通知，不等于 Entry
```

避免：

```text
ContextEntry
HistoryEntry
History Fact
History Event
Context Record
```

等重复或容易混淆的概念。

---

# Agent History

## 6. Agent 是 History 的一级作用域

每个 Agent 拥有独立线性 JSONL：

```text
Session
  ├── Main Agent
  │     └── main.jsonl
  ├── Subagent A
  │     └── subagents/A.jsonl
  └── Subagent B
        └── subagents/B.jsonl
```

History API 显式传：

```go
agentID identity.AgentID
```

不建立：

```text
EntryID -> AgentID
```

这样的 Session 全局映射。

---

## 7. Agent History 相互独立

一个 Agent 的模型上下文只依赖：

```text
自己的 History
+
自己的 Runtime Context
```

不设计：

```text
Main Entry → Subagent JSONL Entry
Subagent Entry → Main JSONL Entry
```

这样的跨 Agent History Lookup。

跨 Agent 信息通过 Runtime 传递；真正进入目标 Agent Model Context 时，在目标 Agent 自己的 History 中形成新的 `Entry`。

---

## 8. 所有成功持久化的 Entry 都属于 History

JSONL 保存完整 History。

包括：

```text
User / Assistant
ToolCall / ToolResult
ModelRequest Started / Completed / Failed
Retry
Summary
RunStarted
RunCompleted
RunFailed
RunCancelled
RunInterrupted
Usage
其他持久化执行事实
```

不再设计：

```go
historyVisible(...)
includeInHistory(...)
isFrontendEntry(...)
```

之类的概念。

一个 Entry 是否最终形成独立 UI 组件，与它是否属于 History 无关。

---

## 9. 所有 Agent History Entry 都必须属于某个 Run

每个 Agent JSONL 中的 `Entry` 必须有明确：

```go
RunID
```

包括：

```text
User Input
Assistant
ToolCall / ToolResult
ModelRequest
Retry
Summary
Run lifecycle
```

如果未来存在：

```text
Session rename
Agent 配置变化
Model 配置变化
```

等不属于某个 Run 的数据，则由 Session / Configuration 自己的存储负责，不塞入 Agent History JSONL。

---

# Runtime Context

## 10. AgentContext 不是 History Cache

AgentContext 定义为：

> 当前真正有效的 Model Context。

即：

```text
JSONL
= 完整 History

AgentContext
= 真正会进入模型下一次请求的 Entry
```

这两个集合不同。

---

## 11. AgentContext 只保存模型需要的 Entry

例如完整 History：

```text
UserInput
RunStarted
ModelRequestStarted
ModelRequestFailed
Retry
ModelRequestStarted
Assistant
ToolResult
RunCompleted
```

JSONL 全部保存。

但 AgentContext 可能只有：

```text
UserInput
Assistant
ToolResult
```

以下执行事实不会进入 AgentContext：

```text
RunStarted
RunCompleted
RunFailed
RunCancelled
RunInterrupted

ModelRequestStarted
ModelRequestCompleted
ModelRequestFailed

Retry
Usage
```

它们仍然是完整 History，只是不属于模型上下文。

---

## 12. AgentContext 直接保存 Entry

推荐：

```go
type AgentContext struct {
    Entries []*entry.Entry
}
```

不新增：

```go
ModelContextEntry
ContextRecord
HistoryEntry
```

等重复类型。

区别只在于：

> 哪些 `entry.Entry` 被加入 AgentContext。

---

## 13. 不在 Entry 中增加 ModelVisible 字段

不增加：

```go
ModelVisible bool
Context bool
```

之类的持久化状态。

某种 Entry 是否参与 Model Context，由其语义决定，而不是每条 Entry 再保存一个布尔值。

概念上可以提供：

```go
func ContributesToModelContext(e *Entry) bool
```

它表达 Entry 自身的领域语义，不属于 ContextBuilder 的筛选职责。

---

## 14. Model Context Entry 的基本规则

概念上：

```text
User / Parent Input
→ yes

Assistant
→ yes

ToolCall
→ yes

ToolResult
→ yes

Subagent Report
→ yes

Summary
→ yes


RunStarted
RunCompleted
RunFailed
RunCancelled
RunInterrupted
→ no


ModelRequestStarted
ModelRequestCompleted
ModelRequestFailed
Retry
Usage
→ no
```

具体规则由 `core/entry` 的 Entry 语义决定。

---

## 15. Entry 提交流程

所有 Entry 都先进入 JSONL：

```text
构造 Entry
↓
Storage.Append
↓
完整 History 已提交
```

然后：

```text
ContributesToModelContext?
├── yes
│   ↓
│ AgentContext append
│
└── no
    ↓
  不进入 AgentContext
```

因此仍然严格满足：

```text
persist first
↓
memory second
```

只是并非所有 History Entry 都需要更新 Runtime Context。

---

## 16. Summary 的 Runtime 行为

Summary 本身属于 Model Context。

但它不是普通 append。

例如原 AgentContext：

```text
Summary1
User
Assistant
ToolCall
ToolResult
```

生成新的：

```text
Summary2
```

流程：

```text
Storage.Append(Summary2)
↓
成功
↓
AgentContext = [Summary2]
```

之后再逐步追加新的 model-context Entry：

```text
Summary2
User
Assistant
ToolResult
```

旧 Entry 继续保存在 JSONL。

---

# Context Builder

## 17. ContextBuilder 不负责 History 筛选

ContextBuilder 不应该接收：

```text
完整 History
```

然后自己判断：

```text
RunStarted 要不要？
Retry 要不要？
ModelRequestFailed 要不要？
```

这是错误职责边界。

---

## 18. ContextBuilder 接收已经有效的 AgentContext

正确链路：

```text
Session AgentContext
    ↓
已经只包含 Model Context Entry
    ↓
ContextBuilder
    ↓
Provider Context
```

ContextBuilder 负责：

```text
Entry → Provider 协议编码
Summary 表达
ToolCall / ToolResult 编码
token budget
context encoding
```

不负责：

```text
History → Model Context
```

的业务筛选。

---

## 19. 三个概念必须严格区分

```text
JSONL History
================================================
所有持久化 Entry


AgentContext
================================================
当前真正有效的 Model Context


ContextBuilder
================================================
把 AgentContext 转换为 Provider Request Context
```

避免重新把 History、Runtime Context 和 Provider Context 混在一起。

---

# HistoryPage 与 Frontend

## 20. HistoryPage 返回完整 History

接口：

```go
HistoryPage(
    ctx context.Context,
    agentID identity.AgentID,
    cursor *int64,
    limit int,
)
```

返回：

```go
type HistoryPage struct {
    Entries    []entry.Entry
    NextCursor int64
    HasMore    bool
}
```

`limit` 表示：

> 最多多少个真实 History Entry。

不表示视觉组件数量。

---

## 21. Backend 不为 UI 过滤 History

以下内容都正常返回：

```text
Summary
ModelRequestStarted
ModelRequestFailed
Retry
RunCompleted
ToolResult
...
```

即使 Frontend 最终：

```text
聚合
折叠
不单独生成 DOM
```

Backend 也不删除这些历史 Entry。

---

## 22. Wails 保持 1:1 DTO 映射

```text
entry.Entry[]
↓
Wails
↓
dto.Entry[]
```

原则：

```text
len(input) == len(output)
```

Wails 不做：

```text
过滤
Tool 聚合
ModelRequest 聚合
Summary 隐藏
Retry 隐藏
```

---

## 23. dto.Entry 不是最终视觉单元

Frontend：

```text
dto.Entry[]
↓
buildEntryHistory()
↓
History ViewModel
↓
UI
```

最终一个视觉组件可以由多个 Entry 构成。

---

## 24. ToolCall 与 ToolResult 保持独立 Entry

历史：

```text
Assistant Entry
  └── ToolCall(callID)

Tool Entry
  └── ToolResult(callID)
```

Frontend 根据：

```text
callID
```

组合显示。

不能为了 UI 修改之前已经持久化的 Assistant Entry。

---

## 25. ToolCall 与 ToolResult 允许跨 HistoryPage

例如：

```text
当前页：
ToolResult(call-1)

更旧页：
Assistant ToolCall(call-1)
```

Frontend prepend 后重新：

```text
buildEntryHistory(allLoadedEntries)
```

即可重新组合。

Backend 不为视觉分组 over-read。

---

## 26. ModelRequest 完整返回 Frontend

以下 Entry 全部可恢复：

```text
ModelRequestStarted
ModelRequestFailed
ModelRequestCompleted
Retry
```

例如：

```text
attempt 1 failed
↓
retry
↓
attempt 2 completed
```

重新打开 Session 后仍然能够恢复真实执行历史。

---

## 27. Summary 返回 Frontend

Summary 同时承担：

```text
Runtime Context checkpoint
+
Context compression 历史记录
```

Frontend 可以展示：

```text
──────── Context Compressed ────────
```

等视觉 Marker。

---

# Cursor 与 Reverse Scanner

## 28. Cursor 使用 JSONL byte offset

Cursor 定义为：

> 下一次反向读取的 exclusive end byte offset。

例如：

```text
0        100        230        300
│ Entry A │ Entry B  │ Entry C  │ EOF
```

第一次：

```go
HistoryPage(ctx, agentID, nil, 2)
```

从 EOF=300 读取：

```text
C
B
```

对外返回：

```text
[B, C]
```

以及：

```text
NextCursor = 100
HasMore    = true
```

---

## 29. Cursor 使用 `*int64`

```text
nil
= 从当前 EOF 开始

0
= BOF

N > 0
= 从 N 之前继续读取
```

Frontend 使用：

```ts
number | null
```

即可。

不使用：

```text
string
Base64
opaque token
```

---

## 30. Cursor 不编码 AgentID

`agentID` 已经是 HistoryPage 参数。

因此 Cursor 只表示当前 Agent JSONL 的 byte offset。

---

## 31. Cursor 必须是合法 Record Boundary

要求：

```text
0 <= cursor <= fileSize
```

若：

```text
cursor > 0
```

还应满足：

```text
file[cursor-1] == '\n'
```

否则返回：

```go
ErrInvalidCursor
```

不自动修正。

---

## 32. Cursor 对 Append 天然稳定

JSONL 是 append-only。

旧 Cursor：

```text
100
```

永远表示：

```text
[0,100)
```

后续 EOF 追加不会改变其语义。

因此删除：

```text
headEntryId
beforeEntryId
snapshotHead
```

---

## 33. HistoryPage 返回旧 → 新

Reverse Scanner 内部：

```text
新 → 旧
```

API 对外：

```text
旧 → 新
```

Frontend 可直接：

```ts
entries = [...page.entries, ...entries]
```

---

## 34. Reverse Scanner 从文件尾部按块读取

JSONL 每个 Entry 独占一行：

```text
Entry A\n
Entry B\n
Entry C\n
```

所谓 Block 只是 I/O buffer，例如：

```text
64KB
```

用于从 EOF 向前高效寻找换行。

不是 JSONL 逻辑结构。

---

## 35. Record 可以跨 I/O Buffer

Entry 编码后的 JSONL Record 是变长的，Buffer 是固定 byte range。

因此一个 Record 可能横跨多个 Buffer。

跨 Buffer 拼接由：

```text
infra/jsonl
```

内部处理。

Core / Session 不感知。

---

## 36. 不建立全量 Record Index

不维护：

```go
records []logPosition
```

也不维护：

```text
EntryID index
record index
offset array
```

HistoryPage 直接使用 byte offset Cursor 反向扫描。

---

# Runtime Context Recovery

## 37. Session Load 不扫描整个 JSONL

恢复 Runtime Context 时：

```text
EOF
↑
逐条反向读取
↑
找到 latest Summary
```

即可停止。

例如：

```text
Summary1
...
Summary2
User
Assistant
RunCompleted
EOF
```

扫描时：

```text
RunCompleted          → 不进入 AgentContext
Assistant             → 收集
User                  → 收集
Summary2              → 收集并停止
```

最终恢复：

```text
Summary2
User
Assistant
```

---

## 38. Runtime Recovery 只收集 Model Context Entry

从 EOF 向前读取时：

```text
ContributesToModelContext == true
→ 加入待恢复 AgentContext

false
→ 只跳过，不删除 History
```

发现 latest Summary 后结束扫描。

因此无需把完整 History 加载进 Session 内存。

---

## 39. Storage 不理解 Summary 或 Model Context

Storage 只负责：

```text
file
offset
newline record
reverse scan
```

是否：

```text
Summary
Model Context Entry
Run Terminal
```

全部由 Core 判断。

---

# Subagent

## 40. Subagent 是运行时对象

未完成 Subagent 在应用退出后：

```text
不恢复
不继续执行
```

尚未进入 Model Context 的：

```text
pending instruction
pending report
```

只保存在 Run Memory。

---

## 41. 删除 SubagentInstructionReceived / Accepted

删除：

```go
SubagentInstructionReceived
SubagentInstructionAccepted
```

也不新增：

```go
SubagentInstruction
```

Payload。

---

## 42. Parent Instruction 是普通输入 Entry

Main：

```text
ToolCall(send_to_subagent, text)
```

已经记录发送动作。

Subagent 真正消费 instruction 时：

```text
runtime instruction
↓
普通 Input Entry
↓
Append
↓
加入 Subagent AgentContext
```

不额外创造特殊 Instruction 类型。

---

## 43. spawn_subagent task 是 Subagent 初始输入

Main：

```text
ToolCall(spawn_subagent, task)
```

Subagent：

```text
普通 Initial Input Entry
```

---

## 44. 删除 SubagentReportReceived 持久化记录

Report 到达 Parent：

```text
Parent runtime reports
```

即可。

真正被 Parent 纳入 Model Context 时：

```text
构造自包含 Entry
↓
Append
↓
加入 Parent AgentContext
```

---

## 45. 进入 Model Context 的跨 Agent Entry 必须自包含

不能：

```text
只保存 ReportID
↓
以后随机查另一条 Entry 恢复正文
```

进入 Context 的 Entry 必须独立包含模型真正需要的信息。

因此不需要 Storage `byID`。

---

## 46. Event / Run Memory / Entry 严格分层

```text
Realtime Event
= 当前正在发生什么

Run Memory
= 尚未正式进入模型 Context 的运行时协作状态

Entry / JSONL
= 已经正式持久化的不可变历史记录
```

三者不要混合。

---

# Writer

## 47. 每个 Agent JSONL 一个专属 Writer

```text
Main
↓
Main Writer
↓
main.jsonl

Subagent A
↓
Writer A
↓
subagent-a.jsonl
```

不同 Agent Writer 可以并行。

同一个 Agent 的：

```text
Append
Sync
Close
```

全部串行经过同一个 Writer。

---

## 48. Writer 由 Storage 实现持有

不是：

```go
Session.writers
Coordinator.writer
```

而是：

```text
Core Session
↓
HistoryStore interface
↓
Storage implementation
↓
AgentID → agentWriter
```

`agentWriter` 是 infra 实现细节。

---

## 49. Writer 生命周期跟随 Session / Agent History

```text
Session 打开
↓
创建 Writer
↓
打开并持有 JSONL file

Run 1
Run 2
Run 3
↓
持续复用

Session 关闭
↓
Writer Close
```

不是每个 Run 单独创建 Writer。

---

## 50. Coordinator 不接触 Writer

Coordinator 只决定：

```text
产生什么 Entry
Entry 的业务顺序
Run 生命周期
```

不接触：

```text
channel
file
offset
truncate
sync
```

调用链：

```text
Coordinator
↓
Session
↓
HistoryStore
↓
Agent Writer
↓
JSONL
```

---

## 51. Writer 只负责提交顺序，不负责业务排序

并行 Tool：

```text
Tool A
Tool B
Tool C
```

最终应该以什么顺序形成 History，由 Run / Loop 决定。

Writer 只保证：

> 按收到请求的顺序串行写入。

---

## 52. Writer 使用 channel 串行接收命令

概念：

```go
type appendRequest struct {
    entry  entry.Entry
    result chan error
}
```

调用方把请求发送给 Writer，Writer 单 goroutine 顺序处理。

---

## 53. Append 仍然是同步提交

虽然内部使用 channel，但：

```go
Append(...)
```

必须等待 Writer 真正完成 Write 后才能返回。

不能 fire-and-forget。

---

## 54. 进入 Writer channel 之前可以取消

```go
select {
case w.requests <- req:
case <-ctx.Done():
    return ctx.Err()
}
```

如果 context 先取消：

```text
请求未进入 Writer
↓
没有文件操作
↓
安全返回 error
```

---

## 55. 请求一旦进入 Writer channel 就必须等待最终结果

正确：

```go
select {
case w.requests <- req:
case <-ctx.Done():
    return ctx.Err()
}

return <-req.result
```

一旦：

```text
request 已进入 queue
```

就不能再因为调用方 context 取消而提前返回。

否则可能出现：

```text
调用方收到 ctx.Err()
↓
Writer 稍后实际写成功
```

重新产生提交歧义。

---

## 56. Sync 也必须经过同一个 Writer

外部不能直接：

```go
file.Sync()
```

Sync 与 Append 都由 Writer 顺序执行。

---

# Append

## 57. Append 使用 persist-first

统一流程：

```text
构造 Entry
↓
Writer Append
↓
成功
↓
必要时更新 AgentContext
```

删除：

```text
PendingEntries
PendingNodes
IndexedNodes
syncHistory
```

---

## 58. Append 的精确定义

`Append`：

> 要么完整提交一条 JSONL Record，要么恢复到调用前状态并返回 error。

调用者只面对：

```text
nil
→ Record 已完整存在

error
→ Record 未提交，或者 Storage 已进入严重不可用状态
```

---

## 59. Partial Write 不能保留

Append 前记录：

```text
startOffset
```

然后：

```go
n, err := file.Write(record)
```

只有：

```text
err == nil
&&
n == len(record)
```

才成功。

否则：

```text
Truncate(startOffset)
↓
return error
```

不保留半条 JSONL Record。

---

## 60. Storage 不自动 Retry Append

Short / partial write 后：

```text
rollback
↓
return error
```

是否继续执行由上层决定。

Storage 不偷偷 retry。

---

## 61. Truncate 失败是严重 Storage Error

如果：

```text
Write 失败
↓
Truncate 也失败
```

则日志完整性已经无法保证。

之后不能继续正常向该 Agent JSONL 写入。

---

## 62. Append 与 Crash Recovery 分开

正常 partial write：

```text
Append 内 rollback
```

进程突然退出留下 incomplete tail：

```text
下一次启动由 Storage Recovery 修复
```

这两个问题不通过 Pending 状态混在一起。

---

## 63. Append 成功后、更新内存前 Crash 可以自然恢复

可能出现：

```text
Storage.Append 成功
↓
Entry 已在 JSONL
↓
进程退出
↓
AgentContext 尚未更新
```

重启后：

```text
JSONL
↓
Runtime Context Recovery
```

自然重新恢复该 Entry。

因此不需要 Pending。

---

# Sync 与 Run Terminal

## 64. Append 与 Sync 是不同保证

```text
Append
= 当前 OS 文件视图中完整 Record 已存在

Sync
= 更强 durability boundary
```

不追求每条 Entry 的断电级 durability。

---

## 65. 一次 Run 只 Sync 一次

规则：

> Terminal Entry 成功 Append 后执行一次 Sync。

```text
RunCompleted
↓
Append
↓
Sync
```

Failed / Cancelled / Interrupted 同理。

User Input 不单独 Sync。

---

## 66. Run 保留四种 Terminal 类型

```go
RunCompleted
RunFailed
RunCancelled
RunInterrupted
```

不压成：

```go
RunEnded{Status: ...}
```

Frontend 需要根据不同类型展示不同终态。

---

## 67. 三种正常终态与异常终态

```text
RunCompleted
RunFailed
RunCancelled
```

是明确正常产生的终态。

```text
RunInterrupted
```

表示：

> Run 没有产生前三种正常终态，但执行环境已经停止，需要异常闭合 History。

---

## 68. Terminal 是 Run 最后一条 Entry

强制 invariant：

> Terminal Entry 后不允许继续向该 Run 写任何 Entry。

一个 Run 最多一个 Terminal。

Coordinator 是这个 invariant 的唯一业务权威。

---

## 69. Terminal Append 与 Sync 必须连续

不能：

```text
Append RunCompleted(run-1)
↓
Append UserInput(run-2)
↓
Sync
```

Terminal durability boundary 应保持：

```text
Append Terminal
↓
Sync
```

之间不插入同 Agent 的其他 Append。

实现可以采用：

```text
AppendTerminalAndSync
```

单个 Writer command，或者由上层保证同等原子顺序。

---

# Recovery

## 70. Storage Recovery

首先处理物理 JSONL 问题：

```text
incomplete tail
partial trailing record
```

Storage 不理解 Run 或 Summary。

---

## 71. Run Recovery 只看最后一条完整 Entry

由于：

```text
所有 Entry 都有 RunID
Terminal 必须是 Run 最后一条 Entry
```

所以无需重放 Run 状态。

读取最后一个完整 Record。

如果是：

```text
RunCompleted
RunFailed
RunCancelled
RunInterrupted
```

则无需处理。

否则：

```text
runID = lastEntry.RunID
↓
Append RunInterrupted
↓
Sync
```

---

## 72. Run Recovery 天然幂等

第一次补：

```text
RunInterrupted
```

以后最后一条就是 Terminal，因此不会重复追加。

---

## 73. Empty History 无需 Run Recovery

如果没有完整 Entry：

```text
无需处理
```

---

## 74. RunInterrupted 只闭合 History

它不表示：

```text
恢复旧 Loop
继续旧 Run
恢复 Subagent
```

新的用户操作启动新的 Run。

---

## 75. Runtime Context Recovery 与 Run Recovery 分离

Run Recovery：

```text
只读最后一个 Record
```

Runtime Context Recovery：

```text
从 EOF 向前扫描到 latest Summary
```

目的不同，虽然都可以复用 Reverse Scanner。

---

## 76. Startup 推荐顺序

```text
Storage 修复物理 JSONL
↓
Run Recovery
↓
必要时补 RunInterrupted + Sync
↓
Runtime Context Recovery
↓
恢复 latest Summary + 后续有效 Model Context Entry
```

这样 Session 恢复出来的是已经语义闭合后的最终状态。

---

# Projection 与 Repository

## 77. 不长期维护重复业务 Projection

Storage / Session 不长期维护：

```text
Run projection
ModelRequest projection
ToolCall projection
```

当前执行状态由 Coordinator / Run 持有。

历史状态从 Entry 推导。

---

## 78. Repository 不承载 JSONL 实现细节

以下类型不应作为通用 Repository 抽象：

```text
LogPosition
IndexedEntry
HistoryUpdate
RunLocation
```

依赖：

```text
offset
length
record
writer
JSONL
```

的类型属于：

```text
infra/storage
infra/jsonl
```

---

# 推荐接口形态

## 79. HistoryStore

概念上：

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
    ) (HistoryRecords, error)

    Sync(
        context.Context,
        identity.AgentID,
    ) error
}
```

实现时也可以把 Terminal durability 封装成：

```go
CommitTerminal(
    context.Context,
    identity.AgentID,
    entry.Entry,
) error
```

内部：

```text
Append terminal
↓
Sync
```

---

# 最终数据流

## 80. History-only Entry

例如：

```text
ModelRequestStarted
```

流程：

```text
构造 Entry
↓
Append
↓
JSONL History
↓
不加入 AgentContext
```

---

## 81. Model Context Entry

例如：

```text
Assistant
ToolResult
User Input
```

流程：

```text
构造 Entry
↓
Append
↓
成功
↓
加入 AgentContext
```

---

## 82. Compression

```text
AgentContext
↓
生成 Summary Entry
↓
Append
↓
成功
↓
AgentContext = [Summary]
```

---

## 83. ContextBuilder

```text
AgentContext
↓
ContextBuilder
↓
Provider Context
```

ContextBuilder 不读取完整 History，也不进行 History 筛选。

---

## 84. HistoryPage

```text
Frontend cursor
↓
Session HistoryPage
↓
Storage Reverse Scanner
↓
完整 Entry[]
↓
Wails 1:1 dto.Entry[]
↓
Frontend
↓
buildEntryHistory()
```

---

## 85. 并行 Tool

```text
Tool A ─┐
Tool B ─┼─→ Run / Loop 收集结果
Tool C ─┘
             ↓
       决定业务顺序
             ↓
          Entry
             ↓
        Agent Writer
             ↓
           JSONL
```

Writer 只负责串行提交，不决定业务排序。

---

## 86. Terminal Run

```text
Coordinator 决定 terminal
↓
构造 Terminal Entry
↓
Session 提交
↓
Agent Writer
↓
Append
↓
Sync
↓
完成 terminal commit
```

Terminal Entry 不进入 AgentContext。

---

## 87. Recovery

```text
应用启动
↓
Storage 修复 JSONL
↓
读取最后一条完整 Entry
↓
Terminal?
├── yes
│   └── 无需 Run Recovery
└── no
    ↓
    Append RunInterrupted
    ↓
    Sync
↓
反向恢复 AgentContext
```

---

# 最终原则

1. 核心领域类型统一命名为 `entry.Entry`。
2. `ContextEntry` 作为正式类型和正式术语退出。
3. 不新增 `HistoryEntry` 类型。
4. “History Entry”只是对 History 中某条 `Entry` 的语境描述。
5. “model-context Entry”只是对进入 AgentContext 的某条 `Entry` 的语境描述。
6. `JSONL Record` 表示 Entry 编码后的物理记录。
7. JSONL 是完整 History 的事实来源。
8. 所有成功持久化的 Entry 天然属于 History。
9. 所有 Agent History Entry 都必须属于某个 Run。
10. Agent 是 History 一级作用域。
11. Main / Subagent 使用独立 JSONL。
12. 不设计跨 Agent Entry Lookup。
13. AgentContext 不是 History Cache。
14. AgentContext 只保存当前有效 Model Context。
15. AgentContext 直接保存 `[]*entry.Entry`。
16. 不为 AgentContext 再创建另一种 Entry 类型。
17. 不在 Entry 中增加 `ModelVisible` 等冗余持久化字段。
18. Entry 是否参与 Model Context 由其自身语义决定。
19. ContextBuilder 不负责从完整 History 中筛选 Entry。
20. ContextBuilder 只接收已经有效的 AgentContext。
21. JSONL History、AgentContext、ContextBuilder 三层职责严格分离。
22. History-only Entry 仍完整持久化，但不进入 AgentContext。
23. Summary 属于 Model Context，并替换旧 AgentContext。
24. Runtime Recovery 只收集有效 Model Context Entry。
25. Runtime Recovery 遇到 latest Summary 后停止。
26. 删除 Node / PathNodes / IndexedNodes。
27. HistoryPage 返回完整 History，不做 UI filtering。
28. `HistoryPage.Entries` 类型为 `[]entry.Entry`。
29. Wails 对 `entry.Entry` 1:1 映射为 `dto.Entry`。
30. Frontend 负责所有视觉聚合。
31. ToolCall / ToolResult 保持独立 Entry。
32. ModelRequest 失败、Retry、Summary 等都能从 History 恢复。
33. Cursor 是 JSONL exclusive byte offset。
34. Cursor 使用 `*int64`。
35. `nil` 表示从 EOF 开始，`0` 表示 BOF。
36. Cursor 必须处于 Record Boundary。
37. HistoryPage 返回旧 → 新。
38. Reverse Scanner 使用固定 I/O Buffer。
39. Record 跨 Buffer 的拼接属于 jsonl 实现细节。
40. 不构建全量 Record Index。
41. 不维护 Storage byID。
42. Backend 不缓存用户浏览过的旧 History。
43. Frontend 保存已加载 History。
44. Subagent 不跨应用生命周期恢复执行。
45. 删除 SubagentInstructionReceived / Accepted。
46. 不新增 SubagentInstruction Payload。
47. Parent Instruction 在 Subagent History 中表现为普通 Input Entry。
48. 删除 SubagentReportReceived 持久化记录。
49. Pending Instruction / Report 只属于 Run Memory。
50. 真正进入 Model Context 的跨 Agent Entry 必须自包含。
51. Event、Run Memory、Entry 严格分层。
52. 每个 Agent JSONL 使用一个专属 Writer。
53. Writer 由 Storage 实现持有。
54. Writer 生命周期跟随 Session / Agent History。
55. Coordinator 不接触 Writer、channel 或文件。
56. Writer 只负责串行提交，不决定业务顺序。
57. Append 内部可通过 channel 提交给 Writer。
58. Append 仍然具有同步提交语义。
59. 请求进入 Writer channel 前允许 context 取消。
60. 请求进入 channel 后必须等待 Writer 最终结果。
61. 进入 queue 后不能因 `ctx.Done()` 提前返回。
62. Sync 也必须通过同一 Writer。
63. persist-first、memory-second。
64. Append 要么完整成功，要么恢复调用前状态。
65. Partial write 不允许残留。
66. Partial write 时 truncate 回 Append 前 offset。
67. Storage 不自动 Retry Append。
68. rollback 失败属于严重 Storage Error。
69. Append 与 Crash Recovery 分开。
70. Append 与 Sync 是不同 durability 保证。
71. 不追求每条 Entry 的断电级持久化。
72. 一个 Run 只在 Terminal 时 Sync 一次。
73. User Input 不单独 Sync。
74. RunCompleted / RunFailed / RunCancelled / RunInterrupted 是四种独立 Terminal。
75. Completed / Failed / Cancelled 是正常明确终态。
76. Interrupted 是正常终态缺失时的异常闭合。
77. 一个 Run 最多一个 Terminal。
78. Terminal 必须是 Run 最后一条 Entry。
79. Terminal 后禁止继续向该 Run 写 Entry。
80. Terminal Append 与 Sync 之间不能插入同 Agent 其他 Append。
81. Coordinator 是 Run 生命周期唯一权威。
82. Session 不维护第二套 Run 状态机。
83. Run Recovery 只需读取最后一条完整 Entry。
84. 最后一条不是 Terminal 时追加 RunInterrupted。
85. RunInterrupted 后立即 Sync。
86. Run Recovery 天然幂等。
87. RunInterrupted 只闭合 History，不恢复执行。
88. Runtime Context Recovery 与 Run Recovery 是不同问题。
89. Storage 恢复文件，Coordinator 恢复 Run 语义，Session 恢复 Model Context。
90. 不长期维护 Run / ModelRequest / ToolCall 重复 Projection。
91. Repository 不承载 JSONL writer / offset / record 等实现细节。
92. Runtime 内存规模由有效 Model Context 大小决定，而不是完整 History 大小。
93. Session 启动恢复成本主要取决于 latest Summary 距离 EOF 的距离。