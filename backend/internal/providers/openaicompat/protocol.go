package openaicompat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	domainsecurity "praxis/internal/domain/security"
	"praxis/internal/providers/streaming"
	runtimecontract "praxis/internal/runtime"
)

type protocolAdapter interface {
	requestPayload(runtimecontract.ExecutionTurnSnapshot) ([]byte, string, error)
	Decode(context.Context, *streaming.SSEReader, streaming.EmitFunc) (string, error)
}

func protocolFor(protocol Protocol) (protocolAdapter, error) {
	switch protocol {
	case ProtocolChatCompletions:
		return chatCompletionsProtocol{}, nil
	case ProtocolResponses:
		return responsesProtocol{}, nil
	default:
		return nil, errors.New("unsupported openai-compatible protocol")
	}
}

type toolAccumulator struct{ id, name, arguments string }

func emitTool(emit streaming.EmitFunc, call *toolAccumulator) error {
	if call == nil || call.name == "" {
		return nil
	}
	args := json.RawMessage(call.arguments)
	if len(bytes.TrimSpace(args)) == 0 {
		args = json.RawMessage(`{}`)
	}
	return emit(runtimecontract.ModelStreamEvent{
		Kind: runtimecontract.StreamToolCall,
		ToolCall: runtimecontract.ToolCall{
			ID:        call.id,
			Name:      domainsecurity.ToolName(call.name),
			Input:     append(json.RawMessage(nil), args...),
			Arguments: append(json.RawMessage(nil), args...),
		},
	})
}
