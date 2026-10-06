package start_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

	"praxis/internal/contracts"
	taskmodel "praxis/internal/core/task"
	turnmodel "praxis/internal/core/turn"
	"praxis/internal/loop"
	"praxis/internal/service/runtime/task/start"
	taskturn "praxis/internal/service/runtime/task/turn"
	toolcontracts "praxis/internal/tools/contracts"
)

// scriptedModel 输出脚本化事件：第一次请求产生工具调用，之后直接结束 loop。
type scriptedModel struct {
	fail    bool
	replies atomic.Int32
}

func (m *scriptedModel) Stream(_ context.Context, request loop.ModelRequest) (<-chan loop.ModelStreamEvent, error) {
	if request.TurnID == "" {
		return nil, errors.New("模型请求必须携带迭代身份")
	}
	events := make(chan loop.ModelStreamEvent, 4)
	reply := m.replies.Add(1)
	go func() {
		defer close(events)
		if m.fail {
			events <- loop.ModelStreamEvent{Kind: loop.StreamError, Err: errors.New("provider unavailable")}
			return
		}
		if reply == 1 {
			events <- loop.ModelStreamEvent{Kind: loop.StreamToolCall, ToolCall: toolcontracts.ToolCall{
				ID: "call-1", Name: contracts.ToolReadFile, Arguments: json.RawMessage(`{"path":"README.md"}`),
			}}
			return
		}
		events <- loop.ModelStreamEvent{Kind: loop.StreamTextDelta, Text: "完成"}
	}()
	return events, nil
}

type scriptedModelBuilder struct{ fail bool }

func (b scriptedModelBuilder) BuildTaskModel(contracts.ModelSnapshot) (loop.TaskModel, error) {
	return loop.TaskModel{Stream: &scriptedModel{fail: b.fail}, MaxOutputTokens: 1024}, nil
}

type emptyCatalog struct{}

func (emptyCatalog) List() []toolcontracts.ToolDefinition { return nil }

func (emptyCatalog) Get(toolcontracts.ToolName) (toolcontracts.Tool, bool) { return nil, false }

// recordingToolCalls 记录 loop 交给工具层的迭代归属；真实实现据此持久化 ToolInvocation。
type recordingToolCalls struct{ turnIDs []contracts.TurnID }

func (r *recordingToolCalls) Invoke(
	_ context.Context,
	_ toolcontracts.ToolCall,
	meta loop.ToolInvocationMetadata,
) (toolcontracts.ToolResult, error) {
	r.turnIDs = append(r.turnIDs, meta.TurnID)
	return toolcontracts.ToolResult{Payload: "文件内容"}, nil
}

// newRunner 构造已校验的 loop 执行器；默认使用顺序脚本模型、空工具目录和记录型工具调用，
// 用例只通过 apply 覆盖差异项。
func newRunner(t *testing.T, turns loop.TurnRecorder, apply ...func(*loop.Config)) *loop.Runner {
	t.Helper()
	config := loop.Config{
		ModelBuilder: scriptedModelBuilder{},
		Tools:        emptyCatalog{},
		ToolCalls:    &recordingToolCalls{},
		Turns:        turns,
	}
	for _, mutate := range apply {
		mutate(&config)
	}
	runner, err := loop.NewRunner(config)
	if err != nil {
		t.Fatal(err)
	}
	return &runner
}

