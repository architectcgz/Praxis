package session

import (
	"context"
	"fmt"
	agentmodel "praxis/internal/core/agent"
	modelapi "praxis/internal/core/model"
	modelconfig "praxis/internal/core/model/config"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/core/security"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
	agentservice "praxis/internal/service/agent"
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

func (s *Service) Configuration(ctx context.Context, id identity.SessionID) (*Configuration, error) {
	owner, err := s.openedSession(ctx, id)
	if err != nil {
		return nil, err
	}
	owner.configMu.RLock()
	defer owner.configMu.RUnlock()
	return owner.config, nil
}

func (s *Service) ModelOptions(ctx context.Context, id identity.SessionID) ([]modelconfig.Option, error) {
	config, err := s.Configuration(ctx, id)
	if err != nil {
		return nil, err
	}
	return config.Models.ListModels(), nil
}

func (s *Service) Definition(ctx context.Context, id identity.SessionID, definitionID identity.AgentDefinitionID) (agentmodel.AgentDefinition, error) {
	config, err := s.Configuration(ctx, id)
	if err != nil {
		return agentmodel.AgentDefinition{}, err
	}
	return config.Definition(definitionID)
}

// RunConfiguration 在同一配置读锁内取得配置和模型选择，执行期间不再读取全局目录。
func (s *Service) RunConfiguration(ctx context.Context, id identity.SessionID) (*Configuration, modelapi.Config, error) {
	owner, err := s.openedSession(ctx, id)
	if err != nil {
		return nil, modelapi.Config{}, err
	}
	owner.configMu.RLock()
	defer owner.configMu.RUnlock()
	model, err := owner.modelSelection.CurrentModelConfig()
	return owner.config, model, err
}

// ReloadConfiguration 由 Coordinator 持有会话命令锁并确认整个会话无活跃执行后调用。
// 加载失败不发布任何候选内容。
func (s *Service) ReloadConfiguration(ctx context.Context, id identity.SessionID) error {
	owner, err := s.openedSession(ctx, id)
	if err != nil {
		return err
	}
	next, err := s.loadConfig(ctx)
	if err != nil {
		return err
	}
	info, err := s.sessions.Get(ctx, id)
	if err != nil {
		return err
	}
	next.ApprovalMode = security.EffectiveApprovalMode(next.GlobalApprovalMode, info.ApprovalMode)
	owner.configMu.Lock()
	defer owner.configMu.Unlock()
	if err := agentservice.ValidateDefinitions(owner.config.Definitions, next.Definitions); err != nil {
		return err
	}
	selection := sessionmodel.ModelSelection{}
	choice := owner.modelSelection.Choice()
	if choice.ModelID != "" {
		model, err := next.Models.Get(choice.ProviderID, choice.ModelID, choice.ReasoningLevel)
		if err != nil {
			return fmt.Errorf("reload selected model: %w", err)
		}
		selection = sessionmodel.NewModelSelection(model)
	}
	owner.config, owner.modelSelection = next, selection
	return nil
}
