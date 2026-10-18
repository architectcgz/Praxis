package main

import (
	"context"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"praxis/internal/bootstrap"
	"praxis/internal/dataroot"
	"praxis/internal/logging"
	"praxis/wails"
	"strings"

	wailslib "github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

// 嵌入前端构建产物，使发布的二进制不依赖运行时文件。
//
//go:embed all:frontend/dist
var assets embed.FS

const dataRootEnvironment = "PRAXIS_DATA_ROOT"

// startupDataRootPath 选择启动时使用的数据根目录。
func startupDataRootPath() (string, error) {
	root := os.Getenv(dataRootEnvironment)
	if strings.TrimSpace(root) != "" {
		return root, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home directory: %w", err)
	}
	return filepath.Join(home, ".praxis"), nil
}

func main() {
	a := wails.New(logging.NewFactory().Console())
	startup := func(ctx context.Context) {
		a.Startup(ctx)
		rootPath, err := startupDataRootPath()
		if err != nil {
			a.FailStartup(fmt.Errorf("resolve data root path: %w", err))
			return
		}
		root, err := dataroot.Resolve(rootPath)
		if err != nil {
			a.FailStartup(fmt.Errorf("initialize data directory: %w", err))
			return
		}
		application, err := bootstrap.Open(ctx, root, nil)
		if err != nil {
			a.FailStartup(fmt.Errorf("initialize application services: %w", err))
			return
		}
		if err := a.Attach(application.Services(), application.ApplicationLogger(), application); err != nil {
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
