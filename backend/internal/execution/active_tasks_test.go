package execution

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"praxis/internal/contracts"
)

func TestReservationAndRunningOwnership(t *testing.T) {
	slots := &ActiveTasks{}
	requestCtx, cancelRequest := context.WithCancel(t.Context())
	ctx, created, err := slots.Reserve(requestCtx, "agent", "first")
	if err != nil || !created {
		t.Fatalf("预留失败: %v %v", created, err)
	}
	cancelRequest()
	if ctx.Err() != nil {
		t.Fatal("执行继承了请求的取消")
	}
	var claimed atomic.Int32
	var attempts sync.WaitGroup
	for range 30 {
		attempts.Go(func() {
			if _, ok, err := slots.TryRun("agent", "first"); err != nil {
				t.Error(err)
			} else if ok {
				claimed.Add(1)
			}
		})
	}
	attempts.Wait()
	if claimed.Load() != 1 {
		t.Fatalf("领取运行权 %d 次", claimed.Load())
	}
	slots.BeginEnding("agent", "first")
	if _, _, err := slots.Reserve(t.Context(), "agent", "second"); !errors.Is(err, contracts.ErrAgentExecuting) {
		t.Fatalf("结算途中身份被替换: %v", err)
	}
	slots.Release("agent", "first")
	next, _, err := slots.Reserve(t.Context(), "agent", "second")
	if err != nil {
		t.Fatal(err)
	}
	slots.Release("agent", "first")
	if next.Err() != nil || !slots.occupied("agent") {
		t.Fatal("旧执行清理影响了新执行")
	}
	slots.Pause("agent", "second")
	slots.Stop("agent", "second")
	if !errors.Is(context.Cause(next), ErrExecutionPaused) {
		t.Fatal("未保留首次取消原因")
	}
}
