package agent_test

import (
	"context"
	"errors"
	"testing"

	"praxis/internal/contracts"
	"praxis/internal/core/agent"
)

func TestExecutionsBindAndIsolateTaskControl(t *testing.T) {
	var executions agent.Executions
	request, cancel := context.WithCancel(t.Context())
	ctx, created, err := executions.Start(request, "agent", "first")
	if err != nil || !created {
		t.Fatalf("建立执行绑定失败: %v", err)
	}
	cancel()
	if ctx.Err() != nil {
		t.Fatal("请求结束不能取消执行")
	}
	if same, created, err := executions.Start(t.Context(), "agent", "first"); err != nil || created || same != ctx {
		t.Fatalf("重复准入必须复用绑定: %v", err)
	}
	if _, _, err := executions.Start(t.Context(), "agent", "second"); !errors.Is(err, contracts.ErrAgentExecuting) {
		t.Fatalf("未返回的执行不能被替换: %v", err)
	}
	if executions.Stop("agent", "future") || executions.Stop("other", "first") || ctx.Err() != nil {
		t.Fatal("未知 Task 或 Agent 不得取消执行或暂存意图")
	}
	if !executions.Pause("agent", "first") || !errors.Is(context.Cause(ctx), agent.ErrExecutionPaused) {
		t.Fatal("暂停必须取消已绑定的 Context")
	}
	executions.BeginEnding("agent", "first")
	if executions.Stop("agent", "first") {
		t.Fatal("loop 返回后不再向执行发信号")
	}
	next, _, err := executions.Start(t.Context(), "agent", "future")
	if err != nil {
		t.Fatal(err)
	}
	executions.End("agent", "first")
	if bound, err := executions.Context("agent", "future"); err != nil || bound != next || next.Err() != nil {
		t.Fatalf("旧执行清理不能删除或取消下一项绑定: %v", err)
	}
	executions.End("agent", "future")
	if _, err := executions.Context("agent", "future"); !errors.Is(err, contracts.ErrNotFound) || next.Err() == nil {
		t.Fatalf("执行结束必须释放绑定: %v", err)
	}
}
