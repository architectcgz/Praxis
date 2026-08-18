package domain

import "time"

type DomainEventType string

const (
	EventDelegationPending   DomainEventType = "delegation_pending"
	EventDelegationApproved  DomainEventType = "delegation_approved"
	EventDelegationRejected  DomainEventType = "delegation_rejected"
	EventDelegationCancelled DomainEventType = "delegation_cancelled"
	EventThreadStarted       DomainEventType = "thread_started"
	EventThreadPausing       DomainEventType = "thread_pausing"
	EventThreadPaused        DomainEventType = "thread_paused"
	EventThreadSettled       DomainEventType = "thread_settled"
	EventThreadFailed        DomainEventType = "thread_failed"
	EventThreadInterrupted   DomainEventType = "thread_interrupted"
	EventThreadClosed        DomainEventType = "thread_closed"
	EventRunSettled          DomainEventType = "run_settled"
	EventWorkItemQueued      DomainEventType = "work_item_queued"
	EventWorkItemStarted     DomainEventType = "work_item_started"
	EventWorkItemResumed     DomainEventType = "work_item_resumed"
	EventWorkItemSettled     DomainEventType = "work_item_settled"
	EventWorkItemCancelled   DomainEventType = "work_item_cancelled"
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
	ID          EventID
	Type        DomainEventType
	OccurredAt  time.Time
	TaskSession TaskSessionID
	AgentThread AgentThreadID
	AgentRun    AgentRunID
	WorkItem    WorkItemID
	Delegation  DelegationRequestID
	Delivery    DeliveryID
	Payload     map[string]string
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
