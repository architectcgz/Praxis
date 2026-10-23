package loop

import (
	"errors"
	providerapi "praxis/internal/core/provider"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
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
	event providerapi.Event,
) {
	output := Event{
		AgentID:     agentID,
		ExecutionID: executionID,
		TurnID:      turnID,
		SessionID:   sessionID,
	}
	switch event.Kind {
	case providerapi.StreamTextDelta:
		output.Kind = EventTextDelta
		output.Text = event.Text
	case providerapi.StreamThinkingDelta:
		output.Kind = EventThinkingDelta
		output.Text = event.Text
	case providerapi.StreamUsage:
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
	block sessionentry.Block,
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
	message := "input failed"
	var failure *Failure
	if errors.As(err, &failure) {
		if failure.message != "" {
			message = failure.message
		} else if detail := providerFailureMessage(failure.code, failure.cause); detail != "" {
			message = detail
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

func providerFailureMessage(code FailureCode, err error) string {
	if code != FailureProvider || err == nil {
		return ""
	}
	text := []rune(err.Error())
	if len(text) > 4096 {
		return string(text[:4096]) + "..."
	}
	return string(text)
}
