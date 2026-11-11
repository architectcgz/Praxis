package session

import (
	"context"
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
	ID       string `json:"id"`
	ParentID string `json:"parentId"`
	Summary  string `json:"summary"`
	Safe     bool   `json:"safe"`
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
	return *selected, nil
}

func (s *Session) PrepareBranch(ctx context.Context, sourceID identity.BranchID, input BranchInput) (Branch, bool, error) {
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	s.mu.RLock()
	if prior := s.data.BranchByRequest(input.RequestID); prior != nil {
		branch := *prior
		s.mu.RUnlock()
		if branch.BaseEntryID != input.EntryID || input.Name != "" && branch.Name != input.Name {
			return Branch{}, false, errorcode.ErrRequestConflict
		}
		return branch, true, nil
	}
	source := s.data.Branch(sourceID)
	var head string
	if source != nil {
		head = source.HeadEntryID
	}
	branches := slices.Clone(s.data.Branches)
	s.mu.RUnlock()
	if source == nil {
		return Branch{}, false, errorcode.ErrNotFound
	}
	nodes, err := s.pathNodes(ctx, head)
	if err != nil {
		return Branch{}, false, err
	}
	index := slices.IndexFunc(nodes, func(node sessionentry.Node) bool { return node.ID == input.EntryID })
	if index < 0 || !nodes[index].Run || nodes[index].AgentID != s.data.MainAgentID() {
		return Branch{}, false, ErrInvalidForkPoint
	}
	branchID := identity.BranchID(s.ids.New("branch"))
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = "branch-" + branchID.String()[max(0, len(branchID.String())-8):]
	}
	if slices.ContainsFunc(branches, func(branch Branch) bool { return branch.Name == name }) {
		return Branch{}, false, fmt.Errorf("branch name already exists: %w", ErrBranchNameConflict)
	}
	at := s.clock.Now()
	return Branch{
		ID:             branchID,
		Name:           name,
		RequestID:      input.RequestID,
		SourceBranchID: nodes[index].BranchID,
		BaseEntryID:    input.EntryID,
		HeadEntryID:    input.EntryID,
		CreatedAt:      at,
		UpdatedAt:      at,
	}, false, nil
}

func (s *Session) AcceptBranch(branch Branch) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.AcceptBranch(branch)
}
func (s *Session) SelectBranch(id identity.BranchID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.RecentBranch = id
}
func (s *Session) Branches() []Branch {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Clone(s.data.Branches)
}

func (s *Session) Tree(ctx context.Context) ([]TreeNode, error) {
	s.commitMu.Lock()
	if err := s.syncHistory(ctx); err != nil {
		s.commitMu.Unlock()
		return nil, err
	}
	nodes, err := s.log.TreeNodes(ctx)
	s.mu.RLock()
	for _, node := range s.data.PendingNodes {
		if node.AgentID == s.data.MainAgentID() {
			nodes = append(nodes, node)
		}
	}
	s.mu.RUnlock()
	s.commitMu.Unlock()
	if err != nil {
		return nil, err
	}
	children := make(map[string][]sessionentry.Node)
	seen := make(map[string]bool)
	for _, node := range nodes {
		if !seen[node.ID] {
			children[node.ParentID] = append(children[node.ParentID], node)
			seen[node.ID] = true
		}
	}
	pending := slices.Clone(children[""])
	slices.Reverse(pending)
	tree := make([]TreeNode, 0, len(nodes))
	for len(pending) > 0 {
		node := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		tree = append(tree, TreeNode{
			ID:       node.ID,
			ParentID: node.ParentID,
			Summary:  node.PreviewText,
			Safe:     node.Run,
		})
		for index := len(children[node.ID]) - 1; index >= 0; index-- {
			pending = append(pending, children[node.ID][index])
		}
	}
	return tree, nil
}

func (s *Session) Previews(ctx context.Context, ids []string) (map[string]string, error) {
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	if err := s.syncHistory(ctx); err != nil {
		return nil, err
	}
	nodes, err := s.log.IndexedNodes(ctx, ids)
	if err != nil {
		return nil, err
	}
	previews := make(map[string]string, len(ids))
	for _, node := range nodes {
		previews[node.ID] = node.PreviewText
	}
	s.mu.RLock()
	for _, id := range ids {
		if node, found := s.data.PendingNodes[id]; found {
			previews[id] = node.PreviewText
		}
	}
	s.mu.RUnlock()
	for _, id := range ids {
		if _, found := previews[id]; !found {
			return nil, errorcode.ErrNotFound
		}
	}
	return previews, nil
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

func (s *Session) RemoveBranch(id, selected identity.BranchID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.RemoveBranch(id)
	s.data.RecentBranch = selected
}
