package execution

import (
	"context"
	"errors"
	agentexecution "praxis/internal/core/agent/execution"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	providerapi "praxis/internal/core/provider"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
	"time"
)

func prepareToolCalls(execution loop.ExecutionParams, turnID identity.TurnID, values []providerapi.ToolCall) []sessionentry.ToolCall {
	calls := make([]sessionentry.ToolCall, 0, len(values))
	for _, call := range values {
		calls = append(calls, sessionentry.ToolCall{
			Key: sessionentry.Key{
				ExecutionID: execution.ID,
				CallID:      call.ID,
			},
			SessionID: execution.SessionID,
			AgentID:   execution.AgentID,
			RequestID: execution.RequestID.String(),
			TurnID:    turnID,
			Name:      call.Name,
			Arguments: call.Arguments,
		})
	}
	return calls
}

// runToolCalls 返回已取得真实回执的调用数，与回执是否成功写入日志无关。
func (s *Service) runToolCalls(ctx context.Context, run *agentexecution.Run, executor loop.ToolExecutor, calls []sessionentry.ToolCall) (int, error) {
	for index, call := range calls {
		outcome, err := executor.Execute(ctx, call)
		if err != nil {
			if _, ok := errors.AsType[*hook.Error](err); ok {
				return index, loop.NewFailure(loop.FailureInternal, err)
			}
			return index, storageFailure(err)
		}
		commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		receipt, inserted, err := s.appendEntry(commitCtx, run, call.ResultEntry(outcome.ToolResult))
		cancel()
		if err != nil {
			return index + 1, err
		}
		if inserted && outcome.OperationID != "" {
			identity := executionIdentity(run, call.TurnID)
			identity.OperationID = outcome.OperationID
			if hookErr := s.hookDispatcher.AfterToolExecution(context.WithoutCancel(ctx), hook.ToolExecutionResult{
				ToolExecution: hook.ToolExecution{
					Identity:  identity,
					Call:      call,
					Arguments: outcome.Arguments,
				},
				Result: outcome.ToolResult,
				Err:    outcome.Err,
			}); hookErr != nil {
				s.logger.Errorf("after tool execution hook failed: %v", hookErr)
			}
		}
		if inserted {
			block := receipt.Content[0]
			s.emitter.Emit(coreevent.Event{
				Source: coreevent.SourceForRun(run, call.TurnID),
				Payload: coreevent.ToolResult{
					CallID:  block.CallID,
					Result:  block.Text,
					IsError: block.IsError,
				},
			})
		}
		if err := ctx.Err(); err != nil {
			return index + 1, err
		}
	}
	return len(calls), nil
}

// interruptToolCalls 只补齐尚未取得真实回执的调用，不执行工具。
func (s *Service) interruptToolCalls(ctx context.Context, run *agentexecution.Run, calls []sessionentry.ToolCall) error {
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	for _, call := range calls {
		entry := call.ResultEntry(tool.NewToolError("tool_interrupted", "The execution exited before a reliable tool result was available."))
		if _, _, err := s.appendEntry(commitCtx, run, entry); err != nil {
			return err
		}
	}
	return nil
}
