package app

import (
	"context"
	"errors"
	"time"

	"praxis/internal/contracts"
	"praxis/internal/logging"
	"praxis/internal/providers/registry"
	"praxis/internal/storage/dataroot"
)

// SetDataRoot configures the project domain with the resolved application root.
func (a *App) SetDataRoot(root dataroot.DataRoot) {
	a.runtime.logInfo("data root resolved root=%s", root.Root)
}

// Attach wires each domain binding to its production interface.
func (a *App) Attach(dependencies Dependencies) error {
	if err := dependencies.validate(); err != nil {
		return err
	}
	unsubscribe := dependencies.Output.SubscribeAgentOutput(a.emitAgentOutput)
	logger := logging.NewFactory().Ensure(dependencies.Diagnostics.RuntimeLogger())

	a.sessions.service.set(dependencies.Sessions)
	a.projects.service.set(dependencies.Projects)
	a.agents.queries.set(dependencies.Agents)
	a.events.queries.set(dependencies.Events)
	a.commands.commands.set(dependencies.Commands)
	a.models.catalog.set(dependencies.Models)
	a.models.editor.set(dependencies.ModelConfig)
	a.runtime.attach(dependencies.Readiness, logger)

	a.lifecycleMu.Lock()
	previousUnsubscribe := a.unsubscribeOutput
	a.closer = dependencies.Lifecycle
	a.unsubscribeOutput = unsubscribe
	a.lifecycleMu.Unlock()
	if previousUnsubscribe != nil {
		previousUnsubscribe()
	}
	logger.Infof("desktop dependencies attached ready=%t", dependencies.Readiness.Ready())
	return nil
}

// SetStartupError preserves a safe diagnostic for the system binding.
func (a *App) SetStartupError(err error) {
	issue := startupIssue(err)
	a.runtime.setStartupIssue(issue)
	a.runtime.logError("startup failed code=%s: %v", issue.Code, err)
}

// Startup stores the Wails runtime context for binding calls.
func (a *App) Startup(ctx context.Context) {
	a.runtime.setContext(ctx)
	a.runtime.logInfo("Wails startup callback completed")
}

// Shutdown closes the production host before releasing the binding context.
func (a *App) Shutdown(ctx context.Context) {
	a.lifecycleMu.Lock()
	closer := a.closer
	unsubscribe := a.unsubscribeOutput
	a.closer = nil
	a.unsubscribeOutput = nil
	a.lifecycleMu.Unlock()
	a.runtime.detach()
	if unsubscribe != nil {
		unsubscribe()
	}
	a.runtime.logInfo("Wails shutdown callback started")
	if closer == nil {
		a.runtime.loggerSnapshot().Warnf("desktop application closer is not attached")
		return
	}
	closeContext := context.Background()
	if ctx != nil {
		closeContext = context.WithoutCancel(ctx)
	}
	closeContext, cancel := context.WithTimeout(closeContext, 30*time.Second)
	defer cancel()
	a.runtime.logInfo("Wails shutdown callback closing application host")
	if err := closer.Close(closeContext); err != nil {
		a.runtime.logError("application host close failed: %v", err)
	}
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
