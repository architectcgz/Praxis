package task

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	contextmodel "praxis/internal/core/context"
	"praxis/internal/core/model"
	projectmodel "praxis/internal/core/project"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	workspacemodel "praxis/internal/core/workspace"
	"praxis/internal/infra/jsonl"
)

func textMessage(id, taskID string, sequence uint64, role sessionmodel.Role) sessionmodel.MessageData {
	author := sessionmodel.AuthorUser
	if role == sessionmodel.RoleAssistant {
		author = sessionmodel.AuthorAgent
	}
	return sessionmodel.MessageData{
		ID:         id,
		TaskID:     taskID,
		Sequence:   sequence,
		Role:       role,
		AuthorKind: author,
		CreatedAt:  time.Unix(1, 0).UTC(),
		Blocks:     []sessionmodel.Block{{Kind: sessionmodel.BlockText, Text: id}},
	}
}

func TestSelectTaskMessagesBoundaryAndLateReceipts(t *testing.T) {
	messages := []sessionmodel.MessageData{
		textMessage("history", "a", 1, sessionmodel.RoleUser),
		{
			ID:       "call-a",
			TaskID:   "a",
			Sequence: 2,
			Role:     sessionmodel.RoleAssistant,
			Blocks: []sessionmodel.Block{
				{Kind: sessionmodel.BlockToolCall, CallID: "same", Name: "bash"},
			},
		},
		textMessage("input-c", "c", 3, sessionmodel.RoleUser),
		textMessage("input-b", "b", 4, sessionmodel.RoleUser),
		textMessage("new-input", "d", 5, sessionmodel.RoleUser),
		{
			ID:       "call-c",
			TaskID:   "c",
			Sequence: 6,
			Role:     sessionmodel.RoleAssistant,
			Blocks: []sessionmodel.Block{
				{Kind: sessionmodel.BlockToolCall, CallID: "same", Name: "bash"},
			},
		},
		{
			ID:       "result-c",
			TaskID:   "c",
			Sequence: 7,
			Role:     sessionmodel.RoleTool,
			Blocks: []sessionmodel.Block{
				{
					Kind:   sessionmodel.BlockToolResult,
					CallID: "same",
					Name:   "bash",
					Text:   "wrong",
				},
			},
		},
		{
			ID:       "result-a",
			TaskID:   "a",
			Sequence: 8,
			Role:     sessionmodel.RoleTool,
			Blocks: []sessionmodel.Block{
				{
					Kind:   sessionmodel.BlockToolResult,
					CallID: "same",
					Name:   "bash",
					Text:   "late",
				},
				{
					Kind:   sessionmodel.BlockToolResult,
					CallID: "unselected",
					Name:   "bash",
					Text:   "unrelated",
				},
			},
		},
		textMessage("answer-b", "b", 9, sessionmodel.RoleAssistant),
	}
	input := taskmodel.Input{
		MessageSequenceBoundary: 4,
		CurrentInputMessageID:   "input-b",
		ExcludedMessageIDs:      []string{"input-c"},
	}
	history, continuation := selectTaskMessages(messages, input, "b")
	var ids []string
	for _, message := range history {
		ids = append(ids, message.ID)
	}
	if !reflect.DeepEqual(ids, []string{"history", "call-a", "input-b", "result-a"}) ||
		len(continuation) != 1 || continuation[0].ID != "answer-b" {
		t.Fatalf("历史或续接消息错误: %v, %+v", ids, continuation)
	}
	if len(history[3].Blocks) != 1 || len(messages[7].Blocks) != 2 {
		t.Fatal("延迟回执必须只包含所选调用，且不能修改源消息")
	}
	built, err := contextmodel.NewContextBuilder().Build(contextmodel.BuildInput{
		ContextWindow:         8192,
		MaxOutputTokens:       1024,
		Messages:              history,
		CurrentInputMessageID: input.CurrentInputMessageID,
		ContinuationMessages:  continuation,
	})
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, entry := range built.Context.Entries {
		for _, block := range entry.Content {
			if block.Text != "" {
				texts = append(texts, block.Text)
			}
		}
	}
	if !reflect.DeepEqual(texts, []string{"history", "late", "input-b", "answer-b"}) {
		t.Fatalf("延迟回执必须跟在历史调用后: %v", texts)
	}
	// 立即创建的 Task 输入在历史边界之后独立写入，零边界也不能纳入后续消息。
	for _, boundary := range []uint64{0, 2} {
		input.MessageSequenceBoundary = boundary
		history, _ := selectTaskMessages(messages, input, "b")
		found := false
		for _, message := range history {
			found = found || message.ID == "input-b"
			if message.ID == "new-input" || boundary == 0 && message.ID != "input-b" {
				t.Fatalf("边界 %d 混入未来历史: %+v", boundary, history)
			}
		}
		if !found {
			t.Fatal("当前输入必须独立纳入")
		}
	}
}

