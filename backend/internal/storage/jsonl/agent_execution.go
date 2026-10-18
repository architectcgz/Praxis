package jsonl

import (
	"cmp"
	"context"
	taskmodel "praxis/internal/agent"
	"praxis/internal/identity"
)

type AgentExecutionRepository struct{ s *Store }

// Get 读取指定对象，不存在时返回 fault.ErrNotFound。
func (r AgentExecutionRepository) Get(ctx context.Context, id identity.AgentExecutionID) (taskmodel.Execution, error) {
	return get[taskmodel.Execution](ctx, r.s, "agent_execution", id.String())
}

// Save 保存入口已处理的对象，事务未提交时只更新工作区。
func (r AgentExecutionRepository) Save(ctx context.Context, v taskmodel.Execution) error {
	return save(ctx, r.s, v.SessionID.String(), "agent_execution", v.ID.String(), "agent_execution.saved", v)
}

// FindByRequest 按 Agent 与请求身份查找，未找到时返回 fault.ErrNotFound。
func (r AgentExecutionRepository) FindByRequest(ctx context.Context, id identity.AgentID, request identity.RequestID) (taskmodel.Execution, error) {
	return getByClaim[taskmodel.Execution](ctx, r.s, lookupKey{kind: "execution-request", owner: id.String(), value: request.String()})
}

// GetActiveByAgent 通过唯一索引读取 Agent 的活动 Task。
func (r AgentExecutionRepository) GetActiveByAgent(ctx context.Context, id identity.AgentID) (taskmodel.Execution, error) {
	return getByClaim[taskmodel.Execution](ctx, r.s, lookupKey{kind: "active-agent", owner: id.String()})
}

// ListActive 按创建时间返回所有活动 Task，供启动恢复使用。
func (r AgentExecutionRepository) ListActive(ctx context.Context) ([]taskmodel.Execution, error) {
	v, e := listIndexed[taskmodel.Execution](ctx, r.s, lookupKey{kind: "active-executions"}, nil)
	return sorted(v, func(a, b taskmodel.Execution) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	}), e
}

// ListByAgent 返回指定 Agent 的对象副本；非正数 limit 返回全部任务。
func (r AgentExecutionRepository) ListByAgent(ctx context.Context, id identity.AgentID, limit int) ([]taskmodel.Execution, error) {
	v, e := listIndexed[taskmodel.Execution](ctx, r.s, lookupKey{kind: "executions-agent", owner: id.String()}, nil)
	sorted(v, func(a, b taskmodel.Execution) int {
		return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), cmp.Compare(b.ID, a.ID))
	})
	if limit > 0 {
		v = capped(v, limit)
	}
	return v, e
}

// CountActiveBySession 返回 Session 内活动 Task 的数量。
func (r AgentExecutionRepository) CountActiveBySession(ctx context.Context, id identity.SessionID) (int, error) {
	count := 0
	err := r.s.view(ctx, func(readCtx context.Context) error {
		count = len(r.s.keys(readCtx, lookupKey{kind: "active-session", owner: id.String()}))
		return nil
	})
	return count, err
}
