package runtimetest

import (
	"context"
	"sync"

	"praxis/internal/agentruntime"
	"praxis/internal/core/domain"
)

// ScriptedToolCall records a call and the immutable execution context observed by the faux executor.
type ScriptedToolCall struct {
	Call    agentruntime.ToolCall
	Context agentruntime.ToolExecutionContext
}

// ScriptedTool is a deterministic tool executor with optional per-call behavior.
type ScriptedTool struct {
	mu       sync.Mutex
	results  map[domain.ToolName]agentruntime.ToolExecutionResult
	errors   map[domain.ToolName]error
	calls    []ScriptedToolCall
	ExecuteF func(context.Context, agentruntime.ToolCall, agentruntime.ToolExecutionContext) (agentruntime.ToolExecutionResult, error)
}

// NewScriptedTool creates an executor with fixed successful results by tool name.
func NewScriptedTool(results map[domain.ToolName]agentruntime.ToolExecutionResult) *ScriptedTool {
	copy := make(map[domain.ToolName]agentruntime.ToolExecutionResult, len(results))
	for name, result := range results {
		copy[name] = result
	}
	return &ScriptedTool{results: copy, errors: make(map[domain.ToolName]error)}
}

// SetError configures a deterministic error for a tool name.
func (t *ScriptedTool) SetError(name domain.ToolName, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.errors == nil {
		t.errors = make(map[domain.ToolName]error)
	}
	t.errors[name] = err
}

// Calls returns all calls with defensive capability snapshots.
func (t *ScriptedTool) Calls() []ScriptedToolCall {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := make([]ScriptedToolCall, len(t.calls))
	for i, call := range t.calls {
		result[i] = call
		result[i].Context.Grant = call.Context.Grant.Snapshot()
		result[i].Call.Input = append([]byte(nil), call.Call.Input...)
	}
	return result
}

// Execute implements agentruntime.ToolExecutor.
func (t *ScriptedTool) Execute(ctx context.Context, call agentruntime.ToolCall, execCtx agentruntime.ToolExecutionContext) (agentruntime.ToolExecutionResult, error) {
	if t.ExecuteF != nil {
		return t.ExecuteF(ctx, call, execCtx)
	}
	t.mu.Lock()
	t.calls = append(t.calls, ScriptedToolCall{Call: call, Context: execCtx})
	err := t.errors[call.Name]
	result := t.results[call.Name]
	t.mu.Unlock()
	if err != nil {
		return result, err
	}
	return result, nil
}
