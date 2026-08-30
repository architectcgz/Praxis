package app

import (
	"context"
	"sync"
	"time"

	"praxis/internal/contracts"
	"praxis/internal/logging"
)

type bindingRuntime struct {
	mu           sync.RWMutex
	ctx          context.Context
	readiness    ReadinessSource
	logger       *logging.Logger
	startupIssue *contracts.StartupIssue
}

func newBindingRuntime(logger *logging.Logger) *bindingRuntime {
	return &bindingRuntime{logger: logging.NewFactory().Ensure(logger)}
}

func (r *bindingRuntime) context() (context.Context, error) {
	r.mu.RLock()
	ctx, readiness := r.ctx, r.readiness
	r.mu.RUnlock()
	if ctx == nil || readiness == nil || !readiness.Ready() {
		r.logDebug("binding rejected: application is not ready")
		return nil, bindingError(contracts.ErrorCodeNotReady)
	}
	return context.WithoutCancel(ctx), nil
}

func (r *bindingRuntime) setContext(ctx context.Context) {
	r.mu.Lock()
	r.ctx = ctx
	r.mu.Unlock()
}

func (r *bindingRuntime) attach(readiness ReadinessSource, logger *logging.Logger) {
	r.mu.Lock()
	r.readiness = readiness
	r.logger = logging.NewFactory().Ensure(logger)
	r.startupIssue = nil
	r.mu.Unlock()
}

func (r *bindingRuntime) detach() {
	r.mu.Lock()
	r.ctx = nil
	r.readiness = nil
	r.mu.Unlock()
}

func (r *bindingRuntime) setStartupIssue(issue *contracts.StartupIssue) {
	r.mu.Lock()
	r.startupIssue = issue
	r.mu.Unlock()
}

func (r *bindingRuntime) health() contracts.HealthSnapshot {
	r.mu.RLock()
	readiness, issue := r.readiness, r.startupIssue
	r.mu.RUnlock()
	if issue != nil {
		copy := *issue
		r.logDebug("binding Readiness ready=false issue=%s", issue.Code)
		return contracts.HealthSnapshot{Ready: false, Issue: &copy}
	}
	ready := readiness != nil && readiness.Ready()
	r.logDebug("binding Readiness ready=%t", ready)
	return contracts.HealthSnapshot{Ready: ready}
}

func (r *bindingRuntime) eventContext() context.Context {
	r.mu.RLock()
	ctx := r.ctx
	r.mu.RUnlock()
	return ctx
}

func (r *bindingRuntime) begin(name string) func(error) {
	started := time.Now()
	if commandBinding(name) {
		r.logInfo("binding=%s started", name)
	} else {
		r.logDebug("binding=%s started", name)
	}
	return func(err error) {
		if err != nil {
			r.logError("binding=%s failed: %v", name, err)
			return
		}
		if commandBinding(name) {
			r.logInfo("binding=%s completed duration_ms=%d", name, time.Since(started).Milliseconds())
			return
		}
		r.logDebug("binding=%s completed duration_ms=%d", name, time.Since(started).Milliseconds())
	}
}

func commandBinding(name string) bool {
	switch name {
	case "CreateProject", "CreateSession", "SendInput", "Resume", "RequestControl", "QueueWork":
		return true
	default:
		return false
	}
}

func (r *bindingRuntime) logInfo(format string, args ...any) {
	r.loggerSnapshot().Infof(format, args...)
}

func (r *bindingRuntime) logDebug(format string, args ...any) {
	r.loggerSnapshot().Debugf(format, args...)
}

func (r *bindingRuntime) logError(format string, args ...any) {
	r.loggerSnapshot().Errorf(format, args...)
}

func (r *bindingRuntime) loggerSnapshot() *logging.Logger {
	r.mu.RLock()
	logger := r.logger
	r.mu.RUnlock()
	return logger
}

type serviceRef[T any] struct {
	mu    sync.RWMutex
	value T
}

func (r *serviceRef[T]) set(value T) {
	r.mu.Lock()
	r.value = value
	r.mu.Unlock()
}

func (r *serviceRef[T]) get() T {
	r.mu.RLock()
	value := r.value
	r.mu.RUnlock()
	return value
}
