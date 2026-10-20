package execution

import (
	"context"
	"errors"
	"praxis/internal/core/hook"
)

// ProgressHook 在模型边界调用 Execution 用例，序号和计数在同一事务中提交。
// Hook 不持有某次执行的状态，可由共享 Runner 并发使用。
type ProgressHook struct {
	Service *Service
}

func (h ProgressHook) BeforeRequest(ctx context.Context, request hook.ModelRequest) error {
	switch request.Kind {
	case hook.ModelRequestIteration:
		return h.Service.recordRequest(ctx, request.ExecutionID, request.Sequence, true)
	case hook.ModelRequestSummary:
		return h.Service.recordRequest(ctx, request.ExecutionID, request.Sequence, false)
	default:
		return errors.New("unsupported model request kind")
	}
}

func (h ProgressHook) AfterResponse(ctx context.Context, response hook.ModelResponse) error {
	return h.Service.recordModelOutput(ctx, response.Request.ExecutionID, response.OutputBytes)
}

var _ hook.ModelHook = ProgressHook{}
