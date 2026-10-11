package loop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	agentruntime "praxis/internal/agent_runtime"
	"praxis/internal/contracts"
	appcontext "praxis/internal/core/context"
	"praxis/internal/core/model"
	taskmodel "praxis/internal/core/task"
	turnmodel "praxis/internal/core/turn"
	"praxis/internal/infra/providers/anthropicmessages"
	openaichat "praxis/internal/infra/providers/openai_chat"
	openairesponses "praxis/internal/infra/providers/openai_responses"
	toolcontracts "praxis/internal/tools/contracts"
)

type protocolScenario struct {
	name         string
	tools        int
	thinking     bool
	terminal     string
	invalidTool  bool
	usageMode    string
	summaryOnly  bool
	thinkingMode string
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
			{name: "无Usage工具流", tools: 1, usageMode: "missing"},
			{name: "无效Usage工具流", tools: 1, usageMode: "invalid"},
			{name: "仅推理摘要", thinking: true, summaryOnly: true},
			{
				name:        "推理摘要与工具",
				tools:       1,
				thinking:    true,
				summaryOnly: true,
			},
			{name: "自定义停止序列", terminal: "sequence"},
			{name: "停止序列保留Thinking", thinking: true, terminal: "sequence"},
			{name: "Adaptive无Thinking工具续传", tools: 1, thinkingMode: "adaptive"},
			{
				name:         "Enabled工具续传",
				tools:        1,
				thinking:     true,
				thinkingMode: "enabled",
			},
		} {
			if protocol == "openai_chat" && scenario.thinking || scenario.summaryOnly && protocol != contracts.ProtocolOpenAIResponses || (scenario.terminal == "sequence" || scenario.thinkingMode != "") && protocol != contracts.ProtocolAnthropicMessages {
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
				if scenario.thinkingMode != "" {
					request.Model.Thinking.Mode = scenario.thinkingMode
					if scenario.thinkingMode == "enabled" {
						request.Model.Thinking.BudgetTokens = 1024
						request.MaxOutputTokens = 2048
					}
				}
				observedTools, observedUsage := 0, 0
				response, err := requestModel(t.Context(), provider, request, func(event model.ModelStreamEvent) {
					if event.Kind == model.StreamToolCall {
						observedTools++
					}
					if event.Kind == model.StreamUsage {
						observedUsage++
					}
					if event.ReasoningState != nil || strings.Contains(event.Text, "secret") {
						t.Error("观察者收到了协议秘密")
					}
				})
				<-bodies
				if scenario.terminal != "" && scenario.terminal != "sequence" || scenario.invalidTool {
					if err == nil || observedTools != 0 || len(response.toolCalls()) != 0 {
						t.Fatalf("非法终态释放了工具：%v %d", err, observedTools)
					}
					return
				}
				if err != nil || len(response.toolCalls()) != scenario.tools || observedTools != scenario.tools {
					t.Fatalf("Loop 语义错误：%v %d", err, observedTools)
				}
				if scenario.usageMode != "" && observedUsage != 0 {
					t.Fatal("缺失或无效 Usage 不应进入统计")
				}
				expected := model.StopCompleted
				if scenario.terminal == "sequence" {
					expected = model.StopSequence
				}
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
				if scenario.summaryOnly {
					for _, block := range request.Context.Entries[1].Content {
						if block.Kind == appcontext.ContextBlockReasoningState {
							t.Fatal("仅展示的推理被保存为可重放状态")
						}
					}
				} else if scenario.thinking {
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
		if scenario.usageMode == "invalid" {
			appendFrame("", chunk([]any{}, map[string]any{"prompt_tokens": 1}))
		} else if scenario.usageMode != "missing" {
			appendFrame("", chunk([]any{}, map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}))
		}
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
			if scenario.summaryOnly && final["type"] == "reasoning" {
				delete(initial, "encrypted_content")
				delete(final, "encrypted_content")
			}
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
		if scenario.usageMode == "invalid" {
			terminal["usage"] = map[string]any{"input_tokens": 1}
		}
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
			"usage": func() any {
				if scenario.usageMode != "" {
					return nil
				}
				return map[string]any{"input_tokens": 1, "output_tokens": 0}
			}(),
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
		var stopSequence any
		if scenario.terminal == "sequence" {
			stop, stopSequence = "stop_sequence", "END"
		}
		var usage any = map[string]any{"output_tokens": 1}
		if scenario.usageMode == "missing" {
			usage = nil
		} else if scenario.usageMode == "invalid" {
			usage = map[string]any{}
		}
		appendFrame("message_delta", map[string]any{"delta": map[string]any{"stop_reason": stop, "stop_sequence": stopSequence}, "usage": usage})
		appendFrame("message_stop", map[string]any{})
	}
	return sse.String()
}

