package session

import (
	"context"
	"errors"

	agentruntime "praxis/internal/agent_runtime"
	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/repository"
)

// MessageStore 按 Agent 的可见范围在 Session 消息和 Agent 私有消息之间路由。
type MessageStore struct {
	tx       repository.TxRunner
	agents   repository.SessionAgentRepository
	sessions repository.SessionMessageRepository
	private  repository.AgentMessageRepository
}

// NewMessageStore 创建 Session 消息访问路由。
func NewMessageStore(
	tx repository.TxRunner,
	agents repository.SessionAgentRepository,
	sessions repository.SessionMessageRepository,
	private repository.AgentMessageRepository,
) MessageStore {
	return MessageStore{
		tx:       tx,
		agents:   agents,
		sessions: sessions,
		private:  private,
	}
}

func (r MessageStore) resolve(
	ctx context.Context,
	sessionID contracts.SessionID,
	agentID contracts.AgentID,
) (agentruntime.TurnMessageStore, agentmodel.Agent, error) {
	if ctx == nil {
		return nil, agentmodel.Agent{}, errors.New("message route context is required")
	}
	agent, err := r.agents.Get(ctx, agentID)
	if err != nil {
		return nil, agentmodel.Agent{}, err
	}
	if agent.SessionID != sessionID {
		return nil, agentmodel.Agent{}, contracts.ErrNotFound
	}
	return routedMessageStore{router: r, agent: agent}, agent, nil
}

// Resolve 为指定执行主体解析其 owner 限定的消息存储。
func (r MessageStore) Resolve(
	ctx context.Context,
	sessionID contracts.SessionID,
	agentID contracts.AgentID,
) (agentruntime.TurnMessageStore, error) {
	store, _, err := r.resolve(ctx, sessionID, agentID)
	return store, err
}

// LoadMessages 在一致性边界内读取 Agent 当前可见的消息流。
func (r MessageStore) LoadMessages(
	ctx context.Context,
	sessionID contracts.SessionID,
	agentID contracts.AgentID,
	limit int,
) (repository.MessageStream, error) {
	if r.tx == nil {
		return repository.MessageStream{}, errors.New("message stream transaction runner is required")
	}
	var stream repository.MessageStream
	err := r.tx.InTx(ctx, func(txCtx context.Context) error {
		_, agent, err := r.resolve(txCtx, sessionID, agentID)
		if err != nil {
			return err
		}
		if agent.CanReadSessionContext() {
			values, err := r.sessions.List(txCtx, sessionID, 0, limit)
			if err != nil {
				return err
			}
			boundary, err := r.sessions.LatestSequence(txCtx, sessionID)
			if err != nil {
				return err
			}
			stream = repository.MessageStream{SequenceBoundary: boundary, Messages: sessionMessageData(values)}
			return nil
		}
		values, err := r.private.List(txCtx, agentID, 0, limit)
		if err != nil {
			return err
		}
		boundary, err := r.private.LatestSequence(txCtx, agentID)
		if err != nil {
			return err
		}
		stream = repository.MessageStream{SequenceBoundary: boundary, Messages: agentMessageData(values)}
		return nil
	})
	return stream, err
}

// ListAgent 返回 Agent 当前可见的消息数据，供应用层查询使用。
func (r MessageStore) ListAgent(
	ctx context.Context,
	sessionID contracts.SessionID,
	agentID contracts.AgentID,
	limit int,
) ([]sessionmodel.MessageData, error) {
	stream, err := r.LoadMessages(ctx, sessionID, agentID, limit)
	if err != nil {
		return nil, err
	}
	return stream.Messages, nil
}

type routedMessageStore struct {
	router MessageStore
	agent  agentmodel.Agent
}

func (s routedMessageStore) Append(ctx context.Context, value sessionmodel.MessageData) (sessionmodel.MessageData, error) {
	if s.agent.CanReadSessionContext() {
		stored, err := s.router.sessions.Append(ctx, sessionMessage(s.agent.SessionID, value))
		return stored.Data, err
	}
	stored, err := s.router.private.Append(ctx, agentmodel.AgentMessage{AgentID: s.agent.ID, Data: value})
	return stored.Data, err
}

func (s routedMessageStore) ListByTurn(ctx context.Context, turnID contracts.TurnID) ([]sessionmodel.MessageData, error) {
	if s.agent.CanReadSessionContext() {
		values, err := s.router.sessions.ListByTurn(ctx, s.agent.SessionID, turnID)
		return sessionMessageData(values), err
	}
	values, err := s.router.private.ListByTurn(ctx, s.agent.ID, turnID)
	return agentMessageData(values), err
}

func sessionMessage(id contracts.SessionID, value sessionmodel.MessageData) sessionmodel.SessionMessage {
	return sessionmodel.SessionMessage{SessionID: id, Data: value}
}

func sessionMessageData(values []sessionmodel.SessionMessage) []sessionmodel.MessageData {
	result := make([]sessionmodel.MessageData, len(values))
	for index, value := range values {
		result[index] = value.Data
	}
	return result
}

func agentMessageData(values []agentmodel.AgentMessage) []sessionmodel.MessageData {
	result := make([]sessionmodel.MessageData, len(values))
	for index, value := range values {
		result[index] = value.Data
	}
	return result
}
