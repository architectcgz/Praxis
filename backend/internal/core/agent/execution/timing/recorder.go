package timing

import (
	"context"
	"errors"
	"praxis/internal/identity"
	"time"
)

// Recorder 只测量操作耗时；开始和结束快照由装配时注入的回调处理。
type Recorder struct {
	record func(Record)
}

func New(record func(Record)) *Recorder { return &Recorder{record: record} }

type spanContextKey struct{}
type parentOperation struct{ executionID, id string }

// Start 为每次尝试创建新 ID。Context 只传递同一 Execution 的父操作标识。
// nil Recorder 表示未启用计时；开始回调完成后才启动耗时测量，排除写入开销。
func (r *Recorder) Start(ctx context.Context, operation Operation) (context.Context, *Span) {
	if r == nil {
		return ctx, nil
	}
	parentID := ""
	if parent, ok := ctx.Value(spanContextKey{}).(parentOperation); ok && parent.executionID == operation.ExecutionID {
		parentID = parent.id
	}
	record := Record{
		Operation: operation,
		ID:        (identity.SecureIDGenerator{}).New("timing"),
		ParentID:  parentID,
		StartedAt: time.Now().UTC(),
		Status:    Running,
	}
	r.publish(record)
	span := &Span{recorder: r, record: record, started: time.Now()}
	return context.WithValue(ctx, spanContextKey{}, parentOperation{executionID: operation.ExecutionID, id: record.ID}), span
}

func (r *Recorder) publish(record Record) {
	// 快照拥有独立指针，回调不能修改 Span 内部状态。
	if record.DurationMS != nil {
		value := *record.DurationMS
		record.DurationMS = &value
	}
	if record.FirstResponseMS != nil {
		value := *record.FirstResponseMS
		record.FirstResponseMS = &value
	}
	if r.record != nil {
		r.record(record)
	}
}

// ResultStatus 将调用错误映射为计时状态，取消和截止时间优先于普通失败。
func ResultStatus(ctx context.Context, err error) Status {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return Cancelled
	}
	if err != nil {
		return Failed
	}
	return Completed
}
