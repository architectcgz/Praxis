package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"praxis/internal/security"
)

// InitializeDefaultFiles 仅在启动时补充缺失文件，不覆盖已有文件；加载和重载只读。
func (d DataRoot) InitializeDefaultFiles(ctx context.Context) error {
	files := map[string]any{
		d.ModelProvidersConfig: ModelDocument{
			Groups:    []ModelGroup{},
			Providers: []ModelProvider{},
		},
		d.ModelCredentialsFile: map[string]any{},
		d.ToolConfig:           DefaultToolsDocument(),
		d.SettingsFile:         Settings{ApprovalMode: security.ApprovalAsk},
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
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
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
