package modelregistry

import (
	agentruntime "praxis/internal/application/agent_runtime"
	domainmodel "praxis/internal/domain/model"
)

// ResolveExecutionModel materializes a configured model selection into the
// runtime stream and resource limits required for one execution.
func (r *Registry) ResolveExecutionModel(selection domainmodel.ModelSelection) (agentruntime.ExecutionModel, error) {
	if err := selection.Validate(); err != nil {
		return agentruntime.ExecutionModel{}, err
	}
	model, err := r.Model(selection.ProviderID, selection.ModelID)
	if err != nil {
		return agentruntime.ExecutionModel{}, err
	}
	selected, err := r.selectModel(selection.ProviderID, model, selection.ReasoningLevel)
	if err != nil {
		return agentruntime.ExecutionModel{}, err
	}
	stream, err := r.StreamFor(selected.Selection)
	if err != nil {
		return agentruntime.ExecutionModel{}, err
	}
	return agentruntime.ExecutionModel{
		Stream: stream, ContextWindow: selected.ContextWindow, MaxOutputTokens: selected.MaxOutputTokens,
	}, nil
}
