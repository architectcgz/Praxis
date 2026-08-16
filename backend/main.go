package main

import (
	"embed"

	"praxis/app"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

// Embed the frontend build so the release binary does not depend on runtime files.
//
//go:embed all:frontend/dist
var assets embed.FS

func main() {
	a := app.New()

	err := wails.Run(&options.App{
		Title:       "Praxis",
		Width:       1280,
		Height:      800,
		AssetServer: &assetserver.Options{Assets: assets},
		OnStartup:   a.Startup,
		OnShutdown:  a.Shutdown,
		Bind:        []interface{}{a},
	})

	if err != nil {
		println("fatal:", err.Error())
	}
}
