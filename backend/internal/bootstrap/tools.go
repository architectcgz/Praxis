package bootstrap

import (
	"praxis/internal/core/tool"
	"praxis/internal/core/tools"
	patchinfra "praxis/internal/infra/tools/applypatch"
	bashinfra "praxis/internal/infra/tools/bash"
	readinfra "praxis/internal/infra/tools/readfile"
)

func buildToolRegistry(extensions []tool.Tool) (*tool.ToolRegistry, error) {
	registry := tool.NewToolRegistry()
	builtins := []tool.Tool{
		tool.Function[tools.ReadFileArguments]{
			Name:             tools.ReadFileName,
			Description:      tools.ReadFileDescription,
			InputSchema:      []byte(tools.ReadFileInputSchema),
			IsReadOnly:       true,
			DefaultArguments: tools.DefaultReadFileArguments,
			PrepareArguments: tools.PrepareReadFileArguments,
			Execute:          readinfra.Call,
		},
		tool.Function[tools.BashArguments]{
			Name:             tools.BashName,
			Description:      tools.BashDescription,
			InputSchema:      []byte(tools.BashInputSchema),
			DefaultArguments: tools.DefaultBashArguments,
			PrepareArguments: tools.PrepareBashArguments,
			Execute:          bashinfra.Call,
		},
		tool.Function[tools.ApplyPatchArguments]{
			Name:             tools.ApplyPatchName,
			Description:      tools.ApplyPatchDescription,
			InputSchema:      []byte(tools.ApplyPatchInputSchema),
			DefaultArguments: tools.DefaultApplyPatchArguments,
			PrepareArguments: tools.PrepareApplyPatchArguments,
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
