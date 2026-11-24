package main

import (
	"context"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"praxis/internal/bootstrap"
	"praxis/internal/config"
	"praxis/internal/logging"
	"praxis/wails"
	"praxis/wails/bindings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// 嵌入前端构建产物，使发布的二进制不依赖运行时文件。
//
//go:embed all:frontend/dist
var assets embed.FS

// acquireInstanceLock 在启动数据恢复前获取当前用户的独占运行权。
func acquireInstanceLock() (*os.File, error) {
	path, err := config.InstanceLockPath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create application instance directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
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
	logOutput := os.Stderr
	logLevel, err := config.ParseLogLevel(os.Getenv("PRAXIS_LOG_LEVEL"))
	if err != nil {
		fmt.Fprintln(logOutput, err)
		os.Exit(2)
	}
	logger := logging.New(logOutput, logLevel)
	instance, err := acquireInstanceLock()
	if err != nil {
		logger.Errorf("startup failed: %v", err)
		return
	}
	// 持有运行权直到 Wails 完成关闭；不删除文件，避免重建文件绕过已有锁。
	defer instance.Close()

	desktop := application.New(application.Options{
		Name: "Praxis",
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})
	runtime := config.DefaultRuntime()
	a := wails.New(desktop, logger, runtime.Lifecycle.StartupWait, runtime.Lifecycle.Shutdown)
	desktopBindings := bindings.New(a, bindings.PageConfig{
		DefaultHistorySize: runtime.Pages.DefaultHistorySize,
		MaxHistorySize:     runtime.Pages.MaxHistorySize,
		ListLimit:          runtime.Pages.ListLimit,
	})
	for _, service := range desktopBindings.All() {
		desktop.RegisterService(service)
	}
	// Run 失败也必须关闭业务资源；正常退出时与 Wails 关闭回调共用一次清理。
	shutdown := sync.OnceFunc(func() { a.Shutdown(desktop.Context()) })
	desktop.OnShutdown(shutdown)
	defer shutdown()
	startup := func(ctx context.Context) {
		a.Startup(ctx)
		root, err := config.DefaultDataRoot()
		if err != nil {
			a.FailStartup(fmt.Errorf("initialize data directory: %w", err))
			return
		}
		application, err := bootstrap.Open(ctx, root, runtime, bootstrap.Options{
			OnAgentEvent: desktopBindings.EmitAgentEvent,
			LogOutput:    logOutput,
			LogLevel:     logLevel,
		})
		if err != nil {
			a.FailStartup(fmt.Errorf("initialize application services: %w", err))
			return
		}
		if err := a.Attach(application.Services(), application.ApplicationLogger(), application); err != nil {
			a.FailStartup(fmt.Errorf("attach desktop services: %w", err))
			closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), runtime.Lifecycle.Shutdown)
			defer cancel()
			if closeErr := application.Close(closeCtx); closeErr != nil {
				a.LogError("close application after startup failed: %v", closeErr)
			}
		}
	}

	startup(desktop.Context())
	desktop.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "Praxis",
		Width:  1280,
		Height: 800,
		URL:    "/",
	})

	if err := desktop.Run(); err != nil {
		logger.Errorf("desktop run failed: %v", err)
	}
}
