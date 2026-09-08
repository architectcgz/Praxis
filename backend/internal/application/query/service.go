// Package query provides application read models over durable state.
package query

import (
	"context"
	"errors"
	"fmt"
	"time"

	domainagent "praxis/internal/domain/agent"
	domaincontext "praxis/internal/domain/context"
	domainexecution "praxis/internal/domain/execution"
	domainfoundation "praxis/internal/domain/foundation"
	domainproject "praxis/internal/domain/project"
	domainsession "praxis/internal/domain/session"
	domainworkflow "praxis/internal/domain/workflow"
	domainworkspace "praxis/internal/domain/workspace"
	"praxis/internal/persistence"
	sessionport "praxis/internal/session"
)

// AgentMessageQuery loads one Agent-owned transcript without exposing its
// storage implementation to callers.
type AgentMessageQuery func(context.Context, domainfoundation.SessionID, domainfoundation.AgentID, int) ([]sessionport.AgentSessionMessage, error)

// Config contains the read-only ports required by Service.
type Config struct {
	Projects   persistence.ProjectRepository
	Workspaces persistence.WorkspaceRepository
	Sessions   persistence.SessionRepository
	Contexts   persistence.SessionContextRepository
	Agents     persistence.AgentRepository
	Executions persistence.AgentExecutionRepository
	Waits      persistence.WaitConditionRepository
	Controls   persistence.AgentControlRequestRepository
	Deliveries persistence.ContextDeliveryRepository
	Events     persistence.EventRepository
	Messages   AgentMessageQuery
}

// Service executes application read use cases for desktop bindings and
// recovery diagnostics. Runtime actors and channels are intentionally excluded.
type Service struct {
	projects   persistence.ProjectRepository
	workspaces persistence.WorkspaceRepository
	sessions   persistence.SessionRepository
	contexts   persistence.SessionContextRepository
	agents     persistence.AgentRepository
	executions persistence.AgentExecutionRepository
	waits      persistence.WaitConditionRepository
	controls   persistence.AgentControlRequestRepository
	deliveries persistence.ContextDeliveryRepository
	events     persistence.EventRepository
	messages   AgentMessageQuery
}

// NewService creates the query service from core-owned query ports.
func NewService(config Config) (*Service, error) {
	for _, required := range []struct {
		name  string
		value any
	}{
		{name: "projects", value: config.Projects},
		{name: "workspaces", value: config.Workspaces},
		{name: "sessions", value: config.Sessions},
		{name: "session contexts", value: config.Contexts},
		{name: "agents", value: config.Agents},
		{name: "executions", value: config.Executions},
		{name: "wait conditions", value: config.Waits},
		{name: "control requests", value: config.Controls},
		{name: "context deliveries", value: config.Deliveries},
		{name: "events", value: config.Events},
		{name: "agent message query", value: config.Messages},
	} {
		if required.value == nil {
			return nil, fmt.Errorf("query %s is required", required.name)
		}
	}
	return &Service{
		projects:   config.Projects,
		workspaces: config.Workspaces,
		sessions:   config.Sessions,
		contexts:   config.Contexts,
		agents:     config.Agents,
		executions: config.Executions,
		waits:      config.Waits,
		controls:   config.Controls,
		deliveries: config.Deliveries,
		events:     config.Events,
		messages:   config.Messages,
	}, nil
}

// AgentView is the durable detail view for a single Agent.
type AgentView struct {
	Agent           domainagent.Agent
	ActiveExecution *domainexecution.AgentExecution
	Executions      []domainexecution.AgentExecution
	Waits           []domainworkflow.WaitCondition
	Deliveries      []domainworkflow.ContextDelivery
	Controls        []domainworkflow.AgentControlRequest
}

// SessionView is the smallest durable detail view required to render a Session
// workspace without granting bindings repository access.
type SessionView struct {
	Session domainsession.Session
	Agents  []domainagent.Agent
}

// ListProjects returns the durable Project catalog.
func (s *Service) ListProjects(ctx context.Context, limit int) ([]domainproject.Project, error) {
	if ctx == nil {
		return nil, errors.New("project catalog context is required")
	}
	lister, ok := s.projects.(persistence.ProjectListRepository)
	if !ok {
		return nil, errors.New("project catalog is unavailable")
	}
	return lister.List(ctx, limit)
}

// ListWorkspaces returns the indexed workspace catalog for one Project.
func (s *Service) ListWorkspaces(ctx context.Context, projectID domainfoundation.ProjectID, limit int) ([]domainworkspace.Workspace, error) {
	if ctx == nil {
		return nil, errors.New("workspace catalog context is required")
	}
	if projectID == "" {
		return nil, invalidQuery("project id is required")
	}
	return s.workspaces.ListByProject(ctx, projectID, limit)
}

// ListSessionsByProject returns the indexed Session catalog for one Project.
func (s *Service) ListSessionsByProject(ctx context.Context, projectID domainfoundation.ProjectID, limit int) ([]domainsession.Session, error) {
	if ctx == nil {
		return nil, errors.New("project session catalog context is required")
	}
	if projectID == "" {
		return nil, invalidQuery("project id is required")
	}
	lister, ok := s.sessions.(persistence.ProjectSessionListRepository)
	if !ok {
		return nil, errors.New("project session catalog is unavailable")
	}
	return lister.ListByProject(ctx, projectID, limit)
}

