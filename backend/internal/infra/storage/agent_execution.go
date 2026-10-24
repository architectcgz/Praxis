package storage

import (
	"cmp"
	"context"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/identity"
)

type AgentExecutionRepository struct{ s *Store }

// Get 读取指定对象，不存在时返回 errorcode.ErrNotFound。
func (r AgentExecutionRepository) Get(ctx context.Context, id identity.AgentExecutionID) (agentexecution.Execution, error) {
	return get[agentexecution.Execution](ctx, r.s, "agent_execution", id.String())
}

// Save 保存所属执行者提交的执行事实；同一 Execution 的更新由调用方串行推进。
func (r AgentExecutionRepository) Save(ctx context.Context, v agentexecution.Execution) error {
	return r.s.view(ctx, func() error {
		return r.s.appendLocked(ctx, v.SessionID.String(), "agent_execution", v.ID.String(), "agent_execution.saved", v)
	})
}

// FindByRequest 按 Agent 与请求身份查找，未找到时返回 errorcode.ErrNotFound。
func (r AgentExecutionRepository) FindByRequest(ctx context.Context, id identity.AgentID, request identity.RequestID) (agentexecution.Execution, error) {
	return getByClaim[agentexecution.Execution](ctx, r.s, lookupKey{kind: "execution-request", owner: id.String(), value: request.String()})
}

// GetActiveByAgent 通过唯一索引读取 Agent 的活动 Execution。
func (r AgentExecutionRepository) GetActiveByAgent(ctx context.Context, id identity.AgentID) (agentexecution.Execution, error) {
	return getByClaim[agentexecution.Execution](ctx, r.s, lookupKey{kind: "active-agent", owner: id.String()})
}

// ListActive 按创建时间返回所有活动 Execution，供启动恢复使用。
func (r AgentExecutionRepository) ListActive(ctx context.Context) ([]agentexecution.Execution, error) {
	v, e := listIndexed[agentexecution.Execution](ctx, r.s, lookupKey{kind: "active-executions"}, nil)
	return sorted(v, func(a, b agentexecution.Execution) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	}), e
}

// ListBySession 按创建顺序返回执行事实，用于启动时重建会话所属 Agent 的状态。
func (r AgentExecutionRepository) ListBySession(ctx context.Context, id identity.SessionID) ([]agentexecution.Execution, error) {
	v, e := listIndexed[agentexecution.Execution](ctx, r.s, lookupKey{kind: "executions-session", owner: id.String()}, nil)
	return sorted(v, func(a, b agentexecution.Execution) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	}), e
}

// ListByAgent 返回指定 Agent 的对象副本；非正数 limit 返回全部任务。
func (r AgentExecutionRepository) ListByAgent(ctx context.Context, id identity.AgentID, limit int) ([]agentexecution.Execution, error) {
	v, e := listIndexed[agentexecution.Execution](ctx, r.s, lookupKey{kind: "executions-agent", owner: id.String()}, nil)
	sorted(v, func(a, b agentexecution.Execution) int {
		return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), cmp.Compare(b.ID, a.ID))
	})
	if limit > 0 {
		v = capped(v, limit)
	}
	return v, e
}
