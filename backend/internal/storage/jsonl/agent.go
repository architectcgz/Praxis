package jsonl

import (
	"cmp"
	"context"
	agentmodel "praxis/internal/agent"
	"praxis/internal/fault"
	"praxis/internal/identity"
)

type AgentRepository struct{ s *Store }

type ControlRepository struct{ s *Store }

// Get 读取指定对象，不存在时返回 fault.ErrNotFound。
func (r AgentRepository) Get(ctx context.Context, id identity.AgentID) (agentmodel.Agent, error) {
	return get[agentmodel.Agent](ctx, r.s, "agent", id.String())
}

// Save 保存入口已处理的对象，事务未提交时只更新工作区。
func (r AgentRepository) Save(ctx context.Context, v agentmodel.Agent) error {
	return save(ctx, r.s, v.SessionID.String(), "agent", v.ID.String(), "agent.saved", v)
}

// ListBySession 返回指定 Session 的对象副本。
func (r AgentRepository) ListBySession(ctx context.Context, id identity.SessionID, limit int) ([]agentmodel.Agent, error) {
	v, e := listIndexed[agentmodel.Agent](ctx, r.s, lookupKey{kind: "agents-session", owner: id.String()}, nil)
	return capped(sorted(v, func(a, b agentmodel.Agent) int { return cmp.Compare(a.ID, b.ID) }), limit), e
}

// GetBySessionAndDefinition 按 Session 与 Agent 定义查找，未找到时返回 fault.ErrNotFound。
func (r AgentRepository) GetBySessionAndDefinition(ctx context.Context, id identity.SessionID, definition identity.AgentDefinitionID) (agentmodel.Agent, error) {
	v, e := listIndexed[agentmodel.Agent](ctx, r.s, lookupKey{kind: "agents-definition", owner: id.String(), value: definition.String()}, nil)
	if e != nil {
		return agentmodel.Agent{}, e
	}
	if len(v) == 0 {
		return agentmodel.Agent{}, fault.ErrNotFound
	}
	sorted(v, func(a, b agentmodel.Agent) int { return cmp.Compare(a.ID, b.ID) })
	return v[0], nil
}

// Get 读取指定对象，不存在时返回 fault.ErrNotFound。
func (r ControlRepository) Get(ctx context.Context, id identity.AgentControlCommandID) (agentmodel.AgentControlCommand, error) {
	return get[agentmodel.AgentControlCommand](ctx, r.s, "control", id.String())
}

// Save 保存入口已处理的对象，事务未提交时只更新工作区。
func (r ControlRepository) Save(ctx context.Context, v agentmodel.AgentControlCommand) error {
	a, e := (AgentRepository{r.s}).Get(ctx, v.AgentID)
	if e != nil {
		return e
	}
	return save(ctx, r.s, a.SessionID.String(), "control", v.ID.String(), "control.saved", v)
}

// ListOpenByAgent 返回 Agent 尚未应用的控制命令。
func (r ControlRepository) ListOpenByAgent(ctx context.Context, id identity.AgentID, limit int) ([]agentmodel.AgentControlCommand, error) {
	v, e := listIndexed[agentmodel.AgentControlCommand](ctx, r.s, lookupKey{kind: "controls-agent", owner: id.String()}, nil)
	return capped(sorted(v, func(a, b agentmodel.AgentControlCommand) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	}), limit), e
}
