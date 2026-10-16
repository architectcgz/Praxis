package task

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	toolmodel "praxis/internal/core/tool_invocation"
	"reflect"
	"sync"
	"testing"
	"time"

	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/model"
	projectmodel "praxis/internal/core/project"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	workspacemodel "praxis/internal/core/workspace"
	"praxis/internal/hooks"
	"praxis/internal/infra/jsonl"
	"praxis/internal/logging"
	"praxis/internal/repository"
	runtimequeue "praxis/internal/service/runtime/queue"
	"praxis/internal/system"
)

type committedHookFunc func(context.Context, hooks.CommittedEvent) error

func (f committedHookFunc) OnCommitted(ctx context.Context, event hooks.CommittedEvent) error {
	return f(ctx, event)
}

type hookDefinitions struct{}

func (hookDefinitions) Definition(id contracts.AgentDefinitionID) (agentmodel.AgentDefinition, error) {
	return agentmodel.AgentDefinition{
		ID:           id,
		Profile:      contracts.ProfilePrimary,
		Instructions: "回答问题",
	}, nil
}

type hookModels struct{}

func (hookModels) ResolveDefaultTaskModel(contracts.AgentDefinitionID) (model.Config, error) {
	return model.Config{
		ProviderID:      "provider",
		ModelID:         "model",
		APIFormat:       "openai_chat",
		ContextWindow:   8192,
		MaxOutputTokens: 1024,
		BaseURL:         "https://example.com",
	}, nil
}

func (m hookModels) ResolveTaskModel(string, string, string) (model.Config, error) {
	return m.ResolveDefaultTaskModel("")
}

type hookSlots struct {
	ExecutionSlots
	released bool
}

func (*hookSlots) Reserve(ctx context.Context, _ contracts.AgentID, _ contracts.TaskID) (context.Context, bool, error) {
	return ctx, true, nil
}

func (s *hookSlots) Release(contracts.AgentID, contracts.TaskID) { s.released = true }

type hookFixture struct {
	root  string
	store *jsonl.Store
	repos jsonl.Repositories
	svc   *Service
	slots *hookSlots
}

