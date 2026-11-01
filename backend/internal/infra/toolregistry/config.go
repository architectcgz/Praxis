package toolregistry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"praxis/internal/core/tool"
)

// Load 只读加载并校验工具配置，缺失、格式非法或引用未注册工具时返回错误。
func Load(path string, registry *tool.ToolRegistry) (tool.Config, error) {
	var config tool.Config
	encoded, err := os.ReadFile(path)
	if err != nil {
		return tool.Config{}, fmt.Errorf("read tool config: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return tool.Config{}, fmt.Errorf("decode tool config: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return tool.Config{}, errors.New("tool config contains multiple JSON values")
		}
		return tool.Config{}, fmt.Errorf("decode trailing tool config: %w", err)
	}
	if err := validateRegistered(config, registry); err != nil {
		return tool.Config{}, err
	}
	return config, nil
}

func validateRegistered(config tool.Config, registry *tool.ToolRegistry) error {
	for name := range config.Tools {
		if _, registered := registry.Get(name); !registered {
			return fmt.Errorf("tool configuration refers to unregistered tool %q", name)
		}
	}
	return nil
}
