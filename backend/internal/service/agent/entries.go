package agent

import (
	"context"
	"errors"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/repository"
	sessionservice "praxis/internal/service/session"
)

// AppendEntry 向 Agent 自身追加输入；Main 与 Subagent 使用相同的消息存取规则。
func (s *Service) AppendEntry(ctx context.Context, agentID identity.AgentID, data sessionentry.ContextEntry) (stored sessionentry.ContextEntry, inserted bool, err error) {
	err = func(txCtx context.Context) error {
		owner, err := s.agent(agentID)
		if err != nil {
			return err
		}
		if data.Role != role.User || data.ExecutionID != "" || data.SourceTaskID != "" {
			return errorcode.ErrRequestConflict
		}
		if err := sessionservice.RequireWritable(txCtx, s.sessions, owner.SessionID); err != nil {
			return err
		}
		stored, inserted, err = s.entries.Append(txCtx, sessionentry.Record{SessionID: owner.SessionID, AgentID: owner.ID, Data: data})
		return err
	}(ctx)
	if err != nil {
		return sessionentry.ContextEntry{}, false, err
	}
	return stored, inserted, nil
}

// ListAgentEntries 读取 Agent 自有消息流。
func (s *Service) ListAgentEntries(ctx context.Context, agentID identity.AgentID) ([]sessionentry.ContextEntry, error) {
	if ctx == nil {
		return nil, errors.New("agent message context is required")
	}
	var entries []sessionentry.ContextEntry
	err := func(ctx context.Context) error {
		agent, err := s.agent(agentID)
		if err != nil {
			return err
		}
		if _, err := s.sessions.Get(ctx, agent.SessionID); err != nil {
			return err
		}
		page, err := s.entries.List(ctx, repository.EntryQuery{AgentID: agent.ID})
		if err != nil {
			return err
		}
		entries = page.Entries
		return nil
	}(ctx)
	return entries, err
}
