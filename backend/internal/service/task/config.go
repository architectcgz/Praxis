package task

import (
	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/model"
	taskmodel "praxis/internal/core/task"

	"context"
)

// buildConfig 解析模型配置并确定本次执行输入；任一步失败都不产生执行输入。
func (s *Service) buildConfig(
	ctx context.Context,
	agent agentmodel.Agent,
	providerID string,
	modelID string,
	reasoningLevel string,
) (taskmodel.Config, error) {
	session, err := s.sessions.Get(ctx, agent.SessionID)
	if err != nil {
		return taskmodel.Config{}, err
	}
	workspace, err := s.workspaces.Get(ctx, session.WorkspaceID)
	if err != nil {
		return taskmodel.Config{}, err
	}
	definition, err := s.definitions.Definition(agent.DefinitionID)
	if err != nil {
		return taskmodel.Config{}, err
	}
	if definition.Profile != agent.Profile {
		return taskmodel.Config{}, contracts.ErrRevisionConflict
	}
	var modelConfig model.Config
	switch {
	case providerID == "" && modelID == "" && reasoningLevel == "":
		modelConfig, err = s.models.ResolveDefaultTaskModel(agent.DefinitionID)
	case providerID == "" || modelID == "":
		return taskmodel.Config{}, contracts.New(contracts.InvalidRequest, "")
	default:
		modelConfig, err = s.models.ResolveTaskModel(providerID, modelID, reasoningLevel)
	}
	if err != nil {
		return taskmodel.Config{}, contracts.New(contracts.InvalidRequest, "")
	}
	config := taskmodel.Config{
		AgentDefinitionID: definition.ID,
		SystemPrompt:      definition.Instructions,
		Model:             modelConfig,
		WorkspacePath:     workspace.Path,
	}
	return config, nil
}
