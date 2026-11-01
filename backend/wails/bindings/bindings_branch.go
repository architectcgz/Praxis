package bindings

import (
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
	coordinator "praxis/internal/service/coordinator"
	"praxis/wails/dto"
	"praxis/wails/validation"
	"strings"
)

func (b *SessionBindings) GetSessionSummary(sessionID string) (dto.SessionSummary, error) {
	if err := validation.Value("sessionId", sessionID, "session_id"); err != nil {
		return dto.SessionSummary{}, publicInputError(b.host, "GetSessionSummary", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return dto.SessionSummary{}, err
	}
	info, err := services.Sessions.GetSessionInfo(ctx, identity.SessionID(sessionID))
	if err != nil {
		return dto.SessionSummary{}, publicError(b.host, "GetSessionSummary", err)
	}
	return sessionSummary(info), nil
}
func (b *SessionBindings) CreateBranch(wire dto.CreateBranchRequest) (dto.SessionSnapshot, error) {
	wire.Name = strings.TrimSpace(wire.Name)
	if err := validation.Struct(wire); err != nil {
		return dto.SessionSnapshot{}, publicInputError(b.host, "CreateBranch", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return dto.SessionSnapshot{}, err
	}
	view, err := services.Commands.CreateBranch(ctx, identity.SessionID(wire.SessionID), identity.BranchID(wire.BranchID), wire.BindingID, sessionmodel.BranchInput{
		RequestID: wire.RequestID,
		Name:      wire.Name,
		EntryID:   wire.EntryID,
	})
	if err != nil {
		return dto.SessionSnapshot{}, publicError(b.host, "CreateBranch", err)
	}
	return b.sessionSnapshot(ctx, services, view)
}
func (b *SessionBindings) SwitchBranch(wire dto.SwitchBranchRequest) (dto.SessionSnapshot, error) {
	wire.Name = strings.TrimSpace(wire.Name)
	if err := validation.Struct(wire); err != nil {
		return dto.SessionSnapshot{}, publicInputError(b.host, "SwitchBranch", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return dto.SessionSnapshot{}, err
	}
	view, err := services.Commands.SwitchBranch(ctx, identity.SessionID(wire.SessionID), identity.BranchID(wire.BranchID), wire.BindingID, wire.Name)
	if err != nil {
		return dto.SessionSnapshot{}, publicError(b.host, "SwitchBranch", err)
	}
	return b.sessionSnapshot(ctx, services, view)
}
func (b *SessionBindings) ListBranches(sessionID, bindingID string) ([]coordinator.BranchView, error) {
	if err := validation.Value("sessionId", sessionID, "session_id"); err != nil {
		return nil, publicInputError(b.host, "ListBranches", err)
	}
	if err := validation.Value("bindingId", bindingID, "omitempty,id"); err != nil {
		return nil, publicInputError(b.host, "ListBranches", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return nil, err
	}
	values, err := services.Commands.ListBranches(ctx, identity.SessionID(sessionID), bindingID)
	return values, publicError(b.host, "ListBranches", err)
}
func (b *SessionBindings) GetHistoryTree(sessionID string) ([]sessionmodel.TreeNode, error) {
	if err := validation.Value("sessionId", sessionID, "session_id"); err != nil {
		return nil, publicInputError(b.host, "GetHistoryTree", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return nil, err
	}
	values, err := services.Commands.Tree(ctx, identity.SessionID(sessionID))
	return values, publicError(b.host, "GetHistoryTree", err)
}
func (b *SessionBindings) GetHistoryAt(sessionID, entryID string) ([]dto.Entry, error) {
	if err := validation.Value("sessionId", sessionID, "session_id"); err != nil {
		return nil, publicInputError(b.host, "GetHistoryAt", err)
	}
	if err := validation.Value("entryId", entryID, "omitempty,id"); err != nil {
		return nil, publicInputError(b.host, "GetHistoryAt", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return nil, err
	}
	values, err := services.Commands.HistoryAt(ctx, identity.SessionID(sessionID), entryID)
	if err != nil {
		return nil, publicError(b.host, "GetHistoryAt", err)
	}
	return publicEntries(values), nil
}
func (b *SessionBindings) CloseSession(pageID, bindingID string) error {
	if err := validation.ID("pageId", pageID); err != nil {
		return publicInputError(b.host, "CloseSession", err)
	}
	if err := validation.Value("bindingId", bindingID, "omitempty,id"); err != nil {
		return publicInputError(b.host, "CloseSession", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return err
	}
	return publicError(b.host, "CloseSession", services.Commands.CloseSession(ctx, pageID, bindingID))
}
