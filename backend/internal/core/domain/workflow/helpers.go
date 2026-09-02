package workflow

import (
	"strings"
	"time"

	domaincontext "praxis/internal/core/domain/context"
	"praxis/internal/core/domain/execution"
	foundation "praxis/internal/core/domain/foundation"
	"praxis/internal/core/domain/security"
)

type (
	AgentID                = foundation.AgentID
	AgentControlRequestID  = foundation.AgentControlRequestID
	AgentExecutionID       = foundation.AgentExecutionID
	AgentResultID          = foundation.AgentResultID
	ApprovalRecord         = security.ApprovalRecord
	AgentProfile           = security.AgentProfile
	BriefingID             = foundation.BriefingID
	CapabilityGrant        = security.CapabilityGrant
	ContextManifestID      = foundation.ContextManifestID
	ContentRef             = domaincontext.ContentRef
	DelegationRequestID    = foundation.DelegationRequestID
	DeliveryID             = foundation.DeliveryID
	DomainEvent            = foundation.DomainEvent
	DomainEventType        = foundation.DomainEventType
	ExecutionFailureCode   = execution.ExecutionFailureCode
	ExecutionInputSnapshot = execution.ExecutionInputSnapshot
	ExecutionOutcome       = execution.ExecutionOutcome
	NoteID                 = foundation.NoteID
	SessionID              = foundation.SessionID
	WaitConditionID        = foundation.WaitConditionID
	WorkItemID             = foundation.WorkItemID
)

const (
	ExecutionCompleted   = execution.ExecutionCompleted
	ExecutionYielded     = execution.ExecutionYielded
	ExecutionPaused      = execution.ExecutionPaused
	ExecutionFailed      = execution.ExecutionFailed
	ExecutionInterrupted = execution.ExecutionInterrupted

	EventDelegationPending   = foundation.EventDelegationPending
	EventDelegationApproved  = foundation.EventDelegationApproved
	EventDelegationRejected  = foundation.EventDelegationRejected
	EventDelegationCancelled = foundation.EventDelegationCancelled
	EventArtifactSubmitted   = foundation.EventArtifactSubmitted
	EventArtifactApproved    = foundation.EventArtifactApproved
	EventArtifactRejected    = foundation.EventArtifactRejected
	EventDeliveryCreated     = foundation.EventDeliveryCreated
	EventDeliveryDelivered   = foundation.EventDeliveryDelivered
	EventDeliveryFailed      = foundation.EventDeliveryFailed
	EventQueuedWorkCreated   = foundation.EventQueuedWorkCreated
	EventQueuedWorkStarted   = foundation.EventQueuedWorkStarted
	EventQueuedWorkResumed   = foundation.EventQueuedWorkResumed
	EventQueuedWorkSettled   = foundation.EventQueuedWorkSettled
	EventQueuedWorkCancelled = foundation.EventQueuedWorkCancelled
)

func idIsEmpty(value string) bool { return strings.TrimSpace(value) == "" }

func invalidValue(field, message string) error {
	return &foundation.ValidationError{Field: field, Message: message}
}

func invalidTransition(entity, from, to string) error {
	return &foundation.TransitionError{Entity: entity, From: from, To: to}
}

func fmtField(field string, err error) error {
	if err == nil {
		return nil
	}
	return invalidValue(field, strings.TrimPrefix(err.Error(), field+": "))
}

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}
	return append([]string(nil), values...)
}

func cloneContentRefs(values []ContentRef) []ContentRef {
	if values == nil {
		return nil
	}
	return append([]ContentRef(nil), values...)
}

func validExecutionOutcome(outcome ExecutionOutcome) bool {
	return execution.ValidExecutionOutcome(outcome)
}

func newDomainEvent(eventType DomainEventType, at time.Time) DomainEvent {
	return foundation.NewDomainEvent(eventType, at)
}
