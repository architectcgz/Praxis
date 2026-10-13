package loop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	agentruntime "praxis/internal/agent_runtime"
	"praxis/internal/contracts"
	appcontext "praxis/internal/core/context"
	contextcompress "praxis/internal/core/context/compress"
	"praxis/internal/core/model"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	turnmodel "praxis/internal/core/turn"
	"praxis/internal/hooks"
	"praxis/internal/timing"
	toolcontracts "praxis/internal/tools/contracts"
)

type loopHookKey struct{}

type operationProbe struct {
	operations []hooks.Operation
	scopes     []*probeScope
}

type probeScope struct {
	result  hooks.Result
	signals int
	ends    int
}

func (h *operationProbe) Start(ctx context.Context, op hooks.Operation) (context.Context, hooks.Scope) {
	scope := &probeScope{}
	h.operations = append(h.operations, op)
	h.scopes = append(h.scopes, scope)
	return context.WithValue(ctx, loopHookKey{}, op.Kind), scope
}
func (s *probeScope) OnSignal(signal hooks.Signal) {
	if signal == hooks.SignalFirstResponse {
		s.signals++
	}
}
func (s *probeScope) End(result hooks.Result) { s.result = result; s.ends++ }

type loopTimingStore struct {
	timing.Store
	records map[string]timing.Record
}

func (s loopTimingStore) Save(_ context.Context, record timing.Record) error {
	s.records[record.ID] = record
	return record.Validate()
}

type streamFunc func(context.Context, model.ModelRequest) (<-chan model.ModelStreamEvent, error)

func (f streamFunc) Stream(ctx context.Context, request model.ModelRequest) (<-chan model.ModelStreamEvent, error) {
	return f(ctx, request)
}

func modelEvents(events ...model.ModelStreamEvent) <-chan model.ModelStreamEvent {
	stream := make(chan model.ModelStreamEvent, len(events))
	for _, event := range events {
		stream <- event
	}
	close(stream)
	return stream
}

// hookLoopFixture 运行摘要、工具轮和最终回答轮，不访问外部 API。
type hookLoopFixture struct {
	toolcontracts.ToolCatalog
	t          *testing.T
	withHooks  bool
	requests   []model.ModelRequest
	messages   []sessionmodel.MessageData
	turns      int
	tools      int
	compressed bool
	recordErr  error
}

func (f *hookLoopFixture) BuildModel(model.ModelSnapshot) (model.Model, error) {
	return model.Model{Stream: f, MaxOutputTokens: 100}, nil
}
func (f *hookLoopFixture) Stream(ctx context.Context, request model.ModelRequest) (<-chan model.ModelStreamEvent, error) {
	if f.withHooks && ctx.Value(loopHookKey{}) != hooks.OperationProvider {
		f.t.Fatal("Provider 未收到 Hook 的 Context")
	}
	f.requests = append(f.requests, request)
	switch len(f.requests) {
	case 1:
		if len(request.Tools) != 0 {
			f.t.Fatal("摘要不应提供工具")
		}
		return modelEvents(
			model.ModelStreamEvent{Kind: model.StreamTextDelta, Text: "摘要"},
			model.ModelStreamEvent{Kind: model.StreamComplete, StopReason: model.StopCompleted},
		), nil
	case 2:
		return modelEvents(
			model.ModelStreamEvent{
				Kind: model.StreamReasoningState,
				ReasoningState: &contracts.ReasoningState{
					Protocol: contracts.ProtocolAnthropicMessages,
					Version:  contracts.ReasoningStateVersion,
					Payload:  json.RawMessage(`{"type":"redacted_thinking","data":"opaque"}`),
				},
			},
			model.ModelStreamEvent{Kind: model.StreamThinkingDelta, Text: "思考"},
			model.ModelStreamEvent{Kind: model.StreamTextDelta, Text: "准备调用工具"},
			model.ModelStreamEvent{Kind: model.StreamToolCall, ToolCall: contracts.ToolCall{ID: "call", Name: "lookup", Arguments: json.RawMessage(`{}`)}},
			model.ModelStreamEvent{Kind: model.StreamComplete, StopReason: model.StopToolCalls},
		), nil
	case 3:
		return modelEvents(
			model.ModelStreamEvent{Kind: model.StreamTextDelta, Text: "最终回答"},
			model.ModelStreamEvent{Kind: model.StreamComplete, StopReason: model.StopCompleted},
		), nil
	default:
		return nil, errors.New("模型请求超出预期")
	}
}
func (f *hookLoopFixture) List() []contracts.ToolDefinition {
	return []contracts.ToolDefinition{{Name: "lookup"}}
}
func (f *hookLoopFixture) RecordStart(_ context.Context, metadata contracts.TurnExecutionContext) (turnmodel.Turn, error) {
	f.turns++
	return turnmodel.Turn{ID: contracts.TurnID(fmt.Sprintf("turn%d", metadata.Sequence))}, nil
}
func (f *hookLoopFixture) RecordEnd(context.Context, contracts.TurnID, turnmodel.TurnOutcome, contracts.TaskFailureCode, string) error {
	return nil
}
func (f *hookLoopFixture) Append(_ context.Context, message sessionmodel.MessageData) (sessionmodel.MessageData, error) {
	f.messages = append(f.messages, message)
	return message, nil
}
func (f *hookLoopFixture) RecordAssistant(ctx context.Context, appendMessage func(context.Context, sessionmodel.MessageData) (sessionmodel.MessageData, error), message sessionmodel.MessageData, _ contracts.ToolInvocationContext) error {
	if f.recordErr != nil {
		return f.recordErr
	}
	_, err := appendMessage(ctx, message)
	return err
}
func (f *hookLoopFixture) Invoke(ctx context.Context, _ contracts.ToolCall, _ contracts.ToolInvocationContext) (toolcontracts.ToolResult, error) {
	if f.withHooks && ctx.Value(loopHookKey{}) != hooks.OperationTool {
		f.t.Fatal("Tool 未收到 Hook 的 Context")
	}
	f.tools++
	return toolcontracts.ToolResult{Payload: "工具拒绝", ErrorClass: "denied"}, nil
}
func (f *hookLoopFixture) Compress(ctx context.Context, history appcontext.ModelContext, options contextcompress.Options, summarize contextcompress.SummarizeFunc) (appcontext.ModelContext, bool, error) {
	if !f.compressed {
		f.compressed = true
		text, err := summarize(ctx, history, options.MaxOutputTokens)
		if err != nil || text != "摘要" {
			return history, false, fmt.Errorf("摘要错误：%q %w", text, err)
		}
	}
	return history, false, nil
}

