package loop

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"praxis/internal/contracts"
	appcontext "praxis/internal/core/context"
	"praxis/internal/core/model"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	toolmodel "praxis/internal/core/tool_invocation"
	"praxis/internal/hooks"
	"praxis/internal/repository"
	toolcontracts "praxis/internal/tools/contracts"
)

type loopCommittedHook func(context.Context, hooks.CommittedEvent) error

func (f loopCommittedHook) OnCommitted(ctx context.Context, event hooks.CommittedEvent) error {
	return f(ctx, event)
}

type committedProgress struct{ task taskmodel.Task }

func (p *committedProgress) LoadTask(context.Context, contracts.TaskID) (taskmodel.Task, error) {
	return p.task, nil
}
func (*committedProgress) RecordIteration(context.Context, contracts.TaskID, uint64) error {
	return nil
}
func (*committedProgress) RecordSummaryRequest(context.Context, contracts.TaskID, uint64) error {
	return nil
}
func (*committedProgress) RecordModelOutput(context.Context, contracts.TaskID, int) error {
	return nil
}

type committedModel struct {
	stop     model.ModelStopReason
	requests int
}

func (m *committedModel) BuildModel(model.Config) (model.Model, error) {
	return model.Model{Stream: m, MaxOutputTokens: 1024}, nil
}
func (m *committedModel) Stream(context.Context, model.ModelRequest) (<-chan model.ModelStreamEvent, error) {
	m.requests++
	events := make(chan model.ModelStreamEvent, 2)
	if m.stop == model.StopToolCalls {
		events <- model.ModelStreamEvent{
			Kind: model.StreamToolCall,
			ToolCall: contracts.ToolCall{
				ID:        "call",
				Name:      "read_file",
				Arguments: []byte(`{}`),
			},
		}
	} else {
		events <- model.ModelStreamEvent{Kind: model.StreamTextDelta, Text: "回答"}
	}
	events <- model.ModelStreamEvent{Kind: model.StreamComplete, StopReason: m.stop}
	close(events)
	return events, nil
}

type committedInvocations struct {
	repository.ToolInvocationRepository
}

func (committedInvocations) FindByTaskCall(context.Context, contracts.TaskID, string) (toolmodel.ToolInvocation, error) {
	return toolmodel.ToolInvocation{Status: toolmodel.ToolInvocationAwaitingApproval}, nil
}

type committedCatalog struct{ toolcontracts.ToolCatalog }

func (committedCatalog) List() []ToolDefinition { return nil }

type committedTools struct{}

func (committedTools) Execute(context.Context, toolcontracts.PreparedToolCall, contracts.ToolInvocationContext) (toolcontracts.ToolExecutionResult, error) {
	return toolcontracts.ToolExecutionResult{ApprovalPending: true}, nil
}

type committedWriter struct {
	repository.MessageWriter
	inserted bool
	err      error
	writes   int
	cancel   context.CancelFunc
}

func (w *committedWriter) AppendAssistant(_ context.Context, value repository.AssistantWrite) (sessionmodel.MessageData, bool, error) {
	message := value.Message
	w.writes++
	w.cancel()
	message.Sequence = 10
	message.CreatedAt = time.Now().UTC()
	return message, w.inserted, w.err
}

func TestAssistantCommittedHook(t *testing.T) {
	for _, tc := range []struct {
		name     string
		stop     model.ModelStopReason
		inserted bool
		writeErr error
		notifies int
		outcome  taskmodel.TaskOutcome
		failure  contracts.TaskFailureCode
	}{
		{
			name:     "completed",
			stop:     model.StopCompleted,
			inserted: true,
			notifies: 1,
			outcome:  taskmodel.TaskCompleted,
		},
		{
			name:     "stop_sequence",
			stop:     model.StopSequence,
			inserted: true,
			notifies: 1,
			outcome:  taskmodel.TaskCompleted,
		},
		{name: "duplicate", stop: model.StopCompleted, outcome: taskmodel.TaskCompleted},
		{
			name:     "storage_failure",
			stop:     model.StopCompleted,
			writeErr: errors.New("write failed"),
			outcome:  taskmodel.TaskFailed,
			failure:  contracts.TaskFailureStorage,
		},
		{
			name:     "intermediate",
			stop:     model.StopToolCalls,
			inserted: true,
			outcome:  taskmodel.TaskAwaitingApproval,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			type traceKey struct{}
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), traceKey{}, "trace"))
			defer cancel()
			task := taskmodel.Task{
				ID:        "task",
				SessionID: "session",
				AgentID:   "agent",
				RequestID: "request",
				Config: taskmodel.Config{
					Model: model.Config{ContextWindow: 8192, MaxOutputTokens: 1024},
				},
			}
			provider := &committedModel{stop: tc.stop}
			// 最终消息提交后取消原请求，Hook 仍应收到未取消且保留值的 Context。
			cancelAfterWrite := func() {}
			if tc.stop == model.StopCompleted || tc.stop == model.StopSequence {
				cancelAfterWrite = cancel
			}
			writer := &committedWriter{inserted: tc.inserted, err: tc.writeErr, cancel: cancelAfterWrite}
			notifications, hookErrors := 0, 0
			dispatcher := hooks.NewDispatcherWithEvents(nil, loopCommittedHook(func(hookCtx context.Context, event hooks.CommittedEvent) error {
				notifications++
				if err := event.Message.Validate(); err != nil {
					t.Fatalf("hook must receive the complete message with sequence and timestamp: %v", err)
				}
				if writer.writes != 1 || hookCtx.Err() != nil || hookCtx.Value(traceKey{}) != "trace" {
					t.Error("hook must run after persistence with a context detached from cancellation")
				}
				if event.SessionID != task.SessionID || event.AgentID != task.AgentID || event.TaskID != task.ID ||
					event.TurnID != contracts.NewTurnID(task.ID, 1) || event.Message.StopReason != string(tc.stop) || event.Message.TextContent() != "回答" {
					t.Fatalf("incorrect final message event content: %+v", event)
				}
				return errors.New("hook failed")
			}))
			runner, err := NewRunner(Config{
				Invocations:    committedInvocations{},
				Progress:       &committedProgress{task: task},
				ModelBuilder:   provider,
				Tools:          committedCatalog{},
				ToolExecutor:   committedTools{},
				HookDispatcher: dispatcher,
				EventObserver:  func(Event) {},
				Logf: func(format string, _ ...any) {
					if strings.Contains(format, "assistant committed hook failed") {
						hookErrors++
					}
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			content := appcontext.Content{
				Entries: []appcontext.ContextEntry{{
					Kind:    appcontext.ContextEntryUserInput,
					Role:    appcontext.ContextRoleUser,
					Content: []appcontext.ContextBlock{{Kind: appcontext.ContextBlockText, Text: "你好"}},
				}},
			}
			outcome, failure, err := runner.Run(ctx, task, content, nil, writer)
			if outcome != tc.outcome || failure != tc.failure || (err != nil) != (tc.writeErr != nil) {
				t.Fatalf("hook must not change execution results: outcome=%s failure=%s err=%v", outcome, failure, err)
			}
			if notifications != tc.notifies || hookErrors != tc.notifies || provider.requests != 1 || writer.writes != 1 {
				t.Fatalf("incorrect notification or retry counts: notifications=%d logs=%d requests=%d writes=%d", notifications, hookErrors, provider.requests, writer.writes)
			}
		})
	}
}
