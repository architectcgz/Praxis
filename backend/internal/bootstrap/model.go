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
	modelservice "praxis/internal/service/model"
	"praxis/internal/settings"
	"sync"
)

type configuration struct {
	settings        settings.Config
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
	return &configuration{
		settings:        configured,
		models:          models,
		agents:          agents,
		tools:           tool.NewCatalog(tools, registry),
		registry:        registry,
		registeredNames: names,
	}, nil
}

func buildModelService(root dataroot.DataRoot, config *configuration, logger *logging.Logger, configMu *sync.RWMutex) *modelservice.Service {
	return modelservice.NewService(modelservice.Dependencies{
		Models:   config.models,
		ConfigMu: configMu,
		Logger:   logger,
		ReloadConfig: func(ctx context.Context) error {
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
			if err := tools.ValidateRegistered(config.registeredNames); err != nil {
				return err
			}
			catalog := tool.NewCatalog(tools, config.registry)
			// Model Service 已持有配置写锁；配置发布不占用持久化锁。
			config.settings = configured
			config.models.ReplaceFrom(models)
			config.agents.ReplaceFrom(agents)
			config.tools.ReplaceFrom(catalog)
			return nil
		},
	})
}
