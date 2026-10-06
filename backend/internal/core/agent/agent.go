package agent

import (
	"praxis/internal/contracts"
	task "praxis/internal/core/task"

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
	CurrentTaskID          contracts.TaskID
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

// Startable 判断当前状态是否允许开始新的 Task。
func (s AgentState) Startable() bool {
	switch s {
	case AgentIdle, AgentWaiting, AgentInterrupted, AgentFailed, AgentPaused:
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
		if contracts.EmptyID(string(a.CurrentTaskID)) {
			return contracts.InvalidValue("agent.currentTaskID", "active state requires an task")
		}
	} else if a.CurrentTaskID != "" {
		return contracts.InvalidValue("agent.currentTaskID", "inactive state cannot retain an task")
	}
	if a.CreatedAt.IsZero() || a.UpdatedAt.IsZero() || a.UpdatedAt.Before(a.CreatedAt) {
		return contracts.InvalidValue("agent.timestamps", "timestamps are invalid")
	}
	return nil
}

func (a *Agent) Start(taskID contracts.TaskID, at time.Time) error {
	if !a.State.Startable() {
		return contracts.InvalidTransition("agent", string(a.State), string(AgentExecuting))
	}
	if contracts.EmptyID(string(taskID)) {
		return contracts.InvalidValue("agent.currentTaskID", "task id is required")
	}
	a.State, a.CurrentTaskID, a.UpdatedAt = AgentExecuting, taskID, at.UTC()
	return nil
}

func (a *Agent) RequestPause(at time.Time) error {
	if a.State != AgentExecuting {
		return contracts.InvalidTransition("agent", string(a.State), string(AgentPausing))
	}
	a.State, a.UpdatedAt = AgentPausing, at.UTC()
	return nil
}

// EndTask 根据本轮最终结果释放活动回合；中断后仍可开始新回合。
func (a *Agent) EndTask(outcome task.TaskOutcome, at time.Time) error {
	if a.State != AgentExecuting && a.State != AgentPausing {
		return contracts.InvalidTransition("agent", string(a.State), "ended")
	}
	if !task.ValidTaskOutcome(outcome) {
		return contracts.InvalidValue("agent.outcome", "unknown task outcome")
	}
	if outcome == task.TaskPaused && a.State != AgentPausing {
		return contracts.InvalidTransition("agent", string(a.State), string(AgentPaused))
	}
	switch outcome {
	case task.TaskCompleted:
		a.State = AgentIdle
	case task.TaskYielded:
		a.State = AgentWaiting
	case task.TaskPaused:
		a.State = AgentPaused
	case task.TaskInterrupted:
		a.State = AgentInterrupted
	case task.TaskFailed:
		a.State = AgentFailed
	}
	a.CurrentTaskID, a.UpdatedAt = "", at.UTC()
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
