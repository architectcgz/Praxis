package app

import "context"

// App owns the Wails lifecycle context and is the minimal desktop binding entry point.
type App struct{ ctx context.Context }

func New() *App { return &App{} }

// Startup stores the runtime context; application services will be wired at this boundary later.
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
}

// Shutdown is the close-phase extension point; P0 currently owns no resources to release.
func (a *App) Shutdown(ctx context.Context) {}

// Ping is a minimal desktop binding smoke endpoint and will be removed once domain bindings stabilize.
func (a *App) Ping() string { return "pong" }
