package openaichat

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	appcontext "praxis/internal/core/context"
	"praxis/internal/core/model"
	"praxis/internal/infra/providers/streaming"
)

func chatFixtureChunk(delta map[string]any, finish any) map[string]any {
	return map[string]any{
		"id":      "chatcmpl_1",
		"object":  "chat.completion.chunk",
		"created": 0,
		"model":   "gpt-5",
		"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}},
		"usage":   nil,
	}
}
func chatFixtureUsage() map[string]any {
	chunk := chatFixtureChunk(nil, nil)
	chunk["choices"] = []any{}
	chunk["usage"] = map[string]any{
		"prompt_tokens":         10,
		"completion_tokens":     3,
		"total_tokens":          13,
		"prompt_tokens_details": map[string]any{"cached_tokens": 4},
	}
	return chunk
}
func chatFixtureTextStream() []map[string]any {
	return []map[string]any{
		chatFixtureChunk(map[string]any{"role": "assistant", "content": nil, "refusal": nil}, nil),
		chatFixtureChunk(map[string]any{"content": "你好", "reasoning_content": "非官方字段"}, nil),
		chatFixtureChunk(map[string]any{}, "stop"),
		chatFixtureUsage(),
	}
}
func decodeChatFixture(t *testing.T, frames []map[string]any, done bool) ([]model.ModelStreamEvent, model.ModelStopReason, error) {
	t.Helper()
	var sse strings.Builder
	for _, frame := range frames {
		data, err := json.Marshal(frame)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&sse, "data: %s\n\n", data)
	}
	if done {
		sse.WriteString("data: [DONE]\n\n")
	}
	var events []model.ModelStreamEvent
	reason, err := (decoder{}).Decode(t.Context(), streaming.NewSSEReader(strings.NewReader(sse.String())), func(event model.ModelStreamEvent) error { events = append(events, event); return nil })
	return events, reason, err
}
func TestChatChunkAndText(t *testing.T) {
	events, reason, err := decodeChatFixture(t, chatFixtureTextStream(), true)
	if err != nil || reason != model.StopCompleted || len(events) != 2 || events[0].Kind != model.StreamTextDelta || events[0].Text != "你好" || events[1].Kind != model.StreamUsage || *events[1].Usage.CacheReadInputTokens != 4 {
		t.Fatalf("Chunk 语义错误：%v %s %v", events, reason, err)
	}
	for _, name := range []string{
		"id",
		"object",
		"created",
		"model",
		"choices",
	} {
		for _, null := range []bool{false, true} {
			chunk := chatFixtureTextStream()[0]
			if null {
				chunk[name] = nil
			} else {
				delete(chunk, name)
			}
			data, _ := json.Marshal(chunk)
			if _, err := decodeChunk(data); err == nil {
				t.Fatalf("未拒绝缺失或 null 必填字段 %s", name)
			}
		}
	}
	for _, delta := range []string{
		`{"tool_calls":[{"index":0,"id":"call_1"}]}`,
		`{"tool_calls":[{"index":0,"type":"function"}]}`,
		`{"tool_calls":[{"index":0,"function":{"name":"lookup"}}]}`,
		`{"tool_calls":[{"index":0,"function":{"arguments":"{"}}]}`,
	} {
		var parsed chatCompletionDelta
		if err := json.Unmarshal([]byte(delta), &parsed); err != nil {
			t.Fatalf("合法稀疏工具分片被拒绝：%v", err)
		}
	}
}
func chatFixtureTools() []map[string]any {
	return []map[string]any{
		chatFixtureChunk(map[string]any{"tool_calls": []any{
			map[string]any{"index": 0, "function": map[string]any{"name": "look", "arguments": "{"}},
			map[string]any{
				"index":    1,
				"id":       "call_2",
				"type":     "function",
				"function": map[string]any{"name": "lookup", "arguments": "{"},
			},
		}}, nil),
		chatFixtureChunk(map[string]any{"tool_calls": []any{
			map[string]any{
				"index":    0,
				"id":       "call_1",
				"type":     "function",
				"function": map[string]any{"name": "up", "arguments": "}"},
			},
			map[string]any{"index": 1, "function": map[string]any{"arguments": "}"}},
		}}, nil),
		chatFixtureChunk(map[string]any{}, "tool_calls"),
		chatFixtureUsage(),
	}
}
func TestChatParallelToolsAndTerminalGuard(t *testing.T) {
	events, reason, err := decodeChatFixture(t, chatFixtureTools(), true)
	if err != nil || reason != model.StopToolCalls {
		t.Fatalf("工具流失败：%s %v", reason, err)
	}
	calls := 0
	for _, event := range events {
		if event.Kind == model.StreamToolCall {
			calls++
			if string(event.ToolCall.Arguments) != "{}" || event.ToolCall.Name != "lookup" || event.ToolCall.ID != fmt.Sprintf("call_%d", calls) {
				t.Fatal("工具分片累积错误")
			}
		}
	}
	if calls != 2 {
		t.Fatalf("工具数量错误：%d", calls)
	}
	for _, frames := range [][]map[string]any{chatFixtureTools(), chatFixtureTools()[:3]} {
		events, _, err := decodeChatFixture(t, frames, false)
		if err == nil {
			t.Fatal("未拒绝中断流")
		}
		for _, event := range events {
			if event.Kind == model.StreamToolCall {
				t.Fatal("[DONE] 前释放了工具")
			}
		}
	}
	frames := chatFixtureTools()
	frames[1]["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)["tool_calls"].([]any)[1].(map[string]any)["function"] = map[string]any{"arguments": "bad}"}
	events, _, err = decodeChatFixture(t, frames, true)
	if err == nil {
		t.Fatal("未拒绝无效 JSON 参数")
	}
	for _, event := range events {
		if event.Kind == model.StreamToolCall {
			t.Fatal("第二个工具无效时释放了第一个工具")
		}
	}
}
func TestChatInvalidStreams(t *testing.T) {
	cases := []struct {
		name   string
		change func([]map[string]any) []map[string]any
	}{
		{"错误对象", func(f []map[string]any) []map[string]any { f[1]["object"] = "chat.completion"; return f }},
		{"身份改变", func(f []map[string]any) []map[string]any { f[1]["id"] = "other"; return f }},
		{"模型改变", func(f []map[string]any) []map[string]any { f[1]["model"] = "other"; return f }},
		{"创建时间改变", func(f []map[string]any) []map[string]any { f[1]["created"] = 1; return f }},
		{"错误时间类型", func(f []map[string]any) []map[string]any { f[1]["created"] = "0"; return f }},
		{"缺失delta", func(f []map[string]any) []map[string]any {
			delete(f[1]["choices"].([]any)[0].(map[string]any), "delta")
			return f
		}},
		{"非零choice", func(f []map[string]any) []map[string]any {
			f[1]["choices"].([]any)[0].(map[string]any)["index"] = 1
			return f
		}},
		{"未知终态", func(f []map[string]any) []map[string]any {
			f[2]["choices"].([]any)[0].(map[string]any)["finish_reason"] = "other"
			return f
		}},
		{"废弃终态", func(f []map[string]any) []map[string]any {
			f[2]["choices"].([]any)[0].(map[string]any)["finish_reason"] = "function_call"
			return f
		}},
		{"废弃接口", func(f []map[string]any) []map[string]any {
			f[1]["choices"].([]any)[0].(map[string]any)["delta"] = map[string]any{"function_call": map[string]any{"name": "lookup"}}
			return f
		}},
		{"不支持音频", func(f []map[string]any) []map[string]any {
			f[1]["choices"].([]any)[0].(map[string]any)["delta"] = map[string]any{"audio": map[string]any{"data": "opaque"}}
			return f
		}},
		{"错误文本类型", func(f []map[string]any) []map[string]any {
			f[1]["choices"].([]any)[0].(map[string]any)["delta"] = map[string]any{"content": 12}
			return f
		}},
		{"终态后增量", func(f []map[string]any) []map[string]any { return append(f[:3], f[1], f[3]) }},
		{"提前Usage", func(f []map[string]any) []map[string]any { return append(f[:2], f[3], f[2]) }},
		{"重复Usage", func(f []map[string]any) []map[string]any { return append(f, chatFixtureUsage()) }},
		{"缺失Usage", func(f []map[string]any) []map[string]any { return f[:3] }},
		{"不完整Usage", func(f []map[string]any) []map[string]any {
			delete(f[3]["usage"].(map[string]any), "completion_tokens")
			return f
		}},
		{"计数不一致", func(f []map[string]any) []map[string]any {
			f[3]["usage"].(map[string]any)["total_tokens"] = 1
			return f
		}},
		{"负工具索引", func(f []map[string]any) []map[string]any {
			f[1]["choices"].([]any)[0].(map[string]any)["delta"] = map[string]any{"tool_calls": []any{map[string]any{"index": -1}}}
			return f
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := decodeChatFixture(t, test.change(chatFixtureTextStream()), true); err == nil {
				t.Fatal("非法流未报错")
			}
		})
	}
	if _, _, err := decodeChatFixture(t, []map[string]any{{"error": map[string]any{"type": "server_error", "message": "失败"}}}, true); err == nil {
		t.Fatal("错误事件被当成正常完成")
	}
}
func TestChatStopReasonsAndEncoder(t *testing.T) {
	for _, test := range []struct {
		finish  string
		refusal bool
		reason  model.ModelStopReason
	}{
		{"stop", true, model.StopRefused},
		{"length", false, model.StopTruncated},
		{"content_filter", false, model.StopFiltered},
	} {
		frames := chatFixtureTextStream()
		frames[2]["choices"].([]any)[0].(map[string]any)["finish_reason"] = test.finish
		if test.refusal {
			frames[1]["choices"].([]any)[0].(map[string]any)["delta"] = map[string]any{"refusal": "不能回答"}
		}
		_, reason, err := decodeChatFixture(t, frames, true)
		if err != nil || reason != test.reason {
			t.Fatalf("终态错误：%s %v", reason, err)
		}
	}
	ctx := appcontext.ModelContext{Entries: []appcontext.ContextEntry{{
		Kind: appcontext.ContextEntryProviderOutput,
		Role: appcontext.ContextRoleAssistant,
		Content: []appcontext.ContextBlock{{
			Kind:   appcontext.ContextBlockToolCall,
			CallID: "call_1",
			Name:   "lookup",
			Input:  json.RawMessage(`{}`),
		}},
	}}}
	messages, err := encodeContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(messages)
	if strings.Contains(string(data), "function_call") || !strings.Contains(string(data), "tool_calls") {
		t.Fatalf("请求使用了错误 DTO：%s", data)
	}
}

func TestChatToolSchemaBoundaries(t *testing.T) {
	for _, delta := range []string{
		`{"role":null}`,
		`{"tool_calls":null}`,
		`{"tool_calls":[{"index":0,"id":null}]}`,
		`{"tool_calls":[{"index":0,"type":null}]}`,
		`{"tool_calls":[{"index":0,"function":null}]}`,
		`{"tool_calls":[{"index":0,"function":{"name":null}}]}`,
		`{"tool_calls":[{"index":0,"function":{"arguments":12}}]}`,
	} {
		var parsed chatCompletionDelta
		if err := json.Unmarshal([]byte(delta), &parsed); err == nil {
			t.Fatalf("非法分片被接受：%s", delta)
		}
	}
	frames := chatFixtureTools()
	for _, frame := range frames[:2] {
		for _, call := range frame["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)["tool_calls"].([]any) {
			delete(call.(map[string]any), "type")
		}
	}
	if _, reason, err := decodeChatFixture(t, frames, true); err != nil || reason != model.StopToolCalls {
		t.Fatalf("可选 type 字段被错误要求：%s %v", reason, err)
	}
}
