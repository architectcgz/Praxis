package session

import (
	"context"
	"fmt"
	agentmodel "praxis/internal/core/agent"
	sessionentry "praxis/internal/core/entry"
	modelapi "praxis/internal/core/model"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/core/security"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
	"sync"
)

// HistoryStore 保存和读取 Agent 的完整 History，不解释 Entry 的业务语义。
type HistoryStore interface {
	CreateSubagent(context.Context, identity.AgentID) error
	RemoveSubagent(context.Context, identity.AgentID) error
	Append(context.Context, identity.AgentID, *sessionentry.Entry) error
	CommitTerminal(context.Context, identity.AgentID, *sessionentry.Entry) error
	ReadBackward(context.Context, identity.AgentID, *int64, int) (HistoryPage, error)
	Close(context.Context) error
}

// Configuration 属于已打开的会话；发布后只读，只有显式 reload 才整体替换。
// Provider 持有同一批次的凭据，配置不写入会话日志，也不通过 DTO 暴露。
type Configuration struct {
	Models             *modelapi.Catalog
	Definitions        []agentmodel.AgentDefinition
	Tools              tool.ToolCatalog
	Provider           providerapi.Provider
	GlobalApprovalMode security.ApprovalMode
	ApprovalMode       security.ApprovalMode
}

func (c *Configuration) Definition(id identity.AgentDefinitionID) (agentmodel.AgentDefinition, error) {
	for _, definition := range c.Definitions {
		if definition.ID == id {
			return definition, nil
		}
	}
	return agentmodel.AgentDefinition{}, fmt.Errorf("agent definition %q is not configured", id)
}

// ModelChoice 表达用户选择的模型和推理等级；查询结果全空表示尚未选择。
type ModelChoice struct {
	ProviderID     string
	ModelID        string
	ReasoningLevel string
}

// Session 持有会话配置和各 Agent 的 AgentContext。
// 数据锁保护配置、Agent 登记与上下文；各 Agent 的 History 读写由存储串行化。
type Session struct {
	mu            sync.RWMutex
	configuration *Configuration
	choice        ModelChoice
	data          *Data
	log           HistoryStore
}

func New(data *Data, log HistoryStore) *Session {
	return &Session{
		data: data,
		log:  log,
	}
}

func (s *Session) Configure(config *Configuration, choice ModelChoice) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configuration = config
	s.choice = choice
}

func (s *Session) Configuration() *Configuration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.configuration
}

func (s *Session) Choice() ModelChoice {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.choice
}

func (s *Session) RunConfiguration() (*Configuration, modelapi.Config, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	model, err := s.configuration.Models.Get(s.choice.ProviderID, s.choice.ModelID, s.choice.ReasoningLevel)
	return s.configuration, model, err
}

// AgentContext 返回指定 Agent 最近摘要及之后的活跃历史窗口只读切片，供所属串行执行筛选使用。
func (s *Session) AgentContext(agentID identity.AgentID) ([]*sessionentry.Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.data.HasAgent(agentID) {
		return nil, fmt.Errorf("agent %s is not part of session", agentID)
	}
	return s.data.Context(agentID).Entries, nil
}

// RegisterAgent 记录已存在的 Agent History 作用域。
func (s *Session) RegisterAgent(agentID identity.AgentID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.data.HasAgent(agentID) {
		s.data.AgentContexts[agentID] = AgentContext{}
	}
}

// RestoreAgentContext 发布启动恢复得到的活跃历史窗口，接管调用方构造的切片。
func (s *Session) RestoreAgentContext(agentID identity.AgentID, entries []*sessionentry.Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.data.HasAgent(agentID) {
		return fmt.Errorf("agent %s is not part of session", agentID)
	}
	s.data.AgentContexts[agentID] = AgentContext{Entries: entries}
	return nil
}

func (s *Session) Close(ctx context.Context) error {
	return s.log.Close(ctx)
}
