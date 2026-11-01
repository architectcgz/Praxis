package model

import (
	"context"
	"sync"

	modelconfig "praxis/internal/core/model/config"
	"praxis/internal/logging"
)

// Dependencies 注入配置编辑能力及与会话配置加载共用的锁。
type Dependencies struct {
	Models   modelconfig.ConfigManager
	Logger   *logging.Logger
	ConfigMu *sync.RWMutex
}

type Service struct {
	models   modelconfig.ConfigManager
	logger   *logging.Logger
	configMu *sync.RWMutex
}

// ConfigSnapshot 在一次配置读取中固定元数据和凭据存在性，不包含密钥。
type ConfigSnapshot struct {
	Config       modelconfig.Config
	ProviderKeys map[string]bool
}

func NewService(deps Dependencies) *Service {
	return &Service{
		models:   deps.Models,
		logger:   deps.Logger,
		configMu: deps.ConfigMu,
	}
}

func (s *Service) Configuration() ConfigSnapshot {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	result := ConfigSnapshot{ProviderKeys: make(map[string]bool)}
	result.Config = s.models.Config()
	for _, provider := range result.Config.Providers {
		result.ProviderKeys[provider.ID] = s.models.HasProviderKey(provider.ID)
	}
	return result
}

// SaveModelConfig 更新配置文件和编辑目录，不修改已打开会话的配置。
func (s *Service) SaveModelConfig(config modelconfig.Config) error {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if err := s.models.Save(config); err != nil {
		return err
	}
	s.logger.Infof("model config saved providers=%d groups=%d", len(config.Providers), len(config.Groups))
	return nil
}

func (s *Service) SetProviderKey(providerID, key string) error {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if err := s.models.SetProviderKey(providerID, key); err != nil {
		return err
	}
	s.logger.Infof("provider API key updated provider=%s cleared=%t", providerID, key == "")
	return nil
}

func (s *Service) DiscoverProviderModels(ctx context.Context, providerID string) ([]string, error) {
	return s.models.DiscoverProviderModels(ctx, providerID)
}
