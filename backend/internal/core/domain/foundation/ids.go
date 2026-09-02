package foundation

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync/atomic"
)

// IDs are distinct types so a repository cannot accidentally swap identifiers
// between aggregates while all persistence formats can still use strings.
type (
	ProjectID             string
	WorkspaceID           string
	SessionID             string
	AgentID               string
	AgentExecutionID      string
	RequestID             string
	WaitConditionID       string
	AgentControlRequestID string
	WorkItemID            string
	CapabilityGrantID     string
	ContextManifestID     string
	DelegationRequestID   string
	WorkspaceLeaseID      string
	AgentResultID         string
	BriefingID            string
	DeliveryID            string
	EventID               string
	NoteID                string
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

func idIsEmpty(value string) bool { return strings.TrimSpace(value) == "" }

func NewProjectID() ProjectID     { return ProjectID(newOpaqueID("project")) }
func NewWorkspaceID() WorkspaceID { return WorkspaceID(newOpaqueID("workspace")) }
func NewSessionID() SessionID     { return SessionID(newOpaqueID("session")) }
func NewAgentID() AgentID         { return AgentID(newOpaqueID("agent")) }
func NewAgentExecutionID() AgentExecutionID {
	return AgentExecutionID(newOpaqueID("execution"))
}
func NewRequestID() RequestID { return RequestID(newOpaqueID("request")) }
func NewWaitConditionID() WaitConditionID {
	return WaitConditionID(newOpaqueID("wait"))
}
func NewAgentControlRequestID() AgentControlRequestID {
	return AgentControlRequestID(newOpaqueID("control"))
}
func NewWorkItemID() WorkItemID { return WorkItemID(newOpaqueID("work")) }
func NewCapabilityGrantID() CapabilityGrantID {
	return CapabilityGrantID(newOpaqueID("grant"))
}
func NewContextManifestID() ContextManifestID { return ContextManifestID(newOpaqueID("manifest")) }
func NewDelegationRequestID() DelegationRequestID {
	return DelegationRequestID(newOpaqueID("delegation"))
}
func NewWorkspaceLeaseID() WorkspaceLeaseID { return WorkspaceLeaseID(newOpaqueID("lease")) }
func NewAgentResultID() AgentResultID       { return AgentResultID(newOpaqueID("result")) }
func NewBriefingID() BriefingID             { return BriefingID(newOpaqueID("briefing")) }
func NewDeliveryID() DeliveryID             { return DeliveryID(newOpaqueID("delivery")) }
func NewEventID() EventID                   { return EventID(newOpaqueID("event")) }
func NewNoteID() NoteID                     { return NoteID(newOpaqueID("note")) }

func (id ProjectID) String() string        { return string(id) }
func (id WorkspaceID) String() string      { return string(id) }
func (id SessionID) String() string        { return string(id) }
func (id AgentID) String() string          { return string(id) }
func (id AgentExecutionID) String() string { return string(id) }
func (id RequestID) String() string        { return string(id) }
func (id WaitConditionID) String() string  { return string(id) }
func (id AgentControlRequestID) String() string {
	return string(id)
}
func (id WorkItemID) String() string          { return string(id) }
func (id CapabilityGrantID) String() string   { return string(id) }
func (id ContextManifestID) String() string   { return string(id) }
func (id DelegationRequestID) String() string { return string(id) }
func (id WorkspaceLeaseID) String() string    { return string(id) }
func (id AgentResultID) String() string       { return string(id) }
func (id BriefingID) String() string          { return string(id) }
func (id DeliveryID) String() string          { return string(id) }
func (id EventID) String() string             { return string(id) }
func (id NoteID) String() string              { return string(id) }
