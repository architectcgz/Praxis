package task

import (
	"context"
	"slices"
	"testing"
	"time"

	"praxis/internal/contracts"
	contextmodel "praxis/internal/core/context"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	runtimequeue "praxis/internal/service/runtime/queue"
)

func TestFrozenSelectionAndContinuation(t *testing.T) {
	f := newHookFixture(t)
	ctx := context.Background()
	first, err := f.svc.SendInput(ctx, hookInput())
	if err != nil {
		t.Fatal(err)
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
	for _, id := range []contracts.TaskID{"queued-one", "queued-two"} {
		if _, err := queue.EnqueueTask(ctx, runtimequeue.EnqueueParams{
			ID:        id,
			AgentID:   "agent",
			RequestID: contracts.RequestID(id),
			Prompt:    id.String(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.svc.Settle(ctx, first.Task.ID, taskmodel.TaskCompleted, "", ""); err != nil {
		t.Fatal(err)
	}
	pending, err := f.repos.Tasks.Get(ctx, "queued-one")
	if err != nil {
		t.Fatal(err)
	}
	prepared, ok, err := f.svc.PrepareQueuedTask(ctx, pending)
	if err != nil || !ok {
		t.Fatal(prepared, ok, err)
	}
	if !slices.Contains(prepared.ContextSelection.ExcludedMessageIDs, "input:queued-two") {
		t.Fatal("frozen history did not exclude other pending inputs")
	}
	agent, err := f.repos.Agents.Get(ctx, "agent")
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.repos.Messages.Append(ctx, agent, sessionmodel.MessageData{
		ID:         "late",
		Role:       sessionmodel.RoleUser,
		AuthorKind: sessionmodel.AuthorUser,
		Blocks:     []sessionmodel.Block{{Kind: sessionmodel.BlockText, Text: "冻结之后的输入"}},
		CreatedAt:  time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	writer, err := f.repos.Messages.Writer(ctx, "session", "agent")
	if err != nil {
		t.Fatal(err)
	}
	turn := contracts.NewTurnID(prepared.ID, 1)
	_, _, err = writer.AppendAssistant(ctx, testAssistantWrite(t, sessionmodel.MessageData{
		ID:         "assistant:" + turn.String(),
		TaskID:     prepared.ID.String(),
		RequestID:  prepared.RequestID.String(),
		TurnID:     turn.String(),
		Role:       sessionmodel.RoleAssistant,
		AuthorKind: sessionmodel.AuthorAgent,
		AuthorID:   "agent",
		StopReason: "completed",
		Blocks:     []sessionmodel.Block{{Kind: sessionmodel.BlockText, Text: "当前 Task 输出"}},
	}, contracts.ToolInvocationContext{
		SessionID:     "session",
		AgentID:       "agent",
		TaskID:        prepared.ID,
		TurnID:        turn,
		WorkspacePath: prepared.Config.WorkspacePath,
	}))
	if err != nil {
		t.Fatal(err)
	}
	f.restart(t)
	content, continuation, err := f.svc.LoadContext(ctx, prepared)
	if err != nil {
		t.Fatal(err)
	}
	if len(continuation) != 1 || continuation[0].TaskID != prepared.ID.String() {
		t.Fatal(continuation)
	}
	for _, entry := range content.Entries {
		for _, block := range entry.Content {
			if block.Text == "queued-two" || block.Text == "冻结之后的输入" {
				t.Fatal("recovery crossed the frozen boundary or included excluded entries")
			}
		}
	}
}

func TestLateToolResultsPairByTaskAndRespectExclusions(t *testing.T) {
	messages := []sessionmodel.MessageData{
		{
			ID:       "call-a",
			Sequence: 1,
			TaskID:   "a",
			Role:     sessionmodel.RoleAssistant,
			Blocks:   []sessionmodel.Block{{Kind: sessionmodel.BlockToolCall, CallID: "same", Name: "read_file"}},
		},
		{
			ID:       "excluded",
			Sequence: 2,
			TaskID:   "b",
			Role:     sessionmodel.RoleAssistant,
			Blocks:   []sessionmodel.Block{{Kind: sessionmodel.BlockToolCall, CallID: "same", Name: "read_file"}},
		},
		{
			ID:       "current",
			Sequence: 3,
			TaskID:   "current-task",
			Role:     sessionmodel.RoleUser,
			Blocks:   []sessionmodel.Block{{Kind: sessionmodel.BlockText, Text: "当前输入"}},
		},
		{
			ID:       "result-a",
			Sequence: 4,
			TaskID:   "a",
			Role:     sessionmodel.RoleTool,
			Blocks: []sessionmodel.Block{{
				Kind:   sessionmodel.BlockToolResult,
				CallID: "same",
				Name:   "read_file",
				Text:   "a 的结果",
			}},
		},
		{
			ID:       "result-b",
			Sequence: 5,
			TaskID:   "b",
			Role:     sessionmodel.RoleTool,
			Blocks: []sessionmodel.Block{{
				Kind:   sessionmodel.BlockToolResult,
				CallID: "same",
				Name:   "read_file",
				Text:   "不可纳入",
			}},
		},
		{
			ID:       "continuation",
			Sequence: 6,
			TaskID:   "current-task",
			Role:     sessionmodel.RoleAssistant,
			Blocks:   []sessionmodel.Block{{Kind: sessionmodel.BlockText, Text: "续接输出"}},
		},
	}
	selected, continuation := selectTaskMessages(messages, taskmodel.ContextSelection{MessageSequenceBoundary: 3, ExcludedMessageIDs: []string{"excluded"}}, "current", "current-task")
	if len(selected) != 3 || selected[2].ID != "result-a" || len(continuation) != 1 || continuation[0].ID != "continuation" {
		t.Fatal(selected, continuation)
	}
	entries := historyEntries(selected)
	if len(entries) != 3 || entries[1].Role != contextmodel.ContextRoleTool || entries[1].Content[0].Text != "a 的结果" {
		t.Fatal(entries)
	}
}

func TestContextKeepsReasoningStateAndFrozenSharedFacts(t *testing.T) {
	state := &contracts.ReasoningState{Protocol: contracts.ProtocolOpenAIResponses, Version: contracts.ReasoningStateVersion, Payload: []byte(`{"encrypted_content":"state"}`)}
	entry, ok := entryFromMessageData(sessionmodel.MessageData{Role: sessionmodel.RoleAssistant, Blocks: []sessionmodel.Block{
		{Kind: sessionmodel.BlockThinking, Text: "仅供展示"},
		{Kind: sessionmodel.BlockReasoningState, ReasoningState: state},
		{Kind: sessionmodel.BlockText, Text: "正文"},
	}})
	if !ok || len(entry.Content) != 2 || entry.Content[0].ReasoningState == state {
		t.Fatal("reasoning state was not isolated or display thinking entered context")
	}
	f := newHookFixture(t)
	ctx := context.Background()
	shared, err := sessionmodel.NewSessionContextEntry("context", "session", sessionmodel.SessionContextReference, "", "共享事实", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.InTx(ctx, func(txCtx context.Context) error { return f.repos.Contexts.Append(txCtx, shared) }); err != nil {
		t.Fatal(err)
	}
	created, err := f.svc.SendInput(ctx, hookInput())
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Task.ContextSelection.SessionContextRefs) != 1 {
		t.Fatal("shared facts were not frozen")
	}
	content, _, err := f.svc.LoadContext(ctx, created.Task)
	if err != nil {
		t.Fatal(err)
	}
	if !content.Entries[0].Protected {
		t.Fatal("shared facts must preserve their original text")
	}
	corrupt := created.Task
	corrupt.ContextSelection = created.Task.ContextSelection.Clone()
	corrupt.ContextSelection.SessionContextRefs[0].Digest = "wrong"
	if _, _, err := f.svc.LoadContext(ctx, corrupt); err == nil {
		t.Fatal("recovery must verify the frozen digest")
	}
}
