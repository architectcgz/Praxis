package entry

import (
	"context"
	"encoding/json"
	"errors"
	modelusage "praxis/internal/core/model/usage"
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
	RequestRunning     ModelRequestStatus = "running"
	RequestCompleted   ModelRequestStatus = "completed"
	RequestFailed      ModelRequestStatus = "failed"
	RequestInterrupted ModelRequestStatus = "interrupted"
)

// ModelRequestRecord 保存一次实际模型请求的状态与用量，普通请求及审核关联所在 Turn，摘要仅关联 Run。
type ModelRequestRecord struct {
	ID          identity.ModelRequestID `json:"id"`
	RunID       identity.RunID          `json:"runId"`
	TurnID      identity.TurnID         `json:"turnId,omitempty"`
	CallID      string                  `json:"callId,omitempty"`
	Kind        ModelRequestKind        `json:"kind"`
	Status      ModelRequestStatus      `json:"status"`
	StopReason  string                  `json:"stopReason,omitempty"`
	RetryCount  int                     `json:"retryCount"`
	Error       string                  `json:"error,omitempty"`
	Partial     []ContentBlock          `json:"partial,omitempty"`
	OutputBytes int                     `json:"outputBytes"`
	Usage       *modelusage.Usage       `json:"usage,omitempty"`
}

func RequestStatus(ctx context.Context, err error) ModelRequestStatus {
	if err == nil {
		return RequestCompleted
	}
	if ctx.Err() != nil {
		return RequestInterrupted
	}
	return RequestFailed
}

// Validate 仅在日志读取边界校验请求事实，运行流程使用已确定的内部值。
func (r ModelRequestRecord) Validate() error {
	if r.ID == "" || r.RunID == "" || r.OutputBytes < 0 || r.RetryCount < 0 || r.RetryCount > 3 {
		return errors.New("invalid model request identity or output size")
	}
	switch r.Kind {
	case ModelRequestIteration:
		if r.TurnID == "" || r.CallID != "" {
			return errors.New("iteration request requires a turn and no review call")
		}
	case ModelRequestSummary:
		if r.TurnID != "" || r.CallID != "" {
			return errors.New("summary requests belong to a run outside turns")
		}
	case ModelRequestRiskReview:
		if r.TurnID == "" || r.CallID == "" {
			return errors.New("risk review requests require their triggering turn and call")
		}

	default:
		return errors.New("unknown model request kind")
	}
	switch r.Status {
	case RequestRunning, RequestCompleted, RequestFailed, RequestInterrupted:
	default:
		return errors.New("unknown model request status")
	}
	return nil
}

func (r *ModelRequestRecord) UnmarshalJSON(data []byte) error {
	type value ModelRequestRecord
	decoded := struct {
		*value
		RawPartial []json.RawMessage `json:"partial"`
	}{value: (*value)(r)}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	r.Partial = make([]ContentBlock, len(decoded.RawPartial))
	for i, raw := range decoded.RawPartial {
		block, err := decodeContentBlock(raw)
		if err != nil {
			return err
		}
		r.Partial[i] = block
	}
	return nil
}
