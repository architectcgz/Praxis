package contracts

import "fmt"

// ToolInvocationContext 固定工具调用的执行归属和安全快照身份。
type ToolInvocationContext struct {
	TaskID              TaskID
	TurnID              TurnID
	SessionID           SessionID
	AgentID             AgentID
	WorkspacePath       string
	SecurityFingerprint string
}

// NewTurnID 根据持久化的任务请求序号生成关联标识，不创建独立执行状态。
func NewTurnID(taskID TaskID, sequence uint64) TurnID {
	return TurnID(fmt.Sprintf("turn:%s:%d", taskID, sequence))
}
