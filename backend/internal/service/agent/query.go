// Package agent 提供执行所属 Agent 与主会话入口的只读展示查询。
package agent

import (
	"cmp"
	"context"
	"fmt"
	agentmodel "praxis/internal/core/agent"
	agentrun "praxis/internal/core/agent/run"
	sessionmodel "praxis/internal/core/session"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/repository"
	"slices"
	"time"
)

type Dependencies struct {
	Entries     sessionmodel.EntryReader
	ActiveRun   func(identity.AgentID, identity.BranchID) *agentrun.Run
	SessionRuns func(identity.SessionID, identity.BranchID) []*agentrun.Run
	RunRecords  sessionmodel.RunRecords
	Sessions    repository.Session
	Definitions func(context.Context, identity.SessionID, identity.AgentDefinitionID) (agentmodel.AgentDefinition, error)
}

type Service struct {
	entries     sessionmodel.EntryReader
	activeRun   func(identity.AgentID, identity.BranchID) *agentrun.Run
	sessionRuns func(identity.SessionID, identity.BranchID) []*agentrun.Run
	runRecords  sessionmodel.RunRecords
	sessions    repository.Session
	definitions func(context.Context, identity.SessionID, identity.AgentDefinitionID) (agentmodel.AgentDefinition, error)
}

// AgentView 是展示投影；没有活跃 Run 时不需要对应的 Agent 实例。
type AgentView struct {
	ID           identity.AgentID
	SessionID    identity.SessionID
	BranchID     identity.BranchID
	DefinitionID identity.AgentDefinitionID
	Kind         agentmodel.AgentKind
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Name         string                `json:"name"`
	State        agentmodel.AgentState `json:"state"`
	CurrentRunID identity.RunID        `json:"currentRunId"`
	CurrentRun   *agentrun.RunRecord   `json:"currentRun"`
	LatestRun    *agentrun.RunRecord   `json:"latestRun"`
}

func NewService(deps Dependencies) *Service {
	return &Service{
		entries:     deps.Entries,
		activeRun:   deps.ActiveRun,
		sessionRuns: deps.SessionRuns,
		runRecords:  deps.RunRecords,
		definitions: deps.Definitions,
		sessions:    deps.Sessions,
	}
}

// GetAgentView 优先读取活跃 Run；主、子执行结束后从持久化历史派生展示。
func (s *Service) GetAgentView(ctx context.Context, sessionID identity.SessionID, agentID identity.AgentID, branchID identity.BranchID) (AgentView, error) {
	if run := s.activeRun(agentID, branchID); run != nil {
		if run.Snapshot().SessionID != sessionID {
			return AgentView{}, errorcode.ErrRequestConflict
		}
		return s.describeRun(ctx, run)
	}
	latest, err := s.runRecords.ListAgentRuns(ctx, sessionID, agentID, branchID, 1)
	if err != nil {
		return AgentView{}, err
	}
	main := agentID == identity.AgentID(sessionID)
	if !main && len(latest) == 0 {
		return AgentView{}, errorcode.ErrNotFound
	}
	session, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		return AgentView{}, err
	}
	definitionID := agentmodel.DefinitionPrimary
	kind := agentmodel.KindMain
	createdAt := session.CreatedAt
	name := ""
	if !main {
		definitionID = latest[0].Config.AgentDefinitionID
		kind = agentmodel.KindSubagent
		createdAt = latest[0].CreatedAt
		name = latest[0].AgentName
	} else {
		definition, err := s.definitions(ctx, sessionID, definitionID)
		if err != nil {
			return AgentView{}, err
		}
		name = definition.Name
	}
	view := AgentView{
		ID:           agentID,
		SessionID:    session.ID,
		BranchID:     branchID,
		DefinitionID: definitionID,
		Kind:         kind,
		CreatedAt:    createdAt,
		UpdatedAt:    session.UpdatedAt,
		Name:         name,
		State:        agentmodel.AgentIdle,
	}
	if len(latest) > 0 {
		values, err := s.entries.ListAgentEntries(ctx, session.ID, agentID, branchID)
		if err != nil {
			return AgentView{}, err
		}
		entries := make([]*sessionentry.ContextEntry, len(values))
		for index := range values {
			entries[index] = &values[index]
		}
		latest[0].Progress = latest[0].Progress.WithEntries(latest[0].ID.String(), entries)
		view.LatestRun = &latest[0]
		view.State = latest[0].AgentState()
		view.UpdatedAt = latest[0].EndedAt
	}
	return view, nil
}

func (s *Service) ListSessionAgents(ctx context.Context, sessionID identity.SessionID, branchID identity.BranchID, limit int) ([]AgentView, error) {
	main, err := s.GetAgentView(ctx, sessionID, identity.AgentID(sessionID), branchID)
	if err != nil {
		return nil, err
	}
	values := []AgentView{main}
	seen := map[identity.AgentID]bool{main.ID: true}
	for _, run := range s.sessionRuns(sessionID, branchID) {
		if run.Main() {
			continue
		}
		view, err := s.describeRun(ctx, run)
		if err != nil {
			return nil, err
		}
		values = append(values, view)
		seen[view.ID] = true
	}
	runs, err := s.runRecords.ListSessionRuns(ctx, sessionID, branchID)
	if err != nil {
		return nil, err
	}
	for _, record := range runs {
		if seen[record.AgentID] {
			continue
		}
		view, err := s.GetAgentView(ctx, sessionID, record.AgentID, branchID)
		if err != nil {
			return nil, err
		}
		values = append(values, view)
		seen[view.ID] = true
	}
	slices.SortFunc(values[1:], func(a, b AgentView) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	})
	if limit > 0 {
		values = values[:min(limit, len(values))]
	}
	return values, nil
}

func (s *Service) describeRun(ctx context.Context, run *agentrun.Run) (AgentView, error) {
	value := run.Agent
	definition, err := s.definitions(ctx, value.SessionID, value.DefinitionID)
	if err != nil {
		return AgentView{}, fmt.Errorf("load agent definition for display: %w", err)
	}
	state, runID, updatedAt := value.State()
	record := run.Snapshot()
	view := AgentView{
		ID:           value.ID,
		SessionID:    value.SessionID,
		BranchID:     record.BranchID,
		DefinitionID: value.DefinitionID,
		Kind:         value.Kind,
		CreatedAt:    value.CreatedAt,
		UpdatedAt:    updatedAt,
		Name:         definition.Name,
		State:        state,
		CurrentRunID: runID,
		LatestRun:    &record,
	}
	if runID != "" {
		view.CurrentRun = &record
	}
	return view, nil
}
