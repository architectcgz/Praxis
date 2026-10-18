// Package model 负责模型配置引用约束、配置保存和 Provider 凭据业务。
package model

import (
	"context"
	"errors"
	"slices"

	"praxis/internal/core/fault"
	modelconfig "praxis/internal/core/model/config"
	"praxis/internal/logging"
)

// Config 收集模型配置业务依赖；配置文件读取通过组合根注入，不直接访问文件系统。
type Config struct {
	Models       modelconfig.ConfigManager
	Agents       modelconfig.AgentDefinitions
	ReloadConfig func(context.Context) error
	Logger       *logging.Logger
}

// Service 协调已准备配置的引用约束和持久化，不认识应用展示结构。
type Service struct {
	models       modelconfig.ConfigManager
	agents       modelconfig.AgentDefinitions
	reloadConfig func(context.Context) error
	logger       *logging.Logger
}

// NewService 连接模型配置能力；缺少模型目录时查询返回空结果，修改操作返回错误。
func NewService(config Config) *Service {
	return &Service{
		models:       config.Models,
		agents:       config.Agents,
		reloadConfig: config.ReloadConfig,
		logger:       logging.NewFactory().Ensure(config.Logger),
	}
}

// ModelConfig 返回不含凭据的业务配置副本，缺少目录时返回空配置。
func (s *Service) ModelConfig() modelconfig.Config {
	if s.models == nil {
		return modelconfig.Config{}
	}
	return s.models.Config()
}

// ListModels 返回已配置模型及引用它的 Agent 定义，调用方不能修改目录内部切片。
func (s *Service) ListModels() []modelconfig.Option {
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

// SaveModelConfig 只检查业务引用关系，不重新规范化或校验已准备配置的字段。
func (s *Service) SaveModelConfig(config modelconfig.Config) error {
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

// SetProviderKey 保存或清除一个已配置 Provider 的密钥；日志不记录凭据内容。
func (s *Service) SetProviderKey(providerID, value string) error {
	if s.models == nil {
		return errors.New("model registry is not available")
	}
	if err := s.models.SetProviderKey(providerID, value); err != nil {
		return err
	}
	s.logger.Infof("provider API key updated provider=%s cleared=%t", providerID, value == "")
	return nil
}

// HasProviderKey 只报告凭据是否存在，不读取或暴露其内容。
func (s *Service) HasProviderKey(providerID string) bool {
	return s.models != nil && s.models.HasProviderKey(providerID)
}

// DiscoverProviderModels 查询 Provider 支持的模型，凭据只在配置能力内部解析。
func (s *Service) DiscoverProviderModels(ctx context.Context, providerID string) ([]string, error) {
	if s.models == nil {
		return nil, errors.New("model registry is not available")
	}
	return s.models.DiscoverProviderModels(ctx, providerID)
}

// ReloadConfig 调用配置恢复能力；恢复失败不替换当前运行时快照。
func (s *Service) ReloadConfig(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.reloadConfig == nil {
		return errors.New("configuration reload is not available")
	}
	if err := s.reloadConfig(ctx); err != nil {
		return err
	}
	s.logger.Infof("configuration reloaded")
	return nil
}
