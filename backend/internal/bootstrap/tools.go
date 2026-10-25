package bootstrap

import (
	"context"
	"errors"
	"praxis/internal/core/tool"
	"praxis/internal/core/tool/applypatch"
	"praxis/internal/core/tool/bash"
	"praxis/internal/core/tool/readfile"
	patchinfra "praxis/internal/infra/tools/applypatch"
	bashinfra "praxis/internal/infra/tools/bash"
	readinfra "praxis/internal/infra/tools/readfile"
)

// builtinTool 只连接 Core 参数规则与 Infra 执行能力，不决定默认值或业务策略。
type builtinTool[Arguments any] struct {
	definition      func() tool.ToolDefinition
	arguments       func() Arguments
	prepare         func(Arguments) (Arguments, error)
	call            func(context.Context, Arguments, string) (tool.ToolResult, error)
	approvalCommand func(Arguments) string
}

func (t builtinTool[Arguments]) Definition() tool.ToolDefinition { return t.definition() }

func (t builtinTool[Arguments]) NewArguments() any { return new(t.arguments()) }

func (t builtinTool[Arguments]) Prepare(arguments any, workspacePath string) (tool.PreparedCall, error) {
	args, ok := arguments.(*Arguments)
	if !ok {
		return tool.PreparedCall{}, errors.New("tool argument type does not match the registered arguments")
	}
	prepared, err := t.prepare(*args)
	if err != nil {
		return tool.PreparedCall{}, err
	}
	command := ""
	if t.approvalCommand != nil {
		command = t.approvalCommand(prepared)
	}
	return tool.PreparedCall{
		Arguments: prepared,
		Command:   command,
		// 本次参数和工作区随调用存续，不回写注册表中的共享工具。
		Call: func(ctx context.Context) (tool.ToolResult, error) {
			return t.call(ctx, prepared, workspacePath)
		},
	}, nil
}

func buildToolRegistry(extensions []tool.Tool) (*tool.ToolRegistry, error) {
	registry := tool.NewToolRegistry()
	builtins := []tool.Tool{
		builtinTool[readfile.Arguments]{
			definition: readfile.Definition,
			arguments:  readfile.NewArguments,
			prepare:    readfile.Prepare,
			call:       readinfra.Call,
		},
		builtinTool[bash.Arguments]{
			definition:      bash.Definition,
			arguments:       bash.NewArguments,
			prepare:         bash.Prepare,
			call:            bashinfra.Call,
			approvalCommand: func(args bash.Arguments) string { return args.Command },
		},
		builtinTool[applypatch.Arguments]{
			definition: applypatch.Definition,
			arguments:  applypatch.NewArguments,
			prepare:    applypatch.Prepare,
			call:       patchinfra.Call,
		},
	}
	for _, candidate := range append(builtins, extensions...) {
		if err := registry.Register(candidate); err != nil {
			return nil, err
		}
	}
	return registry, nil
}
