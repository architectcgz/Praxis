package session

import (
	"context"
	"errors"
	"fmt"
	sessionmodel "praxis/internal/core/session"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"slices"
	"strings"
)

type BranchInput struct {
	RequestID string
	Name      string
	EntryID   string
}

type TreeNode struct {
	ID       string            `json:"id"`
	ParentID string            `json:"parentId"`
	BranchID identity.BranchID `json:"branchId"`
	Summary  string            `json:"summary"`
	Safe     bool              `json:"safe"`
}

func (s *Service) Prepare(ctx context.Context, id identity.SessionID, branchID identity.BranchID) (owner *Session, branch sessionmodel.Branch, err error) {
	owner, err = s.openSession(ctx, id)
	if err != nil {
		return
	}
	err = owner.use(ctx, func() error {
		if branchID == "" {
			branchID = owner.data.RecentBranch
		}
		selected := owner.data.Branch(branchID)
		if selected == nil {
			return errorcode.ErrNotFound
		}
		if !owner.data.Safe(selected.HeadEntryID) {
			return sessionmodel.ErrUnsafeHistory
		}
		branch = *selected
		return nil
	})
	return
}

func (s *Service) CreateBranch(ctx context.Context, id identity.SessionID, source sessionmodel.Branch, input BranchInput) (branch sessionmodel.Branch, err error) {
	owner, err := s.openedSession(ctx, id)
	if err != nil {
		return branch, err
	}
	err = owner.use(ctx, func() error {
		if prior := owner.data.BranchByRequest(input.RequestID); prior != nil {
			if prior.SourceBranchID != source.ID || prior.BaseEntryID != input.EntryID || input.Name != "" && prior.Name != input.Name {
				return errorcode.ErrRequestConflict
			}
			branch = *prior
			return nil
		}
		branchID := identity.BranchID(s.ids.New("branch"))
		name := strings.TrimSpace(input.Name)
		if name == "" {
			name = "branch-" + branchID.String()[max(0, len(branchID.String())-8):]
		}
		branch = sessionmodel.Branch{
			ID:             branchID,
			Name:           name,
			RequestID:      input.RequestID,
			SourceBranchID: source.ID,
			BaseEntryID:    input.EntryID,
			HeadEntryID:    input.EntryID,
			CreatedAt:      s.clock.Now(),
			UpdatedAt:      s.clock.Now(),
		}
		return owner.createBranch(ctx, branch)
	})
	return
}

func (s *Service) Branches(ctx context.Context, id identity.SessionID) (branches []sessionmodel.Branch, err error) {
	owner, err := s.openedSession(ctx, id)
	if err != nil {
		return nil, err
	}
	err = owner.use(ctx, func() error {
		branches = slices.Clone(owner.data.Branches)
		return nil
	})
	return
}

func (s *Service) Tree(ctx context.Context, id identity.SessionID) (nodes []TreeNode, err error) {
	owner, err := s.openedSession(ctx, id)
	if err != nil {
		return nil, err
	}
	err = owner.use(ctx, func() error {
		nodes = make([]TreeNode, 0, len(owner.data.Entries))
		for _, entry := range owner.data.Entries {
			text := strings.Join(strings.Fields(entry.TextContent()), " ")
			if entry.IsSummary() {
				text = "上下文摘要"
			}
			if text == "" {
				text = string(entry.Role)
			}
			runes := []rune(text)
			if len(runes) > 100 {
				text = string(runes[:100]) + "…"
			}
			nodes = append(nodes, TreeNode{
				ID:       entry.ID,
				ParentID: entry.ParentID,
				BranchID: entry.BranchID,
				Summary:  text,
				Safe:     owner.data.Safe(entry.ID),
			})
		}
		return nil
	})
	return
}

func (s *Service) Path(ctx context.Context, id identity.SessionID, head string) (entries []sessionentry.ContextEntry, err error) {
	owner, err := s.openedSession(ctx, id)
	if err != nil {
		return nil, err
	}
	err = owner.use(ctx, func() error {
		if _, found := owner.data.Entry(head); head != "" && !found {
			return errorcode.ErrNotFound
		}
		entries = sessionentry.Values(owner.data.Path(head))
		return nil
	})
	return
}

func (s *Service) SafeHead(ctx context.Context, id identity.SessionID, branchID identity.BranchID) (head string, err error) {
	owner, err := s.openedSession(ctx, id)
	if err != nil {
		return "", err
	}
	err = owner.use(ctx, func() error {
		branch := owner.data.Branch(branchID)
		if branch == nil {
			return errorcode.ErrNotFound
		}
		for head = branch.HeadEntryID; head != ""; {
			if owner.data.Safe(head) {
				break
			}
			entry, _ := owner.data.Entry(head)
			head = entry.ParentID
		}
		return nil
	})
	return
}

func (s *Session) createBranch(ctx context.Context, branch sessionmodel.Branch) error {
	if err := validateBranch(s.data, branch); err != nil {
		return err
	}
	if err := s.appendEvent(ctx, "branch.created", "branch", branch.ID.String(), branch); err != nil {
		return err
	}
	s.data.AcceptBranch(branch)
	return nil
}

func validateBranch(owner *sessionmodel.Data, branch sessionmodel.Branch) error {
	if err := branch.Validate(); err != nil {
		return err
	}
	if len(owner.Branches) == 0 && (branch.Name != "main" || branch.BaseEntryID != "" || branch.SourceBranchID != "") {
		return errors.New("initial branch must be main at the session origin")
	}
	if branch.RequestID == "" || owner.Branch(branch.ID) != nil || branch.HeadEntryID != branch.BaseEntryID {
		return errors.New("invalid branch creation identity or head")
	}
	if owner.BranchByRequest(branch.RequestID) != nil {
		return errorcode.ErrRequestConflict
	}
	for _, prior := range owner.Branches {
		if prior.Name == branch.Name {
			return fmt.Errorf("branch name already exists: %w", sessionmodel.ErrBranchNameConflict)
		}
	}
	if _, found := owner.Entry(branch.BaseEntryID); branch.BaseEntryID != "" && !found {
		return errors.New("branch base is missing")
	}
	if !owner.Safe(branch.BaseEntryID) {
		return sessionmodel.ErrUnsafeHistory
	}
	if branch.SourceBranchID != "" && owner.Branch(branch.SourceBranchID) == nil {
		return errors.New("source branch is missing")
	}
	return nil
}

func (s *Service) DefaultBranch(ctx context.Context, id identity.SessionID) (branch sessionmodel.Branch, err error) {
	owner, err := s.openSession(ctx, id)
	if err != nil {
		return branch, err
	}
	err = owner.use(ctx, func() error {
		branch = *owner.data.Branch(owner.data.RecentBranch)
		return nil
	})
	return
}

func (s *Service) ConfirmBranchRequest(ctx context.Context, id identity.SessionID, input BranchInput) (branch sessionmodel.Branch, err error) {
	owner, err := s.openedSession(ctx, id)
	if err != nil {
		return branch, err
	}
	err = owner.use(ctx, func() error {
		prior := owner.data.BranchByRequest(input.RequestID)
		if prior == nil || prior.BaseEntryID != input.EntryID || input.Name != "" && prior.Name != input.Name {
			return errorcode.ErrRequestConflict
		}
		branch = *prior
		return nil
	})
	return
}
