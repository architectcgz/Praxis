package execution

import (
	"context"
	agentmodel "praxis/internal/core/agent"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/core/agent/execution/timing"
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
	Task              sessionmodel.Task
	AgentID           identity.AgentID
	ParentExecutionID identity.AgentExecutionID
	ParentCallID      string
	Definition        agentmodel.AgentDefinition
	Model             modelapi.Config
	Owner             agentexecution.SessionContext
	BindingID         string
}

func (s *Service) PrepareTask(ctx context.Context, input sessionmodel.Task, agentID identity.AgentID, definition agentmodel.AgentDefinition, model modelapi.Config, owner agentexecution.SessionContext, bindingID string) (*agentexecution.Run, bool, error) {
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
	entries, err := owner.BranchEntries(ctx, input.BranchID)
	if err != nil {
		return nil, existing, err
	}
	run, err := agentexecution.NewRun(record, true, entries)
	if err == nil {
		run.Session, run.BindingID = owner, bindingID
		s.bindTiming(run)
	}
	return run, existing, err
}

func (s *Service) PrepareSubagent(ctx context.Context, input SubagentInput) (*agentexecution.Run, error) {
	if input.Model.ProviderID == "" || input.Model.ModelID == "" {
		return nil, modelconfig.ErrNotConfigured
	}
	at := s.clock.Now().UTC()
	requestID := identity.RequestID(input.Task.ID)
	record := agentexecution.NewExecutionRecord(identity.AgentExecutionID(s.ids.New("execution")), input.Task.SessionID, input.AgentID, requestID, "input:"+requestID.String(), agentexecution.ExecutionConfig{
		AgentDefinitionID: input.Definition.ID,
		SystemPrompt:      input.Definition.Instructions,
		Model:             input.Model.Clone(),
	}, at)
	record.SourceTaskID = input.Task.ID
	record.AgentName = input.Definition.Name
	record.BranchID = input.Task.BranchID
	record.ParentExecutionID, record.ParentCallID = input.ParentExecutionID, input.ParentCallID
	stored, err := s.records.CreateSubagent(ctx, record, sessionentry.ContextEntry{
		ID:           record.InputEntryID,
		CreatedAt:    at,
		RequestID:    requestID.String(),
		SourceTaskID: input.Task.ID.String(),
		ExecutionID:  record.ID.String(),
		Role:         role.User,
		AuthorKind:   sessionentry.AuthorUser,
		Content:      []sessionentry.Block{{Kind: sessionentry.BlockText, Text: input.Task.Prompt}},
	})
	if err != nil {
		return nil, err
	}
	run, err := agentexecution.NewRun(record, false, []*sessionentry.ContextEntry{stored})
	if err != nil {
		return nil, err
	}
	run.Session, run.BindingID = input.Owner, input.BindingID
	s.bindTiming(run)
	return run, nil
}

func (s *Service) bindTiming(run *agentexecution.Run) {
	run.Timing = timing.New(func(record timing.Record) { s.RecordTiming(run, record) })
}
