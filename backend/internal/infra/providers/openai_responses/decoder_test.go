package openairesponses

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

func responseFixtureSnapshot(status string, output ...map[string]any) map[string]any {
	if output == nil {
		output = []map[string]any{}
	}
	return map[string]any{
		"id":                  "resp_1",
		"object":              "response",
		"created_at":          123,
		"model":               "gpt-5",
		"status":              status,
		"parallel_tool_calls": true,
		"tool_choice":         "auto",
		"tools":               []any{},
		"output":              output,
	}
}
func responseFixtureEvent(kind string, fields map[string]any) map[string]any {
	if fields == nil {
		fields = map[string]any{}
	}
	fields["type"] = kind
	return fields
}
func responseFixtureSSE(t *testing.T, frames []map[string]any) string {
	t.Helper()
	var sse strings.Builder
	for sequence, frame := range frames {
		if _, exists := frame["sequence_number"]; !exists {
			frame["sequence_number"] = sequence
		}
		data, err := json.Marshal(frame)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&sse, "event: %s\ndata: %s\n\n", frame["type"], data)
	}
	return sse.String()
}
func decodeResponseFixture(t *testing.T, frames []map[string]any) ([]model.ModelStreamEvent, model.ModelStopReason, error) {
	t.Helper()
	var events []model.ModelStreamEvent
	reason, err := (decoder{}).Decode(t.Context(), streaming.NewSSEReader(strings.NewReader(responseFixtureSSE(t, frames))), func(event model.ModelStreamEvent) error { events = append(events, event); return nil })
	return events, reason, err
}
func responseFixtureMessage(status string, part map[string]any) map[string]any {
	parts := []any{}
	if part != nil {
		parts = append(parts, part)
	}
	return map[string]any{
		"id":      "msg_1",
		"type":    "message",
		"role":    "assistant",
		"status":  status,
		"content": parts,
	}
}
func responseFixtureText(text string) map[string]any {
	return map[string]any{"type": "output_text", "text": text, "annotations": []any{}}
}
func responseFixtureTextStream() []map[string]any {
	return []map[string]any{
		responseFixtureEvent("response.created", map[string]any{"response": responseFixtureSnapshot("in_progress")}),
		responseFixtureEvent("response.in_progress", map[string]any{"response": responseFixtureSnapshot("in_progress")}),
		responseFixtureEvent("response.output_item.added", map[string]any{"output_index": 0, "item": responseFixtureMessage("in_progress", nil)}),
		responseFixtureEvent("response.content_part.added", map[string]any{
			"output_index":  0,
			"item_id":       "msg_1",
			"content_index": 0,
			"part":          responseFixtureText(""),
		}),
		responseFixtureEvent("response.output_text.delta", map[string]any{
			"output_index":  0,
			"item_id":       "msg_1",
			"content_index": 0,
			"delta":         "你好",
			"logprobs":      []any{},
		}),
		responseFixtureEvent("response.output_text.done", map[string]any{
			"output_index":  0,
			"item_id":       "msg_1",
			"content_index": 0,
			"text":          "你好",
			"logprobs":      []any{},
		}),
		responseFixtureEvent("response.content_part.done", map[string]any{
			"output_index":  0,
			"item_id":       "msg_1",
			"content_index": 0,
			"part":          responseFixtureText("你好"),
		}),
		responseFixtureEvent("response.output_item.done", map[string]any{"output_index": 0, "item": responseFixtureMessage("completed", responseFixtureText("你好"))}),
		responseFixtureEvent("response.completed", map[string]any{"response": responseFixtureSnapshot("completed", responseFixtureMessage("completed", responseFixtureText("你好")))}),
	}
}

