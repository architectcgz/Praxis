package openaicompat

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	domainsecurity "praxis/internal/domain/security"
	"praxis/internal/providers/streaming"
	runtimecontract "praxis/internal/runtime"
)

func testExecutionTurnSnapshot() runtimecontract.ExecutionTurnSnapshot {
	return runtimecontract.ExecutionTurnSnapshot{
		Model:           domainsecurity.ModelSelection{ModelID: "model", Reasoning: "high"},
		MaxOutputTokens: 1024,
	}
}

func TestRequestPayloadUsesOpenAIv1ResponsesPath(t *testing.T) {
	provider := &Provider{protocol: responsesProtocol{}}

	_, path, err := provider.requestPayload(testExecutionTurnSnapshot())
	if err != nil {
		t.Fatalf("build responses payload: %v", err)
	}
	if path != "/v1/responses" {
		t.Fatalf("responses path = %q, want /v1/responses", path)
	}
}

func TestRequestPayloadUsesOpenAIv1ChatCompletionsPath(t *testing.T) {
	provider := &Provider{protocol: chatCompletionsProtocol{}}

	payload, path, err := provider.requestPayload(testExecutionTurnSnapshot())
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

func TestRequestPayloadUsesExecutionTurnModelConfiguration(t *testing.T) {
	snapshot := testExecutionTurnSnapshot()
	tests := []struct {
		name             string
		provider         *Provider
		maxTokensField   string
		reasoningField   string
		expectedEndpoint string
	}{
		{
			name: "responses", provider: &Provider{protocol: responsesProtocol{}},
			maxTokensField: "max_output_tokens", reasoningField: "reasoning", expectedEndpoint: "/v1/responses",
		},
		{
			name: "chat completions", provider: &Provider{protocol: chatCompletionsProtocol{}},
			maxTokensField: "max_tokens", reasoningField: "reasoning_effort", expectedEndpoint: "/v1/chat/completions",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload, endpoint, err := test.provider.requestPayload(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if endpoint != test.expectedEndpoint {
				t.Fatalf("endpoint = %q, want %q", endpoint, test.expectedEndpoint)
			}
			var body map[string]any
			if err := json.Unmarshal(payload, &body); err != nil {
				t.Fatal(err)
			}
			if body["model"] != snapshot.Model.ModelID {
				t.Fatalf("model = %#v, want %q", body["model"], snapshot.Model.ModelID)
			}
			if body[test.maxTokensField] != float64(snapshot.MaxOutputTokens) {
				t.Fatalf("%s = %#v, want %d", test.maxTokensField, body[test.maxTokensField], snapshot.MaxOutputTokens)
			}
			if test.reasoningField == "reasoning_effort" {
				if body[test.reasoningField] != snapshot.Model.Reasoning {
					t.Fatalf("%s = %#v, want %q", test.reasoningField, body[test.reasoningField], snapshot.Model.Reasoning)
				}
				return
			}
			reasoning, ok := body[test.reasoningField].(map[string]any)
			if !ok || reasoning["effort"] != snapshot.Model.Reasoning {
				t.Fatalf("%s = %#v, want effort %q", test.reasoningField, body[test.reasoningField], snapshot.Model.Reasoning)
			}
		})
	}
}

func TestResponsesPayloadEncodesFunctionCallAndOutputItems(t *testing.T) {
	provider := &Provider{protocol: responsesProtocol{}}
	snapshot := testExecutionTurnSnapshot()
	snapshot.Messages = []runtimecontract.TurnMessage{
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
	}
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

func TestChatCompletionsDecoderCombinesToolArguments(t *testing.T) {
	reason, events, err := decodeEvents(t, chatCompletionsProtocol{}, `data: {"choices":[{"delta":{"content":"Checking. "}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","function":{"name":"read_file","arguments":"{\"path\":\""}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"a\"}"}}]},"finish_reason":"tool_calls"}]}

`)
	if err != nil {
		t.Fatalf("decode chat completions stream: %v", err)
	}
	if reason != "tool_calls" {
		t.Fatalf("stop reason = %q, want tool_calls", reason)
	}
	if len(events) != 2 || events[0].Text != "Checking. " || events[1].ToolCall.ID != "call-1" ||
		events[1].ToolCall.Name != "read_file" || string(events[1].ToolCall.Input) != `{"path":"a"}` {
		t.Fatalf("unexpected events: %#v", events)
	}
}

func TestResponsesDecoderCombinesToolArguments(t *testing.T) {
	reason, events, err := decodeEvents(t, responsesProtocol{}, `event: response.output_text.delta
data: {"type":"response.output_text.delta","delta":"Checking. "}

data: {"type":"response.output_item.added","item":{"type":"function_call","call_id":"call-1","name":"list_dir"}}

data: {"type":"response.function_call_arguments.delta","call_id":"call-1","delta":"{\"path\":\""}

data: {"type":"response.function_call_arguments.done","call_id":"call-1","arguments":"{\"path\":\".\"}"}

data: {"type":"response.completed","response":{"status":"completed"}}

`)
	if err != nil {
		t.Fatalf("decode responses stream: %v", err)
	}
	if reason != "completed" {
		t.Fatalf("stop reason = %q, want completed", reason)
	}
	if len(events) != 2 || events[0].Text != "Checking. " || events[1].ToolCall.ID != "call-1" ||
		events[1].ToolCall.Name != "list_dir" || string(events[1].ToolCall.Input) != `{"path":"."}` {
		t.Fatalf("unexpected events: %#v", events)
	}
}

func decodeEvents(t *testing.T, decoder streaming.Decoder, input string) (string, []runtimecontract.ModelStreamEvent, error) {
	t.Helper()
	events := make([]runtimecontract.ModelStreamEvent, 0)
	reason, err := decoder.Decode(
		context.Background(),
		streaming.NewSSEReader(strings.NewReader(input)),
		func(event runtimecontract.ModelStreamEvent) error {
			events = append(events, event)
			return nil
		},
	)
	return reason, events, err
}
