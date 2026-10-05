package bindings

import (
	agentruntime "praxis/internal/agent_runtime"
	"praxis/wails/validation"
)

// ListSessionUsage 返回会话所有请求的已上报用量；未上报的计数不补零。
func (b *AgentBindings) ListSessionUsage(sessionID string) ([]agentruntime.ModelUsageRecord, error) {
	if err := validation.ValidateSessionID(sessionID); err != nil {
		return nil, err
	}
	ctx, services, err := b.runtime.BindingContext()
	if err != nil {
		return nil, err
	}
	records, err := services.Usages.ListSession(ctx, sessionID)
	if err != nil {
		return nil, publicError(b.runtime, "AgentBindings.ListSessionUsage", err)
	}
	return records, nil
}
