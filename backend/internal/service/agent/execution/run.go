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

// PrepareTask 准入执行后创建 Run；主执行从分支路径共享 Entry，子执行使用私有输入。
func (s *Service) PrepareTask(ctx context.Context, input sessionmodel.Task, agentID identity.AgentID, definition agentmodel.AgentDefinition, model modelapi.Config, owner agentexecution.SessionContext, bindingID string) (*agentexecution.Run, bool, error) {
	session, err := s.sessions.Get(ctx, input.SessionID)
	if err != nil {
		return nil, false, err
	}
	if session.State != sessionmodel.SessionActive {
		return nil, false, errorcode.ErrRequestConflict
	}
	if agentID == session.MainAgentID() {
		if definition.ID != agentmodel.DefinitionPrimary {
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
	if model.ProviderID == "" || model.ModelID == "" {
		return nil, false, modelconfig.ErrNotConfigured
	}
	at := s.clock.Now().UTC()
	requestID := identity.RequestID(input.ID)
	record := agentexecution.NewExecutionRecord(identity.AgentExecutionID(s.ids.New("execution")), session.ID, agentID, requestID, "input:"+requestID.String(), agentexecution.ExecutionConfig{
		AgentDefinitionID: definition.ID,
		SystemPrompt:      definition.Instructions,
		Model:             model.Clone(),
	}, at)
	record.SourceTaskID = input.ID
	record.BranchID = input.BranchID
	run, err := agentexecution.NewRun(record, false, []*sessionentry.ContextEntry{{
		ID:          record.InputEntryID,
		Sequence:    1,
		CreatedAt:   at,
		RequestID:   requestID.String(),
		ExecutionID: record.ID.String(),
		Role:        role.User,
		AuthorKind:  sessionentry.AuthorUser,
		Content:     []sessionentry.Block{{Kind: sessionentry.BlockText, Text: input.Prompt}},
	}})
	if err != nil {
		return nil, false, err
	}
	s.bindTiming(run)
	run.BindingID = bindingID
	return run, false, nil
}

func (s *Service) bindTiming(run *agentexecution.Run) {
	run.Timing = timing.New(func(record timing.Record) { s.RecordTiming(run, record) })
}
