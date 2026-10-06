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

// NewAgentControlCommand 创建已规范化的控制命令，缺少身份或非法类型时返回校验错误。
func NewAgentControlCommand(id contracts.AgentControlCommandID, agentID contracts.AgentID, targetTaskID contracts.TaskID, kind AgentControlKind, at time.Time) (AgentControlCommand, error) {
	command := AgentControlCommand{
		ID:           id,
		AgentID:      agentID,
		TargetTaskID: targetTaskID,
		Kind:         kind,
		Status:       AgentControlPending,
		CreatedAt:    at.UTC(),
	}
	if err := command.Validate(); err != nil {
		return AgentControlCommand{}, err
	}
	return command, nil
}

// Validate 只读校验控制命令的身份、状态和时间，不补默认值或修改持久化事实。
func (r AgentControlCommand) Validate() error {
	if contracts.EmptyID(string(r.ID)) || contracts.EmptyID(string(r.AgentID)) || contracts.EmptyID(string(r.TargetTaskID)) {
		return contracts.InvalidValue("agentControlCommand", "required reference is missing")
	}
	if r.Kind != AgentControlPause && r.Kind != AgentControlCancel {
		return contracts.InvalidValue("agentControlCommand.kind", "unknown control kind")
	}
	if r.Status != AgentControlPending && r.Status != AgentControlApplied {
		return contracts.InvalidValue("agentControlCommand.status", "unknown control status")
	}
	if r.CreatedAt.IsZero() || (!r.AppliedAt.IsZero() && r.AppliedAt.Before(r.CreatedAt)) {
		return contracts.InvalidValue("agentControlCommand.timestamps", "timestamps are invalid")
	}
	if r.Status == AgentControlPending && !r.AppliedAt.IsZero() {
		return contracts.InvalidValue("agentControlCommand.appliedAt", "pending control has an apply time")
	}
	if r.Status == AgentControlApplied && r.AppliedAt.IsZero() {
		return contracts.InvalidValue("agentControlCommand.appliedAt", "applied control requires a time")
	}
	return nil
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
