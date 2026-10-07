package contracts

// TurnExecutionContext 描述一次模型迭代的持久化归属。
type TurnExecutionContext struct {
	TaskID    TaskID
	SessionID SessionID
	AgentID   AgentID
	Sequence  uint64
}

// ToolInvocationContext 固定工具调用的执行归属和安全快照身份。
type ToolInvocationContext struct {
	TaskID              TaskID
	TurnID              TurnID
	SessionID           SessionID
	AgentID             AgentID
	WorkspacePath       string
	SecurityFingerprint string
}
