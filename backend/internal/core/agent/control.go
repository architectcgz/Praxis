package agent

import (
	"praxis/internal/identity"
	"time"
)

type AgentControlKind string

const (
	AgentControlPause  AgentControlKind = "pause"
	AgentControlCancel AgentControlKind = "cancel"
)

type AgentControlStatus string

const (
	AgentControlPending AgentControlStatus = "pending"
	AgentControlApplied AgentControlStatus = "applied"
)

// AgentControlCommand 是针对单个 Agent 当前 Execution 的持久化控制意图。
type AgentControlCommand struct {
	ID                identity.AgentControlCommandID
	AgentID           identity.AgentID
	TargetExecutionID identity.AgentExecutionID
	Kind              AgentControlKind
	// 命令是否已应用由目标 Execution 的终态派生，只持久化控制意图。
	Status    AgentControlStatus `json:"-"`
	CreatedAt time.Time
	AppliedAt time.Time `json:"-"`
}

// NewAgentControlCommand 使用入口已校验的身份和控制类型创建待应用命令。
func NewAgentControlCommand(id identity.AgentControlCommandID, agentID identity.AgentID, targetExecutionID identity.AgentExecutionID, kind AgentControlKind, at time.Time) AgentControlCommand {
	return AgentControlCommand{
		ID:                id,
		AgentID:           agentID,
		TargetExecutionID: targetExecutionID,
		Kind:              kind,
		Status:            AgentControlPending,
		CreatedAt:         at.UTC(),
	}
}

// MarkApplied 记录控制命令的应用结果，应用条件和时间由调用方确定。
func (r *AgentControlCommand) MarkApplied(at time.Time) {
	r.Status = AgentControlApplied
	r.AppliedAt = at.UTC()
}
