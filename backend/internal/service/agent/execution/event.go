package execution

import (
	agentmodel "praxis/internal/core/agent"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
)

const (
	EventEntriesAppended = "entries_appended"
	EventExecutionEnded  = "execution_ended"
	EventRequestCanceled = "request_canceled"
)

// Event 是 Execution Service 在业务提交成功后发布的执行通知。
type Event struct {
	Entries        []sessionentry.ContextEntry
	Kind           string
	SessionID      identity.SessionID
	AgentID        identity.AgentID
	ExecutionID    identity.AgentExecutionID
	Outcome        string
	FailureCode    agentmodel.ExecutionFailureCode
	FailureMessage string
}
