package bindings

import (
	"praxis/internal/core/agent/execution/timing"
	"praxis/internal/identity"
	"praxis/wails/dto"
)

func (b *AgentBindings) ListAgentTimings(sessionID, branchID, agentID string) ([]dto.OperationTiming, error) {
	if err := validateAgentQuery(sessionID, branchID, agentID); err != nil {
		return nil, publicInputError(b.host, "AgentBindings.ListAgentTimings.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return nil, err
	}
	_, err = services.Agents.GetAgentView(ctx, identity.SessionID(sessionID), identity.AgentID(agentID), identity.BranchID(branchID))
	if err != nil {
		return nil, publicError(b.host, "ListAgentTimings.scope", err)
	}
	records, err := services.Timings.ListAgentTimings(ctx, agentID, identity.BranchID(branchID), 2000)
	if err != nil {
		return nil, publicError(b.host, "AgentBindings.ListAgentTimings", err)
	}
	result := make([]dto.OperationTiming, 0, len(records))
	for _, record := range records {
		result = append(result, operationTiming(record))
	}
	return result, nil
}

func operationTiming(value timing.View) dto.OperationTiming {
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
