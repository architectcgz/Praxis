package agent

import (
	"context"
	"errors"
	agentmodel "praxis/internal/core/agent"
	messagemodel "praxis/internal/core/message"
	"praxis/internal/fault"
	"praxis/internal/identity"
	"praxis/internal/repository"
	sessionservice "praxis/internal/service/session"
)

// MessageStream 是共享流与私有流的唯一业务选择规则；Agent 的 DefinitionID 不可变。
func MessageStream(owner agentmodel.Agent) messagemodel.StreamKey {
	stream := messagemodel.StreamKey{SessionID: owner.SessionID}
	if !owner.CanReadSessionContext() {
		stream.OwnerAgentID = owner.ID
	}
	return stream
}

// AppendMessage 追加非主 Agent 的私有输入；执行输出通过 Execution/ToolExecution 提交。
func (s *Service) AppendMessage(ctx context.Context, agentID identity.AgentID, data messagemodel.Message) (stored messagemodel.Message, inserted bool, err error) {
	err = func(txCtx context.Context) error {
		owner, err := s.agents.Get(txCtx, agentID)
		if err != nil {
			return err
		}
		if owner.CanReadSessionContext() || data.Role != messagemodel.RoleUser || data.ExecutionID != "" || data.SourceTaskID != "" {
			return fault.ErrRequestConflict
		}
		if err := sessionservice.RequireWritable(txCtx, s.sessions, owner.SessionID); err != nil {
			return err
		}
		stored, inserted, err = s.messages.Append(txCtx, messagemodel.Record{Stream: MessageStream(owner), AgentID: owner.ID, Data: data})
		return err
	}(ctx)
	if err != nil {
		return messagemodel.Message{}, false, err
	}
	return stored, inserted, nil
}

// ListAgentMessages 读取 Agent 自有消息流。
func (s *Service) ListAgentMessages(ctx context.Context, agentID identity.AgentID, limit int) ([]messagemodel.Message, error) {
	if ctx == nil {
		return nil, errors.New("agent message context is required")
	}
	if s.messages == nil {
		return nil, errors.New("agent message query is unavailable")
	}
	var messages []messagemodel.Message
	err := func(ctx context.Context) error {
		agent, err := s.agents.Get(ctx, agentID)
		if err != nil {
			return err
		}
		if _, err := s.sessions.Get(ctx, agent.SessionID); err != nil {
			return err
		}
		stream, err := s.messages.List(ctx, repository.MessageQuery{Stream: MessageStream(agent), Limit: limit, Tail: true})
		if err != nil {
			return err
		}
		executions, err := s.executionRecords.ListByAgent(ctx, agentID, 0)
		if err != nil {
			return err
		}
		sources := make(map[string]string, len(executions))
		for _, execution := range executions {
			if execution.SourceTaskID != "" {
				sources[execution.SourceTaskID.String()] = execution.ID.String()
			}
		}
		for index := range stream.Messages {
			value := &stream.Messages[index]
			if value.ExecutionID == "" && value.SourceTaskID != "" {
				value.ExecutionID = sources[value.SourceTaskID]
			}
		}

		messages = stream.Messages
		return nil
	}(ctx)
	return messages, err
}
