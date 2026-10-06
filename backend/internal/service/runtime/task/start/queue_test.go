package start_test

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"praxis/internal/agent_runtime"
	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	contextmodel "praxis/internal/core/context"
	projectmodel "praxis/internal/core/project"
	securitymodel "praxis/internal/core/security"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	workspacemodel "praxis/internal/core/workspace"
	"praxis/internal/infra/jsonl"
	appagent "praxis/internal/service/agent"
	applicationruntime "praxis/internal/service/runtime"
	"praxis/internal/service/runtime/queue"
	applicationtask "praxis/internal/service/runtime/task"
	"praxis/internal/service/runtime/task/lifecycle"
	"praxis/internal/service/runtime/task/start"
	appsession "praxis/internal/service/session"
)

// failingRuntimeFactory 模拟 runtime 无法创建，用于验证激活失败的收敛路径。
type failingRuntimeFactory struct{}

func (failingRuntimeFactory) New(context.Context, contracts.AgentID) (agentruntime.ManagedRuntime, error) {
	return nil, errors.New("runtime unavailable")
}

func TestActivationFailureSettlesTaskAndReleasesAgent(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	taskService, err := applicationtask.NewService(f.start, f.lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := agentruntime.NewRegistry(failingRuntimeFactory{})
	if err != nil {
		t.Fatal(err)
	}
	service, err := applicationruntime.NewService(applicationruntime.Config{
		Tasks: taskService, Queue: f.queue, Registry: registry,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.SendInput(ctx, start.SendInputParams{SessionID: "session", RequestID: "direct", Content: "当前输入"})
	if err != nil {
		t.Fatal(err)
	}
	if result.ActivationError == "" {
		t.Fatal("激活失败必须上报给调用方")
	}
	task, err := f.repos.Tasks.Get(ctx, result.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != taskmodel.TaskEnded || task.Outcome != taskmodel.TaskInterrupted {
		t.Fatalf("未接管的 Task 必须收敛为中断: %+v", task)
	}
	agent, err := f.repos.Agents.Get(ctx, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if agent.State == agentmodel.AgentExecuting || agent.CurrentTaskID != "" {
		t.Fatalf("Agent 不能停留在执行中: %+v", agent)
	}
	if _, err := f.executions.Context("agent", result.Task.ID); !errors.Is(err, contracts.ErrNotFound) {
		t.Fatalf("激活失败必须释放执行绑定: %v", err)
	}
}

type taskFixture struct {
	store       *jsonl.Store
	root        string
	repos       jsonl.Repositories
	start       *start.Service
	queue       *queue.Service
	next        *queue.TaskQueue
	lifecycle   *lifecycle.Service
	sessions    *appsession.Service
	messages    appsession.MessageStore
	context     *countedContext
	executions  *agentmodel.Executions
	agents      *appagent.Service
	startConfig start.Config
}

type countedContext struct {
	next  start.ContextProvider
	calls atomic.Int32
	fail  atomic.Bool
}

func (c *countedContext) BuildContext(ctx context.Context, agent agentmodel.Agent, system, input, messageID string) (contextmodel.BuildResult, error) {
	c.calls.Add(1)
	if c.fail.Load() {
		return contextmodel.BuildResult{}, errors.New("context unavailable")
	}
	return c.next.BuildContext(ctx, agent, system, input, messageID)
}

type testModel struct{}

func (testModel) FreezeDefaultTaskModel(contracts.AgentDefinitionID) (contracts.ModelSnapshot, error) {
	return testModel{}.FreezeTaskModel("provider", "model", "")
}

func (testModel) FreezeTaskModel(provider, model, reasoning string) (contracts.ModelSnapshot, error) {
	return contracts.ModelSnapshot{
		ProviderID:      provider,
		ModelID:         model,
		ReasoningLevel:  reasoning,
		APIFormat:       "openai_chat_completions",
		ContextWindow:   8192,
		MaxOutputTokens: 1024,
		BaseURL:         "https://example.invalid",
	}, nil
}

type testDefinition struct{}

func (testDefinition) Definition(id contracts.AgentDefinitionID) (agentmodel.AgentDefinition, error) {
	return agentmodel.AgentDefinition{
		ID:           id,
		Profile:      contracts.ProfilePrimary,
		Instructions: "执行当前输入",
		Revision:     "revision",
	}, nil
}

func newFixture(t *testing.T) *taskFixture {
	t.Helper()
	root := filepath.Clean(t.TempDir())
	store, err := jsonl.Open(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(context.Background()) })
	repos := store.Repositories()
	at := time.Now().UTC()
	policy, err := securitymodel.NewAgentSecurityPolicy(1, securitymodel.CapabilityPolicy{},
		securitymodel.SandboxPolicy{Mode: contracts.SandboxReadOnly}, securitymodel.ApprovalPolicy{Mode: contracts.ApprovalAlwaysAsk})
	if err != nil {
		t.Fatal(err)
	}
	err = store.InTx(t.Context(), func(ctx context.Context) error {
		project, err := projectmodel.NewProject("project", "测试", root, "workspace", at)
		if err != nil {
			return err
		}
		workspace, err := workspacemodel.NewWorkspace("workspace", project.ID, workspacemodel.WorkspaceProjectRoot, root, at)
		if err != nil {
			return err
		}
		if err := repos.Projects.Save(ctx, project); err != nil {
			return err
		}
		return repos.Workspaces.Save(ctx, workspace)
	})
	if err != nil {
		t.Fatal(err)
	}
	messages := appsession.NewMessageStore(store, repos.Agents, repos.SessionMessages, repos.AgentMessages)
	sessions, err := appsession.NewService(appsession.Config{
		Transactions:    store,
		Projects:        repos.Projects,
		Workspaces:      repos.Workspaces,
		Sessions:        repos.Sessions,
		Contexts:        repos.Contexts,
		Policies:        repos.Policies,
		Agents:          repos.Agents,
		Tasks:           repos.Tasks,
		ToolInvocations: repos.ToolInvocations,
		Controls:        repos.Controls,
		Turns:           repos.Turns,
		SessionMessages: repos.SessionMessages,
		AgentMessages:   repos.AgentMessages,
		Definitions:     testDefinition{}.Definition,
		PolicyFactory: func(workspacemodel.Workspace, contracts.AgentDefinitionID) (securitymodel.AgentSecurityPolicy, error) {
			return policy, nil
		},
		Messages: messages,
		UsageRecords: func(context.Context, string) ([]agentruntime.ModelUsageRecord, error) {
			return nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.CreateSession(t.Context(), appsession.CreateParams{
		RequestID:    "create",
		SessionID:    "session",
		AgentID:      "agent",
		ProjectID:    "project",
		WorkspaceID:  "workspace",
		DefinitionID: agentmodel.DefinitionPrimary,
	}); err != nil {
		t.Fatal(err)
	}
	contextProvider := &countedContext{next: sessions}
	executions := &agentmodel.Executions{}
	startConfig := start.Config{
		Transactions:        store,
		Workspaces:          repos.Workspaces,
		Sessions:            repos.Sessions,
		Policies:            repos.Policies,
		Agents:              repos.Agents,
		Tasks:               repos.Tasks,
		Executions:          executions,
		SessionMessages:     repos.SessionMessages,
		AgentMessages:       repos.AgentMessages,
		PrimaryAgent:        sessions,
		Definitions:         testDefinition{},
		AgentDefinitionsDir: root,
		Models:              testModel{},
		ContextProvider:     contextProvider,
	}
	builder, err := start.NewService(startConfig)
	if err != nil {
		t.Fatal(err)
	}
	queueService, err := queue.NewService(queue.Config{
		Transactions:    store,
		Agents:          repos.Agents,
		Tasks:           repos.Tasks,
		SessionMessages: repos.SessionMessages,
	})
	if err != nil {
		t.Fatal(err)
	}
	next, err := queue.NewTaskQueue(repos.Tasks, "agent")
	if err != nil {
		t.Fatal(err)
	}
	lifecycleService, err := lifecycle.NewService(lifecycle.Config{
		Transactions: store,
		Sessions:     repos.Sessions,
		Agents:       repos.Agents,
		Tasks:        repos.Tasks,
		Controls:     repos.Controls,
		Messages:     messages,
	})
	if err != nil {
		t.Fatal(err)
	}
	agents, err := appagent.NewService(appagent.Config{
		Transactions: store,
		Agents:       repos.Agents,
		Tasks:        repos.Tasks,
		Controls:     repos.Controls,
		Executions:   executions,
		Messages:     messages.ListAgent,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &taskFixture{
		store:       store,
		root:        root,
		repos:       repos,
		start:       builder,
		queue:       queueService,
		next:        next,
		lifecycle:   lifecycleService,
		sessions:    sessions,
		messages:    messages,
		context:     contextProvider,
		executions:  executions,
		agents:      agents,
		startConfig: startConfig,
	}
}

func TestQueueUsesSameTaskAndBuildsContextOnlyWhenIdle(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	params := queue.EnqueueParams{ID: "later-1", AgentID: "agent", RequestID: "request-1", Prompt: " 第一个预约 "}
	if _, err := f.queue.EnqueueTask(ctx, params); !errors.Is(err, contracts.ErrAgentUnavailable) {
		t.Fatalf("空闲时不能预约: %v", err)
	}
	directParams := start.SendInputParams{SessionID: "session", RequestID: "direct", Content: " 当前输入 "}
	direct, err := f.start.SendInput(ctx, directParams)
	if err != nil {
		t.Fatal(err)
	}
	if direct.Task.Sequence != 0 || direct.Task.Status != taskmodel.TaskStarting || f.context.calls.Load() != 1 {
		t.Fatal("空闲输入应直接构建 Context，不经过 Queue")
	}
	if _, found, err := f.next.Next(ctx); err != nil || found {
		t.Fatalf("直接输入不应出现在 Queue: found=%v err=%v", found, err)
	}
	if retry, err := f.start.SendInput(ctx, directParams); err != nil || !retry.ExistingRequest || retry.Task.ID != direct.Task.ID {
		t.Fatalf("直接输入重试应幂等: %v", err)
	}
	directConflict := directParams
	directConflict.Content = "不同输入"
	if _, err := f.start.SendInput(ctx, directConflict); !errors.Is(err, contracts.ErrRequestConflict) {
		t.Fatalf("直接输入的重复请求必须按用户消息校验正文: %v", err)
	}
	first, err := f.queue.EnqueueTask(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.queue.EnqueueTask(ctx, queue.EnqueueParams{ID: "later-2", AgentID: "agent", RequestID: "request-2", Prompt: "第二个预约"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Task.Status != taskmodel.TaskPending || first.Task.Sequence >= second.Task.Sequence ||
		!reflect.ValueOf(first.Task.Input).IsZero() || first.Task.Active() || f.context.calls.Load() != 1 {
		t.Fatal("入队只能创建 Pending Task，不能构建 Context 或占用执行槽")
	}
	if retry, err := f.queue.EnqueueTask(ctx, params); err != nil || !retry.ExistingTask || retry.Task.ID != first.Task.ID {
		t.Fatalf("预约重试应返回同一 Task: %v", err)
	}
	conflict := params
	conflict.Prompt = "不同输入"
	if _, err := f.queue.EnqueueTask(ctx, conflict); !errors.Is(err, contracts.ErrRequestConflict) {
		t.Fatalf("重复请求内容不同必须冲突: %v", err)
	}
	if _, prepared, err := f.start.BuildTask(ctx, first.Task); err != nil || prepared || f.context.calls.Load() != 1 {
		t.Fatalf("忙碌时不得构建预约 Context: prepared=%v err=%v", prepared, err)
	}
	if err := f.store.InTx(ctx, func(ctx context.Context) error {
		_, err := f.repos.SessionMessages.Append(ctx, sessionmodel.SessionMessage{
			SessionID: "session",
			Data: sessionmodel.MessageData{
				ID:         "answer",
				TaskID:     direct.Task.ID.String(),
				Role:       sessionmodel.RoleAssistant,
				AuthorKind: sessionmodel.AuthorAgent,
				AuthorID:   "agent",
				Blocks:     []sessionmodel.Block{{Kind: sessionmodel.BlockText, Text: "当前回答"}},
				CreatedAt:  time.Now().UTC(),
			},
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.lifecycle.End(ctx, direct.Task.ID, taskmodel.TaskCompleted, "", ""); err != nil {
		t.Fatal(err)
	}
	f.executions.End(direct.Task.AgentID, direct.Task.ID)
	if _, prepared, err := f.start.BuildTask(ctx, second.Task); err != nil || prepared {
		t.Fatalf("不能越过队首 Task: prepared=%v err=%v", prepared, err)
	}
	f.context.fail.Store(true)
	if _, prepared, err := f.start.BuildTask(ctx, first.Task); err == nil || prepared {
		t.Fatal("Context 失败必须回滚")
	}
	pending, found, err := f.next.Next(ctx)
	if err != nil || !found || pending.ID != first.Task.ID || pending.Status != taskmodel.TaskPending || !reflect.ValueOf(pending.Input).IsZero() {
		t.Fatalf("构建失败不能消费或改变 Task: %+v %v", pending, err)
	}
	if active, err := f.repos.Tasks.ListActive(ctx); err != nil || len(active) != 0 {
		t.Fatalf("失败后不应留下活动 Task: %+v %v", active, err)
	}
	f.context.fail.Store(false)
	prepared, ok, err := f.start.BuildTask(ctx, pending)
	if err != nil || !ok || prepared.ID != first.Task.ID {
		t.Fatalf("领取必须继续使用入队 Task ID: %+v %v", prepared, err)
	}
	var text strings.Builder
	for _, entry := range prepared.Input.Context.Entries {
		for _, block := range entry.Content {
			text.WriteString(block.Text)
		}
	}
	if !strings.Contains(text.String(), "当前回答") || !strings.HasSuffix(text.String(), "第一个预约") || strings.Contains(text.String(), "第二个预约") {
		t.Fatalf("Context 应包含之前的回答和当前输入，但不包含后续输入: %s", text.String())
	}
	if _, ok, err := f.start.BuildTask(ctx, pending); err != nil || ok {
		t.Fatalf("重复领取不得创建新的 Task: %v", err)
	}
	if err := f.store.InTx(ctx, func(ctx context.Context) error {
		changed := prepared
		changed.Input.MessageSequenceBoundary++
		return f.repos.Tasks.Save(ctx, changed)
	}); !errors.Is(err, contracts.ErrRequestConflict) {
		t.Fatalf("执行快照冻结后不能改变: %v", err)
	}
	if _, err := f.lifecycle.End(ctx, prepared.ID, taskmodel.TaskCompleted, "", ""); err != nil {
		t.Fatal(err)
	}
	f.executions.End(prepared.AgentID, prepared.ID)
	if retry, err := f.queue.EnqueueTask(ctx, params); err != nil || !retry.ExistingTask || retry.Task.Status != taskmodel.TaskEnded {
		t.Fatalf("执行结束后仍应幂等返回原 Task: %v", err)
	}
	if err := f.store.Close(ctx); err != nil {
		t.Fatal(err)
	}
	reopened, err := jsonl.Open(ctx, f.root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close(context.Background())
	restored, err := reopened.Repositories().Tasks.FindNextPendingByAgent(ctx, "agent")
	if err != nil || restored.ID != second.Task.ID || restored.Status != taskmodel.TaskPending {
		t.Fatalf("日志恢复应保留剩余 Task: %+v %v", restored, err)
	}
}

func TestRuntimeAdvancesQueuedTasksOnlyAfterCompletion(t *testing.T) {
	for _, cancelTask := range []bool{false, true} {
		t.Run(map[bool]string{false: "completed", true: "cancelled"}[cancelTask], func(t *testing.T) {
			f := newFixture(t)
			started := make(chan taskmodel.Task, 3)
			release := make(chan struct{}, 3)
			runtime, err := agentruntime.NewRuntime(agentruntime.RuntimeConfig{
				AgentID:          "agent",
				Executions:       f.executions,
				MessageRecorders: f.messages.Resolve,
				TaskBuilder:      f.start,
				Lifecycle:        f.lifecycle,
				Queue:            f.next,
				RunLoop: func(ctx context.Context, task taskmodel.Task, _ agentruntime.MessageRecorder) (taskmodel.TaskOutcome, contracts.TaskFailureCode, error) {
					started <- task
					select {
					case <-release:
						return taskmodel.TaskCompleted, "", nil
					case <-ctx.Done():
						return taskmodel.TaskInterrupted, contracts.TaskFailureRuntimeCancelled, ctx.Err()
					}
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := runtime.Close(ctx); err != nil {
					t.Error(err)
				}
			})
			ctx := t.Context()
			direct, err := f.start.SendInput(ctx, start.SendInputParams{SessionID: "session", RequestID: "direct", Content: "当前输入"})
			if err != nil {
				t.Fatal(err)
			}
			if err := runtime.Activate(ctx, direct.Task); err != nil {
				t.Fatal(err)
			}
			awaitTask(t, started, direct.Task.ID)
			for _, id := range []contracts.TaskID{"later-1", "later-2"} {
				if _, err := f.queue.EnqueueTask(ctx, queue.EnqueueParams{ID: id, AgentID: "agent", RequestID: contracts.RequestID(id), Prompt: id.String()}); err != nil {
					t.Fatal(err)
				}
			}
			if activated, err := runtime.StartNext(ctx); err != nil || activated || f.context.calls.Load() != 1 {
				t.Fatalf("忙碌时不能激活预约 Task: %v", err)
			}
			if cancelTask {
				if _, err := f.agents.StopAgent(ctx, appagent.ControlParams{
					CommandID: "stop", AgentID: "agent", TargetTaskID: direct.Task.ID,
				}); err != nil {
					t.Fatalf("取消当前 Task 失败: %v", err)
				}
				if err := runtime.Close(ctx); err != nil {
					t.Fatal(err)
				}
				if len(started) != 0 || f.context.calls.Load() != 1 {
					t.Fatal("取消后不得自动执行后续 Task")
				}
				if pending, found, err := f.next.Next(ctx); err != nil || !found || pending.ID != "later-1" {
					t.Fatalf("取消不能丢弃预约 Task: %+v %v", pending, err)
				}
				return
			}
			release <- struct{}{}
			awaitTask(t, started, "later-1")
			release <- struct{}{}
			awaitTask(t, started, "later-2")
		})
	}
}

func awaitTask(t *testing.T, started <-chan taskmodel.Task, id contracts.TaskID) {
	t.Helper()
	select {
	case task := <-started:
		if task.ID != id {
			t.Fatalf("执行顺序不符: want=%s got=%s", id, task.ID)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("Task %s 未执行", id)
	}
}

func TestNewInputUsesHistoryAfterTaskEnds(t *testing.T) {
	for _, outcome := range []taskmodel.TaskOutcome{
		taskmodel.TaskCompleted,
		taskmodel.TaskYielded,
		taskmodel.TaskPaused,
		taskmodel.TaskInterrupted,
		taskmodel.TaskFailed,
	} {
		t.Run(string(outcome), func(t *testing.T) {
			f := newFixture(t)
			ctx := t.Context()
			directParams := start.SendInputParams{SessionID: "session", RequestID: "direct", Content: "当前输入"}
			direct, err := f.start.SendInput(ctx, directParams)
			if err != nil {
				t.Fatal(err)
			}
			queued, err := f.queue.EnqueueTask(ctx, queue.EnqueueParams{ID: "later", AgentID: "agent", RequestID: "later", Prompt: "预约输入"})
			if err != nil {
				t.Fatal(err)
			}
			if err := f.store.InTx(ctx, func(ctx context.Context) error {
				for _, message := range []sessionmodel.MessageData{
					{
						ID:         "answer",
						TaskID:     direct.Task.ID.String(),
						Role:       sessionmodel.RoleAssistant,
						AuthorKind: sessionmodel.AuthorAgent,
						Blocks: []sessionmodel.Block{
							{Kind: sessionmodel.BlockText, Text: "已有回答"},
							{Kind: sessionmodel.BlockToolCall, CallID: "history-call", Name: "read_file"},
						},
					},
					{
						ID:         "result",
						TaskID:     direct.Task.ID.String(),
						Role:       sessionmodel.RoleTool,
						AuthorKind: sessionmodel.AuthorTool,
						Blocks: []sessionmodel.Block{{
							Kind:   sessionmodel.BlockToolResult,
							CallID: "history-call",
							Name:   "read_file",
							Text:   "已有工具结果",
						}},
					},
				} {
					if _, err := f.repos.SessionMessages.Append(ctx, sessionmodel.SessionMessage{SessionID: "session", Data: message}); err != nil {
						return err
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if outcome == taskmodel.TaskPaused {
				if _, err := f.agents.PauseAgent(ctx, appagent.ControlParams{
					CommandID: "pause", AgentID: "agent", TargetTaskID: direct.Task.ID,
				}); err != nil {
					t.Fatal(err)
				}
			}
			var code contracts.TaskFailureCode
			if outcome == taskmodel.TaskFailed {
				code = contracts.TaskFailureProvider
			}
			ended, err := f.lifecycle.End(ctx, direct.Task.ID, outcome, code, "")
			if err != nil {
				t.Fatal(err)
			}
			f.executions.End(direct.Task.AgentID, direct.Task.ID)
			nextParams := start.SendInputParams{AgentID: "agent", RequestID: "next", Content: " 继续执行 "}
			next, err := f.start.SendInput(ctx, nextParams)
			if err != nil {
				t.Fatal(err)
			}
			if next.ExistingRequest || next.Task.ID == direct.Task.ID || next.Task.ID == queued.Task.ID || next.Task.Sequence != 0 {
				t.Fatalf("新请求必须创建独立 Task: %+v", next)
			}
			var text strings.Builder
			for _, entry := range next.Task.Input.Context.Entries {
				for _, block := range entry.Content {
					text.WriteString(block.Text)
				}
			}
			for _, content := range []string{"当前输入", "已有回答", "已有工具结果"} {
				if !strings.Contains(text.String(), content) {
					t.Fatalf("新 Task 应包含历史 %q: %s", content, text.String())
				}
			}
			if !strings.HasSuffix(text.String(), "继续执行") || strings.Contains(text.String(), "预约输入") {
				t.Fatalf("新 Context 应追加当前输入并排除预约输入: %s", text.String())
			}
			if previous, err := f.repos.Tasks.Get(ctx, direct.Task.ID); err != nil || !reflect.DeepEqual(previous, ended) {
				t.Fatalf("新请求不能改写旧 Task: %+v %v", previous, err)
			}
			if pending, found, err := f.next.Next(ctx); err != nil || !found || pending.ID != queued.Task.ID {
				t.Fatalf("新请求不能消费预约 Task: %+v %v", pending, err)
			}
			if retry, err := f.start.SendInput(ctx, directParams); err != nil || !retry.ExistingRequest || !reflect.DeepEqual(retry.Task, ended) {
				t.Fatalf("重复旧请求只能返回旧终态，不能重新执行: %+v %v", retry, err)
			}
			if retry, err := f.start.SendInput(ctx, nextParams); err != nil || !retry.ExistingRequest || retry.Task.ID != next.Task.ID {
				t.Fatalf("重复新请求不能重复创建 Task: %+v %v", retry, err)
			}
			messages, err := f.repos.SessionMessages.List(ctx, "session", 0, 0)
			if err != nil || len(messages) != 5 {
				t.Fatalf("重复提交不得新增用户消息: count=%d err=%v", len(messages), err)
			}
		})
	}
}

func TestTaskInputValidation(t *testing.T) {
	for _, input := range []string{"", " \t\n", strings.Repeat("a", taskmodel.MaxInputBytes+1), string([]byte{0xff})} {
		f := newFixture(t)
		ctx := t.Context()
		if _, err := f.start.SendInput(ctx, start.SendInputParams{AgentID: "agent", RequestID: "invalid", Content: input}); err == nil {
			t.Fatal("直接输入必须拒绝空白、超限或非法 UTF-8 文本")
		}
		if _, err := f.start.SendInput(ctx, start.SendInputParams{AgentID: "agent", RequestID: "valid", Content: "有效输入"}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.queue.EnqueueTask(ctx, queue.EnqueueParams{ID: "invalid", AgentID: "agent", RequestID: "invalid", Prompt: input}); err == nil {
			t.Fatal("预约输入必须拒绝空白、超限或非法 UTF-8 文本")
		}
		if _, err := f.repos.Tasks.FindByRequest(ctx, "agent", "invalid"); !errors.Is(err, contracts.ErrNotFound) {
			t.Fatalf("非法输入不能持久化 Task: %v", err)
		}
	}
}
