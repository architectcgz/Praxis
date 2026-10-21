package session

import (
	"context"
	messagemodel "praxis/internal/core/message"
	"praxis/internal/fault"
	"praxis/internal/identity"
	"praxis/internal/repository"
)

// AppendMessage 追加共享输入；绑定 Execution 的输入和输出由执行用例原子提交。
func (s *Service) AppendMessage(ctx context.Context, sessionID identity.SessionID, agentID identity.AgentID, data messagemodel.Message) (stored messagemodel.Message, inserted bool, err error) {
	err = func(txCtx context.Context) error {
		if err := RequireWritable(txCtx, s.sessions, sessionID); err != nil {
			return err
		}
		owner, err := s.agents.Get(txCtx, agentID)
		if err != nil {
			return err
		}
		if owner.SessionID != sessionID || !owner.CanReadSessionContext() || data.Role != messagemodel.RoleUser || data.ExecutionID != "" || data.SourceTaskID != "" {
			return fault.ErrRequestConflict
		}
		stored, inserted, err = s.messages.Append(txCtx, messagemodel.Record{Stream: messagemodel.StreamKey{SessionID: sessionID}, AgentID: agentID, Data: data})
		return err
	}(ctx)
	if err != nil {
		return messagemodel.Message{}, false, err
	}
	return stored, inserted, nil
}

// ListMessages 只读取共享流，Session 全量消息索引不作为可见历史。
func (s *Service) ListMessages(ctx context.Context, sessionID identity.SessionID, after uint64, limit int) (page repository.MessagePage, err error) {
	err = func(txCtx context.Context) error {
		if _, err := s.sessions.Get(txCtx, sessionID); err != nil {
			return err
		}
		var err error
		page, err = s.messages.List(txCtx, repository.MessageQuery{Stream: messagemodel.StreamKey{SessionID: sessionID}, After: after, Limit: limit})
		return err
	}(ctx)
	return page, err
}
