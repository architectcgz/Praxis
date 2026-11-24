package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"praxis/internal/tool"
)

type ToolSetting struct {
	Enabled *bool `json:"enabled"`
}

type ToolsDocument struct {
	Tools map[tool.ToolName]ToolSetting `json:"tools"`
}

func DefaultToolsDocument() ToolsDocument {
	return ToolsDocument{Tools: make(map[tool.ToolName]ToolSetting)}
}

// LoadTools 在文件边界解析出有效启用集合；缺失的注册项默认启用。
func LoadTools(path string, registry *tool.ToolRegistry) (*tool.Catalog, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read tool config: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var document ToolsDocument
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode tool config: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return nil, fmt.Errorf("decode trailing tool config: %w", err)
	}
	if document.Tools == nil {
		return nil, errors.New("tool config requires a non-null tools object")
	}
	for name, setting := range document.Tools {
		if _, registered := registry.Get(name); !registered {
			return nil, fmt.Errorf("tool configuration refers to unregistered tool %q", name)
		}
		if setting.Enabled == nil {
			return nil, fmt.Errorf("tool %q requires a boolean enabled field", name)
		}
	}
	definitions := registry.List()
	enabled := make(map[tool.ToolName]tool.Tool, len(definitions))
	for _, definition := range definitions {
		setting, specified := document.Tools[definition.Name]
		if !specified || *setting.Enabled {
			enabled[definition.Name], _ = registry.Get(definition.Name)
		}
	}
	return tool.NewCatalog(enabled), nil
}
