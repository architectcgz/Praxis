package compose

import (
	"context"
	"errors"

	"praxis/internal/agent_runtime"
	"praxis/internal/contracts"
	"praxis/internal/core/model"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/timing"
	toolcontracts "praxis/internal/tools/contracts"
)

// 计时包装器只存在于组合边界；业务执行器与 Provider 协议实现不承担计时职责。
// timedLoop 记录任意 Agent 的完整 loop 耗时，不改变执行结果与取消语义。
func timedLoop(next agentruntime.LoopFunc, recorder *timing.Recorder) agentruntime.LoopFunc {
	return func(ctx context.Context, task taskmodel.Task, messageRecorder agentruntime.MessageRecorder) (outcome taskmodel.TaskOutcome, code contracts.TaskFailureCode, err error) {
		ctx, span := recorder.Start(ctx, timing.Operation{
			SessionID: task.SessionID.String(),
			AgentID:   task.AgentID.String(),
			TaskID:    task.ID.String(),
			Kind:      timing.Agent,
			Name:      task.Input.AgentDefinitionID.String(),
		})
		status := timing.Failed
		defer func() { span.End(status) }()
		outcome, code, err = next(ctx, task, messageRecorder)
		status = timing.ResultStatus(ctx, err)
		if outcome == taskmodel.TaskFailed {
			status = timing.Failed
		} else if outcome == taskmodel.TaskInterrupted || outcome == taskmodel.TaskPaused {
			status = timing.Cancelled
		}
		if ctx != nil && ctx.Err() != nil {
			status = timing.Cancelled
		}
		return
	}
}

type timedModelBuilder struct {
	next     model.ModelBuilder
	recorder *timing.Recorder
}

// BuildModel 只装饰有效模型流，不修改模型配置或业务错误。
func (b timedModelBuilder) BuildModel(snapshot model.ModelSnapshot) (model.Model, error) {
	built, err := b.next.BuildModel(snapshot)
	if err == nil && built.Stream != nil {
		built.Stream = timedModelStream{next: built.Stream, recorder: b.recorder}
	}
	return built, err
}

type timedModelStream struct {
	next     model.ModelStream
	recorder *timing.Recorder
}

// Stream 覆盖请求建立到流结束，首响应仅统计有效内容事件；取消时停止转发。
func (s timedModelStream) Stream(ctx context.Context, request model.ModelRequest) (<-chan model.ModelStreamEvent, error) {
	if ctx == nil {
		return nil, errors.New("provider context is required")
	}
	ctx, span := s.recorder.Start(ctx, timing.Operation{
		Kind:        timing.Provider,
		Name:        request.Model.ProviderID + "/" + request.Model.ModelID,
		ReferenceID: "assistant:" + request.TurnID.String(),
	})
	streamCtx, cancel := context.WithCancel(ctx)
	input, err := s.next.Stream(streamCtx, request)
	if err == nil && input == nil {
		err = errors.New("provider returned a nil stream")
	}
	if err != nil {
		span.End(timing.ResultStatus(ctx, err))
		cancel()
		return nil, err
	}
	output := make(chan model.ModelStreamEvent)
	go func() {
		defer close(output)
		defer cancel()
		status := timing.Failed
		defer func() { span.End(status) }()
		for {
			select {
			case <-ctx.Done():
				status = timing.Cancelled
				return
			case event, ok := <-input:
				if !ok {
					status = timing.ResultStatus(ctx, nil)
					return
				}
				if (event.Kind == model.StreamTextDelta || event.Kind == model.StreamThinkingDelta) && event.Text != "" || event.Kind == model.StreamToolCall {
					span.FirstResponse()
				}
				knownContent := event.Kind == model.StreamTextDelta || event.Kind == model.StreamThinkingDelta || event.Kind == model.StreamToolCall || event.Kind == model.StreamUsage
				terminal := !knownContent
				if terminal {
					status = timing.ResultStatus(ctx, event.Err)
					if event.Kind != model.StreamComplete && status == timing.Completed {
						status = timing.Failed
					}
					// 先保存计时，再让业务层收到终态，避免刷新历史时丢失最后一次计时。
					span.End(status)
				}
				select {
				case output <- event:
				case <-ctx.Done():
					status = timing.Cancelled
					return
				}
				if terminal {
					return
				}
			}
		}
	}()
	return output, nil
}

type timedToolCalls struct {
	next     agentruntime.ToolCallHandler
	recorder *timing.Recorder
}

// Invoke 统计调用边界的校验、准入、执行和结果保存；业务失败与 Go 错误均记失败。
func (t timedToolCalls) Invoke(ctx context.Context, call toolcontracts.ToolCall, metadata agentruntime.ToolInvocationMetadata) (result toolcontracts.ToolResult, err error) {
	ctx, span := t.recorder.Start(ctx, timing.Operation{
		SessionID:   metadata.SessionID.String(),
		AgentID:     metadata.AgentID.String(),
		TaskID:      metadata.TaskID.String(),
		Kind:        timing.Tool,
		Name:        string(call.Name),
		ReferenceID: call.ID,
	})
	status := timing.Failed
	defer func() { span.End(status) }()
	result, err = t.next.Invoke(ctx, call, metadata)
	resultErr := err
	if resultErr == nil && result.ErrorClass != "" {
		resultErr = errors.New(result.ErrorClass)
	}
	status = timing.ResultStatus(ctx, resultErr)
	return
}

// RecordAssistant 不启动工具计时，只转交原子保存消息与调用意图的事务。
func (t timedToolCalls) RecordAssistant(ctx context.Context, recorder agentruntime.MessageRecorder, message sessionmodel.MessageData, metadata agentruntime.ToolInvocationMetadata) error {
	return t.next.RecordAssistant(ctx, recorder, message, metadata)
}
