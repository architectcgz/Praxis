// Package agent 提供执行所属 Agent 与主会话入口的只读展示查询。
package agent

import (
	"context"
	"fmt"
	agentmodel "praxis/internal/core/agent"
	agentexecution "praxis/internal/core/agent/execution"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
	"praxis/internal/repository"
	"time"
)

type Dependencies struct {
	History          sessionmodel.History
	ActiveRun        func(identity.AgentID) *agentexecution.Run
	SessionRuns      func(identity.SessionID) []*agentexecution.Run
	ExecutionRecords repository.AgentExecution
	Sessions         repository.Session
	Definitions      func(identity.AgentDefinitionID) (agentmodel.AgentDefinition, error)
}

type Service struct {
	history          sessionmodel.History
	activeRun        func(identity.AgentID) *agentexecution.Run
	sessionRuns      func(identity.SessionID) []*agentexecution.Run
	executionRecords repository.AgentExecution
	sessions         repository.Session
	definitions      func(identity.AgentDefinitionID) (agentmodel.AgentDefinition, error)
}

// AgentView 是展示投影；没有活跃 Execution 时不需要对应的 Agent 实例。
type AgentView struct {
	ID                 identity.AgentID
	SessionID          identity.SessionID
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
		history:          deps.History,
		activeRun:        deps.ActiveRun,
		sessionRuns:      deps.SessionRuns,
		executionRecords: deps.ExecutionRecords,
		definitions:      deps.Definitions,
		sessions:         deps.Sessions,
	}
}

// GetAgentView 优先读取活跃 Run；主会话空闲时从元数据和执行历史派生展示，不创建 Agent。
func (s *Service) GetAgentView(ctx context.Context, agentID identity.AgentID) (AgentView, error) {
	if run := s.activeRun(agentID); run != nil {
		return s.describeRun(run)
	}
	session, err := s.sessions.Get(ctx, identity.SessionID(agentID))
	if err != nil {
		return AgentView{}, err
	}
	if _, err := s.history.ContextEntries(ctx, session.ID); err != nil {
		return AgentView{}, err
	}
	definition, err := s.definitions(agentmodel.DefinitionPrimary)
	if err != nil {
		return AgentView{}, err
	}
	view := AgentView{
		ID:           session.MainAgentID(),
		SessionID:    session.ID,
		DefinitionID: definition.ID,
		Kind:         agentmodel.KindMain,
		CreatedAt:    session.CreatedAt,
		UpdatedAt:    session.UpdatedAt,
		Name:         definition.Name,
		State:        agentmodel.AgentIdle,
	}
	latest, err := s.executionRecords.ListByAgent(ctx, agentID, 1)
	if err != nil {
		return AgentView{}, err
	}
	if len(latest) > 0 {
		page, err := s.history.ListEntries(ctx, session.ID)
		if err != nil {
			return AgentView{}, err
		}
		latest[0].Progress = latest[0].Progress.WithEntries(latest[0].ID.String(), page.Entries)
		view.LatestExecution = &latest[0]
		view.State = latest[0].AgentState()
		view.UpdatedAt = latest[0].EndedAt
	}
	return view, nil
}

func (s *Service) ListSessionAgents(ctx context.Context, sessionID identity.SessionID, limit int) ([]AgentView, error) {
	main, err := s.GetAgentView(ctx, identity.AgentID(sessionID))
	if err != nil {
		return nil, err
	}
	values := []AgentView{main}
	for _, run := range s.sessionRuns(sessionID) {
		if run.Main() {
			continue
		}
		view, err := s.describeRun(run)
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

func (s *Service) describeRun(run *agentexecution.Run) (AgentView, error) {
	value := run.Agent
	definition, err := s.definitions(value.DefinitionID)
	if err != nil {
		return AgentView{}, fmt.Errorf("load agent definition for display: %w", err)
	}
	state, executionID, updatedAt := value.State()
	record := run.Snapshot()
	view := AgentView{
		ID:                 value.ID,
		SessionID:          value.SessionID,
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
