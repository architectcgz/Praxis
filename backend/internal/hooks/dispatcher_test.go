package hooks

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type operationFunc func(context.Context, Operation) (context.Context, Scope)

func (f operationFunc) Start(ctx context.Context, op Operation) (context.Context, Scope) {
	return f(ctx, op)
}

type testScope struct {
	signal func(Signal)
	end    func(Result)
}

func (s testScope) OnSignal(signal Signal) { s.signal(signal) }
func (s testScope) End(result Result)      { s.end(result) }

func TestDispatcherLifecycle(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(t.Context(), contextKey{}, "root")
	var calls []string
	operation := Operation{Kind: OperationProvider, Name: "model"}
	result := Result{Err: errors.New("执行失败")}
	operations := make([]OperationHook, 0, 3)
	for _, name := range []string{"a", "b", "c"} {
		operations = append(operations, operationFunc(func(ctx context.Context, op Operation) (context.Context, Scope) {
			if op != operation {
				t.Fatal("操作被改写")
			}
			previous := ctx.Value(contextKey{}).(string)
			calls = append(calls, "start:"+name+":"+previous)
			next := context.WithValue(ctx, contextKey{}, previous+name)
			if name == "b" {
				return next, nil
			}
			return next, testScope{
				signal: func(signal Signal) { calls = append(calls, "signal:"+name+":"+string(signal)) },
				end: func(got Result) {
					if got != result {
						t.Fatal("结果被改写")
					}
					calls = append(calls, "end:"+name)
				},
			}
		}))
	}
	d := NewDispatcher(operations...)
	// 外部修改注册切片不能改变分发器。
	operations[0] = nil
	next, scope := d.Start(ctx, operation)
	if next.Value(contextKey{}) != "rootabc" {
		t.Fatal("Context 未按顺序传播")
	}
	scope.OnSignal(SignalFirstResponse)
	scope.End(result)
	want := []string{
		"start:a:root",
		"start:b:roota",
		"start:c:rootab",
		"signal:a:first_response",
		"signal:c:first_response",
		"end:c",
		"end:a",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("生命周期顺序错误：%v", calls)
	}
}

func TestDispatcherWithoutHooks(t *testing.T) {
	for _, d := range []*Dispatcher{nil, NewDispatcher()} {
		ctx, scope := d.Start(t.Context(), Operation{})
		if ctx != t.Context() || scope == nil {
			t.Fatal("空分发器改变了 Context 或未返回空 Scope")
		}
		scope.OnSignal(SignalFirstResponse)
		scope.End(Result{})
	}
}