func TestLoopReasoningStateContract(t *testing.T) {
	state := &contracts.ReasoningState{
		Protocol: contracts.ProtocolAnthropicMessages,
		Version:  contracts.ReasoningStateVersion,
		Payload:  json.RawMessage(`{"type":"redacted_thinking","data":"opaque"}`),
	}
	zero, one := 0, 1
	for _, indices := range [][]*int{{nil, nil}, {&zero, &zero}, {&zero, &one}} {
		stream := make(chan model.ModelStreamEvent, 3)
		for _, index := range indices {
			stream <- model.ModelStreamEvent{Kind: model.StreamReasoningState, ReasoningState: state, OutputIndex: index}
		}
		stream <- model.ModelStreamEvent{Kind: model.StreamComplete, StopReason: model.StopCompleted}
		close(stream)
		response, err := collectModelResponse(t.Context(), stream, func(event model.ModelStreamEvent) {
			if event.Kind == model.StreamReasoningState {
				t.Fatal("推理状态不应发送给观察者")
			}
		})
		if err != nil || len(response.events) != 2 || response.validateReasoningState(state.Protocol) != nil {
			t.Fatalf("Loop 不应根据载荷内容或索引去重：%v", err)
		}
		if response.validateReasoningState(contracts.ProtocolOpenAIResponses) == nil {
			t.Fatal("跨协议推理状态未被拒绝")
		}
	}
	invalid := state.Clone()
	invalid.Version = 1
	stream := make(chan model.ModelStreamEvent, 1)
	stream <- model.ModelStreamEvent{Kind: model.StreamReasoningState, ReasoningState: invalid}
	close(stream)
	if _, err := collectModelResponse(t.Context(), stream, nil); err == nil {
		t.Fatal("内部推理状态的版本校验被移除")
	}
}

// stopSequenceFixture 只允许一次无工具请求；未实现的接口方法一旦被调用，测试立即失败。
type stopSequenceFixture struct {
	ToolCallHandler
	toolcontracts.ToolCatalog
	agentruntime.MessageRecorder
	requests int
	ends     int
	outcome  turnmodel.TurnOutcome
}

func (f *stopSequenceFixture) BuildModel(model.ModelSnapshot) (model.Model, error) {
	return model.Model{Stream: f, MaxOutputTokens: 100}, nil
}
func (f *stopSequenceFixture) Stream(context.Context, model.ModelRequest) (<-chan model.ModelStreamEvent, error) {
	f.requests++
	if f.requests > 1 {
		return nil, errors.New("stop_sequence 后不应再次请求模型")
	}
	stream := make(chan model.ModelStreamEvent, 1)
	stream <- model.ModelStreamEvent{Kind: model.StreamComplete, StopReason: model.StopSequence}
	close(stream)
	return stream, nil
}
func (f *stopSequenceFixture) List() []contracts.ToolDefinition { return nil }
func (f *stopSequenceFixture) RecordStart(context.Context, contracts.TurnExecutionContext) (turnmodel.Turn, error) {
	return turnmodel.Turn{ID: "turn_1"}, nil
}
func (f *stopSequenceFixture) RecordEnd(_ context.Context, _ contracts.TurnID, outcome turnmodel.TurnOutcome, _ contracts.TaskFailureCode, _ string) error {
	f.ends++
	f.outcome = outcome
	return nil
}

func TestRunnerCompletesAtStopSequence(t *testing.T) {
	fixture := &stopSequenceFixture{}
	runner, err := NewRunner(Config{
		ModelBuilder:  fixture,
		Tools:         fixture,
		ToolCalls:     fixture,
		Turns:         fixture,
		EventObserver: func(agentruntime.AgentEvent) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	task := taskmodel.Task{Input: taskmodel.InputSnapshot{
		Model: model.ModelSnapshot{ContextWindow: 4096},
		Context: appcontext.ModelContext{Entries: []appcontext.ContextEntry{{
			Kind: appcontext.ContextEntryUserInput, Role: appcontext.ContextRoleUser,
			Content: []appcontext.ContextBlock{{Kind: appcontext.ContextBlockText, Text: "你好"}},
		}}},
	}}
	outcome, code, err := runner.Run(t.Context(), task, fixture)
	if err != nil || code != "" || outcome != taskmodel.TaskCompleted || fixture.requests != 1 || fixture.ends != 1 || fixture.outcome != turnmodel.TurnCompleted {
		t.Fatalf("停止序列未正常结算：%s %s %v %+v", outcome, code, err, fixture)
	}
}
