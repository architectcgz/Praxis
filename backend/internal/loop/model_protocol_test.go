package loop

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"praxis/internal/contracts"
	appcontext "praxis/internal/core/context"
	"praxis/internal/core/model"
	"praxis/internal/infra/providers/anthropicmessages"
	openaichat "praxis/internal/infra/providers/openai_chat"
	openairesponses "praxis/internal/infra/providers/openai_responses"
)

type protocolScenario struct {
	name        string
	tools       int
	thinking    bool
	terminal    string
	invalidTool bool
}

// 离线联调真实 Provider 的 HTTP 编码、SSE 解码、Loop 收集与下一轮上下文续传。
// 不执行实际工具或访问外部 API；非法终态必须在产生可执行调用前失败。
func TestProviderProtocolsDriveLoop(t *testing.T) {
	for _, protocol := range []string{"openai_chat", contracts.ProtocolOpenAIResponses, contracts.ProtocolAnthropicMessages} {
		for _, scenario := range []protocolScenario{
			{name: "纯文本"},
			{name: "单工具", tools: 1},
			{name: "并行工具", tools: 2},
			{name: "截断", terminal: "truncated"},
			{name: "拒绝", terminal: "refused"},
			{name: "无效并行工具", tools: 2, invalidTool: true},
			{name: "Thinking续传", tools: 1, thinking: true},
		} {
			if protocol == "openai_chat" && scenario.thinking {
				continue
			}
			t.Run(protocol+"/"+scenario.name, func(t *testing.T) {
				fixtures := make(chan string, 2)
				fixtures <- loopProtocolFixture(t, protocol, scenario)
				fixtures <- loopProtocolFixture(t, protocol, protocolScenario{})
				bodies := make(chan string, 2)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, err := io.ReadAll(r.Body)
					if err != nil {
						http.Error(w, "读取请求失败", http.StatusBadRequest)
						return
					}
					bodies <- string(body)
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, <-fixtures)
				}))
				defer server.Close()
				var provider model.ModelStream
				var err error
				switch protocol {
				case "openai_chat":
					provider, err = openaichat.New(openaichat.Config{BaseURL: server.URL, APIKey: "test", HTTPClient: server.Client()})
				case contracts.ProtocolOpenAIResponses:
					provider, err = openairesponses.New(openairesponses.Config{BaseURL: server.URL, APIKey: "test", HTTPClient: server.Client()})
				case contracts.ProtocolAnthropicMessages:
					provider, err = anthropicmessages.New(anthropicmessages.Config{BaseURL: server.URL, APIKey: "test", HTTPClient: server.Client()})
				}
				if err != nil {
					t.Fatal(err)
				}
				request := model.ModelRequest{
					Model:           model.ModelSnapshot{ModelID: "test-model", APIFormat: protocol},
					MaxOutputTokens: 100,
					Context: appcontext.ModelContext{Entries: []appcontext.ContextEntry{{
						Kind:    appcontext.ContextEntryUserInput,
						Role:    appcontext.ContextRoleUser,
						Content: []appcontext.ContextBlock{{Kind: appcontext.ContextBlockText, Text: "你好"}},
					}}},
				}
				observedTools := 0
				response, err := requestModel(t.Context(), provider, request, func(event model.ModelStreamEvent) {
					if event.Kind == model.StreamToolCall {
						observedTools++
					}
					if event.ReasoningState != nil || strings.Contains(event.Text, "secret") {
						t.Error("观察者收到了协议秘密")
					}
				})
				<-bodies
				if scenario.terminal != "" || scenario.invalidTool {
					if err == nil || observedTools != 0 || len(response.toolCalls()) != 0 {
						t.Fatalf("非法终态释放了工具：%v %d", err, observedTools)
					}
					return
				}
				if err != nil || len(response.toolCalls()) != scenario.tools || observedTools != scenario.tools {
					t.Fatalf("Loop 语义错误：%v %d", err, observedTools)
				}
				expected := model.StopCompleted
				if scenario.tools > 0 {
					expected = model.StopToolCalls
				}
				if response.stopReason != expected || response.textBytes != len("你好") {
					t.Fatalf("终态或文本计数不一致：%s %d", response.stopReason, response.textBytes)
				}
				blocks := model.ContextBlocksFromEvents(response.events)
				request.Context = request.Context.AppendProviderOutput(blocks)
				for _, call := range response.toolCalls() {
					request.Context = request.Context.AppendToolResult(appcontext.NewToolResultBlock(call.ID, string(call.Name), "结果", false))
				}
				if _, err := requestModel(t.Context(), provider, request, nil); err != nil {
					t.Fatalf("下一轮请求失败：%v", err)
				}
				body := <-bodies
				if scenario.thinking {
					stateCount := 0
					for _, block := range request.Context.Entries[1].Content {
						if block.Kind != appcontext.ContextBlockReasoningState {
							continue
						}
						stateCount++
						if block.ReasoningState.Protocol != protocol || block.ReasoningState.Version != contracts.ReasoningStateVersion || !strings.Contains(body, "final_secret") {
							t.Fatal("推理状态未按来源协议续传")
						}
					}
					if stateCount != 1 || strings.Count(body, "final_secret") != 1 || strings.Contains(body, "partial_secret") {
						t.Fatal("推理状态丢失、重复或使用了未完成载荷")
					}
				}
			})
		}
	}
}

