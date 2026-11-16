package session

import (
	"context"
	"errors"
	"fmt"
	sessionentry "praxis/internal/core/entry"
	"praxis/internal/core/entry/role"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"slices"
)

func (s *Session) AppendEntry(ctx context.Context, value sessionentry.ContextEntry) (*sessionentry.ContextEntry, bool, error) {
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	return s.appendEntry(ctx, s.data.MainAgentID(), value)
}

// appendEntry 在唯一接纳边界同步保存，写入失败交给所属执行明确停止。
func (s *Session) appendEntry(ctx context.Context, agentID identity.AgentID, value sessionentry.ContextEntry) (*sessionentry.ContextEntry, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if prior, err := s.entry(ctx, value.ID); err == nil {
		return prior, false, nil
	} else if !errors.Is(err, errorcode.ErrNotFound) {
		return nil, false, err
	}
	s.mu.Lock()
	if s.data.RunEndEntries[identity.RunID(value.RunID)] != "" {
		s.mu.Unlock()
		return nil, false, errorcode.ErrInvalidTransition
	}
	if agentID != s.data.MainAgentID() {
		if _, ok := s.data.AgentHistories[agentID]; !ok {
			s.mu.Unlock()
			return nil, false, errorcode.ErrNotFound
		}
	}
	if value.CreatedAt.IsZero() {
		value.CreatedAt = s.clock.Now()
	}
	stored := s.data.acceptEntry(agentID, value)
	s.mu.Unlock()
	persisted, err := s.log.AppendEntry(ctx, agentID, *stored)
	if persisted {
		s.mu.Lock()
		s.data.releaseEntry(stored.ID)
		if stored.IsRun() {
			delete(s.data.RunStates, identity.RunID(stored.RunID))
		}
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
		var reference string
		switch v := entry.Payload.(type) {
		case sessionentry.SubagentReportAccepted:
			reference = v.ReportID
		case sessionentry.SubagentInstructionAccepted:
			reference = v.MessageID
		}
		if reference != "" {
			sources, err := s.log.Entries(ctx, []string{reference})
			if err != nil {
				return nil, err
			}
			if len(sources) != 1 {
				return nil, fmt.Errorf("accepted message source %s is missing", reference)
			}
			text := ""
			switch v := sources[0].Payload.(type) {
			case sessionentry.SubagentReportReceived:
				status := string(v.Report.Outcome)
				if status == "" {
					status = "progress"
				}
				text = fmt.Sprintf("[Subagent report %s] agent=%s run=%s outcome=%s final=%t partial=%t failure=%s\n%s", v.Report.ReportID, v.Report.AgentID, v.Report.RunID, status, v.Report.Final, v.Report.Partial, v.Report.FailureCode, v.Report.Text)
			case sessionentry.SubagentInstructionReceived:
				text = v.Text
			}
			view := *entry
			view.Role = role.User
			view.AuthorKind = sessionentry.AuthorAgent
			view.Content = []sessionentry.ContentBlock{sessionentry.TextBlock{Text: text}}
			entry = &view
		}
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
	return s.log.PathNodes(ctx, head)
}

// Entries 选择最近摘要及其后的完整历史，不受界面分页范围限制。
func (s *Session) Entries(ctx context.Context) ([]*sessionentry.ContextEntry, error) {
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	s.mu.RLock()
	head := s.data.HeadEntryID
	s.mu.RUnlock()
	return s.contextEntries(ctx, head, "")
}

func (s *Session) contextEntries(ctx context.Context, head, inputMessageID string) ([]*sessionentry.ContextEntry, error) {
	nodes, err := s.pathNodes(ctx, head)
	if err != nil {
		return nil, err
	}

	start := 0
	currentRun := ""
	for _, node := range nodes {
		if node.MessageID == inputMessageID && inputMessageID != "" {
			currentRun = node.RunID
		}
	}
	latestTurn := ""
	for i := len(nodes) - 1; i >= 0; i-- {
		if nodes[i].HasToolCalls && nodes[i].RunID == currentRun {
			latestTurn = nodes[i].TurnID
			break
		}
	}
	for i := len(nodes) - 1; i >= 0; i-- {
		if nodes[i].Summary {
			start = i
			break
		}
	}
	var ids []string
	for i, node := range nodes {
		keep := i >= start || inputMessageID != "" && node.MessageID == inputMessageID || latestTurn != "" && node.RunID == currentRun && node.TurnID == latestTurn
		if keep && !node.Run && !node.Metadata {
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
	visible := make([]sessionentry.Node, 0, len(nodes))
	for _, node := range nodes {
		if node.Visible {
			visible = append(visible, node)
		}
	}
	nodes = visible
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

func (s *Session) AgentPage(ctx context.Context, agentID identity.AgentID, head, before string, limit int) (HistoryPage, error) {
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	s.mu.RLock()
	current := s.data.HeadEntryID
	if agentID != s.data.MainAgentID() {
		history, found := s.data.AgentHistories[agentID]
		if !found {
			s.mu.RUnlock()
			return HistoryPage{}, errorcode.ErrNotFound
		}
		current = history.HeadEntryID
	}
	s.mu.RUnlock()
	nodes, err := s.pathNodes(ctx, current)
	if err != nil {
		return HistoryPage{}, err
	}
	if head == "" {
		head = current
	}
	if head != "" {
		index := slices.IndexFunc(nodes, func(node sessionentry.Node) bool { return node.ID == head })
		if index < 0 {
			return HistoryPage{}, errorcode.ErrRequestConflict
		}
		nodes = nodes[:index+1]
	}
	return s.page(ctx, head, before, limit, nodes)
}

func (s *Session) ModelEntries(ctx context.Context, agentID identity.AgentID, inputMessageID string) ([]*sessionentry.ContextEntry, error) {
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	s.mu.RLock()
	head := s.data.HeadEntryID
	if agentID != s.data.MainAgentID() {
		head = s.data.AgentHistories[agentID].HeadEntryID
	}
	s.mu.RUnlock()
	return s.contextEntries(ctx, head, inputMessageID)
}
