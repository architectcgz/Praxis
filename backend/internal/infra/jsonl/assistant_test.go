package jsonl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"praxis/internal/contracts"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	toolmodel "praxis/internal/core/tool_invocation"
	"praxis/internal/repository"
	"praxis/internal/request"
	applicationruntime "praxis/internal/service/runtime"
)

func (f *storeFixture) toolCall(t testing.TB, task taskmodel.Task) (repository.MessageWriter, contracts.ToolInvocationContext) {
	t.Helper()
	ctx := context.Background()
	writer, err := f.repos.Messages.Writer(ctx, "session", "agent")
	requireOK(t, err)
	metadata := contracts.ToolInvocationContext{
		SessionID:     "session",
		AgentID:       "agent",
		TaskID:        task.ID,
		TurnID:        contracts.NewTurnID(task.ID, 1),
		WorkspacePath: task.Config.WorkspacePath,
	}
	message := sessionmodel.MessageData{
		ID:         "assistant:" + metadata.TurnID.String(),
		TaskID:     task.ID.String(),
		RequestID:  task.RequestID.String(),
		TurnID:     metadata.TurnID.String(),
		Role:       sessionmodel.RoleAssistant,
		AuthorKind: sessionmodel.AuthorAgent,
		AuthorID:   "agent",
		StopReason: "tool_calls",
		Blocks: []sessionmodel.Block{{
			Kind:   sessionmodel.BlockToolCall,
			CallID: "call",
			Name:   "read_file",
			Input:  []byte(`{}`),
		}},
	}
	_, inserted, err := writer.AppendAssistant(ctx, testAssistantWrite(t, message, metadata))
	requireOK(t, err)
	if !inserted {
		t.Fatal("initial tool invocation was not saved")
	}
	_, inserted, err = writer.AppendAssistant(ctx, testAssistantWrite(t, message, metadata))
	requireOK(t, err)
	if inserted {
		t.Fatal("same call declaration was saved twice")
	}
	return writer, metadata
}

