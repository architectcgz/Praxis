package bindings

import (
	executionservice "praxis/internal/service/agent/execution"
	"praxis/wails/dto"
	"praxis/wails/validation"
)

func (b *AgentBindings) ListAgentTimings(agentID string) ([]dto.OperationTiming, error) {
	if err := validation.ValidateAgentID(agentID); err != nil {
		return nil, err
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return nil, err
	}
	records, err := services.Timings.ListAgentTimings(ctx, agentID, 2000)
	if err != nil {
		return nil, publicError(b.host, "AgentBindings.ListAgentTimings", err)
	}
	result := make([]dto.OperationTiming, 0, len(records))
	for _, record := range records {
		result = append(result, operationTiming(record))
	}
	return result, nil
}

func operationTiming(value executionservice.Timing) dto.OperationTiming {
	return dto.OperationTiming{
		SessionID:       value.SessionID,
		AgentID:         value.AgentID,
		ExecutionID:     value.ExecutionID,
		Kind:            string(value.Kind),
		Name:            value.Name,
		ReferenceID:     value.ReferenceID,
		ID:              value.ID,
		ParentID:        value.ParentID,
		StartedAt:       value.StartedAt,
		FinishedAt:      value.FinishedAt,
		DurationMS:      clonePointer(value.DurationMS),
		FirstResponseMS: clonePointer(value.FirstResponseMS),
		Status:          string(value.Status),
		Revision:        value.Revision,
	}
}

func clonePointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
