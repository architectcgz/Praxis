package loop

import (
	"context"
	"praxis/internal/core/hook"
	sessionentry "praxis/internal/core/session/entry"
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

func (r *Runner) notifyAssistant(ctx context.Context, input Input, entry sessionentry.ContextEntry, final bool) {
	event := hook.EntryCommitted{Identity: executionIdentity(input, identity.TurnID(entry.TurnID)), Entry: entry}
	ctx = context.WithoutCancel(ctx)
	if err := r.HookDispatcher.AssistantCommitted(ctx, event); err != nil {
		r.Logf("assistant committed hook failed: %v", err)
	}
	if final {
		if err := r.HookDispatcher.FinalOutputCommitted(ctx, event); err != nil {
			r.Logf("final output committed hook failed: %v", err)
		}
	}
}
