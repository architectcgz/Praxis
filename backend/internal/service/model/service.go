package model

import (
	"context"
	"sync"

	modelconfig "praxis/internal/core/model/config"
	"praxis/internal/logging"
)

// Dependencies 注入模型目录及与会话创建、执行准入共用的配置锁。
type Dependencies struct {
	Models       modelconfig.ConfigManager
	ReloadConfig func(context.Context) error
	Logger       *logging.Logger
	ConfigMu     *sync.RWMutex
}

type Service struct {
	models       modelconfig.ConfigManager
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
	return &Service{
		models:       deps.Models,
		reloadConfig: deps.ReloadConfig,
		logger:       deps.Logger,
		configMu:     deps.ConfigMu,
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

func (s *Service) ListModels() []modelconfig.Option {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	return s.models.ListModels()
}

// SaveModelConfig 只保存配置并更新可选模型目录，不修改会话已选中的模型配置。
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

// ReloadConfig 由配置持有者串行加载并发布新配置，不修改会话选择；失败时保留原配置。
func (s *Service) ReloadConfig(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.reloadConfig(ctx); err != nil {
		return err
	}
	s.logger.Infof("configuration reloaded")
	return nil
}
