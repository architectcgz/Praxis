package timing

import (
	"context"

	taskmodel "praxis/internal/core/task"
	"praxis/internal/hooks"
)

// Hook 将 Agent、Provider 和 Tool 执行边界映射成计时记录；其余操作及 nil Recorder 均忽略。
type Hook struct {
	Recorder *Recorder
}

// Start 启动单次计时并在 Context 中保留父子 Span 关联；归属由调用方显式提供。
func (h Hook) Start(ctx context.Context, operation hooks.Operation) (context.Context, hooks.Scope) {
	switch operation.Kind {
	case hooks.OperationAgent, hooks.OperationProvider, hooks.OperationTool:
	default:
		return ctx, nil
	}
	ctx, span := h.Recorder.Start(ctx, Operation{
		SessionID:   operation.SessionID.String(),
		AgentID:     operation.AgentID.String(),
		TaskID:      operation.TaskID.String(),
		Kind:        Kind(operation.Kind),
		Name:        operation.Name,
		ReferenceID: operation.ReferenceID,
	})
	if span == nil {
		return ctx, nil
	}
	return ctx, hookScope{ctx: ctx, kind: operation.Kind, span: span}
}

type hookScope struct {
	ctx  context.Context
	kind hooks.OperationKind
	span *Span
}

func (s hookScope) OnSignal(signal hooks.Signal) {
	if signal == hooks.SignalFirstResponse {
		s.span.FirstResponse()
	}
}

func (s hookScope) End(result hooks.Result) {
	status := ResultStatus(s.ctx, result.Err)
	switch s.kind {
	case hooks.OperationAgent:
		switch result.Outcome {
		case taskmodel.TaskFailed:
			status = Failed
		case taskmodel.TaskInterrupted, taskmodel.TaskPaused:
			status = Cancelled
		}
		if s.ctx.Err() != nil {
			status = Cancelled
		}
	case hooks.OperationTool:
		if status == Completed && result.ToolErrorClass != "" {
			status = Failed
		}
	}
	s.span.End(status)
}

var _ hooks.OperationHook = Hook{}
