基于当前 `backend/internal/core/event/event.go`，我建议把现在这个“所有事件字段全部塞进一个 `Event`”的设计改成 **Envelope + Scope + Payload** 三层。

当前的问题是类似这样：

```go
type Event struct {
    Transient      bool
    Kind           Kind
    Entries        []sessionentry.ContextEntry
    SessionID      identity.SessionID
    AgentID        identity.AgentID
    ExecutionID    identity.AgentExecutionID
    Outcome        string
    FailureCode    string
    FailureMessage string
    TurnID         identity.TurnID
    StopReason     providerapi.StopReason
    Text           string
    CallID         string
    Name           string
    Input          json.RawMessage
    Result         string
    IsError        bool
    Error          string
    Timing         *timing.View
    Usage          *providerapi.Usage
}
```

它实际上是一个手写的 union，但 Go 类型系统完全不知道：

> `Kind == TextDelta` 时只有 `Text` 合法。

所以完全可能构造出：

```go
Event{
    Kind:   TextDelta,
    CallID: "xxx",
    Usage:  usage,
}
```

这种语义上非法但编译完全合法的对象。

我建议最终改成下面的模型。

### 1. `Event` 只做 envelope

```go
type Event struct {
    Scope   Scope
    Payload Payload
}

func (e Event) Kind() Kind {
    return e.Payload.Kind()
}
```

`Event` 本身不再保存任何具体事件的数据。

---

### 2. 公共标识单独放到 `Scope`

```go
type Scope struct {
    SessionID   identity.SessionID
    AgentID     identity.AgentID
    ExecutionID identity.AgentExecutionID
    TurnID      identity.TurnID
}
```

我更推荐叫 `Scope`，而不是 `Meta`。

因为这些字段表达的其实不是“额外元数据”，而是：

> 这个事件属于哪个 session / agent / execution / turn。

也就是事件的作用域。

暂时不要为了“以后可能有用”加 `EventID`、`Timestamp`、`Sequence`。当前没有需求就不需要。

---

### 3. Payload 使用封闭 interface

```go
type Payload interface {
    Kind() Kind
    isPayload()
}
```

这里的 `isPayload()` 很有用。

因为它是非导出的，意味着只有 `event` package 自己能够定义合法事件类型，外部 package 无法随便实现一个奇怪的 `Payload`。

同时把当前 Kind 常量改名：

```go
type Kind string

const (
    KindEntriesAppended      Kind = "entries_appended"
    KindTurnStarted          Kind = "turn_started"
    KindProviderWaiting      Kind = "provider_waiting"
    KindTextDelta            Kind = "text_delta"
    KindThinkingDelta        Kind = "thinking_delta"
    KindToolCall             Kind = "tool_call"
    KindToolApprovalRequired Kind = "tool_approval_required"
    KindToolResult           Kind = "tool_result"
    KindTurnCompleted        Kind = "turn_completed"
    KindModelUsage           Kind = "model_usage"
    KindExecutionEnded       Kind = "execution_ended"
    KindRequestCanceled      Kind = "request_canceled"
    KindOperationTiming      Kind = "operation_timing"
    KindError                Kind = "error"
)
```

这样 `TextDelta` 这个名字就可以直接留给真正的事件数据类型。

---

### 4. 每类事件独立定义 payload

比如：

```go
type TextDelta struct {
    Text string
}

func (TextDelta) Kind() Kind  { return KindTextDelta }
func (TextDelta) isPayload()  {}
```

```go
type ThinkingDelta struct {
    Text string
}

func (ThinkingDelta) Kind() Kind { return KindThinkingDelta }
func (ThinkingDelta) isPayload() {}
```

这样虽然两个事件现在数据结构相同，但它们仍然是两个明确的业务类型。

不要为了少一个 struct 写成：

```go
type Delta struct {
    Type DeltaType
    Text string
}
```

否则又开始往“大而全的联合类型”方向走了。

---

## 完整映射

当前 `Event` 的字段，我建议拆成：

```go
type EntriesAppended struct {
    Entries   []sessionentry.ContextEntry
    Transient bool
}

func (EntriesAppended) Kind() Kind { return KindEntriesAppended }
func (EntriesAppended) isPayload() {}
```

这里我特意把 `Transient` 从公共 Event 移走。

