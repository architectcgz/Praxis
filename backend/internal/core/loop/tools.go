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

// runToolCalls 顺序执行工具，先保存实际结果，再运行后置 Hook 并更新回执正文。
// 已执行调用即使被取消也先提交回执，避免收尾逻辑将实际结果替换为中断条目。
func (r *Runner) runToolCalls(ctx context.Context, executionInput Input, deps ExecutionDependencies, calls []sessionentry.ToolCall, turnID identity.TurnID) ([]sessionentry.ContextEntry, error) {
	results := make([]sessionentry.ContextEntry, 0, len(calls))
	for _, call := range calls {
		outcome, err := deps.ToolExecutor.Execute(ctx, call)
		if err != nil {
			if _, ok := errors.AsType[*hook.Error](err); ok {
				return results, NewFailure(FailureInternal, err)
			}
			return results, deps.History.EntryFailure(err)
		}
		commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		receipt, inserted, err := deps.History.AppendEntry(commitCtx, call.ResultEntry(outcome.ToolResult))
		cancel()
		if err != nil {
			return results, err
		}
		if inserted && outcome.OperationID != "" {
			identity := executionIdentity(executionInput, call.TurnID)
			identity.OperationID = outcome.OperationID
			receipt, err = r.afterToolExecution(ctx, deps, receipt, hook.ToolExecutionResult{
				ToolExecution: hook.ToolExecution{
					Identity:  identity,
					Call:      call,
					Arguments: outcome.Arguments,
				},
				Result: outcome.ToolResult,
				Err:    outcome.Err,
			})
		}
		block := receipt.Content[0]
		r.emitToolResult(executionInput.SessionID, executionInput.AgentID, executionInput.ID, turnID, appcontext.NewToolResultBlock(block.CallID, block.Name, block.Text, block.IsError))
		results = append(results, receipt)
		if err != nil {
			return results, err
		}
		if err := ctx.Err(); err != nil {
			return results, err
		}
	}
	return results, nil
}

// afterToolExecution 失败时保留实际回执并继续循环；历史更新失败属于存储失败，不能静默忽略。
// Hook 只能修改展示正文，不能改写执行错误和副作用事实，超限结果也不能覆盖已保存内容。
func (r *Runner) afterToolExecution(ctx context.Context, deps ExecutionDependencies, receipt sessionentry.ContextEntry, event hook.ToolExecutionResult) (sessionentry.ContextEntry, error) {
	modified, err := r.HookDispatcher.AfterToolExecution(context.WithoutCancel(ctx), event)
	if err == nil && (modified.ErrorClass != event.Result.ErrorClass || modified.SideEffect != event.Result.SideEffect) {
		err = errors.New("tool result hook cannot change execution error or side-effect facts")
	}
	if err == nil && len(modified.Payload) > tool.MaxInlineResultBytes {
		err = errors.New("tool result hook exceeded the inline size limit")
	}
	if err != nil {
		r.Logf("after tool execution hook failed execution=%s call=%s: %v", event.ExecutionID, event.Call.CallID, err)
		return receipt, nil
	}
	if modified.Payload == "" {
		modified.Payload = "(empty tool result)"
	}
	if modified.Payload == receipt.Content[0].Text {
		return receipt, nil
	}
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	stored, err := deps.History.UpdateToolResult(commitCtx, receipt.ID, modified.Payload)
	if err != nil {
		return receipt, err
	}
	return stored, nil
}

// interruptToolCalls 只收尾本轮尚未返回回执的调用，不扫描整份历史，也不再执行工具。
// 使用独立提交上下文处理取消后的消息收尾；中断回执不推断工具是否已产生副作用。
func (r *Runner) interruptToolCalls(ctx context.Context, input Input, deps ExecutionDependencies, calls []sessionentry.ToolCall) ([]sessionentry.ContextEntry, error) {
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	results := make([]sessionentry.ContextEntry, 0, len(calls))
	for _, call := range calls {
		entry := call.ResultEntry(tool.NewToolError("tool_interrupted", "The execution exited before a reliable tool result was available."))
		stored, inserted, err := deps.History.AppendEntry(commitCtx, entry)
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
