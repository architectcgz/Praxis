package compose

import (
	"praxis/internal/application/agent_runtime"
	domainsecurity "praxis/internal/domain/security"

	"praxis/internal/providers/registry"
)

type providerModelResolver struct {
	registry *registry.Registry
}

func (r providerModelResolver) ResolveExecutionModel(selection domainsecurity.ModelSelection) (agentruntime.ExecutionModel, error) {
	model, err := r.registry.Model(selection.ProviderID, selection.ModelID)
	if err != nil {
		return agentruntime.ExecutionModel{}, err
	}
	stream, err := r.registry.StreamFor(selection)
	if err != nil {
		return agentruntime.ExecutionModel{}, err
	}
	return agentruntime.ExecutionModel{
		Stream: stream, ContextWindow: model.ContextWindow, MaxOutputTokens: model.MaxOutputTokens,
	}, nil
}
