package hook

import (
	"bytes"
	"context"
	"encoding/json"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/tool"
)

// ToolExecution 的调用归属只读；Before 只返回参数，不能替换工具或审批结果。
type ToolExecution struct {
	Identity
	Call      sessionentry.ToolCall
	Arguments json.RawMessage
}

func (e ToolExecution) clone() ToolExecution {
	e.Call.Arguments = bytes.Clone(e.Call.Arguments)
	e.Arguments = bytes.Clone(e.Arguments)
	return e
}

type ToolExecutionResult struct {
	ToolExecution
	Result tool.ToolResult
	Err    error
}

func (e ToolExecutionResult) clone() ToolExecutionResult {
	e.ToolExecution = e.ToolExecution.clone()
	return e
}

type BeforeToolExecutionHook interface {
	BeforeToolExecution(context.Context, ToolExecution) (json.RawMessage, error)
}

// AfterToolExecutionHook 观察已提交的执行结果，不能覆盖回执；通知失败不改变执行事实。
type AfterToolExecutionHook interface {
	AfterToolExecution(context.Context, ToolExecutionResult) error
}

type ToolExecutionWaitingHook interface {
	ToolExecutionWaiting(context.Context, ToolExecution) error
}
