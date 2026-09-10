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
	r.mu.RLock()
	defer r.mu.RUnlock()
	model, selected, err := r.selectModelLocked(selection)
	if err != nil {
		return agentruntime.ExecutionModel{}, err
	}
	stream, err := r.streamForLocked(model.config, selected)
	if err != nil {
		return agentruntime.ExecutionModel{}, err
	}
	return agentruntime.ExecutionModel{
		Stream: stream, ContextWindow: model.config.ContextWindow, MaxOutputTokens: model.config.MaxOutputTokens,
	}, nil
}
