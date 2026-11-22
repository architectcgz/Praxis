// Package config 管理本地数据目录布局和配置文件加载。
package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// DataRoot 包含本地产品数据路径；配置和会话按目录管理，应用级日志直接保存在 Root 下。
// 此类型只解析路径并初始化目录，不读写配置内容。
type DataRoot struct {
	Root                 string
	Config               string
	Projects             string
	ModelProvidersConfig string
	ModelCredentialsFile string
	ToolConfig           string
	SettingsFile         string
	AgentDefinitions     string
}

// ResolveDataRoot 将已确定的数据根目录解析为绝对路径并构建路径布局。
// 不读取环境变量，不选择默认目录，不创建文件。
func ResolveDataRoot(root string) (DataRoot, error) {
	if root == "" {
		return DataRoot{}, errors.New("data root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return DataRoot{}, fmt.Errorf("resolve data root: %w", err)
	}
	root = absolute
	if filepath.Dir(root) == root {
		return DataRoot{}, errors.New("data root cannot be the filesystem root")
	}
	config := filepath.Join(root, "config")
	return DataRoot{
		Root:                 root,
		Config:               config,
		Projects:             filepath.Join(root, "projects"),
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
	for _, path := range []string{
		d.Root,
		d.Config,
		d.Projects,
		d.AgentDefinitions,
	} {
		if path == "" {
			return errors.New("data root contains an empty required path")
		}
		if err := os.MkdirAll(path, 0o700); err != nil {
			return fmt.Errorf("create data directory %q: %w", path, err)
		}
	}
	return nil
}
