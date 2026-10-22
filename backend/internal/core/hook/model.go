package hook

import (
	"context"
	providerapi "praxis/internal/core/provider"
)

type ModelRequestKind string

const (
	ModelRequestIteration ModelRequestKind = "iteration"
	ModelRequestSummary   ModelRequestKind = "summary"
)

// ModelRequest 携带本次请求的只读归属；处理器只返回待发送的请求，不返回执行进度。
type ModelRequest struct {
	Identity
	Kind     ModelRequestKind
	Sequence uint64
	Request  providerapi.Request
}

func (e ModelRequest) clone() ModelRequest {
	e.Request = e.Request.Clone()
	return e
}

// ModelResponse 在流消费结束后通知，包括断流和取消时已经收到的输出量。
type ModelResponse struct {
	Request     ModelRequest
	OutputBytes int
	StopReason  providerapi.StopReason
	Err         error
}

func (e ModelResponse) clone() ModelResponse {
	e.Request = e.Request.clone()
	return e
}

type BeforeModelRequestHook interface {
	BeforeModelRequest(context.Context, ModelRequest) (providerapi.Request, error)
}

type AfterModelResponseHook interface {
	AfterModelResponse(context.Context, ModelResponse) error
}
