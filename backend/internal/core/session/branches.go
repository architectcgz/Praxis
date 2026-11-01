package session

import (
	"context"
	"errors"
	"fmt"
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

func (s *Session) Prepare(branchID identity.BranchID) (Branch, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if branchID == "" {
		branchID = s.data.RecentBranch
	}
	selected := s.data.Branch(branchID)
	if selected == nil {
		return Branch{}, errorcode.ErrNotFound
	}
	if !s.data.Safe(selected.HeadEntryID) {
		return Branch{}, ErrUnsafeHistory
	}
	return *selected, nil
}

func (s *Session) CreateBranch(ctx context.Context, sourceID identity.BranchID, input BranchInput) (Branch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if prior := s.data.BranchByRequest(input.RequestID); prior != nil {
		if prior.SourceBranchID != sourceID || prior.BaseEntryID != input.EntryID || input.Name != "" && prior.Name != input.Name {
			return Branch{}, errorcode.ErrRequestConflict
		}
		return *prior, nil
	}
	branchID := identity.BranchID(s.ids.New("branch"))
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = "branch-" + branchID.String()[max(0, len(branchID.String())-8):]
	}
	branch := Branch{
		ID:             branchID,
		Name:           name,
		RequestID:      input.RequestID,
		SourceBranchID: sourceID,
		BaseEntryID:    input.EntryID,
		HeadEntryID:    input.EntryID,
		CreatedAt:      s.clock.Now(),
		UpdatedAt:      s.clock.Now(),
	}
	if err := validateBranch(s.data, branch); err != nil {
		return branch, err
	}
	s.data.AcceptBranch(branch)
	return branch, s.appendEvent(ctx, EventBranchCreated, "branch", branch.ID.String(), branch)
}

func (s *Session) Branches() []Branch {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Clone(s.data.Branches)
}

func (s *Session) Tree() []TreeNode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	nodes := make([]TreeNode, 0, len(s.data.Entries))
	for _, entry := range s.data.Entries {
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
			Safe:     s.data.Safe(entry.ID),
		})
	}
	return nodes
}

func (s *Session) Path(head string) ([]sessionentry.ContextEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, found := s.data.Entry(head); head != "" && !found {
		return nil, errorcode.ErrNotFound
	}
	return sessionentry.Values(s.data.Path(head)), nil
}

func (s *Session) SafeHead(branchID identity.BranchID) (head string, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	branch := s.data.Branch(branchID)
	if branch == nil {
		return "", errorcode.ErrNotFound
	}
	for head = branch.HeadEntryID; head != ""; {
		if s.data.Safe(head) {
			break
		}
		entry, _ := s.data.Entry(head)
		head = entry.ParentID
	}
	return head, nil
}

func (s *Session) DefaultBranch() Branch {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return *s.data.Branch(s.data.RecentBranch)
}

func (s *Session) ConfirmBranchRequest(input BranchInput) (Branch, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	prior := s.data.BranchByRequest(input.RequestID)
	if prior == nil || prior.BaseEntryID != input.EntryID || input.Name != "" && prior.Name != input.Name {
		return Branch{}, errorcode.ErrRequestConflict
	}
	return *prior, nil
}

func validateBranch(owner *Data, branch Branch) error {
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
			return fmt.Errorf("branch name already exists: %w", ErrBranchNameConflict)
		}
	}
	if _, found := owner.Entry(branch.BaseEntryID); branch.BaseEntryID != "" && !found {
		return errors.New("branch base is missing")
	}
	if !owner.Safe(branch.BaseEntryID) {
		return ErrUnsafeHistory
	}
	if branch.SourceBranchID != "" && owner.Branch(branch.SourceBranchID) == nil {
		return errors.New("source branch is missing")
	}
	return nil
}
