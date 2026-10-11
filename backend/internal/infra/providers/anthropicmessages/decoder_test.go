package anthropicmessages

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"praxis/internal/contracts"
	appcontext "praxis/internal/core/context"
	"praxis/internal/core/model"
	"praxis/internal/infra/providers/streaming"
)

func messageFixtureEvent(kind string, fields map[string]any) map[string]any {
	if fields == nil {
		fields = map[string]any{}
	}
	fields["type"] = kind
	return fields
}
func messageFixtureStart() map[string]any {
	return messageFixtureEvent("message_start", map[string]any{"message": map[string]any{
		"id":            "msg_1",
		"type":          "message",
		"model":         "claude-sonnet-4-5",
		"role":          "assistant",
		"content":       []any{},
		"stop_reason":   nil,
		"stop_sequence": nil,
		"usage": map[string]any{
			"input_tokens":                10,
			"output_tokens":               0,
			"cache_read_input_tokens":     3,
			"cache_creation_input_tokens": 2,
		},
	}})
}
func messageFixtureTerminal(reason string) map[string]any {
	return messageFixtureEvent("message_delta", map[string]any{
		"delta": map[string]any{"stop_reason": reason, "stop_sequence": nil},
		"usage": map[string]any{"output_tokens": 5},
	})
}
func messageFixtureTextStream() []map[string]any {
	return []map[string]any{
		messageFixtureStart(),
		messageFixtureEvent("ping", nil),
		messageFixtureEvent("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "text", "text": ""}}),
		messageFixtureEvent("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "text_delta", "text": "你好"}}),
		messageFixtureEvent("content_block_stop", map[string]any{"index": 0}),
		messageFixtureTerminal("end_turn"),
		messageFixtureEvent("message_stop", nil),
	}
}
func decodeMessageFixture(t *testing.T, frames []map[string]any) ([]model.ModelStreamEvent, model.ModelStopReason, error) {
	t.Helper()
	var sse strings.Builder
	for _, frame := range frames {
		data, err := json.Marshal(frame)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&sse, "event: %s\ndata: %s\n\n", frame["type"], data)
	}
	var events []model.ModelStreamEvent
	reason, err := (decoder{}).Decode(t.Context(), streaming.NewSSEReader(strings.NewReader(sse.String())), func(event model.ModelStreamEvent) error { events = append(events, event); return nil })
	return events, reason, err
}
func TestMessageTypedSchemas(t *testing.T) {
	cases := []struct {
		kind   string
		value  messageEvent
		fields map[string]any
	}{
		{"message_start", &messageStartEvent{}, map[string]any{"message": messageFixtureStart()["message"]}},
		{"content_block_start", &contentBlockStartEvent{}, map[string]any{"index": 0, "content_block": map[string]any{"type": "text", "text": ""}}},
		{"content_block_delta", &contentBlockDeltaEvent{}, map[string]any{"index": 0, "delta": map[string]any{"type": "text_delta", "text": ""}}},
		{"content_block_stop", &contentBlockStopEvent{}, map[string]any{"index": 0}},
		{"message_delta", &messageDeltaEvent{}, map[string]any{"delta": map[string]any{}, "usage": map[string]any{"output_tokens": 0}}},
		{"message_stop", &messageStopEvent{}, map[string]any{}},
		{"ping", &pingEvent{}, map[string]any{}},
		{"error", &errorEvent{}, map[string]any{"error": map[string]any{"type": "overloaded_error", "message": "过载"}}},
	}
	for _, test := range cases {
		t.Run(test.kind, func(t *testing.T) {
			fields := messageFixtureEvent(test.kind, test.fields)
			data, _ := json.Marshal(fields)
			event, err := decodeMessageEvent(data)
			if err != nil || reflect.TypeOf(event) != reflect.TypeOf(test.value) {
				t.Fatalf("具体 Schema 错误：%T %v", event, err)
			}
			for name, value := range fields {
				if name == "usage" {
					continue
				}
				delete(fields, name)
				data, _ = json.Marshal(fields)
				if _, err := decodeMessageEvent(data); err == nil {
					t.Fatalf("未拒绝缺失字段 %s", name)
				}
				fields[name] = nil
				data, _ = json.Marshal(fields)
				if _, err := decodeMessageEvent(data); err == nil {
					t.Fatalf("未拒绝 null 字段 %s", name)
				}
				fields[name] = value
			}
		})
	}
	for _, test := range []struct {
		kind, field string
		value       contentDelta
	}{
		{"text_delta", "text", &textDelta{}},
		{"thinking_delta", "thinking", &thinkingDelta{}},
		{"signature_delta", "signature", &signatureDelta{}},
		{"input_json_delta", "partial_json", &inputJSONDelta{}},
		{"citations_delta", "citation", &citationsDelta{}},
	} {
		t.Run(test.kind, func(t *testing.T) {
			fields := map[string]any{"type": test.kind, test.field: ""}
			if test.kind == "citations_delta" {
				fields[test.field] = map[string]any{"type": "char_location", "cited_text": "引用"}
			}
			data, _ := json.Marshal(fields)
			var delta contentDeltaUnion
			if err := json.Unmarshal(data, &delta); err != nil || reflect.TypeOf(delta.Value) != reflect.TypeOf(test.value) {
				t.Fatalf("Delta Schema 错误：%T %v", delta.Value, err)
			}
			delete(fields, test.field)
			data, _ = json.Marshal(fields)
			if err := json.Unmarshal(data, &delta); err == nil {
				t.Fatal("未拒绝缺失的 Delta 字段")
			}
		})
	}
}
func TestMessageTextAndUsage(t *testing.T) {
	events, reason, err := decodeMessageFixture(t, messageFixtureTextStream())
	if err != nil || reason != model.StopCompleted || len(events) != 3 || events[1].Text != "你好" {
		t.Fatalf("文本错误：%v %s %v", events, reason, err)
	}
	if events[0].Usage.InputTokens != 15 || events[2].Usage.InputTokens != 15 || *events[0].Usage.OutputTokens != 0 || *events[2].Usage.OutputTokens != 5 {
		t.Fatal("缓存计数或累计 Usage 错误")
	}
}
func messageFixtureReasoningTools() []map[string]any {
	frames := []map[string]any{
		messageFixtureStart(),
		messageFixtureEvent("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{
			"type":      "thinking",
			"thinking":  "",
			"signature": "",
			"future":    map[string]any{"opaque": true},
		}}),
		messageFixtureEvent("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "thinking_delta", "thinking": "思考"}}),
		messageFixtureEvent("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "signature_delta", "signature": "sig_"}}),
		messageFixtureEvent("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "signature_delta", "signature": "secret"}}),
		messageFixtureEvent("content_block_stop", map[string]any{"index": 0}),
		messageFixtureEvent("content_block_start", map[string]any{"index": 1, "content_block": map[string]any{"type": "redacted_thinking", "data": "redacted_secret"}}),
		messageFixtureEvent("content_block_stop", map[string]any{"index": 1}),
	}
	for index := 2; index <= 3; index++ {
		frames = append(frames,
			messageFixtureEvent("content_block_start", map[string]any{"index": index, "content_block": map[string]any{
				"type":  "tool_use",
				"id":    fmt.Sprintf("call_%d", index-1),
				"name":  "lookup",
				"input": map[string]any{},
			}}),
			messageFixtureEvent("content_block_delta", map[string]any{"index": index, "delta": map[string]any{"type": "input_json_delta", "partial_json": "{"}}),
			messageFixtureEvent("content_block_delta", map[string]any{"index": index, "delta": map[string]any{"type": "input_json_delta", "partial_json": "}"}}),
			messageFixtureEvent("content_block_stop", map[string]any{"index": index}),
		)
	}
	return append(frames, messageFixtureTerminal("tool_use"), messageFixtureEvent("message_stop", nil))
}
func TestMessageReasoningAndParallelTools(t *testing.T) {
	events, reason, err := decodeMessageFixture(t, messageFixtureReasoningTools())
	if err != nil || reason != model.StopToolCalls {
		t.Fatalf("工具流失败：%s %v", reason, err)
	}
	var saved []*contracts.ReasoningState
	calls, thinking := 0, ""
	for _, event := range events {
		switch event.Kind {
		case model.StreamThinkingDelta:
			thinking += event.Text
		case model.StreamReasoningState:
			saved = append(saved, event.ReasoningState)
		case model.StreamToolCall:
			calls++
			if string(event.ToolCall.Arguments) != "{}" {
				t.Fatal("空 input 占位符被拼进参数")
			}
		}
	}
	if thinking != "思考" || calls != 2 || len(saved) != 2 || !strings.Contains(string(saved[0].Payload), "sig_secret") || !strings.Contains(string(saved[0].Payload), "future") || !strings.Contains(string(saved[1].Payload), "redacted_secret") {
		t.Fatalf("签名或状态丢失：%d %s %v", calls, thinking, saved)
	}
	for _, state := range saved {
		ctx := appcontext.ModelContext{Entries: []appcontext.ContextEntry{{
			Kind:    appcontext.ContextEntryProviderOutput,
			Role:    appcontext.ContextRoleAssistant,
			Content: []appcontext.ContextBlock{{Kind: appcontext.ContextBlockReasoningState, ReasoningState: state}},
		}}}
		encoded, err := encodeContext(ctx)
		if err != nil || len(encoded.Messages) != 1 || string(encoded.Messages[0].Content[0]) != string(state.Payload) {
			t.Fatalf("状态未原样回传：%v", err)
		}
		for _, change := range []func(*contracts.ReasoningState){
			func(s *contracts.ReasoningState) { s.Version = 1 },
			func(s *contracts.ReasoningState) { s.Protocol = contracts.ProtocolOpenAIResponses },
			func(s *contracts.ReasoningState) {
				s.Payload = json.RawMessage(`{"type":"thinking","signature":"sig_secret"}`)
			},
		} {
			copy := state.Clone()
			change(copy)
			ctx.Entries[0].Content[0].ReasoningState = copy
			if _, err := encodeContext(ctx); err == nil {
				t.Fatal("未拒绝旧状态、跨协议或不完整状态")
			}
		}
	}
	frames := messageFixtureReasoningTools()
	frames[14]["delta"] = map[string]any{"type": "input_json_delta", "partial_json": "bad}"}
	events, _, err = decodeMessageFixture(t, frames)
	if err == nil {
		t.Fatal("未拒绝无效工具")
	}
	for _, event := range events {
		if event.Kind == model.StreamToolCall || event.Kind == model.StreamReasoningState {
			t.Fatal("所有校验完成前释放了工具或状态")
		}
	}
	frames = messageFixtureReasoningTools()
	events, _, err = decodeMessageFixture(t, frames[:len(frames)-1])
	if err == nil {
		t.Fatal("未拒绝提前中断")
	}
	for _, event := range events {
		if event.Kind == model.StreamToolCall {
			t.Fatal("message_stop 前释放了工具")
		}
	}
}
func TestMessageInvalidStreams(t *testing.T) {
	cases := []struct {
		name   string
		change func([]map[string]any) []map[string]any
	}{
		{"提前中断", func(f []map[string]any) []map[string]any { return f[:6] }},
		{"缺失开始", func(f []map[string]any) []map[string]any { return f[1:] }},
		{"重复开始", func(f []map[string]any) []map[string]any { f[1] = messageFixtureStart(); return f }},
		{"缺失模型", func(f []map[string]any) []map[string]any { delete(f[0]["message"].(map[string]any), "model"); return f }},
		{"错误对象", func(f []map[string]any) []map[string]any {
			f[0]["message"].(map[string]any)["type"] = "other"
			return f
		}},
		{"负索引", func(f []map[string]any) []map[string]any { f[2]["index"] = -1; return f }},
		{"缺失索引", func(f []map[string]any) []map[string]any { delete(f[3], "index"); return f }},
		{"重复块开始", func(f []map[string]any) []map[string]any { f[3] = f[2]; return f }},
		{"错误Delta目标", func(f []map[string]any) []map[string]any {
			f[3]["delta"] = map[string]any{"type": "thinking_delta", "thinking": "思考"}
			return f
		}},
		{"工具 Delta 目标错误", func(f []map[string]any) []map[string]any {
			f[3]["delta"] = map[string]any{"type": "input_json_delta", "partial_json": "{}"}
			return f
		}},
		{"签名 Delta 目标错误", func(f []map[string]any) []map[string]any {
			f[3]["delta"] = map[string]any{"type": "signature_delta", "signature": "sig_secret"}
			return f
		}},
		{"缺失Delta字段", func(f []map[string]any) []map[string]any {
			f[3]["delta"] = map[string]any{"type": "text_delta"}
			return f
		}},
		{"错误字段类型", func(f []map[string]any) []map[string]any {
			f[3]["delta"] = map[string]any{"type": "text_delta", "text": 12}
			return f
		}},
		{"未知Delta", func(f []map[string]any) []map[string]any {
			f[3]["delta"] = map[string]any{"type": "future_delta", "text": "隐藏"}
			return f
		}},
		{"不支持块", func(f []map[string]any) []map[string]any {
			f[2]["content_block"] = map[string]any{"type": "server_tool_use"}
			return f
		}},
		{"重复块停止", func(f []map[string]any) []map[string]any { f[5] = f[4]; return f }},
		{"缺少块停止", func(f []map[string]any) []map[string]any { return append(f[:4], f[5:]...) }},
		{"终态后增量", func(f []map[string]any) []map[string]any { return append(f[:6], f[3], f[6]) }},
		{"重复终态", func(f []map[string]any) []map[string]any {
			return append(f[:6], messageFixtureTerminal("end_turn"), f[6])
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := decodeMessageFixture(t, test.change(messageFixtureTextStream())); err == nil {
				t.Fatal("非法流未报错")
			}
		})
	}
}
func TestMessageStopReasonsAndUnknownEvents(t *testing.T) {
	for _, test := range []struct {
		reason string
		stop   model.ModelStopReason
	}{
		{"max_tokens", model.StopTruncated},
		{"model_context_window_exceeded", model.StopTruncated},
		{"refusal", model.StopRefused},
		{"pause_turn", model.StopPaused},
		{"stop_sequence", model.StopSequence},
	} {
		frames := messageFixtureTextStream()
		frames[1] = messageFixtureEvent("future_telemetry", map[string]any{"metadata": "ignored"})
		frames[5] = messageFixtureTerminal(test.reason)
		if test.reason == "stop_sequence" {
			frames[5]["delta"].(map[string]any)["stop_sequence"] = "END"
		}
		_, reason, err := decodeMessageFixture(t, frames)
		if err != nil || reason != test.stop {
			t.Fatalf("终态错误：%s %v", reason, err)
		}
	}
	if _, _, err := decodeMessageFixture(t, []map[string]any{messageFixtureEvent("error", map[string]any{"error": map[string]any{"type": "overloaded_error", "message": "失败"}})}); err == nil {
		t.Fatal("错误事件被当成正常完成")
	}
	frames := messageFixtureReasoningTools()
	frames[4]["delta"] = map[string]any{"type": "thinking_delta", "thinking": "签名后的思考"}
	if _, _, err := decodeMessageFixture(t, frames); err == nil {
		t.Fatal("签名之后仍允许修改思考文本")
	}
}

func TestMessageOptionalUsageAndCitations(t *testing.T) {
	for _, test := range []struct {
		name        string
		change      func([]map[string]any)
		usageEvents int
	}{
		{"首帧无 Usage", func(f []map[string]any) { delete(f[0]["message"].(map[string]any), "usage") }, 0},
		{"终态无 Usage", func(f []map[string]any) { delete(f[5], "usage") }, 1},
		{"两帧 Usage 均缺失", func(f []map[string]any) {
			delete(f[0]["message"].(map[string]any), "usage")
			delete(f[5], "usage")
		}, 0},
		{"可空 Usage", func(f []map[string]any) {
			f[0]["message"].(map[string]any)["usage"], f[5]["usage"] = nil, nil
		}, 0},
		{"首帧 Usage 不完整", func(f []map[string]any) {
			delete(f[0]["message"].(map[string]any)["usage"].(map[string]any), "output_tokens")
		}, 0},
		{"终态 Usage 无效", func(f []map[string]any) {
			f[5]["usage"] = map[string]any{"output_tokens": -1}
		}, 1},
		{"后续提供完整计数", func(f []map[string]any) {
			delete(f[0]["message"].(map[string]any), "usage")
			f[5]["usage"].(map[string]any)["input_tokens"] = 10
		}, 1},
		{"文本引用与模型空格", func(f []map[string]any) {
			f[0]["message"].(map[string]any)["model"] = " claude-sonnet-4-5 "
			f[2]["content_block"].(map[string]any)["citations"] = []any{map[string]any{"type": "char_location", "cited_text": "引用"}}
		}, 2},
		{"Usage 允许累计计数修正", func(f []map[string]any) {
			f[0]["message"].(map[string]any)["usage"].(map[string]any)["output_tokens"] = 6
			f[5]["usage"] = map[string]any{"input_tokens": 9, "output_tokens": 5}
		}, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			frames := messageFixtureTextStream()
			test.change(frames)
			events, reason, err := decodeMessageFixture(t, frames)
			if err != nil || reason != model.StopCompleted {
				t.Fatalf("可选元数据影响了文本解码：%s %v", reason, err)
			}
			var text strings.Builder
			usageCount := 0
			for _, event := range events {
				if event.Kind == model.StreamTextDelta {
					text.WriteString(event.Text)
				}
				if event.Kind == model.StreamUsage {
					usageCount++
					if !event.Usage.Valid() {
						t.Fatal("输出了无效用量")
					}
				}
			}
			if text.String() != "你好" || usageCount != test.usageEvents {
				t.Fatalf("文本或用量错误：%s %d", text.String(), usageCount)
			}
		})
	}
}

func TestMessageMetadataEventsAndUsageRecovery(t *testing.T) {
	frames := messageFixtureTextStream()
	// 引用及新增顶层事件可包含关联字段，但不能改变正文或生命周期。
	extra := []map[string]any{
		messageFixtureEvent("content_block_delta", map[string]any{
			"index": 0, "delta": map[string]any{"type": "citations_delta", "citation": map[string]any{"type": "char_location"}},
		}),
		messageFixtureEvent("future_event", map[string]any{"index": 0, "delta": "不应输出", "message": map[string]any{}}),
	}
	frames = append(append(frames[:4:4], extra...), frames[4:]...)
	// 无效的累计更新不能污染下一次有效 Usage，且不能修改已输出的计数。
	frames[7]["usage"] = map[string]any{"input_tokens": -1, "output_tokens": 99}
	frames = append(frames[:8:8], messageFixtureEvent("message_delta", map[string]any{
		"delta": map[string]any{}, "usage": map[string]any{"output_tokens": 5},
	}), frames[8])
	events, reason, err := decodeMessageFixture(t, frames)
	if err != nil || reason != model.StopCompleted || len(events) != 3 || events[1].Text != "你好" || events[0].Usage.InputTokens != 15 || *events[0].Usage.OutputTokens != 0 || events[2].Usage.InputTokens != 15 || *events[2].Usage.OutputTokens != 5 {
		t.Fatalf("元数据或无效统计影响了有效响应：%v %s %v", events, reason, err)
	}
	frames = messageFixtureReasoningTools()
	frames[2]["delta"] = map[string]any{"type": "citations_delta", "citation": map[string]any{"type": "char_location"}}
	if _, _, err := decodeMessageFixture(t, frames); err == nil {
		t.Fatal("引用增量不能关联 Thinking 块")
	}
	frames = messageFixtureTextStream()
	frames = append(frames[:5:5], extra[0], frames[5], frames[6])
	if _, _, err := decodeMessageFixture(t, frames); err == nil {
		t.Fatal("引用增量不能更新已关闭的块")
	}
}

func TestMessageOfficialThinkingRawSSE(t *testing.T) {
	// 官方 Thinking 示例的 message_start 与 message_delta 均不带 Usage。
	// 来源：https://platform.claude.com/docs/en/build-with-claude/streaming#streaming-request-with-thinking
	const sse = `event: message_start
data: {"type": "message_start", "message": {"id": "msg_01...", "type": "message", "role": "assistant", "content": [], "model": "claude-opus-5-5", "stop_reason": null, "stop_sequence": null}}

event: content_block_start
data: {"type": "content_block_start", "index": 0, "content_block": {"type": "thinking", "thinking": "", "signature": ""}}

event: content_block_delta
data: {"type": "content_block_delta", "index": 0, "delta": {"type": "thinking_delta", "thinking": "I need to find the GCD of 1071 and 462 using the Euclidean algorithm.\n\n1071 = 2 × 462 + 147"}}

event: content_block_delta
data: {"type": "content_block_delta", "index": 0, "delta": {"type": "thinking_delta", "thinking": "\n462 = 3 × 147 + 21"}}

event: content_block_delta
data: {"type": "content_block_delta", "index": 0, "delta": {"type": "thinking_delta", "thinking": "\n147 = 7 × 21 + 0"}}

event: content_block_delta
data: {"type": "content_block_delta", "index": 0, "delta": {"type": "thinking_delta", "thinking": "\nThe remainder is 0, so GCD(1071, 462) = 21."}}

event: content_block_delta
data: {"type": "content_block_delta", "index": 0, "delta": {"type": "signature_delta", "signature": "EqQBCgIYAhIM1gbcDa9GJwZA2b3hGgxBdjrkzLoky3dl1pkiMOYds..."}}

event: content_block_stop
data: {"type": "content_block_stop", "index": 0}

event: content_block_start
data: {"type": "content_block_start", "index": 1, "content_block": {"type": "text", "text": ""}}

event: content_block_delta
data: {"type": "content_block_delta", "index": 1, "delta": {"type": "text_delta", "text": "The greatest common divisor of 1071 and 462 is **21**."}}

event: content_block_stop
data: {"type": "content_block_stop", "index": 1}

event: message_delta
data: {"type": "message_delta", "delta": {"stop_reason": "end_turn", "stop_sequence": null}}

event: message_stop
data: {"type": "message_stop"}

`
	var events []model.ModelStreamEvent
	reason, err := (decoder{}).Decode(t.Context(), streaming.NewSSEReader(strings.NewReader(sse)), func(event model.ModelStreamEvent) error { events = append(events, event); return nil })
	if err != nil || reason != model.StopCompleted || len(events) != 6 {
		t.Fatalf("官方无 Usage Thinking 流解码失败：%v %s %v", events, reason, err)
	}
	state := events[len(events)-1].ReasoningState
	if state == nil || !strings.Contains(string(state.Payload), "EqQBCgIYAhIM") || !strings.Contains(string(state.Payload), "GCD(1071, 462) = 21.") || events[len(events)-2].Text != "The greatest common divisor of 1071 and 462 is **21**." {
		t.Fatal("正文或 Thinking 签名丢失")
	}
}

func TestMessageBlockSchemaBoundaries(t *testing.T) {
	for _, raw := range []string{
		`{"type":"text"}`,
		`{"type":"text","text":null}`,
		`{"type":"thinking","thinking":""}`,
		`{"type":"thinking","thinking":"","signature":null}`,
		`{"type":"redacted_thinking","data":""}`,
		`{"type":"tool_use","id":"call_1","name":"lookup","input":[]}`,
		`{"type":"tool_use","id":"call_1","name":"lookup","input":null}`,
		`{"type":"tool_use","id":"call_1","name":"lookup"}`,
	} {
		var block contentBlockUnion
		if err := json.Unmarshal([]byte(raw), &block); err == nil {
			t.Fatalf("非法 Block 被接受：%s", raw)
		}
	}
	frames := messageFixtureReasoningTools()
	frames[3]["delta"] = map[string]any{"type": "signature_delta", "signature": ""}
	frames[4]["delta"] = map[string]any{"type": "signature_delta", "signature": ""}
	events, _, err := decodeMessageFixture(t, frames)
	if err == nil {
		t.Fatal("缺失签名未报错")
	}
	for _, event := range events {
		if event.Kind == model.StreamToolCall {
			t.Fatal("缺失签名仍释放了工具")
		}
	}
	frames = messageFixtureReasoningTools()
	frames[12]["content_block"].(map[string]any)["id"] = "call_1"
	events, _, err = decodeMessageFixture(t, frames)
	if err == nil {
		t.Fatal("重复工具 ID 未报错")
	}
	for _, event := range events {
		if event.Kind == model.StreamToolCall {
			t.Fatal("重复工具 ID 仍释放了工具")
		}
	}
}

func TestMessageThinkingContinuationModes(t *testing.T) {
	for _, test := range []struct {
		name, mode, payload string
		wantError           bool
	}{
		{"默认模式无 Thinking", "", "", false},
		{"Adaptive 无 Thinking", "adaptive", "", false},
		{"Enabled 缺失 Thinking", "enabled", "", true},
		{"Enabled 签名原样续传", "enabled", `{"type":"thinking","thinking":"思考","signature":"sig_secret","future":true}`, false},
		{"Enabled 加密 Thinking", "enabled", `{"type":"redacted_thinking","data":"opaque_secret"}`, false},
		{"Adaptive 缺失签名", "adaptive", `{"type":"thinking","thinking":"思考","signature":""}`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var content []appcontext.ContextBlock
			if test.payload != "" {
				content = append(content, appcontext.ContextBlock{
					Kind: appcontext.ContextBlockReasoningState,
					ReasoningState: &contracts.ReasoningState{
						Protocol: contracts.ProtocolAnthropicMessages,
						Version:  contracts.ReasoningStateVersion,
						Payload:  json.RawMessage(test.payload),
					},
				})
			}
			content = append(content, appcontext.ContextBlock{
				Kind:   appcontext.ContextBlockToolCall,
				CallID: "call_1",
				Name:   "lookup",
				Input:  json.RawMessage(`{}`),
			})
			request := model.ModelRequest{
				Model: model.ModelSnapshot{
					ModelID:  "test-model",
					Thinking: contracts.ModelThinking{Mode: test.mode},
				},
				MaxOutputTokens: 2048,
				Context: appcontext.ModelContext{Entries: []appcontext.ContextEntry{{
					Kind: appcontext.ContextEntryProviderOutput, Role: appcontext.ContextRoleAssistant, Content: content,
				}}},
			}
			if test.mode == "enabled" {
				request.Model.Thinking.BudgetTokens = 1024
			}
			request.Context = request.Context.AppendToolResult(appcontext.NewToolResultBlock("call_1", "lookup", "结果", false))
			payload, err := buildPayload(request)
			if (err != nil) != test.wantError {
				t.Fatalf("Thinking 模式约束错误：%v", err)
			}
			if !test.wantError && test.payload != "" && !strings.Contains(string(payload), test.payload) {
				t.Fatal("签名载荷未原样重放")
			}
		})
	}
}
