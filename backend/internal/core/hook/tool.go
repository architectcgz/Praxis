package hook

import (
	"context"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/tool"
)

// ToolExecution 的调用归属只读；Before 只返回参数，不能替换工具或审批结果。
type ToolExecution struct {
	Identity
	Call      sessionentry.ToolCall
	Arguments string
}

type ToolExecutionResult struct {
	ToolExecution
	Result tool.ToolResult
	Err    error
}

type BeforeToolExecutionHook interface {
	BeforeToolExecution(context.Context, ToolExecution) (string, error)
}

// AfterToolExecutionHook 观察已提交的执行结果，不能覆盖回执；通知失败不改变执行事实。
type AfterToolExecutionHook interface {
	AfterToolExecution(context.Context, ToolExecutionResult) error
}

type ToolExecutionWaitingHook interface {
	ToolExecutionWaiting(context.Context, ToolExecution) error
}
