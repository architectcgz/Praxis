package loop

import (
	"context"
	"errors"
	appcontext "praxis/internal/core/context"
	"praxis/internal/core/message"
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
func (r *Runner) runToolCalls(ctx context.Context, executionInput Input, deps ExecutionDependencies, calls []message.ToolCall, turnID identity.TurnID) ([]message.Message, error) {
	results := make([]message.Message, 0, len(calls))
	for _, call := range calls {
		receipt, err := deps.ToolExecutor.Execute(ctx, call)
		if err != nil {
			return results, failCause(FailureStorage, err)
		}
		if len(receipt.Blocks) != 1 || receipt.Blocks[0].Kind != message.BlockToolResult {
			return results, failCause(FailureContract, errors.New("tool service returned an invalid committed receipt"))
		}
		block := receipt.Blocks[0]
		r.emitToolResult(executionInput.SessionID, executionInput.AgentID, executionInput.ID, turnID, appcontext.NewToolResultBlock(block.CallID, block.Name, block.Text, block.IsError))
		results = append(results, receipt)
		if err := ctx.Err(); err != nil {
			return results, err
		}
	}
	return results, nil
}