func newHookFixture(t *testing.T) *hookFixture {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	store, err := jsonl.Open(ctx, filepath.Join(root, "runtime"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(ctx) })
	repos := store.Repositories()
	at := time.Now().UTC()
	project, err := projectmodel.NewProject("project", "项目", root, "workspace", at)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := workspacemodel.NewWorkspace("workspace", project.ID, workspacemodel.WorkspaceProjectRoot, root, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InTx(ctx, func(txCtx context.Context) error {
		if err := repos.Projects.Save(txCtx, project); err != nil {
			return err
		}
		return repos.Workspaces.Save(txCtx, workspace)
	}); err != nil {
		t.Fatal(err)
	}
	session, err := sessionmodel.NewSession("session", project.ID, workspace.ID, at)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := agentmodel.NewAgent("agent", session.ID, agentmodel.DefinitionPrimary, contracts.ProfilePrimary, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InTx(ctx, func(txCtx context.Context) error {
		if err := repos.Sessions.Save(txCtx, session); err != nil {
			return err
		}
		return repos.Agents.Save(txCtx, agent)
	}); err != nil {
		t.Fatal(err)
	}
	slots := &hookSlots{}
	return &hookFixture{
		root:  filepath.Join(root, "runtime"),
		store: store,
		repos: repos,
		slots: slots,
		svc: &Service{
			tx:          store,
			controls:    repos.Controls,
			workspaces:  repos.Workspaces,
			sessions:    repos.Sessions,
			agents:      repos.Agents,
			tasks:       repos.Tasks,
			invocations: repos.ToolInvocations,
			messages:    repos.Messages,
			contexts:    repos.Contexts,
			executions:  slots,
			definitions: hookDefinitions{},
			models:      hookModels{},
			logger:      logging.NewFactory().Nop(),
			clock:       system.ClockOrDefault(nil),
			ids:         system.IDsOrDefault(nil),
		},
	}
}

func hookInput() SendInputParams {
	return SendInputParams{
		SessionID: "session",
		AgentID:   "agent",
		RequestID: "request",
		Content:   "你好",
	}
}

// 在真实事务回调完成后注入失败，覆盖消息、Task 和执行预留已变更的回滚路径。
type hookRollbackTx struct{ repository.TxRunner }

var errHookRollback = errors.New("rollback after callback")

func (r hookRollbackTx) InTx(ctx context.Context, fn func(context.Context) error) error {
	return r.TxRunner.InTx(ctx, func(txCtx context.Context) error {
		if err := fn(txCtx); err != nil {
			return err
		}
		return errHookRollback
	})
}

type hookCancelAfterCommitTx struct {
	repository.TxRunner
	cancel context.CancelFunc
}

func (r hookCancelAfterCommitTx) InTx(ctx context.Context, fn func(context.Context) error) error {
	err := r.TxRunner.InTx(ctx, fn)
	if err == nil {
		r.cancel()
	}
	return err
}

func TestInputCommittedHook(t *testing.T) {
	f := newHookFixture(t)
	type contextKey struct{}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), contextKey{}, "trace"))
	defer cancel()
	f.svc.tx = hookCancelAfterCommitTx{TxRunner: f.store, cancel: cancel}
	calls := 0
	f.svc.hookDispatcher = hooks.NewDispatcherWithEvents(nil, committedHookFunc(func(hookCtx context.Context, event hooks.CommittedEvent) error {
		calls++
		if hookCtx.Err() != nil || hookCtx.Value(contextKey{}) != "trace" {
			t.Error("post-commit hook must preserve context values and detach cancellation")
		}
		// 使用独立 Context 查询实际提交状态，同时验证 Store 锁已释放。
		task, err := f.repos.Tasks.Get(context.Background(), event.TaskID)
		if err != nil || task.ID != event.TaskID || event.SessionID != "session" || event.AgentID != "agent" {
			t.Fatalf("invalid ownership or commit state: %+v, %v", event.Identity, err)
		}
		stream, err := f.repos.Messages.LoadMessages(context.Background(), "session", "agent", 0)
		if err != nil || len(stream.Messages) != 1 || !reflect.DeepEqual(stream.Messages[0], *event.Message) {
			t.Fatalf("hook must receive the persisted user message: %+v, %v", stream, err)
		}
		return errors.New("hook failed")
	}))
	result, err := f.svc.SendInput(ctx, hookInput())
	if err != nil || result.ExistingRequest || calls != 1 {
		t.Fatalf("hook failure must not change the business result: %+v, %v, calls=%d", result, err, calls)
	}
	f.svc.tx = f.store
	duplicate, err := f.svc.SendInput(context.Background(), hookInput())
	if err != nil || !duplicate.ExistingRequest || calls != 1 {
		t.Fatalf("duplicate input must not trigger another notification: %+v, %v, calls=%d", duplicate, err, calls)
	}
	conflict := hookInput()
	conflict.Content = "不同输入"
	if _, err := f.svc.SendInput(context.Background(), conflict); !errors.Is(err, contracts.ErrRequestConflict) || calls != 1 {
		t.Fatalf("conflicting request must not trigger a notification: %v, calls=%d", err, calls)
	}
}

func TestInputRollbackDoesNotNotify(t *testing.T) {
	f := newHookFixture(t)
	f.svc.tx = hookRollbackTx{f.store}
	calls := 0
	f.svc.hookDispatcher = hooks.NewDispatcherWithEvents(nil, committedHookFunc(func(context.Context, hooks.CommittedEvent) error {
		calls++
		return nil
	}))
	if _, err := f.svc.SendInput(context.Background(), hookInput()); !errors.Is(err, errHookRollback) {
		t.Fatal(err)
	}
	stream, err := f.repos.Messages.LoadMessages(context.Background(), "session", "agent", 0)
	if err != nil || len(stream.Messages) != 0 || calls != 0 || !f.slots.released {
		t.Fatalf("rollback must discard messages, release reservations, and skip notifications: %+v, %v, calls=%d, released=%v", stream, err, calls, f.slots.released)
	}
}

