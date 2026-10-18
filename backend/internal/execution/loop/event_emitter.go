package loop

import (
	"errors"
	taskmodel "praxis/internal/agent"
	appcontext "praxis/internal/context"
	"praxis/internal/identity"
	modelapi "praxis/internal/model"
)

func (r *Runner) emit(event Event) {
	r.EventObserver(event)
}

// emitModelEvent 为实时模型事件补齐任务归属；工具事件需在 assistant 回执落库后发布。
func (r *Runner) emitModelEvent(
	sessionID identity.SessionID,
	agentID identity.AgentID,
	executionID identity.AgentExecutionID,
	turnID identity.TurnID,
	event modelapi.ModelStreamEvent,
) {
	output := Event{
		AgentID:     agentID,
		ExecutionID: executionID,
		TurnID:      turnID,
		SessionID:   sessionID,
	}
	switch event.Kind {
	case modelapi.StreamTextDelta:
		output.Kind = EventTextDelta
		output.Text = event.Text
	case modelapi.StreamThinkingDelta:
		output.Kind = EventThinkingDelta
		output.Text = event.Text
	case modelapi.StreamUsage:
		output.Kind = EventModelUsage
		output.Usage = event.Usage
	default:
		return
	}
	r.emit(output)
}

func (r *Runner) emitToolResult(
	sessionID identity.SessionID,
	agentID identity.AgentID,
	executionID identity.AgentExecutionID,
	turnID identity.TurnID,
	block appcontext.ContextBlock,
) {
	r.emit(Event{
		Kind:        EventToolResult,
		SessionID:   sessionID,
		AgentID:     agentID,
		ExecutionID: executionID,
		TurnID:      turnID,
		CallID:      block.CallID,
		Name:        block.Name,
		Result:      block.Text,
		IsError:     block.IsError,
	})
}

func (r *Runner) emitError(agentID identity.AgentID, executionID identity.AgentExecutionID, turnID identity.TurnID, err error) {
	if err == nil {
		return
	}
	message := "task failed"
	var runtimeErr *RuntimeError
	if errors.As(err, &runtimeErr) && runtimeErr.Message != "" {
		message = runtimeErr.Message
	} else {
		var failure *taskFailure
		if errors.As(err, &failure) {
			if detail := taskmodel.ExecutionFailureMessage(failure.code, failure.cause); detail != "" {
				message = detail
			}
		}
	}
	r.emit(Event{
		Kind:        EventError,
		AgentID:     agentID,
		ExecutionID: executionID,
		TurnID:      turnID,
		Error:       message,
	})
}
