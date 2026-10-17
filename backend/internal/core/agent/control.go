package agent

import (
	"time"

	"praxis/internal/contracts"
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

// AgentControlCommand 是针对单个 Agent 当前 Task 的持久化控制意图。
type AgentControlCommand struct {
	ID           contracts.AgentControlCommandID
	AgentID      contracts.AgentID
	TargetTaskID contracts.TaskID
	Kind         AgentControlKind
	Status       AgentControlStatus
	CreatedAt    time.Time
	AppliedAt    time.Time
}

// NewAgentControlCommand 使用入口已校验的身份和控制类型创建待应用命令。
func NewAgentControlCommand(id contracts.AgentControlCommandID, agentID contracts.AgentID, targetTaskID contracts.TaskID, kind AgentControlKind, at time.Time) AgentControlCommand {
	return AgentControlCommand{
		ID:           id,
		AgentID:      agentID,
		TargetTaskID: targetTaskID,
		Kind:         kind,
		Status:       AgentControlPending,
		CreatedAt:    at.UTC(),
	}
}

// MarkApplied 确认控制命令已结算；重复确认或时间早于创建时间时拒绝修改。
func (r *AgentControlCommand) MarkApplied(at time.Time) error {
	if r.Status != AgentControlPending {
		return contracts.InvalidTransition("agentControlCommand", string(r.Status), string(AgentControlApplied))
	}
	if at.Before(r.CreatedAt) {
		return contracts.InvalidValue("agentControlCommand.appliedAt", "apply time cannot precede creation")
	}
	r.Status = AgentControlApplied
	r.AppliedAt = at.UTC()
	return nil
}
