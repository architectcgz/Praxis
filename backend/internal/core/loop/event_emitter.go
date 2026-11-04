package loop

import (
	coreevent "praxis/internal/core/event"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/identity"
)

func (r *Runner) emit(params RunParams, turnID identity.TurnID, payload coreevent.Payload) {
	r.Emitter.Emit(coreevent.Event{
		Source: coreevent.EventSource{
			SessionID: params.SessionID,
			AgentID:   params.AgentID,
			BranchID:  params.BranchID,
			BindingID: params.BindingID,
			RunID:     params.ID,
			TurnID:    turnID,
		},
		Payload: payload,
	})
}

func (r *Runner) emitModelEvent(params RunParams, turnID identity.TurnID, event providerapi.Event) {
	switch event.Kind {
	case providerapi.StreamTextDelta:
		r.emit(params, turnID, coreevent.TextDelta{Text: event.Text})
	case providerapi.StreamThinkingDelta:
		r.emit(params, turnID, coreevent.ThinkingDelta{Text: event.Text})
	}
}
