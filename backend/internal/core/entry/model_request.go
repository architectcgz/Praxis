package entry

import (
	"context"
	"errors"
	modelapi "praxis/internal/core/model"
	"praxis/internal/core/timing"
	"praxis/internal/identity"
	"strings"
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

// ModelRequest 保存一次实际模型请求的状态与用量，普通请求及审核关联所在 Turn，摘要仅关联 Run。
type ModelRequest struct {
	ID          identity.ModelRequestID `json:"id"`
	RunID       identity.RunID          `json:"runId"`
	TurnID      identity.TurnID         `json:"turnId,omitempty"`
	CallID      string                  `json:"callId,omitempty"`
	Kind        ModelRequestKind        `json:"requestKind"`
	Status      ModelRequestStatus      `json:"status"`
	StopReason  string                  `json:"stopReason,omitempty"`
	RetryCount  int                     `json:"retryCount"`
	Error       string                  `json:"error,omitempty"`
	Partial     Content                 `json:"partial,omitempty"`
	OutputBytes int                     `json:"outputBytes"`
	Usage       *modelapi.Usage         `json:"usage,omitempty"`
	Timing      *timing.Record          `json:"timing,omitempty"`
}

func (ModelRequest) kind() string { return kindModelRequest }

// PartialText 只提取不完整响应中的正文，不向观察者传递内部内容块。
func (r ModelRequest) PartialText() string {
	var text strings.Builder
	for _, block := range r.Partial {
		if part, ok := block.(TextBlock); ok {
			text.WriteString(part.Text)
		}
	}
	return text.String()
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
func (r ModelRequest) Validate() error {
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
