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
