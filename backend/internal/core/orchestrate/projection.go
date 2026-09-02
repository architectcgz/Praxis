package orchestrate

import (
	"context"
	"errors"
	"fmt"
	domaincontext "praxis/internal/core/domain/context"
	domainexecution "praxis/internal/core/domain/execution"
	domainfoundation "praxis/internal/core/domain/foundation"
	domainproject "praxis/internal/core/domain/project"
	domainsession "praxis/internal/core/domain/session"
	domainworkflow "praxis/internal/core/domain/workflow"
	domainworkspace "praxis/internal/core/domain/workspace"
	"time"

	domainagent "praxis/internal/core/domain/agent"
	"praxis/internal/core/persistence"
	coresession "praxis/internal/core/session"
)

// AgentProjection is a read-only core view for bindings and recovery
// diagnostics. It contains durable facts only; runtime actors and channels are
// intentionally absent.
type AgentProjection struct {
	Agent           domainagent.Agent
	ActiveExecution *domainexecution.AgentExecution
	Executions      []domainexecution.AgentExecution
	Waits           []domainworkflow.WaitCondition
	Deliveries      []domainworkflow.ContextDelivery
	Controls        []domainworkflow.AgentControlRequest
}

// SessionProjection is the smallest state tree needed to render a Session
// workspace without giving the UI direct repository access.
type SessionProjection struct {
	Session domainsession.Session
	Agents  []domainagent.Agent
}

func (o *AgentOrchestrator) ListProjects(ctx context.Context, limit int) ([]domainproject.Project, error) {
	if ctx == nil {
		return nil, errors.New("project catalog context is required")
	}
	lister, ok := o.projects.(persistence.ProjectListRepository)
	if !ok {
		return nil, errors.New("project catalog is unavailable")
	}
	return lister.List(ctx, limit)
}

// ListWorkspaces returns the indexed workspace catalog for a Project.
func (o *AgentOrchestrator) ListWorkspaces(ctx context.Context, projectID domainfoundation.ProjectID, limit int) ([]domainworkspace.Workspace, error) {
	if ctx == nil {
		return nil, errors.New("workspace catalog context is required")
	}
	if projectID == "" {
		return nil, commandError(CommandErrorInvalidRequest)
	}
	return o.workspaces.ListByProject(ctx, projectID, limit)
}

// ListSessionsByProject returns the indexed Session catalog for a Project.
func (o *AgentOrchestrator) ListSessionsByProject(ctx context.Context, projectID domainfoundation.ProjectID, limit int) ([]domainsession.Session, error) {
	if ctx == nil {
		return nil, errors.New("project session catalog context is required")
	}
	if projectID == "" {
		return nil, commandError(CommandErrorInvalidRequest)
	}
	lister, ok := o.sessions.(persistence.ProjectSessionListRepository)
	if !ok {
		return nil, errors.New("project session catalog is unavailable")
	}
	return lister.ListByProject(ctx, projectID, limit)
}

// ListSessions returns the durable session catalog for the desktop shell. The
// shell needs discovery before a user has selected a session, so this query is
// intentionally separate from ProjectSession's full state tree.
func (o *AgentOrchestrator) ListSessions(
	ctx context.Context,
	limit int,
) ([]domainsession.Session, error) {
	if ctx == nil {
		return nil, errors.New("session catalog context is required")
	}
	lister, ok := o.sessions.(persistence.SessionListRepository)
	if !ok {
		return nil, errors.New("session catalog is unavailable")
	}
	return lister.List(ctx, limit)
}

func (o *AgentOrchestrator) ProjectAgent(
	ctx context.Context,
	agentID domainfoundation.AgentID,
	limit int,
) (AgentProjection, error) {
	if ctx == nil {
		return AgentProjection{}, errors.New("agent projection context is required")
	}
	if agentID == "" {
		return AgentProjection{}, commandError(CommandErrorInvalidRequest)
	}
	agent, err := o.agents.Get(ctx, agentID)
	if err != nil {
		return AgentProjection{}, err
	}
	active, err := o.executions.GetActiveByAgent(ctx, agentID)
	if errors.Is(err, domainfoundation.ErrNotFound) {
		active = domainexecution.AgentExecution{}
	} else if err != nil {
		return AgentProjection{}, fmt.Errorf("load active agent execution: %w", err)
	}
	executions, err := o.executions.ListByAgent(ctx, agentID, limit)
	if err != nil {
		return AgentProjection{}, err
	}
	waits, err := o.waits.ListUnresolvedByAgent(ctx, agentID, limit)
	if err != nil {
		return AgentProjection{}, err
	}
	deliveries, err := o.deliveries.ListPendingByTarget(ctx, agentID, limit)
	if err != nil {
		return AgentProjection{}, err
	}
	controls, err := o.controls.ListOpenByAgent(ctx, agentID, limit)
	if err != nil {
		return AgentProjection{}, err
	}
	projection := AgentProjection{
		Agent:      agent,
		Executions: executions,
		Waits:      waits,
		Deliveries: deliveries,
		Controls:   controls,
	}
	if active.ID != "" {
		activeCopy := active
		projection.ActiveExecution = &activeCopy
	}
	return projection, nil
}

