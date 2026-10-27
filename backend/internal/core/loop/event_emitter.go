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
		Scope: coreevent.Scope{
			SessionID:   sessionID,
			AgentID:     agentID,
			ExecutionID: executionID,
			TurnID:      turnID,
		},
	}
	switch event.Kind {
	case providerapi.StreamTextDelta:
		output.Payload = coreevent.TextDelta{Text: event.Text}
	case providerapi.StreamThinkingDelta:
		output.Payload = coreevent.ThinkingDelta{Text: event.Text}
	case providerapi.StreamUsage:
		if event.Usage == nil || !event.Usage.Valid() {
			return
		}
		output.Payload = coreevent.ModelUsage{Usage: *event.Usage}
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
		Scope: coreevent.Scope{
			SessionID:   sessionID,
			AgentID:     agentID,
			ExecutionID: executionID,
			TurnID:      turnID,
		},
		Payload: coreevent.ToolResult{
			CallID:  block.CallID,
			Result:  block.Text,
			IsError: block.IsError,
		},
	})
}

// emitTurnCompletion 先发布本轮已接纳的条目，再结束实时展示，避免完成通知先清掉未回填的输出。
func (r *Runner) emitTurnCompletion(input Input, deps ExecutionDependencies, turnID identity.TurnID, entries []sessionentry.ContextEntry, stopReason providerapi.StopReason) {
	scope := coreevent.Scope{
		SessionID:   input.SessionID,
		AgentID:     input.AgentID,
		ExecutionID: input.ID,
		TurnID:      turnID,
	}
	deps.History.PublishEntries(turnID, entries)
	r.Emitter.Emit(coreevent.Event{
		Scope:   scope,
		Payload: coreevent.TurnCompleted{StopReason: stopReason},
	})
}

func (r *Runner) emitError(agentID identity.AgentID, executionID identity.AgentExecutionID, turnID identity.TurnID, err error) {
	if err == nil {
		return
	}
	message := "input failed"
	if failure, ok := errors.AsType[*Failure](err); ok {
		if failure.message != "" {
			message = failure.message
		} else if detail := providerFailureMessage(failure.code, failure.cause); detail != "" {
			message = detail
		}
	}
	r.Emitter.Emit(coreevent.Event{
		Scope: coreevent.Scope{
			AgentID:     agentID,
			ExecutionID: executionID,
			TurnID:      turnID,
		},
		Payload: coreevent.ErrorOccurred{Message: message},
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
