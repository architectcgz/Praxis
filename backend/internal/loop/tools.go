package loop

import (
	"context"
	"errors"
	appcontext "praxis/internal/core/context"
	"praxis/internal/core/hook"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
	"regexp"
)

var toolNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_]{0,63}$`)

func validateToolCalls(calls []ToolCall) error {
	seen := map[string]bool{}
	for _, call := range calls {
		if seen[call.ID] || !toolNamePattern.MatchString(string(call.Name)) {
			return fail(FailureContract, ErrorContract, "provider emitted an invalid tool call")
		}
		seen[call.ID] = true
	}
	return nil
}

// runToolCalls 顺序执行工具，只消费 Service 完成整次调用后提交的回执。
func (r *Runner) runToolCalls(ctx context.Context, executionInput Input, deps ExecutionDependencies, calls []sessionentry.ToolCall, turnID identity.TurnID) ([]sessionentry.ContextEntry, error) {
	results := make([]sessionentry.ContextEntry, 0, len(calls))
	for _, call := range calls {
		receipt, err := deps.ToolExecutor.Execute(ctx, call)
		if err != nil && receipt.ID == "" {
			var hookErr *hook.Error
			if errors.As(err, &hookErr) {
				return results, failCause(FailureInternal, err)
			}
			return results, failCause(FailureStorage, err)
		}
		if len(receipt.Content) != 1 || receipt.Content[0].Kind != sessionentry.BlockToolResult {
			return results, failCause(FailureContract, errors.New("tool service returned an invalid committed receipt"))
		}
		block := receipt.Content[0]
		r.emitToolResult(executionInput.SessionID, executionInput.AgentID, executionInput.ID, turnID, appcontext.NewToolResultBlock(block.CallID, block.Name, block.Text, block.IsError))
		results = append(results, receipt)
		if err != nil {
			return results, failCause(FailureInternal, err)
		}
		if err := ctx.Err(); err != nil {
			return results, err
		}
	}
	return results, nil
}
