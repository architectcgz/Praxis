package bootstrap

import (
	"context"
	"fmt"
	"praxis/internal/infra/agentregistry"
	modelregistry "praxis/internal/infra/modelregistry"
	"praxis/internal/infra/toolregistry"
	modelservice "praxis/internal/service/model"
	"sync"

	"praxis/internal/core/tool"
	"praxis/internal/dataroot"
	"praxis/internal/infra/jsonl"
	"praxis/internal/logging"
	"praxis/internal/security"
	"praxis/internal/settings"
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
	models, err := modelregistry.Load(root.ModelProvidersConfig, root.ModelCredentialsFile, nil, newModelStream)
	if err != nil {
		return nil, err
	}
	registry, err := buildToolRegistry(extensions)
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
	agents, err := agentregistry.Load(root.AgentDefinitions, func(providerID, modelID string) error {
		if !modelregistry.ContainsModel(models.Config(), providerID, modelID) {
			return fmt.Errorf("model %q for provider %q is not configured", modelID, providerID)
		}
		return nil
	})
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

func (c *configuration) globalApprovalMode() security.ApprovalMode { return c.settings.ApprovalMode }

func buildModelService(root dataroot.DataRoot, config *configuration, store *jsonl.Store, logger *logging.Logger, configMu *sync.RWMutex) *modelservice.Service {
	return modelservice.NewService(modelservice.Dependencies{
		Models:   config.models,
		Agents:   config.agents,
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
			models, err := modelregistry.Load(root.ModelProvidersConfig, root.ModelCredentialsFile, nil, newModelStream)
			if err != nil {
				return err
			}
			candidate := models.Config()
			agents, err := agentregistry.Load(root.AgentDefinitions, func(providerID, modelID string) error {
				if !modelregistry.ContainsModel(candidate, providerID, modelID) {
					return fmt.Errorf("model %q for provider %q is not configured", modelID, providerID)
				}
				return nil
			})
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
			// 配置锁在外、JSONL 事务锁在内，队列准入和工具授权不会读取混合快照。
			return store.InTx(ctx, func(context.Context) error {
				config.settings = configured
				config.models.ReplaceFrom(models)
				config.agents.ReplaceFrom(agents)
				config.tools.ReplaceFrom(catalog)
				return nil
			})
		},
	})
}
