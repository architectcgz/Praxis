package toolexecution

import (
	"context"
	"errors"
	"praxis/internal/core/message"
	"praxis/internal/core/tool"
	executionmodel "praxis/internal/core/toolexecution"
	"praxis/internal/fault"
	"praxis/internal/identity"
)

// Recover 仅由已确认旧 Loop 和工具终止的业务恢复编排调用。
// 不读取审批或 started 判断重放，所有孤儿调用都补同一错误回执。
func (s *Service) Recover(ctx context.Context, executionID identity.AgentExecutionID, callID string) (message.Message, error) {
	key := executionmodel.Key{ExecutionID: executionID, CallID: callID}
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	if s.active[key] != nil {
		return message.Message{}, fault.ErrRequestConflict
	}
	var result message.Message
	err := func(txCtx context.Context) error {
		stored, err := s.deps.Calls.GetResult(txCtx, key)
		if err == nil {
			result = stored
			return nil
		}
		if !errors.Is(err, fault.ErrNotFound) {
			return err
		}
		call, err := s.call(txCtx, executionID, callID)
		if err != nil {
			return err
		}
		result, err = s.appendResult(txCtx, call, tool.ToolResult{Payload: "No result provided", ErrorClass: "missing_tool_result"})
		return err
	}(ctx)
	return result, err
}

// RecoverAll 只允许在准入开放前调用，此时上一进程的执行者已不存在。
func (s *Service) RecoverAll(ctx context.Context) error {
	return s.recoverMatching(ctx, "")
}

// RecoverExecution 的调用方必须已经持有唯一 Loop 运行权，且旧工具已返回。
func (s *Service) RecoverExecution(ctx context.Context, id identity.AgentExecutionID) error {
	return s.recoverMatching(ctx, id)
}

func (s *Service) recoverMatching(ctx context.Context, executionID identity.AgentExecutionID) error {
	missing, err := s.deps.Calls.ListMissingToolResults(ctx)
	if err != nil {
		return err
	}
	for _, fact := range missing {
		if executionID != "" && fact.ExecutionID != executionID {
			continue
		}
		if _, err := s.Recover(ctx, fact.ExecutionID, fact.CallID); err != nil {
			return err
		}
	}
	return nil
}