func loopProtocolFixture(t *testing.T, protocol string, scenario protocolScenario) string {
	t.Helper()
	var sse strings.Builder
	sequence := 0
	appendFrame := func(kind string, fields map[string]any) {
		if kind != "" {
			fields["type"] = kind
		}
		if protocol == contracts.ProtocolOpenAIResponses {
			fields["sequence_number"] = sequence
			sequence++
		}
		data, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		if kind != "" {
			fmt.Fprintf(&sse, "event: %s\n", kind)
		}
		fmt.Fprintf(&sse, "data: %s\n\n", data)
	}
	arguments := func(index int) string {
		if scenario.invalidTool && index == scenario.tools-1 {
			return "[]"
		}
		return "{}"
	}
	switch protocol {
	case "openai_chat":
		chunk := func(choices []any, usage any) map[string]any {
			return map[string]any{
				"id":      "chatcmpl_1",
				"object":  "chat.completion.chunk",
				"created": 1,
				"model":   "test-model",
				"choices": choices,
				"usage":   usage,
			}
		}
		delta := map[string]any{"content": "你好"}
		if scenario.terminal == "refused" {
			delta = map[string]any{"refusal": "你好"}
		}
		var calls []any
		for index := range scenario.tools {
			calls = append(calls, map[string]any{
				"index":    index,
				"id":       fmt.Sprintf("call_%d", index),
				"type":     "function",
				"function": map[string]any{"name": "lookup", "arguments": arguments(index)},
			})
		}
		if len(calls) > 0 {
			delta["tool_calls"] = calls
		}
		finish := "stop"
		if scenario.tools > 0 {
			finish = "tool_calls"
		}
		if scenario.terminal == "truncated" {
			finish = "length"
		}
		appendFrame("", chunk([]any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}}, nil))
		appendFrame("", chunk([]any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finish}}, nil))
		appendFrame("", chunk([]any{}, map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}))
		sse.WriteString("data: [DONE]\n\n")
	case contracts.ProtocolOpenAIResponses:
		snapshot := func(status string, output []any) map[string]any {
			return map[string]any{
				"id":                  "resp_1",
				"object":              "response",
				"created_at":          1,
				"model":               "test-model",
				"status":              status,
				"output":              output,
				"parallel_tool_calls": true,
				"tool_choice":         "auto",
				"tools":               []any{},
			}
		}
		appendFrame("response.created", map[string]any{"response": snapshot("in_progress", []any{})})
		outputs := []any{}
		appendItem := func(initial, final map[string]any) {
			index := len(outputs)
			appendFrame("response.output_item.added", map[string]any{"output_index": index, "item": initial})
			appendFrame("response.output_item.done", map[string]any{"output_index": index, "item": final})
			outputs = append(outputs, final)
		}
		if scenario.thinking {
			appendItem(map[string]any{
				"id":                "rs_1",
				"type":              "reasoning",
				"summary":           []any{},
				"encrypted_content": "partial_secret",
			}, map[string]any{
				"id":                "rs_1",
				"type":              "reasoning",
				"summary":           []any{map[string]any{"type": "summary_text", "text": "思考"}},
				"encrypted_content": "final_secret",
			})
		}
		part := map[string]any{"type": "output_text", "text": "你好", "annotations": []any{}}
		if scenario.terminal == "refused" {
			part = map[string]any{"type": "refusal", "refusal": "你好"}
		}
		message := func(status string, parts []any) map[string]any {
			return map[string]any{
				"id":      "msg_1",
				"type":    "message",
				"role":    "assistant",
				"status":  status,
				"content": parts,
			}
		}
		status := "completed"
		if scenario.terminal == "truncated" {
			status = "incomplete"
		}
		appendItem(message("in_progress", []any{}), message(status, []any{part}))
		for index := range scenario.tools {
			call := func(args string) map[string]any {
				return map[string]any{
					"id":        fmt.Sprintf("fc_%d", index),
					"type":      "function_call",
					"call_id":   fmt.Sprintf("call_%d", index),
					"name":      "lookup",
					"arguments": args,
				}
			}
			appendItem(call(""), call(arguments(index)))
		}
		terminal := snapshot(status, outputs)
		if status == "incomplete" {
			terminal["incomplete_details"] = map[string]any{"reason": "max_output_tokens"}
		}
		appendFrame("response."+status, map[string]any{"response": terminal})
	case contracts.ProtocolAnthropicMessages:
		appendFrame("message_start", map[string]any{"message": map[string]any{
			"id":            "msg_1",
			"type":          "message",
			"model":         "test-model",
			"role":          "assistant",
			"content":       []any{},
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage":         map[string]any{"input_tokens": 1, "output_tokens": 0},
		}})
		blockIndex := 0
		if scenario.thinking {
			appendFrame("content_block_start", map[string]any{"index": blockIndex, "content_block": map[string]any{"type": "thinking", "thinking": "", "signature": ""}})
			appendFrame("content_block_delta", map[string]any{"index": blockIndex, "delta": map[string]any{"type": "thinking_delta", "thinking": "思考"}})
			appendFrame("content_block_delta", map[string]any{"index": blockIndex, "delta": map[string]any{"type": "signature_delta", "signature": "final_secret"}})
			appendFrame("content_block_stop", map[string]any{"index": blockIndex})
			blockIndex++
		}
		appendFrame("content_block_start", map[string]any{"index": blockIndex, "content_block": map[string]any{"type": "text", "text": "你好"}})
		appendFrame("content_block_stop", map[string]any{"index": blockIndex})
		blockIndex++
		for index := range scenario.tools {
			appendFrame("content_block_start", map[string]any{"index": blockIndex, "content_block": map[string]any{
				"type":  "tool_use",
				"id":    fmt.Sprintf("call_%d", index),
				"name":  "lookup",
				"input": map[string]any{},
			}})
			appendFrame("content_block_delta", map[string]any{"index": blockIndex, "delta": map[string]any{"type": "input_json_delta", "partial_json": arguments(index)}})
			appendFrame("content_block_stop", map[string]any{"index": blockIndex})
			blockIndex++
		}
		stop := "end_turn"
		if scenario.tools > 0 {
			stop = "tool_use"
		}
		if scenario.terminal == "truncated" {
			stop = "max_tokens"
		}
		if scenario.terminal == "refused" {
			stop = "refusal"
		}
		appendFrame("message_delta", map[string]any{"delta": map[string]any{"stop_reason": stop, "stop_sequence": nil}, "usage": map[string]any{"output_tokens": 1}})
		appendFrame("message_stop", map[string]any{})
	}
	return sse.String()
}
