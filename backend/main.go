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

// acquireInstanceLock 使用固定的用户级路径，切换数据目录也不能绕过单实例约束。
func acquireInstanceLock() (*os.File, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("resolve application instance directory: %w", err)
	}
	directory := filepath.Join(cache, "praxis")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, fmt.Errorf("create application instance directory: %w", err)
	}
	file, err := os.OpenFile(filepath.Join(directory, "instance.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open application instance lock: %w", err)
	}
	if err := lockInstanceFile(file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("acquire application instance lock (Praxis may already be running): %w", err)
	}
	return file, nil
}

func main() {
	instance, err := acquireInstanceLock()
	if err != nil {
		println("fatal:", err.Error())
		return
	}
	// 持有运行权直到 Wails 完成关闭；不删除文件，避免重建文件绕过已有锁。
	defer instance.Close()

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
		application, err := bootstrap.Open(ctx, root, bootstrap.Options{})
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

	err = wailslib.Run(&options.App{
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
