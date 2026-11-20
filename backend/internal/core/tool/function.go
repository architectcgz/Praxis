package tool

import (
	"bytes"
	"context"
	"encoding/json"
)

// Function 声明工具定义与类型化的执行函数，可直接注册。
type Function[Arguments any] struct {
	Name        ToolName
	Description string
	InputSchema json.RawMessage
	IsReadOnly  bool

	// DefaultArguments 和 PrepareArguments 分别用于填充业务默认值和准备已解码的参数，可省略。
	DefaultArguments func() Arguments
	PrepareArguments func(Arguments) (Arguments, error)
	Execute          func(context.Context, Arguments, Environment) (ToolResult, error)
}

func (t Function[Arguments]) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        t.Name,
		Description: t.Description,
		InputSchema: bytes.Clone(t.InputSchema),
	}
}

func (t Function[Arguments]) ReadOnly() bool { return t.IsReadOnly }

func (t Function[Arguments]) NewArguments() any {
	if t.DefaultArguments != nil {
		return new(t.DefaultArguments())
	}
	return new(Arguments)
}

func (t Function[Arguments]) Prepare(arguments any, env Environment) (PreparedCall, error) {
	args := *arguments.(*Arguments)
	if t.PrepareArguments != nil {
		var err error
		args, err = t.PrepareArguments(args)
		if err != nil {
			return PreparedCall{}, err
		}
	}
	return PreparedCall{
		Arguments: args,
		Call: func(ctx context.Context) (ToolResult, error) {
			return t.Execute(ctx, args, env)
		},
	}, nil
}

// JSONResult 将成功的结构化结果编码成工具回执，并传递执行错误。
func JSONResult(value any, err error) (ToolResult, error) {
	if err != nil {
		return ToolResult{}, err
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return ToolResult{}, err
	}
	return NewToolSuccess(string(payload), false), nil
}