func (o *AgentOrchestrator) ProjectSession(
	ctx context.Context,
	sessionID domainfoundation.SessionID,
	limit int,
) (SessionProjection, error) {
	if ctx == nil {
		return SessionProjection{}, errors.New("session projection context is required")
	}
	if sessionID == "" {
		return SessionProjection{}, commandError(CommandErrorInvalidRequest)
	}
	session, err := o.sessions.Get(ctx, sessionID)
	if err != nil {
		return SessionProjection{}, err
	}
	agents, err := o.agents.ListBySession(ctx, sessionID, limit)
	if err != nil {
		return SessionProjection{}, fmt.Errorf("list agents in session %s: %w", sessionID, err)
	}
	return SessionProjection{Session: session, Agents: agents}, nil
}

// ListAgentMessages projects an Agent-owned transcript through the injected
// core port. Bindings never open JSONL files directly.
func (o *AgentOrchestrator) ListAgentMessages(ctx context.Context, agentID domainfoundation.AgentID, limit int) ([]coresession.AgentSessionMessage, error) {
	if ctx == nil {
		return nil, errors.New("agent message context is required")
	}
	if agentID == "" || o.messages == nil {
		return nil, commandError(CommandErrorInvalidRequest)
	}
	agent, err := o.agents.Get(ctx, agentID)
	if err != nil {
		return nil, err
	}
	return o.messages(ctx, agent.SessionID, agent.ID, limit)
}

func (o *AgentOrchestrator) ListSessionEvents(ctx context.Context, sessionID domainfoundation.SessionID, after time.Time, limit int) ([]domainfoundation.DomainEvent, error) {
	if ctx == nil || sessionID == "" || o.events == nil {
		return nil, commandError(CommandErrorInvalidRequest)
	}
	query, ok := o.events.(persistence.EventQueryRepository)
	if !ok {
		return nil, errors.New("event query is unavailable")
	}
	return query.ListBySession(ctx, sessionID, after, limit)
}

func (o *AgentOrchestrator) ListSessionContext(ctx context.Context, sessionID domainfoundation.SessionID, afterRevision uint64, limit int) ([]domaincontext.SessionContextEntry, error) {
	if ctx == nil || sessionID == "" || o.contexts == nil {
		return nil, commandError(CommandErrorInvalidRequest)
	}
	if _, err := o.sessions.Get(ctx, sessionID); err != nil {
		return nil, err
	}
	return o.contexts.List(ctx, sessionID, afterRevision, limit)
}

func (o *AgentOrchestrator) ListAgentEvents(ctx context.Context, agentID domainfoundation.AgentID, after time.Time, limit int) ([]domainfoundation.DomainEvent, error) {
	if ctx == nil || agentID == "" || o.events == nil {
		return nil, commandError(CommandErrorInvalidRequest)
	}
	query, ok := o.events.(persistence.EventQueryRepository)
	if !ok {
		return nil, errors.New("event query is unavailable")
	}
	return query.ListByAgent(ctx, agentID, after, limit)
}

func (o *AgentOrchestrator) ListExecutionEvents(ctx context.Context, executionID domainfoundation.AgentExecutionID, after time.Time, limit int) ([]domainfoundation.DomainEvent, error) {
	if ctx == nil || executionID == "" || o.events == nil {
		return nil, commandError(CommandErrorInvalidRequest)
	}
	query, ok := o.events.(persistence.EventQueryRepository)
	if !ok {
		return nil, errors.New("event query is unavailable")
	}
	return query.ListByExecution(ctx, executionID, after, limit)
}
