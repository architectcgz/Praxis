package loop

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"praxis/internal/agent_runtime"
	"praxis/internal/contracts"
	appcontext "praxis/internal/core/context"
	toolcontracts "praxis/internal/tools/contracts"
)

// validateToolCalls 检查模型输出契约；具体参数规范化和安全授权不在 loop 内重复执行。
func validateToolCalls(calls []ToolCall) error {
	seen := map[string]bool{}
	for _, call := range calls {
		if call.ID == "" || call.ID != strings.TrimSpace(call.ID) || seen[call.ID] || !call.Name.Valid() || !json.Valid(call.Arguments) {
			return fail(contracts.TaskFailureContract, ErrorContract, "provider emitted an invalid tool call")
		}
		seen[call.ID] = true
	}
	return nil
}

// runToolCalls 顺序派发工具，先提交回执，再根据持久化预算决定是否继续。
// 返回的结果与 calls 顺序一致；失败时只返回此前已写入回执的结果，不继续执行后续工具。
// 授权和持久化准入由 ToolExecutor 负责。
// 下一次模型请求读取结果前必须写入工具结果回执，避免崩溃后留下无法对账的副作用。
func (r *Runner) runToolCalls(
	ctx context.Context,
	messageRecorder agentruntime.MessageRecorder,
	calls []ToolCall,
	metadata contracts.ToolInvocationContext,
	requestID contracts.RequestID,
	budget *taskBudget,
) ([]toolcontracts.ToolResult, error) {
	results := make([]toolcontracts.ToolResult, 0, len(calls))
	for callIndex, call := range calls {
		execution, err := r.ToolExecutor.Execute(ctx, call, metadata)
		if execution.ApprovalPending {
			r.emit(agentruntime.AgentEvent{
				Kind:      agentruntime.AgentEventToolApprovalRequired,
				SessionID: metadata.SessionID,
				AgentID:   metadata.AgentID,
				TaskID:    metadata.TaskID,
				TurnID:    metadata.TurnID,
				CallID:    call.ID,
				Name:      string(call.Name),
				Input:     call.Arguments,
			})
			return results, errAwaitingApproval
		}
		result := execution.Result
		if err != nil {
			// 在结算失败前写入受限回执，确保人工检查时能看到已尝试的副作用。
			payload := result.Payload
			if payload == "" {
				if errors.Is(err, contracts.New(contracts.TaskBusy, "")) {
					return results, failCause(contracts.TaskFailureBusy, err)
				}
				if errors.Is(err, contracts.New(contracts.TaskPolicyBlocked, "")) {
					return results, failCause(contracts.TaskFailurePolicyBlocked, err)
				}
				if errors.Is(err, contracts.New(contracts.TaskResourceLimit, "")) {
					return results, failCause(contracts.TaskFailureResourceLimit, err)
				}
				return results, failCause(contracts.TaskFailureStorage, err)
			}
			block := appcontext.NewToolResultBlock(call.ID, string(call.Name), payload, true)
			if receiptErr := appendToolResult(ctx, messageRecorder, requestID, metadata.TaskID, metadata.TurnID, callIndex, block); receiptErr != nil {
				return results, failCause(contracts.TaskFailureStorage,
					&RuntimeError{Code: ErrorStorage, Message: "tool result receipt failed", Cause: receiptErr})
			}
			r.emitToolResult(metadata.SessionID, metadata.AgentID, metadata.TaskID, metadata.TurnID, block)
			return results, failCause(contracts.TaskFailureTool,
				&RuntimeError{Code: ErrorTool, Message: "tool execution failed", Cause: err})
		}
		if result.Payload == "" {
			result.Payload = "(empty tool result)"
		}
		block := appcontext.NewToolResultBlock(call.ID, string(call.Name), result.Payload, result.ErrorClass != "")
		if err := appendToolResult(ctx, messageRecorder, requestID, metadata.TaskID, metadata.TurnID, callIndex, block); err != nil {
			return results, err
		}
		r.emitToolResult(metadata.SessionID, metadata.AgentID, metadata.TaskID, metadata.TurnID, block)
		results = append(results, result)
		task, err := r.Tasks.Get(ctx, metadata.TaskID)
		if err != nil {
			return results, failCause(contracts.TaskFailureStorage, err)
		}
		budget.outputBytes = task.Progress.OutputBytes
		if budget.limits.MaxOutputBytes > 0 && budget.outputBytes > budget.limits.MaxOutputBytes {
			return results, fail(contracts.TaskFailureResourceLimit, ErrorResourceLimit, "task output limit exceeded")
		}

	}
	return results, nil
}
