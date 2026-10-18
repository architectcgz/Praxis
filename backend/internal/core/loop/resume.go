package loop

import (
	"context"
	"errors"
	appcontext "praxis/internal/core/context"
	messagemodel "praxis/internal/core/message"
	"praxis/internal/core/tool"
	"praxis/internal/core/toolcall"
	"praxis/internal/identity"
)

var errAwaitingApproval = errors.New("input is awaiting tool approval")

// resumeToolCalls 先结算已经提交的工具轮，再发起新的模型请求。
func (r *Runner) resumeToolCalls(ctx context.Context, input Input, messages []messagemodel.Message, recorder messagemodel.Writer, modelContext *appcontext.ModelContext) error {
	receipts := make(map[string]bool)
	for _, message := range messages {
		for _, block := range message.Blocks {
			if block.Kind == messagemodel.BlockToolResult {
				receipts[block.CallID] = true
			}
		}
	}
	for _, message := range messages {
		if message.Role != messagemodel.RoleAssistant {
			continue
		}
		metadata := toolcall.ToolExecutionMetadata{
			ExecutionID:   input.ID,
			TurnID:        identity.TurnID(message.TurnID),
			SessionID:     input.SessionID,
			AgentID:       input.AgentID,
			WorkspacePath: input.Config.WorkspacePath,
		}
		var calls []ToolCall
		for _, block := range message.Blocks {
			if block.Kind == messagemodel.BlockToolCall && !receipts[block.CallID] {
				calls = append(calls, tool.ToolCall{ID: block.CallID, Name: tool.ToolName(block.Name), Arguments: block.Input})
			}
		}
		results, err := r.runToolCalls(ctx, recorder, calls, metadata, input.RequestID)
		if err != nil {
			return err
		}
		for index, result := range results {
			call := calls[index]
			*modelContext = modelContext.RestoreToolResult(appcontext.NewToolResultBlock(call.ID, string(call.Name), result.Payload, result.ErrorClass != ""))
		}
	}
	return nil
}