func hookTask() taskmodel.Task {
	return taskmodel.Task{
		ID:        "task",
		SessionID: "session",
		AgentID:   "agent",
		RequestID: "request",
		Input: taskmodel.InputSnapshot{
			AgentDefinitionID: "primary",
			Model: model.ModelSnapshot{
				ProviderID:    "provider",
				ModelID:       "model",
				APIFormat:     contracts.ProtocolAnthropicMessages,
				ContextWindow: 4096,
			},
			Security: contracts.SecuritySnapshot{Permissions: contracts.TaskPermissions{AllowedTools: []contracts.ToolName{"lookup"}}},
			Context: appcontext.ModelContext{Entries: []appcontext.ContextEntry{{
				Kind:    appcontext.ContextEntryUserInput,
				Role:    appcontext.ContextRoleUser,
				Content: []appcontext.ContextBlock{{Kind: appcontext.ContextBlockText, Text: "你好"}},
			}}},
		},
	}
}

func runHookFixture(t *testing.T, f *hookLoopFixture, d *hooks.Dispatcher) (taskmodel.TaskOutcome, contracts.TaskFailureCode, error) {
	t.Helper()
	runner, err := NewRunner(Config{
		ModelBuilder:        f,
		Tools:               f,
		ToolCalls:           f,
		Turns:               f,
		HookDispatcher:      d,
		CompressionStrategy: f,
		EventObserver:       func(agentruntime.AgentEvent) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	return runner.Run(t.Context(), hookTask(), f)
}

func TestLoopHooksPreserveExecutionAndTimeSummary(t *testing.T) {
	without := &hookLoopFixture{t: t}
	outcome, code, err := runHookFixture(t, without, nil)
	if err != nil || outcome != taskmodel.TaskCompleted || code != "" {
		t.Fatalf("无 Hook 执行失败：%s %s %v", outcome, code, err)
	}
	probe := &operationProbe{}
	store := loopTimingStore{records: make(map[string]timing.Record)}
	recorder, err := timing.New(store, nil, t.Errorf)
	if err != nil {
		t.Fatal(err)
	}
	d := hooks.NewDispatcher(probe, timing.Hook{Recorder: recorder})
	with := &hookLoopFixture{t: t, withHooks: true}
	got, gotCode, err := runHookFixture(t, with, d)
	if err != nil || got != outcome || gotCode != code || !reflect.DeepEqual(with.requests, without.requests) || !reflect.DeepEqual(with.messages, without.messages) {
		t.Fatalf("Hook 改变了模型请求或消息输出：%s %s %v", got, gotCode, err)
	}
	if with.tools != 1 || len(probe.operations) != 4 || len(store.records) != 4 {
		t.Fatalf("摘要、工具或最终回答丢失：%+v %+v", with, probe)
	}
	for index, op := range probe.operations {
		if op.SessionID != "session" || op.AgentID != "agent" || op.TaskID != "task" || op.TurnID == "" || probe.scopes[index].ends != 1 {
			t.Fatalf("操作归属或生命周期错误：%+v", op)
		}
	}
	for _, record := range store.records {
		if record.Kind == timing.Provider && (record.Status != timing.Completed || record.FirstResponseMS == nil) || record.Kind == timing.Tool && record.Status != timing.Failed {
			t.Fatalf("Provider 或 Tool 计时错误：%+v", record)
		}
	}
}

func TestAssistantStorageFailureDoesNotInvoke(t *testing.T) {
	probe := &operationProbe{}
	f := &hookLoopFixture{t: t, recordErr: errors.New("提交失败")}
	outcome, _, err := runHookFixture(t, f, hooks.NewDispatcher(probe))
	if err == nil || outcome != taskmodel.TaskFailed || f.tools != 0 || len(f.messages) != 0 {
		t.Fatalf("提交失败仍写入消息或执行工具：%s %v", outcome, err)
	}
	for _, op := range probe.operations {
		if op.Kind == hooks.OperationTool {
			t.Fatal("Assistant 提交失败后不应进入工具调用边界")
		}
	}
}

func TestProviderHookEndsAfterValidation(t *testing.T) {
	state := &contracts.ReasoningState{
		Protocol: contracts.ProtocolAnthropicMessages,
		Version:  contracts.ReasoningStateVersion,
		Payload:  json.RawMessage(`{"type":"redacted_thinking","data":"opaque"}`),
	}
	streamErr := errors.New("流建立失败")
	for _, test := range []struct {
		name      string
		events    []model.ModelStreamEvent
		startErr  error
		cancelled bool
		wantErr   bool
	}{
		{name: "推理状态不终止", events: []model.ModelStreamEvent{
			{Kind: model.StreamReasoningState, ReasoningState: state},
			{Kind: model.StreamTextDelta, Text: ""},
			{Kind: model.StreamTextDelta, Text: "回答"},
			{Kind: model.StreamComplete, StopReason: model.StopCompleted},
		}},
		{name: "跨协议校验", events: []model.ModelStreamEvent{
			{Kind: model.StreamReasoningState, ReasoningState: state},
			{Kind: model.StreamComplete, StopReason: model.StopCompleted},
		}, wantErr: true},
		{name: "截断", events: []model.ModelStreamEvent{{Kind: model.StreamComplete, StopReason: model.StopTruncated}}, wantErr: true},
		{name: "提前关闭", wantErr: true},
		{name: "流建立失败", startErr: streamErr, wantErr: true},
		{name: "取消", cancelled: true, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			probe := &operationProbe{}
			d := hooks.NewDispatcher(probe)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if test.cancelled {
				cancel()
			}
			request := model.ModelRequest{Model: hookTask().Input.Model, TurnID: "turn"}
			if test.name == "跨协议校验" {
				request.Model.APIFormat = contracts.ProtocolOpenAIResponses
			}
			response, err := requestModel(ctx, streamFunc(func(context.Context, model.ModelRequest) (<-chan model.ModelStreamEvent, error) {
				if test.cancelled {
					return make(chan model.ModelStreamEvent), nil
				}
				return modelEvents(test.events...), test.startErr
			}), request, hooks.Identity{
				SessionID: "session",
				AgentID:   "agent",
				TaskID:    "task",
				TurnID:    "turn",
			}, d, func(event model.ModelStreamEvent) {
				if event.Kind == model.StreamReasoningState {
					t.Fatal("推理状态被发送给观察者")
				}
			})
			if (err != nil) != test.wantErr || probe.scopes[0].result.Err != err || probe.scopes[0].result.StopReason != response.stopReason || probe.scopes[0].ends != 1 {
				t.Fatalf("Hook 未观察到最终校验结果：%v %+v", err, probe.scopes[0])
			}
			if test.cancelled && !errors.Is(err, context.Canceled) || test.startErr != nil && !errors.Is(err, test.startErr) {
				t.Fatalf("原错误丢失：%v", err)
			}
			if test.name == "推理状态不终止" && (response.textBytes != len("回答") || len(response.events) != 2 || probe.scopes[0].signals != 1) {
				t.Fatal("推理状态提前终止、首响应重复或未记录回答")
			}
		})
	}
}
