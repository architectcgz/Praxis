package loop

import (
	"context"
	"encoding/json"

	"praxis/internal/agent_runtime"
	"praxis/internal/contracts"
	appcontext "praxis/internal/core/context"
	toolcontracts "praxis/internal/tools/contracts"
)

// validateToolCalls 检查模型输出契约；具体参数规范化和安全授权不在 loop 内重复执行。
func validateToolCalls(calls []ToolCall) error {
	for _, call := range calls {
		if call.ID == "" || !call.Name.Valid() || len(call.Arguments) > 0 && !json.Valid(call.Arguments) {
			return fail(contracts.TaskFailureContract, ErrorContract, "provider emitted an invalid tool call")
		}
	}
	return nil
}

// runToolCalls 按已构造的执行归属顺序派发工具，不读取 Task 或更新模型上下文。
// 返回的结果与 calls 顺序一致；失败时只返回此前已写入回执的结果，不继续执行后续工具。
// 授权和持久化准入由 ToolCalls 负责。
// 下一次模型请求读取结果前必须写入工具结果回执，避免崩溃后留下无法对账的副作用。
func (r *Runner) runToolCalls(
	ctx context.Context,
	messageRecorder agentruntime.MessageRecorder,
	calls []ToolCall,
	metadata ToolInvocationMetadata,
	requestID contracts.RequestID,
	budget *taskBudget,
) ([]toolcontracts.ToolResult, error) {
	if err := budget.reserveToolCalls(len(calls)); err != nil {
		return nil, err
	}
	results := make([]toolcontracts.ToolResult, 0, len(calls))
	for callIndex, call := range calls {
		result, err := r.ToolCalls.Invoke(ctx, call, metadata)
		if err != nil {
			// 在结算失败前写入受限回执，确保人工检查时能看到已尝试的副作用。
			block := appcontext.NewToolResultBlock(call.ID, string(call.Name), "tool execution failed", true)
			if receiptErr := appendToolResult(ctx, messageRecorder, requestID, metadata.TaskID, metadata.TurnID, callIndex, block); receiptErr != nil {
				return results, failCause(contracts.TaskFailureStorage,
					&RuntimeError{Code: ErrorStorage, Message: "tool result receipt failed", Cause: receiptErr})
			}
			r.emitToolResult(metadata.AgentID, metadata.TaskID, metadata.TurnID, block)
			return results, failCause(contracts.TaskFailureTool,
				&RuntimeError{Code: ErrorTool, Message: "tool execution failed", Cause: err})
		}
		budget.toolCalls++
		if result.Payload == "" {
			result.Payload = "(empty tool result)"
		}
		if err := budget.addOutput(len(result.Payload)); err != nil {
			return results, err
		}
		block := appcontext.NewToolResultBlock(call.ID, string(call.Name), result.Payload, result.ErrorClass != "")
		if err := appendToolResult(ctx, messageRecorder, requestID, metadata.TaskID, metadata.TurnID, callIndex, block); err != nil {
			return results, err
		}
		r.emitToolResult(metadata.AgentID, metadata.TaskID, metadata.TurnID, block)
		results = append(results, result)
	}
	return results, nil
}
