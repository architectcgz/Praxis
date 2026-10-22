package agent

import (
	"context"
	"errors"
	agentmodel "praxis/internal/core/agent"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/fault"
	"praxis/internal/identity"
	"praxis/internal/repository"
	sessionservice "praxis/internal/service/session"
)

// EntryStream 是共享流与私有流的唯一业务选择规则；Agent 的 DefinitionID 不可变。
func EntryStream(owner agentmodel.Agent) sessionentry.StreamKey {
	stream := sessionentry.StreamKey{SessionID: owner.SessionID}
	if !owner.CanReadSessionContext() {
		stream.OwnerAgentID = owner.ID
	}
	return stream
}

// AppendEntry 追加非主 Agent 的私有输入；执行输出通过 Execution/ToolExecution 提交。
func (s *Service) AppendEntry(ctx context.Context, agentID identity.AgentID, data sessionentry.ContextEntry) (stored sessionentry.ContextEntry, inserted bool, err error) {
	err = func(txCtx context.Context) error {
		owner, err := s.agents.Get(txCtx, agentID)
		if err != nil {
			return err
		}
		if owner.CanReadSessionContext() || data.Role != role.User || data.ExecutionID != "" || data.SourceTaskID != "" {
			return fault.ErrRequestConflict
		}
		if err := sessionservice.RequireWritable(txCtx, s.sessions, owner.SessionID); err != nil {
			return err
		}
		stored, inserted, err = s.entries.Append(txCtx, sessionentry.Record{Stream: EntryStream(owner), AgentID: owner.ID, Data: data})
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
	if s.entries == nil {
		return nil, errors.New("agent message query is unavailable")
	}
	var entries []sessionentry.ContextEntry
	err := func(ctx context.Context) error {
		agent, err := s.agents.Get(ctx, agentID)
		if err != nil {
			return err
		}
		if _, err := s.sessions.Get(ctx, agent.SessionID); err != nil {
			return err
		}
		stream, err := s.entries.List(ctx, repository.EntryQuery{Stream: EntryStream(agent)})
		if err != nil {
			return err
		}
		entries = stream.Entries
		return nil
	}(ctx)
	return entries, err
}
