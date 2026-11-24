package session

import (
	"context"
	"fmt"
	"praxis/internal/agent"
	"praxis/internal/entry"
	"praxis/internal/identity"
	"praxis/internal/model"
	"praxis/internal/provider"
	"praxis/internal/security"
	"praxis/internal/tool"
	"sync"
	"sync/atomic"
)

// HistoryStore 保存和读取 Agent 的完整 History，不解释 Entry 的业务语义。
type HistoryStore interface {
	CreateSubagent(context.Context, identity.AgentID) error
	RemoveSubagent(context.Context, identity.AgentID) error
	Append(context.Context, identity.AgentID, *entry.Entry) error
	CommitTerminal(context.Context, identity.AgentID, *entry.Entry) error
	ReadBackward(context.Context, identity.AgentID, *int64, int) (HistoryPage, error)
	Close(context.Context) error
}

// Configuration 属于已打开的会话；发布后只读，只有显式 reload 才整体替换。
// Provider 持有同一批次的凭据，配置不写入会话日志，也不通过 DTO 暴露。
type Configuration struct {
	Models             *model.Catalog
	Definitions        []agent.AgentDefinition
	Tools              tool.ToolCatalog
	Provider           provider.Provider
	GlobalApprovalMode security.ApprovalMode
}

func (c *Configuration) Definition(id identity.AgentDefinitionID) (agent.AgentDefinition, error) {
	for _, definition := range c.Definitions {
		if definition.ID == id {
			return definition, nil
		}
	}
	return agent.AgentDefinition{}, fmt.Errorf("agent definition %q is not configured", id)
}

// sessionSettings 及其引用的配置和审批覆盖在发布后只读。
type sessionSettings struct {
	configuration    *Configuration
	choice           model.Choice
	approvalOverride *security.ApprovalMode
}

// Session 的设置原子发布；agentsMu 只保护成员表，各 Agent 的上下文由所属执行串行访问。
type Session struct {
	settings atomic.Pointer[sessionSettings]
	agentsMu sync.RWMutex
	data     *Data
	log      HistoryStore
}

func New(data *Data, log HistoryStore) *Session {
	s := &Session{
		data: data,
		log:  log,
	}
	s.settings.Store(&sessionSettings{})
	return s
}

func (s *Session) Configure(config *Configuration, choice model.Choice, override *security.ApprovalMode) {
	if choice.ModelID == "" {
		choice = config.Models.DefaultModel().DefaultChoice()
	}
	s.settings.Store(&sessionSettings{
		configuration:    config,
		choice:           choice,
		approvalOverride: override,
	})
}

func (s *Session) Configuration() *Configuration {
	return s.settings.Load().configuration
}

func (s *Session) Choice() model.Choice {
	return s.settings.Load().choice
}

func (s *Session) SetChoice(choice model.Choice) {
	for {
		current := s.settings.Load()
		next := new(*current)
		next.choice = choice
		if s.settings.CompareAndSwap(current, next) {
			return
		}
	}
}

func (s *Session) SetApprovalMode(override *security.ApprovalMode) {
	for {
		current := s.settings.Load()
		next := new(*current)
		next.approvalOverride = override
		if s.settings.CompareAndSwap(current, next) {
			return
		}
	}
}

func (s *Session) ApprovalMode() security.ApprovalMode {
	settings := s.settings.Load()
	return security.EffectiveApprovalMode(settings.configuration.GlobalApprovalMode, settings.approvalOverride)
}

func (s *Session) RunConfiguration() (*Configuration, model.Config, error) {
	settings := s.settings.Load()
	model, err := settings.configuration.Models.Get(settings.choice)
	return settings.configuration, model, err
}

// AgentContext 返回指定 Agent 最近摘要及之后的活跃历史窗口只读切片，供所属串行执行筛选使用。
func (s *Session) AgentContext(agentID identity.AgentID) ([]*entry.Entry, error) {
	agentCtx := s.agentContext(agentID)
	if agentCtx == nil {
		return nil, fmt.Errorf("agent %s is not part of session", agentID)
	}
	return agentCtx.Entries, nil
}

func (s *Session) agentContext(agentID identity.AgentID) *AgentContext {
	s.agentsMu.RLock()
	agentCtx := s.data.AgentContexts[agentID]
	s.agentsMu.RUnlock()
	return agentCtx
}

func (s *Session) Close(ctx context.Context) error {
	return s.log.Close(ctx)
}
