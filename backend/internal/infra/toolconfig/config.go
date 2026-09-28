// Package toolconfig 加载所有 Tool 的外部配置，并将权限字段交给权限侧处理。
package toolconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"praxis/internal/contracts"
)

// Config 是所有 Tool 外部配置的结构；行为实现仍由代码注册到 ToolRegistry。
type Config struct {
	Tools map[contracts.ToolName]contracts.ToolPermission `json:"tools"`
}

// Load 加载所有 Tool 配置；配置文件不存在时使用内置权限策略。
func Load(path string) (Config, error) {
	if path == "" {
		return Config{}, nil
	}
	encoded, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read tool config: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var config Config
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decode tool config: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Config{}, errors.New("tool config contains multiple JSON values")
		}
		return Config{}, fmt.Errorf("decode trailing tool config: %w", err)
	}
	return config, nil
}
