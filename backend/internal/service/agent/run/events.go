package run

import (
	"errors"
	agentrun "praxis/internal/core/agent/run"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	"praxis/internal/identity"
)

func runIdentity(run *agentrun.Run, turnID identity.TurnID) hook.Identity {
	task := run.Snapshot()
	return hook.Identity{
		SourceTaskID: task.SourceTaskID,
		SessionID:    task.SessionID,
		AgentID:      task.AgentID,
		RunID:        task.ID,
		TurnID:       turnID,
	}
}

func (s *Service) emitError(run *agentrun.Run, turnID identity.TurnID, err error) {
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
	s.emitter.Emit(coreevent.Event{
		Source:  coreevent.SourceForRun(run, turnID),
		Payload: coreevent.ErrorOccurred{Message: message},
	})
}
