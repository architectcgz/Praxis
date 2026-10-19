package jsonl

import (
	"cmp"
	"context"
	"praxis/internal/core/toolexecution"
	"praxis/internal/identity"
)

type ToolExecutionRepository struct{ s *Store }

// Get 读取指定对象，不存在时返回 fault.ErrNotFound。
func (r ToolExecutionRepository) Get(ctx context.Context, id identity.ToolExecutionID) (toolexecution.Execution, error) {
	return get[toolexecution.Execution](ctx, r.s, "tool", id.String())
}

// Save 保存入口已处理的对象，事务未提交时只更新工作区。
func (r ToolExecutionRepository) Save(ctx context.Context, v toolexecution.Execution) error {
	return save(ctx, r.s, v.SessionID.String(), "tool", v.ID.String(), "tool.saved", v)
}

// FindByExecutionCall 按 Agent 执行和 Provider 调用身份查询工具执行记录。
func (r ToolExecutionRepository) FindByExecutionCall(ctx context.Context, id identity.AgentExecutionID, call string) (toolexecution.Execution, error) {
	return getByClaim[toolexecution.Execution](ctx, r.s, lookupKey{kind: "tool-call", owner: id.String(), value: call})
}

// ListUnsettledBySession 返回指定 Session 尚未结算的工具调用，供恢复使用。
func (r ToolExecutionRepository) ListUnsettledBySession(ctx context.Context, id identity.SessionID) ([]toolexecution.Execution, error) {
	v, e := listIndexed[toolexecution.Execution](ctx, r.s, lookupKey{kind: "unsettled-session", owner: id.String()}, nil)
	return sorted(v, func(a, b toolexecution.Execution) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	}), e
}

func (r ToolExecutionRepository) ListByExecution(ctx context.Context, id identity.AgentExecutionID) ([]toolexecution.Execution, error) {
	return listIndexed[toolexecution.Execution](ctx, r.s, lookupKey{kind: "tools-task", owner: id.String()}, nil)
}

func (r ToolExecutionRepository) ListAwaitingByAgent(ctx context.Context, id identity.AgentID) ([]toolexecution.Execution, error) {
	values, err := listIndexed[toolexecution.Execution](ctx, r.s, lookupKey{kind: "awaiting-agent", owner: id.String()}, nil)
	return sorted(values, func(a, b toolexecution.Execution) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	}), err
}
