package task

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"praxis/internal/contracts"
	appcontext "praxis/internal/core/context"
	contextcompress "praxis/internal/core/context/compress"
	"praxis/internal/core/model"
	"praxis/internal/core/security"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	toolmodel "praxis/internal/core/tool_invocation"
	"praxis/internal/infra/toolconfig"
	"praxis/internal/loop"
	toolexecution "praxis/internal/tool_execution"
	toolcontracts "praxis/internal/tools/contracts"
)

func limitTask(t *testing.T, f *hookFixture) taskmodel.Task {
	t.Helper()
	result, err := f.svc.SendInput(context.Background(), hookInput())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ConfirmRunning(context.Background(), result.Task.ID); err != nil {
		t.Fatal(err)
	}
	value, err := f.svc.LoadTask(context.Background(), result.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestIterationReservationsSurviveRestart(t *testing.T) {
	f := newHookFixture(t)
	value := limitTask(t, f)
	ctx := context.Background()
	for i := uint64(1); i <= 29; i++ {
		if err := f.svc.RecordIteration(ctx, value.ID, i); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.svc.RecordSummaryRequest(ctx, value.ID, 30); err != nil {
		t.Fatal(err)
	}
	f.restart(t)
	value, err := f.svc.LoadTask(ctx, value.ID)
	if err != nil || value.Progress.Iterations != 29 || value.Progress.ModelRequests != 30 || value.Progress.NextSequence != 31 {
		t.Fatal(value.Progress, err)
	}
	if err := f.svc.RecordIteration(ctx, value.ID, 31); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.RecordIteration(ctx, value.ID, 32); !errors.Is(err, contracts.New(contracts.TaskResourceLimit, "")) {
		t.Fatal(err)
	}
	if err := f.svc.RecordSummaryRequest(ctx, value.ID, 30); !errors.Is(err, contracts.ErrRequestConflict) {
		t.Fatal("old sequence replay was accepted", err)
	}
	if err := f.svc.RecordIteration(ctx, value.ID, 31); !errors.Is(err, contracts.ErrRequestConflict) {
		t.Fatal("duplicate sequence was accepted", err)
	}
	value, err = f.svc.LoadTask(ctx, value.ID)
	if err != nil || value.Progress.Iterations != 30 || value.Progress.ModelRequests != 31 {
		t.Fatal(value.Progress, err)
	}
}

func TestConcurrentIterationReservation(t *testing.T) {
	f := newHookFixture(t)
	value := limitTask(t, f)
	var successes atomic.Int32
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			err := f.svc.RecordIteration(context.Background(), value.ID, 1)
			if err == nil {
				successes.Add(1)
			} else if !errors.Is(err, contracts.ErrRequestConflict) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatal(successes.Load())
	}
}

type limitTool struct {
	normalizations, executions atomic.Int32
	invalid                    bool
	name                       contracts.ToolName
}

func (s *limitTool) Definition() contracts.ToolDefinition {
	name := s.name
	if name == "" {
		name = "read_file"
	}
	return contracts.ToolDefinition{Name: name, Description: "test", InputSchema: json.RawMessage(`{"type":"object"}`)}
}
func (s *limitTool) Normalize(call contracts.ToolCall, _ string) (toolcontracts.NormalizedToolCall, error) {
	s.normalizations.Add(1)
	if s.invalid {
		return toolcontracts.NormalizedToolCall{}, errors.New("invalid test arguments")
	}
	return toolcontracts.NormalizedToolCall{Name: call.Name, NormalizedArguments: call.Arguments}, nil
}
func (s *limitTool) Execute(context.Context, toolcontracts.ExecutionCall) (toolcontracts.ToolResult, error) {
	s.executions.Add(1)
	return toolcontracts.ToolResult{Payload: "ok"}, nil
}

type limitCatalog struct{ tool *limitTool }

func (s limitCatalog) Get(contracts.ToolName) (toolcontracts.Tool, bool) { return s.tool, true }
func (s limitCatalog) List() []contracts.ToolDefinition {
	return []contracts.ToolDefinition{s.tool.Definition()}
}

func limitExecutor(t *testing.T, f *hookFixture, tool *limitTool, action security.CommandAction) (*toolexecution.Executor, *toolconfig.Catalog) {
	t.Helper()
	catalog := toolconfig.NewCatalog(toolconfig.Config{Bash: security.CommandPolicy{DefaultAction: action}}, limitCatalog{tool})
	executor, err := toolexecution.NewExecutor(toolexecution.Config{
		CommandPolicy: catalog,
		Transactions:  f.store,
		Tasks:         f.repos.Tasks,
		Invocations:   f.repos.ToolInvocations,
		Catalog:       catalog,
	})
	if err != nil {
		t.Fatal(err)
	}
	return executor, catalog
}

type limitModel struct {
	requests   int
	calls      int
	completeAt int
	name       contracts.ToolName
	arguments  json.RawMessage
}

func (m *limitModel) BuildModel(model.Config) (model.Model, error) {
	return model.Model{Stream: m, MaxOutputTokens: 1024}, nil
}
func (m *limitModel) Stream(_ context.Context, _ model.ModelRequest) (<-chan model.ModelStreamEvent, error) {
	m.requests++
	name := m.name
	if name == "" {
		name = "read_file"
	}
	arguments := m.arguments
	if arguments == nil {
		arguments = []byte(`{}`)
	}
	events := make(chan model.ModelStreamEvent, m.calls+2)
	stop := model.StopToolCalls
	if m.completeAt == m.requests {
		events <- model.ModelStreamEvent{Kind: model.StreamTextDelta, Text: "done"}
		stop = model.StopCompleted
	} else {
		for i := range m.calls {
			events <- model.ModelStreamEvent{Kind: model.StreamToolCall, ToolCall: contracts.ToolCall{ID: fmt.Sprintf("call-%d-%d", m.requests, i), Name: name, Arguments: arguments}}
		}
	}
	events <- model.ModelStreamEvent{Kind: model.StreamComplete, StopReason: stop}
	close(events)
	return events, nil
}

func runLimitLoop(t *testing.T, f *hookFixture, value taskmodel.Task, provider *limitModel, tool *limitTool, continuation []sessionmodel.MessageData) (taskmodel.TaskOutcome, contracts.TaskFailureCode, error) {
	t.Helper()
	action := security.CommandAllow
	if tool.name == contracts.ToolBash {
		action = security.CommandAsk
	}
	executor, catalog := limitExecutor(t, f, tool, action)
	runner, err := loop.NewRunner(loop.Config{
		Progress:      f.svc,
		Invocations:   f.repos.ToolInvocations,
		ModelBuilder:  provider,
		Tools:         catalog,
		ToolExecutor:  executor,
		EventObserver: func(loop.Event) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	writer, err := f.repos.Messages.Writer(context.Background(), value.SessionID, value.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	content, restored, err := f.svc.LoadContext(context.Background(), value)
	if err != nil {
		t.Fatal(err)
	}
	if continuation == nil {
		continuation = restored
	}
	return runner.Run(context.Background(), value, content, continuation, writer)
}

func TestLoopIterationBoundaryAndFinalOutput(t *testing.T) {
	for _, complete := range []bool{true, false} {
		t.Run(fmt.Sprint(complete), func(t *testing.T) {
			f := newHookFixture(t)
			value := limitTask(t, f)
			provider := &limitModel{calls: 1}
			if complete {
				provider.completeAt = 30
			}
			tool := &limitTool{}
			outcome, code, err := runLimitLoop(t, f, value, provider, tool, nil)
			if complete {
				if err != nil || outcome != taskmodel.TaskCompleted || code != "" {
					t.Fatal(outcome, code, err)
				}
			} else if outcome != taskmodel.TaskFailed || code != contracts.TaskFailureResourceLimit || err == nil {
				t.Fatal(outcome, code, err)
			}
			if provider.requests != 30 {
				t.Fatal(provider.requests)
			}
			stored, err := f.svc.LoadTask(context.Background(), value.ID)
			if err != nil || stored.Progress.Iterations != 30 {
				t.Fatal(stored.Progress, err)
			}
			if !complete && tool.executions.Load() != 30 {
				t.Fatal("last allowed iteration did not settle its calls")
			}
			f.restart(t)
			provider = &limitModel{calls: 1}
			outcome, code, err = runLimitLoop(t, f, stored, provider, tool, nil)
			if provider.requests != 0 {
				t.Fatal("restart issued another model request")
			}
			if complete && outcome != taskmodel.TaskCompleted || !complete && code != contracts.TaskFailureResourceLimit {
				t.Fatal(outcome, code, err)
			}
		})
	}
}

func TestToolBatchLimitSettlesEveryDeclaredCall(t *testing.T) {
	f := newHookFixture(t)
	value := limitTask(t, f)
	value.Progress.ToolCalls = 98
	if err := f.store.InTx(context.Background(), func(ctx context.Context) error { return f.repos.Tasks.Save(ctx, value) }); err != nil {
		t.Fatal(err)
	}
	provider := &limitModel{calls: 5}
	tool := &limitTool{}
	outcome, code, err := runLimitLoop(t, f, value, provider, tool, nil)
	if err == nil || outcome != taskmodel.TaskFailed || code != contracts.TaskFailureResourceLimit {
		t.Fatal(outcome, code, err)
	}
	if tool.executions.Load() != 2 {
		t.Fatal(tool.executions.Load())
	}
	missing, err := f.repos.Messages.ListMissingToolResults(context.Background())
	if err != nil || len(missing) != 0 {
		t.Fatal(missing, err)
	}
	calls, err := f.repos.ToolInvocations.ListByTask(context.Background(), value.ID)
	if err != nil || len(calls) != 5 {
		t.Fatal(calls, err)
	}
	for _, call := range calls {
		if !call.Status.Terminal() {
			t.Fatal(call.Status)
		}
	}
	f.restart(t)
	value, err = f.svc.LoadTask(context.Background(), value.ID)
	if err != nil || value.Progress.ToolCalls != 100 {
		t.Fatal(value.Progress, err)
	}
}

func TestInvalidPreparationIsPersisted(t *testing.T) {
	f := newHookFixture(t)
	value := limitTask(t, f)
	provider := &limitModel{calls: 1, completeAt: 2}
	tool := &limitTool{invalid: true}
	outcome, _, err := runLimitLoop(t, f, value, provider, tool, nil)
	if err != nil || outcome != taskmodel.TaskCompleted || tool.executions.Load() != 0 {
		t.Fatal(outcome, err)
	}
	calls, err := f.repos.ToolInvocations.ListByTask(context.Background(), value.ID)
	if err != nil || len(calls) != 1 || calls[0].FailureCode != toolmodel.ToolFailureInvalidCall || !calls[0].BudgetReserved {
		t.Fatal(calls, err)
	}
}

func TestConcurrentToolAdmissionCannotExceedOneHundred(t *testing.T) {
	f := newHookFixture(t)
	ctx := context.Background()
	value := limitTask(t, f)
	value.Progress.ToolCalls = 98
	if err := f.store.InTx(ctx, func(ctx context.Context) error { return f.repos.Tasks.Save(ctx, value) }); err != nil {
		t.Fatal(err)
	}
	turn := contracts.NewTurnID(value.ID, 1)
	metadata := contracts.ToolInvocationContext{
		TaskID:        value.ID,
		SessionID:     value.SessionID,
		AgentID:       value.AgentID,
		TurnID:        turn,
		WorkspacePath: value.Config.WorkspacePath,
	}
	message := sessionmodel.MessageData{
		ID:         "assistant:" + turn.String(),
		TaskID:     value.ID.String(),
		RequestID:  value.RequestID.String(),
		TurnID:     turn.String(),
		Role:       sessionmodel.RoleAssistant,
		AuthorKind: sessionmodel.AuthorAgent,
		AuthorID:   value.AgentID.String(),
		StopReason: "tool_calls",
	}
	for i := range 16 {
		message.Blocks = append(message.Blocks, sessionmodel.Block{
			Kind:   sessionmodel.BlockToolCall,
			CallID: fmt.Sprintf("call-%d", i),
			Name:   "read_file",
			Input:  []byte(`{}`),
		})
	}
	writer, err := f.repos.Messages.Writer(ctx, value.SessionID, value.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	write := testAssistantWrite(t, message, metadata)
	if _, _, err := writer.AppendAssistant(ctx, write); err != nil {
		t.Fatal(err)
	}
	tool := &limitTool{}
	executor, _ := limitExecutor(t, f, tool, security.CommandAllow)
	var wg sync.WaitGroup
	for _, invocation := range write.ToolCalls {
		wg.Go(func() {
			call := contracts.ToolCall{ID: invocation.ProviderToolCallID, Name: invocation.Name, Arguments: invocation.Arguments}
			normalized, err := tool.Normalize(call, metadata.WorkspacePath)
			if err != nil {
				t.Error(err)
				return
			}
			result, err := executor.Execute(ctx, toolcontracts.PreparedToolCall{Call: call, Normalized: normalized}, metadata)
			if err != nil || result.ApprovalPending {
				t.Error(result, err)
			}
		})
	}
	wg.Wait()
	value, err = f.svc.LoadTask(ctx, value.ID)
	if err != nil || value.Progress.ToolCalls != 100 || tool.executions.Load() != 2 {
		t.Fatal(value.Progress, tool.executions.Load(), err)
	}
	calls, err := f.repos.ToolInvocations.ListByTask(ctx, value.ID)
	if err != nil {
		t.Fatal(err)
	}
	reserved := 0
	for _, call := range calls {
		if !call.Status.Terminal() {
			t.Fatal(call.Status)
		}
		if call.BudgetReserved {
			reserved++
		}
	}
	if reserved != 2 {
		t.Fatal(reserved)
	}
}

func TestApprovalRestartUsesPersistedPreparationAndReservation(t *testing.T) {
	f := newHookFixture(t)
	value := limitTask(t, f)
	tool := &limitTool{name: contracts.ToolBash}
	provider := &limitModel{calls: 1, name: contracts.ToolBash, arguments: []byte(`{"command":"echo test"}`)}
	outcome, code, err := runLimitLoop(t, f, value, provider, tool, nil)
	if err != nil || code != "" || outcome != taskmodel.TaskAwaitingApproval || tool.normalizations.Load() != 1 || tool.executions.Load() != 0 {
		t.Fatal(outcome, code, err)
	}
	f.restart(t)
	ctx := context.Background()
	invocations, err := f.repos.ToolInvocations.ListByTask(ctx, value.ID)
	if err != nil || len(invocations) != 1 {
		t.Fatal(invocations, err)
	}
	invocation := invocations[0]
	before := append([]byte(nil), invocation.NormalizedArguments...)
	if invocation.Status != toolmodel.ToolInvocationAwaitingApproval || !invocation.BudgetReserved {
		t.Fatal(invocation)
	}
	if err := f.store.InTx(ctx, func(ctx context.Context) error {
		approval := contracts.ApprovalRecord{Source: contracts.ApprovalSourceUser, ArgumentsDigest: toolmodel.ArgumentsDigest(invocation.Name, invocation.NormalizedArguments), ApprovedAt: time.Now()}
		if err := invocation.Approve(approval); err != nil {
			return err
		}
		return f.repos.ToolInvocations.Save(ctx, invocation)
	}); err != nil {
		t.Fatal(err)
	}
	f.restart(t)
	provider = &limitModel{completeAt: 1}
	outcome, code, err = runLimitLoop(t, f, value, provider, tool, nil)
	if err != nil || code != "" || outcome != taskmodel.TaskCompleted || tool.normalizations.Load() != 1 || tool.executions.Load() != 1 {
		t.Fatal(outcome, code, err, tool.normalizations.Load(), tool.executions.Load())
	}
	stored, err := f.svc.LoadTask(ctx, value.ID)
	if err != nil || stored.Progress.ToolCalls != 1 || stored.Progress.Iterations != 2 {
		t.Fatal(stored.Progress, err)
	}
	invocation, err = f.repos.ToolInvocations.Get(ctx, invocation.ID)
	if err != nil || !bytes.Equal(before, invocation.NormalizedArguments) {
		t.Fatal("approved arguments changed", err)
	}
	if _, _, err := runLimitLoop(t, f, stored, provider, tool, nil); err != nil || tool.executions.Load() != 1 || tool.normalizations.Load() != 1 {
		t.Fatal("completed call replayed", err)
	}
}

type summaryStrategy struct{ calls int }

func (s *summaryStrategy) Compress(ctx context.Context, value appcontext.ModelContext, _ contextcompress.Options, summarize contextcompress.SummarizeFunc) (appcontext.ModelContext, bool, error) {
	s.calls++
	text, err := summarize(ctx, value, 128)
	if err != nil {
		return appcontext.ModelContext{}, false, err
	}
	if text != "summary" {
		return appcontext.ModelContext{}, false, fmt.Errorf("summary boundary did not normalize text: %q", text)
	}
	return value, false, nil
}

type summaryModel struct{ sequences []contracts.TurnID }

func (s *summaryModel) BuildModel(model.Config) (model.Model, error) {
	return model.Model{Stream: s, MaxOutputTokens: 1024}, nil
}
func (s *summaryModel) Stream(_ context.Context, request model.ModelRequest) (<-chan model.ModelStreamEvent, error) {
	s.sequences = append(s.sequences, request.TurnID)
	text := "done"
	if len(request.Tools) == 0 {
		text = "  summary \n"
	}
	events := make(chan model.ModelStreamEvent, 2)
	events <- model.ModelStreamEvent{Kind: model.StreamTextDelta, Text: text}
	events <- model.ModelStreamEvent{Kind: model.StreamComplete, StopReason: model.StopCompleted}
	close(events)
	return events, nil
}

func TestLoopSummaryRequestsHaveSeparateCountersAndUniqueTurns(t *testing.T) {
	f := newHookFixture(t)
	value := limitTask(t, f)
	tool := &limitTool{}
	executor, catalog := limitExecutor(t, f, tool, security.CommandAllow)
	provider := &summaryModel{}
	strategy := &summaryStrategy{}
	runner, err := loop.NewRunner(loop.Config{
		Progress:            f.svc,
		Invocations:         f.repos.ToolInvocations,
		ModelBuilder:        provider,
		Tools:               catalog,
		ToolExecutor:        executor,
		CompressionStrategy: strategy,
		EventObserver:       func(loop.Event) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	content, continuation, err := f.svc.LoadContext(context.Background(), value)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := f.repos.Messages.Writer(context.Background(), value.SessionID, value.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	outcome, code, err := runner.Run(context.Background(), value, content, continuation, writer)
	if err != nil || outcome != taskmodel.TaskCompleted || code != "" {
		t.Fatal(outcome, code, err)
	}
	stored, err := f.svc.LoadTask(context.Background(), value.ID)
	if err != nil || stored.Progress.Iterations != 1 || stored.Progress.ModelRequests != 2 || stored.Progress.NextSequence != 3 {
		t.Fatal(stored.Progress, err)
	}
	if len(provider.sequences) != 2 || provider.sequences[0] == provider.sequences[1] || provider.sequences[1] != contracts.NewTurnID(value.ID, 2) {
		t.Fatal(provider.sequences)
	}
}
