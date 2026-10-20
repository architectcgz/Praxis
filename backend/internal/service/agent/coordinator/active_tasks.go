package coordinator

import (
	"context"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/fault"
	"praxis/internal/identity"
	"sync"
)

// ActiveTasks 保存唯一的进程内执行状态，零值可用；准入、控制与协调器共享此实例。
type ActiveTasks struct {
	mu     sync.Mutex
	active map[identity.AgentID]*activeTask
}

type activeTask struct {
	executionID identity.AgentExecutionID
	ctx         context.Context
	cancel      context.CancelCauseFunc
	ending      bool
	running     bool
	wake        chan struct{}
	result      *executionResultState
}

// Reserve 为已准入的 Execution 创建执行 Context；重复启动返回原 Context。
// 调用方应在同一准入事务中先建立绑定，再持久化 Agent 的 executing 状态。
func (e *ActiveTasks) Reserve(ctx context.Context, agentID identity.AgentID, executionID identity.AgentExecutionID) (context.Context, bool, error) {
	if ctx == nil || agentID == "" || executionID == "" {
		return nil, false, fault.InvalidValue("agent.execution", "context, agent and task are required")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if active := e.active[agentID]; active != nil {
		if active.executionID == executionID {
			return active.ctx, false, nil
		}
		return nil, false, agentmodel.ErrAgentExecuting
	}
	if e.active == nil {
		e.active = make(map[identity.AgentID]*activeTask)
	}
	executionCtx, cancel := context.WithCancelCause(context.WithoutCancel(ctx))
	e.active[agentID] = &activeTask{
		executionID: executionID,
		ctx:         executionCtx,
		cancel:      cancel,
		wake:        make(chan struct{}, 1),
	}
	return executionCtx, true, nil
}

// Context 返回准入时建立的执行 Context；缺少绑定时拒绝接管。
// 结算失败时仍保留绑定，直到结算提交后才释放。
func (e *ActiveTasks) Context(agentID identity.AgentID, executionID identity.AgentExecutionID) (context.Context, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	active := e.active[agentID]
	if active == nil || active.executionID != executionID {
		return nil, fault.ErrNotFound
	}
	return active.ctx, nil
}

// Pause 暂停指定 Execution 的活动执行；返回 true 仅表示信号送达，不表示已经持久化终态。
func (e *ActiveTasks) Pause(agentID identity.AgentID, executionID identity.AgentExecutionID) bool {
	return e.cancel(agentID, executionID, ErrExecutionPaused)
}

// Stop 停止指定 Execution 的活动执行；旧 Execution ID 和已经返回的执行不影响后续 Execution。
func (e *ActiveTasks) Stop(agentID identity.AgentID, executionID identity.AgentExecutionID) bool {
	return e.cancel(agentID, executionID, ErrExecutionStopped)
}

func (e *ActiveTasks) cancel(agentID identity.AgentID, executionID identity.AgentExecutionID, cause error) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	active := e.active[agentID]
	if active == nil || active.executionID != executionID || active.ending {
		return false
	}
	// Context 保留第一次取消原因；多条控制命令的最终优先级由持久化结算事务确定。
	active.cancel(cause)
	return true
}

// BeginEnding 标记 loop 已返回，之后控制请求由结算事务仲裁，不再向执行发送信号。
func (e *ActiveTasks) BeginEnding(agentID identity.AgentID, executionID identity.AgentExecutionID) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if active := e.active[agentID]; active != nil && active.executionID == executionID {
		active.ending = true
	}
}

// Release 释放指定执行的 Context 和句柄；旧执行的延迟清理不会删除后续执行。
func (e *ActiveTasks) Release(agentID identity.AgentID, executionID identity.AgentExecutionID) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if active := e.active[agentID]; active != nil && active.executionID == executionID {
		active.cancel(nil)
		delete(e.active, agentID)
	}
}

// TryRun 原子领取同一 Execution 的运行权；重复激活不会创建第二个 Loop。
func (e *ActiveTasks) TryRun(agentID identity.AgentID, executionID identity.AgentExecutionID) (*activeTask, bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	active := e.active[agentID]
	if active == nil || active.executionID != executionID {
		return nil, false, fault.ErrNotFound
	}
	if active.running {
		return active, false, nil
	}
	active.running = true
	return active, true, nil
}

// runReturned 保留结算失败的占用，允许后续激活重试同一份结算结果。
func (e *ActiveTasks) runReturned(agentID identity.AgentID, active *activeTask) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.active[agentID] == active {
		active.running = false
	}
}

// Wake 保留审批通知，审批早于进入等待也不会丢失。
func (e *ActiveTasks) Wake(agentID identity.AgentID, executionID identity.AgentExecutionID) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if active := e.active[agentID]; active != nil && active.executionID == executionID && !active.ending {
		select {
		case active.wake <- struct{}{}:
		default:
		}
	}
}

func (e *ActiveTasks) occupied(agentID identity.AgentID) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.active[agentID] != nil
}

func (e *ActiveTasks) stopAll() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, active := range e.active {
		active.cancel(ErrExecutionStopped)
	}
}

func (e *ActiveTasks) hasActive() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.active) != 0
}
