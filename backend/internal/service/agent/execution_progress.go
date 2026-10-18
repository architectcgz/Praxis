package agent

// ExecutionProgress 在模型请求及工具准入时提交，重启不能重置已消耗预算。
type ExecutionProgress struct {
	NextSequence     uint64
	Iterations       uint64
	ModelRequests    uint64
	OutputBytes      int64
	ToolCalls        int
	FinalStopReason  string
	FinalAssistantID string
}
