// Package config 管理本地数据目录布局和配置文件加载。
package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// DataRoot 定义应用顶层布局；存储内部文件名由存储层管理。
type DataRoot struct {
	Root                 string
	Config               string
	Projects             string
	ApplicationLog       string
	ToolOutput           string
	ModelProvidersConfig string
	ModelCredentialsFile string
	ToolConfig           string
	SettingsFile         string
	AgentDefinitions     string
}

// DefaultDataRoot 固定使用当前用户主目录下的 .praxis，不创建文件。
func DefaultDataRoot() (DataRoot, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return DataRoot{}, fmt.Errorf("resolve user home directory: %w", err)
	}
	return ResolveDataRoot(filepath.Join(home, ".praxis"))
}

// ResolveDataRoot 将指定目录解析为绝对路径并构建应用布局，不创建文件或目录。
func ResolveDataRoot(root string) (DataRoot, error) {
	if root == "" {
		return DataRoot{}, errors.New("data root is required")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return DataRoot{}, fmt.Errorf("resolve data root: %w", err)
	}
	if filepath.Dir(root) == root {
		return DataRoot{}, errors.New("data root cannot be the filesystem root")
	}
	config := filepath.Join(root, "config")
	return DataRoot{
		Root:                 root,
		Config:               config,
		Projects:             filepath.Join(root, "projects"),
		ApplicationLog:       filepath.Join(root, "praxis.log"),
		ToolOutput:           filepath.Join(os.TempDir(), "praxis", "tool-output"),
		ModelProvidersConfig: filepath.Join(config, "models.json"),
		ModelCredentialsFile: filepath.Join(config, "auth.json"),
		ToolConfig:           filepath.Join(config, "tools.json"),
		SettingsFile:         filepath.Join(config, "settings.json"),
		AgentDefinitions:     filepath.Join(config, "agents"),
	}, nil
}

// Initialize 创建不含密钥的目录结构，仅允许所有者访问。
func (d DataRoot) Initialize(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	directories := []string{
		d.Root,
		d.Config,
		d.Projects,
		d.AgentDefinitions,
	}
	if d.ApplicationLog == "" || d.ToolOutput == "" || slices.Contains(directories, "") {
		return errors.New("data root contains an empty required path")
	}
	for _, path := range directories {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return fmt.Errorf("create data directory %q: %w", path, err)
		}
	}
	return nil
}

func InstanceLockPath() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve application instance directory: %w", err)
	}
	return filepath.Join(cache, "praxis", "instance.lock"), nil
}