func TestToolReceiptAtomicityIdempotencyAndConcurrentBilling(t *testing.T) {
	f := newStoreFixture(t)
	task := f.activeTask(t)
	writer, metadata := f.toolCall(t, task)
	ctx := context.Background()
	missing, err := f.repos.Messages.ListMissingToolResults(ctx)
	requireOK(t, err)
	if len(missing) != 1 || missing[0].TurnID != metadata.TurnID {
		t.Fatal(missing)
	}
	receipt := sessionmodel.MessageData{
		ID:         "tool-result:" + task.ID.String() + ":call",
		TaskID:     task.ID.String(),
		RequestID:  task.RequestID.String(),
		TurnID:     metadata.TurnID.String(),
		Role:       sessionmodel.RoleTool,
		AuthorKind: sessionmodel.AuthorTool,
		AuthorID:   "read_file",
		Blocks: []sessionmodel.Block{{
			Kind:    sessionmodel.BlockToolResult,
			CallID:  "call",
			Name:    "read_file",
			Text:    "denied",
			IsError: true,
		}},
	}
	if err := writer.AppendToolResult(ctx, receipt); err == nil {
		t.Fatal("unsettled invocation must not have a receipt")
	}
	requireOK(t, f.store.InTx(ctx, func(txCtx context.Context) error {
		inv, err := f.repos.ToolInvocations.FindByTaskCall(txCtx, task.ID, "call")
		if err != nil {
			return err
		}
		if err := inv.Deny(toolmodel.ToolInvocationResult{InlineContent: "denied", ErrorCode: toolmodel.ToolFailureNotAllowed}, time.Now()); err != nil {
			return err
		}
		return f.repos.ToolInvocations.Save(txCtx, inv)
	}))
	sentinel := errors.New("rollback receipt")
	err = f.store.InTx(ctx, func(txCtx context.Context) error {
		if err := writer.AppendToolResult(txCtx, receipt); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	stored, err := f.repos.Tasks.Get(ctx, task.ID)
	requireOK(t, err)
	if stored.Progress.OutputBytes != 0 {
		t.Fatal("rolled-back receipt was charged")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if err := writer.AppendToolResult(ctx, receipt); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	stored, err = f.repos.Tasks.Get(ctx, task.ID)
	requireOK(t, err)
	if stored.Progress.OutputBytes != int64(len("denied")) {
		t.Fatal(stored.Progress)
	}
	missing, err = f.repos.Messages.ListMissingToolResults(ctx)
	requireOK(t, err)
	if len(missing) != 0 {
		t.Fatal(missing)
	}
	f.restart(t)
	missing, err = f.repos.Messages.ListMissingToolResults(ctx)
	requireOK(t, err)
	if len(missing) != 0 {
		t.Fatal("paired invocation reappeared after restart")
	}
	receipt.ID = "another-receipt"
	writer, err = f.repos.Messages.Writer(ctx, "session", "agent")
	requireOK(t, err)
	if err := writer.AppendToolResult(ctx, receipt); err == nil {
		t.Fatal("same invocation must not produce a second receipt")
	}
}

func TestWriteFailureLeavesNoPublishedIndex(t *testing.T) {
	f := newStoreFixture(t)
	task := f.activeTask(t)
	ctx := context.Background()
	path, err := f.store.path("session")
	requireOK(t, err)
	err = f.store.InTx(ctx, func(txCtx context.Context) error {
		task.Progress.OutputBytes = 100
		if err := f.repos.Tasks.Save(txCtx, task); err != nil {
			return err
		}
		if err := os.Rename(path, path+".saved"); err != nil {
			return err
		}
		return os.Mkdir(path, 0700)
	})
	if err == nil {
		t.Fatal("unverifiable external file changes must block the store")
	}
	if err := f.store.append("session", []byte("unwritten\n")); err == nil || f.store.fault == nil {
		t.Fatal("append must block writes after an external file change")
	}
	requireOK(t, f.store.Close(ctx))
	requireOK(t, os.Remove(path))
	requireOK(t, os.Rename(path+".saved", path))
	f.restart(t)
	stored, err := f.repos.Tasks.Get(ctx, task.ID)
	requireOK(t, err)
	if stored.Progress.OutputBytes != 0 {
		t.Fatal("failed transaction corrupted restart results")
	}
}

func TestConcurrentToolApprovalIsIdempotent(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	task := f.activeTask(t)
	f.toolCall(t, task)
	requireOK(t, f.store.InTx(ctx, func(txCtx context.Context) error {
		inv, err := f.repos.ToolInvocations.FindByTaskCall(txCtx, task.ID, "call")
		if err != nil {
			return err
		}
		inv.NormalizedArguments = []byte(`{}`)
		if err := inv.AwaitApproval(); err != nil {
			return err
		}
		return f.repos.ToolInvocations.Save(txCtx, inv)
	}))
	approvals := applicationruntime.ToolApprovals{Transactions: f.store, Tasks: f.repos.Tasks, Invocations: f.repos.ToolInvocations}
	inv, err := f.repos.ToolInvocations.FindByTaskCall(ctx, task.ID, "call")
	requireOK(t, err)
	params := request.ToolDecision{
		SessionID:       "session",
		AgentID:         "agent",
		TaskID:          task.ID,
		CallID:          "call",
		ArgumentsDigest: toolmodel.ArgumentsDigest(inv.Name, inv.NormalizedArguments),
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := approvals.Decide(ctx, params, true); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	inv, err = f.repos.ToolInvocations.FindByTaskCall(ctx, task.ID, "call")
	requireOK(t, err)
	if inv.Status != toolmodel.ToolInvocationApproved {
		t.Fatal(inv)
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

func TestAssistantFinalEvidenceIsExplicitAndAtomic(t *testing.T) {
	f := newStoreFixture(t)
	value := f.activeTask(t)
	ctx := context.Background()
	writer, err := f.repos.Messages.Writer(ctx, "session", "agent")
	requireOK(t, err)
	turn := contracts.NewTurnID(value.ID, 1)
	message := sessionmodel.MessageData{
		ID:         "assistant:" + turn.String(),
		RequestID:  value.RequestID.String(),
		TaskID:     value.ID.String(),
		TurnID:     turn.String(),
		Role:       sessionmodel.RoleAssistant,
		AuthorKind: sessionmodel.AuthorAgent,
		AuthorID:   "agent",
		StopReason: "completed",
		Blocks:     []sessionmodel.Block{{Kind: sessionmodel.BlockText, Text: "done"}},
	}
	write := repository.AssistantWrite{Message: message, FinalOutput: &taskmodel.FinalOutput{AssistantID: message.ID, StopReason: message.StopReason}}
	sentinel := errors.New("rollback")
	requireRollback := f.store.InTx(ctx, func(ctx context.Context) error {
		if _, _, err := writer.AppendAssistant(ctx, write); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(requireRollback, sentinel) {
		t.Fatal(requireRollback)
	}
	stored, err := f.repos.Tasks.Get(ctx, value.ID)
	requireOK(t, err)
	if stored.Progress.FinalAssistantID != "" {
		t.Fatal("rolled-back final evidence persisted")
	}
	stream, err := f.repos.Messages.LoadMessages(ctx, "session", "agent", 0)
	requireOK(t, err)
	if len(stream.Messages) != 1 {
		t.Fatal("rolled-back assistant persisted")
	}
	write.FinalOutput = nil
	_, inserted, err := writer.AppendAssistant(ctx, write)
	requireOK(t, err)
	if !inserted {
		t.Fatal("assistant was not inserted")
	}
	stored, err = f.repos.Tasks.Get(ctx, value.ID)
	requireOK(t, err)
	if stored.Progress.FinalAssistantID != "" {
		t.Fatal("repository inferred completion from StopReason")
	}
	write.FinalOutput = &taskmodel.FinalOutput{AssistantID: message.ID, StopReason: message.StopReason}
	if _, inserted, err := writer.AppendAssistant(ctx, write); err == nil || inserted {
		t.Fatal("replay changed final evidence")
	}
}

func TestAssistantDuplicateKeepsInvocationIdentity(t *testing.T) {
	f := newStoreFixture(t)
	value := f.activeTask(t)
	writer, metadata := f.toolCall(t, value)
	ctx := context.Background()
	initial, err := f.repos.ToolInvocations.FindByTaskCall(ctx, value.ID, "call")
	requireOK(t, err)
	stream, err := f.repos.Messages.LoadMessages(ctx, "session", "agent", 0)
	requireOK(t, err)
	message := stream.Messages[len(stream.Messages)-1]
	write := testAssistantWrite(t, message, metadata)
	write.ToolCalls[0].ID = "replacement-id"
	_, inserted, err := writer.AppendAssistant(ctx, write)
	requireOK(t, err)
	if inserted {
		t.Fatal("duplicate inserted")
	}
	actual, err := f.repos.ToolInvocations.FindByTaskCall(ctx, value.ID, "call")
	requireOK(t, err)
	if initial.ID != actual.ID || !initial.CreatedAt.Equal(actual.CreatedAt) {
		t.Fatal("duplicate replaced persisted identity")
	}
}

func TestAssistantCallFailureRollsBackMessageAndEvidence(t *testing.T) {
	f := newStoreFixture(t)
	value := f.activeTask(t)
	writer, metadata := f.toolCall(t, value)
	ctx := context.Background()
	stream, err := f.repos.Messages.LoadMessages(ctx, "session", "agent", 0)
	requireOK(t, err)
	message := stream.Messages[len(stream.Messages)-1]
	message.ID = "conflicting-assistant"
	message.TurnID = contracts.NewTurnID(value.ID, 2).String()
	metadata.TurnID = contracts.TurnID(message.TurnID)
	write := testAssistantWrite(t, message, metadata)
	if _, inserted, err := writer.AppendAssistant(ctx, write); err == nil || inserted {
		t.Fatal("reused call in a different turn accepted")
	}
	stream, err = f.repos.Messages.LoadMessages(ctx, "session", "agent", 0)
	requireOK(t, err)
	if len(stream.Messages) != 2 {
		t.Fatal("failed call append left a message")
	}
}
