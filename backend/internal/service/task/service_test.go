package task

import (
	"context"
	"errors"
	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/model"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/infra/jsonl"
	"praxis/internal/loop"
	"praxis/internal/repository"
	runtimequeue "praxis/internal/service/runtime/queue"
	"praxis/internal/tools"
	toolcontracts "praxis/internal/tools/contracts"
	"reflect"
	"strings"
	"testing"
)

type testCatalog struct {
	config model.Config
	fail   bool
}

func (c *testCatalog) Definition(id contracts.AgentDefinitionID) (agentmodel.AgentDefinition, error) {
	return agentmodel.AgentDefinition{
		ID:           id,
		Profile:      contracts.ProfilePrimary,
		Revision:     "definition revision",
		Instructions: "system",
	}, nil
}
func (c *testCatalog) ResolveDefaultTaskModel(contracts.AgentDefinitionID) (model.Config, error) {
	return c.config.Clone(), nil
}
func (c *testCatalog) ResolveTaskModel(provider, name, level string) (model.Config, error) {
	if c.fail {
		return model.Config{}, errors.New("model unavailable")
	}
	config := c.config.Clone()
	config.ProviderID, config.ModelID, config.ReasoningLevel = provider, name, level
	return config, nil
}

type testSlots struct {
	tasks map[contracts.AgentID]contracts.TaskID
}

func (s *testSlots) Reserve(ctx context.Context, agent contracts.AgentID, task contracts.TaskID) (context.Context, bool, error) {
	if s.tasks == nil {
		s.tasks = make(map[contracts.AgentID]contracts.TaskID)
	}
	if old := s.tasks[agent]; old != "" {
		if old == task {
			return ctx, false, nil
		}
		return nil, false, contracts.ErrAgentExecuting
	}
	s.tasks[agent] = task
	return context.WithoutCancel(ctx), true, nil
}
func (s *testSlots) Release(agent contracts.AgentID, task contracts.TaskID) {
	if s.tasks[agent] == task {
		delete(s.tasks, agent)
	}
}
func (*testSlots) Pause(contracts.AgentID, contracts.TaskID) bool { return true }
func (*testSlots) Stop(contracts.AgentID, contracts.TaskID) bool  { return true }

type primaryAgent struct{ agent agentmodel.Agent }

func (p primaryAgent) GetOrCreatePrimaryAgent(context.Context, contracts.SessionID, contracts.RequestID) (agentmodel.Agent, error) {
	return p.agent, nil
}

