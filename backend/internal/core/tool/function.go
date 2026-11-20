package tool

import (
	"bytes"
	"context"
	"encoding/json"
)

// NewFunction 将类型化的参数校验与操作绑定为返回 JSON 的工具。
func NewFunction[Arguments, Result any](definition ToolDefinition, readOnly bool, validate func(Arguments) error, call func(context.Context, Arguments, Environment) (Result, error)) Tool {
	return functionTool[Arguments, Result]{
		definition: definition,
		readOnly:   readOnly,
		validate:   validate,
		call:       call,
	}
}

type functionTool[Arguments, Result any] struct {
	definition ToolDefinition
	readOnly   bool
	validate   func(Arguments) error
	call       func(context.Context, Arguments, Environment) (Result, error)
}

func (t functionTool[Arguments, Result]) Definition() ToolDefinition {
	definition := t.definition
	definition.InputSchema = bytes.Clone(definition.InputSchema)
	return definition
}

func (t functionTool[Arguments, Result]) ReadOnly() bool   { return t.readOnly }
func (t functionTool[Arguments, Result]) NewArguments() any { return new(Arguments) }

func (t functionTool[Arguments, Result]) Prepare(arguments any, env Environment) (PreparedCall, error) {
	args := *arguments.(*Arguments)
	if err := t.validate(args); err != nil {
		return PreparedCall{}, err
	}
	return PreparedCall{
		Arguments: args,
		Call: func(ctx context.Context) (ToolResult, error) {
			value, err := t.call(ctx, args, env)
			if err != nil {
				return ToolResult{}, err
			}
			payload, err := json.Marshal(value)
			if err != nil {
				return ToolResult{}, err
			}
			return NewToolSuccess(string(payload), false), nil
		},
	}, nil
}
