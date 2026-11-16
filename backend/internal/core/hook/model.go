package hook

import (
	"context"
	sessionentry "praxis/internal/core/entry"
	providerapi "praxis/internal/core/provider"
)

type ModelRequestKind = sessionentry.ModelRequestKind

const (
	ModelRequestIteration = sessionentry.ModelRequestIteration
	ModelRequestSummary   = sessionentry.ModelRequestSummary
)

// ModelRequest 携带本次请求的只读快照，处理器不返回修改后的请求。
type ModelRequest struct {
	Identity
	Kind    ModelRequestKind
	Request providerapi.Request
}

// ModelResponse 在流消费结束后通知，包括断流和取消时已经收到的输出量。
type ModelResponse struct {
	Request     ModelRequest
	OutputBytes int
	StopReason  providerapi.StopReason
	Err         error
}

type BeforeModelRequestHook interface {
	BeforeModelRequest(context.Context, ModelRequest) error
}

type AfterModelResponseHook interface {
	AfterModelResponse(context.Context, ModelResponse) error
}
