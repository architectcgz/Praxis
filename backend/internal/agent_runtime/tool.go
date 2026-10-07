package agentruntime

import (
	"praxis/internal/contracts"
	sessionmodel "praxis/internal/core/session"
	toolcontracts "praxis/internal/tools/contracts"

	"context"
)

// ToolInvocationMetadata 描述 runtime 发起一次 Tool 调用时的执行归属和运行边界。
type ToolInvocationMetadata struct {
	TaskID              contracts.TaskID
	TurnID              contracts.TurnID
	SessionID           contracts.SessionID
	AgentID             contracts.AgentID
	WorkspacePath       string
	SecurityFingerprint string
}

// ToolCallHandler 接收 runtime 产生的工具调用，并负责完成持久化调用流程。
type ToolCallHandler interface {
	// RecordAssistant 原子保存 assistant 消息及其全部 requested 调用；失败时不得执行工具。
	RecordAssistant(context.Context, MessageRecorder, sessionmodel.MessageData, ToolInvocationMetadata) error
	Invoke(context.Context, toolcontracts.ToolCall, ToolInvocationMetadata) (toolcontracts.ToolResult, error)
}
