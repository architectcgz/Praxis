// Package service 实现桌面 binding 所依赖的前端端口集。
//
// 它是 Wails binding 与具体用例服务之间唯一的适配点：将前端端口签名转换为 service 用例参数，
// 不直接访问 repository 或 store，也不依赖 Wails 包。
package service

import (
	"context"
	"errors"
	"slices"
	"sync"

	"praxis/internal/contracts"
	"praxis/internal/core/model"
	modelconfig "praxis/internal/core/model/config"
	"praxis/internal/logging"
	"praxis/internal/request"
	applicationagent "praxis/internal/service/agent"
	applicationproject "praxis/internal/service/project"
	applicationruntime "praxis/internal/service/runtime"
	applicationsession "praxis/internal/service/session"
	"praxis/internal/timing"
)

// Config 收集实现前端端口所需的用例服务与模型配置能力。
type Config struct {
	Agents           *applicationagent.Service
	Projects         *applicationproject.Service
	Sessions         *applicationsession.Service
	Runtime          *applicationruntime.Service
	Models           modelconfig.ConfigManager
	AgentConfig      modelconfig.AgentDefinitions
	ReloadConfig     func(context.Context) error
	Events           *EventPublisher
	Logger           *logging.Logger
	ListAgentTimings func(context.Context, string, int) ([]timing.Record, error)
	ListSessionUsage func(context.Context, string) ([]model.ModelUsageRecord, error)
}

// Services 是前端端口的实现，由组合根装配后交给 wails 层。
type Services struct {
	agents           *applicationagent.Service
	projects         *applicationproject.Service
	sessions         *applicationsession.Service
	runtime          *applicationruntime.Service
	models           modelconfig.ConfigManager
	agentConfig      modelconfig.AgentDefinitions
	reloadConfig     func(context.Context) error
	configMu         sync.RWMutex
	events           *EventPublisher
	logger           *logging.Logger
	listAgentTimings func(context.Context, string, int) ([]timing.Record, error)
	listSessionUsage func(context.Context, string) ([]model.ModelUsageRecord, error)
}

// New 组装实现。可选依赖（AgentConfig、Events）留空时会退化为安全默认行为。
func New(config Config) *Services {
	return &Services{
		agents:           config.Agents,
		projects:         config.Projects,
		sessions:         config.Sessions,
		runtime:          config.Runtime,
		models:           config.Models,
		agentConfig:      config.AgentConfig,
		reloadConfig:     config.ReloadConfig,
		events:           config.Events,
		logger:           logging.NewFactory().Ensure(config.Logger),
		listAgentTimings: config.ListAgentTimings,
		listSessionUsage: config.ListSessionUsage,
	}
}

// ModelConfig 返回完整模型配置的可编辑副本，供设置界面渲染。
func (s *Services) ModelConfig() ModelConfig {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	if s.models == nil {
		return ModelConfig{
			Groups:    []ModelConfigGroup{},
			Providers: []ModelConfigProvider{},
			Models:    []ConfiguredModel{},
		}
	}
	config := s.models.Config()
	result := ModelConfig{
		Groups:            make([]ModelConfigGroup, 0, len(config.Groups)),
		DefaultProviderID: config.DefaultProviderID,
		Providers:         make([]ModelConfigProvider, 0, len(config.Providers)),
		Models:            []ConfiguredModel{},
	}
	for _, group := range config.Groups {
		result.Groups = append(result.Groups, ModelConfigGroup{
			ID:          group.ID,
			DisplayName: group.DisplayName,
		})
	}
	for _, provider := range config.Providers {
		result.Providers = append(result.Providers, ModelConfigProvider{
			ID:             provider.ID,
			DisplayName:    provider.DisplayName,
			BaseURL:        provider.BaseURL,
			ProxyURL:       provider.ProxyURL,
			DefaultModelID: provider.DefaultModelID,
			HasAPIKey:      s.models.HasProviderKey(provider.ID),
		})
		for _, configured := range provider.Models {
			result.Models = append(result.Models, ConfiguredModel{
				ProviderID:            provider.ID,
				ModelID:               configured.ID,
				DisplayName:           configured.DisplayName,
				GroupID:               configured.GroupID,
				APIFormat:             string(configured.APIFormat),
				ContextWindow:         configured.ContextWindow,
				MaxOutputTokens:       configured.MaxOutputTokens,
				ReasoningLevels:       slices.Clone(configured.ReasoningLevels),
				DefaultReasoningLevel: configured.DefaultReasoningLevel,
			})
		}
	}
	return result
}

