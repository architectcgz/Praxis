package bootstrap

import (
	"praxis/internal/core/tool"
	"praxis/internal/core/tool/applypatch"
	"praxis/internal/core/tool/bash"
	"praxis/internal/core/tool/readfile"
	patchinfra "praxis/internal/infra/tools/applypatch"
	bashinfra "praxis/internal/infra/tools/bash"
	readinfra "praxis/internal/infra/tools/readfile"
)

func buildToolRegistry(extensions []tool.Tool) (*tool.ToolRegistry, error) {
	registry := tool.NewToolRegistry()
	builtins := []tool.Tool{
		tool.Function[readfile.Arguments]{
			Name:             readfile.Name,
			Description:      readfile.Description,
			InputSchema:      []byte(readfile.InputSchema),
			IsReadOnly:       true,
			DefaultArguments: readfile.DefaultArguments,
			PrepareArguments: readfile.Prepare,
			Execute:          readinfra.Call,
		},
		tool.Function[bash.Arguments]{
			Name:             bash.Name,
			Description:      bash.Description,
			InputSchema:      []byte(bash.InputSchema),
			DefaultArguments: bash.DefaultArguments,
			PrepareArguments: bash.Prepare,
			Execute:          bashinfra.Call,
		},
		tool.Function[applypatch.Arguments]{
			Name:             applypatch.Name,
			Description:      applypatch.Description,
			InputSchema:      []byte(applypatch.InputSchema),
			DefaultArguments: applypatch.DefaultArguments,
			PrepareArguments: applypatch.Prepare,
			Execute:          patchinfra.Call,
		},
	}
	for _, candidate := range append(builtins, extensions...) {
		if err := registry.Register(candidate); err != nil {
			return nil, err
		}
	}
	return registry, nil
}
