package execution

import (
	"bytes"

	"praxis/internal/execution/loop"
)

func loopEvent(event loop.Event) Event {
	return Event{
		Kind:        string(event.Kind),
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
