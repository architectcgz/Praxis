package app

import (
	"sync"

	"praxis/internal/logging"
)

// App owns the desktop lifecycle and the domain-specific Wails bindings.
type App struct {
	runtime  *bindingRuntime
	system   *SystemBindings
	projects *ProjectBindings
	sessions *SessionBindings
	agents   *AgentBindings
	commands *CommandBindings
	models   *ModelBindings

	lifecycleMu       sync.Mutex
	closer            ApplicationCloser
	unsubscribeOutput func()
}

func New(loggers ...*logging.Logger) *App {
	factory := logging.NewFactory()
	logger := factory.Nop()
	if len(loggers) > 0 && loggers[0] != nil {
		logger = factory.Ensure(loggers[0])
	}
	runtime := newBindingRuntime(logger)
	return &App{
		runtime:  runtime,
		system:   &SystemBindings{runtime: runtime},
		projects: &ProjectBindings{runtime: runtime},
		sessions: &SessionBindings{runtime: runtime},
		agents:   &AgentBindings{runtime: runtime},
		commands: &CommandBindings{runtime: runtime},
		models:   &ModelBindings{runtime: runtime},
	}
}

// Bindings returns the complete Wails surface as independent domain objects.
func (a *App) Bindings() []interface{} {
	return []interface{}{
		a.system,
		a.projects,
		a.sessions,
		a.agents,
		a.commands,
		a.models,
	}
}
