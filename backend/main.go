package main

import (
	"context"
	"embed"
	"fmt"
	"os"

	"praxis/app"
	"praxis/internal/compose"
	"praxis/internal/logging"
	"praxis/internal/storage"

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
		root, err := storage.ResolveDataRoot("")
		if err != nil {
			a.SetStartupError(err)
			_, _ = fmt.Fprintf(os.Stderr, "fatal: resolve data root: %v\n", err)
			return
		}
		a.SetDataRoot(root)
		service, err := compose.Open(ctx, root, nil)
		if err != nil {
			a.SetStartupError(err)
			_, _ = fmt.Fprintf(os.Stderr, "fatal: open application: %v\n", err)
			return
		}
		a.SetCoreService(service)
	}

	err := wails.Run(&options.App{
		Title:       "Praxis",
		Width:       1280,
		Height:      800,
		AssetServer: &assetserver.Options{Assets: assets},
		OnStartup:   startup,
		OnShutdown:  a.Shutdown,
		Bind:        []interface{}{a},
	})

	if err != nil {
		println("fatal:", err.Error())
	}
}
