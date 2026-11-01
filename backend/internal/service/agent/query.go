// Package agent 提供执行所属 Agent 与主会话入口的只读展示查询。
package agent

import (
	"context"
	"fmt"
	agentmodel "praxis/internal/core/agent"
	agentexecution "praxis/internal/core/agent/execution"
	sessionmodel "praxis/internal/core/session"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/repository"
	"time"
)

type Dependencies struct {
	Entries          sessionmodel.EntryReader
	ActiveRun        func(identity.AgentID, identity.BranchID) *agentexecution.Run
	SessionRuns      func(identity.SessionID, identity.BranchID) []*agentexecution.Run
	ExecutionRecords sessionmodel.ExecutionRecords
	Sessions         repository.Session
	Definitions      func(context.Context, identity.SessionID, identity.AgentDefinitionID) (agentmodel.AgentDefinition, error)
}

type Service struct {
	entries          sessionmodel.EntryReader
	activeRun        func(identity.AgentID, identity.BranchID) *agentexecution.Run
	sessionRuns      func(identity.SessionID, identity.BranchID) []*agentexecution.Run
	executionRecords sessionmodel.ExecutionRecords
	sessions         repository.Session
	definitions      func(context.Context, identity.SessionID, identity.AgentDefinitionID) (agentmodel.AgentDefinition, error)
}

// AgentView 是展示投影；没有活跃 Execution 时不需要对应的 Agent 实例。
type AgentView struct {
	ID                 identity.AgentID
	SessionID          identity.SessionID
	BranchID           identity.BranchID
	DefinitionID       identity.AgentDefinitionID
	Kind               agentmodel.AgentKind
	CreatedAt          time.Time
	UpdatedAt          time.Time
	Name               string                          `json:"name"`
	State              agentmodel.AgentState           `json:"state"`
	CurrentExecutionID identity.AgentExecutionID       `json:"currentExecutionId"`
	CurrentExecution   *agentexecution.ExecutionRecord `json:"currentExecution"`
	LatestExecution    *agentexecution.ExecutionRecord `json:"latestExecution"`
}

func NewService(deps Dependencies) *Service {
	return &Service{
		entries:          deps.Entries,
		activeRun:        deps.ActiveRun,
		sessionRuns:      deps.SessionRuns,
		executionRecords: deps.ExecutionRecords,
		definitions:      deps.Definitions,
		sessions:         deps.Sessions,
	}
}

// GetAgentView 优先读取活跃 Run；主会话空闲时从元数据和执行历史派生展示，不创建 Agent。
func (s *Service) GetAgentView(ctx context.Context, sessionID identity.SessionID, agentID identity.AgentID, branchID identity.BranchID) (AgentView, error) {
	if run := s.activeRun(agentID, branchID); run != nil {
		if run.Snapshot().SessionID != sessionID {
			return AgentView{}, errorcode.ErrRequestConflict
		}
		return s.describeRun(ctx, run)
	}
	if agentID != identity.AgentID(sessionID) {
		return AgentView{}, errorcode.ErrNotFound
	}
	session, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		return AgentView{}, err
	}
	definition, err := s.definitions(ctx, sessionID, agentmodel.DefinitionPrimary)
	if err != nil {
		return AgentView{}, err
	}
	view := AgentView{
		ID:           session.MainAgentID(),
		SessionID:    session.ID,
		BranchID:     branchID,
		DefinitionID: definition.ID,
		Kind:         agentmodel.KindMain,
		CreatedAt:    session.CreatedAt,
		UpdatedAt:    session.UpdatedAt,
		Name:         definition.Name,
		State:        agentmodel.AgentIdle,
	}
	latest, err := s.executionRecords.ListAgentExecutions(ctx, agentID, branchID, 1)
	if err != nil {
		return AgentView{}, err
	}
	if len(latest) > 0 {
		history, err := s.entries.ListEntries(ctx, session.ID, branchID)
		if err != nil {
			return AgentView{}, err
		}
		entries := make([]*sessionentry.ContextEntry, len(history))
		for index := range history {
			entries[index] = &history[index]
		}
		latest[0].Progress = latest[0].Progress.WithEntries(latest[0].ID.String(), entries)
		view.LatestExecution = &latest[0]
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
	for _, run := range s.sessionRuns(sessionID, branchID) {
		if run.Main() {
			continue
		}
		view, err := s.describeRun(ctx, run)
		if err != nil {
			return nil, err
		}
		values = append(values, view)
	}
	if limit > 0 {
		values = values[:min(limit, len(values))]
	}
	return values, nil
}

func (s *Service) describeRun(ctx context.Context, run *agentexecution.Run) (AgentView, error) {
	value := run.Agent
	definition, err := s.definitions(ctx, value.SessionID, value.DefinitionID)
	if err != nil {
		return AgentView{}, fmt.Errorf("load agent definition for display: %w", err)
	}
	state, executionID, updatedAt := value.State()
	record := run.Snapshot()
	view := AgentView{
		ID:                 value.ID,
		SessionID:          value.SessionID,
		BranchID:           record.BranchID,
		DefinitionID:       value.DefinitionID,
		Kind:               value.Kind,
		CreatedAt:          value.CreatedAt,
		UpdatedAt:          updatedAt,
		Name:               definition.Name,
		State:              state,
		CurrentExecutionID: executionID,
		LatestExecution:    &record,
	}
	if executionID != "" {
		view.CurrentExecution = &record
	}
	return view, nil
}
