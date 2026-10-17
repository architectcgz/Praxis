package loop

import (
	"context"
	"errors"

	"praxis/internal/contracts"
	appcontext "praxis/internal/core/context"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/repository"
)

var errAwaitingApproval = errors.New("task is awaiting tool approval")

// resumeToolCalls 先结算已经提交的工具轮，再发起新的模型请求。
func (r *Runner) resumeToolCalls(ctx context.Context, task taskmodel.Task, messages []sessionmodel.MessageData, recorder repository.MessageWriter, modelContext *appcontext.ModelContext) error {
	receipts := make(map[string]bool)
	for _, message := range messages {
		for _, block := range message.Blocks {
			if block.Kind == sessionmodel.BlockToolResult {
				receipts[block.CallID] = true
			}
		}
	}
	for _, message := range messages {
		if message.Role != sessionmodel.RoleAssistant {
			continue
		}
		metadata := contracts.ToolExecutionMetadata{
			TaskID:        task.ID,
			TurnID:        contracts.TurnID(message.TurnID),
			SessionID:     task.SessionID,
			AgentID:       task.AgentID,
			WorkspacePath: task.Config.WorkspacePath,
		}
		var calls []ToolCall
		for _, block := range message.Blocks {
			if block.Kind == sessionmodel.BlockToolCall && !receipts[block.CallID] {
				calls = append(calls, contracts.ToolCall{ID: block.CallID, Name: contracts.ToolName(block.Name), Arguments: block.Input})
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
