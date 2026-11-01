package loop

import (
	"context"
	"errors"
	appcontext "praxis/internal/core/context"
	"praxis/internal/core/hook"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
	"regexp"
	"time"
)

var toolNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_]{0,63}$`)

func validateToolCalls(calls []ToolCall) error {
	seen := map[string]bool{}
	for _, call := range calls {
		if seen[call.ID] || !toolNamePattern.MatchString(string(call.Name)) {
			return fail(FailureContract, ErrContract, "provider emitted an invalid tool call")
		}
		seen[call.ID] = true
	}
	return nil
}

// runToolCalls 顺序执行工具，先保存实际结果，再发送只读的后置 Hook 通知。
// 已执行调用即使被取消也先提交回执，避免收尾逻辑将实际结果替换为中断条目。
func (r *Runner) runToolCalls(ctx context.Context, executionInput Input, deps ExecutionDependencies, calls []sessionentry.ToolCall, turnID identity.TurnID) ([]sessionentry.ContextEntry, error) {
	results := make([]sessionentry.ContextEntry, 0, len(calls))
	for _, call := range calls {
		outcome, err := deps.ToolExecutor.Execute(ctx, call)
		if err != nil {
			if _, ok := errors.AsType[*hook.Error](err); ok {
				return results, NewFailure(FailureInternal, err)
			}
			return results, deps.Entries.EntryFailure(err)
		}
		commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		receipt, inserted, err := deps.Entries.AppendEntry(commitCtx, call.ResultEntry(outcome.ToolResult))
		cancel()
		if err != nil {
			return results, err
		}
		if inserted && outcome.OperationID != "" {
			identity := executionIdentity(executionInput, call.TurnID)
			identity.OperationID = outcome.OperationID
			if hookErr := r.HookDispatcher.AfterToolExecution(context.WithoutCancel(ctx), hook.ToolExecutionResult{
				ToolExecution: hook.ToolExecution{
					Identity:  identity,
					Call:      call,
					Arguments: outcome.Arguments,
				},
				Result: outcome.ToolResult,
				Err:    outcome.Err,
			}); hookErr != nil {
				r.Logf("after tool execution hook failed execution=%s call=%s: %v", executionInput.ID, call.CallID, hookErr)
			}
		}
		block := receipt.Content[0]
		r.emitToolResult(executionInput, turnID, appcontext.NewToolResultBlock(block.CallID, block.Name, block.Text, block.IsError))
		results = append(results, receipt)
		if err := ctx.Err(); err != nil {
			return results, err
		}
	}
	return results, nil
}

// interruptToolCalls 只收尾本轮尚未返回回执的调用，不扫描完整 Entry 集合，也不再执行工具。
// 使用独立提交上下文处理取消后的消息收尾；中断回执不推断工具是否已产生副作用。
func (r *Runner) interruptToolCalls(ctx context.Context, input Input, deps ExecutionDependencies, calls []sessionentry.ToolCall) ([]sessionentry.ContextEntry, error) {
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	results := make([]sessionentry.ContextEntry, 0, len(calls))
	for _, call := range calls {
		entry := call.ResultEntry(tool.NewToolError("tool_interrupted", "The execution exited before a reliable tool result was available."))
		stored, inserted, err := deps.Entries.AppendEntry(commitCtx, entry)
		if err != nil {
			return results, err
		}
		results = append(results, stored)
		if inserted {
			r.emitToolResult(input, call.TurnID, stored.Content[0])
		}
	}
	return results, nil
}
