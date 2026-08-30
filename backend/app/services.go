package app

import (
	"context"
	"errors"

	"praxis/internal/agentruntime"
	"praxis/internal/core/domain"
	"praxis/internal/core/orchestrate"
	coresession "praxis/internal/core/session"
	"praxis/internal/logging"
	"praxis/internal/providers/registry"
)

type ReadinessSource interface {
	Ready() bool
}

type SessionService interface {
	ListSessions(context.Context, int) ([]domain.Session, error)
	ListSessionsByProject(context.Context, domain.ProjectID, int) ([]domain.Session, error)
	CreateSessionForProject(context.Context, domain.ProjectID, domain.WorkspaceID, string) (orchestrate.CreateSessionResult, error)
	ProjectSession(context.Context, domain.SessionID, int) (orchestrate.SessionProjection, error)
}

type ProjectService interface {
	ListProjects(context.Context, int) ([]domain.Project, error)
	CreateProject(context.Context, string) (orchestrate.CreateProjectResult, error)
	ListWorkspaces(context.Context, domain.ProjectID, int) ([]domain.Workspace, error)
}

type AgentQueries interface {
	ProjectAgent(context.Context, domain.AgentID, int) (orchestrate.AgentProjection, error)
	ListAgentMessages(context.Context, domain.AgentID, int) ([]coresession.AgentSessionMessage, error)
}

type AgentCommands interface {
	SendInput(context.Context, orchestrate.SendInputRequest) (orchestrate.SendInputResult, error)
	Resume(context.Context, orchestrate.ResumeRequest) (orchestrate.SendInputResult, error)
	RequestControl(context.Context, orchestrate.ControlRequest) (orchestrate.ControlResult, error)
	EnqueueWork(context.Context, orchestrate.QueueWorkRequest) (orchestrate.QueueWorkResult, error)
}

type ModelCatalog interface {
	ListModels() []registry.ModelOption
}

type ModelConfigEditor interface {
	ModelConfig() registry.FileConfig
	SaveModelConfig(registry.FileConfig) error
	SetProviderKey(string, string) error
	ProviderKey(string) string
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
