package bindings

import (
	"praxis/internal/service"
	"praxis/wails/dto"
	"praxis/wails/validation"
)

// ListAgentTimings 返回 Agent 最近的独立计时记录，包含尚在运行的操作。
func (b *AgentBindings) ListAgentTimings(agentID string) ([]dto.OperationTiming, error) {
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
	result := make([]dto.OperationTiming, 0, len(records))
	for _, record := range records {
		result = append(result, operationTiming(record))
	}
	return result, nil
}

func operationTiming(value service.OperationTiming) dto.OperationTiming {
	return dto.OperationTiming{
		SessionID:       value.SessionID,
		AgentID:         value.AgentID,
		TaskID:          value.TaskID,
		Kind:            value.Kind,
		Name:            value.Name,
		ReferenceID:     value.ReferenceID,
		ID:              value.ID,
		ParentID:        value.ParentID,
		StartedAt:       value.StartedAt,
		FinishedAt:      value.FinishedAt,
		DurationMS:      value.DurationMS,
		FirstResponseMS: value.FirstResponseMS,
		Status:          value.Status,
		Revision:        value.Revision,
	}
}
