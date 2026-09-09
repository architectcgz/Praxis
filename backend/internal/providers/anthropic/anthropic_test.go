package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	domainsecurity "praxis/internal/domain/security"
	"praxis/internal/providers/streaming"
	runtimecontract "praxis/internal/runtime"
)

func TestProviderUsesExecutionTurnModelConfiguration(t *testing.T) {
	type requestBody struct {
		Model        string `json:"model"`
		MaxTokens    int    `json:"max_tokens"`
		OutputConfig struct {
			Effort string `json:"effort"`
		} `json:"output_config"`
	}
	requests := make(chan requestBody, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body requestBody
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		requests <- body
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	provider, err := New(Config{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := provider.Stream(context.Background(), runtimecontract.ModelRequest{Snapshot: runtimecontract.ExecutionTurnSnapshot{
		Model:           domainsecurity.ModelSelection{ModelID: "claude-test", Reasoning: "high"},
		MaxOutputTokens: 2048,
	}})
	if err != nil {
		t.Fatal(err)
	}
	for range stream {
	}
	select {
	case body := <-requests:
		if body.Model != "claude-test" || body.MaxTokens != 2048 || body.OutputConfig.Effort != "high" {
			t.Fatalf("unexpected request body: %#v", body)
		}
	default:
		t.Fatal("provider did not send a request")
	}
}

func TestMessagesDecoderCombinesToolArguments(t *testing.T) {
	input := `event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tool-1","name":"read_file"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"a\"}"}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"}}

`
	events := make([]runtimecontract.ModelStreamEvent, 0)
	reason, err := messagesDecoder{}.Decode(
		context.Background(),
		streaming.NewSSEReader(strings.NewReader(input)),
		func(event runtimecontract.ModelStreamEvent) error {
			events = append(events, event)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("decode messages stream: %v", err)
	}
	if reason != "tool_use" {
		t.Fatalf("stop reason = %q, want tool_use", reason)
	}
	if len(events) != 1 || events[0].ToolCall.ID != "tool-1" || events[0].ToolCall.Name != "read_file" ||
		string(events[0].ToolCall.Input) != `{"path":"a"}` {
		t.Fatalf("unexpected events: %#v", events)
	}
}
