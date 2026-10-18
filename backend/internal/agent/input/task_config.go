package service

import (
	"context"
	agentmodel "praxis/internal/agent"
	"praxis/internal/fault"
	modelapi "praxis/internal/model"
)

// buildConfig 解析模型配置并确定本次执行输入；任一步失败都不产生执行输入。
func (s *Service) buildConfig(
	ctx context.Context,
	agent agentmodel.Agent,
	providerID string,
	modelID string,
	reasoningLevel string,
) (agentmodel.ExecutionConfig, error) {
	session, err := s.sessions.Get(ctx, agent.SessionID)
	if err != nil {
		return agentmodel.ExecutionConfig{}, err
	}
	workspace, err := s.workspaces.Get(ctx, session.WorkspaceID)
	if err != nil {
		return agentmodel.ExecutionConfig{}, err
	}
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
		modelConfig, err = s.models.ResolveTaskModel(reference.ProviderID, reference.ModelID, "")
	case providerID == "" || modelID == "":
		return agentmodel.ExecutionConfig{}, fault.New(fault.InvalidRequest, "")
	default:
		modelConfig, err = s.models.ResolveTaskModel(providerID, modelID, reasoningLevel)
	}
	if err != nil {
		return agentmodel.ExecutionConfig{}, fault.New(fault.InvalidRequest, "")
	}
	config := agentmodel.ExecutionConfig{
		AgentDefinitionID: definition.ID,
		SystemPrompt:      definition.Instructions,
		Model:             modelConfig,
		WorkspacePath:     workspace.Path,
	}
	return config, nil
}
