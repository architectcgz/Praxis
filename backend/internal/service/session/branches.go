package session

import (
	"cmp"
	"context"
	"maps"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
	"slices"
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
	branch, existing, err := owner.PrepareBranch(ctx, source.ID, input)
	if err != nil {
		return branch, err
	}
	if existing {
		return branch, s.SelectBranch(ctx, id, branch.ID)
	}
	if err := s.sessions.CreateBranch(ctx, id, branch); err != nil {
		return sessionmodel.Branch{}, err
	}
	owner.AcceptBranch(branch)
	return branch, nil
}

func (s *Service) SelectBranch(ctx context.Context, id identity.SessionID, branchID identity.BranchID) error {
	owner, err := s.OpenedSession(ctx, id)
	if err != nil {
		return err
	}
	if err := s.sessions.SelectBranch(ctx, id, branchID); err != nil {
		return err
	}
	owner.SelectBranch(branchID)
	return nil
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
	return owner.Tree(ctx)
}

func (s *Service) Path(ctx context.Context, id identity.SessionID, head, before string, limit int) (sessionmodel.HistoryPage, error) {
	owner, err := s.OpenedSession(ctx, id)
	if err != nil {
		return sessionmodel.HistoryPage{}, err
	}
	return owner.Page(ctx, head, before, limit)
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

type BranchDeletion struct {
	Branch       sessionmodel.Branch
	CleanupError string
}

// DeleteBranch 由会话命令边界排除执行、队列交接和新建子分支。
// 逻辑提交后的清理错误作为结果返回，不让调用方继续引用已删除分支。
func (s *Service) DeleteBranch(ctx context.Context, id identity.SessionID, target identity.BranchID) (BranchDeletion, error) {
	owner, err := s.openedSession(ctx, id)
	if err != nil {
		return BranchDeletion{}, err
	}
	branches := owner.session.Branches()
	selected := owner.session.DefaultBranch()
	index := slices.IndexFunc(branches, func(branch sessionmodel.Branch) bool { return branch.ID == target })
	if index >= 0 {
		if branches[index].IsRoot() {
			return BranchDeletion{}, sessionmodel.ErrRootBranch
		}
		if slices.ContainsFunc(branches, func(branch sessionmodel.Branch) bool { return branch.SourceBranchID == target }) {
			return BranchDeletion{}, sessionmodel.ErrBranchHasChildren
		}
		remaining := slices.Delete(branches, index, index+1)
		if selected.ID == target {
			slices.SortFunc(remaining, func(a, b sessionmodel.Branch) int {
				return cmp.Or(b.UpdatedAt.Compare(a.UpdatedAt), cmp.Compare(a.ID, b.ID))
			})
			selected = remaining[0]
		}
		if err := s.sessions.DeleteBranch(ctx, id, target, selected.ID); err != nil {
			return BranchDeletion{}, err
		}
		owner.session.RemoveBranch(target, selected.ID)
	}
	maps.DeleteFunc(owner.log.dirty, func(key logKey, _ struct{}) bool { return key.branch == target })
	result := BranchDeletion{Branch: selected}
	if err := owner.log.store.RemoveBranch(ctx, target); err != nil {
		result.CleanupError = err.Error()
	}
	return result, nil
}

func (s *Service) Previews(ctx context.Context, id identity.SessionID, heads []string) (map[string]string, error) {
	owner, err := s.OpenedSession(ctx, id)
	if err != nil {
		return nil, err
	}
	return owner.Previews(ctx, heads)
}
