package runtime

import "encoding/json"

func cloneRaw(value json.RawMessage) json.RawMessage {
	if value == nil {
		return nil
	}
	return append(json.RawMessage(nil), value...)
}

// Snapshot returns a defensive copy so adapters cannot mutate runtime state
// through the shared raw JSON buffer.
func (b TurnContentBlock) Snapshot() TurnContentBlock {
	b.Input = cloneRaw(b.Input)
	return b
}

// Snapshot returns a defensive copy of the message and all of its blocks.
func (m TurnMessage) Snapshot() TurnMessage {
	copied := m
	copied.Content = make([]TurnContentBlock, len(m.Content))
	for i, block := range m.Content {
		copied.Content[i] = block.Snapshot()
	}
	return copied
}

// CloneTurnMessages returns a defensive copy of a message slice.
func CloneTurnMessages(messages []TurnMessage) []TurnMessage {
	result := make([]TurnMessage, len(messages))
	for i, message := range messages {
		result[i] = message.Snapshot()
	}
	return result
}

// Snapshot returns a defensive copy of the tool definition.
func (d ToolDefinition) Snapshot() ToolDefinition {
	d.InputSchema = cloneRaw(d.InputSchema)
	return d
}

// CloneToolDefinitions returns a defensive copy of a tool definition slice.
func CloneToolDefinitions(definitions []ToolDefinition) []ToolDefinition {
	result := make([]ToolDefinition, len(definitions))
	for i, definition := range definitions {
		result[i] = definition.Snapshot()
	}
	return result
}

// Snapshot returns a defensive copy of the call and reconciles the Input and
// Arguments aliases so adapters can read either field interchangeably.
func (c ToolCall) Snapshot() ToolCall {
	if len(c.Input) == 0 && len(c.Arguments) > 0 {
		c.Input = c.Arguments
	}
	if len(c.Arguments) == 0 && len(c.Input) > 0 {
		c.Arguments = c.Input
	}
	c.Input = cloneRaw(c.Input)
	c.Arguments = cloneRaw(c.Arguments)
	return c
}

// Snapshot returns a defensive copy of normalized executor input.
func (c AuthorizedToolCall) Snapshot() AuthorizedToolCall {
	c.NormalizedArguments = cloneRaw(c.NormalizedArguments)
	c.ReadScopes = append([]string(nil), c.ReadScopes...)
	return c
}

// Snapshot returns a defensive copy of the execution turn input so provider code cannot
// mutate runtime state.
func (s ExecutionTurnSnapshot) Snapshot() ExecutionTurnSnapshot {
	copied := s
	copied.Messages = CloneTurnMessages(s.Messages)
	copied.ContextManifest = s.ContextManifest.Snapshot()
	copied.ContextSelection = s.ContextSelection.Snapshot()
	copied.Tools = CloneToolDefinitions(s.Tools)
	copied.Execution = s.Execution.Snapshot()
	return copied
}
