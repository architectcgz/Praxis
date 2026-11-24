package tools

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

// Discovery 保存本次 Run 已加载的工具；只有串行执行的 list_tools 可以更新它。
type Discovery struct {
	visible map[ToolName]Tool
}

// Visible 固定单次模型请求的工具集合。本轮新加载的工具不参与本轮执行。
func (d *Discovery) Visible() *Catalog { return NewCatalog(maps.Clone(d.visible)) }

func NewDiscovery(business ToolCatalog, collaboration []Tool) (*Discovery, ToolCatalog, error) {
	d := &Discovery{visible: make(map[ToolName]Tool)}
	var allowed ToolCatalog
	discovery := Function[listArguments]{
		Name:         listToolsName,
		Description:  "按需发现本次执行获准使用的工具。names 传空数组时列出名称和用途。传入所需工具名称后，从下一轮开始可直接调用，直到本次执行结束。delegatable 表示该工具可授权给子代理。",
		InputSchema:  []byte(`{"type":"object","properties":{"names":{"type":"array","items":{"type":"string"},"description":"要加载的工具名称，空数组用于查看工具列表"}},"required":["names"],"additionalProperties":false}`),
		SkipApproval: true,
		PrepareArguments: func(args listArguments) (listArguments, error) {
			if args.Names == nil {
				return args, errors.New("names must be an array")
			}
			return args, nil
		},
		Execute: func(_ context.Context, args listArguments, _ Environment) (ToolResult, error) {
			return d.list(business, allowed, args.Names)
		},
	}
	allowed, err := ComposeCatalog(business, append(collaboration, discovery)...)
	if err != nil {
		return nil, nil, err
	}
	d.visible[listToolsName] = discovery
	return d, allowed, nil
}

func (d *Discovery) list(business, allowed ToolCatalog, names []ToolName) (ToolResult, error) {
	selected := allowed
	if len(names) > 0 {
		loaded := make(map[ToolName]Tool, len(names))
		for _, name := range names {
			value, ok := allowed.Get(name)
			if !ok {
				return ToolResult{}, fmt.Errorf("tool %q is unavailable in this run", name)
			}
			loaded[name] = value
		}
		maps.Copy(d.visible, loaded)
		selected = NewCatalog(loaded)
	}
	definitions := selected.List()
	result := listResult{Tools: make([]listedTool, 0, len(definitions))}
	for _, definition := range definitions {
		if definition.Name == listToolsName {
			continue
		}
		_, delegatable := business.Get(definition.Name)
		result.Tools = append(result.Tools, listedTool{
			Name:        definition.Name,
			Description: definition.Description,
			Delegatable: delegatable,
		})
	}
	return JSONResult(result, nil)
}
