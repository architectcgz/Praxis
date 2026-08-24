package orchestrate

import (
	"context"
	"errors"
	"fmt"

	"praxis/internal/core/domain"
	"praxis/internal/core/persistence"
)

// AgentProjection is a read-only core view for bindings and recovery
// diagnostics. It contains durable facts only; runtime actors and channels are
// intentionally absent.
type AgentProjection struct {
	Agent           domain.Agent
	ActiveExecution *domain.AgentExecution
	Executions      []domain.AgentExecution
	Waits           []domain.WaitCondition
	Deliveries      []domain.ContextDelivery
	Controls        []domain.AgentControlRequest
}

// SessionProjection is the smallest state tree needed to render a Session
// workspace without giving the UI direct repository access.
type SessionProjection struct {
	Session domain.Session
	Groups  []domain.AgentGroup
	Agents  []domain.Agent
}

// ListSessions returns the durable session catalog for the desktop shell. The
// shell needs discovery before a user has selected a session, so this query is
// intentionally separate from ProjectSession's full state tree.
func (o *AgentOrchestrator) ListSessions(
	ctx context.Context,
	limit int,
) ([]domain.Session, error) {
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
	agentID domain.AgentID,
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
	if errors.Is(err, domain.ErrNotFound) {
		active = domain.AgentExecution{}
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
	sessionID domain.SessionID,
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
	groups, err := o.groups.ListBySession(ctx, sessionID, limit)
	if err != nil {
		return SessionProjection{}, err
	}
	agents := make([]domain.Agent, 0)
	for _, group := range groups {
		members, err := o.agents.ListByGroup(ctx, group.ID, limit)
		if err != nil {
			return SessionProjection{}, fmt.Errorf("list agents in group %s: %w", group.ID, err)
		}
		agents = append(agents, members...)
	}
	return SessionProjection{Session: session, Groups: groups, Agents: agents}, nil
}
