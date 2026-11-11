package session

import (
	"cmp"
	"context"
	agentrun "praxis/internal/core/agent/run"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"reflect"
	"slices"
)

func (s *Session) GetRun(ctx context.Context, id identity.RunID) (agentrun.RunRecord, error) {
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	return s.run(ctx, id)
}

func (s *Session) run(ctx context.Context, id identity.RunID) (agentrun.RunRecord, error) {
	if err := s.syncHistory(ctx); err != nil {
		return agentrun.RunRecord{}, err
	}
	s.mu.RLock()
	pending, found := s.data.PendingRuns[id]
	s.mu.RUnlock()
	if found {
		return pending, nil
	}
	return s.log.Run(ctx, id)
}

// SaveRun 将结果、累计值与稳定结束节点放在同一行，完整落盘后释放会话持有的正文。
func (s *Session) SaveRun(ctx context.Context, value agentrun.RunRecord) (*sessionentry.ContextEntry, error) {
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	s.mu.RLock()
	endID := s.data.RunEndEntries[value.ID]
	var prior agentrun.HistorySummary
	if index := s.data.RunIndex(value.ID); index >= 0 {
		prior = s.data.Runs[index]
	}
	s.mu.RUnlock()
	if endID != "" {
		if !reflect.DeepEqual(prior, value.HistorySummary()) {
			return nil, errorcode.ErrRequestConflict
		}
		return s.entry(ctx, endID)
	}
	event := Event{
		Type:      EventRunSaved,
		Key:       value.ID.String(),
		AgentID:   value.AgentID,
		BranchID:  value.BranchID,
		CreatedAt: s.clock.Now(),
		Payload:   value,
	}
	var stored *sessionentry.ContextEntry
	s.mu.Lock()
	s.data.SetRun(value.HistorySummary())
	s.data.PendingRuns[value.ID] = value
	if value.Status == agentrun.RunEnded {
		end := sessionentry.ContextEntry{
			ID:         "run:" + value.ID.String(),
			RunID:      value.ID.String(),
			TurnID:     value.CurrentTurn.ID.String(),
			CreatedAt:  event.CreatedAt,
			AuthorKind: sessionentry.AuthorSystem,
			RunResult: &sessionentry.RunResult{
				Outcome:          string(value.Outcome),
				FailureCode:      string(value.FailureCode),
				FailureMessage:   value.FailureMessage,
				StartedAt:        value.StartedAt,
				EndedAt:          value.EndedAt,
				Usage:            value.Progress.Usage,
				ModelRequests:    value.Progress.ModelRequests,
				Iterations:       value.Progress.Iterations,
				OutputBytes:      value.Progress.OutputBytes,
				ToolCalls:        value.Progress.ToolCalls,
				FinalAssistantID: value.Progress.FinalAssistantID,
				FinalStopReason:  value.Progress.FinalStopReason,
			},
		}
		if value.AgentID == s.data.MainAgentID() {
			end.ParentID = s.data.Branch(value.BranchID).HeadEntryID
		} else {
			end.ParentID = s.data.AgentHistories[value.AgentID].HeadEntryID
		}
		stored = s.data.acceptEntry(value.BranchID, value.AgentID, end)
		event.Type, event.Payload = EventRunEnded, RunEnded{Run: value, Entry: *stored}
	}
	s.mu.Unlock()
	persisted, err := s.log.AppendEvent(ctx, event)
	if persisted {
		s.mu.Lock()
		delete(s.data.PendingRuns, value.ID)
		if stored != nil {
			s.data.releaseEntry(stored.ID)
		}
		s.mu.Unlock()
	}
	return stored, err
}

func (s *Session) FindRunByTask(ctx context.Context, agentID identity.AgentID, taskID identity.TaskID) (agentrun.RunRecord, error) {
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	s.mu.RLock()
	var id identity.RunID
	for _, value := range s.data.Runs {
		if value.AgentID == agentID && value.SourceTaskID == taskID {
			id = value.ID
			break
		}
	}
	s.mu.RUnlock()
	if id == "" {
		return agentrun.RunRecord{}, errorcode.ErrNotFound
	}
	return s.run(ctx, id)
}

func runOrder(a, b agentrun.HistorySummary) int {
	return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
}

func (s *Session) SaveModelRequest(ctx context.Context, value agentrun.RunRecord, request agentrun.ModelRequestRecord) error {
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	s.mu.Lock()
	s.data.SetRun(value.HistorySummary())
	s.data.PendingRuns[value.ID] = value
	s.data.ModelRequests[request.ID] = request
	s.mu.Unlock()
	kind := EventModelRequestFinished
	if request.Status == agentrun.ModelRequestRunning {
		kind = EventModelRequestStarted
	}
	persisted, err := s.log.AppendEvent(ctx, Event{
		Type:      kind,
		Key:       request.ID.String(),
		AgentID:   value.AgentID,
		BranchID:  value.BranchID,
		CreatedAt: s.clock.Now(),
		Payload:   ModelRequestSaved{Request: request, Run: value},
	})
	if persisted {
		s.mu.Lock()
		delete(s.data.ModelRequests, request.ID)
		delete(s.data.PendingRuns, value.ID)
		s.mu.Unlock()
	}
	return err
}

func (s *Session) ListSessionRuns(ctx context.Context, branchID identity.BranchID) ([]agentrun.HistorySummary, error) {
	s.commitMu.Lock()
	s.mu.RLock()
	branch := s.data.Branch(branchID)
	if branch == nil {
		s.mu.RUnlock()
		s.commitMu.Unlock()
		return nil, errorcode.ErrNotFound
	}
	head := branch.HeadEntryID
	runs := slices.Clone(s.data.Runs)
	s.mu.RUnlock()
	nodes, err := s.pathNodes(ctx, head)
	s.commitMu.Unlock()
	if err != nil {
		return nil, err
	}
	return sorted(visibleRuns(runs, branchID, nodes), func(a, b agentrun.HistorySummary) int { return -runOrder(a, b) }), nil
}

// visibleRuns 由实际主路径选出根执行，再一次展开所属子执行，不读取历史正文。
func visibleRuns(runs []agentrun.HistorySummary, branchID identity.BranchID, nodes []sessionentry.Node) []agentrun.HistorySummary {
	wanted := make(map[identity.RunID]bool)
	for _, node := range nodes {
		wanted[identity.RunID(node.RunID)] = true
	}
	children := make(map[identity.RunID][]agentrun.HistorySummary)
	var values []agentrun.HistorySummary
	for _, run := range runs {
		if run.ParentRunID != "" {
			children[run.ParentRunID] = append(children[run.ParentRunID], run)
		} else if wanted[run.ID] || run.BranchID == branchID && run.Active() {
			values = append(values, run)
		}
	}
	for index := 0; index < len(values); index++ {
		values = append(values, children[values[index].ID]...)
	}
	return values
}
