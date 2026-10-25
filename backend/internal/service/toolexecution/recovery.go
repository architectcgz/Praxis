package toolexecution

import (
	"context"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/tool"
	executionmodel "praxis/internal/core/toolexecution"
)

// RecoverAll 在准入开放前读取未完成调用，返回待补齐的历史条目，不保存或恢复审批状态。
// 调用方在旧执行者已停止的前提下负责提交，提交失败时可用相同条目重试。
func (s *Service) RecoverAll(ctx context.Context) ([]sessionentry.Record, error) {
	missing, err := s.deps.Calls.ListMissingToolResults(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]sessionentry.Record, 0, len(missing))
	for _, fact := range missing {
		call := sessionentry.ToolCall{
			Key: executionmodel.Key{
				ExecutionID: fact.ExecutionID,
				CallID:      fact.CallID,
			},
			SessionID: fact.SessionID,
			AgentID:   fact.AgentID,
			RequestID: fact.RequestID,
			TurnID:    fact.TurnID,
			Name:      tool.ToolName(fact.Name),
		}
		result = append(result, sessionentry.Record{
			SessionID: fact.SessionID,
			AgentID:   fact.AgentID,
			Data:      call.ResultEntry(tool.NewToolError("tool_interrupted", "The previous executor exited without a reliable tool result.")),
		})
	}
	return result, nil
}