func newTaskService(t *testing.T, f *contextFixture, catalog *testCatalog, slots *testSlots) *Service {
	t.Helper()
	s, err := NewService(Config{
		Transactions: f.store,
		Controls:     f.repos.Controls,
		Workspaces:   f.repos.Workspaces,
		Sessions:     f.repos.Sessions,
		Agents:       f.repos.Agents,
		Tasks:        f.repos.Tasks,
		Contexts:     f.repos.Contexts,
		Messages:     f.repos.Messages,
		PrimaryAgent: primaryAgent{f.agent},
		Definitions:  catalog,
		Models:       catalog,
		Executions:   slots,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func send(t *testing.T, s *Service, f *contextFixture, request, body string) taskmodel.Task {
	t.Helper()
	result, err := s.SendInput(t.Context(), SendInputParams{AgentID: f.agent.ID, RequestID: contracts.RequestID(request), Content: body})
	if err != nil {
		t.Fatal(err)
	}
	return result.Task
}
func finish(t *testing.T, s *Service, task taskmodel.Task) {
	t.Helper()
	if err := s.ConfirmRunning(t.Context(), task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Settle(t.Context(), task.ID, taskmodel.TaskCompleted, "", ""); err != nil {
		t.Fatal(err)
	}
	s.ReleaseExecution(task.AgentID, task.ID)
}

type failingTaskSave struct{ repository.TaskRepository }

func (r failingTaskSave) Save(ctx context.Context, value taskmodel.Task) error {
	if value.Status == taskmodel.TaskStarting {
		return errors.New("task save failed after reservation")
	}
	return r.TaskRepository.Save(ctx, value)
}

func TestImmediateTaskAndPendingFIFOResolveConfigAtClaim(t *testing.T) {
	f := newContextFixture(t)
	catalog, slots := &testCatalog{config: f.config.Model}, &testSlots{}
	s := newTaskService(t, f, catalog, slots)
	first := send(t, s, f, "first", "first input")
	if first.Config.AgentDefinitionID != f.agent.DefinitionID || first.InputMessageID != "input:first" || first.ContextSelection.MessageSequenceBoundary == 0 {
		t.Fatalf("立即执行字段错误: %+v", first)
	}
	content, _, err := s.LoadContext(t.Context(), first)
	if err != nil || len(content.Entries) != 1 || !content.Entries[0].Protected || content.Entries[0].Content[0].Text != "first input" {
		t.Fatalf("未从日志读取当前输入: %+v %v", content, err)
	}
	duplicate, err := s.SendInput(t.Context(), SendInputParams{AgentID: f.agent.ID, RequestID: "first", Content: "first input"})
	if err != nil || !duplicate.ExistingRequest || duplicate.Task.ID != first.ID {
		t.Fatalf("重复请求未幂等: %+v %v", duplicate, err)
	}
	queue, err := runtimequeue.NewService(runtimequeue.Config{
		Transactions: f.store,
		Agents:       f.repos.Agents,
		Tasks:        f.repos.Tasks,
		Messages:     f.repos.Messages,
	})
	if err != nil {
		t.Fatal(err)
	}
	var pending []taskmodel.Task
	for _, id := range []string{"second", "third"} {
		result, err := queue.EnqueueTask(t.Context(), runtimequeue.EnqueueParams{
			ID:         contracts.TaskID(id),
			AgentID:    f.agent.ID,
			RequestID:  contracts.RequestID(id),
			Prompt:     id + " input",
			ProviderID: "chosen",
			ModelID:    "chosen-model",
		})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.ValueOf(result.Task.Config).IsZero() || result.Task.ProviderID != "chosen" || result.Task.InputMessageID != "input:"+id {
			t.Fatalf("Pending 提前解析或丢失模型选择: %+v", result.Task)
		}
		pending = append(pending, result.Task)
	}
	finish(t, s, first)
	if _, prepared, err := s.PrepareQueuedTask(t.Context(), pending[1]); err != nil || prepared {
		t.Fatalf("越过 FIFO: %v %v", prepared, err)
	}
	// 模拟模型目录在排队期间变化，领取必须使用最新配置。
	catalog.config.ContextWindow = 16384
	catalog.fail = true
	if _, prepared, err := s.PrepareQueuedTask(t.Context(), pending[0]); err == nil || prepared {
		t.Fatal("模型解析失败未回滚")
	}
	catalog.fail = false
	s.tasks = failingTaskSave{f.repos.Tasks}
	if _, prepared, err := s.PrepareQueuedTask(t.Context(), pending[0]); err == nil || prepared {
		t.Fatal("持久化失败未回滚")
	}
	s.tasks = f.repos.Tasks
	stored, err := s.LoadTask(t.Context(), pending[0].ID)
	if err != nil || !reflect.DeepEqual(stored, pending[0]) || len(slots.tasks) != 0 {
		t.Fatalf("准备失败改变 Pending 或泄漏执行槽: %+v %v", stored, err)
	}
	second, prepared, err := s.PrepareQueuedTask(t.Context(), pending[0])
	if err != nil || !prepared {
		t.Fatalf("领取失败: %v %v", prepared, err)
	}
	if second.Config.Model.ProviderID != "chosen" || second.Config.Model.ContextWindow != 16384 || !reflect.DeepEqual(second.ContextSelection.ExcludedMessageIDs, []string{"input:third"}) {
		t.Fatalf("领取配置或冻结选择错误: %+v", second)
	}
	finish(t, s, second)
	third, prepared, err := s.PrepareQueuedTask(t.Context(), pending[1])
	if err != nil || !prepared || third.ID != pending[1].ID {
		t.Fatalf("第三项未按 FIFO 执行: %+v %v", third, err)
	}
}

func TestImmediateContextFailureRollsBackUserMessage(t *testing.T) {
	f := newContextFixture(t)
	catalog, slots := &testCatalog{config: f.config.Model}, &testSlots{}
	catalog.config.ContextWindow, catalog.config.MaxOutputTokens = 1024, 128
	s := newTaskService(t, f, catalog, slots)
	if _, err := s.SendInput(t.Context(), SendInputParams{AgentID: f.agent.ID, RequestID: "bad", Content: strings.Repeat("input ", 2000)}); err == nil {
		t.Fatal("超窗请求未失败")
	}
	if _, err := f.repos.Tasks.FindByRequest(t.Context(), f.agent.ID, "bad"); !errors.Is(err, contracts.ErrNotFound) {
		t.Fatalf("失败请求保存了 Task: %v", err)
	}
	messages, err := f.repos.Messages.List(t.Context(), f.agent, 0, 0)
	if err != nil || len(messages) != 0 || len(slots.tasks) != 0 {
		t.Fatalf("失败请求保存了消息或执行槽: %v %v", messages, err)
	}
}

type scriptedModel struct{ stream *scriptedStream }

func (m scriptedModel) BuildModel(config model.Config) (model.Model, error) {
	return model.Model{Stream: m.stream, MaxOutputTokens: config.MaxOutputTokens}, nil
}

type scriptedStream struct{ calls int }

func (s *scriptedStream) Stream(_ context.Context, request model.ModelRequest) (<-chan model.ModelStreamEvent, error) {
	s.calls++
	events := make(chan model.ModelStreamEvent, 3)
	if s.calls == 1 {
		events <- model.ModelStreamEvent{Kind: model.StreamToolCall, ToolCall: contracts.ToolCall{ID: "call", Name: "bash", Arguments: []byte(`{}`)}}
		events <- model.ModelStreamEvent{Kind: model.StreamComplete, StopReason: model.StopToolCalls}
	} else {
		events <- model.ModelStreamEvent{Kind: model.StreamTextDelta, Text: "done"}
		events <- model.ModelStreamEvent{Kind: model.StreamComplete, StopReason: model.StopCompleted}
	}
	close(events)
	return events, nil
}

type approvalExecutor struct{ attempts int }

func (e *approvalExecutor) Execute(context.Context, contracts.ToolCall, contracts.ToolInvocationContext) (toolcontracts.ToolExecutionResult, error) {
	e.attempts++
	if e.attempts == 1 {
		return toolcontracts.ToolExecutionResult{ApprovalPending: true}, nil
	}
	return toolcontracts.ToolExecutionResult{Result: toolcontracts.ToolResult{Payload: "receipt"}}, nil
}

func TestLoopApprovalRecoveryAndCommittedProgress(t *testing.T) {
	f := newContextFixture(t)
	catalog, slots := &testCatalog{config: f.config.Model}, &testSlots{}
	s := newTaskService(t, f, catalog, slots)
	task := send(t, s, f, "resume", "user input")
	if err := s.ConfirmRunning(t.Context(), task.ID); err != nil {
		t.Fatal(err)
	}
	stream, executor := &scriptedStream{}, &approvalExecutor{}
	run := func() taskmodel.TaskOutcome {
		t.Helper()
		runner, err := loop.NewRunner(loop.Config{
			Progress:      s,
			ModelBuilder:  scriptedModel{stream},
			Tools:         tools.NewToolRegistry(),
			ToolExecutor:  executor,
			EventObserver: func(loop.Event) {},
		})
		if err != nil {
			t.Fatal(err)
		}
		content, continuation, err := s.LoadContext(t.Context(), task)
		if err != nil {
			t.Fatal(err)
		}
		recorder, err := f.repos.Messages.Resolve(t.Context(), task.SessionID, task.AgentID)
		if err != nil {
			t.Fatal(err)
		}
		outcome, _, err := runner.Run(t.Context(), task, content, continuation, recorder)
		if err != nil {
			t.Fatal(err)
		}
		return outcome
	}
	if outcome := run(); outcome != taskmodel.TaskAwaitingApproval || stream.calls != 1 {
		t.Fatalf("首次未等待审批: %s", outcome)
	}
	// 重新打开真实日志，恢复服务与 Loop，调用身份和进度必须沿用。
	if err := f.store.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	var err error
	f.store, err = jsonl.Open(t.Context(), f.root)
	if err != nil {
		t.Fatal(err)
	}
	f.repos = f.store.Repositories()
	s = newTaskService(t, f, catalog, slots)
	task, err = s.LoadTask(t.Context(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReclaimTask(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	if outcome := run(); outcome != taskmodel.TaskCompleted {
		t.Fatalf("恢复未完成: %s", outcome)
	}
	stored, err := s.LoadTask(t.Context(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Progress.ModelRequests != 2 || stored.Progress.NextSequence != 3 || stored.Progress.OutputBytes != int64(len("done")+len("receipt")) || stored.Progress.FinalAssistantID == "" {
		t.Fatalf("恢复预算或完成证据错误: %+v", stored.Progress)
	}
	if outcome := run(); outcome != taskmodel.TaskCompleted || stream.calls != 2 || executor.attempts != 2 {
		t.Fatal("重复恢复重新请求模型或执行工具")
	}
	messages, err := f.repos.Messages.List(t.Context(), f.agent, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	receipts, assistants := 0, 0
	for _, message := range messages {
		if message.Role == sessionmodel.RoleTool {
			receipts++
		}
		if message.Role == sessionmodel.RoleAssistant {
			assistants++
		}
	}
	if receipts != 1 || assistants != 2 {
		t.Fatalf("消息或工具回执重复: %+v", messages)
	}
}

func TestSelectionNeverReintroducesExcludedOrOrphanReceipts(t *testing.T) {
	messages := []sessionmodel.MessageData{
		{
			ID:       "call",
			TaskID:   "history",
			Sequence: 1,
			Role:     sessionmodel.RoleAssistant,
			Blocks:   []sessionmodel.Block{{Kind: sessionmodel.BlockToolCall, CallID: "selected", Name: "tool"}},
		},
		{
			ID:       "excluded",
			TaskID:   "history",
			Sequence: 4,
			Role:     sessionmodel.RoleTool,
			Blocks: []sessionmodel.Block{{
				Kind:   sessionmodel.BlockToolResult,
				CallID: "selected",
				Name:   "tool",
				Text:   "excluded receipt",
			}},
		},
		{
			ID:       "orphan",
			TaskID:   "other",
			Sequence: 2,
			Role:     sessionmodel.RoleTool,
			Blocks: []sessionmodel.Block{{
				Kind:   sessionmodel.BlockToolResult,
				CallID: "selected",
				Name:   "tool",
				Text:   "wrong task",
			}},
		},
	}
	selected, _ := selectTaskMessages(messages, taskmodel.ContextSelection{MessageSequenceBoundary: 3, ExcludedMessageIDs: []string{"excluded"}}, "input", "current")
	if len(selected) != 1 || selected[0].ID != "call" {
		t.Fatalf("排除项或跨 Task 回执混入上下文: %+v", selected)
	}
}

func TestFrozenTaskPersistenceAndRestore(t *testing.T) {
	f := newContextFixture(t)
	catalog, slots := &testCatalog{config: f.config.Model}, &testSlots{}
	s := newTaskService(t, f, catalog, slots)
	task := send(t, s, f, "immutable", "persistent input")
	cases := map[string]func(*taskmodel.Task){
		"config":   func(task *taskmodel.Task) { task.Config.SystemPrompt = "changed" },
		"boundary": func(task *taskmodel.Task) { task.ContextSelection.MessageSequenceBoundary++ },
		"excluded": func(task *taskmodel.Task) { task.ContextSelection.ExcludedMessageIDs = []string{"changed"} },
		"shared": func(task *taskmodel.Task) {
			task.ContextSelection.SessionContextRefs = []taskmodel.ContextReference{{ID: "changed", Digest: "changed"}}
		},
		"input":           func(task *taskmodel.Task) { task.InputMessageID = "changed" },
		"requested model": func(task *taskmodel.Task) { task.ProviderID, task.ModelID = "changed", "changed" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			candidate := task
			candidate.Config = task.Config.Clone()
			candidate.ContextSelection = task.ContextSelection.Clone()
			change(&candidate)
			err := f.store.InTx(t.Context(), func(ctx context.Context) error { return f.repos.Tasks.Save(ctx, candidate) })
			if !errors.Is(err, contracts.ErrRequestConflict) {
				t.Fatalf("不可变字段未受保护: %v", err)
			}
		})
	}
	if err := s.RecordModelRequest(t.Context(), task.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordModelOutput(t.Context(), task.ID, 13); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	var err error
	f.store, err = jsonl.Open(t.Context(), f.root)
	if err != nil {
		t.Fatal(err)
	}
	f.repos = f.store.Repositories()
	stored, err := f.repos.Tasks.Get(t.Context(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(task.Config, stored.Config) || !reflect.DeepEqual(task.ContextSelection, stored.ContextSelection) || task.InputMessageID != stored.InputMessageID || stored.Progress.OutputBytes != 13 || stored.Progress.ModelRequests != 1 {
		t.Fatalf("重启丢失冻结数据或预算: %+v", stored)
	}
	candidate := stored
	candidate.Progress.OutputBytes = 0
	if err := f.store.InTx(t.Context(), func(ctx context.Context) error { return f.repos.Tasks.Save(ctx, candidate) }); !errors.Is(err, contracts.ErrRequestConflict) {
		t.Fatalf("消耗预算回退未被拒绝: %v", err)
	}
}
