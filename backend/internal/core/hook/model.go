package hook

import (
	"context"
	"errors"
	"slices"
)

type ModelRequestKind string

const (
	ModelRequestIteration ModelRequestKind = "iteration"
	ModelRequestSummary   ModelRequestKind = "summary"
)

// ModelRequest 描述即将发起的请求，不包含模型输入或响应正文。
type ModelRequest struct {
	Identity
	Kind     ModelRequestKind
	Sequence uint64
}

// ModelResponse 包含实际收到的输出量；请求失败时也保留已消费的字节数。
type ModelResponse struct {
	Request     ModelRequest
	OutputBytes int
}

// ModelHook 同步记录模型请求与输出；错误必须返回调用方，不作为 best-effort 通知。
// 回调在业务事务和互斥锁之外执行，具体业务操作负责事务，不承担授权或恢复编排。
type ModelHook interface {
	BeforeRequest(context.Context, ModelRequest) error
	AfterResponse(context.Context, ModelResponse) error
}

// WithModelHooks 返回使用指定模型 Hook 的独立分发器，保留操作与提交后 Hook。
// 模型记录绑定本次应用的业务服务，替换注册列表，不改写外部共享分发器。
func (d *Dispatcher) WithModelHooks(models ...ModelHook) *Dispatcher {
	next := &Dispatcher{models: slices.Clone(models)}
	if d != nil {
		next.operations = slices.Clone(d.operations)
		next.events = slices.Clone(d.events)
	}
	return next
}

func (d *Dispatcher) HasModelHooks() bool { return d != nil && len(d.models) > 0 }

// BeforeModelRequest 在模型调用前按顺序执行；任一记录失败即停止请求。
func (d *Dispatcher) BeforeModelRequest(ctx context.Context, request ModelRequest) error {
	if !d.HasModelHooks() {
		return errors.New("model request hooks are required")
	}
	for _, model := range d.models {
		if err := model.BeforeRequest(ctx, request); err != nil {
			return err
		}
	}
	return nil
}

// AfterModelResponse 尝试全部输出记录并汇总错误，调用方据此停止后续模型和工具操作。
func (d *Dispatcher) AfterModelResponse(ctx context.Context, response ModelResponse) error {
	if !d.HasModelHooks() {
		return errors.New("model request hooks are required")
	}
	var errs []error
	for _, model := range d.models {
		if err := model.AfterResponse(ctx, response); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
