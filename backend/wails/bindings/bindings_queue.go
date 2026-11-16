package bindings

import (
	"praxis/internal/identity"
	"praxis/wails/validation"
)

func (b *CommandBindings) PauseQueue(sessionID string) error {
	if err := validation.Value("sessionId", sessionID, "session_id"); err != nil {
		return publicInputError(b.host, "PauseQueue", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return err
	}
	return publicError(b.host, "PauseQueue", services.Commands.PauseQueue(ctx, identity.SessionID(sessionID)))
}
func (b *CommandBindings) ResumeQueue(sessionID string) error {
	if err := validation.Value("sessionId", sessionID, "session_id"); err != nil {
		return publicInputError(b.host, "ResumeQueue", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return err
	}
	return publicError(b.host, "ResumeQueue", services.Commands.ResumeQueue(ctx, identity.SessionID(sessionID)))
}
