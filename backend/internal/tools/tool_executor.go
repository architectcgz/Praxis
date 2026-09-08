package tools

import (
	"context"
	"errors"

	domainsecurity "praxis/internal/domain/security"
	runtimecontract "praxis/internal/runtime"
	listdir "praxis/internal/tools/list_dir"
	readfile "praxis/internal/tools/read_file"
)

// Executor dispatches authorized calls to their tool-specific executor.
type Executor struct{}

// NewExecutor creates the tool executor used by the application service.
func NewExecutor() *Executor { return &Executor{} }

// Execute routes an authorized call to the executor registered for its tool name.
func (e *Executor) Execute(
	ctx context.Context,
	call runtimecontract.AuthorizedToolCall,
) (runtimecontract.ToolResult, error) {
	if ctx == nil {
		return runtimecontract.ToolResult{}, errors.New("tool execution context is required")
	}
	if err := ctx.Err(); err != nil {
		return runtimecontract.ToolResult{}, err
	}
	switch call.Name {
	case domainsecurity.ToolReadFile:
		return readfile.Execute(ctx, call)
	case domainsecurity.ToolListDir:
		return listdir.Execute(ctx, call)
	default:
		return runtimecontract.ToolResult{}, errors.New("tool executor does not support the call")
	}
}

var _ runtimecontract.ToolExecutor = (*Executor)(nil)
