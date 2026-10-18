package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"praxis/internal/core/message"
	"praxis/internal/core/model"
	modelconfig "praxis/internal/core/model/config"
	"praxis/internal/core/task"
	"praxis/internal/core/tool"
	"praxis/internal/core/toolcall"
	"praxis/internal/identity"
	"praxis/internal/infra/jsonl"
	"praxis/internal/logging"
	"praxis/internal/security"
	"praxis/internal/service/agent"
	execution "praxis/internal/service/agent/coordinator"
	input "praxis/internal/service/agent/input"
	"praxis/internal/service/project"
	"praxis/internal/service/session"
	queue "praxis/internal/service/task"
	toolservice "praxis/internal/service/toolcall"
	"praxis/internal/workspace"
)

type fixtureModel struct {
	requests chan model.ModelRequest
	release  chan struct{}
	failure  atomic.Bool
	tools    atomic.Bool
	calls    atomic.Int32
}

func (m *fixtureModel) ResolveModelConfig(string, string, string) (model.Config, error) {
	return model.Config{
		ProviderID:      "test",
		ModelID:         "test",
		APIFormat:       string(modelconfig.APIFormatOpenAIResponses),
		ContextWindow:   32768,
		MaxOutputTokens: 1024,
		BaseURL:         "https://example.com",
	}, nil
}
func (m *fixtureModel) BuildModel(c model.Config) (model.Model, error) {
	return model.Model{Stream: m, MaxOutputTokens: c.MaxOutputTokens}, nil
}
func (m *fixtureModel) Stream(ctx context.Context, req model.ModelRequest) (<-chan model.ModelStreamEvent, error) {
	m.requests <- req
	select {
	case <-m.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if m.failure.Load() {
		return nil, errors.New("provider unavailable")
	}
	out := make(chan model.ModelStreamEvent, 3)
	read, creation, output := int64(32), int64(8), int64(0)
	out <- model.ModelStreamEvent{Kind: model.StreamUsage, Usage: &model.ModelUsage{
		InputTokens:              100,
		OutputTokens:             &output,
		CacheReadInputTokens:     &read,
		CacheCreationInputTokens: &creation,
	}}
	if m.tools.Load() && m.calls.Add(1) == 1 {
		out <- model.ModelStreamEvent{Kind: model.StreamToolCall, ToolCall: tool.ToolCall{ID: "call-1", Name: tool.ToolReadFile, Arguments: []byte(`{"path":"test.txt"}`)}}
		out <- model.ModelStreamEvent{Kind: model.StreamComplete, StopReason: model.StopToolCalls}
	} else {
		out <- model.ModelStreamEvent{Kind: model.StreamTextDelta, Text: "answer"}
		out <- model.ModelStreamEvent{Kind: model.StreamComplete, StopReason: model.StopCompleted}
	}
	close(out)
	return out, nil
}

type fixtureDefinitions struct{}

func (fixtureDefinitions) Definition(id identity.AgentDefinitionID) (agent.AgentDefinition, error) {
	return agent.AgentDefinition{
		ID:           id,
		Profile:      agent.ProfilePrimary,
		Instructions: "system",
		Revision:     "revision",
	}, nil
}
func (fixtureDefinitions) ResolveModelReference(identity.AgentDefinitionID) (agent.ModelReference, error) {
	return agent.ModelReference{ProviderID: "test", ModelID: "test"}, nil
}

type fixturePrimary struct{ agents agent.Store }

func (p fixturePrimary) GetOrCreatePrimaryAgent(ctx context.Context, _ identity.SessionID, _ identity.RequestID) (agent.Agent, error) {
	return p.agents.Get(ctx, "agent")
}

type fixtureTool struct{ executed atomic.Int32 }

func (*fixtureTool) Definition() tool.ToolDefinition {
	return tool.ToolDefinition{Name: tool.ToolReadFile, Description: "read", InputSchema: []byte(`{"type":"object"}`)}
}
func (*fixtureTool) Normalize(call tool.ToolCall, root string) (tool.NormalizedToolCall, error) {
	return tool.NormalizedToolCall{Name: call.Name, NormalizedArguments: call.Arguments, Path: filepath.Join(root, "test.txt")}, nil
}
func (v *fixtureTool) Execute(context.Context, tool.ExecutionCall) (tool.ToolResult, error) {
	v.executed.Add(1)
	return tool.ToolResult{Payload: "contents"}, nil
}

type executionFixture struct {
	store       *jsonl.Store
	operations  *input.Service
	queue       *queue.Service
	coordinator *execution.Coordinator
	model       *fixtureModel
	tool        *fixtureTool
	active      *execution.ActiveTasks
	root        string
}

func newExecutionFixture(t *testing.T) *executionFixture {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	s, err := jsonl.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	f := &executionFixture{
		store:  s,
		root:   root,
		model:  &fixtureModel{requests: make(chan model.ModelRequest, 16), release: make(chan struct{}, 16)},
		tool:   &fixtureTool{},
		active: &execution.ActiveTasks{},
	}
	repos := s.Repositories()
	at := time.Now().UTC()
	err = s.InTx(ctx, func(ctx context.Context) error {
		if err := repos.Projects.Save(ctx, project.NewProject("project", "test", root, "workspace", at)); err != nil {
			return err
		}
		if err := repos.Workspaces.Save(ctx, workspace.NewWorkspace("workspace", "project", workspace.WorkspaceProjectRoot, root, at)); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = s.InTx(ctx, func(ctx context.Context) error {
		if err := repos.Sessions.Save(ctx, session.NewSession("session", "project", "workspace", at)); err != nil {
			return err
		}
		return repos.Agents.Save(ctx, agent.NewAgent("agent", "session", agent.DefinitionPrimary, agent.ProfilePrimary, at))
	})
	if err != nil {
		t.Fatal(err)
	}
	f.operations, err = input.NewService(input.Dependencies{
		Transactions:   s,
		Controls:       repos.Controls,
		Workspaces:     repos.Workspaces,
		Sessions:       repos.Sessions,
		Agents:         repos.Agents,
		Records:        repos.Executions,
		Queue:          repos.Tasks,
		ToolExecutions: repos.ToolExecutions,
		Executions:     f.active,
		Messages:       repos.Messages,
		PrimaryAgent:   fixturePrimary{repos.Agents},
		Definitions:    fixtureDefinitions{},
		Models:         f.model,
		Contexts:       repos.Contexts,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.queue, err = queue.NewService(queue.Dependencies{
		Transactions: s,
		Tasks:        repos.Tasks,
		Agents:       repos.Agents,
		Messages:     repos.Messages,
	})
	if err != nil {
		t.Fatal(err)
	}
	registry := tool.NewToolRegistry()
	if err = registry.Register(f.tool); err != nil {
		t.Fatal(err)
	}
	catalog := tool.NewCatalog(tool.Config{Tools: map[tool.ToolName]tool.ToolConfig{tool.ToolReadFile: {Enabled: true}}}, registry)
	f.coordinator, err = assembleCoordinator(coordinatorDependencies{
		Transactions:   s,
		Tasks:          f.operations,
		Queue:          f.queue,
		ActiveTasks:    f.active,
		ToolExecutions: repos.ToolExecutions,
		Messages:       repos.Messages,
		Writers:        repos.Messages,
		Models:         f.model,
		Tools:          catalog,
		Events:         execution.NewEventPublisher(),
		ObserveUsage:   usageObserver(f.store, logging.NewFactory().Nop()),
		ConfigMu:       &sync.RWMutex{},
		Recovery:       execution.RecoveryCatalog{Executions: repos.Executions, Sessions: repos.Sessions, Agents: repos.Agents},
		Rules: toolservice.ApprovalRules{Catalog: catalog, DefaultMode: func(context.Context, identity.SessionID) (security.ApprovalMode, error) {
			return security.ApprovalAsk, nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := f.coordinator.Close(ctx); err != nil {
			t.Error(err)
		}
		if err := f.store.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return f
}
func fixtureInput(request string) input.SendInput {
	return input.SendInput{
		SessionID:  "session",
		AgentID:    "agent",
		RequestID:  identity.RequestID(request),
		Content:    request,
		ProviderID: "test",
		ModelID:    "test",
	}
}
func (f *executionFixture) enqueue(t *testing.T, id string) task.Task {
	t.Helper()
	v, err := f.queue.EnqueueTask(context.Background(), queue.EnqueueInput{
		ID:         identity.TaskID(id),
		RequestID:  identity.RequestID(id),
		AgentID:    "agent",
		Prompt:     id,
		ProviderID: "test",
		ModelID:    "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	return v.Task
}
func waitUntil(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(time.Millisecond * 5)
	}
	t.Fatal("condition was not reached")
}
func (f *executionFixture) waitEnded(t *testing.T, id identity.AgentExecutionID) agent.Execution {
	t.Helper()
	var v agent.Execution
	waitUntil(t, func() bool {
		v, _ = f.store.Repositories().Executions.Get(context.Background(), id)
		return v.Status == agent.ExecutionEnded
	})
	return v
}
func TestDirectAndFIFOExecutionIdentity(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	first, err := f.coordinator.SendInput(ctx, fixtureInput("first"))
	if err != nil {
		t.Fatal(err)
	}
	req := <-f.model.requests
	if req.ExecutionID != first.Execution.ID || req.PromptCacheKey != "session" {
		t.Fatal("execution identity or prompt cache key changed")
	}
	queued, _ := f.store.Repositories().Tasks.ListByAgent(ctx, "agent", 0)
	if len(queued) != 0 {
		t.Fatal("direct input created a Task")
	}
	repeated, err := f.coordinator.SendInput(ctx, fixtureInput("first"))
	if err != nil || !repeated.ExistingRequest || repeated.Execution.ID != first.Execution.ID {
		t.Fatal("direct input is not idempotent", err)
	}
	f.enqueue(t, "queue-1")
	f.enqueue(t, "queue-1")
	f.enqueue(t, "queue-2")
	f.model.release <- struct{}{}
	req = <-f.model.requests
	encodedContext, _ := json.Marshal(req.Context)
	if bytes.Contains(encodedContext, []byte("queue-2")) {
		t.Fatal("later queued input leaked into frozen context")
	}
	second, _ := f.store.Repositories().Executions.Get(ctx, req.ExecutionID)
	if second.SourceTaskID != "queue-1" {
		t.Fatal("FIFO order changed")
	}
	if f.waitEnded(t, first.Execution.ID).Outcome != agent.ExecutionCompleted {
		t.Fatal("previous execution was not settled")
	}
	if second.ContextSelection.MessageSequenceBoundary == 0 {
		t.Fatal("context boundary was not frozen")
	}
	f.model.release <- struct{}{}
	req = <-f.model.requests
	third, _ := f.store.Repositories().Executions.Get(ctx, req.ExecutionID)
	if third.SourceTaskID != "queue-2" {
		t.Fatal("second queued input was lost")
	}
	f.model.release <- struct{}{}
	f.waitEnded(t, third.ID)
	usage, err := (jsonl.ModelUsageStore{Store: f.store}).ListSession(ctx, "session")
	if err != nil || len(usage) != 3 {
		t.Fatal("usage records lost execution identity", err)
	}
	for _, record := range usage {
		if record.ExecutionID == "" || record.Usage.CacheReadInputTokens == nil || *record.Usage.CacheReadInputTokens != 32 || record.Usage.OutputTokens == nil || *record.Usage.OutputTokens != 0 {
			t.Fatal("usage counters lost known zero or cache counts")
		}
	}
	values, _ := f.store.Repositories().Tasks.ListByAgent(ctx, "agent", 0)
	if len(values) != 2 {
		t.Fatal("duplicate queue request created another task")
	}
	for _, v := range values {
		if v.Status != task.Dispatched || v.ExecutionID == "" {
			t.Fatal("dispatch did not commit atomically")
		}
	}
}
func TestProviderFailureKeepsQueuePending(t *testing.T) {
	f := newExecutionFixture(t)
	f.model.failure.Store(true)
	v, err := f.coordinator.SendInput(context.Background(), fixtureInput("first"))
	if err != nil {
		t.Fatal(err)
	}
	<-f.model.requests
	f.enqueue(t, "later")
	f.model.release <- struct{}{}
	if f.waitEnded(t, v.Execution.ID).Outcome != agent.ExecutionFailed {
		t.Fatal("provider error was not settled as execution failure")
	}
	queued, _ := f.store.Repositories().Tasks.Get(context.Background(), "later")
	if queued.Status != task.Pending || queued.ExecutionID != "" {
		t.Fatal("provider failure dispatched next task")
	}
}
func TestApprovalKeepsExecutionAndQueue(t *testing.T) {
	f := newExecutionFixture(t)
	f.model.tools.Store(true)
	v, err := f.coordinator.SendInput(context.Background(), fixtureInput("first"))
	if err != nil {
		t.Fatal(err)
	}
	<-f.model.requests
	f.enqueue(t, "later")
	f.model.release <- struct{}{}
	var pending []toolcall.Execution
	waitUntil(t, func() bool {
		pending, _ = f.coordinator.ListToolApprovals(context.Background(), "agent")
		return len(pending) == 1
	})
	if pending[0].ExecutionID != v.Execution.ID || f.tool.executed.Load() != 0 {
		t.Fatal("approval changed identity or executed early")
	}
	decision := execution.ToolDecision{
		SessionID:       "session",
		AgentID:         "agent",
		ExecutionID:     v.Execution.ID,
		CallID:          pending[0].ProviderToolCallID,
		ArgumentsDigest: toolcall.ArgumentsDigest(pending[0].Name, pending[0].NormalizedArguments),
	}
	if err = f.coordinator.ApproveToolCall(context.Background(), decision); err != nil {
		t.Fatal(err)
	}
	req := <-f.model.requests
	if req.ExecutionID != v.Execution.ID {
		t.Fatal("approval dispatched another execution")
	}
	f.model.release <- struct{}{}
	f.waitEnded(t, v.Execution.ID)
	if f.tool.executed.Load() != 1 {
		t.Fatal("approved tool was not executed once")
	}
	<-f.model.requests
	f.model.release <- struct{}{}
}

// settlementFailure 只模拟结算失败，模型与工具仍使用真实的持久化业务链路。
type settlementFailure struct {
	*input.Service
	remaining atomic.Int32
	attempts  atomic.Int32
}

func (s *settlementFailure) Settle(ctx context.Context, id identity.AgentExecutionID, outcome agent.ExecutionOutcome, code agent.ExecutionFailureCode, message string) (agent.Execution, error) {
	s.attempts.Add(1)
	if s.remaining.Add(-1) >= 0 {
		return agent.Execution{}, errors.New("injected settlement failure")
	}
	return s.Service.Settle(ctx, id, outcome, code, message)
}
func TestSettlementRetryDoesNotReplayModel(t *testing.T) {
	f := newExecutionFixture(t)
	if err := f.coordinator.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	wrapper := &settlementFailure{Service: f.operations}
	wrapper.remaining.Store(1)
	repos := f.store.Repositories()
	runner, err := buildFixtureRunner(f, wrapper)
	if err != nil {
		t.Fatal(err)
	}
	f.coordinator = runner
	v, err := f.coordinator.SendInput(context.Background(), fixtureInput("first"))
	if err != nil {
		t.Fatal(err)
	}
	<-f.model.requests
	f.enqueue(t, "later")
	f.model.release <- struct{}{}
	waitUntil(t, func() bool { return wrapper.attempts.Load() > 0 })
	pending, _ := repos.Tasks.Get(context.Background(), "later")
	if pending.Status != task.Pending {
		t.Fatal("queue advanced before settlement committed")
	}
	f.waitEnded(t, v.Execution.ID)
	req := <-f.model.requests
	if req.ExecutionID == v.Execution.ID {
		t.Fatal("settlement retried the model")
	}
	f.model.release <- struct{}{}
	if wrapper.attempts.Load() < 2 {
		t.Fatal("settlement was not retried")
	}
}
func buildFixtureRunner(f *executionFixture, ops execution.ExecutionOperations) (*execution.Coordinator, error) {
	repos := f.store.Repositories()
	registry := tool.NewToolRegistry()
	if err := registry.Register(f.tool); err != nil {
		return nil, err
	}
	catalog := tool.NewCatalog(tool.Config{Tools: map[tool.ToolName]tool.ToolConfig{tool.ToolReadFile: {Enabled: true}}}, registry)
	return assembleCoordinator(coordinatorDependencies{
		Transactions:   f.store,
		Tasks:          ops,
		Queue:          f.queue,
		ActiveTasks:    f.active,
		ToolExecutions: repos.ToolExecutions,
		Messages:       repos.Messages,
		Writers:        repos.Messages,
		Models:         f.model,
		Tools:          catalog,
		Events:         execution.NewEventPublisher(),
		ObserveUsage:   usageObserver(f.store, logging.NewFactory().Nop()),
		ConfigMu:       &sync.RWMutex{},
		Recovery:       execution.RecoveryCatalog{Executions: repos.Executions, Sessions: repos.Sessions, Agents: repos.Agents},
		Rules: toolservice.ApprovalRules{Catalog: catalog, DefaultMode: func(context.Context, identity.SessionID) (security.ApprovalMode, error) {
			return security.ApprovalAsk, nil
		}},
	})
}
func TestRestartKeepsIdentityAndBudget(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	v, err := f.operations.SendInput(ctx, fixtureInput("first"))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.operations.RecordIteration(ctx, v.Execution.ID, 1); err != nil {
		t.Fatal(err)
	}
	reopenFixture(t, f)
	if err = f.coordinator.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	req := <-f.model.requests
	if req.ExecutionID != v.Execution.ID || req.TurnID != identity.NewTurnID(v.Execution.ID, 2) {
		t.Fatal("restart reset identity or request sequence")
	}
	f.model.release <- struct{}{}
	ended := f.waitEnded(t, v.Execution.ID)
	if ended.Progress.Iterations != 2 {
		t.Fatal("restart reset persisted iteration budget")
	}
}
func TestPauseAndDelayedControl(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	repos := f.store.Repositories()
	service, err := agent.NewService(agent.Dependencies{
		Transactions:     f.store,
		Agents:           repos.Agents,
		ExecutionRecords: repos.Executions,
		Queue:            repos.Tasks,
		Controls:         repos.Controls,
		Executions:       f.coordinator,
		Messages:         repos.Messages,
	})
	if err != nil {
		t.Fatal(err)
	}
	v, err := f.coordinator.SendInput(ctx, fixtureInput("first"))
	if err != nil {
		t.Fatal(err)
	}
	<-f.model.requests
	f.enqueue(t, "later")
	_, err = service.PauseAgent(ctx, agent.ControlInput{CommandID: "pause-1", AgentID: "agent", TargetExecutionID: v.Execution.ID})
	if err != nil {
		t.Fatal(err)
	}
	if f.waitEnded(t, v.Execution.ID).Outcome != agent.ExecutionPaused {
		t.Fatal("committed pause did not win settlement")
	}
	next, err := f.coordinator.SendInput(ctx, fixtureInput("next"))
	if err != nil {
		t.Fatal(err)
	}
	<-f.model.requests
	_, err = service.StopAgent(ctx, agent.ControlInput{CommandID: "late-stop", AgentID: "agent", TargetExecutionID: v.Execution.ID})
	if err != nil {
		t.Fatal(err)
	}
	active, _ := repos.Agents.Get(ctx, "agent")
	if active.CurrentExecutionID != next.Execution.ID {
		t.Fatal("old control affected a later execution")
	}
	f.model.release <- struct{}{}
	f.waitEnded(t, next.Execution.ID)
	// 成功后可继续之前的预约。
	<-f.model.requests
	f.model.release <- struct{}{}
}

func reopenFixture(t *testing.T, f *executionFixture) {
	t.Helper()
	ctx := context.Background()
	if err := f.coordinator.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Close(ctx); err != nil {
		t.Fatal(err)
	}
	var err error
	f.store, err = jsonl.Open(ctx, f.root)
	if err != nil {
		t.Fatal(err)
	}
	f.active = &execution.ActiveTasks{}
	repos := f.store.Repositories()
	f.operations, err = input.NewService(input.Dependencies{
		Transactions:   f.store,
		Controls:       repos.Controls,
		Workspaces:     repos.Workspaces,
		Sessions:       repos.Sessions,
		Agents:         repos.Agents,
		Records:        repos.Executions,
		Queue:          repos.Tasks,
		ToolExecutions: repos.ToolExecutions,
		Executions:     f.active,
		Messages:       repos.Messages,
		PrimaryAgent:   fixturePrimary{repos.Agents},
		Definitions:    fixtureDefinitions{},
		Models:         f.model,
		Contexts:       repos.Contexts,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.queue, err = queue.NewService(queue.Dependencies{
		Transactions: f.store,
		Tasks:        repos.Tasks,
		Agents:       repos.Agents,
		Messages:     repos.Messages,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.coordinator, err = buildFixtureRunner(f, f.operations)
	if err != nil {
		t.Fatal(err)
	}
}
func TestRestartMarksUnknownToolWithoutReplay(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	repos := f.store.Repositories()
	v, err := f.operations.SendInput(ctx, fixtureInput("first"))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.operations.ConfirmRunning(ctx, v.Execution.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.operations.RecordIteration(ctx, v.Execution.ID, 1); err != nil {
		t.Fatal(err)
	}
	turn := identity.NewTurnID(v.Execution.ID, 1)
	args := []byte(`{"path":"test.txt"}`)
	call := toolcall.NewExecution("unknown-call", v.Execution.ID, turn, "session", "agent", "call-unknown", tool.ToolReadFile, args, toolcall.ArgumentsDigest(tool.ToolReadFile, args), time.Now().UTC())
	writer, err := repos.Messages.Writer(ctx, "session", "agent")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = writer.AppendAssistant(ctx, toolcall.AssistantWrite{Message: message.Message{
		ID:          "assistant:" + turn.String(),
		RequestID:   "first",
		ExecutionID: v.Execution.ID.String(),
		TurnID:      turn.String(),
		Role:        message.RoleAssistant,
		AuthorKind:  message.AuthorAgent,
		AuthorID:    "agent",
		StopReason:  string(model.StopToolCalls),
		Blocks: []message.Block{{
			Kind:   message.BlockToolCall,
			CallID: "call-unknown",
			Name:   string(tool.ToolReadFile),
			Input:  args,
		}},
	}, ToolCalls: []toolcall.Execution{call}})
	if err != nil {
		t.Fatal(err)
	}
	if err = call.Prepare(args, filepath.Join(f.root, "test.txt")); err != nil {
		t.Fatal(err)
	}
	if err = f.store.InTx(ctx, func(ctx context.Context) error { return repos.ToolExecutions.Save(ctx, call) }); err != nil {
		t.Fatal(err)
	}
	if err = call.ApplyAuthorization(security.ApprovalDecision{Action: security.ApprovalYolo, RuleID: "test"}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err = f.store.InTx(ctx, func(ctx context.Context) error { return repos.ToolExecutions.Save(ctx, call) }); err != nil {
		t.Fatal(err)
	}
	if err = call.Start(time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err = f.store.InTx(ctx, func(ctx context.Context) error { return repos.ToolExecutions.Save(ctx, call) }); err != nil {
		t.Fatal(err)
	}
	reopenFixture(t, f)
	if err = f.coordinator.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	<-f.model.requests
	f.model.release <- struct{}{}
	f.waitEnded(t, v.Execution.ID)
	saved, err := f.store.Repositories().ToolExecutions.Get(ctx, "unknown-call")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != toolcall.StatusUnknown || f.tool.executed.Load() != 0 {
		t.Fatal("unknown side effect was replayed")
	}
}

type failingTransaction struct {
	store *jsonl.Store
	fail  atomic.Bool
}

func (tx *failingTransaction) InTx(ctx context.Context, fn func(context.Context) error) error {
	return tx.store.InTx(ctx, func(ctx context.Context) error {
		if err := fn(ctx); err != nil {
			return err
		}
		if tx.fail.Swap(false) {
			return errors.New("injected transaction rollback")
		}
		return nil
	})
}
func TestAdmissionAndDispatchRollbackReleaseReservation(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	repos := f.store.Repositories()
	tx := &failingTransaction{store: f.store}
	operations, err := input.NewService(input.Dependencies{
		Transactions:   tx,
		Controls:       repos.Controls,
		Workspaces:     repos.Workspaces,
		Sessions:       repos.Sessions,
		Agents:         repos.Agents,
		Records:        repos.Executions,
		Queue:          repos.Tasks,
		ToolExecutions: repos.ToolExecutions,
		Executions:     f.active,
		Messages:       repos.Messages,
		PrimaryAgent:   fixturePrimary{repos.Agents},
		Definitions:    fixtureDefinitions{},
		Models:         f.model,
		Contexts:       repos.Contexts,
	})
	if err != nil {
		t.Fatal(err)
	}
	tx.fail.Store(true)
	if _, err = operations.SendInput(ctx, fixtureInput("first")); err == nil {
		t.Fatal("admission committed despite rollback")
	}
	records, _ := repos.Executions.ListByAgent(ctx, "agent", 0)
	stream, _ := repos.Messages.LoadMessages(ctx, "session", "agent", 0)
	if len(records) != 0 || len(stream.Messages) != 0 {
		t.Fatal("rollback left execution or message facts")
	}
	v, err := operations.SendInput(ctx, fixtureInput("first"))
	if err != nil {
		t.Fatal("rollback left an active reservation", err)
	}
	f.enqueue(t, "later")
	if _, err = operations.Settle(ctx, v.Execution.ID, agent.ExecutionCompleted, "", ""); err != nil {
		t.Fatal(err)
	}
	f.active.Release("agent", v.Execution.ID)
	tx.fail.Store(true)
	if _, _, err = operations.PrepareNext(ctx, "agent"); err == nil {
		t.Fatal("dispatch committed despite rollback")
	}
	pending, _ := repos.Tasks.Get(ctx, "later")
	active, _ := repos.Executions.ListActive(ctx)
	if pending.Status != task.Pending || pending.ExecutionID != "" || len(active) != 0 {
		t.Fatal("dispatch rollback left partial facts")
	}
	next, prepared, err := operations.PrepareNext(ctx, "agent")
	if err != nil || !prepared || next.SourceTaskID != "later" {
		t.Fatal("dispatch rollback left a reservation", err)
	}
}
func TestIterationBudgetStopsBeforeProvider(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	v, err := f.operations.SendInput(ctx, fixtureInput("first"))
	if err != nil {
		t.Fatal(err)
	}
	for sequence := uint64(1); sequence <= agent.MaxIterations; sequence++ {
		if err = f.operations.RecordIteration(ctx, v.Execution.ID, sequence); err != nil {
			t.Fatal(err)
		}
	}
	if err = f.coordinator.Activate(ctx, v.Execution); err != nil {
		t.Fatal(err)
	}
	ended := f.waitEnded(t, v.Execution.ID)
	if ended.FailureCode != agent.ExecutionFailureResourceLimit || len(f.model.requests) != 0 {
		t.Fatal("persisted iteration budget was bypassed")
	}
}
func TestDeniedToolResumesWithoutExecuting(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	f.model.tools.Store(true)
	v, err := f.coordinator.SendInput(ctx, fixtureInput("first"))
	if err != nil {
		t.Fatal(err)
	}
	<-f.model.requests
	f.model.release <- struct{}{}
	var pending []toolcall.Execution
	waitUntil(t, func() bool { pending, _ = f.coordinator.ListToolApprovals(ctx, "agent"); return len(pending) == 1 })
	decision := execution.ToolDecision{
		SessionID:       "session",
		AgentID:         "agent",
		ExecutionID:     v.Execution.ID,
		CallID:          pending[0].ProviderToolCallID,
		ArgumentsDigest: "wrong",
	}
	if err = f.coordinator.ApproveToolCall(ctx, decision); err == nil {
		t.Fatal("approval ignored the frozen parameter digest")
	}
	decision.ArgumentsDigest = toolcall.ArgumentsDigest(pending[0].Name, pending[0].NormalizedArguments)
	if err = f.coordinator.DenyToolCall(ctx, decision); err != nil {
		t.Fatal(err)
	}
	req := <-f.model.requests
	if req.ExecutionID != v.Execution.ID {
		t.Fatal("denial changed execution identity")
	}
	f.model.release <- struct{}{}
	f.waitEnded(t, v.Execution.ID)
	if f.tool.executed.Load() != 0 {
		t.Fatal("denied tool was executed")
	}
}

func TestStopRetainsPendingQueue(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	repos := f.store.Repositories()
	service, err := agent.NewService(agent.Dependencies{
		Transactions:     f.store,
		Agents:           repos.Agents,
		ExecutionRecords: repos.Executions,
		Queue:            repos.Tasks,
		Controls:         repos.Controls,
		Executions:       f.coordinator,
		Messages:         repos.Messages,
	})
	if err != nil {
		t.Fatal(err)
	}
	v, err := f.coordinator.SendInput(ctx, fixtureInput("first"))
	if err != nil {
		t.Fatal(err)
	}
	<-f.model.requests
	f.enqueue(t, "later")
	if _, err = service.StopAgent(ctx, agent.ControlInput{CommandID: "stop", AgentID: "agent", TargetExecutionID: v.Execution.ID}); err != nil {
		t.Fatal(err)
	}
	ended := f.waitEnded(t, v.Execution.ID)
	if ended.Outcome != agent.ExecutionInterrupted || ended.FailureCode != agent.ExecutionFailureRequestCanceled {
		t.Fatal("stop did not win settlement")
	}
	pending, _ := repos.Tasks.Get(ctx, "later")
	if pending.Status != task.Pending {
		t.Fatal("stop dispatched pending task")
	}
}

func TestRestartPreservesApprovalWait(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	f.model.tools.Store(true)
	v, err := f.coordinator.SendInput(ctx, fixtureInput("first"))
	if err != nil {
		t.Fatal(err)
	}
	<-f.model.requests
	f.model.release <- struct{}{}
	waitUntil(t, func() bool {
		saved, _ := f.store.Repositories().Executions.Get(ctx, v.Execution.ID)
		return saved.Status == agent.ExecutionWaitingApproval
	})
	// 捕获进程在审批等待时已提交的磁盘事实，随后从独立快照模拟重启。
	snapshot := t.TempDir()
	err = filepath.WalkDir(f.root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(f.root, path)
		if err != nil {
			return err
		}
		target := filepath.Join(snapshot, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	f.root = snapshot
	reopenFixture(t, f)
	if err = f.coordinator.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, func() bool {
		saved, _ := f.store.Repositories().Executions.Get(ctx, v.Execution.ID)
		return saved.Status == agent.ExecutionWaitingApproval
	})
	pending, err := f.coordinator.ListToolApprovals(ctx, "agent")
	if err != nil || len(pending) != 1 || pending[0].ExecutionID != v.Execution.ID || f.tool.executed.Load() != 0 {
		t.Fatal("restart ended or replayed approval wait", err)
	}
	decision := execution.ToolDecision{
		SessionID:       "session",
		AgentID:         "agent",
		ExecutionID:     v.Execution.ID,
		CallID:          pending[0].ProviderToolCallID,
		ArgumentsDigest: toolcall.ArgumentsDigest(pending[0].Name, pending[0].NormalizedArguments),
	}
	if err = f.coordinator.ApproveToolCall(ctx, decision); err != nil {
		t.Fatal(err)
	}
	req := <-f.model.requests
	if req.ExecutionID != v.Execution.ID {
		t.Fatal("restart changed approval execution identity")
	}
	f.model.release <- struct{}{}
	f.waitEnded(t, v.Execution.ID)
	if f.tool.executed.Load() != 1 {
		t.Fatal("restored tool did not execute exactly once")
	}
}
