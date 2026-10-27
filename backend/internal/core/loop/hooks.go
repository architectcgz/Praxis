package loop

import (
	"praxis/internal/core/hook"
	"praxis/internal/identity"
)

func executionIdentity(input Input, turnID identity.TurnID) hook.Identity {
	return hook.Identity{
		SourceTaskID: input.SourceTaskID,
		SessionID:    input.SessionID,
		AgentID:      input.AgentID,
		ExecutionID:  input.ID,
		TurnID:       turnID,
	}
}
