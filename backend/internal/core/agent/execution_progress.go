package agent

// ExecutionProgress 保存模型请求消耗；消息相关统计由仓储派生，重启不能重置预算。
type ExecutionProgress struct {
	NextSequence     uint64
	Iterations       uint64
	ModelRequests    uint64
	ModelOutputBytes int64
	OutputBytes      int64  `json:"-"`
	ToolCalls        int    `json:"-"`
	FinalStopReason  string `json:"-"`
	FinalAssistantID string `json:"-"`
}
