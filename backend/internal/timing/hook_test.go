package timing

import (
	"context"
	"errors"
	"testing"

	"praxis/internal/core/model"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/hooks"
)

type hookStore struct {
	Store
	records []Record
}

func (s *hookStore) Save(_ context.Context, record Record) error {
	s.records = append(s.records, record)
	return record.Validate()
}

func TestHookResultStatus(t *testing.T) {
	for _, test := range []struct {
		name      string
		kind      hooks.OperationKind
		result    hooks.Result
		cancelled bool
		want      Status
	}{
		{
			name:   "Agent完成",
			kind:   hooks.OperationAgent,
			result: hooks.Result{Outcome: taskmodel.TaskCompleted},
			want:   Completed,
		},
		{
			name:   "Agent让出",
			kind:   hooks.OperationAgent,
			result: hooks.Result{Outcome: taskmodel.TaskYielded},
			want:   Completed,
		},
		{
			name:   "Agent失败",
			kind:   hooks.OperationAgent,
			result: hooks.Result{Outcome: taskmodel.TaskFailed},
			want:   Failed,
		},
		{
			name:   "Agent暂停",
			kind:   hooks.OperationAgent,
			result: hooks.Result{Outcome: taskmodel.TaskPaused},
			want:   Cancelled,
		},
		{
			name:   "Agent中断",
			kind:   hooks.OperationAgent,
			result: hooks.Result{Outcome: taskmodel.TaskInterrupted},
			want:   Cancelled,
		},
		{
			name:      "Agent取消",
			kind:      hooks.OperationAgent,
			result:    hooks.Result{Outcome: taskmodel.TaskFailed},
			cancelled: true,
			want:      Cancelled,
		},
		{
			name:   "Provider完成",
			kind:   hooks.OperationProvider,
			result: hooks.Result{StopReason: model.StopCompleted},
			want:   Completed,
		},
		{
			name:   "Provider工具",
			kind:   hooks.OperationProvider,
			result: hooks.Result{StopReason: model.StopToolCalls},
			want:   Completed,
		},
		{
			name:   "Provider停止序列",
			kind:   hooks.OperationProvider,
			result: hooks.Result{StopReason: model.StopSequence},
			want:   Failed,
		},
		{
			name:   "Provider截断",
			kind:   hooks.OperationProvider,
			result: hooks.Result{StopReason: model.StopTruncated},
			want:   Failed,
		},
		{
			name:   "Provider校验失败",
			kind:   hooks.OperationProvider,
			result: hooks.Result{StopReason: model.StopCompleted, Err: errors.New("协议不匹配")},
			want:   Failed,
		},
		{
			name:   "Provider流错误",
			kind:   hooks.OperationProvider,
			result: hooks.Result{Err: errors.New("流错误")},
			want:   Failed,
		},
		{
			name:      "Provider取消",
			kind:      hooks.OperationProvider,
			cancelled: true,
			want:      Cancelled,
		},
		{name: "Tool完成", kind: hooks.OperationTool, want: Completed},
		{
			name:   "Tool错误分类",
			kind:   hooks.OperationTool,
			result: hooks.Result{ToolErrorClass: "denied"},
			want:   Failed,
		},
		{
			name:   "Tool错误",
			kind:   hooks.OperationTool,
			result: hooks.Result{Err: errors.New("调用失败")},
			want:   Failed,
		},
		{
			name:   "Tool取消错误",
			kind:   hooks.OperationTool,
			result: hooks.Result{Err: context.Canceled},
			want:   Cancelled,
		},
		{
			name:      "Tool取消优先",
			kind:      hooks.OperationTool,
			result:    hooks.Result{ToolErrorClass: "denied"},
			cancelled: true,
			want:      Cancelled,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &hookStore{}
			recorder, err := New(store, nil, t.Errorf)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			_, scope := (Hook{Recorder: recorder}).Start(ctx, hooks.Operation{
				Identity: hooks.Identity{SessionID: "session", AgentID: "agent", TaskID: "task"},
				Kind:     test.kind,
				Name:     "operation",
			})
			if test.cancelled {
				cancel()
			}
			scope.End(test.result)
			scope.End(hooks.Result{})
			if len(store.records) != 2 || store.records[1].Status != test.want {
				t.Fatalf("计时结算不正确或重复保存：%+v", store.records)
			}
		})
	}
}

func TestHookParentAndFirstResponse(t *testing.T) {
	store := &hookStore{}
	recorder, err := New(store, nil, t.Errorf)
	if err != nil {
		t.Fatal(err)
	}
	d := hooks.NewDispatcher(Hook{Recorder: recorder})
	identity := hooks.Identity{SessionID: "session", AgentID: "agent", TaskID: "task"}
	ctx, agent := d.Start(t.Context(), hooks.Operation{Identity: identity, Kind: hooks.OperationAgent, Name: "primary"})
	_, provider := d.Start(ctx, hooks.Operation{
		Identity:    identity,
		Kind:        hooks.OperationProvider,
		Name:        "provider/model",
		ReferenceID: "assistant:turn",
	})
	provider.OnSignal("ignored")
	provider.OnSignal(hooks.SignalFirstResponse)
	provider.OnSignal(hooks.SignalFirstResponse)
	provider.End(hooks.Result{StopReason: model.StopCompleted})
	provider.OnSignal(hooks.SignalFirstResponse)
	agent.End(hooks.Result{Outcome: taskmodel.TaskCompleted})
	if len(store.records) != 5 {
		t.Fatalf("首响应未去重：%d", len(store.records))
	}
	child := store.records[3]
	if child.ParentID != store.records[0].ID || child.FirstResponseMS == nil || *child.FirstResponseMS > *child.DurationMS || child.ReferenceID != "assistant:turn" {
		t.Fatalf("父子关联或首响应错误：%+v", child)
	}
}

func TestHookIgnoresOtherOperations(t *testing.T) {
	store := &hookStore{}
	recorder, err := New(store, nil, t.Errorf)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []hooks.OperationKind{hooks.OperationSessionCreate, hooks.OperationTaskSettle, "other"} {
		ctx, scope := (Hook{Recorder: recorder}).Start(t.Context(), hooks.Operation{Kind: kind, Name: "operation"})
		if ctx != t.Context() || scope != nil || len(store.records) != 0 {
			t.Fatalf("计时 Hook 不应处理其他生命周期操作：%s", kind)
		}
	}
}

func TestHookWithoutRecorder(t *testing.T) {
	ctx, scope := (Hook{}).Start(t.Context(), hooks.Operation{})
	if ctx != t.Context() || scope != nil {
		t.Fatal("nil Recorder 应保持 Context 且忽略操作")
	}
}
