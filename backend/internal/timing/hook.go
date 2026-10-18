package timing

import (
	"context"
	taskmodel "praxis/internal/agent"
	"praxis/internal/hook"
)

// Hook 将 Agent、Provider 和 Tool 执行边界映射成计时记录；其余操作及 nil Recorder 均忽略。
type Hook struct {
	Recorder *Recorder
}

// Start 启动单次计时并在 Context 中保留父子 Span 关联；归属由调用方显式提供。
func (h Hook) Start(ctx context.Context, operation hook.Operation) (context.Context, hook.Scope) {
	switch operation.Kind {
	case hook.OperationAgent, hook.OperationProvider, hook.OperationTool:
	default:
		return ctx, nil
	}
	ctx, span := h.Recorder.Start(ctx, Operation{
		SessionID:   operation.SessionID.String(),
		AgentID:     operation.AgentID.String(),
		ExecutionID: operation.ExecutionID.String(),
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
	kind hook.OperationKind
	span *Span
}

func (s hookScope) OnSignal(signal hook.Signal) {
	if signal == hook.SignalFirstResponse {
		s.span.FirstResponse()
	}
}

func (s hookScope) End(result hook.Result) {
	status := ResultStatus(s.ctx, result.Err)
	switch s.kind {
	case hook.OperationAgent:
		switch result.Outcome {
		case taskmodel.ExecutionFailed:
			status = Failed
		case taskmodel.ExecutionInterrupted, taskmodel.ExecutionPaused:
			status = Cancelled
		}
		if s.ctx.Err() != nil {
			status = Cancelled
		}
	case hook.OperationTool:
		if status == Completed && result.ToolErrorClass != "" {
			status = Failed
		}
	}
	s.span.End(status)
}

var _ hook.OperationHook = Hook{}
