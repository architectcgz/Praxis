package contracts

import "fmt"

// ToolExecutionMetadata 保存工具执行的归属标识和工作区路径。
type ToolExecutionMetadata struct {
	TaskID        TaskID
	TurnID        TurnID
	SessionID     SessionID
	AgentID       AgentID
	WorkspacePath string
}

// NewTurnID 根据持久化的任务请求序号生成关联标识，不创建独立执行状态。
func NewTurnID(taskID TaskID, sequence uint64) TurnID {
	return TurnID(fmt.Sprintf("turn:%s:%d", taskID, sequence))
}
