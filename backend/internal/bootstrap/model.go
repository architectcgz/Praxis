package bootstrap

import (
	"context"
	"praxis/internal/core/tool"
	"praxis/internal/dataroot"
	"praxis/internal/infra/agentregistry"
	modelregistry "praxis/internal/infra/modelregistry"
	"praxis/internal/infra/toolregistry"
	toolinfra "praxis/internal/infra/tools"
	"praxis/internal/logging"
	agentservice "praxis/internal/service/agent"
	approvalservice "praxis/internal/service/approval"
	modelservice "praxis/internal/service/model"
	"praxis/internal/settings"
	"sync"
)

type configuration struct {
	mu              sync.RWMutex
	approval        approvalservice.ConfigSnapshot
	models          *modelregistry.Registry
	agents          *agentregistry.Registry
	tools           *tool.Catalog
	registry        *tool.ToolRegistry
	registeredNames []tool.ToolName
}

func loadConfiguration(root dataroot.DataRoot, extensions []tool.Tool) (*configuration, error) {
	configured, err := settings.Load(root.SettingsFile)
	if err != nil {
		return nil, err
	}
	models, err := modelregistry.Load(root.ModelProvidersConfig, root.ModelCredentialsFile, nil)
	if err != nil {
		return nil, err
	}
	registry, err := toolinfra.NewRegistry(extensions)
	if err != nil {
		return nil, err
	}
	names := make([]tool.ToolName, 0)
	for _, definition := range registry.List() {
		names = append(names, definition.Name)
	}
	tools, err := toolregistry.Load(root.ToolConfig)
	if err != nil {
		return nil, err
	}
	if err := tools.ValidateRegistered(names); err != nil {
		return nil, err
	}
	agents, err := agentregistry.Load(root.AgentDefinitions)
	if err != nil {
		return nil, err
	}
	catalog := tool.NewCatalog(tools, registry)
	return &configuration{
		approval: approvalservice.ConfigSnapshot{
			GlobalMode: configured.ApprovalMode,
			Tools:      catalog.Snapshot(),
		},
		models:          models,
		agents:          agents,
		tools:           catalog,
		registry:        registry,
		registeredNames: names,
	}, nil
}

// ApprovalConfig 只复制已发布快照，配置锁不会延伸到仓储查询、审批等待或工具执行。
func (c *configuration) ApprovalConfig() approvalservice.ConfigSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.approval
}

// reload 在全部配置加载和校验成功后整体发布，失败不改变已发布的目录和审批快照。
func (c *configuration) reload(ctx context.Context, root dataroot.DataRoot) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	configured, err := settings.Load(root.SettingsFile)
	if err != nil {
		return err
	}
	models, err := modelregistry.Load(root.ModelProvidersConfig, root.ModelCredentialsFile, nil)
	if err != nil {
		return err
	}
	agents, err := agentregistry.Load(root.AgentDefinitions)
	if err != nil {
		return err
	}
	tools, err := toolregistry.Load(root.ToolConfig)
	if err != nil {
		return err
	}
	if err := tools.ValidateRegistered(c.registeredNames); err != nil {
		return err
	}
	catalog := tool.NewCatalog(tools, c.registry)
	if err := agentservice.ValidateDefinitions(c.agents.List(), agents.List()); err != nil {
		return err
	}
	approval := approvalservice.ConfigSnapshot{
		GlobalMode: configured.ApprovalMode,
		Tools:      catalog.Snapshot(),
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// 快照中的规则不引用可重载目录；已取得的快照在本次发布后仍保持原内容。
	c.models.ReplaceFrom(models)
	c.agents.ReplaceFrom(agents)
	c.tools.ReplaceFrom(catalog)
	c.approval = approval
	return nil
}

func buildModelService(root dataroot.DataRoot, config *configuration, logger *logging.Logger) *modelservice.Service {
	return modelservice.NewService(modelservice.Dependencies{
		Models:   config.models,
		ConfigMu: &config.mu,
		Logger:   logger,
		ReloadConfig: func(ctx context.Context) error {
			return config.reload(ctx, root)
		},
	})
}
