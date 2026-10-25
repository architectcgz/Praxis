package toolexecution

import (
	"context"
	"praxis/internal/core/tool"
)

// Key 同时定位消息配对、审批事实和进程内取消句柄。
// 同一 Key 的执行参数在准备后冻结，审批与审核结果直接绑定此 Key。
type Key = tool.CallKey

// Result 仅传递本次运行结果，不保存完成状态。
type Result struct {
	ToolResult tool.ToolResult
	Err        error
}

// Execution 是已授权工具调用的进程内取消句柄，由调用方保证只运行一次。
// call 已绑定类型化参数，不在执行阶段重新解析或准备输入。
type Execution struct {
	ctx    context.Context
	cancel context.CancelFunc
	call   func(context.Context) (tool.ToolResult, error)
}

func NewExecution(ctx context.Context, call func(context.Context) (tool.ToolResult, error)) *Execution {
	toolCtx, cancel := context.WithCancel(ctx)
	return &Execution{
		ctx:    toolCtx,
		cancel: cancel,
		call:   call,
	}
}

func (e *Execution) Run() Result {
	defer e.cancel()
	if err := e.ctx.Err(); err != nil {
		return Result{Err: err}
	}
	result, err := e.call(e.ctx)
	return Result{ToolResult: result, Err: err}
}

// Stop 发出取消请求，不表示副作用回滚或回执已保存。
func (e *Execution) Stop() { e.cancel() }
