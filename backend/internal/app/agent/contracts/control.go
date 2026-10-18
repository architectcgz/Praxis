package contracts

// ControlInfo 返回控制命令的持久化状态。
type ControlInfo struct {
	ID           string
	AgentID      string
	TargetTaskID string
	Kind         string
	Status       string
}

// ControlResult 返回控制命令和幂等状态；取消通知失败不撤销已提交的命令。
type ControlResult struct {
	Command           ControlInfo
	ExistingCommand   bool
	CancellationError string
}
