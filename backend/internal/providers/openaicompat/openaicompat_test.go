package openaicompat

import (
	"encoding/json"
	"strings"
	"testing"

	runtimecontract "praxis/internal/runtime"
)

func TestRequestPayloadUsesOpenAIv1ResponsesPath(t *testing.T) {
	provider := &Provider{protocol: ProtocolResponses, model: "model"}

	_, path, err := provider.requestPayload(runtimecontract.TurnSnapshot{})
	if err != nil {
		t.Fatalf("build responses payload: %v", err)
	}
	if path != "/v1/responses" {
		t.Fatalf("responses path = %q, want /v1/responses", path)
	}
}

func TestRequestPayloadUsesOpenAIv1ChatCompletionsPath(t *testing.T) {
	provider := &Provider{protocol: ProtocolChatCompletions, model: "model"}

	payload, path, err := provider.requestPayload(runtimecontract.TurnSnapshot{})
	if err != nil {
		t.Fatalf("build chat completions payload: %v", err)
	}
	if path != "/v1/chat/completions" {
		t.Fatalf("chat completions path = %q, want /v1/chat/completions", path)
	}
	if !strings.Contains(string(payload), `"messages"`) {
		t.Fatalf("chat completions payload does not contain messages: %s", payload)
	}
}

func TestResponsesPayloadEncodesFunctionCallAndOutputItems(t *testing.T) {
	provider := &Provider{protocol: ProtocolResponses, model: "model"}
	snapshot := runtimecontract.TurnSnapshot{Messages: []runtimecontract.TurnMessage{
		{
			Role: runtimecontract.TurnRoleAssistant,
			Content: []runtimecontract.TurnContentBlock{
				{Kind: runtimecontract.TurnContentText, Text: "Checking."},
				{
					Kind: runtimecontract.TurnContentToolUse, ToolCallID: "call-1", ToolName: "list_dir",
					Input: json.RawMessage(`{"path":"."}`),
				},
			},
		},
		{
			Role: runtimecontract.TurnRoleTool,
			Content: []runtimecontract.TurnContentBlock{{
				Kind: runtimecontract.TurnContentToolResult, ToolCallID: "call-1", Text: `{"entries":[]}`,
			}},
		},
	}}
	payload, _, err := provider.requestPayload(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Input []map[string]any `json:"input"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Input) != 3 {
		t.Fatalf("responses input count=%d want=3: %s", len(body.Input), payload)
	}
	if body.Input[0]["type"] != "message" || body.Input[0]["role"] != "assistant" {
		t.Fatalf("unexpected assistant item: %#v", body.Input[0])
	}
	if body.Input[1]["type"] != "function_call" || body.Input[1]["call_id"] != "call-1" {
		t.Fatalf("unexpected function call item: %#v", body.Input[1])
	}
	if body.Input[2]["type"] != "function_call_output" || body.Input[2]["call_id"] != "call-1" {
		t.Fatalf("unexpected function output item: %#v", body.Input[2])
	}
	if strings.Contains(string(payload), `"role":"tool"`) || strings.Contains(string(payload), `"tool_call_id"`) {
		t.Fatalf("responses payload contains Chat Completions fields: %s", payload)
	}
}