// SaveModelConfig 检查 Agent 引用并协调保存已由 request 层准备的配置。
func (s *Services) SaveModelConfig(canonical request.SaveModelConfig) error {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if s.models == nil {
		return errors.New("model registry is not available")
	}
	prepared := canonical.ValidatedConfig()
	if !prepared.Prepared() {
		return contracts.InvalidValue("modelConfig", "模型配置尚未准备")
	}
	config := canonical.Config()
	if s.agentConfig != nil {
		if err := s.agentConfig.ValidateModelConfiguration(config); err != nil {
			return contracts.InvalidValue("modelConfig", err.Error())
		}
	}
	if err := s.models.Save(prepared); err != nil {
		return err
	}
	s.logger.Infof("model config saved providers=%d groups=%d", len(config.Providers), len(config.Groups))
	return nil
}

// SetProviderKey 写入或清除一个已配置 Provider 的 API key。
func (s *Services) SetProviderKey(providerID, value string) error {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if s.models == nil {
		return errors.New("model registry is not available")
	}
	if err := s.models.SetProviderKey(providerID, value); err != nil {
		return err
	}
	s.logger.Infof("provider API key updated provider=%s cleared=%t", providerID, value == "")
	return nil
}

// HasProviderKey 只报告密钥是否存在，不暴露密钥内容。
func (s *Services) HasProviderKey(providerID string) bool {
	if s.models == nil {
		return false
	}
	return s.models.HasProviderKey(providerID)
}

// DiscoverProviderModels 从已配置的 Provider 端点拉取模型 ID，解析后的 API key
// 始终留在 registry 内部。
func (s *Services) DiscoverProviderModels(ctx context.Context, providerID string) ([]string, error) {
	if s.models == nil {
		return nil, errors.New("model registry is not available")
	}
	return s.models.DiscoverProviderModels(ctx, providerID)
}

// SubscribeAgentEvents 注册瞬时 Agent runtime 事件的监听者。收到 task_ended 或 request_canceled 后，
// 消费方应重新查询持久化消息和执行状态。
func (s *Services) SubscribeAgentEvents(observer func(AgentEvent)) func() {
	if s == nil || s.events == nil {
		return func() {}
	}
	return s.events.Subscribe(observer)
}

// EventPublisher 扇出瞬时 Agent runtime 事件，不拥有持久化状态。
// 调用方应在结算后重新查询消息和执行状态。
type EventPublisher struct {
	mu        sync.RWMutex
	nextID    uint64
	observers map[uint64]func(AgentEvent)
}

// NewEventPublisher 创建空的 Agent 事件扇出器。
func NewEventPublisher() *EventPublisher {
	return &EventPublisher{observers: make(map[uint64]func(AgentEvent))}
}

// Subscribe 注册监听者并返回注销函数。
func (p *EventPublisher) Subscribe(observer func(AgentEvent)) func() {
	if p == nil || observer == nil {
		return func() {}
	}
	p.mu.Lock()
	p.nextID++
	id := p.nextID
	p.observers[id] = observer
	p.mu.Unlock()
	return func() {
		p.mu.Lock()
		delete(p.observers, id)
		p.mu.Unlock()
	}
}

// Publish 把一条输出事件交给当前所有监听者。
func (p *EventPublisher) Publish(event AgentEvent) {
	if p == nil || event.AgentID == "" || event.TaskID == "" {
		return
	}
	p.mu.RLock()
	observers := make([]func(AgentEvent), 0, len(p.observers))
	for _, observer := range p.observers {
		observers = append(observers, observer)
	}
	p.mu.RUnlock()
	for _, observer := range observers {
		observer(event)
	}
}

// ReloadConfig 重新读取全部配置，校验失败时保留当前运行时快照。
func (s *Services) ReloadConfig(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.reloadConfig == nil {
		return errors.New("configuration reload is not available")
	}
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if err := s.reloadConfig(ctx); err != nil {
		return err
	}
	s.logger.Infof("configuration reloaded")
	return nil
}
