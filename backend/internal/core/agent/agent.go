package agent

import (
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/identity"
	"sync"
	"time"
)

type AgentState string

// AgentKind 表示实例与 Session 的关系，与角色定义的职责无关。
type AgentKind string

const (
	KindMain     AgentKind = "main"
	KindSubagent AgentKind = "subagent"
)

const (
	AgentIdle        AgentState = "idle"
	AgentExecuting   AgentState = "executing"
	AgentWaiting     AgentState = "waiting"
	AgentPaused      AgentState = "paused"
	AgentInterrupted AgentState = "interrupted"
	AgentFailed      AgentState = "failed"
)

// Agent 是 Session 持有的内存实例；身份创建后不变，执行状态只通过方法修改。
// 实例含有互斥锁，始终以指针共享，不参与持久化，也不能按值复制。
type Agent struct {
	ID           identity.AgentID
	SessionID    identity.SessionID
	DefinitionID identity.AgentDefinitionID
	Kind         AgentKind
	CreatedAt    time.Time

	mu                 sync.RWMutex
	state              AgentState
	currentExecutionID identity.AgentExecutionID
	updatedAt          time.Time
}

// Startable 判断当前状态是否允许开始新的 Execution。
func (s AgentState) Startable() bool {
	switch s {
	case AgentIdle, AgentWaiting, AgentInterrupted, AgentFailed, AgentPaused:
		return true
	default:
		return false
	}
}

// IsMain 判断实例是否拥有 Session 上下文；Subagent 使用隔离的私有上下文。
func (a *Agent) IsMain() bool {
	return a.Kind == KindMain
}

// NewAgent 使用已校验的身份与 Agent 定义创建 idle 实例，不重复检查入口字段。
func NewAgent(id identity.AgentID, sessionID identity.SessionID, definitionID identity.AgentDefinitionID, kind AgentKind, at time.Time) *Agent {
	return &Agent{
		ID:           id,
		SessionID:    sessionID,
		DefinitionID: definitionID,
		Kind:         kind,
		CreatedAt:    at.UTC(),
		state:        AgentIdle,
		updatedAt:    at.UTC(),
	}
}

// State 在同一次持锁读取中返回状态、当前执行和更新时间。
func (a *Agent) State() (AgentState, identity.AgentExecutionID, time.Time) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.state, a.currentExecutionID, a.updatedAt
}

// BeginExecution 在执行事实提交后登记归属；重复确认不覆盖已接受的暂停或停止状态。
func (a *Agent) BeginExecution(execution agentexecution.Execution) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if execution.AgentID != a.ID || execution.SessionID != a.SessionID || execution.Config.AgentDefinitionID != a.DefinitionID {
		return ErrUnavailable
	}
	if a.currentExecutionID == execution.ID {
		return nil
	}
	if a.currentExecutionID != "" {
		return ErrExecuting
	}
	a.currentExecutionID = execution.ID
	a.state = AgentExecuting
	a.updatedAt = execution.CreatedAt.UTC()
	if execution.StartedAt.After(a.updatedAt) {
		a.updatedAt = execution.StartedAt.UTC()
	}
	return nil
}

// ApplyControl 只更新已接受信号的目标执行；执行身份保留到结算完成，迟到信号不能影响新执行。
func (a *Agent) ApplyControl(id identity.AgentExecutionID, state AgentState, at time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.currentExecutionID != id || id == "" || state != AgentPaused && state != AgentInterrupted {
		return
	}
	if a.state == AgentExecuting {
		a.state = state
		a.updatedAt = at.UTC()
	}
}

// FinishExecution 在结算提交后释放归属，并以实际执行结果确定状态，不重放控制意图。
func (a *Agent) FinishExecution(execution agentexecution.Execution) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.currentExecutionID != execution.ID || execution.Active() {
		return
	}
	a.currentExecutionID = ""
	a.state = AgentIdle
	switch execution.Outcome {
	case agentexecution.ExecutionYielded:
		a.state = AgentWaiting
	case agentexecution.ExecutionPaused:
		a.state = AgentPaused
	case agentexecution.ExecutionInterrupted:
		a.state = AgentInterrupted
	case agentexecution.ExecutionFailed:
		a.state = AgentFailed
	}
	a.updatedAt = execution.EndedAt.UTC()
}
