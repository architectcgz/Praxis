package domain

import "time"

type AgentRunOutcome string

const (
	RunCompleted   AgentRunOutcome = "completed"
	RunPaused      AgentRunOutcome = "paused"
	RunFailed      AgentRunOutcome = "failed"
	RunInterrupted AgentRunOutcome = "interrupted"
)

type AgentRun struct {
	ID            AgentRunID
	AgentThreadID AgentThreadID
	WorkItemID    WorkItemID
	Reason        string
	Execution     RuntimeExecutionSnapshot
	StartedAt     time.Time
	SettledAt     time.Time
	Outcome       AgentRunOutcome
	FailureCode   string
}

func NewAgentRun(
	id AgentRunID,
	threadID AgentThreadID,
	reason string,
	execution RuntimeExecutionSnapshot,
	startedAt time.Time,
) (AgentRun, error) {
	return newAgentRun(id, "", threadID, reason, execution, startedAt)
}

// NewAgentRunForWorkItem creates a product run bound to one durable work item.
func NewAgentRunForWorkItem(
	id AgentRunID,
	workItemID WorkItemID,
	threadID AgentThreadID,
	reason string,
	execution RuntimeExecutionSnapshot,
	startedAt time.Time,
) (AgentRun, error) {
	return newAgentRun(id, workItemID, threadID, reason, execution, startedAt)
}

func newAgentRun(
	id AgentRunID,
	workItemID WorkItemID,
	threadID AgentThreadID,
	reason string,
	execution RuntimeExecutionSnapshot,
	startedAt time.Time,
) (AgentRun, error) {
	run := AgentRun{
		ID:            id,
		AgentThreadID: threadID,
		WorkItemID:    workItemID,
		Reason:        reason,
		Execution:     execution.Snapshot(),
		StartedAt:     startedAt.UTC(),
	}
	if err := run.Validate(); err != nil {
		return AgentRun{}, err
	}
	return run, nil
}

// ValidateForWorkItem verifies that this run belongs to the expected queue item.
func (r AgentRun) ValidateForWorkItem(workItemID WorkItemID) error {
	if idIsEmpty(string(workItemID)) || r.WorkItemID != workItemID {
		return invalidValue("agentRun.workItemID", "run does not belong to the work item")
	}
	return r.Validate()
}

func (r AgentRun) Validate() error {
	if idIsEmpty(string(r.ID)) || idIsEmpty(string(r.AgentThreadID)) {
		return invalidValue("agentRun", "required reference is missing")
	}
	if r.Reason == "" {
		return invalidValue("agentRun.reason", "reason is required")
	}
	if err := r.Execution.Validate(); err != nil {
		return fmtField("agentRun.execution", err)
	}
	if r.StartedAt.IsZero() {
		return invalidValue("agentRun.startedAt", "start time is required")
	}
	if !r.SettledAt.IsZero() && r.SettledAt.Before(r.StartedAt) {
		return invalidValue("agentRun.settledAt", "settle time cannot precede start time")
	}
	if r.SettledAt.IsZero() && r.Outcome != "" {
		return invalidValue("agentRun.outcome", "active run cannot have an outcome")
	}
	if !r.SettledAt.IsZero() && !validRunOutcome(r.Outcome) {
		return invalidValue("agentRun.outcome", "unknown run outcome")
	}
	return nil
}

func (r *AgentRun) Settle(outcome AgentRunOutcome, settledAt time.Time, failureCode string) (DomainEvent, error) {
	if !r.SettledAt.IsZero() {
		return DomainEvent{}, ErrAlreadySettled
	}
	if !validRunOutcome(outcome) {
		return DomainEvent{}, invalidValue("agentRun.outcome", "unknown run outcome")
	}
	settledAt = settledAt.UTC()
	if settledAt.Before(r.StartedAt) {
		return DomainEvent{}, invalidValue("agentRun.settledAt", "settle time cannot precede start time")
	}
	r.Outcome = outcome
	r.SettledAt = settledAt
	r.FailureCode = failureCode
	event := newDomainEvent(EventRunSettled, settledAt)
	event.AgentThread, event.AgentRun = r.AgentThreadID, r.ID
	event.Payload = map[string]string{"outcome": string(outcome)}
	return event, nil
}

func (r AgentRun) Snapshot() AgentRun { return r }

func validRunOutcome(outcome AgentRunOutcome) bool {
	switch outcome {
	case RunCompleted, RunPaused, RunFailed, RunInterrupted:
		return true
	default:
		return false
	}
}