type contextFixture struct {
	store *jsonl.Store
	repos jsonl.Repositories
	root string
	agent agentmodel.Agent
	input taskmodel.Input
}

func newContextFixture(t *testing.T) *contextFixture {
	t.Helper()
	f := &contextFixture{root: t.TempDir()}
	var err error
	f.store, err = jsonl.Open(t.Context(), f.root)
	if err != nil {
		t.Fatal(err)
	}
	f.repos = f.store.Repositories()
	t.Cleanup(func() { _ = f.store.Close(context.Background()) })
	at := time.Unix(1, 0).UTC()
	workspacePath := t.TempDir()
	project, err := projectmodel.NewProject("project", "项目", workspacePath, "workspace", at)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := workspacemodel.NewWorkspace("workspace", project.ID, workspacemodel.WorkspaceProjectRoot, workspacePath, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.InTx(t.Context(), func(ctx context.Context) error {
		if err := f.repos.Projects.Save(ctx, project); err != nil {
			return err
		}
		return f.repos.Workspaces.Save(ctx, workspace)
	}); err != nil {
		t.Fatal(err)
	}
	session, err := sessionmodel.NewSession("session", project.ID, workspace.ID, at)
	if err != nil {
		t.Fatal(err)
	}
	f.agent, err = agentmodel.NewAgent("agent", session.ID, agentmodel.DefinitionPrimary, contracts.ProfilePrimary, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.InTx(t.Context(), func(ctx context.Context) error {
		if err := f.repos.Sessions.Save(ctx, session); err != nil {
			return err
		}
		return f.repos.Agents.Save(ctx, f.agent)
	}); err != nil {
		t.Fatal(err)
	}
	f.input = taskmodel.Input{
		AgentDefinitionID:       f.agent.DefinitionID,
		AgentDefinitionRevision: "revision",
		CurrentInputMessageID:   "input-b",
		WorkspacePath:           workspacePath,
		Model:                   model.Config{
			ProviderID:      "provider",
			ModelID:         "model",
			APIFormat:       "openai_chat_completions",
			ContextWindow:   8192,
			MaxOutputTokens: 1024,
			BaseURL:         "https://example.com",
		},
	}
	return f
}

func (f *contextFixture) builder(t *testing.T) *TaskContextBuilder {
	t.Helper()
	builder, err := NewTaskContextBuilder(ContextBuilderConfig{
		Transactions: f.store,
		Sessions:     f.repos.Sessions,
		Tasks:        f.repos.Tasks,
		Contexts:     f.repos.Contexts,
		Messages:     f.repos.Messages,
	})
	if err != nil {
		t.Fatal(err)
	}
	return builder
}

func TestQueuedContextRemainsStableAfterStateChangeAndReload(t *testing.T) {
	f := newContextFixture(t)
	at := time.Unix(1, 0).UTC()
	tasks := make(map[string]taskmodel.Task)
	for index, id := range []string{"a", "b", "c"} {
		task, err := taskmodel.NewPendingTask(contracts.TaskID(id), f.agent.SessionID, f.agent.ID, contracts.RequestID(id), uint64(index+1), at)
		if err != nil {
			t.Fatal(err)
		}
		tasks[id] = task
		if err := f.store.InTx(t.Context(), func(ctx context.Context) error {
			if err := f.repos.Tasks.Save(ctx, task); err != nil {
				return err
			}
			_, err := f.repos.Messages.Append(ctx, f.agent, textMessage("input-"+id, id, 0, sessionmodel.RoleUser))
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	// A 已执行，B 和 C 仍在排队。
	finish := func(id string) {
		t.Helper()
		task := tasks[id]
		input := f.input
		input.CurrentInputMessageID = "input-" + id
		if err := task.Prepare(input); err != nil {
			t.Fatal(err)
		}
		if err := f.store.InTx(t.Context(), func(ctx context.Context) error {
			if err := f.repos.Tasks.Save(ctx, task); err != nil {
				return err
			}
			if err := task.MarkRunning(at); err != nil {
				return err
			}
			if err := task.End(taskmodel.TaskCompleted, "", at); err != nil {
				return err
			}
			return f.repos.Tasks.Save(ctx, task)
		}); err != nil {
			t.Fatal(err)
		}
	}
	finish("a")
	shared, err := contextmodel.NewSessionContextEntry("shared", f.agent.SessionID, contextmodel.SessionContextDecision, "", "启动前共享内容", at)
	if err != nil {
		t.Fatal(err)
	}
	var input taskmodel.Input
	if err := f.store.InTx(t.Context(), func(ctx context.Context) error {
		if err := f.repos.Contexts.Save(ctx, shared); err != nil {
			return err
		}
		var err error
		input, err = f.builder(t).BuildInitialContext(ctx, f.agent, f.input, "")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(input.ExcludedMessageIDs, []string{"input-c"}) || len(input.SessionContextRefs) != 1 || input.MessageSequenceBoundary == 0 {
		t.Fatalf("初始选择错误: %+v", input)
	}
	b := tasks["b"]
	if err := b.Prepare(input); err != nil {
		t.Fatal(err)
	}
	if err := f.agent.Start(b.ID, at); err != nil {
		t.Fatal(err)
	}
	if err := f.store.InTx(t.Context(), func(ctx context.Context) error {
		if err := f.repos.Tasks.Save(ctx, b); err != nil {
			return err
		}
		return f.repos.Agents.Save(ctx, f.agent)
	}); err != nil {
		t.Fatal(err)
	}
	before, err := f.builder(t).RebuildTaskContext(t.Context(), b)
	if err != nil {
		t.Fatal(err)
	}
	finish("c")
	newShared, err := contextmodel.NewSessionContextEntry("later", f.agent.SessionID, contextmodel.SessionContextDecision, "", "启动后共享内容", at.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.InTx(t.Context(), func(ctx context.Context) error {
		if _, err := f.repos.Messages.Append(ctx, f.agent, textMessage("new-user", "", 0, sessionmodel.RoleUser)); err != nil {
			return err
		}
		return f.repos.Contexts.Save(ctx, newShared)
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.store, err = jsonl.Open(t.Context(), f.root)
	if err != nil {
		t.Fatal(err)
	}
	f.repos = f.store.Repositories()
	restored, err := f.repos.Tasks.Get(t.Context(), b.ID)
	if err != nil {
		t.Fatal(err)
	}
	after, err := f.builder(t).RebuildTaskContext(t.Context(), restored)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(input, restored.Input) || before.Digest != after.Digest {
		t.Fatal("Task 状态、后续消息或共享条目变化影响了恢复上下文")
	}
	call := textMessage("call-b", "b", 0, sessionmodel.RoleAssistant)
	call.Blocks = []sessionmodel.Block{{Kind: sessionmodel.BlockToolCall, CallID: "same", Name: "bash"}}
	result := sessionmodel.MessageData{
		ID:         "result-b",
		TaskID:     "b",
		Role:       sessionmodel.RoleTool,
		AuthorKind: sessionmodel.AuthorTool,
		CreatedAt:  at,
		Blocks: []sessionmodel.Block{{
			Kind:   sessionmodel.BlockToolResult,
			CallID: "same",
			Name:   "bash",
			Text:   "done",
		}},
	}
	if err := f.store.InTx(t.Context(), func(ctx context.Context) error {
		if _, err := f.repos.Messages.Append(ctx, f.agent, call); err != nil {
			return err
		}
		_, err := f.repos.Messages.Append(ctx, f.agent, result)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	continued, err := f.builder(t).RebuildTaskContext(t.Context(), restored)
	if err != nil || len(continued.TaskMessages) != 2 || len(continued.Context.Entries) != len(before.Context.Entries)+2 {
		t.Fatalf("恢复遗漏当前 Task 已提交的工具交互: %+v, %v", continued, err)
	}
	// 恢复必须检查启动时引用的共享版本，不能悄悄使用其他条目。
	restored.Input.SessionContextRefs[0].Digest = "wrong"
	if _, err := f.builder(t).RebuildTaskContext(t.Context(), restored); !errors.Is(err, contracts.ErrRevisionConflict) {
		t.Fatalf("共享上下文版本不一致时应拒绝恢复: %v", err)
	}

}

func TestContextUsesAgentOwnedStream(t *testing.T) {
	f := newContextFixture(t)
	private, err := agentmodel.NewAgent("private", f.agent.SessionID, agentmodel.DefinitionDelegate, contracts.ProfileDelegate, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	var input taskmodel.Input
	if err := f.store.InTx(t.Context(), func(ctx context.Context) error {
		if err := f.repos.Agents.Save(ctx, private); err != nil {
			return err
		}
		if _, err := f.repos.Messages.Append(ctx, f.agent, textMessage("primary-only", "", 0, sessionmodel.RoleUser)); err != nil {
			return err
		}
		if _, err := f.repos.Messages.Append(ctx, private, textMessage("private-only", "", 0, sessionmodel.RoleUser)); err != nil {
			return err
		}
		input = f.input
		input.AgentDefinitionID = private.DefinitionID
		input.CurrentInputMessageID = "private-only"
		var err error
		input, err = f.builder(t).BuildInitialContext(ctx, private, input, "")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	built, err := f.builder(t).RebuildTaskContext(t.Context(), taskmodel.Task{
		ID:        "private-task",
		SessionID: private.SessionID,
		AgentID:   private.ID,
		Input:     input,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(built.Context.Entries) != 1 || built.Context.Entries[0].Content[0].Text != "private-only" || len(input.SessionContextRefs) != 0 {
		t.Fatalf("私有流混入其他 Agent 的内容: %+v", built.Context)
	}
}
