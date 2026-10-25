package loop

import (
	"errors"
	coreevent "praxis/internal/core/event"
	providerapi "praxis/internal/core/provider"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
)

// emitModelEvent 为实时模型事件补齐归属；工具事件需在 Assistant 条目被内存接纳后发布。
func (r *Runner) emitModelEvent(
	sessionID identity.SessionID,
	agentID identity.AgentID,
	executionID identity.AgentExecutionID,
	turnID identity.TurnID,
	event providerapi.Event,
) {
	output := coreevent.Event{
		AgentID:     agentID,
		ExecutionID: executionID,
		TurnID:      turnID,
		SessionID:   sessionID,
	}
	switch event.Kind {
	case providerapi.StreamTextDelta:
		output.Kind = coreevent.TextDelta
		output.Text = event.Text
	case providerapi.StreamThinkingDelta:
		output.Kind = coreevent.ThinkingDelta
		output.Text = event.Text
	case providerapi.StreamUsage:
		output.Kind = coreevent.ModelUsage
		output.Usage = event.Usage
	default:
		return
	}
	r.Emitter.Emit(output)
}

func (r *Runner) emitToolResult(
	sessionID identity.SessionID,
	agentID identity.AgentID,
	executionID identity.AgentExecutionID,
	turnID identity.TurnID,
	block sessionentry.Block,
) {
	r.Emitter.Emit(coreevent.Event{
		Kind:        coreevent.ToolResult,
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
	r.Emitter.Emit(coreevent.Event{
		Kind:        coreevent.Error,
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
