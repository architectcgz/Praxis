package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	modelconfig "praxis/internal/core/model/config"
	"praxis/internal/core/security"
	"praxis/internal/core/tool"
	"praxis/internal/dataroot"
	"praxis/internal/settings"
)

// initializeDefaultFiles 只补充缺失文件，配置加载和重载均不调用此步骤。
func initializeDefaultFiles(ctx context.Context, root dataroot.DataRoot) error {
	files := map[string]any{
		root.ModelProvidersConfig: modelconfig.Config{Groups: []modelconfig.Group{}, Providers: []modelconfig.Provider{}},
		root.ModelCredentialsFile: map[string]any{},
		root.ToolConfig:           tool.Config{Bash: security.CommandPolicy{Rules: []security.CommandRule{}}},
		root.SettingsFile:         settings.Config{ApprovalMode: security.ApprovalAsk},
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
	instructions := defaultAgentInstructions()
	for id, config := range defaultAgentConfigs() {
		directory := filepath.Join(root.AgentDefinitions, id.String())
		if err := os.MkdirAll(directory, 0700); err != nil {
			return err
		}
		encoded, err := json.MarshalIndent(config, "", "  ")
		if err != nil {
			return err
		}
		if err := createDefaultFile(ctx, filepath.Join(directory, "agent.json"), append(encoded, '\n')); err != nil {
			return err
		}
		if err := createDefaultFile(ctx, filepath.Join(directory, "AGENT.md"), []byte(instructions[id])); err != nil {
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
