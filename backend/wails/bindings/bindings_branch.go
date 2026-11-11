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
func (b *SessionBindings) ListBranches(sessionID string) ([]coordinator.BranchView, error) {
	if err := validation.Value("sessionId", sessionID, "session_id"); err != nil {
		return nil, publicInputError(b.host, "ListBranches", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return nil, err
	}
	values, err := services.Commands.ListBranches(ctx, identity.SessionID(sessionID))
	return values, publicError(b.host, "ListBranches", err)
}
func (b *SessionBindings) GetEntryTree(sessionID string) ([]sessionmodel.TreeNode, error) {
	if err := validation.Value("sessionId", sessionID, "session_id"); err != nil {
		return nil, publicInputError(b.host, "GetEntryTree", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return nil, err
	}
	values, err := services.Commands.Tree(ctx, identity.SessionID(sessionID))
	return values, publicError(b.host, "GetEntryTree", err)
}
func (b *SessionBindings) GetEntriesAt(request dto.EntryHistoryRequest) (dto.HistoryPage, error) {
	if request.Limit == 0 {
		request.Limit = 50
	}
	if err := validation.Struct(request); err != nil {
		return dto.HistoryPage{}, publicInputError(b.host, "GetEntriesAt", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return dto.HistoryPage{}, err
	}
	page, err := services.Commands.EntriesAt(ctx, identity.SessionID(request.SessionID), request.HeadEntryID, request.BeforeEntryID, request.Limit)
	if err != nil {
		return dto.HistoryPage{}, publicError(b.host, "GetEntriesAt", err)
	}
	return publicHistoryPage(page), nil
}

func (b *SessionBindings) DeleteBranch(request dto.DeleteBranchRequest) (dto.DeleteBranchResponse, error) {
	if err := validation.Struct(request); err != nil {
		return dto.DeleteBranchResponse{}, publicInputError(b.host, "DeleteBranch", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return dto.DeleteBranchResponse{}, err
	}
	result, err := services.Commands.DeleteBranch(ctx, identity.SessionID(request.SessionID), identity.BranchID(request.BranchID), identity.BranchID(request.TargetBranchID), request.BindingID)
	if err != nil {
		return dto.DeleteBranchResponse{}, publicError(b.host, "DeleteBranch", err)
	}
	return dto.DeleteBranchResponse{Branch: result.Branch, BindingID: result.BindingID, CleanupError: result.CleanupError}, nil
}
func (b *SessionBindings) CloseSession(sessionID string) error {
	if err := validation.Value("sessionId", sessionID, "session_id"); err != nil {
		return publicInputError(b.host, "CloseSession", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return err
	}
	return publicError(b.host, "CloseSession", services.Commands.CloseSession(ctx, identity.SessionID(sessionID)))
}
