package agent

import (
	"praxis/internal/identity"
	"time"
)

type AgentState string

// AgentKind 表示实例与 Session 的关系，与角色定义的职责无关。
type AgentKind string

const (
	KindMain     AgentKind = "main"
	KindSubagent AgentKind = "subagent"
)

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
	Kind         AgentKind
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

// IsMain 判断实例是否拥有 Session 上下文；Subagent 使用隔离的私有上下文。
func (a Agent) IsMain() bool {
	return a.Kind == KindMain
}

// NewAgent 使用已校验的身份与 Agent 定义创建 idle 实例，不重复检查入口字段。
func NewAgent(id identity.AgentID, sessionID identity.SessionID, definitionID identity.AgentDefinitionID, kind AgentKind, at time.Time) Agent {
	return Agent{
		ID:           id,
		SessionID:    sessionID,
		DefinitionID: definitionID,
		Kind:         kind,
		State:        AgentIdle,
		CreatedAt:    at.UTC(),
		UpdatedAt:    at.UTC(),
	}
}
