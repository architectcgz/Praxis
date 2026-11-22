package session

import (
	sessionentry "praxis/internal/core/entry"
	"praxis/internal/identity"
)

// AgentContext 保存某个 Agent 最近摘要及之后的活跃历史窗口，供上下文构建筛选。
type AgentContext struct {
	Entries []*sessionentry.Entry
}

// Accept 在 Entry 已成功持久化后更新活跃历史窗口。
func (c *AgentContext) Accept(value *sessionentry.Entry) {
	if value.IsSummary() {
		c.Entries = []*sessionentry.Entry{value}
		return
	}
	c.Entries = append(c.Entries, value)
}

// Data 只持有 AgentContext 和 Agent 作用域，不缓存完整 History。
type Data struct {
	ID            identity.SessionID
	AgentContexts map[identity.AgentID]AgentContext
}

func Open(id identity.SessionID) *Data {
	return &Data{
		ID: id,
		AgentContexts: map[identity.AgentID]AgentContext{
			identity.AgentID(id): {},
		},
	}
}

func (d *Data) MainAgentID() identity.AgentID { return identity.AgentID(d.ID) }

func (d *Data) HasAgent(id identity.AgentID) bool {
	_, ok := d.AgentContexts[id]
	return ok
}

func (d *Data) Context(id identity.AgentID) AgentContext {
	return d.AgentContexts[id]
}

func (d *Data) Accept(agentID identity.AgentID, value *sessionentry.Entry) {
	context := d.AgentContexts[agentID]
	context.Accept(value)
	d.AgentContexts[agentID] = context
}
