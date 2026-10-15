package task

import (
	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/model"
	taskmodel "praxis/internal/core/task"

	"context"
)

// buildInputSnapshot 冻结本次执行的可见范围与模型快照；任一步失败都不产生执行快照。
func (s *Service) buildInputSnapshot(
	ctx context.Context,
	agent agentmodel.Agent,
	providerID string,
	modelID string,
	reasoningLevel string,
	currentInput string,
	currentInputMessageID string,
) (taskmodel.InputSnapshot, error) {
	session, err := s.sessions.Get(ctx, agent.SessionID)
	if err != nil {
		return taskmodel.InputSnapshot{}, err
	}
	workspace, err := s.workspaces.Get(ctx, session.WorkspaceID)
	if err != nil {
		return taskmodel.InputSnapshot{}, err
	}
	definition, err := s.definitions.Definition(agent.DefinitionID)
	if err != nil {
		return taskmodel.InputSnapshot{}, err
	}
	if definition.Profile != agent.Profile {
		return taskmodel.InputSnapshot{}, contracts.ErrRevisionConflict
	}
	var modelSnapshot model.ModelSnapshot
	switch {
	case providerID == "" && modelID == "" && reasoningLevel == "":
		modelSnapshot, err = s.models.FreezeDefaultTaskModel(agent.DefinitionID)
	case providerID == "" || modelID == "":
		return taskmodel.InputSnapshot{}, contracts.New(contracts.InvalidRequest, "")
	default:
		modelSnapshot, err = s.models.FreezeTaskModel(providerID, modelID, reasoningLevel)
	}
	if err != nil {
		return taskmodel.InputSnapshot{}, contracts.New(contracts.InvalidRequest, "")
	}
	built, err := s.contextBuilder.BuildInitialContext(
		ctx, agent, definition.Instructions, currentInput, currentInputMessageID,
		modelSnapshot.ContextWindow, modelSnapshot.MaxOutputTokens,
	)
	if err != nil {
		return taskmodel.InputSnapshot{}, err
	}
	refs := make([]taskmodel.ContextReference, 0, len(built.SessionContextRefs))
	for _, ref := range built.SessionContextRefs {
		refs = append(refs, taskmodel.ContextReference{ID: ref.ID, Digest: ref.Digest})
	}
	return taskmodel.InputSnapshot{
		AgentDefinitionID:       definition.ID,
		AgentDefinitionRevision: definition.Revision,
		MessageSequenceBoundary: built.MessageSequenceBoundary,
		CurrentInputMessageID:   built.CurrentInputMessageID,
		SystemPrompt:            definition.Instructions,
		VisibleMessageIDs:       built.VisibleMessageIDs,
		SessionContextRefs:      refs,
		Model:                   modelSnapshot,
		WorkspacePath:           workspace.Path,
	}, nil
}
