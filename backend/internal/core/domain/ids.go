package domain

import (
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// IDs are distinct types so a repository cannot accidentally swap identifiers
// between aggregates while all persistence formats can still use strings.
type (
	TaskSessionID       string
	AgentThreadID       string
	AgentRunID          string
	WorkItemID          string
	CapabilityGrantID   string
	TaskPacketID        string
	ContextManifestID   string
	DelegationRequestID string
	WorkspaceLeaseID    string
	AgentResultID       string
	BriefingID          string
	DeliveryID          string
	EventID             string
	NoteID              string
)

var idSequence uint64

func newOpaqueID(prefix string) string {
	sequence := atomic.AddUint64(&idSequence, 1)
	return prefix + "_" + strconv.FormatInt(time.Now().UnixNano(), 36) + "_" + strconv.FormatUint(sequence, 36)
}

func idIsEmpty(value string) bool { return strings.TrimSpace(value) == "" }

func NewTaskSessionID() TaskSessionID { return TaskSessionID(newOpaqueID("task")) }
func NewAgentThreadID() AgentThreadID { return AgentThreadID(newOpaqueID("thread")) }
func NewAgentRunID() AgentRunID       { return AgentRunID(newOpaqueID("run")) }
func NewWorkItemID() WorkItemID       { return WorkItemID(newOpaqueID("work")) }
func NewCapabilityGrantID() CapabilityGrantID {
	return CapabilityGrantID(newOpaqueID("grant"))
}
func NewTaskPacketID() TaskPacketID           { return TaskPacketID(newOpaqueID("packet")) }
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

func (id TaskSessionID) String() string       { return string(id) }
func (id AgentThreadID) String() string       { return string(id) }
func (id AgentRunID) String() string          { return string(id) }
func (id WorkItemID) String() string          { return string(id) }
func (id CapabilityGrantID) String() string   { return string(id) }
func (id TaskPacketID) String() string        { return string(id) }
func (id ContextManifestID) String() string   { return string(id) }
func (id DelegationRequestID) String() string { return string(id) }
func (id WorkspaceLeaseID) String() string    { return string(id) }
func (id AgentResultID) String() string       { return string(id) }
func (id BriefingID) String() string          { return string(id) }
func (id DeliveryID) String() string          { return string(id) }
func (id EventID) String() string             { return string(id) }
func (id NoteID) String() string              { return string(id) }
