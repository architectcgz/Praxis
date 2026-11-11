package run

import (
	"context"
	agentrun "praxis/internal/core/agent/run"
	"praxis/internal/core/agent/run/timing"
	coreevent "praxis/internal/core/event"
	"time"
)

// RecordModelResponse 在一次收尾中接纳请求明细、输出进度和累计值，保存失败仍保留内存事实。
func (s *Service) RecordModelResponse(ctx context.Context, run *agentrun.Run, request agentrun.ModelRequestRecord, measured *timing.Record) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	var owner agentrun.RunRecord
	if err := run.Update(func(record *agentrun.RunRecord) error {
		record.Progress.ModelOutputBytes += int64(request.OutputBytes)
		record.Progress.OutputBytes += int64(request.OutputBytes)
		if request.Usage != nil {
			usage := request.Usage
			record.Progress.Usage.Add(usage.InputTokens, usage.OutputTokens, usage.CacheReadInputTokens, usage.CacheCreationInputTokens)
		}
		owner = *record
		return nil
	}); err != nil {
		return err
	}
	writeErr := s.records.SaveModelRequest(ctx, owner, request)
	s.emitter.Emit(coreevent.Event{
		Source: coreevent.SourceForRun(run, request.TurnID),
		Payload: coreevent.ModelRequestFinished{
			Request: request,
			Timing:  measured,
		},
	})
	if writeErr != nil {
		return storageFailure(writeErr)
	}
	return nil
}
