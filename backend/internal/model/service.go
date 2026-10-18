package model

import (
	"context"
	"errors"
	"slices"
	"sync"

	"praxis/internal/fault"
	"praxis/internal/logging"
	modelconfig "praxis/internal/model/config"
)

// Dependencies 注入模型目录及与会话创建、执行准入共用的配置锁。
type Dependencies struct {
	Models       modelconfig.ConfigManager
	Agents       modelconfig.AgentDefinitions
	ReloadConfig func(context.Context) error
	Logger       *logging.Logger
	ConfigMu     *sync.RWMutex
}

type Service struct {
	models       modelconfig.ConfigManager
	agents       modelconfig.AgentDefinitions
	reloadConfig func(context.Context) error
	logger       *logging.Logger
	configMu     *sync.RWMutex
}

// ConfigSnapshot 在一次配置读取中固定元数据和凭据存在性，不包含密钥。
type ConfigSnapshot struct {
	Config       modelconfig.Config
	ProviderKeys map[string]bool
}

func NewService(deps Dependencies) *Service {
	if deps.ConfigMu == nil {
		deps.ConfigMu = &sync.RWMutex{}
	}
	return &Service{
		models:       deps.Models,
		agents:       deps.Agents,
		reloadConfig: deps.ReloadConfig,
		logger:       logging.NewFactory().Ensure(deps.Logger),
		configMu:     deps.ConfigMu,
	}
}

func (s *Service) Configuration() ConfigSnapshot {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	result := ConfigSnapshot{ProviderKeys: make(map[string]bool)}
	if s.models != nil {
		result.Config = s.models.Config()
		for _, provider := range result.Config.Providers {
			result.ProviderKeys[provider.ID] = s.models.HasProviderKey(provider.ID)
		}
	}
	return result
}

func (s *Service) ListModels() []modelconfig.Option {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	if s.models == nil {
		return []modelconfig.Option{}
	}
	models := slices.Clone(s.models.ListModels())
	for index := range models {
		models[index].AssignedAgentDefinitions = slices.Clone(models[index].AssignedAgentDefinitions)
		if s.agents != nil {
			models[index].AssignedAgentDefinitions = s.agents.DefinitionsForModel(models[index].ProviderID, models[index].ModelID)
		}
	}
	return models
}

// SaveModelConfig 接收已规范化配置，只检查当前 Agent 引用并保存。
func (s *Service) SaveModelConfig(config modelconfig.Config) error {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if s.models == nil {
		return errors.New("model registry is not available")
	}
	if s.agents != nil {
		if err := s.agents.ValidateModelConfiguration(config); err != nil {
			return fault.InvalidValue("modelConfig", err.Error())
		}
	}
	if err := s.models.Save(config); err != nil {
		return err
	}
	s.logger.Infof("model config saved providers=%d groups=%d", len(config.Providers), len(config.Groups))
	return nil
}

func (s *Service) SetProviderKey(providerID, key string) error {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if s.models == nil {
		return errors.New("model registry is not available")
	}
	if err := s.models.SetProviderKey(providerID, key); err != nil {
		return err
	}
	s.logger.Infof("provider API key updated provider=%s cleared=%t", providerID, key == "")
	return nil
}

func (s *Service) DiscoverProviderModels(ctx context.Context, providerID string) ([]string, error) {
	if s.models == nil {
		return nil, errors.New("model registry is not available")
	}
	return s.models.DiscoverProviderModels(ctx, providerID)
}

// ReloadConfig 串行替换配置快照；失败时保留当前运行时状态。
func (s *Service) ReloadConfig(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if s.reloadConfig == nil {
		return errors.New("configuration reload is not available")
	}
	if err := s.reloadConfig(ctx); err != nil {
		return err
	}
	s.logger.Infof("configuration reloaded")
	return nil
}
