package run

import (
	"context"
	sessionentry "praxis/internal/core/entry"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/timing"
)

// 审核请求复用唯一事实提交入口；调用者只提供本次尝试的事实。
func (r *Run) RecordModelRequest(ctx context.Context, request sessionentry.ModelRequestRecord, measured *timing.Record) error {
	_, _, err := r.AppendEntry(ctx, sessionentry.ModelRequestEntry(request, measured))
	if err == nil {
		r.deps.Emitter.Emit(coreevent.Event{Source: r.EventSource(request.TurnID), Payload: coreevent.ModelRequestStarted{Request: request, Timing: measured}})
	}
	return err
}
func (r *Run) RecordModelResponse(ctx context.Context, request sessionentry.ModelRequestRecord, measured *timing.Record) error {
	stored, _, err := r.AppendEntry(ctx, sessionentry.ModelRequestEntry(request, measured))
	if err != nil {
		return err
	}
	if request.Status != sessionentry.RequestCompleted {
		r.deps.Emitter.Emit(coreevent.Event{Source: r.EventSource(request.TurnID), Payload: coreevent.EntriesAppended{Entries: []sessionentry.ContextEntry{*stored}}})
	}
	r.deps.Emitter.Emit(coreevent.Event{Source: r.EventSource(request.TurnID), Payload: coreevent.RequestResult(request, measured)})
	return err
}
