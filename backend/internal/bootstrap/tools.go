package bootstrap

import (
	"praxis/internal/tool"
	"praxis/internal/tool/applypatch"
	"praxis/internal/tool/bash"
	"praxis/internal/tool/readfile"
)

func buildToolRegistry(extensions []tool.Tool) (*tool.ToolRegistry, error) {
	registry := tool.NewToolRegistry()
	builtins := []tool.Tool{
		readfile.New(),
		bash.New(),
		applypatch.New(),
	}
	for _, candidate := range append(builtins, extensions...) {
		if err := registry.Register(candidate); err != nil {
			return nil, err
		}
	}
	return registry, nil
}
