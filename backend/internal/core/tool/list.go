package tool

import (
	"context"
	"errors"
	"fmt"
	"maps"
)

const listToolsName ToolName = "list_tools"

type listArguments struct {
	Names []ToolName `json:"names"`
}

type listedTool struct {
	Name        ToolName `json:"name"`
	Description string   `json:"description"`
	Delegatable bool     `json:"delegatable"`
}

type listResult struct {
	Tools []listedTool `json:"tools"`
}

// RegisterListTools 为本次 Run 注册工具发现入口并初始化模型可见的工具。
func (r *ToolRegistry) RegisterListTools() (*ToolRegistry, error) {
	visible := NewToolRegistry()
	discovery := Function[listArguments]{
		Name:        listToolsName,
		Description: "按需发现本次执行获准使用的工具。names 传空数组时列出名称和用途。传入所需工具名称后，从下一轮开始可按工具定义直接调用，直到本次执行结束。已知工具名时可直接传入。delegatable 表示该工具可在委派时授权给子代理。",
		InputSchema: []byte(`{"type":"object","properties":{"names":{"type":"array","items":{"type":"string"},"description":"要加载的工具名称，空数组用于查看工具列表"}},"required":["names"],"additionalProperties":false}`),
		PrepareArguments: func(args listArguments) (listArguments, error) {
			if args.Names == nil {
				return args, errors.New("names must be an array")
			}
			return args, nil
		},
		Execute: func(_ context.Context, args listArguments, _ Environment) (ToolResult, error) {
			selected := r
			if len(args.Names) > 0 {
				selected = NewToolRegistry()
				for _, name := range args.Names {
					registration, ok := r.Get(name)
					if !ok {
						return ToolResult{}, fmt.Errorf("tool %q is unavailable in this run", name)
					}
					selected.tools[name] = registration
				}
				maps.Copy(visible.tools, selected.tools)
			}
			definitions := selected.List()
			tools := make([]listedTool, 0, len(definitions))
			for _, definition := range definitions {
				if definition.Name == listToolsName {
					continue
				}
				tools = append(tools, listedTool{
					Name:        definition.Name,
					Description: definition.Description,
					Delegatable: !selected.tools[definition.Name].RunScoped,
				})
			}
			return JSONResult(listResult{Tools: tools}, nil)
		},
	}
	if err := r.RegisterRunTool(discovery); err != nil {
		return nil, err
	}
	visible.tools[listToolsName] = r.tools[listToolsName]
	return visible, nil
}
