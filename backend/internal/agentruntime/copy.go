package agentruntime

import "encoding/json"

func cloneRaw(value json.RawMessage) json.RawMessage {
	if value == nil {
		return nil
	}
	return append(json.RawMessage(nil), value...)
}

func cloneTurnContentBlock(block TurnContentBlock) TurnContentBlock {
	block.Input = cloneRaw(block.Input)
	return block
}

func cloneTurnMessage(message TurnMessage) TurnMessage {
	copy := message
	copy.Content = make([]TurnContentBlock, len(message.Content))
	for i, block := range message.Content {
		copy.Content[i] = cloneTurnContentBlock(block)
	}
	return copy
}

func cloneTurnMessages(messages []TurnMessage) []TurnMessage {
	result := make([]TurnMessage, len(messages))
	for i, message := range messages {
		result[i] = cloneTurnMessage(message)
	}
	return result
}

func cloneToolCall(call ToolCall) ToolCall {
	if len(call.Input) == 0 && len(call.Arguments) > 0 {
		call.Input = call.Arguments
	}
	if len(call.Arguments) == 0 && len(call.Input) > 0 {
		call.Arguments = call.Input
	}
	call.Input = cloneRaw(call.Input)
	call.Arguments = cloneRaw(call.Arguments)
	return call
}

func cloneToolDefinitions(definitions []ToolDefinition) []ToolDefinition {
	result := make([]ToolDefinition, len(definitions))
	for i, definition := range definitions {
		result[i] = definition
		result[i].InputSchema = cloneRaw(definition.InputSchema)
	}
	return result
}

func cloneRuntimeEvent(event RuntimeEvent) RuntimeEvent {
	event.Payload = cloneStringMap(event.Payload)
	return event
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	copy := make(map[string]string, len(values))
	for key, value := range values {
		copy[key] = value
	}
	return copy
}
