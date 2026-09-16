package foundation

import (
	"strings"
	"time"
)

type DomainEventType string

const (
	EventDelegationPending      DomainEventType = "delegation_pending"
	EventDelegationApproved     DomainEventType = "delegation_approved"
	EventDelegationRejected     DomainEventType = "delegation_rejected"
	EventDelegationCancelled    DomainEventType = "delegation_cancelled"
	EventAgentStarted           DomainEventType = "agent_started"
	EventAgentPausing           DomainEventType = "agent_pausing"
	EventAgentPaused            DomainEventType = "agent_paused"
	EventAgentSettled           DomainEventType = "agent_settled"
	EventAgentFailed            DomainEventType = "agent_failed"
	EventAgentInterrupted       DomainEventType = "agent_interrupted"
	EventAgentClosed            DomainEventType = "agent_closed"
	EventExecutionSettled       DomainEventType = "execution_settled"
	EventQueuedWorkCreated      DomainEventType = "queued_work_created"
	EventQueuedWorkStarted      DomainEventType = "queued_work_started"
	EventQueuedWorkResumed      DomainEventType = "queued_work_resumed"
	EventQueuedWorkSettled      DomainEventType = "queued_work_settled"
	EventQueuedWorkCancelled    DomainEventType = "queued_work_cancelled"
	EventLeaseAcquired          DomainEventType = "lease_acquired"
	EventLeaseReleased          DomainEventType = "lease_released"
	EventArtifactSubmitted      DomainEventType = "artifact_submitted"
	EventArtifactApproved       DomainEventType = "artifact_approved"
	EventArtifactRejected       DomainEventType = "artifact_rejected"
	EventDeliveryCreated        DomainEventType = "delivery_created"
	EventDeliveryDelivered      DomainEventType = "delivery_delivered"
	EventDeliveryFailed         DomainEventType = "delivery_failed"
	EventProjectCreated         DomainEventType = "project_created"
	EventSessionCreated         DomainEventType = "session_created"
	EventAgentCreated           DomainEventType = "agent_created"
	EventAgentPolicyUpdated     DomainEventType = "agent_policy_updated"
	EventSessionContextAppended DomainEventType = "session_context_appended"
	EventExecutionStarted       DomainEventType = "execution_started"
)

// DomainEvent is a low-sensitivity durable audit fact stored only in SQLite.
// It carries explicit fields and never stores arbitrary payload maps, document
// references or content bodies. It is an audit and wakeup signal only; recovery
// always reads the owning business tables.
type DomainEvent struct {
	ID               EventID
	Type             DomainEventType
	OccurredAt       time.Time
	ProjectID        ProjectID
	WorkspaceID      WorkspaceID
	SessionID        SessionID
	AgentID          AgentID
	TargetAgentID    AgentID
	AgentExecutionID AgentExecutionID
	WorkItemID       WorkItemID
	DelegationID     DelegationRequestID
	DeliveryID       DeliveryID
	ArtifactKind     string
	ArtifactID       string
	ApprovalSource   string
	PolicyRevision   uint64
	ContextRevision  uint64
	ContextKind      string
	ExecutionOutcome string
	FailureCode      string
}

func newDomainEvent(eventType DomainEventType, at time.Time) DomainEvent {
	return DomainEvent{ID: NewEventID(), Type: eventType, OccurredAt: at.UTC()}
}

func NewDomainEvent(eventType DomainEventType, at time.Time) DomainEvent {
	return newDomainEvent(eventType, at)
}

func (e DomainEvent) Snapshot() DomainEvent { return e }

