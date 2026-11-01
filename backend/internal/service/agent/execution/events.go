package execution

import (
	"errors"
	agentexecution "praxis/internal/core/agent/execution"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/identity"
)

func executionIdentity(run *agentexecution.Run, turnID identity.TurnID) hook.Identity {
	task := run.Snapshot()
	return hook.Identity{
		SourceTaskID: task.SourceTaskID,
		SessionID:    task.SessionID,
		AgentID:      task.AgentID,
		ExecutionID:  task.ID,
		TurnID:       turnID,
	}
}

// emitTurnCompletion 只清理回合展示，条目已在各自的接纳边界发布。
func (s *Service) emitTurnCompletion(run *agentexecution.Run, turnID identity.TurnID, reason providerapi.StopReason) {
	s.emitter.Emit(coreevent.Event{
		Source:  coreevent.SourceForRun(run, turnID),
		Payload: coreevent.TurnCompleted{StopReason: reason},
	})
}

func (s *Service) emitError(run *agentexecution.Run, turnID identity.TurnID, err error) {
	message := "input failed"
	if failure, ok := errors.AsType[*loop.Failure](err); ok {
		if failure.Message() != "" {
			message = failure.Message()
		} else if loop.FailureClassification(err) == loop.FailureStorage {
			message = "Failed to save session history."
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
