package session

import (
	"context"
	"errors"
	"fmt"
	agentrun "praxis/internal/core/agent/run"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"reflect"
	"slices"
)

func (s *Session) AppendEntry(ctx context.Context, branchID identity.BranchID, value sessionentry.ContextEntry) (*sessionentry.ContextEntry, bool, error) {
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	return s.appendEntry(ctx, branchID, s.data.MainAgentID(), value)
}

// appendEntry 的调用方持有提交锁；磁盘读取和提交期间不持有数据锁。
func (s *Session) appendEntry(ctx context.Context, branchID identity.BranchID, agentID identity.AgentID, value sessionentry.ContextEntry) (*sessionentry.ContextEntry, bool, error) {
	prior, err := s.entry(ctx, value.ID)
	if err == nil {
		value.ParentID, value.CreatedAt = prior.ParentID, prior.CreatedAt
		if !reflect.DeepEqual(*prior, value) {
			return nil, false, errorcode.ErrRequestConflict
		}
		node, err := s.node(ctx, value.ID)
		if err != nil {
			return nil, false, err
		}
		if node.AgentID != agentID {
			return nil, false, errorcode.ErrRequestConflict
		}
		if agentID == s.data.MainAgentID() {
			s.mu.RLock()
			branch := s.data.Branch(branchID)
			var head string
			if branch != nil {
				head = branch.HeadEntryID
			}
			s.mu.RUnlock()
			nodes, err := s.pathNodes(ctx, head)
			if err != nil {
				return nil, false, err
			}
			if !slices.ContainsFunc(nodes, func(node sessionentry.Node) bool { return node.ID == prior.ID }) {
				return nil, false, errorcode.ErrRequestConflict
			}
		}
		return prior, false, nil
	}
	if !errors.Is(err, errorcode.ErrNotFound) {
		return nil, false, err
	}
	s.mu.Lock()
	if s.data.RunEndEntries[identity.RunID(value.RunID)] != "" {
		s.mu.Unlock()
		return nil, false, errorcode.ErrInvalidTransition
	}
	if agentID == s.data.MainAgentID() {
		branch := s.data.Branch(branchID)
		if branch == nil {
			s.mu.Unlock()
			return nil, false, errorcode.ErrNotFound
		}
		value.ParentID = branch.HeadEntryID
	} else {
		history, found := s.data.AgentHistories[agentID]
		if !found || history.BranchID != branchID {
			s.mu.Unlock()
			return nil, false, errorcode.ErrNotFound
		}
		value.ParentID = history.HeadEntryID
	}
	if value.CreatedAt.IsZero() {
		value.CreatedAt = s.clock.Now()
	}
	stored := s.data.acceptEntry(branchID, agentID, value)
	s.mu.Unlock()
	persisted, err := s.log.AppendEvent(ctx, Event{
		Type:      EventEntryAppended,
		Key:       stored.ID,
		BranchID:  branchID,
		AgentID:   agentID,
		CreatedAt: s.clock.Now(),
		Payload:   *stored,
	})
	if persisted {
		s.mu.Lock()
		s.data.releaseEntry(stored.ID)
		s.mu.Unlock()
	}
	return stored, true, err
}

func (s *Session) Entry(ctx context.Context, id string) (*sessionentry.ContextEntry, error) {
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	return s.entry(ctx, id)
}

func (s *Session) entry(ctx context.Context, id string) (*sessionentry.ContextEntry, error) {
	entries, err := s.readEntries(ctx, []string{id})
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, errorcode.ErrNotFound
	}
	return entries[0], nil
}

func (s *Session) readEntries(ctx context.Context, ids []string) ([]*sessionentry.ContextEntry, error) {
	if err := s.syncHistory(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	pending := make(map[string]*sessionentry.ContextEntry)
	var diskIDs []string
	for _, id := range ids {
		if entry := s.data.PendingEntries[id]; entry != nil {
			pending[id] = entry
		} else {
			diskIDs = append(diskIDs, id)
		}
	}
	s.mu.RUnlock()
	entries, err := s.log.Entries(ctx, diskIDs)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		pending[entry.ID] = entry
	}
	values := make([]*sessionentry.ContextEntry, 0, len(ids))
	for _, id := range ids {
		if entry := pending[id]; entry != nil {
			values = append(values, entry)
		}
	}
	return values, nil
}

func (s *Session) node(ctx context.Context, id string) (sessionentry.Node, error) {
	if err := s.syncHistory(ctx); err != nil {
		return sessionentry.Node{}, err
	}
	s.mu.RLock()
	node, found := s.data.PendingNodes[id]
	s.mu.RUnlock()
	if found {
		return node, nil
	}
	return s.log.Node(ctx, id)
}

// pathNodes 批量读取已落盘路径，只逐个连接尚未落盘的短尾部。
func (s *Session) pathNodes(ctx context.Context, head string) ([]sessionentry.Node, error) {
	if err := s.syncHistory(ctx); err != nil {
		return nil, err
	}
	var tail []sessionentry.Node
	s.mu.RLock()
	for head != "" {
		node, found := s.data.PendingNodes[head]
		if !found {
			break
		}
		tail = append(tail, node)
		head = node.ParentID
	}
	s.mu.RUnlock()
	nodes, err := s.log.PathNodes(ctx, head)
	if err != nil {
		return nil, err
	}
	if head != "" && (len(nodes) == 0 || nodes[len(nodes)-1].ID != head) {
		return nil, errorcode.ErrNotFound
	}
	slices.Reverse(tail)
	return append(nodes, tail...), nil
}

