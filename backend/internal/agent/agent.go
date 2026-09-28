package agent

import (
	"praxis/internal/contracts"
	execution "praxis/internal/execution"

	"time"
)

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

// Agent 表示绑定到一个 Session 和一个 AgentDefinition 的运行实例。
type Agent struct {
	ID                     contracts.AgentID
	SessionID              contracts.SessionID
	DefinitionID           contracts.AgentDefinitionID
	Profile                contracts.AgentProfile
	SecurityPolicyRevision uint64
	State                  AgentState
	CurrentExecutionID     contracts.AgentExecutionID
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

// Startable reports whether a new execution may begin from this state.
func (s AgentState) Startable() bool {
	switch s {
	case AgentIdle, AgentWaiting, AgentFailed, AgentClosed:
		return true
	default:
		return false
	}
}

func NewAgent(id contracts.AgentID, sessionID contracts.SessionID, definitionID contracts.AgentDefinitionID, profile contracts.AgentProfile, securityPolicyRevision uint64, at time.Time) (Agent, error) {
	agent := Agent{
		ID:                     id,
		SessionID:              sessionID,
		DefinitionID:           definitionID,
		Profile:                profile,
		SecurityPolicyRevision: securityPolicyRevision,
		State:                  AgentIdle,
		CreatedAt:              at.UTC(),
		UpdatedAt:              at.UTC(),
	}
	if err := agent.Validate(); err != nil {
		return Agent{}, err
	}
	return agent, nil
}

func (a Agent) Validate() error {
	if contracts.EmptyID(string(a.ID)) || contracts.EmptyID(string(a.SessionID)) || !a.DefinitionID.Valid() {
		return contracts.InvalidValue("agent", "required reference is missing")
	}
	if !a.Profile.Valid() {
		return contracts.InvalidValue("agent.profile", "unknown agent profile")
	}
	if a.SecurityPolicyRevision == 0 {
		return contracts.InvalidValue("agent.securityPolicyRevision", "security policy revision must be positive")
	}
	if !validAgentState(a.State) {
		return contracts.InvalidValue("agent.state", "unknown agent state")
	}
	if a.State == AgentExecuting || a.State == AgentPausing {
		if contracts.EmptyID(string(a.CurrentExecutionID)) {
			return contracts.InvalidValue("agent.currentExecutionID", "active state requires an execution")
		}
	} else if a.CurrentExecutionID != "" {
		return contracts.InvalidValue("agent.currentExecutionID", "inactive state cannot retain an execution")
	}
	if a.CreatedAt.IsZero() || a.UpdatedAt.IsZero() || a.UpdatedAt.Before(a.CreatedAt) {
		return contracts.InvalidValue("agent.timestamps", "timestamps are invalid")
	}
	return nil
}

func (a *Agent) Start(executionID contracts.AgentExecutionID, at time.Time) error {
	if a.State != AgentIdle && a.State != AgentWaiting && a.State != AgentFailed && a.State != AgentClosed {
		return contracts.InvalidTransition("agent", string(a.State), string(AgentExecuting))
	}
	if contracts.EmptyID(string(executionID)) {
		return contracts.InvalidValue("agent.currentExecutionID", "execution id is required")
	}
	a.State, a.CurrentExecutionID, a.UpdatedAt = AgentExecuting, executionID, at.UTC()
	return nil
}

func (a *Agent) Resume(executionID contracts.AgentExecutionID, at time.Time) error {
	if a.State != AgentPaused && a.State != AgentInterrupted {
		return contracts.InvalidTransition("agent", string(a.State), string(AgentExecuting))
	}
	if contracts.EmptyID(string(executionID)) {
		return contracts.InvalidValue("agent.currentExecutionID", "execution id is required")
	}
	a.State, a.CurrentExecutionID, a.UpdatedAt = AgentExecuting, executionID, at.UTC()
	return nil
}

func (a *Agent) RequestPause(at time.Time) error {
	if a.State != AgentExecuting {
		return contracts.InvalidTransition("agent", string(a.State), string(AgentPausing))
	}
	a.State, a.UpdatedAt = AgentPausing, at.UTC()
	return nil
}

func (a *Agent) Settle(outcome execution.ExecutionOutcome, at time.Time) error {
	if a.State != AgentExecuting && a.State != AgentPausing {
		return contracts.InvalidTransition("agent", string(a.State), "settled")
	}
	if !execution.ValidExecutionOutcome(outcome) {
		return contracts.InvalidValue("agent.outcome", "unknown execution outcome")
	}
	if outcome == execution.ExecutionPaused && a.State != AgentPausing {
		return contracts.InvalidTransition("agent", string(a.State), string(AgentPaused))
	}
	switch outcome {
	case execution.ExecutionCompleted:
		a.State = AgentIdle
	case execution.ExecutionYielded:
		a.State = AgentWaiting
	case execution.ExecutionPaused:
		a.State = AgentPaused
	case execution.ExecutionInterrupted:
		a.State = AgentInterrupted
	case execution.ExecutionFailed:
		a.State = AgentFailed
	}
	a.CurrentExecutionID, a.UpdatedAt = "", at.UTC()
	return nil
}

func (a *Agent) Close(at time.Time) error {
	if a.State == AgentExecuting || a.State == AgentPausing {
		return contracts.InvalidTransition("agent", string(a.State), string(AgentClosed))
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
