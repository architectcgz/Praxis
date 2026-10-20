package execution

import (
	"context"
	agentmodel "praxis/internal/core/agent"
	modelapi "praxis/internal/core/model"
	"praxis/internal/fault"
)

// buildConfig 解析模型配置并确定本次执行输入；任一步失败都不产生执行输入。
func (s *Service) buildConfig(
	ctx context.Context,
	agent agentmodel.Agent,
	providerID string,
	modelID string,
	reasoningLevel string,
) (agentmodel.ExecutionConfig, error) {
	definition, err := s.definitions.Definition(agent.DefinitionID)
	if err != nil {
		return agentmodel.ExecutionConfig{}, err
	}
	if definition.Profile != agent.Profile {
		return agentmodel.ExecutionConfig{}, fault.ErrRevisionConflict
	}
	var modelConfig modelapi.Config
	switch {
	case providerID == "" && modelID == "" && reasoningLevel == "":
		reference, resolveErr := s.definitions.ResolveModelReference(agent.DefinitionID)
		if resolveErr != nil {
			return agentmodel.ExecutionConfig{}, fault.New(fault.InvalidRequest, "")
		}
		modelConfig, err = s.models.ResolveModelConfig(reference.ProviderID, reference.ModelID, "")
	case providerID == "" || modelID == "":
		return agentmodel.ExecutionConfig{}, fault.New(fault.InvalidRequest, "")
	default:
		modelConfig, err = s.models.ResolveModelConfig(providerID, modelID, reasoningLevel)
	}
	if err != nil {
		return agentmodel.ExecutionConfig{}, fault.New(fault.InvalidRequest, "")
	}
	config := agentmodel.ExecutionConfig{
		AgentDefinitionID: definition.ID,
		SystemPrompt:      definition.Instructions,
		Model:             modelConfig,
	}
	return config, nil
}
