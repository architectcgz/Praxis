package loop

import (
	"errors"

	"praxis/internal/contracts"
	appcontext "praxis/internal/core/context"
	"praxis/internal/core/model"
)

func (r *Runner) emit(event Event) {
	r.EventObserver(event)
}

// emitModelEvent 为实时模型事件补齐任务归属；工具事件需在 assistant 回执落库后发布。
func (r *Runner) emitModelEvent(
	sessionID contracts.SessionID,
	agentID contracts.AgentID,
	taskID contracts.TaskID,
	turnID contracts.TurnID,
	event model.ModelStreamEvent,
) {
	output := Event{
		AgentID:   agentID,
		TaskID:    taskID,
		TurnID:    turnID,
		SessionID: sessionID,
	}
	switch event.Kind {
	case model.StreamTextDelta:
		output.Kind = EventTextDelta
		output.Text = event.Text
	case model.StreamThinkingDelta:
		output.Kind = EventThinkingDelta
		output.Text = event.Text
	case model.StreamUsage:
		output.Kind = EventModelUsage
		output.Usage = event.Usage
	default:
		return
	}
	r.emit(output)
}

func (r *Runner) emitToolResult(
	sessionID contracts.SessionID,
	agentID contracts.AgentID,
	taskID contracts.TaskID,
	turnID contracts.TurnID,
	block appcontext.ContextBlock,
) {
	r.emit(Event{
		Kind:      EventToolResult,
		SessionID: sessionID,
		AgentID:   agentID,
		TaskID:    taskID,
		TurnID:    turnID,
		CallID:    block.CallID,
		Name:      block.Name,
		Result:    block.Text,
		IsError:   block.IsError,
	})
}

func (r *Runner) emitError(agentID contracts.AgentID, taskID contracts.TaskID, turnID contracts.TurnID, err error) {
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
			if detail := contracts.TaskFailureMessage(failure.code, failure.cause); detail != "" {
				message = detail
			}
		}
	}
	r.emit(Event{
		Kind:    EventError,
		AgentID: agentID,
		TaskID:  taskID,
		TurnID:  turnID,
		Error:   message,
	})
}
