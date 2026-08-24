package domain

import "time"

// AgentState is a durable projection. Runtime phases are intentionally not
// represented here because they cannot survive process loss.
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
	ID                 AgentID
	SessionID          SessionID
	GroupID            AgentGroupID
	Profile            AgentProfile
	TaskPacketID       TaskPacketID
	ContextManifestID  ContextManifestID
	GrantID            CapabilityGrantID
	RuntimeSessionRef  string
	State              AgentState
	CurrentExecutionID AgentExecutionID
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func NewAgent(
	id AgentID,
	sessionID SessionID,
	groupID AgentGroupID,
	profile AgentProfile,
	packetID TaskPacketID,
	manifestID ContextManifestID,
	grantID CapabilityGrantID,
	at time.Time,
) (Agent, error) {
	agent := Agent{
		ID:                id,
		SessionID:         sessionID,
		GroupID:           groupID,
		Profile:           profile,
		TaskPacketID:      packetID,
		ContextManifestID: manifestID,
		GrantID:           grantID,
		State:             AgentIdle,
		CreatedAt:         at.UTC(),
		UpdatedAt:         at.UTC(),
	}
	if err := agent.Validate(); err != nil {
		return Agent{}, err
	}
	return agent, nil
}

func (a Agent) Validate() error {
	if idIsEmpty(string(a.ID)) || idIsEmpty(string(a.SessionID)) || idIsEmpty(string(a.GroupID)) ||
		idIsEmpty(string(a.TaskPacketID)) || idIsEmpty(string(a.ContextManifestID)) || idIsEmpty(string(a.GrantID)) {
		return invalidValue("agent", "required reference is missing")
	}
	if !a.Profile.Valid() {
		return invalidValue("agent.profile", "unknown agent profile")
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
	// A closed Agent can start a new round without changing the Session identity.
	if a.State != AgentIdle && a.State != AgentWaiting && a.State != AgentFailed && a.State != AgentClosed {
		return invalidTransition("agent", string(a.State), string(AgentExecuting))
	}
	if idIsEmpty(string(executionID)) {
		return invalidValue("agent.currentExecutionID", "execution id is required")
	}
	a.State = AgentExecuting
	a.CurrentExecutionID = executionID
	a.UpdatedAt = at.UTC()
	return nil
}

// Resume starts a new execution after a prior pause or interruption. It never
// revives the old runtime context, stream, or tool process.
func (a *Agent) Resume(executionID AgentExecutionID, at time.Time) error {
	if a.State != AgentPaused && a.State != AgentInterrupted {
		return invalidTransition("agent", string(a.State), string(AgentExecuting))
	}
	if idIsEmpty(string(executionID)) {
		return invalidValue("agent.currentExecutionID", "execution id is required")
	}
	a.State = AgentExecuting
	a.CurrentExecutionID = executionID
	a.UpdatedAt = at.UTC()
	return nil
}

func (a *Agent) RequestPause(at time.Time) error {
	if a.State != AgentExecuting {
		return invalidTransition("agent", string(a.State), string(AgentPausing))
	}
	a.State = AgentPausing
	a.UpdatedAt = at.UTC()
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
	a.CurrentExecutionID = ""
	a.UpdatedAt = at.UTC()
	return nil
}

func (a *Agent) Close(at time.Time) error {
	if a.State == AgentExecuting || a.State == AgentPausing {
		return invalidTransition("agent", string(a.State), string(AgentClosed))
	}
	a.State = AgentClosed
	a.CurrentExecutionID = ""
	a.UpdatedAt = at.UTC()
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
