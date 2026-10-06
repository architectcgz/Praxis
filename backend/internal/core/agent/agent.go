package agent

import (
	"praxis/internal/contracts"
	turn "praxis/internal/core/turn"

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
)

// Agent 表示绑定到一个 Session 和一个 AgentDefinition 的运行实例。
type Agent struct {
	ID                     contracts.AgentID
	SessionID              contracts.SessionID
	DefinitionID           contracts.AgentDefinitionID
	Profile                contracts.AgentProfile
	SecurityPolicyRevision uint64
	State                  AgentState
	CurrentTurnID          contracts.TurnID
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

// Startable 判断当前状态是否允许开始新的 Turn。
func (s AgentState) Startable() bool {
	switch s {
	case AgentIdle, AgentWaiting, AgentInterrupted, AgentFailed:
		return true
	default:
		return false
	}
}

// CanReadSessionContext 判断当前 Agent 是否属于可读取 Session 级上下文的主 Agent。
func (a Agent) CanReadSessionContext() bool {
	return a.DefinitionID == DefinitionPrimary
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
		if contracts.EmptyID(string(a.CurrentTurnID)) {
			return contracts.InvalidValue("agent.currentTurnID", "active state requires an turn")
		}
	} else if a.CurrentTurnID != "" {
		return contracts.InvalidValue("agent.currentTurnID", "inactive state cannot retain an turn")
	}
	if a.CreatedAt.IsZero() || a.UpdatedAt.IsZero() || a.UpdatedAt.Before(a.CreatedAt) {
		return contracts.InvalidValue("agent.timestamps", "timestamps are invalid")
	}
	return nil
}

func (a *Agent) Start(turnID contracts.TurnID, at time.Time) error {
	if !a.State.Startable() {
		return contracts.InvalidTransition("agent", string(a.State), string(AgentExecuting))
	}
	if contracts.EmptyID(string(turnID)) {
		return contracts.InvalidValue("agent.currentTurnID", "turn id is required")
	}
	a.State, a.CurrentTurnID, a.UpdatedAt = AgentExecuting, turnID, at.UTC()
	return nil
}

func (a *Agent) Resume(turnID contracts.TurnID, at time.Time) error {
	if a.State != AgentPaused && a.State != AgentInterrupted {
		return contracts.InvalidTransition("agent", string(a.State), string(AgentExecuting))
	}
	if contracts.EmptyID(string(turnID)) {
		return contracts.InvalidValue("agent.currentTurnID", "turn id is required")
	}
	a.State, a.CurrentTurnID, a.UpdatedAt = AgentExecuting, turnID, at.UTC()
	return nil
}

func (a *Agent) RequestPause(at time.Time) error {
	if a.State != AgentExecuting {
		return contracts.InvalidTransition("agent", string(a.State), string(AgentPausing))
	}
	a.State, a.UpdatedAt = AgentPausing, at.UTC()
	return nil
}

// EndTurn 根据本轮最终结果释放活动回合；中断后仍可开始新回合。
func (a *Agent) EndTurn(outcome turn.TurnOutcome, at time.Time) error {
	if a.State != AgentExecuting && a.State != AgentPausing {
		return contracts.InvalidTransition("agent", string(a.State), "ended")
	}
	if !turn.ValidTurnOutcome(outcome) {
		return contracts.InvalidValue("agent.outcome", "unknown turn outcome")
	}
	if outcome == turn.TurnPaused && a.State != AgentPausing {
		return contracts.InvalidTransition("agent", string(a.State), string(AgentPaused))
	}
	switch outcome {
	case turn.TurnCompleted:
		a.State = AgentIdle
	case turn.TurnYielded:
		a.State = AgentWaiting
	case turn.TurnPaused:
		a.State = AgentPaused
	case turn.TurnInterrupted:
		a.State = AgentInterrupted
	case turn.TurnFailed:
		a.State = AgentFailed
	}
	a.CurrentTurnID, a.UpdatedAt = "", at.UTC()
	return nil
}

func validAgentState(state AgentState) bool {
	switch state {
	case AgentIdle, AgentExecuting, AgentWaiting, AgentPausing, AgentPaused, AgentInterrupted, AgentFailed:
		return true
	default:
		return false
	}
}
