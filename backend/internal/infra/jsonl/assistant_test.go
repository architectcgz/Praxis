package jsonl

import (
	"context"
	"errors"
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
	_, inserted, err := writer.AppendAssistant(ctx, message, metadata)
	requireOK(t, err)
	if !inserted {
		t.Fatal("首次工具调用未保存")
	}
	_, inserted, err = writer.AppendAssistant(ctx, message, metadata)
	requireOK(t, err)
	if inserted {
		t.Fatal("同一调用声明重复保存")
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
		t.Fatal("未结算调用不可记录回执")
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
		t.Fatal("回执回滚却计费")
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
		t.Fatal("重启后已配对调用重新出现")
	}
	receipt.ID = "another-receipt"
	writer, err = f.repos.Messages.Writer(ctx, "session", "agent")
	requireOK(t, err)
	if err := writer.AppendToolResult(ctx, receipt); err == nil {
		t.Fatal("同一调用不可产生第二条回执")
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
		t.Fatal("无法确认的外部文件变更必须封锁 Store")
	}
	if err := f.store.append("session", []byte("unwritten\n")); err == nil || f.store.fault == nil {
		t.Fatal("外部文件变更后 append 必须封锁写入")
	}
	requireOK(t, f.store.Close(ctx))
	requireOK(t, os.Remove(path))
	requireOK(t, os.Rename(path+".saved", path))
	f.restart(t)
	stored, err := f.repos.Tasks.Get(ctx, task.ID)
	requireOK(t, err)
	if stored.Progress.OutputBytes != 0 {
		t.Fatal("失败事务污染重启结果")
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
