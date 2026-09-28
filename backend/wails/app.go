package wails

import (
	"context"
	"errors"
	"sync"
	"time"

	"praxis/internal/logging"
	"praxis/wails/bindings"
	apperr "praxis/wails/error"
)

// App 持有 Wails context、前端服务集和关闭句柄，并实现 bindings.Runtime。
type App struct {
	mu          sync.RWMutex
	ctx         context.Context
	service     *bindings.Services
	logger      *logging.Logger
	closer      ApplicationCloser
	unsubscribe func()

	bindings *bindings.Bindings
}

func New(loggers ...*logging.Logger) *App {
	factory := logging.NewFactory()
	logger := factory.Nop()
	if len(loggers) > 0 && loggers[0] != nil {
		logger = factory.Ensure(loggers[0])
	}
	instance := &App{logger: factory.Ensure(logger)}
	instance.bindings = bindings.New(instance)
	return instance
}

// Bindings 返回完整的 Wails 暴露面。
func (a *App) Bindings() []interface{} {
	return a.bindings.All()
}

// ApplicationCloser 关闭生产宿主。
type ApplicationCloser interface {
	Close(context.Context) error
}

// Attach 把前端服务集、桌面 logger 和关闭句柄接入 binding。
func (a *App) Attach(frontend bindings.Services, logger *logging.Logger, closer ApplicationCloser) error {
	if err := frontend.Validate(); err != nil {
		return err
	}
	if closer == nil {
		return errors.New("desktop application closer is required")
	}
	unsubscribe := frontend.Events.SubscribeAgentEvents(a.bindings.EmitAgentEvent)
	logger = logging.NewFactory().Ensure(logger)

	a.mu.Lock()
	a.service = &frontend
	a.mu.Unlock()
	previousUnsubscribe := a.attach(logger, closer, unsubscribe)
	if previousUnsubscribe != nil {
		previousUnsubscribe()
	}
	logger.Infof("desktop dependencies attached")
	return nil
}

// Startup 保存 binding 调用使用的 Wails context。
func (a *App) Startup(ctx context.Context) {
	a.setContext(ctx)
	a.logInfo("Wails startup callback completed")
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

// BindingContext 返回当前 Wails context 和前端服务集；启动前或未 Attach 时返回 binding_unavailable。
func (a *App) BindingContext() (context.Context, bindings.Services, error) {
	a.mu.RLock()
	ctx, service := a.ctx, a.service
	a.mu.RUnlock()
	if ctx == nil || service == nil {
		a.logDebug("binding rejected: desktop context is not available")
		return nil, bindings.Services{}, apperr.CodedError(apperr.ErrorCodeBindingUnavailable)
	}
	return context.WithoutCancel(ctx), *service, nil
}

// EventContext 返回 Wails 事件发布使用的 context。
func (a *App) EventContext() context.Context {
	a.mu.RLock()
	ctx := a.ctx
	a.mu.RUnlock()
	return ctx
}

// LogError 记录 binding 出口已经脱敏前的后端错误，供本地诊断使用。
func (a *App) LogError(format string, args ...any) {
	a.logError(format, args...)
}

func (a *App) setContext(ctx context.Context) {
	a.mu.Lock()
	a.ctx = ctx
	a.mu.Unlock()
}

// attach 保存桌面 logger 与关闭句柄，并返回上一次注册的输出订阅，
// 供调用方替换旧订阅。
func (a *App) attach(logger *logging.Logger, closer ApplicationCloser, unsubscribe func()) func() {
	a.mu.Lock()
	previous := a.unsubscribe
	a.logger = logging.NewFactory().Ensure(logger)
	a.closer = closer
	a.unsubscribe = unsubscribe
	a.mu.Unlock()
	return previous
}

// detach 释放桌面 context，并一次性取走关闭句柄。
func (a *App) detach() (ApplicationCloser, func()) {
	a.mu.Lock()
	closer, unsubscribe := a.closer, a.unsubscribe
	a.ctx, a.service, a.closer, a.unsubscribe = nil, nil, nil, nil
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
