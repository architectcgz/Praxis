package toolexecution

import (
	"context"
	agentexecution "praxis/internal/core/agent/execution"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/tool"
)

// CommitResult 接纳工具实际返回的回执；Loop 退出时只为尚无回执的调用补中断条目。
func (s *Service) CommitResult(ctx context.Context, runtime *agentexecution.Runtime, call sessionentry.ToolCall, result tool.ToolResult) (sessionentry.ContextEntry, error) {
	value := call.ResultEntry(result)
	if runtime.Main() {
		stored, _, err := s.deps.Entries.Append(ctx, sessionentry.Record{
			SessionID: call.SessionID,
			AgentID:   call.AgentID,
			Data:      value,
		})
		if err != nil {
			return sessionentry.ContextEntry{}, err
		}
		value = stored
	}
	accepted, _, err := runtime.Accept(value)
	return accepted, err
}
