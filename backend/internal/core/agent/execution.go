package agent

import (
	"context"
	"errors"
	"sync"

	"praxis/internal/contracts"
)

var (
	ErrExecutionPaused  = errors.New("agent execution paused")
	ErrExecutionStopped = errors.New("agent execution stopped")
)

// Executions 保存 Agent 的进程内执行状态，零值可用，不参与 Agent 的持久化。
// runtime 和 Agent 应用服务必须共享同一实例，取消句柄不能由调用方直接取得。
type Executions struct {
	mu     sync.Mutex
	active map[contracts.AgentID]*execution
}

type execution struct {
	taskID contracts.TaskID
	ctx    context.Context
	cancel context.CancelCauseFunc
	ending bool
}

// Start 为已准入的 Task 创建执行 Context；重复启动返回原 Context。
// 调用方应在同一准入事务中先建立绑定，再持久化 Agent 的 executing 状态。
func (e *Executions) Start(ctx context.Context, agentID contracts.AgentID, taskID contracts.TaskID) (context.Context, bool, error) {
	if ctx == nil || agentID == "" || taskID == "" {
		return nil, false, contracts.InvalidValue("agent.execution", "context, agent and task are required")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if active := e.active[agentID]; active != nil {
		if active.taskID == taskID {
			return active.ctx, false, nil
		}
		if !active.ending {
			return nil, false, contracts.ErrAgentExecuting
		}
	}
	if e.active == nil {
		e.active = make(map[contracts.AgentID]*execution)
	}
	executionCtx, cancel := context.WithCancelCause(context.WithoutCancel(ctx))
	e.active[agentID] = &execution{
		taskID: taskID,
		ctx:    executionCtx,
		cancel: cancel,
	}
	return executionCtx, true, nil
}

// Context 返回准入时建立的执行 Context；缺少绑定或执行已返回时拒绝接管。
// runtime 只能读取 Context，不能获取或创建取消句柄。
func (e *Executions) Context(agentID contracts.AgentID, taskID contracts.TaskID) (context.Context, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	active := e.active[agentID]
	if active == nil || active.taskID != taskID || active.ending {
		return nil, contracts.ErrNotFound
	}
	return active.ctx, nil
}

// Pause 暂停指定 Task 的活动执行；返回 true 仅表示信号送达，不表示已经持久化终态。
func (e *Executions) Pause(agentID contracts.AgentID, taskID contracts.TaskID) bool {
	return e.cancel(agentID, taskID, ErrExecutionPaused)
}

// Stop 停止指定 Task 的活动执行；旧 Task ID 和已经返回的执行不影响后续 Task。
func (e *Executions) Stop(agentID contracts.AgentID, taskID contracts.TaskID) bool {
	return e.cancel(agentID, taskID, ErrExecutionStopped)
}

func (e *Executions) cancel(agentID contracts.AgentID, taskID contracts.TaskID, cause error) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	active := e.active[agentID]
	if active == nil || active.taskID != taskID || active.ending {
		return false
	}
	// Context 保留第一次取消原因；多条控制命令的最终优先级由持久化结算事务确定。
	active.cancel(cause)
	return true
}

// BeginEnding 标记 loop 已返回，之后控制请求由结算事务仲裁，不再向执行发送信号。
func (e *Executions) BeginEnding(agentID contracts.AgentID, taskID contracts.TaskID) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if active := e.active[agentID]; active != nil && active.taskID == taskID {
		active.ending = true
	}
}

// End 释放指定执行的 Context 和句柄；旧执行的延迟清理不会删除后续执行。
func (e *Executions) End(agentID contracts.AgentID, taskID contracts.TaskID) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if active := e.active[agentID]; active != nil && active.taskID == taskID {
		active.cancel(nil)
		delete(e.active, agentID)
	}
}