func TestResponseTypedSchemas(t *testing.T) {
	base := map[string]any{
		"sequence_number": 0,
		"output_index":    0,
		"item_id":         "item_1",
		"content_index":   0,
		"summary_index":   0,
		"delta":           "",
		"text":            "",
		"refusal":         "",
		"arguments":       "{}",
		"logprobs":        []any{},
		"message":         "错误",
	}
	cases := []struct {
		kind   string
		value  responseEvent
		fields []string
	}{
		{"response.created", &responseCreatedEvent{}, []string{"response"}},
		{"response.in_progress", &responseInProgressEvent{}, []string{"response"}},
		{"response.completed", &responseCompletedEvent{}, []string{"response"}},
		{"response.failed", &responseFailedEvent{}, []string{"response"}},
		{"response.incomplete", &responseIncompleteEvent{}, []string{"response"}},
		{"response.output_item.added", &responseOutputItemAddedEvent{}, []string{"output_index", "item"}},
		{"response.output_item.done", &responseOutputItemDoneEvent{}, []string{"output_index", "item"}},
		{"response.content_part.added", &responseContentPartAddedEvent{}, []string{
			"item_id",
			"output_index",
			"content_index",
			"part",
		}},
		{"response.content_part.done", &responseContentPartDoneEvent{}, []string{
			"item_id",
			"output_index",
			"content_index",
			"part",
		}},
		{"response.output_text.delta", &responseOutputTextDeltaEvent{}, []string{
			"item_id",
			"output_index",
			"content_index",
			"delta",
			"logprobs",
		}},
		{"response.output_text.done", &responseOutputTextDoneEvent{}, []string{
			"item_id",
			"output_index",
			"content_index",
			"text",
			"logprobs",
		}},
		{"response.refusal.delta", &responseRefusalDeltaEvent{}, []string{
			"item_id",
			"output_index",
			"content_index",
			"delta",
		}},
		{"response.refusal.done", &responseRefusalDoneEvent{}, []string{
			"item_id",
			"output_index",
			"content_index",
			"refusal",
		}},
		{"response.reasoning_summary_part.added", &responseReasoningSummaryPartAddedEvent{}, []string{
			"item_id",
			"output_index",
			"summary_index",
			"part",
		}},
		{"response.reasoning_summary_part.done", &responseReasoningSummaryPartDoneEvent{}, []string{
			"item_id",
			"output_index",
			"summary_index",
			"part",
		}},
		{"response.reasoning_summary_text.delta", &responseReasoningSummaryTextDeltaEvent{}, []string{
			"item_id",
			"output_index",
			"summary_index",
			"delta",
		}},
		{"response.reasoning_summary_text.done", &responseReasoningSummaryTextDoneEvent{}, []string{
			"item_id",
			"output_index",
			"summary_index",
			"text",
		}},
		{"response.reasoning_text.delta", &responseReasoningTextDeltaEvent{}, []string{
			"item_id",
			"output_index",
			"content_index",
			"delta",
		}},
		{"response.reasoning_text.done", &responseReasoningTextDoneEvent{}, []string{
			"item_id",
			"output_index",
			"content_index",
			"text",
		}},
		{"response.function_call_arguments.delta", &responseFunctionCallArgumentsDeltaEvent{}, []string{"item_id", "output_index", "delta"}},
		{"response.function_call_arguments.done", &responseFunctionCallArgumentsDoneEvent{}, []string{"item_id", "output_index", "arguments"}},
		{"error", &responseErrorEvent{}, []string{"message"}},
	}
	for _, test := range cases {
		t.Run(test.kind, func(t *testing.T) {
			fields := map[string]any{"type": test.kind, "sequence_number": 0}
			for _, name := range test.fields {
				fields[name] = base[name]
			}
			if strings.Contains(test.kind, "summary_part") {
				fields["part"] = map[string]any{"type": "summary_text", "text": ""}
			} else if strings.Contains(test.kind, "content_part") {
				fields["part"] = responseFixtureText("")
			}
			if strings.Contains(test.kind, "output_item") {
				fields["item"] = responseFixtureMessage("in_progress", nil)
			}
			if _, exists := fields["response"]; exists {
				fields["response"] = responseFixtureSnapshot("in_progress")
			}
			data, _ := json.Marshal(fields)
			event, err := decodeResponseEvent(data)
			if err != nil || reflect.TypeOf(event) != reflect.TypeOf(test.value) {
				t.Fatalf("具体 Schema 错误：%T, %v", event, err)
			}
			for _, name := range append(test.fields, "type", "sequence_number") {
				value := fields[name]
				delete(fields, name)
				data, _ = json.Marshal(fields)
				if _, err := decodeResponseEvent(data); err == nil {
					t.Fatalf("未拒绝缺失字段 %s", name)
				}
				fields[name] = nil
				data, _ = json.Marshal(fields)
				if _, err := decodeResponseEvent(data); err == nil {
					t.Fatalf("未拒绝 null 字段 %s", name)
				}
				fields[name] = value
			}
		})
	}
}

