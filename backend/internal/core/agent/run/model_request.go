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

// ModelRequestRecord 保存请求开始或收尾时已知的用量与状态；展示耗时由对应 Entry 持有。
type ModelRequestRecord struct {
	RunID       identity.RunID         `json:"executionId"`
	TurnID      identity.TurnID        `json:"turnId"`
	Kind        ModelRequestKind       `json:"kind"`
	Status      ModelRequestStatus     `json:"status"`
	StopReason  providerapi.StopReason `json:"stopReason,omitempty"`
	OutputBytes int                    `json:"outputBytes"`
	Usage       *providerapi.Usage     `json:"usage,omitempty"`
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
	if r.RunID == "" || r.TurnID == "" || r.OutputBytes < 0 {
		return errors.New("invalid model request identity or output size")
	}
	switch r.Kind {
	case ModelRequestIteration, ModelRequestSummary, ModelRequestRiskReview:
	default:
		return errors.New("unknown model request kind")
	}
	switch r.Status {
	case ModelRequestRunning, ModelRequestCompleted, ModelRequestFailed, ModelRequestCancelled:
	default:
		return errors.New("unknown model request status")
	}
	if r.Usage != nil && !r.Usage.Valid() {
		return errors.New("invalid model request usage")
	}
	return nil
}
