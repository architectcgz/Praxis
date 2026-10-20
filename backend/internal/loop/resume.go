package loop

import (
	"context"
	appcontext "praxis/internal/core/context"
	message "praxis/internal/core/message"
)

// resumeToolCalls 只恢复消息配对；旧 Loop 已由 Coordinator 确认停止，不重放工具。
func (r *Runner) resumeToolCalls(ctx context.Context, input Input, messages []message.Message, modelContext *appcontext.ModelContext) error {
	type key struct{ executionID, callID string }
	receipts := make(map[key]bool)
	for _, item := range messages {
		for _, block := range item.Blocks {
			if block.Kind == message.BlockToolResult {
				receipts[key{item.ExecutionID, block.CallID}] = true
			}
		}
	}
	for _, item := range messages {
		if item.Role != message.RoleAssistant || item.ExecutionID != input.ID.String() {
			continue
		}
		for _, block := range item.Blocks {
			if block.Kind != message.BlockToolCall || receipts[key{item.ExecutionID, block.CallID}] {
				continue
			}
			stored, err := input.ToolExecutor.Recover(ctx, input.ID, block.CallID)
			if err != nil {
				return failCause(FailureStorage, err)
			}
			result := stored.Blocks[0]
			*modelContext = modelContext.RestoreToolResult(appcontext.NewToolResultBlock(result.CallID, result.Name, result.Text, result.IsError))
			receipts[key{item.ExecutionID, block.CallID}] = true
		}
	}
	return nil
}
