package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	modelapi "praxis/internal/core/model"
	modelconfig "praxis/internal/core/model/config"
	"praxis/internal/core/security"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/core/tool"
	"praxis/internal/dataroot"
	"praxis/internal/infra/agentregistry"
	"praxis/internal/infra/modelregistry"
	"praxis/internal/infra/providers"
	"praxis/internal/infra/toolregistry"
	"praxis/internal/settings"
)

// configuration 只拥有固定的工具实现，每次加载返回独立只读配置，不向执行路径发布可变目录。
type configuration struct {
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
	tools, err := toolregistry.Load(root.ToolConfig, c.registry)
	if err != nil {
		return nil, err
	}
	return &sessionmodel.Configuration{
		Models:      modelapi.NewCatalog(models.Config()),
		Definitions: definitions,
		Tools:       c.registry.Enabled(tools),
		Provider: providers.New(providers.Dependencies{
			Credentials: models.Credentials(),
			HTTPClient:  http.DefaultClient,
		}),
		GlobalApprovalMode: configured.ApprovalMode,
	}, nil
}

// initializeDefaultFiles 只补充缺失文件，配置加载和重载均不调用此步骤。
func initializeDefaultFiles(ctx context.Context, root dataroot.DataRoot) error {
	files := map[string]any{
		root.ModelProvidersConfig: modelconfig.Config{
			Groups:    []modelconfig.Group{},
			Providers: []modelconfig.Provider{},
		},
		root.ModelCredentialsFile: map[string]any{},
		root.ToolConfig: tool.Config{
			Tools: map[tool.ToolName]tool.ToolConfig{},
		},
		root.SettingsFile: settings.Config{
			ApprovalMode: security.ApprovalAsk,
		},
	}
	for path, value := range files {
		encoded, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return err
		}
		if err := createDefaultFile(ctx, path, append(encoded, '\n')); err != nil {
			return err
		}
	}
	return nil
}

func createDefaultFile(ctx context.Context, path string, value []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create default config %q: %w", path, err)
	}
	_, writeErr := file.Write(value)
	syncErr := file.Sync()
	closeErr := file.Close()
	return errors.Join(writeErr, syncErr, closeErr)
}
