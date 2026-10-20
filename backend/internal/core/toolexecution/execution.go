package toolexecution

import (
	"context"
	"encoding/json"
	"praxis/internal/core/tool"
)

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
