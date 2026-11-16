package bindings

import (
	"praxis/internal/core/entry"
	"praxis/internal/core/session"
	"praxis/internal/core/timing"
	"praxis/wails/dto"
)

func publicRun(r entry.RunSnapshot) dto.RunSummary {
	return dto.RunSummary{
		ID:             r.ID.String(),
		Status:         string(r.Status),
		Outcome:        string(r.Outcome),
		StartedAt:      r.StartedAt,
		EndedAt:        r.EndedAt,
		FailureCode:    string(r.FailureCode),
		FailureMessage: r.FailureMessage,
		Iterations:     r.Iterations,
		ToolCalls:      r.ToolCalls,
		OutputBytes:    r.OutputBytes,
	}
}
func publicAgents(values []session.AgentView) []dto.AgentView {
	result := make([]dto.AgentView, 0, len(values))
	for _, v := range values {
		out := dto.AgentView{
			ID:           v.ID.String(),
			SessionID:    v.SessionID.String(),
			DefinitionID: v.DefinitionID.String(),
			Kind:         string(v.Kind),
			CreatedAt:    v.CreatedAt,
			UpdatedAt:    v.UpdatedAt,
			Name:         v.Name,
			State:        string(v.State),
			CurrentRunID: v.CurrentRunID.String(),
		}
		if v.CurrentRun != nil {
			out.CurrentRun = new(publicRun(*v.CurrentRun))
		}
		if v.LatestRun != nil {
			out.LatestRun = new(publicRun(*v.LatestRun))
		}
		result = append(result, out)
	}
	return result
}

func publicTiming(value *timing.Record) *dto.Timing {
	if value == nil {
		return nil
	}
	return &dto.Timing{
		StartedAt:       value.StartedAt,
		FinishedAt:      value.FinishedAt,
		DurationMS:      value.DurationMS,
		FirstResponseMS: value.FirstResponseMS,
	}
}
