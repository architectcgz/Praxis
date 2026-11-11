package run

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
	ModelRequestRunning   ModelRequestStatus = "running"
	ModelRequestCompleted ModelRequestStatus = "completed"
	ModelRequestFailed    ModelRequestStatus = "failed"
	ModelRequestCancelled ModelRequestStatus = "cancelled"
)

// ModelRequestRecord 保存一次实际模型请求的状态与用量，普通请求、摘要和审核各有独立 Turn。
type ModelRequestRecord struct {
	ID          identity.ModelRequestID `json:"id"`
	RunID       identity.RunID          `json:"executionId"`
	TurnID      identity.TurnID         `json:"turnId,omitempty"`
	CallID      string                  `json:"callId,omitempty"`
	Kind        ModelRequestKind        `json:"kind"`
	Status      ModelRequestStatus      `json:"status"`
	StopReason  providerapi.StopReason  `json:"stopReason,omitempty"`
	OutputBytes int                     `json:"outputBytes"`
	Usage       *providerapi.Usage      `json:"usage,omitempty"`
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
	if r.ID == "" || r.RunID == "" || r.TurnID == "" || r.OutputBytes < 0 {
		return errors.New("invalid model request identity or output size")
	}
	switch r.Kind {
	case ModelRequestIteration:
		if r.TurnID == "" || r.CallID != "" {
			return errors.New("iteration request requires a turn and no review call")
		}
	case ModelRequestSummary, ModelRequestRiskReview:
	default:
		return errors.New("unknown model request kind")
	}
	switch r.Status {
	case ModelRequestRunning, ModelRequestCompleted, ModelRequestFailed, ModelRequestCancelled:
	default:
		return errors.New("unknown model request status")
	}
	return nil
}
