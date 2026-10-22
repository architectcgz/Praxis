package execution

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"praxis/internal/core/loop"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/fault"
	"praxis/internal/identity"
	agentservice "praxis/internal/service/agent"
	sessionservice "praxis/internal/service/session"
)

type assistantCommitter struct {
	service     *Service
	executionID identity.AgentExecutionID
	agentID     identity.AgentID
	stream      sessionentry.StreamKey
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
			stream:      agentservice.EntryStream(owner),
		}
		return nil
	}(ctx)
	if err != nil {
		return nil, err
	}
	return committer, nil
}

// AppendAssistant 保存 Assistant 消息；完成证据与工具调用计数由消息派生。
func (w assistantCommitter) AppendAssistant(ctx context.Context, value loop.AssistantWrite) (stored sessionentry.ContextEntry, inserted bool, err error) {
	message := value.Entry
	if ctx == nil || message.Role != role.Assistant || message.AuthorKind != sessionentry.AuthorAgent || message.AuthorID != w.agentID.String() || message.TurnID == "" {
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
		if agentservice.EntryStream(owner) != w.stream {
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
		stored, inserted, err = w.service.entries.Append(txCtx, sessionentry.Record{Stream: w.stream, AgentID: w.agentID, Data: message})
		if err != nil {
			return err
		}
		if !inserted {
			return nil
		}
		return nil
	}(ctx)
	if err != nil {
		return sessionentry.ContextEntry{}, false, err
	}
	return stored, inserted, nil
}

// AppendSummary 只保存摘要及替代范围，完整构建结果留在 Session 内存。
func (w assistantCommitter) AppendSummary(ctx context.Context, value sessionentry.ContextEntry) (sessionentry.ContextEntry, error) {
	if !value.IsSummary() {
		return sessionentry.ContextEntry{}, errors.New("summary entry requires its replaced history")
	}
	value.Role = role.User
	value.AuthorKind = sessionentry.AuthorSystem
	value.AuthorID = w.agentID.String()
	value.ExecutionID = w.executionID.String()
	execution, err := w.service.records.Get(ctx, w.executionID)
	if err != nil {
		return sessionentry.ContextEntry{}, err
	}
	value.RequestID = execution.RequestID.String()
	encoded, err := json.Marshal(struct {
		Content  []sessionentry.Block
		Replaces []string
	}{Content: value.Content, Replaces: value.Replaces})
	if err != nil {
		return sessionentry.ContextEntry{}, err
	}
	digest := sha256.Sum256(encoded)
	value.ID = fmt.Sprintf("summary:%s:%x", w.executionID, digest[:12])
	stored, _, err := w.service.entries.Append(ctx, sessionentry.Record{Stream: w.stream, AgentID: w.agentID, Data: value})
	return stored, err
}
