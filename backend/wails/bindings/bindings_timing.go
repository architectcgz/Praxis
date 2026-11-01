package bindings

import (
	"praxis/internal/core/agent/execution/timing"
	"praxis/wails/dto"
)

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
