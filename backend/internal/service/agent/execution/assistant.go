package execution

import (
	"context"
	"errors"
	messagemodel "praxis/internal/core/message"
	"praxis/internal/fault"
	"praxis/internal/identity"
	"praxis/internal/loop"
	agentservice "praxis/internal/service/agent"
	sessionservice "praxis/internal/service/session"
)

type assistantCommitter struct {
	service     *Service
	executionID identity.AgentExecutionID
	agentID     identity.AgentID
	stream      messagemodel.StreamKey
}

// AssistantCommitter 校验归属并冻结本次输出流；返回的是业务提交能力。
func (s *Service) AssistantCommitter(ctx context.Context, id identity.AgentExecutionID) (loop.AssistantCommitter, error) {
	var committer assistantCommitter
	err := func(txCtx context.Context) error {
		execution, err := s.records.Get(txCtx, id)
		if err != nil {
			return err
		}
		owner, err := s.agents.Get(txCtx, execution.AgentID)
		if err != nil {
			return err
		}
		if !execution.Active() || owner.CurrentExecutionID != id || owner.SessionID != execution.SessionID {
			return fault.ErrRequestConflict
		}
		if err := sessionservice.RequireWritable(txCtx, s.sessions, execution.SessionID); err != nil {
			return err
		}
		committer = assistantCommitter{
			service:     s,
			executionID: id,
			agentID:     owner.ID,
			stream:      agentservice.MessageStream(owner),
		}
		return nil
	}(ctx)
	if err != nil {
		return nil, err
	}
	return committer, nil
}

// AppendAssistant 保存 Assistant 消息；完成证据与工具调用计数由消息派生。
func (w assistantCommitter) AppendAssistant(ctx context.Context, value loop.AssistantWrite) (stored messagemodel.Message, inserted bool, err error) {
	message := value.Message
	if ctx == nil || message.Role != messagemodel.RoleAssistant || message.AuthorKind != messagemodel.AuthorAgent || message.AuthorID != w.agentID.String() || message.TurnID == "" {
		return stored, false, errors.New("assistant ownership is required")
	}
	err = func(txCtx context.Context) error {
		if err := sessionservice.RequireWritable(txCtx, w.service.sessions, w.stream.SessionID); err != nil {
			return err
		}
		owner, err := w.service.agents.Get(txCtx, w.agentID)
		if err != nil {
			return err
		}
		if agentservice.MessageStream(owner) != w.stream {
			return fault.ErrRequestConflict
		}
		execution, err := w.service.records.Get(txCtx, identity.AgentExecutionID(message.ExecutionID))
		if err != nil {
			return err
		}
		if execution.ID != w.executionID || execution.SessionID != w.stream.SessionID || execution.AgentID != w.agentID || message.RequestID != execution.RequestID.String() {
			return fault.ErrRequestConflict
		}
		if value.FinalOutput != nil {
			if value.FinalOutput.AssistantID != message.ID || value.FinalOutput.StopReason != message.StopReason || message.StopReason != "completed" && message.StopReason != "stop_sequence" {
				return fault.ErrRequestConflict
			}
		}
		stored, inserted, err = w.service.messages.Append(txCtx, messagemodel.Record{Stream: w.stream, AgentID: w.agentID, Data: message})
		if err != nil {
			return err
		}
		if !inserted {
			return nil
		}
		return nil
	}(ctx)
	if err != nil {
		return messagemodel.Message{}, false, err
	}
	return stored, inserted, nil
}
