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

// runToolCalls 顺序执行工具，只消费 Core ToolExecutor 完成整次调用后提交的回执。
func (r *Runner) runToolCalls(ctx context.Context, executionInput Input, deps ExecutionDependencies, calls []sessionentry.ToolCall, turnID identity.TurnID) ([]sessionentry.ContextEntry, error) {
	results := make([]sessionentry.ContextEntry, 0, len(calls))
	for _, call := range calls {
		receipt, err := deps.ToolExecutor.Execute(ctx, call)
		if err != nil && receipt.ID == "" {
			if _, ok := errors.AsType[*hook.Error](err); ok {
				return results, NewFailure(FailureInternal, err)
			}
			return results, deps.Execution.EntryFailure(err)
		}
		if len(receipt.Content) != 1 || receipt.Content[0].Kind != sessionentry.BlockToolResult {
			return results, NewFailure(FailureContract, errors.New("tool executor returned an invalid committed receipt"))
		}
		block := receipt.Content[0]
		r.emitToolResult(executionInput.SessionID, executionInput.AgentID, executionInput.ID, turnID, appcontext.NewToolResultBlock(block.CallID, block.Name, block.Text, block.IsError))
		results = append(results, receipt)
		if err != nil {
			return results, NewFailure(FailureInternal, err)
		}
		if err := ctx.Err(); err != nil {
			return results, err
		}
	}
	return results, nil
}

// interruptToolCalls 只收尾本轮尚未返回回执的调用，不扫描整份历史，也不再执行工具。
// 使用独立提交上下文处理取消后的消息收尾；中断回执不推断工具是否已产生副作用。
func (r *Runner) interruptToolCalls(ctx context.Context, input Input, deps ExecutionDependencies, calls []sessionentry.ToolCall) ([]sessionentry.ContextEntry, error) {
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	results := make([]sessionentry.ContextEntry, 0, len(calls))
	for _, call := range calls {
		entry := call.ResultEntry(tool.NewToolError("tool_interrupted", "The execution exited before a reliable tool result was available."))
		stored, inserted, err := deps.Execution.AppendEntry(commitCtx, entry)
		if err != nil {
			return results, err
		}
		results = append(results, stored)
		if inserted {
			r.emitToolResult(input.SessionID, input.AgentID, input.ID, call.TurnID, stored.Content[0])
		}
	}
	return results, nil
}