因为你现在代码自己的注释就是：

> Transient 表示条目只属于当前 Run。

也就是说它实际上是 `EntriesAppended` 的语义，而不是所有事件的公共属性。

---

无 payload 的事件直接定义空类型：

```go
type TurnStarted struct{}

func (TurnStarted) Kind() Kind { return KindTurnStarted }
func (TurnStarted) isPayload() {}
```

```go
type ProviderWaiting struct{}

func (ProviderWaiting) Kind() Kind { return KindProviderWaiting }
func (ProviderWaiting) isPayload() {}
```

```go
type RequestCanceled struct{}

func (RequestCanceled) Kind() Kind { return KindRequestCanceled }
func (RequestCanceled) isPayload() {}
```

不需要因为是空 struct 就继续共用一个 Event。

它们本身就是业务事件类型。

---

Tool 相关：

```go
type ToolCall struct {
    CallID string
    Name   string
    Input  json.RawMessage
}

func (ToolCall) Kind() Kind { return KindToolCall }
func (ToolCall) isPayload() {}
```

```go
type ToolApprovalRequired struct {
    CallID string
    Name   string
    Input  json.RawMessage
}

func (ToolApprovalRequired) Kind() Kind {
    return KindToolApprovalRequired
}

func (ToolApprovalRequired) isPayload() {}
```

```go
type ToolResult struct {
    CallID  string
    Result  string
    IsError bool
}

func (ToolResult) Kind() Kind { return KindToolResult }
func (ToolResult) isPayload() {}
```

这里暂时不要急着抽一个：

```go
type Tool struct {
    CallID string
    Name string
}
```

除非后面确实发现多个事件长期共享且语义完全一致。

否则为了消除三行字段反而增加中间概念，不值得。

---

Turn：

```go
type TurnCompleted struct {
    StopReason providerapi.StopReason
}

func (TurnCompleted) Kind() Kind { return KindTurnCompleted }
func (TurnCompleted) isPayload() {}
```

Usage：

```go
type ModelUsage struct {
    Usage providerapi.Usage
}

func (ModelUsage) Kind() Kind { return KindModelUsage }
func (ModelUsage) isPayload() {}
```

注意这里已经没必要：

```go
Usage *providerapi.Usage
```

因为：

> `ModelUsage` 事件存在，本身就意味着 Usage 存在。

以前使用指针，是因为一个超级 `Event` 中 `Usage` 对绝大多数 Kind 都不存在。

拆分之后这个问题自然消失。

`Timing` 同理：

```go
type OperationTiming struct {
    Timing timing.View
}

func (OperationTiming) Kind() Kind { return KindOperationTiming }
func (OperationTiming) isPayload() {}
```

---

Execution：

```go
type ExecutionEnded struct {
    Outcome        string
    FailureCode    string
    FailureMessage string
}

func (ExecutionEnded) Kind() Kind { return KindExecutionEnded }
func (ExecutionEnded) isPayload() {}
```

错误：

```go
type ErrorOccurred struct {
    Message string
}

func (ErrorOccurred) Kind() Kind { return KindError }
func (ErrorOccurred) isPayload() {}
```

我不建议定义：

```go
type Error struct {
    Error string
}
```

读起来比较奇怪。

---

## 最终结构

我建议 `core/event` 最终大概是：

```text
backend/internal/core/event/
├── event.go
├── payload.go
├── tool.go
└── emitter.go
```

不过事件目前不算多，我甚至更倾向于先保持简单：

```text
core/event/
├── event.go
└── emitter.go
```

`event.go`：

