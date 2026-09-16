package app

import (
	"context"
	"errors"
	"time"

	"praxis/internal/application/agent_runtime"
	"praxis/internal/application/execution/control"
	"praxis/internal/application/execution/queue"
	"praxis/internal/application/execution/start"
	"praxis/internal/application/project"
	applicationquery "praxis/internal/application/query"
	applicationsession "praxis/internal/application/session"
	domainfoundation "praxis/internal/domain/foundation"
	domainproject "praxis/internal/domain/project"
	domainsession "praxis/internal/domain/session"
	domainworkspace "praxis/internal/domain/workspace"

	modelregistry "praxis/internal/infrastructure/model_registry"
	"praxis/internal/logging"
	sessionport "praxis/internal/session"
)

type ReadinessSource interface {
	Ready() bool
}

type SessionService interface {
	ListSessions(context.Context, int) ([]domainsession.Session, error)
	ListSessionsByProject(context.Context, domainfoundation.ProjectID, int) ([]domainsession.Session, error)
	CreateSessionForProject(context.Context, domainfoundation.RequestID, domainfoundation.ProjectID, domainfoundation.WorkspaceID, string) (applicationsession.CreateResult, error)
	GetSessionView(context.Context, domainfoundation.SessionID, int) (applicationquery.SessionView, error)
}

type ProjectService interface {
	ListProjects(context.Context, int) ([]domainproject.Project, error)
	CreateProject(context.Context, string, string, domainfoundation.RequestID) (project.CreateProjectResult, error)
	ListWorkspaces(context.Context, domainfoundation.ProjectID, int) ([]domainworkspace.Workspace, error)
}

type AgentQueries interface {
	GetAgentView(context.Context, domainfoundation.AgentID, int) (applicationquery.AgentView, error)
	ListAgentMessages(context.Context, domainfoundation.AgentID, int) ([]sessionport.AgentSessionMessage, error)
}

type EventQueries interface {
	ListSessionEvents(context.Context, domainfoundation.SessionID, time.Time, int) ([]domainfoundation.DomainEvent, error)
	ListAgentEvents(context.Context, domainfoundation.AgentID, time.Time, int) ([]domainfoundation.DomainEvent, error)
	ListExecutionEvents(context.Context, domainfoundation.AgentExecutionID, time.Time, int) ([]domainfoundation.DomainEvent, error)
}

type AgentCommands interface {
	SendInput(context.Context, start.SendInputParams) (start.Result, error)
	Resume(context.Context, start.ResumeParams) (start.Result, error)
	PauseAgent(context.Context, control.Params) (control.Result, error)
	CloseAgent(context.Context, control.Params) (control.Result, error)
	EnqueueWork(context.Context, queue.EnqueueParams) (queue.EnqueueResult, error)
}

type ModelCatalog interface {
	ListModels() []modelregistry.ModelOption
}

type ModelConfigEditor interface {
	ModelConfig() modelregistry.RegistryConfig
	SaveModelConfig(modelregistry.RegistryConfig) error
	SetProviderKey(string, string) error
	HasProviderKey(string) bool
	DiscoverProviderModels(context.Context, string) ([]string, error)
}

type AgentOutputSource interface {
	SubscribeAgentOutput(agentruntime.AgentOutputObserver) func()
}

type DiagnosticsSource interface {
	RuntimeLogger() *logging.Logger
}

type ApplicationCloser interface {
	Close(context.Context) error
}

// Dependencies wires each desktop domain to its narrow production interface.
type Dependencies struct {
	Readiness   ReadinessSource
	Projects    ProjectService
	Sessions    SessionService
	Agents      AgentQueries
	Events      EventQueries
	Commands    AgentCommands
	Models      ModelCatalog
	ModelConfig ModelConfigEditor
	Output      AgentOutputSource
	Diagnostics DiagnosticsSource
	Lifecycle   ApplicationCloser
}

func (d Dependencies) validate() error {
	switch {
	case d.Readiness == nil:
		return errors.New("desktop readiness source is required")
	case d.Sessions == nil:
		return errors.New("desktop session service is required")
	case d.Projects == nil:
		return errors.New("desktop project service is required")
	case d.Agents == nil:
		return errors.New("desktop agent queries are required")
	case d.Events == nil:
		return errors.New("desktop event queries are required")
	case d.Commands == nil:
		return errors.New("desktop agent commands are required")
	case d.Models == nil:
		return errors.New("desktop model catalog is required")
	case d.ModelConfig == nil:
		return errors.New("desktop model configuration editor is required")
	case d.Output == nil:
		return errors.New("desktop agent output source is required")
	case d.Diagnostics == nil:
		return errors.New("desktop diagnostics source is required")
	case d.Lifecycle == nil:
		return errors.New("desktop application closer is required")
	default:
		return nil
	}
}