// TestLoopRecordsOneTurnPerIteration 验证每次迭代各有一条 Turn，工具调用挂在产生它的迭代上。
func TestLoopRecordsOneTurnPerIteration(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	turns, err := taskturn.NewService(taskturn.Config{Transactions: f.store, Turns: f.repos.Turns})
	if err != nil {
		t.Fatal(err)
	}
	created, err := f.start.SendInput(ctx, start.SendInputParams{SessionID: "session", RequestID: "direct", Content: "开始"})
	if err != nil {
		t.Fatal(err)
	}
	store, err := f.messages.Resolve(ctx, created.Task.SessionID, created.Task.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	toolCalls := &recordingToolCalls{}
	outcome, code, err := newRunner(t, turns, func(config *loop.Config) { config.ToolCalls = toolCalls }).Run(ctx, created.Task, store)
	if err != nil || outcome != taskmodel.TaskCompleted || code != "" {
		t.Fatalf("脚本化 loop 应正常完成: outcome=%s code=%s err=%v", outcome, code, err)
	}
	firstID := turnID(t, created.Task.ID, 1)
	first, err := f.repos.Turns.Get(ctx, firstID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != turnmodel.TurnEnded || first.Outcome != turnmodel.TurnCompleted || first.Sequence != 1 {
		t.Fatalf("第一次迭代必须结算为完成: %+v", first)
	}
	if second, err := f.repos.Turns.Get(ctx, turnID(t, created.Task.ID, 2)); err != nil ||
		second.Outcome != turnmodel.TurnCompleted {
		t.Fatalf("每次迭代各有一条记录: %+v %v", second, err)
	}
	if len(toolCalls.turnIDs) != 1 || toolCalls.turnIDs[0] != firstID {
		t.Fatalf("工具调用必须归属于产生它的迭代: %v", toolCalls.turnIDs)
	}
	if open, err := f.repos.Turns.ListOpen(ctx); err != nil || len(open) != 0 {
		t.Fatalf("迭代必须全部结算: %+v %v", open, err)
	}
	// 同一迭代重复准入必须复用记录，不产生第二条日志事实。
	again, err := turns.RecordStart(ctx, loop.TurnParams{
		TaskID: created.Task.ID, SessionID: created.Task.SessionID,
		AgentID: created.Task.AgentID, Sequence: 1,
	})
	if err != nil || again.ID != firstID || again.Status != turnmodel.TurnEnded {
		t.Fatalf("重复准入必须幂等返回已结算迭代: %+v %v", again, err)
	}
}

// TestLoopSettlesTurnWhenProviderFails 验证 Provider 失败时迭代不会停留在 running。
func TestLoopSettlesTurnWhenProviderFails(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	turns, err := taskturn.NewService(taskturn.Config{Transactions: f.store, Turns: f.repos.Turns})
	if err != nil {
		t.Fatal(err)
	}
	created, err := f.start.SendInput(ctx, start.SendInputParams{SessionID: "session", RequestID: "direct", Content: "开始"})
	if err != nil {
		t.Fatal(err)
	}
	store, err := f.messages.Resolve(ctx, created.Task.SessionID, created.Task.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	outcome, code, err := newRunner(t, turns, func(config *loop.Config) { config.ModelBuilder = scriptedModelBuilder{fail: true} }).Run(ctx, created.Task, store)
	if err == nil || outcome != taskmodel.TaskFailed || code != contracts.TaskFailureProvider {
		t.Fatalf("Provider 失败必须终止 loop: outcome=%s code=%s err=%v", outcome, code, err)
	}
	failed, err := f.repos.Turns.Get(ctx, turnID(t, created.Task.ID, 1))
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != turnmodel.TurnEnded || failed.Outcome != turnmodel.TurnFailed ||
		failed.FailureCode != contracts.TaskFailureProvider || failed.FailureMessage == "" {
		t.Fatalf("失败迭代必须带失败码与详情: %+v", failed)
	}
	if open, err := f.repos.Turns.ListOpen(ctx); err != nil || len(open) != 0 {
		t.Fatalf("失败迭代不能停留在 running: %+v %v", open, err)
	}
}

func turnID(t *testing.T, taskID contracts.TaskID, sequence uint64) contracts.TurnID {
	t.Helper()
	id, err := turnmodel.NewTurnID(taskID, sequence)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// TestStrayTurnIsSettledBySessionRecovery 验证已结束 Task 下残留的 running 迭代在按 Session 恢复时被收敛。
func TestStrayTurnIsSettledBySessionRecovery(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	turns, err := taskturn.NewService(taskturn.Config{Transactions: f.store, Turns: f.repos.Turns})
	if err != nil {
		t.Fatal(err)
	}
	created, err := f.start.SendInput(ctx, start.SendInputParams{SessionID: "session", RequestID: "direct", Content: "开始"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.lifecycle.End(ctx, created.Task.ID, taskmodel.TaskCompleted, "", ""); err != nil {
		t.Fatal(err)
	}
	f.executions.End(created.Task.AgentID, created.Task.ID)
	// 模拟结算写入失败后遗留的 running 迭代：Task 已结束，迭代仍未结算。
	open, err := turns.RecordStart(ctx, loop.TurnParams{
		TaskID: created.Task.ID, SessionID: created.Task.SessionID,
		AgentID: created.Task.AgentID, Sequence: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.sessions.RecoverStaleTasks(ctx); err != nil {
		t.Fatal(err)
	}
	settled, err := f.repos.Turns.Get(ctx, open.ID)
	if err != nil {
		t.Fatal(err)
	}
	if settled.Status != turnmodel.TurnEnded || settled.Outcome != turnmodel.TurnInterrupted ||
		settled.FailureCode != contracts.TaskFailureInterrupted || settled.FailureMessage != "" {
		t.Fatalf("残留迭代必须收敛为中断: %+v", settled)
	}
	if remaining, err := f.repos.Turns.ListOpen(ctx); err != nil || len(remaining) != 0 {
		t.Fatalf("恢复后不应残留 running 迭代: %+v %v", remaining, err)
	}
	if err := f.sessions.RecoverStaleTasks(ctx); err != nil {
		t.Fatalf("重复恢复必须幂等: %v", err)
	}
}
