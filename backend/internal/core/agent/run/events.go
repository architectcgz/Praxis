package run

import (
	"errors"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	"praxis/internal/identity"
)

func runIdentity(run *Run, turnID identity.TurnID) hook.Identity {
	record := run.Snapshot()
	return hook.Identity{
		SourceTaskID: record.SourceTaskID,
		SessionID:    record.SessionID,
		AgentID:      record.AgentID,
		RunID:        record.ID,
		TurnID:       turnID,
	}
}

func (s *Run) emitError(turnID identity.TurnID, err error) {
	message := "input failed"
	if failure, ok := errors.AsType[*loop.Failure](err); ok {
		if failure.Message() != "" {
			message = failure.Message()
		} else if loop.FailureClassification(err) == loop.FailureStorage {
			message = "Failed to save session entries."
		} else if loop.FailureClassification(err) == loop.FailureResourceLimit {
			message = "model context window exceeded"
		} else if loop.FailureClassification(err) == loop.FailureProvider {
			text := []rune(err.Error())
			if len(text) > 4096 {
				text = append(text[:4096], '.', '.', '.')
			}
			message = string(text)
		}
	}
	s.deps.Emitter.Emit(coreevent.Event{
		Source:  coreevent.SourceForRun(s, turnID),
		Payload: coreevent.ErrorOccurred{Message: message},
	})
}