// ListSessions returns the durable Session catalog for desktop discovery.
func (s *Service) ListSessions(ctx context.Context, limit int) ([]domainsession.Session, error) {
	if ctx == nil {
		return nil, errors.New("session catalog context is required")
	}
	lister, ok := s.sessions.(persistence.SessionListRepository)
	if !ok {
		return nil, errors.New("session catalog is unavailable")
	}
	return lister.List(ctx, limit)
}

// GetAgentView returns the complete durable detail view for one Agent.
func (s *Service) GetAgentView(ctx context.Context, agentID domainfoundation.AgentID, limit int) (AgentView, error) {
	if ctx == nil {
		return AgentView{}, errors.New("agent query context is required")
	}
	if agentID == "" {
		return AgentView{}, invalidQuery("agent id is required")
	}
	agent, err := s.agents.Get(ctx, agentID)
	if err != nil {
		return AgentView{}, err
	}
	active, err := s.executions.GetActiveByAgent(ctx, agentID)
	if errors.Is(err, domainfoundation.ErrNotFound) {
		active = domainexecution.AgentExecution{}
	} else if err != nil {
		return AgentView{}, fmt.Errorf("load active agent execution: %w", err)
	}
	executions, err := s.executions.ListByAgent(ctx, agentID, limit)
	if err != nil {
		return AgentView{}, err
	}
	waits, err := s.waits.ListUnresolvedByAgent(ctx, agentID, limit)
	if err != nil {
		return AgentView{}, err
	}
	deliveries, err := s.deliveries.ListPendingByTarget(ctx, agentID, limit)
	if err != nil {
		return AgentView{}, err
	}
	controls, err := s.controls.ListOpenByAgent(ctx, agentID, limit)
	if err != nil {
		return AgentView{}, err
	}
	view := AgentView{
		Agent:      agent,
		Executions: executions,
		Waits:      waits,
		Deliveries: deliveries,
		Controls:   controls,
	}
	if active.ID != "" {
		activeCopy := active
		view.ActiveExecution = &activeCopy
	}
	return view, nil
}

// GetSessionView returns the Session and its indexed Agent catalog.
func (s *Service) GetSessionView(ctx context.Context, sessionID domainfoundation.SessionID, limit int) (SessionView, error) {
	if ctx == nil {
		return SessionView{}, errors.New("session query context is required")
	}
	if sessionID == "" {
		return SessionView{}, invalidQuery("session id is required")
	}
	session, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		return SessionView{}, err
	}
	agents, err := s.agents.ListBySession(ctx, sessionID, limit)
	if err != nil {
		return SessionView{}, fmt.Errorf("list agents in session %s: %w", sessionID, err)
	}
	return SessionView{Session: session, Agents: agents}, nil
}

// ListAgentMessages reads an Agent transcript through the injected core port.
func (s *Service) ListAgentMessages(ctx context.Context, agentID domainfoundation.AgentID, limit int) ([]sessionport.AgentSessionMessage, error) {
	if ctx == nil {
		return nil, errors.New("agent message context is required")
	}
	if agentID == "" {
		return nil, invalidQuery("agent id is required")
	}
	agent, err := s.agents.Get(ctx, agentID)
	if err != nil {
		return nil, err
	}
	return s.messages(ctx, agent.SessionID, agent.ID, limit)
}

// ListSessionEvents returns durable audit events for one Session.
func (s *Service) ListSessionEvents(ctx context.Context, sessionID domainfoundation.SessionID, after time.Time, limit int) ([]domainfoundation.DomainEvent, error) {
	if ctx == nil || sessionID == "" {
		return nil, invalidQuery("session event query is invalid")
	}
	query, ok := s.events.(persistence.EventQueryRepository)
	if !ok {
		return nil, errors.New("event query is unavailable")
	}
	return query.ListBySession(ctx, sessionID, after, limit)
}

// ListSessionContext returns immutable SessionContext entries after a revision.
func (s *Service) ListSessionContext(ctx context.Context, sessionID domainfoundation.SessionID, afterRevision uint64, limit int) ([]domaincontext.SessionContextEntry, error) {
	if ctx == nil || sessionID == "" {
		return nil, invalidQuery("session context query is invalid")
	}
	if _, err := s.sessions.Get(ctx, sessionID); err != nil {
		return nil, err
	}
	return s.contexts.List(ctx, sessionID, afterRevision, limit)
}

// ListAgentEvents returns durable audit events for one Agent.
func (s *Service) ListAgentEvents(ctx context.Context, agentID domainfoundation.AgentID, after time.Time, limit int) ([]domainfoundation.DomainEvent, error) {
	if ctx == nil || agentID == "" {
		return nil, invalidQuery("agent event query is invalid")
	}
	query, ok := s.events.(persistence.EventQueryRepository)
	if !ok {
		return nil, errors.New("event query is unavailable")
	}
	return query.ListByAgent(ctx, agentID, after, limit)
}

// ListExecutionEvents returns durable audit events for one execution.
func (s *Service) ListExecutionEvents(ctx context.Context, executionID domainfoundation.AgentExecutionID, after time.Time, limit int) ([]domainfoundation.DomainEvent, error) {
	if ctx == nil || executionID == "" {
		return nil, invalidQuery("execution event query is invalid")
	}
	query, ok := s.events.(persistence.EventQueryRepository)
	if !ok {
		return nil, errors.New("event query is unavailable")
	}
	return query.ListByExecution(ctx, executionID, after, limit)
}

func invalidQuery(message string) error {
	return &InvalidQueryError{message: message}
}

// InvalidQueryError reports an invalid query request without exposing a
// repository or storage implementation error to the desktop binding.
type InvalidQueryError struct {
	message string
}

func (e *InvalidQueryError) Error() string {
	return e.message
}
