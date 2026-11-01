package loop

import (
	coreevent "praxis/internal/core/event"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/identity"
)

func (r *Runner) emitModelEvent(execution ExecutionParams, turnID identity.TurnID, event providerapi.Event) {
	output := coreevent.Event{
		Source: coreevent.EventSource{
			SessionID:   execution.SessionID,
			AgentID:     execution.AgentID,
			BranchID:    execution.BranchID,
			BindingID:   execution.BindingID,
			ExecutionID: execution.ID,
			TurnID:      turnID,
		},
	}
	switch event.Kind {
	case providerapi.StreamTextDelta:
		output.Payload = coreevent.TextDelta{Text: event.Text}
	case providerapi.StreamThinkingDelta:
		output.Payload = coreevent.ThinkingDelta{Text: event.Text}
	default:
		return
	}
	r.Emitter.Emit(output)
}
