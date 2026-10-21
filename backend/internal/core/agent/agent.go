package agent

import (
	"praxis/internal/identity"
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
	ID           identity.AgentID
	SessionID    identity.SessionID
	DefinitionID identity.AgentDefinitionID
	Profile      AgentProfile
	// 执行状态由仓储根据 Execution 和控制命令派生，不写入 Agent 日志。
	State              AgentState                `json:"-"`
	CurrentExecutionID identity.AgentExecutionID `json:"-"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// Startable 判断当前状态是否允许开始新的 Execution。
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
