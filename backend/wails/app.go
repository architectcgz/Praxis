package wails

import (
	"context"
	"errors"
	"fmt"
	"praxis/internal/logging"
	"praxis/wails/bindings"
	apperr "praxis/wails/error"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// App 持有桌面应用、前端服务集和关闭句柄，并实现 bindings.Host。
type App struct {
	desktop     *application.App
	mu          sync.RWMutex
	ctx         context.Context
	service     *bindings.Services
	logger      *logging.Logger
	closer      ApplicationCloser
	unsubscribe func()
	startupDone chan struct{}
	startupErr  error
	closed      bool

	bindings *bindings.Bindings
}

func New(desktop *application.App, loggers ...*logging.Logger) *App {
	factory := logging.NewFactory()
	logger := factory.Nop()
	if len(loggers) > 0 && loggers[0] != nil {
		logger = factory.Ensure(loggers[0])
	}
	instance := &App{
		desktop:     desktop,
		logger:      factory.Ensure(logger),
		startupDone: make(chan struct{}),
	}
	instance.bindings = bindings.New(instance)
	return instance
}

// Bindings 返回完整的 Wails 暴露面。
func (a *App) Bindings() []application.Service {
	return a.bindings.All()
}

// ApplicationCloser 关闭生产宿主。
type ApplicationCloser interface {
	Close(context.Context) error
}

// Attach 接入完整桌面依赖并唤醒等待中的 binding；启动失败或关闭后拒绝接入。
func (a *App) Attach(frontend bindings.Services, logger *logging.Logger, closer ApplicationCloser) error {
	if err := frontend.Validate(); err != nil {
		return err
	}
	if closer == nil {
		return errors.New("desktop application closer is required")
	}
	unsubscribe := frontend.Emitter.Subscribe(a.bindings.EmitAgentEvent)
	logger = logging.NewFactory().Ensure(logger)

	a.mu.Lock()
	if a.closed || a.startupErr != nil {
		err := a.startupErr
		if err == nil {
			err = errors.New("desktop application is closed")
		}
		a.mu.Unlock()
		if unsubscribe != nil {
			unsubscribe()
		}
		return err
	}
	previousUnsubscribe := a.unsubscribe
	a.service = &frontend
	a.logger = logger
	a.closer = closer
	a.unsubscribe = unsubscribe
	if a.ctx != nil {
		a.completeStartupLocked()
	}
	a.mu.Unlock()
	if previousUnsubscribe != nil {
		previousUnsubscribe()
	}
	logger.Infof("desktop dependencies attached")
	return nil
}

// Startup 保存非空 Wails context；只有桌面依赖也已接入时才开放 binding。
func (a *App) Startup(ctx context.Context) {
	if ctx == nil {
		a.FailStartup(errors.New("Wails startup context is required"))
		return
	}
	a.mu.Lock()
	if a.closed || a.startupErr != nil {
		a.mu.Unlock()
		return
	}
	a.ctx = ctx
	if a.service != nil {
		a.completeStartupLocked()
	}
	a.mu.Unlock()
	a.logInfo("Wails startup context available")
}

// FailStartup 保存并记录首次启动失败，立即唤醒等待中的 binding；忽略空错误和已结束的启动。
func (a *App) FailStartup(err error) {
	if err == nil {
		return
	}
	a.mu.Lock()
	select {
	case <-a.startupDone:
		a.mu.Unlock()
		return
	default:
	}
	a.startupErr = fmt.Errorf("Praxis startup failed: %w", err)
	a.completeStartupLocked()
	a.mu.Unlock()
	a.logError("desktop startup failed: %v", err)
}

// Shutdown 在释放 binding context 前关闭生产宿主。
func (a *App) Shutdown(ctx context.Context) {
	closer, unsubscribe := a.detach()
	if unsubscribe != nil {
		unsubscribe()
	}
	if closer == nil {
		a.loggerSnapshot().Warnf("desktop application closer is not attached")
		return
	}
	closeContext := context.Background()
	if ctx != nil {
		closeContext = context.WithoutCancel(ctx)
	}
	closeContext, cancel := context.WithTimeout(closeContext, 30*time.Second)
	defer cancel()
	if err := closer.Close(closeContext); err != nil {
		a.logError("application host close failed: %v", err)
	}
}

// BindingContext 等待桌面依赖就绪后返回 context 和服务集，最长等待 30 秒；
// 启动失败透传具体原因，等待期间取消或超时返回对应错误码，关闭后返回 binding_unavailable。
func (a *App) BindingContext() (context.Context, bindings.Services, error) {
	select {
	case <-a.startupDone:
	default:
		a.mu.RLock()
		ctx := a.ctx
		a.mu.RUnlock()
		var canceled <-chan struct{}
		if ctx != nil {
			canceled = ctx.Done()
		}
		timer := time.NewTimer(30 * time.Second)
		defer timer.Stop()
		select {
		case <-a.startupDone:
		case <-canceled:
			return nil, bindings.Services{}, apperr.PublicError(ctx.Err())
		case <-timer.C:
			a.logError("binding startup wait timed out after 30s")
			return nil, bindings.Services{}, apperr.PublicError(fmt.Errorf("timed out waiting for Praxis startup: %w", context.DeadlineExceeded))
		}
	}
	a.mu.RLock()
	ctx, service, startupErr, closed := a.ctx, a.service, a.startupErr, a.closed
	a.mu.RUnlock()
	if startupErr != nil && !closed {
		return nil, bindings.Services{}, apperr.PublicError(startupErr)
	}
	if closed || ctx == nil || service == nil {
		a.logDebug("binding rejected: desktop context is not available")
		return nil, bindings.Services{}, apperr.PublicError(apperr.ErrBindingUnavailable)
	}
	return context.WithoutCancel(ctx), *service, nil
}

// EmitEvent 仅在桌面宿主可用期间发布通知，关闭后忽略迟到事件。
func (a *App) EmitEvent(name string, value any) {
	a.mu.RLock()
	available := !a.closed && a.ctx != nil
	a.mu.RUnlock()
	if available {
		a.desktop.Event.Emit(name, value)
	}
}

// SelectDirectory 将系统对话框附着到当前窗口，取消时返回空路径。
func (a *App) SelectDirectory(title string) (string, error) {
	path, err := a.desktop.Dialog.OpenFile().
		SetTitle(title).
		CanChooseFiles(false).
		CanChooseDirectories(true).
		AttachToWindow(a.desktop.Window.Current()).
		PromptForSingleSelection()
	// Wails 的 Windows 取消错误位于 internal 包，无法引用；仅精确匹配其错误文本，保留其他失败。
	if err != nil && err.Error() == "cancelled by user" {
		return "", nil
	}
	return path, err
}

// LogError 记录 binding 出口已经脱敏前的后端错误，供本地诊断使用。
func (a *App) LogError(format string, args ...any) {
	a.logError(format, args...)
}

// completeStartupLocked 在持有写锁时发布启动结果，避免成功、失败和关闭并发重复关闭通知。
func (a *App) completeStartupLocked() {
	select {
	case <-a.startupDone:
	default:
		close(a.startupDone)
	}
}

// detach 释放桌面 context，并一次性取走关闭句柄。
func (a *App) detach() (ApplicationCloser, func()) {
	a.mu.Lock()
	closer, unsubscribe := a.closer, a.unsubscribe
	a.closed = true
	a.ctx, a.service, a.closer, a.unsubscribe = nil, nil, nil, nil
	a.completeStartupLocked()
	a.mu.Unlock()
	return closer, unsubscribe
}

func (a *App) logInfo(format string, args ...any) {
	a.loggerSnapshot().Infof(format, args...)
}

func (a *App) logDebug(format string, args ...any) {
	a.loggerSnapshot().Debugf(format, args...)
}

func (a *App) logError(format string, args ...any) {
	a.loggerSnapshot().Errorf(format, args...)
}

func (a *App) loggerSnapshot() *logging.Logger {
	a.mu.RLock()
	logger := a.logger
	a.mu.RUnlock()
	return logger
}
