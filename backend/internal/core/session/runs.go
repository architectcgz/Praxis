package session

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	agentrun "praxis/internal/core/agent/run"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"reflect"
)

func (s *Session) GetRun(id identity.RunID) (agentrun.RunRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	index := s.data.RunIndex(id)
	if index < 0 {
		return agentrun.RunRecord{}, errorcode.ErrNotFound
	}
	return cloneRun(s.data.Runs[index]), nil
}

func (s *Session) SaveRun(ctx context.Context, value agentrun.RunRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateRunOwnership(s.data, value); err != nil {
		return err
	}
	value = cloneRun(value)
	// 展示字段从完整 Entry 派生，不持久化重复数据。
	value.Progress.OutputBytes, value.Progress.ToolCalls = 0, 0
	value.Progress.FinalAssistantID, value.Progress.FinalStopReason = "", ""
	s.data.SetRun(value)
	return s.log.AppendEvent(ctx, s.data.ID, Event{
		Type:      EventRunSaved,
		Key:       value.ID.String(),
		AgentID:   value.AgentID,
		CreatedAt: s.clock.Now(),
		Payload:   value,
	})
}

func (s *Session) FindRunByRequest(agentID identity.AgentID, request identity.RequestID) (agentrun.RunRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, value := range s.data.Runs {
		if value.AgentID == agentID && value.RequestID == request {
			return cloneRun(value), nil
		}
	}
	return agentrun.RunRecord{}, errorcode.ErrNotFound
}

func (s *Session) ListAgentRuns(agentID identity.AgentID, branchID identity.BranchID, limit int) []agentrun.RunRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	values := make([]agentrun.RunRecord, 0)
	for _, value := range s.data.Runs {
		if value.AgentID == agentID && value.BranchID == branchID {
			values = append(values, cloneRun(value))
		}
	}
	return capped(sorted(values, func(a, b agentrun.RunRecord) int { return -runOrder(a, b) }), limit)
}

// ValidateRuns 在完整日志加载后确认主、子执行的跨文件归属。
func (s *Data) ValidateRuns() error {
	for _, record := range s.Runs {
		if err := validateRunOwnership(s, record); err != nil {
			return fmt.Errorf("restore execution %s: %w", record.ID, err)
		}
	}
	return nil
}

func validateRunOwnership(owner *Data, value agentrun.RunRecord) error {
	if value.SessionID != owner.ID || owner.Branch(value.BranchID) == nil {
		return errors.New("execution ownership does not match its session")
	}
	var input *sessionentry.ContextEntry
	var found bool
	if value.AgentID == owner.MainAgentID() {
		input, found = owner.Entry(value.InputEntryID)
		if found && !owner.hasAncestor(owner.Branch(value.BranchID).HeadEntryID, input.ID) {
			return errors.New("execution input is outside its branch")
		}
	} else {
		input, found = owner.SubagentEntry(value.AgentID, value.InputEntryID)
		parent := owner.RunIndex(value.ParentRunID)
		if parent < 0 || owner.Runs[parent].BranchID != value.BranchID || value.ParentCallID == "" {
			return errors.New("subagent execution parent ownership mismatch")
		}
	}
	if !found {
		return errors.New("execution input is missing")
	}
	if input.RunID != value.ID.String() || input.RequestID != value.RequestID.String() || input.SourceTaskID != value.SourceTaskID.String() {
		return errors.New("execution input ownership mismatch")
	}
	if index := owner.RunIndex(value.ID); index >= 0 {
		prior := owner.Runs[index]
		if prior.BranchID != value.BranchID || prior.AgentID != value.AgentID || prior.AgentName != value.AgentName || prior.ParentRunID != value.ParentRunID || prior.ParentCallID != value.ParentCallID || prior.RequestID != value.RequestID || prior.InputEntryID != value.InputEntryID || prior.SourceTaskID != value.SourceTaskID || !prior.CreatedAt.Equal(value.CreatedAt) || !reflect.DeepEqual(prior.Config, value.Config) {
			return errors.New("execution identity or frozen configuration changed")
		}
	}
	return nil
}

func cloneRun(value agentrun.RunRecord) agentrun.RunRecord {
	value.Config = value.Config.Clone()
	return value
}

func runOrder(a, b agentrun.RunRecord) int {
	return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
}

func (s *Session) SaveModelRequest(ctx context.Context, value agentrun.RunRecord, request agentrun.ModelRequestRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	value = cloneRun(value)
	request = request.Clone()
	s.data.SetRun(value)
	s.data.SetModelRequest(request)
	kind := EventModelRequestFinished
	if request.Status == agentrun.ModelRequestRunning {
		kind = EventModelRequestStarted
	}
	return s.log.AppendEvent(ctx, s.data.ID, Event{
		Type:      kind,
		Key:       request.TurnID.String(),
		AgentID:   value.AgentID,
		CreatedAt: s.clock.Now(),
		Payload: ModelRequestSaved{
			Request: request,
			Run:     value,
		},
	})
}

func (s *Session) ListSessionRuns(branchID identity.BranchID) []agentrun.RunRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	values := make([]agentrun.RunRecord, 0)
	for _, value := range s.data.Runs {
		if value.BranchID == branchID {
			values = append(values, cloneRun(value))
		}
	}
	return sorted(values, func(a, b agentrun.RunRecord) int { return -runOrder(a, b) })
}
