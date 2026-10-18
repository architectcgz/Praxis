// Package contracts 定义任务提交和工具审批应用入口的公开结果。
package contracts

// SendInputResult 区分持久化结果与激活失败。
type SendInputResult struct {
	TaskID          string
	ExistingRequest bool
	ActivationError string
}

// EnqueueTaskResult 返回预约任务的身份和状态。
type EnqueueTaskResult struct {
	TaskID       string
	Status       string
	ExistingTask bool
}
