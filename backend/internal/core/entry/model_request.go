package entry

import (
	"context"
	"praxis/internal/core/entry/record"
)

type (
	ModelRequestKind   = record.ModelRequestKind
	ModelRequestStatus = record.ModelRequestStatus
	ModelRequestRecord = record.ModelRequestRecord
)

const (
	ModelRequestIteration  = record.ModelRequestIteration
	ModelRequestSummary    = record.ModelRequestSummary
	ModelRequestRiskReview = record.ModelRequestRiskReview
	RequestRunning         = record.RequestRunning
	RequestCompleted       = record.RequestCompleted
	RequestFailed          = record.RequestFailed
	RequestInterrupted     = record.RequestInterrupted
)

func RequestStatus(ctx context.Context, err error) ModelRequestStatus {
	if err == nil {
		return RequestCompleted
	}
	if ctx.Err() != nil {
		return RequestInterrupted
	}
	return RequestFailed
}