func TestResponseTextAndProtocolErrors(t *testing.T) {
	events, reason, err := decodeResponseFixture(t, responseFixtureTextStream())
	if err != nil || reason != model.StopCompleted || len(events) != 1 || events[0].Text != "你好" {
		t.Fatalf("文本应只输出一次：%v %s %v", events, reason, err)
	}
	cases := []struct {
		name   string
		change func([]map[string]any) []map[string]any
	}{
		{"提前中断", func(f []map[string]any) []map[string]any { return f[:8] }},
		{"重复序号", func(f []map[string]any) []map[string]any { f[4]["sequence_number"] = 3; return f }},
		{"缺失索引", func(f []map[string]any) []map[string]any { delete(f[4], "content_index"); return f }},
		{"负索引", func(f []map[string]any) []map[string]any { f[4]["output_index"] = -1; return f }},
		{"错误字段类型", func(f []map[string]any) []map[string]any { f[4]["delta"] = 12; return f }},
		{"错误身份", func(f []map[string]any) []map[string]any { f[4]["item_id"] = "other"; return f }},
		{"重复块开始", func(f []map[string]any) []map[string]any { f[4] = f[3]; return f }},
		{"重复文本完成", func(f []map[string]any) []map[string]any { f[6] = f[5]; return f }},
		{"完成后增量", func(f []map[string]any) []map[string]any { f[5], f[6] = f[6], f[4]; return f }},
		{"缺少块开始", func(f []map[string]any) []map[string]any { return append(f[:3], f[4:]...) }},
		{"缺少响应开始", func(f []map[string]any) []map[string]any { return f[1:] }},
		{"快照不一致", func(f []map[string]any) []map[string]any {
			f[8]["response"] = responseFixtureSnapshot("completed", responseFixtureMessage("completed", responseFixtureText("其他")))
			return f
		}},
		{"终态身份冲突", func(f []map[string]any) []map[string]any {
			f[8]["response"].(map[string]any)["model"] = "other"
			return f
		}},
		{"不支持的输出", func(f []map[string]any) []map[string]any {
			f[2]["item"] = map[string]any{"type": "web_search_call", "id": "ws_1"}
			return f
		}},
		{"未知关键事件", func(f []map[string]any) []map[string]any { f[4]["type"] = "response.future.delta"; return f }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			events, _, err := decodeResponseFixture(t, test.change(responseFixtureTextStream()))
			if err == nil {
				t.Fatal("非法流未报错")
			}
			for _, event := range events {
				if event.Kind == model.StreamToolCall {
					t.Fatal("协议失败前释放了工具")
				}
			}
		})
	}
}

func responseFixtureReasoningTools() []map[string]any {
	initial := map[string]any{
		"id":                "rs_1",
		"type":              "reasoning",
		"summary":           []any{},
		"encrypted_content": "partial",
	}
	final := map[string]any{
		"id":                "rs_1",
		"type":              "reasoning",
		"summary":           []any{map[string]any{"type": "summary_text", "text": "摘要"}},
		"content":           []any{map[string]any{"type": "reasoning_text", "text": "思考"}},
		"encrypted_content": "final_secret",
		"future":            map[string]any{"opaque": true},
	}
	frames := []map[string]any{
		responseFixtureEvent("response.created", map[string]any{"response": responseFixtureSnapshot("in_progress")}),
		responseFixtureEvent("response.output_item.added", map[string]any{"output_index": 0, "item": initial}),
	}
	for _, part := range []struct{ kind, index, text string }{
		{"reasoning_summary", "summary_index", "摘要"},
		{"reasoning", "content_index", "思考"},
	} {
		partType, prefix := "summary_text", "response.reasoning_summary_part"
		if part.kind == "reasoning" {
			partType, prefix = "reasoning_text", "response.content_part"
		}
		frames = append(frames,
			responseFixtureEvent(prefix+".added", map[string]any{
				"output_index": 0,
				"item_id":      "rs_1",
				part.index:     0,
				"part":         map[string]any{"type": partType, "text": ""},
			}),
			responseFixtureEvent("response."+part.kind+"_text.delta", map[string]any{
				"output_index": 0,
				"item_id":      "rs_1",
				part.index:     0,
				"delta":        part.text,
			}),
			responseFixtureEvent("response."+part.kind+"_text.done", map[string]any{
				"output_index": 0,
				"item_id":      "rs_1",
				part.index:     0,
				"text":         part.text,
			}),
			responseFixtureEvent(prefix+".done", map[string]any{
				"output_index": 0,
				"item_id":      "rs_1",
				part.index:     0,
				"part":         map[string]any{"type": partType, "text": part.text},
			}),
		)
	}
	frames = append(frames, responseFixtureEvent("response.output_item.done", map[string]any{"output_index": 0, "item": final}))
	outputs := []map[string]any{final}
	for index := 1; index <= 2; index++ {
		id, callID := fmt.Sprintf("fc_%d", index), fmt.Sprintf("call_%d", index)
		call := func(arguments string) map[string]any {
			return map[string]any{
				"id":        id,
				"type":      "function_call",
				"call_id":   callID,
				"name":      "lookup",
				"arguments": arguments,
			}
		}
		frames = append(frames,
			responseFixtureEvent("response.output_item.added", map[string]any{"output_index": index, "item": call("")}),
			responseFixtureEvent("response.function_call_arguments.delta", map[string]any{"output_index": index, "item_id": id, "delta": "{"}),
			responseFixtureEvent("response.function_call_arguments.delta", map[string]any{"output_index": index, "item_id": id, "delta": "}"}),
			responseFixtureEvent("response.function_call_arguments.done", map[string]any{"output_index": index, "item_id": id, "arguments": "{}"}),
			responseFixtureEvent("response.output_item.done", map[string]any{"output_index": index, "item": call("{}")}),
		)
		outputs = append(outputs, call("{}"))
	}
	snapshot := responseFixtureSnapshot("completed", outputs...)
	snapshot["usage"] = map[string]any{
		"input_tokens":          10,
		"output_tokens":         5,
		"total_tokens":          15,
		"input_tokens_details":  map[string]any{"cached_tokens": 3},
		"output_tokens_details": map[string]any{"reasoning_tokens": 2},
	}
	return append(frames, responseFixtureEvent("response.completed", map[string]any{"response": snapshot}))
}

