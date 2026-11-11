package loop

import (
	"context"
	"errors"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	providerapi "praxis/internal/core/provider"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
	"time"
)

func prepareToolCalls(params RunParams, turnID identity.TurnID, values []providerapi.ToolCall) []sessionentry.ToolCall {
	calls := make([]sessionentry.ToolCall, 0, len(values))
	for _, call := range values {
		calls = append(calls, sessionentry.ToolCall{
			Key: sessionentry.Key{
				RunID:  params.ID,
				CallID: call.ID,
			},
			SessionID: params.SessionID,
			AgentID:   params.AgentID,
			TurnID:    turnID,
			Name:      call.Name,
			Arguments: call.Arguments,
		})
	}
	return calls
}

// runToolCalls 返回已取得真实回执的调用数，与回执是否成功写入日志无关。
func (r *Runner) runToolCalls(ctx context.Context, params RunParams, appendEntry AppendEntry, executor ToolExecutor, calls []sessionentry.ToolCall) (int, error) {
	for index, call := range calls {
		outcome, err := executor.Execute(ctx, call)
		if err != nil {
			if _, ok := errors.AsType[*hook.Error](err); ok {
				return index, NewFailure(FailureInternal, err)
			}
			return index, NewFailure(FailureStorage, err)
		}
		entry := call.ResultEntry(outcome.ToolResult)
		block := entry.Content[0].(sessionentry.ToolResultBlock)
		block.Timing = outcome.Timing
		entry.Content[0] = block
		r.emit(params, call.TurnID, coreevent.ToolResult{
			CallID:  block.CallID,
			Result:  block.Text,
			IsError: block.IsError,
			Timing:  outcome.Timing,
		})
		commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		_, inserted, err := appendEntry(commitCtx, entry)
		cancel()
		if err != nil {
			return index + 1, err
		}
		if inserted && outcome.OperationID != "" {
			identity := runIdentity(params, call.TurnID)
			identity.OperationID = outcome.OperationID
			if hookErr := r.HookDispatcher.AfterToolExecution(context.WithoutCancel(ctx), hook.ToolExecutionResult{
				ToolExecution: hook.ToolExecution{
					Identity:  identity,
					Call:      call,
					Arguments: string(outcome.Arguments),
				},
				Result: outcome.ToolResult,
				Err:    outcome.Err,
			}); hookErr != nil {
				r.Logf("after tool execution hook failed: %v", hookErr)
			}
		}
		if err := ctx.Err(); err != nil {
			return index + 1, err
		}
	}
	return len(calls), nil
}

// interruptToolCalls 只补齐尚未取得真实回执的调用，不执行工具。
func interruptToolCalls(ctx context.Context, appendEntry AppendEntry, calls []sessionentry.ToolCall) error {
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	for _, call := range calls {
		entry := call.ResultEntry(tool.NewToolError("tool_interrupted", "The execution exited before a reliable tool result was available."))
		if _, _, err := appendEntry(commitCtx, entry); err != nil {
			return err
		}
	}
	return nil
}
