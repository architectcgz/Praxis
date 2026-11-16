package session

import (
	"fmt"
	agentmodel "praxis/internal/core/agent"
	modelapi "praxis/internal/core/model"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/core/security"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
)

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
			return definition.Snapshot(), nil
		}
	}
	return agentmodel.AgentDefinition{}, fmt.Errorf("agent definition %q is not configured", id)
}

func (s *Session) Configure(config *Configuration, choice ModelChoice) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	s.configuration = config
	s.choice = choice
}
func (s *Session) Configuration() *Configuration {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	return s.configuration
}
func (s *Session) Choice() ModelChoice {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	return s.choice
}
func (s *Session) RunConfiguration() (*Configuration, modelapi.Config, error) {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	model, err := s.configuration.Models.Get(s.choice.ProviderID, s.choice.ModelID, s.choice.ReasoningLevel)
	return s.configuration, model, err
}
func (s *Session) Definition(id identity.AgentDefinitionID) (agentmodel.AgentDefinition, error) {
	return s.Configuration().Definition(id)
}