func TestResponseReasoningAndParallelTools(t *testing.T) {
	frames := responseFixtureReasoningTools()
	events, reason, err := decodeResponseFixture(t, frames)
	if err != nil || reason != model.StopToolCalls {
		t.Fatalf("工具流失败：%s %v", reason, err)
	}
	var state *contracts.ReasoningState
	var texts strings.Builder
	calls := 0
	for _, event := range events {
		switch event.Kind {
		case model.StreamThinkingDelta:
			texts.WriteString(event.Text)
		case model.StreamReasoningState:
			if state != nil {
				t.Fatal("推理状态重复")
			}
			state = event.ReasoningState
		case model.StreamToolCall:
			calls++
		case model.StreamUsage:
			if event.Usage.InputTokens != 10 || *event.Usage.CacheReadInputTokens != 3 {
				t.Fatal("Usage 错误")
			}
		}
	}
	if calls != 2 || texts.String() != "摘要思考" || state == nil || !strings.Contains(string(state.Payload), "final_secret") || strings.Contains(string(state.Payload), "partial") || !strings.Contains(string(state.Payload), "future") {
		t.Fatalf("推理或工具丢失：%d %s %v", calls, texts.String(), state)
	}
	ctx := appcontext.ModelContext{Entries: []appcontext.ContextEntry{{
		Kind:    appcontext.ContextEntryProviderOutput,
		Role:    appcontext.ContextRoleAssistant,
		Content: []appcontext.ContextBlock{{Kind: appcontext.ContextBlockReasoningState, ReasoningState: state}},
	}}}
	input, err := encodeContext(ctx)
	if err != nil || len(input) != 1 || string(input[0]) != string(state.Payload) {
		t.Fatalf("推理状态未原样回传：%v", err)
	}
	for _, change := range []func(*contracts.ReasoningState){
		func(s *contracts.ReasoningState) { s.Version = 1 },
		func(s *contracts.ReasoningState) { s.Protocol = contracts.ProtocolAnthropicMessages },
		func(s *contracts.ReasoningState) {
			s.Payload = json.RawMessage(`{"id":"rs_1","type":"reasoning","encrypted_content":"secret"}`)
		},
	} {
		copy := state.Clone()
		change(copy)
		ctx.Entries[0].Content[0].ReasoningState = copy
		if _, err := encodeContext(ctx); err == nil {
			t.Fatal("未拒绝旧状态、跨协议或缺失必填字段")
		}
	}
	frames = responseFixtureReasoningTools()
	last := frames[len(frames)-1]["response"].(map[string]any)
	last["output"].([]map[string]any)[2]["arguments"] = "[]"
	events, _, err = decodeResponseFixture(t, frames)
	if err == nil {
		t.Fatal("未拒绝无效工具快照")
	}
	for _, event := range events {
		if event.Kind == model.StreamToolCall || event.Kind == model.StreamReasoningState {
			t.Fatal("全部校验前释放了工具或状态")
		}
	}
}

