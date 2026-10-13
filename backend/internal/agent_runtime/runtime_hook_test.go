package agentruntime

import (
	"context"
	"errors"
	"testing"

	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/hooks"
	"praxis/internal/logging"
)

type runtimeHookKey struct{}

type runtimeHook struct {
	operation hooks.Operation
	result    hooks.Result
	ends      int
}

func (h *runtimeHook) Start(ctx context.Context, op hooks.Operation) (context.Context, hooks.Scope) {
	h.operation = op
	return context.WithValue(ctx, runtimeHookKey{}, true), h
}
func (h *runtimeHook) OnSignal(hooks.Signal) {}
func (h *runtimeHook) End(result hooks.Result) {
	h.result = result
	h.ends++
}

func TestExecuteHookObservesNormalizedOutcome(t *testing.T) {
	loopErr := errors.New("模型失败")
	for _, test := range []struct {
		name    string
		outcome taskmodel.TaskOutcome
		err     error
		cause   error
		want    taskmodel.TaskOutcome
	}{
		{name: "完成", outcome: taskmodel.TaskCompleted, want: taskmodel.TaskCompleted},
		{
			name:    "错误",
			outcome: taskmodel.TaskCompleted,
			err:     loopErr,
			want:    taskmodel.TaskFailed,
		},
		{
			name:    "取消",
			outcome: taskmodel.TaskCompleted,
			cause:   context.Canceled,
			want:    taskmodel.TaskInterrupted,
		},
		{
			name:    "暂停",
			outcome: taskmodel.TaskCompleted,
			err:     context.Canceled,
			cause:   agentmodel.ErrExecutionPaused,
			want:    taskmodel.TaskPaused,
		},
		{name: "非法结果", outcome: "unknown", want: taskmodel.TaskFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			hook := &runtimeHook{}
			r := &Runtime{
				logger:         logging.NewFactory().Nop(),
				hookDispatcher: hooks.NewDispatcher(hook),
				runLoop: func(runCtx context.Context, _ taskmodel.Task, _ MessageRecorder) (taskmodel.TaskOutcome, contracts.TaskFailureCode, error) {
					if runCtx.Value(runtimeHookKey{}) != true {
						t.Fatal("Agent Hook 的 Context 未传给 Loop")
					}
					if test.cause != nil {
						cancel(test.cause)
					}
					return test.outcome, "", test.err
				},
			}
			task := taskmodel.Task{
				ID:        "task",
				SessionID: "session",
				AgentID:   "agent",
				Input:     taskmodel.InputSnapshot{AgentDefinitionID: "primary"},
			}
			outcome, code, _ := r.execute(ctx, task, nil)
			if outcome != test.want || hook.result.Outcome != outcome || hook.result.Err != test.err || hook.ends != 1 {
				t.Fatalf("Hook 改变了执行结果或观察到未仲裁结果：%s %s %+v", outcome, code, hook.result)
			}
			if test.cause != nil && code != contracts.TaskFailureRuntimeCancelled {
				t.Fatalf("取消分类丢失：%s", code)
			}
			if hook.operation.Kind != hooks.OperationAgent || hook.operation.SessionID != task.SessionID || hook.operation.AgentID != task.AgentID || hook.operation.TaskID != task.ID || hook.operation.Name != "primary" {
				t.Fatalf("Agent 归属不完整：%+v", hook.operation)
			}
		})
	}
}
