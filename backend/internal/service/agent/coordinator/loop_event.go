package coordinator

import (
	"bytes"
	"praxis/internal/loop"
)

func LoopEvent(event loop.Event) Event {
	return Event{
		Kind:        string(event.Kind),
		Entries:     event.Entries,
		SessionID:   event.SessionID,
		AgentID:     event.AgentID,
		ExecutionID: event.ExecutionID,
		TurnID:      event.TurnID,
		Text:        event.Text,
		CallID:      event.CallID,
		Name:        event.Name,
		Input:       bytes.Clone(event.Input),
		Result:      event.Result,
		IsError:     event.IsError,
		Error:       event.Error,
		Usage:       event.Usage,
	}
}