// Validate enforces the exact field combination each event type requires.
// Rejecting unknown or missing fields keeps the audit model finite and prevents
// re-introducing a generic extension map.
func (e DomainEvent) Validate() error {
	if strings.TrimSpace(string(e.ID)) == "" || e.Type == "" || e.OccurredAt.IsZero() {
		return eventInvalid("domainEvent", "identity, type and occurrence time are required")
	}
	switch e.Type {
	case EventProjectCreated:
		if e.ProjectID == "" || e.WorkspaceID == "" {
			return eventInvalid("domainEvent.project", "project_created requires project and workspace")
		}
	case EventSessionCreated, EventAgentCreated, EventAgentStarted, EventAgentSettled:
		if e.SessionID == "" || e.AgentID == "" {
			return eventInvalid("domainEvent.agent", string(e.Type)+" requires session and agent")
		}
	case EventAgentPausing:
		if e.SessionID == "" || e.AgentID == "" || e.AgentExecutionID == "" {
			return eventInvalid("domainEvent.execution", "agent_pausing requires session, agent and execution")
		}
	case EventAgentClosed:
		if e.SessionID == "" || e.AgentID == "" {
			return eventInvalid("domainEvent.agent", "agent_closed requires session and agent")
		}
	case EventAgentPaused, EventAgentFailed, EventAgentInterrupted, EventExecutionSettled:
		if e.SessionID == "" || e.AgentID == "" || e.AgentExecutionID == "" || e.ExecutionOutcome == "" {
			return eventInvalid("domainEvent.outcome", string(e.Type)+" requires session, agent, execution and outcome")
		}
	case EventExecutionStarted:
		if e.SessionID == "" || e.AgentID == "" || e.AgentExecutionID == "" {
			return eventInvalid("domainEvent.execution", "execution_started requires session, agent and execution")
		}
	case EventAgentPolicyUpdated:
		if e.SessionID == "" || e.AgentID == "" || e.PolicyRevision == 0 {
			return eventInvalid("domainEvent.policy", "agent_policy_updated requires session, agent and policy revision")
		}
	case EventSessionContextAppended:
		if e.SessionID == "" || e.ContextRevision == 0 || e.ContextKind == "" {
			return eventInvalid("domainEvent.context", "session_context_appended requires session, revision and kind")
		}
	case EventQueuedWorkCreated, EventQueuedWorkResumed, EventQueuedWorkCancelled:
		if e.SessionID == "" || e.AgentID == "" || e.WorkItemID == "" {
			return eventInvalid("domainEvent.work", string(e.Type)+" requires session, agent and work item")
		}
	case EventQueuedWorkStarted:
		if e.SessionID == "" || e.AgentID == "" || e.WorkItemID == "" || e.AgentExecutionID == "" {
			return eventInvalid("domainEvent.work", "queued_work_started requires session, agent, work item and execution")
		}
	case EventQueuedWorkSettled:
		if e.SessionID == "" || e.AgentID == "" || e.WorkItemID == "" || e.AgentExecutionID == "" || e.ExecutionOutcome == "" {
			return eventInvalid("domainEvent.work", "queued_work_settled requires session, agent, work item, execution and outcome")
		}
	case EventDeliveryCreated, EventDeliveryDelivered, EventDeliveryFailed:
		if e.SessionID == "" || e.DeliveryID == "" {
			return eventInvalid("domainEvent.delivery", string(e.Type)+" requires session and delivery")
		}
	case EventDelegationPending, EventDelegationRejected, EventDelegationCancelled:
		if e.SessionID == "" || e.AgentID == "" || e.DelegationID == "" {
			return eventInvalid("domainEvent.delegation", string(e.Type)+" requires session, agent and delegation")
		}
	case EventDelegationApproved:
		if e.SessionID == "" || e.AgentID == "" || e.DelegationID == "" || e.ApprovalSource == "" {
			return eventInvalid("domainEvent.delegation", "delegation_approved requires session, agent, delegation and approval source")
		}
	case EventLeaseAcquired, EventLeaseReleased:
		if e.WorkspaceID == "" || e.AgentID == "" {
			return eventInvalid("domainEvent.lease", string(e.Type)+" requires workspace and agent")
		}
	case EventArtifactSubmitted, EventArtifactApproved, EventArtifactRejected:
		if e.SessionID == "" || e.AgentID == "" || e.ArtifactKind == "" || e.ArtifactID == "" {
			return eventInvalid("domainEvent.artifact", string(e.Type)+" requires session, agent, artifact kind and id")
		}
	default:
		return eventInvalid("domainEvent.type", "unknown event type")
	}
	return nil
}

func eventInvalid(field, message string) error {
	return &ValidationError{Field: field, Message: message}
}
