package toolexecution

import (
	"context"
	"encoding/json"
	"fmt"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
)

// Key 同时定位消息配对、审批事实和进程内取消句柄。
// 同一 Key 的执行参数在准备后冻结，审批与审核结果直接绑定此 Key。
type Key struct {
	ExecutionID identity.AgentExecutionID
	CallID      string
}

func (k Key) String() string { return fmt.Sprintf("%s:%d:%s", k.ExecutionID, len(k.CallID), k.CallID) }

// Result 仅传递本次运行结果，不保存完成状态。
type Result struct {
	ToolResult tool.ToolResult
	Err        error
}

// Execution 是一次工具调用的进程内取消句柄，由调用方保证只运行一次。
type Execution struct {
	ctx    context.Context
	cancel context.CancelFunc
	tool   tool.Tool
	args   json.RawMessage
}

func NewExecution(ctx context.Context, t tool.Tool, args json.RawMessage) *Execution {
	toolCtx, cancel := context.WithCancel(ctx)
	return &Execution{
		ctx:    toolCtx,
		cancel: cancel,
		tool:   t,
		args:   args,
	}
}

func (e *Execution) Run() Result {
	defer e.cancel()
	if err := e.ctx.Err(); err != nil {
		return Result{Err: err}
	}
	result, err := e.tool.Call(e.ctx, e.args)
	return Result{ToolResult: result, Err: err}
}

// Stop 发出取消请求，不表示副作用回滚或回执已保存。
func (e *Execution) Stop() { e.cancel() }
