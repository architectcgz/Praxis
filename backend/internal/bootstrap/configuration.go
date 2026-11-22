package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"praxis/internal/config"
	"praxis/internal/engine/session"
	"praxis/internal/provider/router"
	"praxis/internal/security"
	"praxis/internal/tool"
)

// configuration 只拥有固定的工具实现，每次加载返回独立只读配置，不向执行路径发布可变目录。
type configuration struct {
	registry *tool.ToolRegistry
}

func loadConfiguration(root config.DataRoot, extensions []tool.Tool) (*configuration, error) {
	registry, err := buildToolRegistry(extensions)
	if err != nil {
		return nil, err
	}
	loader := &configuration{registry: registry}
	if _, err := loader.loadSessionConfiguration(context.Background(), root); err != nil {
		return nil, err
	}
	return loader, nil
}

func (c *configuration) loadSessionConfiguration(ctx context.Context, root config.DataRoot) (*session.Configuration, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	settings, err := config.LoadSettings(root.SettingsFile)
	if err != nil {
		return nil, err
	}
	models, err := config.LoadModels(root.ModelProvidersConfig, root.ModelCredentialsFile, nil)
	if err != nil {
		return nil, err
	}
	definitions, err := config.LoadAgentDefinitions(root.AgentDefinitions)
	if err != nil {
		return nil, err
	}
	tools, err := config.LoadTools(root.ToolConfig, c.registry)
	if err != nil {
		return nil, err
	}
	return &session.Configuration{
		Models:      models.Catalog(),
		Definitions: definitions,
		Tools:       tool.NewCatalog(tools, c.registry),
		Provider: router.New(router.Dependencies{
			Credentials: models.Credentials(),
			HTTPClient:  http.DefaultClient,
		}),
		GlobalApprovalMode: settings.ApprovalMode,
	}, nil
}

// initializeDefaultFiles 只补充缺失文件，配置加载和重载均不调用此步骤。
func initializeDefaultFiles(ctx context.Context, root config.DataRoot) error {
	files := map[string]any{
		root.ModelProvidersConfig: config.ModelDocument{
			Groups:    []config.ModelGroup{},
			Providers: []config.ModelProvider{},
		},
		root.ModelCredentialsFile: map[string]any{},
		root.ToolConfig: tool.Config{
			Tools: map[tool.ToolName]tool.ToolConfig{},
		},
		root.SettingsFile: config.Settings{
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
