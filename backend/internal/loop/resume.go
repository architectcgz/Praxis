package loop

import (
	"context"
	"errors"

	agentruntime "praxis/internal/agent_runtime"
	"praxis/internal/contracts"
	appcontext "praxis/internal/core/context"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
)

var errAwaitingApproval = errors.New("task is awaiting tool approval")

// resumeToolCalls 先结算已经提交的工具轮，再发起新的模型请求。
func (r *Runner) resumeToolCalls(ctx context.Context, task taskmodel.Task, messages []sessionmodel.MessageData, recorder agentruntime.MessageRecorder, modelContext *appcontext.ModelContext, budget *taskBudget) error {
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
		metadata := contracts.ToolInvocationContext{
			TaskID: task.ID, TurnID: contracts.TurnID(message.TurnID), SessionID: task.SessionID,
			AgentID: task.AgentID, WorkspacePath: task.Input.WorkspacePath,
		}
		for _, block := range message.Blocks {
			if block.Kind != sessionmodel.BlockToolCall || receipts[block.CallID] {
				continue
			}
			call := contracts.ToolCall{ID: block.CallID, Name: contracts.ToolName(block.Name), Arguments: block.Input}
			results, err := r.runToolCalls(ctx, recorder, []ToolCall{call}, metadata, task.RequestID, budget)
			if err != nil {
				return err
			}
			result := results[0]
			*modelContext = modelContext.AppendToolResult(appcontext.NewToolResultBlock(call.ID, string(call.Name), result.Payload, result.ErrorClass != ""))
		}
	}
	return nil
}
