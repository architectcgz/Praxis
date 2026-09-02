package workspace

import (
	"strings"
	"time"

	foundation "praxis/internal/core/domain/foundation"
	"praxis/internal/core/domain/security"
)

type (
	AgentID           = foundation.AgentID
	CapabilityGrantID = foundation.CapabilityGrantID
	ProjectID         = foundation.ProjectID
	WorkspaceID       = foundation.WorkspaceID
	WorkspaceLeaseID  = foundation.WorkspaceLeaseID
	DomainEvent       = foundation.DomainEvent
	CapabilityGrant   = security.CapabilityGrant
)

const (
	EventLeaseAcquired = foundation.EventLeaseAcquired
	EventLeaseReleased = foundation.EventLeaseReleased
)

func idIsEmpty(value string) bool { return strings.TrimSpace(value) == "" }

func invalidValue(field, message string) error {
	return &foundation.ValidationError{Field: field, Message: message}
}

func invalidTransition(entity, from, to string) error {
	return &foundation.TransitionError{Entity: entity, From: from, To: to}
}

func newDomainEvent(eventType foundation.DomainEventType, at time.Time) DomainEvent {
	return foundation.NewDomainEvent(eventType, at)
}
