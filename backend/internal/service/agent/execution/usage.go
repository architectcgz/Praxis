package execution

import (
	"context"
	agentexecution "praxis/internal/core/agent/execution"
	coreevent "praxis/internal/core/event"
	"time"
)

// RecordModelResponse 在一次收尾中接纳请求明细、输出进度和累计值，保存失败仍保留内存事实。
func (s *Service) RecordModelResponse(ctx context.Context, run *agentexecution.Run, request agentexecution.ModelRequestRecord) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	var writeErr error
	if err := run.Update(func(record *agentexecution.ExecutionRecord) error {
		record.Progress.ModelOutputBytes += int64(request.OutputBytes)
		if request.Usage != nil {
			record.Progress.Usage.Add(*request.Usage)
		}
		writeErr = s.records.SaveModelRequest(ctx, *record, request)
		return nil
	}); err != nil {
		return err
	}
	if request.Usage != nil {
		s.emitter.Emit(coreevent.Event{
			Source:  coreevent.SourceForRun(run, request.TurnID),
			Payload: coreevent.ModelUsage{Usage: request.Usage.Clone()},
		})
	}
	if writeErr != nil {
		return storageFailure(writeErr)
	}
	return nil
}