func TestQueueCommittedHook(t *testing.T) {
	f := newHookFixture(t)
	if _, err := f.svc.SendInput(context.Background(), hookInput()); err != nil {
		t.Fatal(err)
	}
	calls := 0
	dispatcher := hooks.NewDispatcherWithEvents(nil, committedHookFunc(func(ctx context.Context, event hooks.CommittedEvent) error {
		calls++
		task, err := f.repos.Tasks.Get(context.Background(), event.TaskID)
		if err != nil || task.Status != taskmodel.TaskPending || event.Message.Sequence == 0 || event.Message.TextContent() != "排队输入" ||
			event.SessionID != task.SessionID || event.AgentID != task.AgentID {
			t.Fatalf("queued message must be published after commit with complete identity: %+v, %v", event, err)
		}
		return errors.New("hook failed")
	}))
	config := runtimequeue.Config{
		Transactions:   hookRollbackTx{f.store},
		Agents:         f.repos.Agents,
		Tasks:          f.repos.Tasks,
		Messages:       f.repos.Messages,
		HookDispatcher: dispatcher,
	}
	queue, err := runtimequeue.NewService(config)
	if err != nil {
		t.Fatal(err)
	}
	params := runtimequeue.EnqueueParams{
		ID:        "queued",
		AgentID:   "agent",
		RequestID: "queued-request",
		Prompt:    "排队输入",
	}
	if _, err := queue.EnqueueTask(context.Background(), params); !errors.Is(err, errHookRollback) || calls != 0 {
		t.Fatalf("queue rollback must not trigger a notification: %v, calls=%d", err, calls)
	}
	config.Transactions = f.store
	queue, err = runtimequeue.NewService(config)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		result, err := queue.EnqueueTask(context.Background(), params)
		if err != nil || result.ExistingTask != (i == 1) || calls != 1 {
			t.Fatalf("incorrect queue or idempotent notification result: %+v, %v, calls=%d", result, err, calls)
		}
	}
}