func TestResponseTerminalAndUnknownEvents(t *testing.T) {
	for _, test := range []struct {
		status, detail string
		reason         model.ModelStopReason
	}{
		{"incomplete", "max_output_tokens", model.StopTruncated},
		{"incomplete", "content_filter", model.StopFiltered},
		{"completed", "", model.StopCompleted},
	} {
		snapshot := responseFixtureSnapshot(test.status)
		if test.detail != "" {
			snapshot["incomplete_details"] = map[string]any{"reason": test.detail}
		}
		frames := []map[string]any{
			responseFixtureEvent("response.created", map[string]any{"response": responseFixtureSnapshot("in_progress")}),
			responseFixtureEvent("response.future.telemetry", map[string]any{"metadata": "ignored"}),
			responseFixtureEvent("response."+test.status, map[string]any{"response": snapshot}),
		}
		_, reason, err := decodeResponseFixture(t, frames)
		if err != nil || reason != test.reason {
			t.Fatalf("终态错误：%s %v", reason, err)
		}
	}
	refusal := map[string]any{"type": "refusal", "refusal": "不能回答"}
	frames := responseFixtureTextStream()
	frames[3]["part"] = map[string]any{"type": "refusal", "refusal": ""}
	frames[4]["type"], frames[4]["delta"] = "response.refusal.delta", "不能回答"
	frames[5]["type"], frames[5]["refusal"] = "response.refusal.done", "不能回答"
	frames[6]["part"] = refusal
	frames[7]["item"] = responseFixtureMessage("completed", refusal)
	frames[8]["response"] = responseFixtureSnapshot("completed", responseFixtureMessage("completed", refusal))
	events, reason, err := decodeResponseFixture(t, frames)
	if err != nil || reason != model.StopRefused || len(events) != 1 || events[0].Text != "不能回答" {
		t.Fatalf("拒绝语义错误：%s %v", reason, err)
	}
	for _, frame := range []map[string]any{
		responseFixtureEvent("error", map[string]any{"code": nil, "param": nil, "message": "服务失败"}),
		responseFixtureEvent("response.failed", map[string]any{"response": responseFixtureSnapshot("failed")}),
	} {
		frames := []map[string]any{responseFixtureEvent("response.created", map[string]any{"response": responseFixtureSnapshot("in_progress")}), frame}
		if _, _, err := decodeResponseFixture(t, frames); err == nil {
			t.Fatal("错误事件被当成正常完成")
		}
	}
}

func TestResponseItemsAndSnapshotOrdering(t *testing.T) {
	for _, raw := range []string{
		`{"type":"message","id":"msg_1","role":"assistant","status":"completed"}`,
		`{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","annotations":[]}]}`,
		`{"type":"reasoning","id":"rs_1"}`,
		`{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text"}]}`,
		`{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":12}`,
		`{"type":"function_call","call_id":"call_1","name":"lookup"}`,
		`{"type":"function_call","call_id":" call_1 ","name":"lookup","arguments":"{}"}`,
	} {
		var item responseOutput
		if err := json.Unmarshal([]byte(raw), &item); err == nil {
			t.Fatalf("非法 Item 被接受：%s", raw)
		}
	}
	message := responseFixtureMessage("completed", nil)
	message["content"] = []any{responseFixtureText("一"), responseFixtureText("二"), responseFixtureText("三")}
	for range 20 {
		frames := []map[string]any{
			responseFixtureEvent("response.created", map[string]any{"response": responseFixtureSnapshot("in_progress")}),
			responseFixtureEvent("response.completed", map[string]any{"response": responseFixtureSnapshot("completed", message)}),
		}
		events, reason, err := decodeResponseFixture(t, frames)
		var text strings.Builder
		for _, event := range events {
			text.WriteString(event.Text)
		}
		if err != nil || reason != model.StopCompleted || text.String() != "一二三" {
			t.Fatalf("快照内容顺序错误：%s %v", text.String(), err)
		}
	}
	// function_call 的 id/status 可选，不能把可选字段误设成 Snapshot 的必填字段。
	call := map[string]any{
		"type":      "function_call",
		"call_id":   "call_1",
		"name":      "lookup",
		"arguments": "{}",
	}
	frames := []map[string]any{
		responseFixtureEvent("response.created", map[string]any{"response": responseFixtureSnapshot("in_progress")}),
		responseFixtureEvent("response.completed", map[string]any{"response": responseFixtureSnapshot("completed", call)}),
	}
	events, reason, err := decodeResponseFixture(t, frames)
	if err != nil || reason != model.StopToolCalls || len(events) != 1 {
		t.Fatalf("可选字段被错误要求：%s %v", reason, err)
	}
	frames = responseFixtureReasoningTools()
	frames[5]["status"] = "incomplete"
	events, _, err = decodeResponseFixture(t, frames)
	if err == nil {
		t.Fatal("未完成的摘要被当作正常完成")
	}
	for _, event := range events {
		if event.Kind == model.StreamToolCall {
			t.Fatal("摘要不完整仍释放了工具")
		}
	}
}
