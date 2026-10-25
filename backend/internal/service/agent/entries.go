package agent

import (
	"context"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/repository"
	sessionservice "praxis/internal/service/session"
)

// AppendEntry 只追加 Main 的独立输入；子执行必须通过显式委派输入创建 Runtime。
func (s *Service) AppendEntry(ctx context.Context, agentID identity.AgentID, data sessionentry.ContextEntry) (stored sessionentry.ContextEntry, inserted bool, err error) {
	err = func(txCtx context.Context) error {
		owner, err := s.agent(agentID)
		if err != nil {
			return err
		}
		if !owner.IsMain() || data.Role != role.User || data.ExecutionID != "" || data.SourceTaskID != "" {
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

// ListAgentEntries 查询 Main 的持久化历史或子执行的活跃内存快照。
func (s *Service) ListAgentEntries(ctx context.Context, agentID identity.AgentID) ([]sessionentry.ContextEntry, error) {
	var entries []sessionentry.ContextEntry
	err := func(ctx context.Context) error {
		agent, err := s.agent(agentID)
		if err != nil {
			return err
		}
		if _, err := s.sessions.Get(ctx, agent.SessionID); err != nil {
			return err
		}
		if !agent.IsMain() {
			entries = s.activeEntries(agentID)
			return nil
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
