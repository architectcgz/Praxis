package contracts

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync/atomic"
)

// IDs are distinct types so a repository cannot accidentally swap identifiers
// between aggregates while all persistence formats can still use strings.
type (
	ProjectID             string
	WorkspaceID           string
	SessionID             string
	AgentDefinitionID     string
	AgentID               string
	AgentExecutionID      string
	ToolInvocationID      string
	RequestID             string
	WaitConditionID       string
	AgentControlCommandID string
	WorkItemID            string
	ContextEntryID        string
)

var idSequence uint64

func newOpaqueID(prefix string) string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err == nil {
		return prefix + "_" + hex.EncodeToString(value[:])
	}
	sequence := atomic.AddUint64(&idSequence, 1)
	return fmt.Sprintf("%s_fallback_%d", prefix, sequence)
}

func NewProjectID() ProjectID     { return ProjectID(newOpaqueID("project")) }
func NewWorkspaceID() WorkspaceID { return WorkspaceID(newOpaqueID("workspace")) }
func NewSessionID() SessionID     { return SessionID(newOpaqueID("session")) }
func NewAgentID() AgentID         { return AgentID(newOpaqueID("agent")) }
func NewAgentExecutionID() AgentExecutionID {
	return AgentExecutionID(newOpaqueID("execution"))
}
func NewToolInvocationID() ToolInvocationID { return ToolInvocationID(newOpaqueID("toolinvocation")) }
func NewRequestID() RequestID               { return RequestID(newOpaqueID("request")) }
func NewWaitConditionID() WaitConditionID {
	return WaitConditionID(newOpaqueID("wait"))
}
func NewAgentControlCommandID() AgentControlCommandID {
	return AgentControlCommandID(newOpaqueID("control"))
}
func NewWorkItemID() WorkItemID         { return WorkItemID(newOpaqueID("work")) }
func NewContextEntryID() ContextEntryID { return ContextEntryID(newOpaqueID("context")) }

func (id ProjectID) String() string         { return string(id) }
func (id WorkspaceID) String() string       { return string(id) }
func (id SessionID) String() string         { return string(id) }
func (id AgentDefinitionID) String() string { return string(id) }
func (id AgentID) String() string           { return string(id) }
func (id AgentExecutionID) String() string  { return string(id) }
func (id ToolInvocationID) String() string  { return string(id) }
func (id RequestID) String() string         { return string(id) }
func (id WaitConditionID) String() string   { return string(id) }
func (id AgentControlCommandID) String() string {
	return string(id)
}
func (id WorkItemID) String() string     { return string(id) }
func (id ContextEntryID) String() string { return string(id) }