```go
package event

type Kind string

const (
    KindEntriesAppended Kind = "entries_appended"
    ...
)

type Scope struct {
    SessionID   identity.SessionID
    AgentID     identity.AgentID
    ExecutionID identity.AgentExecutionID
    TurnID      identity.TurnID
}

type Event struct {
    Scope   Scope
    Payload Payload
}

func (e Event) Kind() Kind {
    return e.Payload.Kind()
}

type Payload interface {
    Kind() Kind
    isPayload()
}

// ---------- entries ----------

type EntriesAppended struct {
    Entries   []sessionentry.ContextEntry
    Transient bool
}

// ---------- turn ----------

type TurnStarted struct{}

type TurnCompleted struct {
    StopReason providerapi.StopReason
}

// ---------- model ----------

type ProviderWaiting struct{}

type TextDelta struct {
    Text string
}

type ThinkingDelta struct {
    Text string
}

type ModelUsage struct {
    Usage providerapi.Usage
}

// ---------- tool ----------

type ToolCall struct {
    CallID string
    Name   string
    Input  json.RawMessage
}

type ToolApprovalRequired struct {
    CallID string
    Name   string
    Input  json.RawMessage
}

type ToolResult struct {
    CallID  string
    Result  string
    IsError bool
}

// ---------- execution ----------

type ExecutionEnded struct {
    Outcome        string
    FailureCode    string
    FailureMessage string
}

type RequestCanceled struct{}

type OperationTiming struct {
    Timing timing.View
}

type ErrorOccurred struct {
    Message string
}
```

然后分别实现 `Kind()` 和 `isPayload()`。

---

## 生产事件会明显干净

现在可能类似：

```go
emitter.Emit(event.Event{
    Kind:        event.TextDelta,
    SessionID:   sessionID,
    AgentID:     agentID,
    ExecutionID: executionID,
    TurnID:      turnID,
    Text:        delta,
})
```

改成：

```go
emitter.Emit(event.Event{
    Scope: event.Scope{
        SessionID:   sessionID,
        AgentID:     agentID,
        ExecutionID: executionID,
        TurnID:      turnID,
    },
    Payload: event.TextDelta{
        Text: delta,
    },
})
```

这时候下面这种状态在类型上已经不可能出现：

```go
Kind: TextDelta
CallID: xxx
Usage: xxx
Timing: xxx
```

因为 `TextDelta` 只有：

```go
Text string
```

---

## 消费端也应该按 payload 类型处理

不要再写：

```go
switch e.Kind {
case event.TextDelta:
    use(e.Text)

case event.ToolCall:
    use(e.CallID, e.Name, e.Input)
}
```

改成：

```go
switch payload := e.Payload.(type) {
case event.TextDelta:
    use(payload.Text)

case event.ToolCall:
    use(payload.CallID, payload.Name, payload.Input)

case event.ToolResult:
    use(payload.CallID, payload.Result, payload.IsError)
}
```

如果某些地方只需要判断 Kind：

```go
if e.Kind() == event.KindTextDelta {
    ...
}
```

所以 `Kind` 仍然存在，但它是 **Payload 类型的派生属性**，而不是另一个可以和 payload 产生冲突的状态字段。

这是这次重构中我认为最重要的一点：

```text
不要：

Kind + Payload
  ↓
两份状态，需要保证一致

而要：

Payload
  ↓
Kind()
  ↓
由类型唯一决定
```

---

## SSE / 前端的数据结构

内部使用 interface 不代表 HTTP 层也直接 marshal interface。

传输层单独映射成：

```json
{
  "kind": "tool_result",
  "session_id": "...",
  "agent_id": "...",
  "execution_id": "...",
  "turn_id": "...",
  "data": {
    "call_id": "...",
    "result": "...",
    "is_error": false
  }
}
```

也就是：

```text
Event
├── Scope
│   ├── SessionID
│   ├── AgentID
│   ├── ExecutionID
│   └── TurnID
│
└── Payload
    └── ToolResult
        ├── CallID
        ├── Result
        └── IsError
```

传输后：

```text
{
    kind,
    session_id,
    agent_id,
    execution_id,
    turn_id,
    data
}
```

这样 core 领域模型和前端协议也都很清晰。

### 我建议的最终原则

**公共作用域信息：**

```go
Scope
```

只放：

```text
SessionID
AgentID
ExecutionID
TurnID
```

**事件公共容器：**

```go
Event {
    Scope
    Payload
}
```

**事件类型：**

```text
EntriesAppended
TurnStarted
ProviderWaiting
TextDelta
ThinkingDelta
ToolCall
ToolApprovalRequired
ToolResult
TurnCompleted
ModelUsage
ExecutionEnded
RequestCanceled
OperationTiming
ErrorOccurred
```

每个事件自己拥有自己的数据。

这比给每一种 event 都定义完整 `AgentEvent`，也比现在一个巨大 `AgentEvent` 放十几个 nullable 字段都更合适。`Emitter` 本身基本不需要改，只需要继续从 `func(Event)` 接收新的 envelope 即可。