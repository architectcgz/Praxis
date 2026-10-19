// Package dataroot 定义本地应用根目录的路径布局。其他基础设施适配器
// 均以此为基础解析文件路径，因此本包不依赖其他基础设施组件。
package dataroot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// DataRoot 包含所有本地产品数据路径。配置、项目和运行状态在 Root 下分别管理。
// 此类型只解析路径；模型和凭据文件只由 Provider 注册表读写。
type DataRoot struct {
	Root                 string
	Config               string
	Projects             string
	State                string
	ModelProvidersConfig string
	ModelCredentialsFile string
	ToolConfig           string
	SettingsFile         string
	AgentDefinitions     string
}

// Resolve 将已确定的数据根目录解析为绝对路径并构建路径布局。
// 不读取环境变量，不选择默认目录，不创建文件。
func Resolve(root string) (DataRoot, error) {
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
	state := filepath.Join(root, "state")
	return DataRoot{
		Root:                 root,
		Config:               config,
		Projects:             filepath.Join(root, "projects"),
		State:                state,
		ModelProvidersConfig: filepath.Join(config, "models.json"),
		ModelCredentialsFile: filepath.Join(config, "auth.json"),
		ToolConfig:           filepath.Join(config, "tools.json"),
		SettingsFile:         filepath.Join(config, "settings.json"),
		AgentDefinitions:     filepath.Join(config, "agents"),
	}, nil
}

// Initialize 创建不含密钥的目录结构，仅允许所有者访问。
func (d DataRoot) Initialize(ctx context.Context) error {
	if ctx == nil {
		return errors.New("data root initialization context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, path := range []string{
		d.Root,
		d.Config,
		d.Projects,
		d.State,
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