// BranchEntries 只读取最后摘要、保留原文和其后的模型可见历史，与界面页大小无关。
func (s *Session) BranchEntries(ctx context.Context, branchID identity.BranchID) ([]*sessionentry.ContextEntry, error) {
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	s.mu.RLock()
	branch := s.data.Branch(branchID)
	var head string
	if branch != nil {
		head = branch.HeadEntryID
	}
	s.mu.RUnlock()
	if branch == nil {
		return nil, errorcode.ErrNotFound
	}
	return s.contextEntries(ctx, head)
}

func (s *Session) contextEntries(ctx context.Context, head string) ([]*sessionentry.ContextEntry, error) {
	nodes, err := s.pathNodes(ctx, head)
	if err != nil {
		return nil, err
	}
	start := 0
	retained := make(map[string]bool)
	for index := len(nodes) - 1; index >= 0; index-- {
		if nodes[index].Summary {
			start = index
			for _, id := range nodes[index].RetainedEntryIDs {
				retained[id] = true
			}
			break
		}
	}
	var ids []string
	for index, node := range nodes {
		if !node.Run && (index >= start || retained[node.ID]) {
			ids = append(ids, node.ID)
		}
	}
	entries, err := s.readEntries(ctx, ids)
	if err != nil {
		return nil, err
	}
	if len(entries) != len(ids) {
		return nil, fmt.Errorf("context path contains an entry without a complete record")
	}
	return orderToolResults(entries), nil
}

type HistoryPage struct {
	HeadEntryID string
	Entries     []sessionentry.ContextEntry
	HasMore     bool
	NextCursor  string
}

func (s *Session) Page(ctx context.Context, head, before string, limit int) (HistoryPage, error) {
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	nodes, err := s.pathNodes(ctx, head)
	if err != nil {
		return HistoryPage{}, err
	}
	if len(nodes) > 0 && nodes[len(nodes)-1].AgentID != s.data.MainAgentID() {
		return HistoryPage{}, errorcode.ErrRequestConflict
	}
	return s.page(ctx, head, before, limit, nodes)
}

func (s *Session) page(ctx context.Context, head, before string, limit int, nodes []sessionentry.Node) (HistoryPage, error) {
	end := len(nodes)
	if before != "" {
		end = slices.IndexFunc(nodes, func(node sessionentry.Node) bool { return node.ID == before })
		if end < 0 {
			return HistoryPage{}, errorcode.ErrRequestConflict
		}
	}
	start := max(0, end-limit)
	ids := make([]string, 0, end-start)
	for _, node := range nodes[start:end] {
		ids = append(ids, node.ID)
	}
	entries, err := s.readEntries(ctx, ids)
	if err != nil {
		return HistoryPage{}, err
	}
	if len(entries) != len(ids) {
		return HistoryPage{}, errors.New("history page contains an entry without a complete record")
	}
	page := HistoryPage{HeadEntryID: head, Entries: sessionentry.Values(entries), HasMore: start > 0}
	if page.HasMore {
		page.NextCursor = nodes[start].ID
	}
	return page, nil
}

func (s *Session) AgentPage(ctx context.Context, branchID identity.BranchID, agentID identity.AgentID, head, before string, limit int) (HistoryPage, error) {
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	s.mu.RLock()
	branch := s.data.Branch(branchID)
	if branch == nil {
		s.mu.RUnlock()
		return HistoryPage{}, errorcode.ErrNotFound
	}
	branchHead := branch.HeadEntryID
	current := branchHead
	if agentID != s.data.MainAgentID() {
		history, found := s.data.AgentHistories[agentID]
		if !found {
			s.mu.RUnlock()
			return HistoryPage{}, errorcode.ErrNotFound
		}
		current = history.HeadEntryID
	}
	runs := slices.Clone(s.data.Runs)
	s.mu.RUnlock()
	if agentID != s.data.MainAgentID() {
		nodes, err := s.pathNodes(ctx, branchHead)
		if err != nil {
			return HistoryPage{}, err
		}
		visible := visibleRuns(runs, branchID, nodes)
		if !slices.ContainsFunc(visible, func(run agentrun.HistorySummary) bool { return run.AgentID == agentID }) {
			return HistoryPage{}, errorcode.ErrNotFound
		}
	}
	if head == "" {
		head = current
	}
	nodes, err := s.pathNodes(ctx, current)
	if err != nil {
		return HistoryPage{}, err
	}
	if head != "" {
		// 固定 Head 必须属于当前主路径或被选择子代理的独立路径。
		index := slices.IndexFunc(nodes, func(node sessionentry.Node) bool { return node.ID == head })
		if index < 0 {
			return HistoryPage{}, errorcode.ErrRequestConflict
		}
		nodes = nodes[:index+1]
	}
	return s.page(ctx, head, before, limit, nodes)
}
