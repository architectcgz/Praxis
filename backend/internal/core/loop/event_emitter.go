package loop

import (
	"errors"
	coreevent "praxis/internal/core/event"
	providerapi "praxis/internal/core/provider"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
)

func (input Input) scope(turnID identity.TurnID) coreevent.Scope {
	return coreevent.Scope{
		SessionID:   input.SessionID,
		BranchID:    input.BranchID,
		BindingID:   input.BindingID,
		AgentID:     input.AgentID,
		ExecutionID: input.ID,
		TurnID:      turnID,
	}
}
func (r *Runner) emitModelEvent(input Input, turnID identity.TurnID, event providerapi.Event) {
	output := coreevent.Event{Scope: input.scope(turnID)}
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
func (r *Runner) emitToolResult(input Input, turnID identity.TurnID, block sessionentry.Block) {
	r.Emitter.Emit(coreevent.Event{Scope: input.scope(turnID), Payload: coreevent.ToolResult{CallID: block.CallID, Result: block.Text, IsError: block.IsError}})
}

// emitTurnCompletion 先发布已接纳条目，再清理实时展示。
func (r *Runner) emitTurnCompletion(input Input, deps ExecutionDependencies, turnID identity.TurnID, entries []sessionentry.ContextEntry, stopReason providerapi.StopReason) {
	deps.Entries.PublishEntries(turnID, entries)
	r.Emitter.Emit(coreevent.Event{Scope: input.scope(turnID), Payload: coreevent.TurnCompleted{StopReason: stopReason}})
}
func (r *Runner) emitError(input Input, turnID identity.TurnID, err error) {
	if err == nil {
		return
	}
	message := "input failed"
	if failure, ok := errors.AsType[*Failure](err); ok {
		if failure.message != "" {
			message = failure.message
		} else if failure.code == FailureStorage {
			message = "Failed to save session history."
		} else if detail := providerFailureMessage(failure.code, failure.cause); detail != "" {
			message = detail
		}
	}
	r.Emitter.Emit(coreevent.Event{Scope: input.scope(turnID), Payload: coreevent.ErrorOccurred{Message: message}})
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
