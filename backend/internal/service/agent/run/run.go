package run

import (
	"context"
	agentmodel "praxis/internal/core/agent"
	agentrun "praxis/internal/core/agent/run"
	modelapi "praxis/internal/core/model"
	modelconfig "praxis/internal/core/model/config"
	sessionmodel "praxis/internal/core/session"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

// SubagentInput 表达委派准入所需的父调用、冻结配置及历史归属，不进入主会话输入队列。
type SubagentInput struct {
	Task           sessionmodel.Task
	AgentID        identity.AgentID
	ParentRunID    identity.RunID
	ParentCallID   string
	Definition     agentmodel.AgentDefinition
	Model          modelapi.Config
	Owner          agentrun.SessionContext
	BindingID      string
	SourceBranchID identity.BranchID
}

func (s *Service) PrepareTask(ctx context.Context, input sessionmodel.Task, agentID identity.AgentID, definition agentmodel.AgentDefinition, model modelapi.Config, owner agentrun.SessionContext, bindingID string) (*agentrun.Run, bool, error) {
	session, err := s.sessions.Get(ctx, input.SessionID)
	if err != nil {
		return nil, false, err
	}
	if session.State != sessionmodel.SessionActive || agentID != session.MainAgentID() || definition.ID != agentmodel.DefinitionPrimary {
		return nil, false, errorcode.ErrRequestConflict
	}
	record, existing, err := s.prepareMainTask(ctx, input, session, owner, bindingID, model, definition)
	if err != nil {
		return nil, existing, err
	}
	entry, err := owner.Entry(ctx, record.InputEntryID)
	if err != nil {
		return nil, existing, err
	}
	run, err := agentrun.NewRun(record, true, []*sessionentry.ContextEntry{entry})
	if err == nil {
		run.Session, run.BindingID = owner, bindingID
	}
	return run, existing, err
}

func (s *Service) PrepareSubagent(ctx context.Context, input SubagentInput) (*agentrun.Run, error) {
	if input.Model.ProviderID == "" || input.Model.ModelID == "" {
		return nil, modelconfig.ErrNotConfigured
	}
	at := s.clock.Now().UTC()
	record := agentrun.NewRunRecord(identity.RunID(s.ids.New("execution")), input.Task.SessionID, input.AgentID, input.Task.ID, "input:"+input.Task.ID.String(), agentrun.RunConfig{
		AgentDefinitionID: input.Definition.ID,
		SystemPrompt:      input.Definition.Instructions,
		Model:             input.Model,
	}, at)
	record.AgentName = input.Definition.Name
	record.BranchID = input.SourceBranchID
	record.ParentRunID, record.ParentCallID = input.ParentRunID, input.ParentCallID
	stored, err := s.records.CreateSubagent(ctx, input.SourceBranchID, record, sessionentry.ContextEntry{
		ID:         record.InputEntryID,
		CreatedAt:  at,
		RunID:      record.ID.String(),
		TurnID:     record.CurrentTurn.ID.String(),
		Role:       role.User,
		AuthorKind: sessionentry.AuthorUser,
		Content:    []sessionentry.ContentBlock{sessionentry.TextBlock{Text: input.Task.Prompt}},
	})
	if err != nil {
		return nil, err
	}
	run, err := agentrun.NewRun(record, false, []*sessionentry.ContextEntry{stored})
	if err != nil {
		return nil, err
	}
	run.Session, run.BindingID = input.Owner, input.BindingID
	return run, nil
}
