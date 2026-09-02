package agent

import "time"

type AgentState string

const (
	AgentIdle        AgentState = "idle"
	AgentExecuting   AgentState = "executing"
	AgentWaiting     AgentState = "waiting"
	AgentPausing     AgentState = "pausing"
	AgentPaused      AgentState = "paused"
	AgentInterrupted AgentState = "interrupted"
	AgentFailed      AgentState = "failed"
	AgentClosed      AgentState = "closed"
)

type Agent struct {
	ID                     AgentID
	SessionID              SessionID
	Profile                AgentProfile
	SecurityPolicyRevision uint64
	State                  AgentState
	CurrentExecutionID     AgentExecutionID
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

func NewAgent(id AgentID, sessionID SessionID, profile AgentProfile, securityPolicyRevision uint64, at time.Time) (Agent, error) {
	agent := Agent{
		ID: id, SessionID: sessionID, Profile: profile,
		SecurityPolicyRevision: securityPolicyRevision, State: AgentIdle,
		CreatedAt: at.UTC(), UpdatedAt: at.UTC(),
	}
	if err := agent.Validate(); err != nil {
		return Agent{}, err
	}
	return agent, nil
}

func (a Agent) Validate() error {
	if idIsEmpty(string(a.ID)) || idIsEmpty(string(a.SessionID)) {
		return invalidValue("agent", "required reference is missing")
	}
	if !a.Profile.Valid() {
		return invalidValue("agent.profile", "unknown agent profile")
	}
	if a.SecurityPolicyRevision == 0 {
		return invalidValue("agent.securityPolicyRevision", "security policy revision must be positive")
	}
	if !validAgentState(a.State) {
		return invalidValue("agent.state", "unknown agent state")
	}
	if a.State == AgentExecuting || a.State == AgentPausing {
		if idIsEmpty(string(a.CurrentExecutionID)) {
			return invalidValue("agent.currentExecutionID", "active state requires an execution")
		}
	} else if a.CurrentExecutionID != "" {
		return invalidValue("agent.currentExecutionID", "inactive state cannot retain an execution")
	}
	if a.CreatedAt.IsZero() || a.UpdatedAt.IsZero() || a.UpdatedAt.Before(a.CreatedAt) {
		return invalidValue("agent.timestamps", "timestamps are invalid")
	}
	return nil
}

func (a *Agent) Start(executionID AgentExecutionID, at time.Time) error {
	if a.State != AgentIdle && a.State != AgentWaiting && a.State != AgentFailed && a.State != AgentClosed {
		return invalidTransition("agent", string(a.State), string(AgentExecuting))
	}
	if idIsEmpty(string(executionID)) {
		return invalidValue("agent.currentExecutionID", "execution id is required")
	}
	a.State, a.CurrentExecutionID, a.UpdatedAt = AgentExecuting, executionID, at.UTC()
	return nil
}

func (a *Agent) Resume(executionID AgentExecutionID, at time.Time) error {
	if a.State != AgentPaused && a.State != AgentInterrupted {
		return invalidTransition("agent", string(a.State), string(AgentExecuting))
	}
	if idIsEmpty(string(executionID)) {
		return invalidValue("agent.currentExecutionID", "execution id is required")
	}
	a.State, a.CurrentExecutionID, a.UpdatedAt = AgentExecuting, executionID, at.UTC()
	return nil
}

func (a *Agent) RequestPause(at time.Time) error {
	if a.State != AgentExecuting {
		return invalidTransition("agent", string(a.State), string(AgentPausing))
	}
	a.State, a.UpdatedAt = AgentPausing, at.UTC()
	return nil
}

func (a *Agent) Settle(outcome ExecutionOutcome, at time.Time) error {
	if a.State != AgentExecuting && a.State != AgentPausing {
		return invalidTransition("agent", string(a.State), "settled")
	}
	if !validExecutionOutcome(outcome) {
		return invalidValue("agent.outcome", "unknown execution outcome")
	}
	if outcome == ExecutionPaused && a.State != AgentPausing {
		return invalidTransition("agent", string(a.State), string(AgentPaused))
	}
	switch outcome {
	case ExecutionCompleted:
		a.State = AgentIdle
	case ExecutionYielded:
		a.State = AgentWaiting
	case ExecutionPaused:
		a.State = AgentPaused
	case ExecutionInterrupted:
		a.State = AgentInterrupted
	case ExecutionFailed:
		a.State = AgentFailed
	}
	a.CurrentExecutionID, a.UpdatedAt = "", at.UTC()
	return nil
}

func (a *Agent) Close(at time.Time) error {
	if a.State == AgentExecuting || a.State == AgentPausing {
		return invalidTransition("agent", string(a.State), string(AgentClosed))
	}
	a.State, a.CurrentExecutionID, a.UpdatedAt = AgentClosed, "", at.UTC()
	return nil
}

func validAgentState(state AgentState) bool {
	switch state {
	case AgentIdle, AgentExecuting, AgentWaiting, AgentPausing, AgentPaused, AgentInterrupted, AgentFailed, AgentClosed:
		return true
	default:
		return false
	}
}
