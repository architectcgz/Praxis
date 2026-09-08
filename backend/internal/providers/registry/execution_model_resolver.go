package registry

import (
	agentruntime "praxis/internal/application/agent_runtime"
	domainsecurity "praxis/internal/domain/security"
)

// ResolveExecutionModel materializes a configured model selection into the
// runtime stream and resource limits required for one execution.
func (r *Registry) ResolveExecutionModel(selection domainsecurity.ModelSelection) (agentruntime.ExecutionModel, error) {
	model, err := r.Model(selection.ProviderID, selection.ModelID)
	if err != nil {
		return agentruntime.ExecutionModel{}, err
	}
	stream, err := r.StreamFor(selection)
	if err != nil {
		return agentruntime.ExecutionModel{}, err
	}
	return agentruntime.ExecutionModel{
		Stream: stream, ContextWindow: model.ContextWindow, MaxOutputTokens: model.MaxOutputTokens,
	}, nil
}
