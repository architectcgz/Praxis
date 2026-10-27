package execution

import (
	"context"
	"errors"
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

// PrepareTask 根据业务实例和 Session 的 MainAgentID 决定保留边界，模型不能选择持久化路径。
func (s *Service) PrepareTask(ctx context.Context, input sessionmodel.Task, agentID identity.AgentID, model modelapi.Config) (*agentexecution.Run, bool, error) {
	owner, err := s.agent(agentID)
	if err != nil {
		return nil, false, err
	}
	session, err := s.sessions.Get(ctx, input.SessionID)
	if err != nil {
		return nil, false, err
	}
	if owner.SessionID != session.ID || owner.IsMain() != (owner.ID == session.MainAgentID) || session.State != sessionmodel.SessionActive {
		return nil, false, errorcode.ErrRequestConflict
	}
	if owner.IsMain() {
		record, existing, err := s.prepareMainTask(ctx, input, agentID, model)
		if err != nil {
			return nil, existing, err
		}
		run, err := s.RestoreRun(ctx, record)
		return run, existing, err
	}
	if model.ProviderID == "" || model.ModelID == "" {
		return nil, false, modelconfig.ErrNotConfigured
	}
	definition, err := s.definitions.Definition(owner.DefinitionID)
	if err != nil {
		return nil, false, err
	}
	at := s.clock.Now().UTC()
	requestID := identity.RequestID(input.ID)
	record := agentexecution.NewExecutionRecord(identity.AgentExecutionID(s.ids.New("execution")), session.ID, owner.ID, requestID, "input:"+requestID.String(), agentexecution.ExecutionConfig{
		AgentDefinitionID: definition.ID,
		SystemPrompt:      definition.Instructions,
		Model:             model.Clone(),
	}, at)
	record.SourceTaskID = input.ID
	run, err := agentexecution.NewRun(record, false, []sessionentry.ContextEntry{{
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
	if err := owner.BeginExecution(record); err != nil {
		return nil, false, err
	}
	s.bindTiming(run)
	return run, false, nil
}

// RestoreRun 从主会话历史恢复本次执行证据；跨执行上下文不保存在 Run 中。
func (s *Service) RestoreRun(ctx context.Context, record agentexecution.ExecutionRecord) (*agentexecution.Run, error) {
	session, err := s.sessions.Get(ctx, record.SessionID)
	if err != nil {
		return nil, err
	}
	if session.MainAgentID != record.AgentID {
		return nil, errors.New("subagent execution cannot be restored")
	}
	page, err := s.history.ListEntries(ctx, record.SessionID)
	if err != nil {
		return nil, err
	}
	run, err := agentexecution.NewRun(record, true, page.Entries)
	if err == nil {
		s.bindTiming(run)
	}
	return run, err
}

func (s *Service) bindTiming(run *agentexecution.Run) {
	run.Timing = timing.New(func(record timing.Record) { s.RecordTiming(run, record) })
}
