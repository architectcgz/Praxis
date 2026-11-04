package loop

import (
	"praxis/internal/core/hook"
	"praxis/internal/identity"
)

func runIdentity(params RunParams, turnID identity.TurnID) hook.Identity {
	return hook.Identity{
		SourceTaskID: params.SourceTaskID,
		SessionID:    params.SessionID,
		AgentID:      params.AgentID,
		RunID:        params.ID,
		TurnID:       turnID,
	}
}
