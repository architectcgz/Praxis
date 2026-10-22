package hook

import (
	"context"
	providerapi "praxis/internal/core/provider"
)

// Iteration 对应一次正常模型响应及其工具调用，不包含摘要请求。
type Iteration struct {
	Identity
	Sequence uint64
}

type IterationResult struct {
	Iteration
	StopReason providerapi.StopReason
	Err        error
}

type BeforeIterationHook interface {
	BeforeIteration(context.Context, Iteration) error
}

type AfterIterationHook interface {
	AfterIteration(context.Context, IterationResult) error
}
