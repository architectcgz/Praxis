package app

import (
	"context"
	"errors"
	"sync"
	"time"

	"praxis/internal/agentruntime"
	"praxis/internal/contracts"
	"praxis/internal/core/domain"
	"praxis/internal/core/orchestrate"
	coresession "praxis/internal/core/session"
	"praxis/internal/logging"
	"praxis/internal/providers/registry"
	"praxis/internal/storage"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const agentOutputEventName = "praxis:agent-output"

// App owns the Wails lifecycle context and is the minimal desktop binding entry point.
type App struct {
	mu                sync.RWMutex
	ctx               context.Context
	service           CoreService
	projectRoot       string
	logger            *logging.Logger
	startupIssue      *contracts.StartupIssue
	unsubscribeOutput func()
}

// CoreService is the narrow application-facing surface. The concrete
// orchestrator remains the owner of durable state and command admission.
type CoreService interface {
	Ready() bool
	ProjectSession(context.Context, domain.SessionID, int) (orchestrate.SessionProjection, error)
	ProjectAgent(context.Context, domain.AgentID, int) (orchestrate.AgentProjection, error)
	ListAgentMessages(context.Context, domain.AgentID, int) ([]coresession.AgentSessionMessage, error)
	SendInput(context.Context, orchestrate.SendInputRequest) (orchestrate.SendInputResult, error)
	Resume(context.Context, orchestrate.ResumeRequest) (orchestrate.SendInputResult, error)
	RequestControl(context.Context, orchestrate.ControlRequest) (orchestrate.ControlResult, error)
	EnqueueWork(context.Context, orchestrate.QueueWorkRequest) (orchestrate.QueueWorkResult, error)
}

type sessionCatalogService interface {
	ListSessions(context.Context, int) ([]domain.Session, error)
}

type projectCreatorService interface {
	CreateProject(context.Context, orchestrate.CreateProjectRequest) (orchestrate.CreateSessionResult, error)
}

type sessionCreatorService interface {
	CreateSessionForWorkspace(context.Context, string, string) (orchestrate.CreateSessionResult, error)
}

type modelCatalogService interface {
	ListModels() []registry.ModelOption
}

type runtimeLoggerProvider interface {
	RuntimeLogger() *logging.Logger
}

type agentOutputSubscriber interface {
	SubscribeAgentOutput(agentruntime.AgentOutputObserver) func()
}

func New(loggers ...*logging.Logger) *App {
	factory := logging.NewFactory()
	logger := factory.Nop()
	if len(loggers) > 0 && loggers[0] != nil {
		logger = factory.Ensure(loggers[0])
	}
	return &App{logger: logger}
}

// SetDataRoot keeps project creation on the same resolved root as storage and
// configuration. Wails bindings never choose an independent filesystem root.
func (a *App) SetDataRoot(root storage.DataRoot) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.projectRoot = root.Projects
	a.logger.Infof("data root resolved root=%s", root.Root)
}

// SetCoreService injects the composition-root orchestrator without allowing
// Wails or frontend code to access its repositories directly.
func (a *App) SetCoreService(service CoreService) {
	var unsubscribe func()
	if subscriber, ok := service.(agentOutputSubscriber); ok {
		unsubscribe = subscriber.SubscribeAgentOutput(a.emitAgentOutput)
	}
	a.mu.Lock()
	previousUnsubscribe := a.unsubscribeOutput
	a.service = service
	a.startupIssue = nil
	a.unsubscribeOutput = unsubscribe
	factory := logging.NewFactory()
	a.logger = factory.Nop()
	if provider, ok := service.(runtimeLoggerProvider); ok {
		a.logger = factory.Ensure(provider.RuntimeLogger())
	}
	logger := a.logger
	a.mu.Unlock()
	if previousUnsubscribe != nil {
		previousUnsubscribe()
	}
	logger.Infof("core service injected ready=%t", service != nil && service.Ready())
}

// SetStartupError preserves a safe diagnostic for Readiness when composition
// fails before a CoreService can be exposed to Wails.
func (a *App) SetStartupError(err error) {
	issue := startupIssue(err)
	a.mu.Lock()
	a.startupIssue = issue
	logger := a.logger
	a.mu.Unlock()
	logger.Errorf("startup failed code=%s: %v", issue.Code, err)
}

// Startup stores the Wails runtime context for binding calls.
func (a *App) Startup(ctx context.Context) {
	a.mu.Lock()
	a.ctx = ctx
	logger := a.logger
	a.mu.Unlock()
	logger.Infof("Wails startup callback completed")
}

