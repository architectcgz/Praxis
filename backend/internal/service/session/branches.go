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
	owner, err = s.OpenedSession(ctx, id)
	if err != nil {
		return
	}
	owner.mu.RLock()
	defer owner.mu.RUnlock()
	if err := owner.checkState(ctx); err != nil {
		return owner, branch, err
	}
	if branchID == "" {
		branchID = owner.data.RecentBranch
	}
	selected := owner.data.Branch(branchID)
	if selected == nil {
		return owner, branch, errorcode.ErrNotFound
	}
	if !owner.data.Safe(selected.HeadEntryID) {
		return owner, branch, sessionmodel.ErrUnsafeHistory
	}
	return owner, *selected, nil
}

func (s *Service) CreateBranch(ctx context.Context, id identity.SessionID, source sessionmodel.Branch, input BranchInput) (branch sessionmodel.Branch, err error) {
	owner, err := s.OpenedSession(ctx, id)
	if err != nil {
		return branch, err
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if err := owner.checkState(ctx); err != nil {
		return branch, err
	}
	if prior := owner.data.BranchByRequest(input.RequestID); prior != nil {
		if prior.SourceBranchID != source.ID || prior.BaseEntryID != input.EntryID || input.Name != "" && prior.Name != input.Name {
			return branch, errorcode.ErrRequestConflict
		}
		return *prior, nil
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
	return branch, owner.createBranch(ctx, branch)
}

func (s *Service) Branches(ctx context.Context, id identity.SessionID) (branches []sessionmodel.Branch, err error) {
	owner, err := s.OpenedSession(ctx, id)
	if err != nil {
		return nil, err
	}
	owner.mu.RLock()
	defer owner.mu.RUnlock()
	if err := owner.checkState(ctx); err != nil {
		return nil, err
	}
	return slices.Clone(owner.data.Branches), nil
}

func (s *Service) Tree(ctx context.Context, id identity.SessionID) (nodes []TreeNode, err error) {
	owner, err := s.OpenedSession(ctx, id)
	if err != nil {
		return nil, err
	}
	owner.mu.RLock()
	defer owner.mu.RUnlock()
	if err := owner.checkState(ctx); err != nil {
		return nil, err
	}
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
	return nodes, nil
}

func (s *Service) Path(ctx context.Context, id identity.SessionID, head string) (entries []sessionentry.ContextEntry, err error) {
	owner, err := s.OpenedSession(ctx, id)
	if err != nil {
		return nil, err
	}
	owner.mu.RLock()
	defer owner.mu.RUnlock()
	if err := owner.checkState(ctx); err != nil {
		return nil, err
	}
	if _, found := owner.data.Entry(head); head != "" && !found {
		return nil, errorcode.ErrNotFound
	}
	return sessionentry.Values(owner.data.Path(head)), nil
}

func (s *Service) SafeHead(ctx context.Context, id identity.SessionID, branchID identity.BranchID) (head string, err error) {
	owner, err := s.OpenedSession(ctx, id)
	if err != nil {
		return "", err
	}
	owner.mu.RLock()
	defer owner.mu.RUnlock()
	if err := owner.checkState(ctx); err != nil {
		return "", err
	}
	branch := owner.data.Branch(branchID)
	if branch == nil {
		return "", errorcode.ErrNotFound
	}
	for head = branch.HeadEntryID; head != ""; {
		if owner.data.Safe(head) {
			break
		}
		entry, _ := owner.data.Entry(head)
		head = entry.ParentID
	}
	return head, nil
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
	owner, err := s.OpenedSession(ctx, id)
	if err != nil {
		return branch, err
	}
	owner.mu.RLock()
	defer owner.mu.RUnlock()
	if err := owner.checkState(ctx); err != nil {
		return branch, err
	}
	return *owner.data.Branch(owner.data.RecentBranch), nil
}

func (s *Service) ConfirmBranchRequest(ctx context.Context, id identity.SessionID, input BranchInput) (branch sessionmodel.Branch, err error) {
	owner, err := s.OpenedSession(ctx, id)
	if err != nil {
		return branch, err
	}
	owner.mu.RLock()
	defer owner.mu.RUnlock()
	if err := owner.checkState(ctx); err != nil {
		return branch, err
	}
	prior := owner.data.BranchByRequest(input.RequestID)
	if prior == nil || prior.BaseEntryID != input.EntryID || input.Name != "" && prior.Name != input.Name {
		return branch, errorcode.ErrRequestConflict
	}
	return *prior, nil
}
