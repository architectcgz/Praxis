package jsonl

import (
	"cmp"
	"context"
	"praxis/internal/core/toolcall"
	"praxis/internal/identity"
)

type ToolExecutionRepository struct{ s *Store }

// Get 读取指定对象，不存在时返回 fault.ErrNotFound。
func (r ToolExecutionRepository) Get(ctx context.Context, id identity.ToolExecutionID) (toolcall.Execution, error) {
	return get[toolcall.Execution](ctx, r.s, "tool", id.String())
}

// Save 保存入口已处理的对象，事务未提交时只更新工作区。
func (r ToolExecutionRepository) Save(ctx context.Context, v toolcall.Execution) error {
	return save(ctx, r.s, v.SessionID.String(), "tool", v.ID.String(), "tool.saved", v)
}

// FindByExecutionCall 按 Task 和 Provider 调用身份查询，第二个返回值表示是否存在。
func (r ToolExecutionRepository) FindByExecutionCall(ctx context.Context, id identity.AgentExecutionID, call string) (toolcall.Execution, error) {
	return getByClaim[toolcall.Execution](ctx, r.s, lookupKey{kind: "tool-call", owner: id.String(), value: call})
}

// ListUnsettledBySession 返回指定 Session 尚未结算的工具调用，供恢复使用。
func (r ToolExecutionRepository) ListUnsettledBySession(ctx context.Context, id identity.SessionID) ([]toolcall.Execution, error) {
	v, e := listIndexed[toolcall.Execution](ctx, r.s, lookupKey{kind: "unsettled-session", owner: id.String()}, nil)
	return sorted(v, func(a, b toolcall.Execution) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	}), e
}

func (r ToolExecutionRepository) ListByExecution(ctx context.Context, id identity.AgentExecutionID) ([]toolcall.Execution, error) {
	return listIndexed[toolcall.Execution](ctx, r.s, lookupKey{kind: "tools-task", owner: id.String()}, nil)
}

func (r ToolExecutionRepository) ListAwaitingByAgent(ctx context.Context, id identity.AgentID) ([]toolcall.Execution, error) {
	values, err := listIndexed[toolcall.Execution](ctx, r.s, lookupKey{kind: "awaiting-agent", owner: id.String()}, nil)
	return sorted(values, func(a, b toolcall.Execution) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	}), err
}