// Shutdown closes the composition root before releasing the binding context.
// The shutdown budget is detached from Wails' callback context so a canceled
// UI context cannot leave SQLite or a runtime actor open.
func (a *App) Shutdown(ctx context.Context) {
	a.mu.Lock()
	service := a.service
	logger := a.logger
	unsubscribe := a.unsubscribeOutput
	a.service = nil
	a.ctx = nil
	a.unsubscribeOutput = nil
	a.mu.Unlock()
	if unsubscribe != nil {
		unsubscribe()
	}
	logger.Infof("Wails shutdown callback started")
	closer, ok := service.(interface{ Close(context.Context) error })
	if !ok {
		logger.Warnf("core service does not implement Close")
		return
	}
	closeContext := context.Background()
	if ctx != nil {
		closeContext = context.WithoutCancel(ctx)
	}
	closeContext, cancel := context.WithTimeout(closeContext, 30*time.Second)
	defer cancel()
	logger.Infof("Wails shutdown callback closing core service")
	if err := closer.Close(closeContext); err != nil {
		logger.Errorf("core service close failed: %v", err)
		return
	}
}

func (a *App) emitAgentOutput(event agentruntime.AgentOutputEvent) {
	if event.AgentID == "" || event.ExecutionID == "" {
		return
	}
	a.mu.RLock()
	ctx := a.ctx
	a.mu.RUnlock()
	if ctx == nil {
		return
	}
	wailsruntime.EventsEmit(ctx, agentOutputEventName, event)
}

// Ping is a minimal desktop binding smoke endpoint and will be removed once domain bindings stabilize.
func (a *App) Ping() string {
	a.logDebug("binding Ping")
	return "pong"
}

func (a *App) Readiness() contracts.HealthSnapshot {
	a.mu.RLock()
	service, issue := a.service, a.startupIssue
	a.mu.RUnlock()
	if issue != nil {
		copy := *issue
		a.logDebug("binding Readiness ready=false issue=%s", issue.Code)
		return contracts.HealthSnapshot{Ready: false, Issue: &copy}
	}
	ready := service != nil && service.Ready()
	a.logDebug("binding Readiness ready=%t", ready)
	return contracts.HealthSnapshot{Ready: ready}
}

func startupIssue(err error) *contracts.StartupIssue {
	var configurationError *registry.ConfigurationError
	if errors.As(err, &configurationError) {
		message := "Configuration could not be loaded."
		if configurationError.Err != nil {
			message = configurationError.Err.Error()
		}
		return &contracts.StartupIssue{
			Code:    contracts.ErrorCodeConfiguration,
			Path:    configurationError.Path,
			Message: message,
		}
	}
	return &contracts.StartupIssue{
		Code:    contracts.ErrorCodeInternal,
		Message: "Praxis could not start. See the desktop log for details.",
	}
}

func (a *App) bindingContext() (context.Context, CoreService, error) {
	a.mu.RLock()
	ctx, service := a.ctx, a.service
	a.mu.RUnlock()
	if ctx == nil || service == nil || !service.Ready() {
		a.logDebug("binding rejected: core service is not ready")
		return nil, nil, bindingError(contracts.ErrorCodeNotReady)
	}
	return context.WithoutCancel(ctx), service, nil
}

func (a *App) beginBinding(name string) func(error) {
	started := time.Now()
	if commandBinding(name) {
		a.logInfo("binding=%s started", name)
	} else {
		a.logDebug("binding=%s started", name)
	}
	return func(err error) {
		if err != nil {
			a.logError("binding=%s failed: %v", name, err)
			return
		}
		if commandBinding(name) {
			a.logInfo("binding=%s completed duration_ms=%d", name, time.Since(started).Milliseconds())
			return
		}
		a.logDebug("binding=%s completed duration_ms=%d", name, time.Since(started).Milliseconds())
	}
}

func commandBinding(name string) bool {
	switch name {
	case "CreateProject", "CreateSession", "SendInput", "Resume", "RequestControl", "QueueWork":
		return true
	default:
		return false
	}
}

func (a *App) logInfo(format string, args ...any) {
	a.mu.RLock()
	logger := a.logger
	a.mu.RUnlock()
	logger.Infof(format, args...)
}

func (a *App) logDebug(format string, args ...any) {
	a.mu.RLock()
	logger := a.logger
	a.mu.RUnlock()
	logger.Debugf(format, args...)
}

func (a *App) logError(format string, args ...any) {
	a.mu.RLock()
	logger := a.logger
	a.mu.RUnlock()
	logger.Errorf(format, args...)
}

func runtimeSnapshot(sandbox, approval, revision string) (domain.RuntimeExecutionSnapshot, error) {
	return domain.NewRuntimeExecutionSnapshot(
		domain.SandboxMode(sandbox), domain.ApprovalMode(approval), revision,
	)
}
