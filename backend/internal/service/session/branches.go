package session

import (
	"context"
	sessionmodel "praxis/internal/core/session"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
)

func (s *Service) Prepare(ctx context.Context, id identity.SessionID, branchID identity.BranchID) (owner *sessionmodel.Session, branch sessionmodel.Branch, err error) {
	owner, err = s.OpenedSession(ctx, id)
	if err != nil {
		return
	}
	branch, err = owner.Prepare(branchID)
	return
}

func (s *Service) CreateBranch(ctx context.Context, id identity.SessionID, source sessionmodel.Branch, input sessionmodel.BranchInput) (branch sessionmodel.Branch, err error) {
	owner, err := s.OpenedSession(ctx, id)
	if err != nil {
		return branch, err
	}
	return owner.CreateBranch(ctx, source.ID, input)
}

func (s *Service) Branches(ctx context.Context, id identity.SessionID) ([]sessionmodel.Branch, error) {
	owner, err := s.OpenedSession(ctx, id)
	if err != nil {
		return nil, err
	}
	return owner.Branches(), nil
}

func (s *Service) Tree(ctx context.Context, id identity.SessionID) ([]sessionmodel.TreeNode, error) {
	owner, err := s.OpenedSession(ctx, id)
	if err != nil {
		return nil, err
	}
	return owner.Tree(), nil
}

func (s *Service) Path(ctx context.Context, id identity.SessionID, head string) ([]sessionentry.ContextEntry, error) {
	owner, err := s.OpenedSession(ctx, id)
	if err != nil {
		return nil, err
	}
	return owner.Path(head)
}

func (s *Service) SafeHead(ctx context.Context, id identity.SessionID, branchID identity.BranchID) (string, error) {
	owner, err := s.OpenedSession(ctx, id)
	if err != nil {
		return "", err
	}
	return owner.SafeHead(branchID)
}

func (s *Service) DefaultBranch(ctx context.Context, id identity.SessionID) (branch sessionmodel.Branch, err error) {
	owner, err := s.OpenedSession(ctx, id)
	if err != nil {
		return branch, err
	}
	return owner.DefaultBranch(), nil
}

func (s *Service) ConfirmBranchRequest(ctx context.Context, id identity.SessionID, input sessionmodel.BranchInput) (branch sessionmodel.Branch, err error) {
	owner, err := s.OpenedSession(ctx, id)
	if err != nil {
		return branch, err
	}
	return owner.ConfirmBranchRequest(input)
}
