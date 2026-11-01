package execution

import (
	"context"
	"errors"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/identity"
)

type ModelRequestKind string

const (
	ModelRequestIteration  ModelRequestKind = "iteration"
	ModelRequestSummary    ModelRequestKind = "summary"
	ModelRequestRiskReview ModelRequestKind = "risk_review"
)

type ModelRequestStatus string

const (
	ModelRequestCompleted ModelRequestStatus = "completed"
	ModelRequestFailed    ModelRequestStatus = "failed"
	ModelRequestCancelled ModelRequestStatus = "cancelled"
)

// ModelRequestRecord 保存一次请求收尾时已知的事实；Usage 为空表示未收到有效统计。
type ModelRequestRecord struct {
	ExecutionID identity.AgentExecutionID `json:"executionId"`
	TurnID      identity.TurnID           `json:"turnId"`
	Kind        ModelRequestKind          `json:"kind"`
	Status      ModelRequestStatus        `json:"status"`
	StopReason  providerapi.StopReason    `json:"stopReason,omitempty"`
	OutputBytes int                       `json:"outputBytes"`
	Usage       *providerapi.Usage        `json:"usage,omitempty"`
}

func (r ModelRequestRecord) Clone() ModelRequestRecord {
	if r.Usage != nil {
		r.Usage = new(r.Usage.Clone())
	}
	return r
}

func RequestStatus(ctx context.Context, err error) ModelRequestStatus {
	if err == nil {
		return ModelRequestCompleted
	}
	if ctx.Err() != nil {
		return ModelRequestCancelled
	}
	return ModelRequestFailed
}

// Validate 仅在日志读取边界校验请求事实，运行流程使用已确定的内部值。
func (r ModelRequestRecord) Validate() error {
	if r.ExecutionID == "" || r.TurnID == "" || r.OutputBytes < 0 {
		return errors.New("invalid model request identity or output size")
	}
	switch r.Kind {
	case ModelRequestIteration, ModelRequestSummary, ModelRequestRiskReview:
	default:
		return errors.New("unknown model request kind")
	}
	switch r.Status {
	case ModelRequestCompleted, ModelRequestFailed, ModelRequestCancelled:
	default:
		return errors.New("unknown model request status")
	}
	if r.Usage != nil && !r.Usage.Valid() {
		return errors.New("invalid model request usage")
	}
	return nil
}
