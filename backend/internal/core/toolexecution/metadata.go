package toolexecution

import (
	"praxis/internal/identity"
)

// ToolExecutionMetadata 保存工具执行的归属标识和工作区路径。
type ToolExecutionMetadata struct {
	ExecutionID   identity.AgentExecutionID
	TurnID        identity.TurnID
	SessionID     identity.SessionID
	AgentID       identity.AgentID
	WorkspacePath string
}
