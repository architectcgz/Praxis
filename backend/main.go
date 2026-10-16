package main

import (
	"context"
	"embed"
	"fmt"

	"praxis/internal/compose"
	"praxis/internal/infra/dataroot"
	"praxis/internal/logging"
	"praxis/wails"

	wailslib "github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

// 嵌入前端构建产物，使发布的二进制不依赖运行时文件。
//
//go:embed all:frontend/dist
var assets embed.FS

func main() {
	a := wails.New(logging.NewFactory().Console())
	startup := func(ctx context.Context) {
		a.Startup(ctx)
		root, err := dataroot.Resolve("")
		if err != nil {
			a.FailStartup(fmt.Errorf("initialize data directory: %w", err))
			return
		}
		application, err := compose.Open(ctx, root, nil)
		if err != nil {
			a.FailStartup(fmt.Errorf("initialize application services: %w", err))
			return
		}
		if err := a.Attach(application.Services(), application.RuntimeLogger(), application); err != nil {
			a.FailStartup(fmt.Errorf("attach desktop services: %w", err))
			if closeErr := application.Close(context.Background()); closeErr != nil {
				a.LogError("close application after startup failed: %v", closeErr)
			}
		}
	}

	err := wailslib.Run(&options.App{
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
