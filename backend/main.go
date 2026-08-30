package main

import (
	"context"
	"embed"
	"fmt"
	"os"

	"praxis/app"
	"praxis/internal/compose"
	"praxis/internal/logging"
	"praxis/internal/storage/dataroot"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

// Embed the frontend build so the release binary does not depend on runtime files.
//
//go:embed all:frontend/dist
var assets embed.FS

func main() {
	a := app.New(logging.NewFactory().Console())
	startup := func(ctx context.Context) {
		a.Startup(ctx)
		root, err := dataroot.Resolve("")
		if err != nil {
			a.SetStartupError(err)
			_, _ = fmt.Fprintf(os.Stderr, "fatal: resolve data root: %v\n", err)
			return
		}
		a.SetDataRoot(root)
		application, err := compose.Open(ctx, root, nil)
		if err != nil {
			a.SetStartupError(err)
			_, _ = fmt.Fprintf(os.Stderr, "fatal: open application: %v\n", err)
			return
		}
		if err := a.Attach(app.Dependencies{
			Readiness:   application,
			Sessions:    application,
			Agents:      application,
			Commands:    application,
			Models:      application,
			ModelConfig: application,
			Output:      application,
			Diagnostics: application,
			Lifecycle:   application,
		}); err != nil {
			a.SetStartupError(err)
			_ = application.Close(context.Background())
			_, _ = fmt.Fprintf(os.Stderr, "fatal: attach desktop dependencies: %v\n", err)
		}
	}

	err := wails.Run(&options.App{
		Title:       "Praxis",
		Width:       1280,
		Height:      800,
		AssetServer: &assetserver.Options{Assets: assets},
		OnStartup:   startup,
		OnShutdown:  a.Shutdown,
		Bind:        a.Bindings(),
	})

	if err != nil {
		println("fatal:", err.Error())
	}
}
