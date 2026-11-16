package bootstrap

import (
	"context"
	"net/http"
	agentmodel "praxis/internal/core/agent"
	modelapi "praxis/internal/core/model"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/core/tool"
	"praxis/internal/dataroot"
	"praxis/internal/infra/agentregistry"
	"praxis/internal/infra/modelregistry"
	"praxis/internal/infra/providers"
	"praxis/internal/infra/toolregistry"
	"praxis/internal/settings"
	"sync"
)

// configuration 只拥有固定的工具实现，每次加载返回独立只读配置，不向执行路径发布可变目录。
type configuration struct {
	mu       sync.RWMutex
	registry *tool.ToolRegistry
}

func loadConfiguration(root dataroot.DataRoot, extensions []tool.Tool) (*configuration, error) {
	registry, err := buildToolRegistry(extensions)
	if err != nil {
		return nil, err
	}
	config := &configuration{registry: registry}
	if _, err := config.loadSessionConfiguration(context.Background(), root); err != nil {
		return nil, err
	}
	return config, nil
}

func (c *configuration) loadSessionConfiguration(ctx context.Context, root dataroot.DataRoot) (*sessionmodel.Configuration, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	configured, err := settings.Load(root.SettingsFile)
	if err != nil {
		return nil, err
	}
	models, err := modelregistry.Load(root.ModelProvidersConfig, root.ModelCredentialsFile, nil)
	if err != nil {
		return nil, err
	}
	agents, err := agentregistry.Load(root.AgentDefinitions)
	if err != nil {
		return nil, err
	}
	definitions := agents.List()
	if err := agentmodel.ValidateDefinitions(nil, definitions); err != nil {
		return nil, err
	}
	tools, err := toolregistry.Load(root.ToolConfig, c.registry)
	if err != nil {
		return nil, err
	}
	return &sessionmodel.Configuration{
		Models:      modelapi.NewCatalog(models.Config()),
		Definitions: definitions,
		Tools:       tool.NewCatalog(tools, c.registry),
		Provider: providers.New(providers.Dependencies{
			Credentials: models.Credentials(),
			HTTPClient:  http.DefaultClient,
		}),
		GlobalApprovalMode: configured.ApprovalMode,
	}, nil
}
