package runtime

import toolcontracts "praxis/internal/tools/contracts"

// CloneToolDefinitions returns a defensive copy of a tool definition slice.
func CloneToolDefinitions(definitions []toolcontracts.ToolDefinition) []toolcontracts.ToolDefinition {
	result := make([]toolcontracts.ToolDefinition, len(definitions))
	for i, definition := range definitions {
		result[i] = definition.Snapshot()
	}
	return result
}

// Clone 返回 Provider 请求的独立副本，避免 adapter 修改 runtime 状态。
func (r ModelRequest) Clone() ModelRequest {
	copied := r
	copied.Context = r.Context.Clone()
	copied.Tools = CloneToolDefinitions(r.Tools)
	copied.Execution = r.Execution.Snapshot()
	return copied
}
