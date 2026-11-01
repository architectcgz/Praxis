package loop

import (
	"praxis/internal/core/hook"
	"praxis/internal/identity"
)

func executionIdentity(execution ExecutionParams, turnID identity.TurnID) hook.Identity {
	return hook.Identity{
		SourceTaskID: execution.SourceTaskID,
		SessionID:    execution.SessionID,
		AgentID:      execution.AgentID,
		ExecutionID:  execution.ID,
		TurnID:       turnID,
	}
}
