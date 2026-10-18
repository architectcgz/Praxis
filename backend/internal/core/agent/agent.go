package agent

import (
	task "praxis/internal/core/task"

	"praxis/internal/core/fault"
	"praxis/internal/core/identity"
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
	ID            identity.AgentID
	SessionID     identity.SessionID
	DefinitionID  identity.AgentDefinitionID
	Profile       AgentProfile
	State         AgentState
	CurrentTaskID identity.TaskID
	CreatedAt     time.Time
	UpdatedAt     time.Time
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

// NewAgent 使用已校验的身份与 Agent 定义创建 idle 实例，不重复检查入口字段。
func NewAgent(id identity.AgentID, sessionID identity.SessionID, definitionID identity.AgentDefinitionID, profile AgentProfile, at time.Time) Agent {
	return Agent{
		ID:           id,
		SessionID:    sessionID,
		DefinitionID: definitionID,
		Profile:      profile,
		State:        AgentIdle,
		CreatedAt:    at.UTC(),
		UpdatedAt:    at.UTC(),
	}
}

func (a *Agent) Start(taskID identity.TaskID, at time.Time) error {
	if !a.State.Startable() {
		return fault.InvalidTransition("agent", string(a.State), string(AgentExecuting))
	}
	if taskID == "" {
		return fault.InvalidValue("agent.currentTaskID", "task id is required")
	}
	a.State, a.CurrentTaskID, a.UpdatedAt = AgentExecuting, taskID, at.UTC()
	return nil
}

func (a *Agent) RequestPause(at time.Time) error {
	if a.State != AgentExecuting {
		return fault.InvalidTransition("agent", string(a.State), string(AgentPausing))
	}
	a.State, a.UpdatedAt = AgentPausing, at.UTC()
	return nil
}

// EndTask 根据本轮最终结果释放活动回合；中断后仍可开始新回合。
func (a *Agent) EndTask(outcome task.TaskOutcome, at time.Time) error {
	if a.State != AgentExecuting && a.State != AgentPausing {
		return fault.InvalidTransition("agent", string(a.State), "ended")
	}
	if !task.ValidTaskOutcome(outcome) {
		return fault.InvalidValue("agent.outcome", "unknown task outcome")
	}
	if outcome == task.TaskPaused && a.State != AgentPausing {
		return fault.InvalidTransition("agent", string(a.State), string(AgentPaused))
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
