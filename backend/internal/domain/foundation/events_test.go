package foundation

import (
	"testing"
	"time"
)

func TestDomainEventValidateRequiresFieldsPerType(t *testing.T) {
	at := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	session := func(e *DomainEvent) { e.SessionID = "session_1" }
	agent := func(e *DomainEvent) { e.AgentID = "agent_1" }
	execution := func(e *DomainEvent) { e.AgentExecutionID = "execution_1" }
	work := func(e *DomainEvent) { e.WorkItemID = "work_1" }
	delivery := func(e *DomainEvent) { e.DeliveryID = "delivery_1" }
	delegation := func(e *DomainEvent) { e.DelegationID = "delegation_1" }
	artifact := func(e *DomainEvent) { e.ArtifactKind, e.ArtifactID = "briefing", "briefing_1" }
	outcome := func(e *DomainEvent) { e.ExecutionOutcome = "completed" }
	project := func(e *DomainEvent) { e.ProjectID = "project_1" }
	workspace := func(e *DomainEvent) { e.WorkspaceID = "workspace_1" }
	policy := func(e *DomainEvent) { e.PolicyRevision = 2 }
	context := func(e *DomainEvent) { e.ContextRevision, e.ContextKind = 2, "decision" }
	approval := func(e *DomainEvent) { e.ApprovalSource = "user" }

	cases := []struct {
		eventType DomainEventType
		required  []func(*DomainEvent)
	}{
		{EventProjectCreated, []func(*DomainEvent){project, workspace}},
		{EventSessionCreated, []func(*DomainEvent){session, agent}},
		{EventAgentCreated, []func(*DomainEvent){session, agent}},
		{EventAgentStarted, []func(*DomainEvent){session, agent}},
		{EventAgentSettled, []func(*DomainEvent){session, agent}},
		{EventAgentPausing, []func(*DomainEvent){session, agent, execution}},
		{EventAgentClosed, []func(*DomainEvent){session, agent}},
		{EventAgentPaused, []func(*DomainEvent){session, agent, execution, outcome}},
		{EventAgentFailed, []func(*DomainEvent){session, agent, execution, outcome}},
		{EventAgentInterrupted, []func(*DomainEvent){session, agent, execution, outcome}},
		{EventExecutionSettled, []func(*DomainEvent){session, agent, execution, outcome}},
		{EventExecutionStarted, []func(*DomainEvent){session, agent, execution}},
		{EventAgentPolicyUpdated, []func(*DomainEvent){session, agent, policy}},
		{EventSessionContextAppended, []func(*DomainEvent){session, context}},
		{EventQueuedWorkCreated, []func(*DomainEvent){session, agent, work}},
		{EventQueuedWorkStarted, []func(*DomainEvent){session, agent, work, execution}},
		{EventQueuedWorkResumed, []func(*DomainEvent){session, agent, work}},
		{EventQueuedWorkSettled, []func(*DomainEvent){session, agent, work, execution, outcome}},
		{EventQueuedWorkCancelled, []func(*DomainEvent){session, agent, work}},
		{EventLeaseAcquired, []func(*DomainEvent){workspace, agent}},
		{EventLeaseReleased, []func(*DomainEvent){workspace, agent}},
		{EventArtifactSubmitted, []func(*DomainEvent){session, agent, artifact}},
		{EventArtifactApproved, []func(*DomainEvent){session, agent, artifact}},
		{EventArtifactRejected, []func(*DomainEvent){session, agent, artifact}},
		{EventDeliveryCreated, []func(*DomainEvent){session, delivery}},
		{EventDeliveryDelivered, []func(*DomainEvent){session, delivery}},
		{EventDeliveryFailed, []func(*DomainEvent){session, delivery}},
		{EventDelegationPending, []func(*DomainEvent){session, agent, delegation}},
		{EventDelegationApproved, []func(*DomainEvent){session, agent, delegation, approval}},
		{EventDelegationRejected, []func(*DomainEvent){session, agent, delegation}},
		{EventDelegationCancelled, []func(*DomainEvent){session, agent, delegation}},
	}

	for _, testCase := range cases {
		event := DomainEvent{ID: "event_1", Type: testCase.eventType, OccurredAt: at}
		for _, apply := range testCase.required {
			apply(&event)
		}
		if err := event.Validate(); err != nil {
			t.Fatalf("%s valid event rejected: %v", testCase.eventType, err)
		}
		// Every listed field must actually be required.
		for index := range testCase.required {
			missing := DomainEvent{ID: "event_1", Type: testCase.eventType, OccurredAt: at}
			for other, otherApply := range testCase.required {
				if other != index {
					otherApply(&missing)
				}
			}
			if err := missing.Validate(); err == nil {
				t.Fatalf("%s accepted a missing required field at index %d", testCase.eventType, index)
			}
		}
	}
}

func TestDomainEventValidateRejectsUnknownAndIncomplete(t *testing.T) {
	if err := (DomainEvent{ID: "event_1", Type: "unknown_event", OccurredAt: time.Now()}).Validate(); err == nil {
		t.Fatal("unknown event type should be rejected")
	}
	if err := (DomainEvent{Type: EventProjectCreated, OccurredAt: time.Now()}).Validate(); err == nil {
		t.Fatal("event without identity should be rejected")
	}
}
