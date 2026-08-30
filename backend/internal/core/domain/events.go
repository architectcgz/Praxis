package domain

import "time"

type DomainEventType string

const (
	EventDelegationPending   DomainEventType = "delegation_pending"
	EventDelegationApproved  DomainEventType = "delegation_approved"
	EventDelegationRejected  DomainEventType = "delegation_rejected"
	EventDelegationCancelled DomainEventType = "delegation_cancelled"
	EventAgentStarted        DomainEventType = "agent_started"
	EventAgentPausing        DomainEventType = "agent_pausing"
	EventAgentPaused         DomainEventType = "agent_paused"
	EventAgentSettled        DomainEventType = "agent_settled"
	EventAgentFailed         DomainEventType = "agent_failed"
	EventAgentInterrupted    DomainEventType = "agent_interrupted"
	EventAgentClosed         DomainEventType = "agent_closed"
	EventExecutionSettled    DomainEventType = "execution_settled"
	EventQueuedWorkCreated   DomainEventType = "queued_work_created"
	EventQueuedWorkStarted   DomainEventType = "queued_work_started"
	EventQueuedWorkResumed   DomainEventType = "queued_work_resumed"
	EventQueuedWorkSettled   DomainEventType = "queued_work_settled"
	EventQueuedWorkCancelled DomainEventType = "queued_work_cancelled"
	EventLeaseAcquired       DomainEventType = "lease_acquired"
	EventLeaseReleased       DomainEventType = "lease_released"
	EventArtifactSubmitted   DomainEventType = "artifact_submitted"
	EventArtifactApproved    DomainEventType = "artifact_approved"
	EventArtifactRejected    DomainEventType = "artifact_rejected"
	EventDeliveryCreated     DomainEventType = "delivery_created"
	EventDeliveryDelivered   DomainEventType = "delivery_delivered"
	EventDeliveryFailed      DomainEventType = "delivery_failed"
)

type DomainEvent struct {
	ID               EventID
	Type             DomainEventType
	OccurredAt       time.Time
	SessionID        SessionID
	AgentID          AgentID
	AgentExecutionID AgentExecutionID
	WorkItemID       WorkItemID
	DelegationID     DelegationRequestID
	DeliveryID       DeliveryID
	Payload          map[string]string
}

func newDomainEvent(eventType DomainEventType, at time.Time) DomainEvent {
	return DomainEvent{ID: NewEventID(), Type: eventType, OccurredAt: at.UTC()}
}

func (e DomainEvent) Snapshot() DomainEvent {
	copy := e
	if e.Payload != nil {
		copy.Payload = make(map[string]string, len(e.Payload))
		for key, value := range e.Payload {
			copy.Payload[key] = value
		}
	}
	return copy
}
