// Package logging provides the process-wide diagnostics sink for the desktop
// application. Log records are written to stderr and, when configured, the
// protected runtime log file.
package logging

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
)

// Level controls which records are emitted. The default is InfoLevel so
// lifecycle and command boundaries are visible without logging every query.
type Level uint8

const (
	DebugLevel Level = iota + 1
	InfoLevel
	WarnLevel
	ErrorLevel
)

// Factory centralizes sink selection and environment-based configuration.
// Callers should depend on the returned Logger rather than assembling writers.
type Factory struct {
	level Level
}

// NewFactory creates a logger factory using PRAXIS_LOG_LEVEL when valid.
func NewFactory() Factory {
	result := Factory{level: InfoLevel}
	if level, ok := ParseLevel(os.Getenv("PRAXIS_LOG_LEVEL")); ok {
		result.level = level
	}
	return result
}

// Logger serializes writes so a concurrent runtime cannot interleave records
// or write to a file while it is being closed during Wails shutdown.
type Logger struct {
	mu     sync.Mutex
	logger *log.Logger
	file   *os.File
	level  Level
	closed bool
}

// newLogger creates a logger for a factory-selected sink.
func newLogger(writer io.Writer) *Logger {
	if writer == nil {
		writer = os.Stderr
	}
	return &Logger{
		logger: log.New(writer, "", log.LstdFlags|log.Lmicroseconds),
		level:  InfoLevel,
	}
}

// newNop creates a disabled sink for pre-composition construction paths.
func newNop() *Logger {
	return &Logger{logger: log.New(io.Discard, "", 0), level: ErrorLevel + 1}
}

// Nop returns a disabled logger for pre-composition construction paths.
func (f Factory) Nop() *Logger { return newNop() }

// Ensure replaces a nil logger with a disabled sink at an injection boundary.
func (f Factory) Ensure(logger *Logger) *Logger {
	if logger == nil {
		return f.Nop()
	}
	return logger
}

// Console returns a logger that writes to the current process stderr.
func (f Factory) Console() *Logger {
	result := newLogger(os.Stderr)
	result.SetLevel(f.level)
	return result
}

// Runtime returns a logger that mirrors records to stderr and a protected file.
func (f Factory) Runtime(path string) (*Logger, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, err
	}
	result := newLogger(io.MultiWriter(os.Stderr, file))
	result.file = file
	result.SetLevel(f.level)
	return result, nil
}

// SetLevel changes the minimum emitted level for future records.
func (l *Logger) SetLevel(level Level) {
	if l == nil || level < DebugLevel || level > ErrorLevel {
		return
	}
	l.mu.Lock()
	l.level = level
	l.mu.Unlock()
}

// ParseLevel parses debug, info, warn/warning, or error.
func ParseLevel(value string) (Level, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return DebugLevel, true
	case "info":
		return InfoLevel, true
	case "warn", "warning":
		return WarnLevel, true
	case "error":
		return ErrorLevel, true
	default:
		return 0, false
	}
}

// Debugf records details useful when diagnosing a command or provider call.
func (l *Logger) Debugf(format string, args ...any) { l.logf(DebugLevel, "DEBUG", format, args...) }

// Infof records normal lifecycle and command events.
func (l *Logger) Infof(format string, args ...any) { l.logf(InfoLevel, "INFO", format, args...) }

// Warnf records recoverable conditions.
func (l *Logger) Warnf(format string, args ...any) { l.logf(WarnLevel, "WARN", format, args...) }

// Errorf records failures that require attention.
func (l *Logger) Errorf(format string, args ...any) { l.logf(ErrorLevel, "ERROR", format, args...) }

func (l *Logger) logf(level Level, label, format string, args ...any) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed || level < l.level {
		return
	}
	l.logger.Printf("[%s] %s", label, formatMessage(format, args...))
}

func formatMessage(format string, args ...any) string {
	if len(args) == 0 {
		return format
	}
	return fmt.Sprintf(format, args...)
}

// Close flushes and closes the optional runtime log file.
func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}
