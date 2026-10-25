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
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	sessionservice "praxis/internal/service/session"
)

type assistantCommitter struct {
	service     *Service
	executionID identity.AgentExecutionID
	agentID     identity.AgentID
	sessionID   identity.SessionID
}

// AssistantCommitter 校验执行并绑定 Agent；后续输出与摘要都追加到该 Agent 的唯一历史。
func (s *Service) AssistantCommitter(ctx context.Context, id identity.AgentExecutionID) (loop.AssistantCommitter, error) {
	var committer assistantCommitter
	err := func(txCtx context.Context) error {
		execution, err := s.records.Get(txCtx, id)
		if err != nil {
			return err
		}
		if !execution.Active() {
			return errorcode.ErrRequestConflict
		}
		if err := sessionservice.RequireWritable(txCtx, s.sessions, execution.SessionID); err != nil {
			return err
		}
		committer = assistantCommitter{
			service:     s,
			executionID: id,
			agentID:     execution.AgentID,
			sessionID:   execution.SessionID,
		}
		return nil
	}(ctx)
	if err != nil {
		return nil, err
	}
	return committer, nil
}

// AppendAssistant 保存 Assistant 消息；完成证据与工具调用计数由消息派生。
func (w assistantCommitter) AppendAssistant(ctx context.Context, message sessionentry.ContextEntry) (stored sessionentry.ContextEntry, inserted bool, err error) {
	if message.Role != role.Assistant || message.AuthorKind != sessionentry.AuthorAgent || message.AuthorID != w.agentID.String() || message.TurnID == "" {
		return stored, false, errors.New("assistant ownership is required")
	}
	err = func(txCtx context.Context) error {
		if err := sessionservice.RequireWritable(txCtx, w.service.sessions, w.sessionID); err != nil {
			return err
		}
		execution, err := w.service.records.Get(txCtx, identity.AgentExecutionID(message.ExecutionID))
		if err != nil {
			return err
		}
		if execution.ID != w.executionID || execution.SessionID != w.sessionID || execution.AgentID != w.agentID || message.RequestID != execution.RequestID.String() {
			return errorcode.ErrRequestConflict
		}
		stored, inserted, err = w.service.entries.Append(txCtx, sessionentry.Record{SessionID: w.sessionID, AgentID: w.agentID, Data: message})
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

// AppendSummary 将摘要及替代范围写入同一 Agent 历史，模型上下文由本次 Loop 独占。
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
	stored, _, err := w.service.entries.Append(ctx, sessionentry.Record{SessionID: w.sessionID, AgentID: w.agentID, Data: value})
	return stored, err
}
