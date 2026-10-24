package toolexecution

import (
	"context"
	"errors"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/core/tool"
	executionmodel "praxis/internal/core/toolexecution"
	"praxis/internal/errorcode"
	sessionservice "praxis/internal/service/session"
	"time"
)

// commitResult 只保存消息回执；输出字节从回执派生，重复请求不会重复累计。
func (s *Service) commitResult(ctx context.Context, call sessionentry.ToolCall, data sessionentry.ContextEntry) (stored sessionentry.ContextEntry, inserted bool, err error) {
	err = func(txCtx context.Context) error {
		if err := sessionservice.RequireWritable(txCtx, s.deps.Sessions, call.SessionID); err != nil {
			return err
		}
		stored, inserted, err = s.deps.Entries.Append(txCtx, sessionentry.Record{SessionID: call.SessionID, AgentID: call.AgentID, Data: data})
		return err
	}(ctx)
	if err != nil {
		return sessionentry.ContextEntry{}, false, err
	}
	return stored, inserted, nil
}

// CommitResult 是唯一的正常回执路径，写入失败只向上传播存储错误。
func (s *Service) CommitResult(ctx context.Context, call sessionentry.ToolCall, result tool.ToolResult) (sessionentry.ContextEntry, error) {
	if len(result.Payload) > tool.MaxInlineResultBytes {
		result = tool.NewToolError("tool_execution_failed", "Tool result exceeds the inline size limit.")
	}
	if result.Payload == "" {
		result.Payload = "(empty tool result)"
	}
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	stored, _, err := s.commitResult(commitCtx, call, sessionentry.ContextEntry{
		ID:          "tool-result:" + call.ExecutionID.String() + ":" + call.CallID,
		RequestID:   call.RequestID,
		ExecutionID: call.ExecutionID.String(),
		TurnID:      call.TurnID.String(),
		Role:        role.Tool,
		AuthorKind:  sessionentry.AuthorTool,
		AuthorID:    string(call.Name),
		Content: []sessionentry.Block{{
			Kind:    sessionentry.BlockToolResult,
			CallID:  call.CallID,
			Name:    string(call.Name),
			Text:    result.Payload,
			IsError: result.ErrorClass != "",
		}},
	})
	return stored, err
}

// GetResult 统一将回执不存在与存储失败区分，完成依据始终来自消息配对。
func (s *Service) GetResult(ctx context.Context, key executionmodel.Key) (sessionentry.ContextEntry, bool, error) {
	value, err := s.deps.Calls.GetResult(ctx, key)
	if errors.Is(err, errorcode.ErrNotFound) {
		return sessionentry.ContextEntry{}, false, nil
	}
	return value, err == nil, err
}
