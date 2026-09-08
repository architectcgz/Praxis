package tools

import (
	"errors"

	domainsecurity "praxis/internal/domain/security"
	runtimecontract "praxis/internal/runtime"
	listdir "praxis/internal/tools/list_dir"
	readfile "praxis/internal/tools/read_file"
)

type toolNormalizer func(
	runtimecontract.ToolCall,
	runtimecontract.ToolInvocationContext,
) (runtimecontract.AuthorizedToolCall, error)

type registeredTool struct {
	definition runtimecontract.ToolDefinition
	normalize  toolNormalizer
}

// ToolRegistry owns the registered tools and dispatches their input normalization.
type ToolRegistry struct {
	tools map[domainsecurity.ToolName]registeredTool
}

// NewToolRegistry creates the registry with all tools supported by this package.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: map[domainsecurity.ToolName]registeredTool{
			domainsecurity.ToolReadFile: {
				definition: readfile.Definition(),
				normalize:  readfile.Normalize,
			},
			domainsecurity.ToolListDir: {
				definition: listdir.Definition(),
				normalize:  listdir.Normalize,
			},
		},
	}
}

// Definition returns a defensive copy of the model-visible tool definition.
func (r *ToolRegistry) Definition(name domainsecurity.ToolName) (runtimecontract.ToolDefinition, bool) {
	tool, ok := r.tools[name]
	if !ok {
		return runtimecontract.ToolDefinition{}, false
	}
	return tool.definition.Snapshot(), true
}

// Normalize dispatches a provider tool call to the registered tool normalizer.
func (r *ToolRegistry) Normalize(
	call runtimecontract.ToolCall,
	invocation runtimecontract.ToolInvocationContext,
) (runtimecontract.AuthorizedToolCall, error) {
	tool, ok := r.tools[call.Name]
	if !ok || tool.normalize == nil {
		return runtimecontract.AuthorizedToolCall{}, errors.New("tool is not registered")
	}
	return tool.normalize(call, invocation)
}

var _ runtimecontract.ToolCatalog = (*ToolRegistry)(nil)
