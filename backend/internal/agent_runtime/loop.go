package agentruntime

import (
	"context"

	"praxis/internal/contracts"
	taskmodel "praxis/internal/core/task"
)

// LoopFunc 是组合根注入的 loop.Run 执行函数，接收快照和消息记录器并返回结果及失败分类。
// 函数只负责 provider/tool loop；取消与执行失败通过 error 返回，runtime 负责最终结算。
type LoopFunc func(context.Context, taskmodel.Task, MessageRecorder) (taskmodel.TaskOutcome, contracts.TaskFailureCode, error)
