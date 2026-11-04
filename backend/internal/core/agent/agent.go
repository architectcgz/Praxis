package agent

import (
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
	AgentInterrupted AgentState = "interrupted"
	AgentFailed      AgentState = "failed"
)

// Agent 由单次 Run 创建并持有；执行结束后随 Run 释放，不由 Session 缓存。
// 实例含有互斥锁，始终以指针共享，不参与持久化，也不能按值复制。
type Agent struct {
	ID           identity.AgentID
	SessionID    identity.SessionID
	DefinitionID identity.AgentDefinitionID
	Kind         AgentKind
	CreatedAt    time.Time

	mu           sync.RWMutex
	state        AgentState
	currentRunID identity.RunID
	updatedAt    time.Time
}

// IsMain 判断实例是否使用 Session 上下文；Subagent 使用隔离的私有上下文。
func (a *Agent) IsMain() bool {
	return a.Kind == KindMain
}

// NewAgent 为已准入的 Run 创建专属实例，不重复校验入口身份。
func NewAgent(id identity.AgentID, sessionID identity.SessionID, definitionID identity.AgentDefinitionID, kind AgentKind, runID identity.RunID, at time.Time) *Agent {
	return &Agent{
		ID:           id,
		SessionID:    sessionID,
		DefinitionID: definitionID,
		Kind:         kind,
		CreatedAt:    at.UTC(),
		state:        AgentExecuting,
		currentRunID: runID,
		updatedAt:    at.UTC(),
	}
}

// State 在同一次持锁读取中返回状态、当前执行和更新时间。
func (a *Agent) State() (AgentState, identity.RunID, time.Time) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.state, a.currentRunID, a.updatedAt
}

// ApplyInterrupt 只更新已接受中断信号的当前执行；执行身份保留到结算完成，迟到信号不能影响新执行。
func (a *Agent) ApplyInterrupt(id identity.RunID, at time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.currentRunID != id || id == "" {
		return
	}
	if a.state == AgentExecuting {
		a.state = AgentInterrupted
		a.updatedAt = at.UTC()
	}
}

// FinishRun 由所属 Run 在结算提交后传入结果状态，不重放控制意图。
func (a *Agent) FinishRun(state AgentState, at time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.currentRunID = ""
	a.state = state
	a.updatedAt = at.UTC()
}
