package loop

import (
	"context"
	"errors"
	taskmodel "praxis/internal/agent"
	appcontext "praxis/internal/context"
	"praxis/internal/identity"
	messagemodel "praxis/internal/message"
	"praxis/internal/tool"
	"praxis/internal/toolcall"
)

var errAwaitingApproval = errors.New("task is awaiting tool approval")

// resumeToolCalls 先结算已经提交的工具轮，再发起新的模型请求。
func (r *Runner) resumeToolCalls(ctx context.Context, task taskmodel.Execution, messages []messagemodel.Message, recorder messagemodel.Writer, modelContext *appcontext.ModelContext) error {
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
			ExecutionID:   task.ID,
			TurnID:        identity.TurnID(message.TurnID),
			SessionID:     task.SessionID,
			AgentID:       task.AgentID,
			WorkspacePath: task.Config.WorkspacePath,
		}
		var calls []ToolCall
		for _, block := range message.Blocks {
			if block.Kind == messagemodel.BlockToolCall && !receipts[block.CallID] {
				calls = append(calls, tool.ToolCall{ID: block.CallID, Name: tool.ToolName(block.Name), Arguments: block.Input})
			}
		}
		results, err := r.runToolCalls(ctx, recorder, calls, metadata, task.RequestID)
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
