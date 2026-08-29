package agentruntime

import (
	"encoding/json"

	coreruntime "praxis/internal/core/runtime"
)

func cloneRaw(value json.RawMessage) json.RawMessage {
	if value == nil {
		return nil
	}
	return append(json.RawMessage(nil), value...)
}

// The helpers below are local shorthands over the copy semantics that
// core/runtime defines for its own DTOs.
func cloneTurnContentBlock(block TurnContentBlock) TurnContentBlock {
	return block.Snapshot()
}

func cloneTurnMessage(message TurnMessage) TurnMessage {
	return message.Snapshot()
}

func cloneTurnMessages(messages []TurnMessage) []TurnMessage {
	return coreruntime.CloneTurnMessages(messages)
}

func cloneToolCall(call ToolCall) ToolCall {
	return call.Snapshot()
}

func cloneToolDefinitions(definitions []ToolDefinition) []ToolDefinition {
	return coreruntime.CloneToolDefinitions(definitions)
}

func cloneRuntimeEvent(event RuntimeEvent) RuntimeEvent {
	event.Payload = cloneStringMap(event.Payload)
	return event
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	copied := make(map[string]string, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}