func TestSettleCommittedHook(t *testing.T) {
	f := newHookFixture(t)
	ctx := context.Background()
	created, err := f.svc.SendInput(ctx, hookInput())
	if err != nil {
		t.Fatal(err)
	}
	control, err := agentmodel.NewAgentControlCommand("cancel", "agent", created.Task.ID, agentmodel.AgentControlCancel, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.InTx(ctx, func(txCtx context.Context) error {
		return f.repos.Controls.Save(txCtx, control)
	}); err != nil {
		t.Fatal(err)
	}
	calls, uiCalls := 0, 0
	f.svc.eventObserver = func(event TerminalEvent) {
		uiCalls++
		if event.Outcome != string(taskmodel.TaskInterrupted) {
			t.Errorf("terminal UI event must use the arbitrated outcome: %+v", event)
		}
	}
	f.svc.hookDispatcher = hooks.NewDispatcherWithEvents(nil, committedHookFunc(func(ctx context.Context, event hooks.CommittedEvent) error {
		calls++
		stored, err := f.repos.Tasks.Get(context.Background(), event.TaskID)
		if err != nil || !reflect.DeepEqual(stored, *event.Task) || stored.Outcome != taskmodel.TaskInterrupted {
			t.Fatalf("hook must receive the actual arbitrated terminal state: %+v, %v", event, err)
		}
		// 扩展不能通过通知参数改变返回结果或后续 UI 事件。
		event.Task.Outcome = taskmodel.TaskCompleted
		return errors.New("hook failed")
	}))
	f.svc.tx = hookRollbackTx{f.store}
	if _, err := f.svc.Settle(ctx, created.Task.ID, taskmodel.TaskCompleted, "", ""); !errors.Is(err, errHookRollback) || calls != 0 || uiCalls != 0 {
		t.Fatalf("settlement rollback must not trigger notifications: %v, calls=%d, uiCalls=%d", err, calls, uiCalls)
	}
	f.svc.tx = f.store
	for range 2 {
		ended, err := f.svc.Settle(ctx, created.Task.ID, taskmodel.TaskCompleted, "", "")
		if err != nil || ended.Outcome != taskmodel.TaskInterrupted || calls != 1 || uiCalls != 1 {
			t.Fatalf("incorrect settlement or idempotency result: %+v, %v, calls=%d, uiCalls=%d", ended, err, calls, uiCalls)
		}
	}
}

func TestConcurrentInputNotifiesOnce(t *testing.T) {
	f := newHookFixture(t)
	var mu sync.Mutex
	calls := 0
	f.svc.hookDispatcher = hooks.NewDispatcherWithEvents(nil, committedHookFunc(func(context.Context, hooks.CommittedEvent) error {
		mu.Lock()
		defer mu.Unlock()
		calls++
		return nil
	}))
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := f.svc.SendInput(context.Background(), hookInput()); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if calls != 1 {
		t.Fatalf("concurrent identical requests triggered %d notifications", calls)
	}
}

func TestAssistantWriterReportsOnlyCommittedInsert(t *testing.T) {
	f := newHookFixture(t)
	ctx := context.Background()
	result, err := f.svc.SendInput(ctx, hookInput())
	if err != nil {
		t.Fatal(err)
	}
	writer, err := f.repos.Messages.Writer(ctx, "session", "agent")
	if err != nil {
		t.Fatal(err)
	}
	turnID := contracts.NewTurnID(result.Task.ID, 1)
	message := sessionmodel.MessageData{
		ID:         "assistant:" + turnID.String(),
		RequestID:  result.Task.RequestID.String(),
		TaskID:     result.Task.ID.String(),
		TurnID:     turnID.String(),
		Role:       sessionmodel.RoleAssistant,
		AuthorKind: sessionmodel.AuthorAgent,
		AuthorID:   "agent",
		StopReason: "completed",
		Blocks:     []sessionmodel.Block{{Kind: sessionmodel.BlockText, Text: "回答"}},
	}
	metadata := contracts.ToolInvocationContext{
		SessionID:     "session",
		AgentID:       "agent",
		TaskID:        result.Task.ID,
		TurnID:        turnID,
		WorkspacePath: result.Task.Config.WorkspacePath,
	}
	// 重复工具身份在追加消息后失败，整笔事务及 inserted 标记都必须回滚。
	invalid := message
	invalid.Blocks = []sessionmodel.Block{
		{Kind: sessionmodel.BlockToolCall, CallID: "call", Name: "read_file"},
		{Kind: sessionmodel.BlockToolCall, CallID: "call", Name: "read_file"},
	}
	if _, inserted, err := writer.AppendAssistant(ctx, testAssistantWrite(t, invalid, metadata)); err == nil || inserted {
		t.Fatalf("failed transaction must not report insertion: inserted=%v, err=%v", inserted, err)
	}
	stream, err := f.repos.Messages.LoadMessages(ctx, "session", "agent", 0)
	if err != nil || len(stream.Messages) != 1 {
		t.Fatalf("failed transaction left a message: %+v, %v", stream, err)
	}
	for i := 0; i < 2; i++ {
		saved, inserted, err := writer.AppendAssistant(ctx, testAssistantWrite(t, message, metadata))
		if err != nil || inserted != (i == 0) || saved.Sequence == 0 || saved.CreatedAt.IsZero() {
			t.Fatalf("incorrect duplicate write flag: inserted=%v, err=%v", inserted, err)
		}
	}
	stored, err := f.repos.Tasks.Get(ctx, result.Task.ID)
	if err != nil || stored.Progress.FinalAssistantID != message.ID || stored.Progress.FinalStopReason != message.StopReason {
		t.Fatalf("final output evidence was not saved atomically: %+v, %v", stored.Progress, err)
	}
	conflict := message
	conflict.Blocks = []sessionmodel.Block{{Kind: sessionmodel.BlockText, Text: "不同回答"}}
	if _, inserted, err := writer.AppendAssistant(ctx, testAssistantWrite(t, conflict, metadata)); !errors.Is(err, contracts.ErrRequestConflict) || inserted {
		t.Fatalf("conflicting message must not report insertion: inserted=%v, err=%v", inserted, err)
	}
}

func testAssistantWrite(t testing.TB, message sessionmodel.MessageData, metadata contracts.ToolInvocationContext) repository.AssistantWrite {
	t.Helper()
	value := repository.AssistantWrite{Message: message}
	if message.StopReason == "completed" || message.StopReason == "stop_sequence" {
		value.FinalOutput = &taskmodel.FinalOutput{AssistantID: message.ID, StopReason: message.StopReason}
	}
	for index, block := range message.Blocks {
		if block.Kind != sessionmodel.BlockToolCall {
			continue
		}
		if len(block.Input) == 0 {
			continue
		}
		call, err := toolmodel.NewToolInvocation(contracts.ToolInvocationID(fmt.Sprintf("inv-%s-%d", message.ID, index)), metadata.TaskID, metadata.TurnID, metadata.SessionID, metadata.AgentID, block.CallID, contracts.ToolName(block.Name), block.Input, toolmodel.ArgumentsDigest(contracts.ToolName(block.Name), block.Input), time.Now())
		if err != nil {
			t.Fatal(err)
		}
		value.ToolCalls = append(value.ToolCalls, call)
	}
	return value
}
