package agent

type AgentState string

// AgentKind 表示展示身份与 Session 的关系，与角色定义的职责无关。
type AgentKind string

const (
	KindMain     AgentKind = "main"
	KindSubagent AgentKind = "subagent"
)

const (
	AgentIdle        AgentState = "idle"
	AgentExecuting   AgentState = "executing"
	AgentWaiting     AgentState = "waiting"
	AgentInterrupted AgentState = "interrupted"
	AgentFailed      AgentState = "failed"
)
