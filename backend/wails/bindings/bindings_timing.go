package bindings

import (
	"praxis/internal/timing"
	"praxis/wails/validation"
)

// ListAgentTimings 返回 Agent 最近的独立计时记录，包含尚在运行的操作。
func (b *AgentBindings) ListAgentTimings(agentID string) ([]timing.Record, error) {
	if err := validation.ValidateAgentID(agentID); err != nil {
		return nil, err
	}
	ctx, services, err := b.runtime.BindingContext()
	if err != nil {
		return nil, err
	}
	records, err := services.Timings.ListAgent(ctx, agentID, 2000)
	if err != nil {
		return nil, publicError(b.runtime, "AgentBindings.ListAgentTimings", err)
	}
	return records, nil
}
